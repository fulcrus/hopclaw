package registry

import (
	"context"
	"reflect"
	"sort"
	"testing"

	channelmgr "github.com/fulcrus/hopclaw/channels/manager"
	"github.com/fulcrus/hopclaw/config"
	"github.com/fulcrus/hopclaw/eventbus"
	"github.com/fulcrus/hopclaw/internal/modules"
	"github.com/fulcrus/hopclaw/plugin"
)

func TestBuildAllReturnsBuiltinInstallations(t *testing.T) {
	t.Parallel()

	result, err := BuildAll(context.Background(), RuntimeDeps{
		Channels: config.ChannelsConfig{
			Slack: config.SlackChannelConfig{
				Enabled:  boolPtr(true),
				BotToken: "xoxb-test",
				AppToken: "xapp-test",
			},
		},
		StorePath:      t.TempDir(),
		ChannelManager: channelmgr.New(),
		Bus:            eventbus.NewInMemoryBus(),
	})
	if err != nil {
		t.Fatalf("BuildAll() error = %v", err)
	}
	if len(result.Installations) != 1 {
		t.Fatalf("len(Installations) = %d, want 1", len(result.Installations))
	}
	if result.Installations[0].Name != "slack" {
		t.Fatalf("installation name = %q, want slack", result.Installations[0].Name)
	}
}

func TestBuildAllReturnsAdditionalBuiltinInstallations(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		want  string
		setup func(*config.Config)
	}{
		{
			name: "bluebubbles",
			want: "bluebubbles",
			setup: func(cfg *config.Config) {
				cfg.Channels.BlueBubbles = config.BlueBubblesChannelConfig{
					Enabled:  boolPtr(true),
					BaseURL:  "https://bluebubbles.example.com",
					Password: "secret",
				}
			},
		},
		{
			name: "synology-chat",
			want: "synology-chat",
			setup: func(cfg *config.Config) {
				cfg.Channels.SynologyChat = config.SynologyChatChannelConfig{
					Enabled:    boolPtr(true),
					WebhookURL: "https://nas.example.com/webhook",
				}
			},
		},
		{
			name: "tlon",
			want: "tlon",
			setup: func(cfg *config.Config) {
				cfg.Channels.Tlon = config.TlonChannelConfig{
					Enabled:  boolPtr(true),
					ShipURL:  "http://localhost:8080",
					ShipCode: "lidlut-tabwed",
				}
			},
		},
		{
			name: "twitch",
			want: "twitch",
			setup: func(cfg *config.Config) {
				cfg.Channels.Twitch = config.TwitchChannelConfig{
					Enabled:    boolPtr(true),
					OAuthToken: "oauth:test",
					Nick:       "hopclawbot",
					Channels:   "#general",
				}
			},
		},
		{
			name: "zalo",
			want: "zalo",
			setup: func(cfg *config.Config) {
				cfg.Channels.Zalo = config.ZaloChannelConfig{
					Enabled:      boolPtr(true),
					AppID:        "app-id",
					SecretKey:    "secret",
					AccessToken:  "token",
					RefreshToken: "refresh",
				}
			},
		},
		{
			name: "zalouser",
			want: "zalouser",
			setup: func(cfg *config.Config) {
				cfg.Channels.ZaloUser = config.ZaloUserChannelConfig{
					Enabled: boolPtr(true),
					Cookie:  "session=demo",
					IMEI:    "1234567890",
					BaseURL: "https://zalo.example.com",
				}
			},
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			cfg := config.Config{
				Store: config.StoreConfig{Path: t.TempDir()},
			}
			tc.setup(&cfg)

			result, err := BuildAll(context.Background(), RuntimeDeps{
				Channels:       cfg.Channels,
				StorePath:      cfg.Store.Path,
				ChannelManager: channelmgr.New(),
				Bus:            eventbus.NewInMemoryBus(),
			})
			if err != nil {
				t.Fatalf("BuildAll() error = %v", err)
			}
			if len(result.Installations) != 1 {
				t.Fatalf("len(Installations) = %d, want 1", len(result.Installations))
			}
			if result.Installations[0].Name != tc.want {
				t.Fatalf("installation name = %q, want %q", result.Installations[0].Name, tc.want)
			}
		})
	}
}

func TestBuiltinRuntimeChannelConfigsUsesDescriptorSnapshots(t *testing.T) {
	t.Parallel()

	cfg := config.Config{
		Channels: config.ChannelsConfig{
			Slack: config.SlackChannelConfig{
				Enabled:  boolPtr(true),
				BotToken: "xoxb-test",
				AppToken: "xapp-test",
			},
			Feishu: config.FeishuChannelConfig{
				Enabled:   boolPtr(true),
				AppID:     "cli_a123",
				AppSecret: "secret",
			},
			Webhook: config.WebhookChannelConfig{
				Enabled: boolPtr(true),
				Instances: map[string]config.WebhookInstanceConfig{
					"ops": {
						CallbackURL: "https://example.com/callback",
						Secret:      "webhook-secret",
					},
				},
			},
		},
	}

	got := BuiltinRuntimeChannelConfigs(cfg)
	want := map[string]any{
		"feishu":      cfg.Channels.Feishu,
		"slack":       cfg.Channels.Slack,
		"webhook:ops": cfg.Channels.Webhook.Instances["ops"],
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("BuiltinRuntimeChannelConfigs() = %#v, want %#v", got, want)
	}
}

func TestBuildAllPreservesBuiltinRuntimeChannelCountAndNames(t *testing.T) {
	t.Parallel()

	cfg := config.Config{
		Store: config.StoreConfig{Path: t.TempDir()},
		Channels: config.ChannelsConfig{
			Feishu: config.FeishuChannelConfig{
				Enabled:   boolPtr(true),
				AppID:     "cli_app_id",
				AppSecret: "cli_app_secret",
			},
			Slack: config.SlackChannelConfig{
				Enabled:  boolPtr(true),
				BotToken: "xoxb-test",
				AppToken: "xapp-test",
			},
			Discord: config.DiscordChannelConfig{
				Enabled:  boolPtr(true),
				BotToken: "discord-bot",
			},
			Telegram: config.TelegramChannelConfig{
				Enabled:  boolPtr(true),
				BotToken: "telegram-bot",
			},
			Webhook: config.WebhookChannelConfig{
				Enabled: boolPtr(true),
				Instances: map[string]config.WebhookInstanceConfig{
					"ops": {CallbackURL: "https://example.com/webhook", Secret: "secret"},
				},
			},
			WhatsApp: config.WhatsAppChannelConfig{
				Enabled:  boolPtr(true),
				PhoneID:  "phone-id",
				APIToken: "api-token",
			},
			Signal: config.SignalChannelConfig{
				Enabled: boolPtr(true),
				BaseURL: "http://127.0.0.1:8080",
				Number:  "+15551234567",
			},
			IMessage: config.IMessageChannelConfig{
				Enabled: boolPtr(true),
				BaseURL: "http://127.0.0.1:8081",
			},
			LINE: config.LINEChannelConfig{
				Enabled:       boolPtr(true),
				ChannelSecret: "channel-secret",
				ChannelToken:  "channel-token",
			},
			MSTeams: config.MSTeamsChannelConfig{
				Enabled:  boolPtr(true),
				AppID:    "app-id",
				Password: "password",
			},
			GoogleChat: config.GoogleChatChannelConfig{
				Enabled:        boolPtr(true),
				ServiceAccount: "service-account",
			},
			IRC: config.IRCChannelConfig{
				Enabled: boolPtr(true),
				Server:  "irc.example.com:6697",
				Nick:    "hopclawbot",
			},
			Matrix: config.MatrixChannelConfig{
				Enabled:     boolPtr(true),
				HomeServer:  "https://matrix.example.com",
				AccessToken: "matrix-token",
			},
			Mattermost: config.MattermostChannelConfig{
				Enabled:  boolPtr(true),
				BaseURL:  "https://mattermost.example.com",
				BotToken: "mattermost-token",
			},
			NextcloudTalk: config.NextcloudTalkChannelConfig{
				Enabled:  boolPtr(true),
				BaseURL:  "https://nextcloud.example.com",
				Username: "hopclaw",
			},
			Nostr: config.NostrChannelConfig{
				Enabled:    boolPtr(true),
				PrivateKey: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
				Relays:     []string{"wss://relay.example.com"},
			},
			BlueBubbles: config.BlueBubblesChannelConfig{
				Enabled:  boolPtr(true),
				BaseURL:  "https://bluebubbles.example.com",
				Password: "secret",
			},
			SynologyChat: config.SynologyChatChannelConfig{
				Enabled:    boolPtr(true),
				WebhookURL: "https://nas.example.com/webhook",
			},
			Tlon: config.TlonChannelConfig{
				Enabled:  boolPtr(true),
				ShipURL:  "http://localhost:8080",
				ShipCode: "lidlut-tabwed",
			},
			Twitch: config.TwitchChannelConfig{
				Enabled:    boolPtr(true),
				OAuthToken: "oauth:test",
				Nick:       "hopclawbot",
				Channels:   "#general",
			},
			Zalo: config.ZaloChannelConfig{
				Enabled:      boolPtr(true),
				AppID:        "app-id",
				SecretKey:    "secret",
				AccessToken:  "token",
				RefreshToken: "refresh",
			},
			ZaloUser: config.ZaloUserChannelConfig{
				Enabled: boolPtr(true),
				Cookie:  "session=demo",
				IMEI:    "1234567890",
				BaseURL: "https://zalo.example.com",
			},
		},
	}

	result, err := BuildAll(context.Background(), RuntimeDeps{
		Channels:       cfg.Channels,
		StorePath:      cfg.Store.Path,
		ChannelManager: channelmgr.New(),
		Bus:            eventbus.NewInMemoryBus(),
	})
	if err != nil {
		t.Fatalf("BuildAll() error = %v", err)
	}

	gotNames := make([]string, 0, len(result.Installations))
	for _, installation := range result.Installations {
		gotNames = append(gotNames, installation.Name)
	}
	sort.Strings(gotNames)

	active := BuiltinRuntimeChannelConfigs(cfg)
	activeNames := make([]string, 0, len(active))
	for name := range active {
		activeNames = append(activeNames, name)
	}
	sort.Strings(activeNames)

	if len(gotNames) != len(activeNames) {
		t.Fatalf("builtin runtime channel count = %d, active config count = %d", len(gotNames), len(activeNames))
	}
	if len(gotNames) != 22 {
		t.Fatalf("builtin runtime channel count = %d, want 22", len(gotNames))
	}
	if !reflect.DeepEqual(gotNames, activeNames) {
		t.Fatalf("installation names = %#v, active names = %#v", gotNames, activeNames)
	}
}

func TestBuildPluginsReturnsPluginWebhookInstallation(t *testing.T) {
	t.Parallel()

	plugins := plugin.NewManager()
	if err := plugins.Register(plugin.LoadedPlugin{
		Dir: t.TempDir(),
		Manifest: plugin.Manifest{
			Name: "demo",
			Channels: map[string]plugin.ChannelDecl{
				"ops": {
					Type:        "webhook",
					CallbackURL: "https://example.com/callback",
					Secret:      "secret",
				},
			},
		},
	}); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	result, err := BuildPlugins(context.Background(), RuntimeDeps{
		StorePath:      t.TempDir(),
		ChannelManager: channelmgr.New(),
		Bus:            eventbus.NewInMemoryBus(),
		ModuleCatalog:  modules.NewStore(modules.BuildCatalog(plugins.Modules())),
	})
	if err != nil {
		t.Fatalf("BuildPlugins() error = %v", err)
	}
	if len(result.Installations) != 1 {
		t.Fatalf("len(Installations) = %d, want 1", len(result.Installations))
	}
	if result.Installations[0].Name != "webhook:demo/ops" {
		t.Fatalf("installation name = %q, want webhook:demo/ops", result.Installations[0].Name)
	}
	if _, ok := result.WebhookAdapters["demo/ops"]; !ok {
		t.Fatalf("webhook adapter keys = %#v, want demo/ops", result.WebhookAdapters)
	}
}

func TestBuildPluginsUsesModuleCatalogChannelProjections(t *testing.T) {
	t.Parallel()

	plugins := plugin.NewManager()
	if err := plugins.Register(plugin.LoadedPlugin{
		Dir: t.TempDir(),
		Manifest: plugin.Manifest{
			Name: "demo",
			Channels: map[string]plugin.ChannelDecl{
				"ops": {
					Type:        "webhook",
					CallbackURL: "https://example.com/callback",
					Secret:      "secret",
				},
			},
		},
	}); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	result, err := BuildPlugins(context.Background(), RuntimeDeps{
		StorePath:      t.TempDir(),
		ChannelManager: channelmgr.New(),
		Bus:            eventbus.NewInMemoryBus(),
		ModuleCatalog:  modules.NewStore(modules.BuildCatalog(plugins.Modules())),
	})
	if err != nil {
		t.Fatalf("BuildPlugins() error = %v", err)
	}
	if len(result.Installations) != 1 {
		t.Fatalf("len(Installations) = %d, want 1", len(result.Installations))
	}
	if result.Installations[0].Name != "webhook:demo/ops" {
		t.Fatalf("installation name = %q, want webhook:demo/ops", result.Installations[0].Name)
	}
	if _, ok := result.WebhookAdapters["demo/ops"]; !ok {
		t.Fatalf("webhook adapter keys = %#v, want demo/ops", result.WebhookAdapters)
	}
}

func TestBuildPluginsDefersManagedPluginChannelStartupUntilActivation(t *testing.T) {
	t.Parallel()

	plugins := plugin.NewManager()
	if err := plugins.Register(plugin.LoadedPlugin{
		Dir: t.TempDir(),
		Manifest: plugin.Manifest{
			Name: "demo",
			Channels: map[string]plugin.ChannelDecl{
				"ops": {
					Type:    "stdio",
					Command: "./demo-channel",
					Args:    []string{"serve"},
				},
			},
		},
	}); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	result, err := BuildPlugins(context.Background(), RuntimeDeps{
		StorePath:      t.TempDir(),
		ChannelManager: channelmgr.New(),
		Bus:            eventbus.NewInMemoryBus(),
		ModuleCatalog:  modules.NewStore(modules.BuildCatalog(plugins.Modules())),
	})
	if err != nil {
		t.Fatalf("BuildPlugins() error = %v", err)
	}
	if len(result.Installations) != 1 {
		t.Fatalf("len(Installations) = %d, want 1", len(result.Installations))
	}
	if result.ProcessManager == nil {
		t.Fatal("expected process manager for managed stdio plugin channel")
	}
	if got := result.ProcessManager.Handles(); len(got) != 0 {
		t.Fatalf("process manager handles after build = %#v, want none before activation", got)
	}
	if result.Installations[0].ManagedProcess == nil {
		t.Fatal("expected managed process plan on stdio plugin installation")
	}
	if result.Installations[0].ManagedProcess.Config.Name != "plugin:demo/ops" {
		t.Fatalf("managed process name = %q, want plugin:demo/ops", result.Installations[0].ManagedProcess.Config.Name)
	}
}

func boolPtr(v bool) *bool {
	return &v
}

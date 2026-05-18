package overlay

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/fulcrus/hopclaw/config"
	controlsnapshot "github.com/fulcrus/hopclaw/internal/controlplane/snapshot"
	"github.com/fulcrus/hopclaw/store"
)

func TestResolveStateMergesBaseAndAPIOverlay(t *testing.T) {
	t.Parallel()

	base := testOverlayBaseConfig()
	reader := staticStoreReader{
		providers: []store.ProviderConfigRow{{
			Name:         "anthropic",
			API:          "anthropic-messages",
			APIKey:       "sk-anthropic",
			DefaultModel: "claude-sonnet-4-5",
			Source:       store.ConfigSourceAPI,
		}},
		channels: []store.ChannelConfigRow{{
			Name:    "slack",
			Config:  `{"bot_token":"api-token"}`,
			Enabled: boolPtr(false),
			Source:  store.ConfigSourceAPI,
		}},
	}

	state, err := ResolveState(context.Background(), base, reader, Options{
		BaseLayers: []controlsnapshot.Layer{{
			Name:   "runtime-config",
			Kind:   "base",
			Source: "test",
		}},
		SnapshotBuilder: func(cfg config.Config, layers []controlsnapshot.Layer) *controlsnapshot.EffectiveConfigSnapshot {
			return controlsnapshot.Build(cfg, controlsnapshot.BuildOptions{
				Layers:      layers,
				GeneratedAt: time.Date(2026, 3, 19, 9, 0, 0, 0, time.UTC),
			})
		},
	})
	if err != nil {
		t.Fatalf("ResolveState() error = %v", err)
	}

	if _, ok := state.Config.Models.Providers["openai"]; !ok {
		t.Fatalf("missing base provider in effective config: %#v", state.Config.Models.Providers)
	}
	if _, ok := state.Config.Models.Providers["anthropic"]; !ok {
		t.Fatalf("missing api overlay provider in effective config: %#v", state.Config.Models.Providers)
	}
	if state.Config.Channels.Slack.Enabled == nil || *state.Config.Channels.Slack.Enabled {
		t.Fatalf("slack enabled = %#v, want disabled by overlay", state.Config.Channels.Slack.Enabled)
	}
	if state.Config.Channels.Slack.BotToken != "api-token" {
		t.Fatalf("slack bot token = %q, want api-token", state.Config.Channels.Slack.BotToken)
	}
	if state.Snapshot == nil {
		t.Fatal("expected non-nil snapshot")
	}
	if !containsLayer(state.Snapshot.Layers, "provider-overlay") || !containsLayer(state.Snapshot.Layers, "channel-overlay") {
		t.Fatalf("snapshot layers = %#v", state.Snapshot.Layers)
	}
	if sourceForProvider(state.Providers, "openai") != "yaml" {
		t.Fatalf("openai source = %q, want yaml", sourceForProvider(state.Providers, "openai"))
	}
	if sourceForProvider(state.Providers, "anthropic") != "api" {
		t.Fatalf("anthropic source = %q, want api", sourceForProvider(state.Providers, "anthropic"))
	}
	if sourceForChannel(state.Channels, "slack") != "api" {
		t.Fatalf("slack source = %q, want api", sourceForChannel(state.Channels, "slack"))
	}
}

func TestResolveStateIgnoresMirroredYAMLRowsWithoutBaseEntry(t *testing.T) {
	t.Parallel()

	base := testOverlayBaseConfig()
	base.Models.Providers = nil
	base.Channels = config.ChannelsConfig{}
	base.Models.DefaultProvider = ""

	state, err := ResolveState(context.Background(), base, staticStoreReader{
		providers: []store.ProviderConfigRow{{
			Name:         "stale-provider",
			API:          "openai-completions",
			DefaultModel: "gpt-4o-mini",
			Source:       store.ConfigSourceYAML,
		}},
		channels: []store.ChannelConfigRow{{
			Name:   "slack",
			Config: `{"bot_token":"stale"}`,
			Source: store.ConfigSourceYAML,
		}},
	}, Options{})
	if err != nil {
		t.Fatalf("ResolveState() error = %v", err)
	}

	if len(state.Providers) != 0 {
		t.Fatalf("Providers = %#v, want empty when only stale yaml mirror exists", state.Providers)
	}
	if len(state.Channels) != 0 {
		t.Fatalf("Channels = %#v, want empty when only stale yaml mirror exists", state.Channels)
	}
}

func TestResolveStateAppliesSettingsOverlay(t *testing.T) {
	t.Parallel()

	base := testOverlayBaseConfig()
	state, err := ResolveState(context.Background(), base, staticStoreReader{
		settings: []store.DynamicSettingRow{{
			Key:    config.SectionOverlayKey("agent"),
			Value:  `{"default_model":"openai/gpt-4.1"}`,
			Source: store.ConfigSourceAPI,
		}},
	}, Options{
		BaseLayers: []controlsnapshot.Layer{{
			Name:   "runtime-config",
			Kind:   "base",
			Source: "test",
		}},
	})
	if err != nil {
		t.Fatalf("ResolveState() error = %v", err)
	}

	if got := state.Config.Agent.DefaultModel; got != "openai/gpt-4.1" {
		t.Fatalf("Agent.DefaultModel = %q, want %q", got, "openai/gpt-4.1")
	}
	if len(state.Settings) != 1 {
		t.Fatalf("Settings = %#v, want one applied overlay", state.Settings)
	}
	if !state.Settings[0].Applied {
		t.Fatalf("Settings[0].Applied = false, want true")
	}
	if state.Settings[0].Legacy {
		t.Fatalf("Settings[0].Legacy = true, want false")
	}
	if !containsLayer(state.Layers, "settings-overlay") {
		t.Fatalf("Layers = %#v, want settings-overlay", state.Layers)
	}
}

func TestResolveStateAppliesLegacySettingsOverlay(t *testing.T) {
	t.Parallel()

	base := testOverlayBaseConfig()
	state, err := ResolveState(context.Background(), base, staticStoreReader{
		settings: []store.DynamicSettingRow{{
			Key:    config.LegacySectionOverlayKey("agent"),
			Value:  `{"default_model":"openai/gpt-4.1"}`,
			Source: store.ConfigSourceAPI,
		}},
	}, Options{})
	if err != nil {
		t.Fatalf("ResolveState() error = %v", err)
	}
	if got := state.Config.Agent.DefaultModel; got != "openai/gpt-4.1" {
		t.Fatalf("Agent.DefaultModel = %q, want %q", got, "openai/gpt-4.1")
	}
	if len(state.Settings) != 1 || !state.Settings[0].Legacy {
		t.Fatalf("Settings = %#v, want one legacy-applied overlay", state.Settings)
	}
}

func TestResolveStateLeavesUnknownSettingsUnapplied(t *testing.T) {
	t.Parallel()

	base := testOverlayBaseConfig()
	state, err := ResolveState(context.Background(), base, staticStoreReader{
		settings: []store.DynamicSettingRow{{
			Key:    "config.section.unknown",
			Value:  `{"foo":"bar"}`,
			Source: store.ConfigSourceAPI,
		}},
	}, Options{})
	if err != nil {
		t.Fatalf("ResolveState() error = %v", err)
	}
	if len(state.Settings) != 1 {
		t.Fatalf("Settings = %#v, want one projection", state.Settings)
	}
	if state.Settings[0].Applied {
		t.Fatalf("Settings[0].Applied = true, want false")
	}
}

func TestResolverVersionAndDiffSince(t *testing.T) {
	t.Parallel()

	base := testOverlayBaseConfig()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("sql.Open() error = %v", err)
	}
	defer db.Close()

	configStore, err := store.NewConfigStore(db)
	if err != nil {
		t.Fatalf("NewConfigStore() error = %v", err)
	}
	resolver, err := NewResolver(context.Background(), base, configStore, Options{})
	if err != nil {
		t.Fatalf("NewResolver() error = %v", err)
	}
	initialVersion := resolver.Version()
	if initialVersion == "" {
		t.Fatal("Version() returned empty string")
	}
	if diff, ok := resolver.DiffSince(initialVersion); !ok || diff.HasChanges() {
		t.Fatalf("DiffSince(current) = (%+v, %v), want no changes and ok=true", diff, ok)
	}

	if err := configStore.UpsertSetting(context.Background(), &store.DynamicSettingRow{
		Key:    config.SectionOverlayKey("agent"),
		Value:  `{"default_model":"openai/gpt-4.1"}`,
		Source: store.ConfigSourceAPI,
	}); err != nil {
		t.Fatalf("UpsertSetting() error = %v", err)
	}
	if err := resolver.Refresh(context.Background()); err != nil {
		t.Fatalf("Refresh() error = %v", err)
	}
	if diff, ok := resolver.DiffSince(initialVersion); !ok || !diff.HasSection("agent") {
		t.Fatalf("DiffSince(previous) = (%+v, %v), want agent change", diff, ok)
	}
}

type staticStoreReader struct {
	providers []store.ProviderConfigRow
	channels  []store.ChannelConfigRow
	settings  []store.DynamicSettingRow
}

func (s staticStoreReader) ListProviders(context.Context) ([]store.ProviderConfigRow, error) {
	return append([]store.ProviderConfigRow(nil), s.providers...), nil
}

func (s staticStoreReader) ListChannels(context.Context) ([]store.ChannelConfigRow, error) {
	return append([]store.ChannelConfigRow(nil), s.channels...), nil
}

func (s staticStoreReader) ListSettings(context.Context) ([]store.DynamicSettingRow, error) {
	return append([]store.DynamicSettingRow(nil), s.settings...), nil
}

func testOverlayBaseConfig() config.Config {
	cfg := config.Config{
		Server: config.ServerConfig{
			AuthToken: "test-token",
		},
		Store: config.StoreConfig{
			Backend: "sqlite",
			Path:    ".hopclaw/state",
		},
		Runtime: config.RuntimeConfig{
			Profile: config.RuntimeProfileProduction,
			Audit: config.AuditConfig{
				Enabled: true,
			},
		},
		Agent: config.AgentConfig{
			DefaultModel: "openai/gpt-4o",
		},
		Models: config.ModelsConfig{
			Providers: map[string]config.ProviderConfig{
				"openai": {
					API:          "openai-completions",
					BaseURL:      "https://api.openai.com/v1",
					APIKey:       "sk-openai",
					DefaultModel: "gpt-4o",
				},
			},
		},
		Channels: config.ChannelsConfig{
			Slack: config.SlackChannelConfig{
				Enabled:  boolPtr(true),
				BotToken: "yaml-token",
			},
		},
	}
	cfg.ApplyDefaults()
	return cfg
}

func containsLayer(items []controlsnapshot.Layer, name string) bool {
	for _, item := range items {
		if item.Name == name {
			return true
		}
	}
	return false
}

func sourceForProvider(items []ProviderProjection, name string) string {
	for _, item := range items {
		if item.Name == name {
			return item.Source
		}
	}
	return ""
}

func sourceForChannel(items []ChannelProjection, name string) string {
	for _, item := range items {
		if item.Name == name {
			return item.Source
		}
	}
	return ""
}

func boolPtr(value bool) *bool {
	return &value
}

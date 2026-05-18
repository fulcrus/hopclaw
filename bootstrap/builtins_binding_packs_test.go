package bootstrap

import (
	"reflect"
	"testing"
	"time"

	"github.com/fulcrus/hopclaw/agent"
	"github.com/fulcrus/hopclaw/artifact"
	browserclient "github.com/fulcrus/hopclaw/browserapi/client"
	"github.com/fulcrus/hopclaw/canvas"
	channelmgr "github.com/fulcrus/hopclaw/channels/manager"
	"github.com/fulcrus/hopclaw/config"
	cronsvc "github.com/fulcrus/hopclaw/cron"
	desktopclient "github.com/fulcrus/hopclaw/desktopapi/client"
	extregistry "github.com/fulcrus/hopclaw/internal/registry/extensions"
	"github.com/fulcrus/hopclaw/isolation"
	"github.com/fulcrus/hopclaw/knowledge"
	"github.com/fulcrus/hopclaw/skill"
	"github.com/fulcrus/hopclaw/toolruntime"
	wakeupsvc "github.com/fulcrus/hopclaw/wakeup"
	watchsvc "github.com/fulcrus/hopclaw/watch"
)

func TestBuiltinsBindingPacksForConfigComposeExpectedFamilies(t *testing.T) {
	t.Parallel()

	sessions := agent.NewInMemorySessionStore()
	memory := agent.NewInMemoryKVStore()
	artifacts := artifact.NewInMemoryStore()
	skills := &skill.Service{}
	hub := skill.NewFileClawHubClient(t.TempDir())
	knowledgeService := &knowledge.Service{}
	channels := channelmgr.New()
	extensions := extregistry.New(extregistry.Options{})
	cronService := &cronsvc.Service{}
	watchService := &watchsvc.Service{}
	wakeupService := &wakeupsvc.Service{}
	browser := &browserclient.Client{}
	desktop := &desktopclient.Client{}
	canvasHost := &canvas.Host{}
	spawner := &isolation.Spawner{}

	app := &App{
		AppStoreState: AppStoreState{
			Sessions:  sessions,
			Artifacts: artifacts,
		},
		AppRuntimeState: AppRuntimeState{
			SkillService:      skills,
			Knowledge:         knowledgeService,
			ExtensionRegistry: extensions,
		},
		AppSurfaceState: AppSurfaceState{
			Channels:      channels,
			CronService:   cronService,
			WatchService:  watchService,
			WakeupService: wakeupService,
			CanvasHost:    canvasHost,
		},
		appInternalState: appInternalState{
			memoryStore:   memory,
			skillHub:      hub,
			browserClient: browser,
			desktopClient: desktop,
			spawner:       spawner,
		},
	}

	cfg := config.Config{
		Channels: config.ChannelsConfig{
			Feishu: config.FeishuChannelConfig{
				Enabled:        boolPtr(true),
				DefaultAccount: "workspace",
				AppID:          "app-1",
				AppSecret:      "secret-1",
				Domain:         "https://open.feishu.cn",
			},
		},
		Tools: config.ToolsConfig{
			Builtins: config.BuiltinsConfig{
				DefaultExecTimeout: 30 * time.Second,
			},
		},
	}

	packs := app.builtinsBindingPacksForConfig(cfg)
	wantPackIDs := []string{
		builtinBindingPackRuntime,
		builtinBindingPackKnowledge,
		builtinBindingPackIntegration,
		builtinBindingPackAutomation,
		firstPartyPackHost,
		builtinBindingPackUI,
		builtinBindingPackAgent,
	}
	if got := builtinsBindingPackIDs(packs); !reflect.DeepEqual(got, wantPackIDs) {
		t.Fatalf("builtins binding pack ids = %#v, want %#v", got, wantPackIDs)
	}

	bindings := composeBuiltinsBindings(packs...)
	if bindings.Sessions != sessions || bindings.MemoryStore != memory || bindings.ArtifactStore != artifacts {
		t.Fatalf("runtime bindings = %#v", bindings)
	}
	if bindings.SkillService != skills || bindings.ClawHub != hub || bindings.ChannelManager != channels || bindings.ExtensionRegistry != extensions {
		t.Fatalf("integration bindings = %#v", bindings)
	}
	if bindings.KnowledgeService != knowledgeService {
		t.Fatalf("knowledge binding = %#v", bindings.KnowledgeService)
	}
	if bindings.CronService != cronService || bindings.WatchService != watchService || bindings.WakeupService != wakeupService {
		t.Fatalf("automation bindings = %#v", bindings)
	}
	if bindings.BrowserClient != browser || bindings.DesktopClient != desktop || bindings.CanvasHost != canvasHost || bindings.Spawner != spawner {
		t.Fatalf("host/agent bindings = %#v", bindings)
	}
	if len(bindings.DestinationCatalog.ChannelAccounts["feishu"]) != 1 {
		t.Fatalf("destination catalog = %#v", bindings.DestinationCatalog)
	}
	if bindings.DestinationCatalog.ChannelAccounts["feishu"][0].ID != "workspace" {
		t.Fatalf("destination account id = %#v", bindings.DestinationCatalog.ChannelAccounts["feishu"][0].ID)
	}
}

func TestWireBuiltinsForConfigLockedAppliesComposedBindingPacks(t *testing.T) {
	t.Parallel()

	sessions := agent.NewInMemorySessionStore()
	channels := channelmgr.New()
	app := &App{
		AppConfigState: AppConfigState{
			Config: config.Config{},
		},
		AppStoreState: AppStoreState{
			Sessions: sessions,
		},
		AppSurfaceState: AppSurfaceState{
			Channels: channels,
		},
		appInternalState: appInternalState{
			builtins: toolruntime.NewBuiltins(toolruntime.BuiltinsConfig{Root: t.TempDir()}),
		},
	}

	cfg := config.Config{
		Channels: config.ChannelsConfig{
			Feishu: config.FeishuChannelConfig{
				Enabled:   boolPtr(true),
				AppID:     "app-1",
				AppSecret: "secret-1",
			},
		},
	}

	app.wireBuiltinsForConfigLocked(cfg)
	bindings := app.builtins.Bindings()
	if bindings.Sessions != sessions {
		t.Fatal("expected sessions binding to be applied")
	}
	if bindings.ChannelManager != channels {
		t.Fatal("expected channel manager binding to be applied")
	}
	if len(bindings.DestinationCatalog.ChannelAccounts["feishu"]) != 1 {
		t.Fatalf("destination catalog = %#v", bindings.DestinationCatalog)
	}
}

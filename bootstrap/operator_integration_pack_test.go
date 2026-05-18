package bootstrap

import (
	"context"
	"reflect"
	"testing"

	channelmgr "github.com/fulcrus/hopclaw/channels/manager"
	"github.com/fulcrus/hopclaw/channels/webhook"
	"github.com/fulcrus/hopclaw/config"
	"github.com/fulcrus/hopclaw/gateway"
	"github.com/fulcrus/hopclaw/internal/modules"
	extregistry "github.com/fulcrus/hopclaw/internal/registry/extensions"
	"github.com/fulcrus/hopclaw/plugin"
	"github.com/fulcrus/hopclaw/skill"
	"github.com/fulcrus/hopclaw/toolruntime"
)

type integrationGatewayTargetStub struct {
	skillService    *skill.Service
	skillHub        skill.ClawHubClient
	channelManager  *channelmgr.Manager
	extensionReg    *extregistry.Registry
	moduleCatalog   *modules.Store
	webhooks        map[string]*webhook.Adapter
	pluginInstaller *plugin.Installer
}

func (s *integrationGatewayTargetStub) ApplyIntegrationServices(services gateway.IntegrationServices) {
	s.skillService = services.SkillService
	s.skillHub = services.SkillHub
	s.channelManager = services.Channels
	s.extensionReg = services.Extensions
	s.moduleCatalog = services.ModuleCatalog
	s.webhooks = services.Webhooks
	s.pluginInstaller = services.PluginInstaller
}

func TestPreparedOperatorIntegrationPackAppliesAcrossTargets(t *testing.T) {
	t.Parallel()

	skillService := &skill.Service{}
	skillHub := skill.NewFileClawHubClient(t.TempDir())
	channelManager := channelmgr.New()
	extensionRegistry := extregistry.New(extregistry.Options{})
	moduleCatalog := modules.NewStore(modules.Catalog{})
	webhooks := map[string]*webhook.Adapter{"ops": nil}
	plugins := plugin.NewManager()
	pluginInstaller := plugin.NewInstaller(plugins)
	processManager := &plugin.ProcessManager{}
	pack := &preparedOperatorIntegrationPack{
		skillService:       skillService,
		skillHub:           skillHub,
		channelManager:     channelManager,
		extensionRegistry:  extensionRegistry,
		moduleCatalog:      moduleCatalog,
		webhooks:           webhooks,
		destinationCatalog: toolruntime.BuildDestinationCatalog(config.Config{}),
		pluginInstaller:    pluginInstaller,
		processManager:     processManager,
	}

	if got := firstPartyPackContributionIDs(pack); !reflect.DeepEqual(got, []string{builtinBindingPackIntegration}) {
		t.Fatalf("firstPartyPackContributionIDs() = %#v", got)
	}

	surface := &preparedBootstrapSurface{}
	pack.applySurface(surface)
	if surface.channels != channelManager || !reflect.DeepEqual(surface.webhooks, webhooks) || surface.pluginInstaller != pluginInstaller || surface.processManager != processManager {
		t.Fatalf("surface wiring = %#v", surface)
	}

	bindings := toolruntime.BuiltinsBindings{}
	pack.applyBuiltins(&bindings)
	if bindings.SkillService != skillService || bindings.ClawHub != skillHub || bindings.ChannelManager != channelManager || bindings.ExtensionRegistry != extensionRegistry {
		t.Fatalf("builtin wiring = %#v", bindings)
	}
	module := pack.module()
	if module.Manifest().ID != builtinBindingPackIntegration || module.Health(context.Background()).Status != modules.HealthReady {
		t.Fatalf("module = %#v health=%#v", module.Manifest(), module.Health(context.Background()))
	}

	target := &integrationGatewayTargetStub{}
	pack.applyIntegrationGateway(target)
	if target.skillService != skillService || target.skillHub != skillHub || target.channelManager != channelManager || target.extensionReg != extensionRegistry || target.moduleCatalog != moduleCatalog || !reflect.DeepEqual(target.webhooks, webhooks) || target.pluginInstaller != pluginInstaller {
		t.Fatalf("gateway wiring = %#v", target)
	}
}

func TestWireIntegrationGatewayForConfigLockedSyncsPluginInstallerManager(t *testing.T) {
	t.Parallel()

	plugins := plugin.NewManager()
	installer := plugin.NewInstaller(nil)
	app := &App{
		AppConfigState: AppConfigState{
			Config: config.Config{},
		},
		AppRuntimeState: AppRuntimeState{
			Plugins: plugins,
		},
		AppSurfaceState: AppSurfaceState{
			PluginInstaller: installer,
		},
	}

	app.wireIntegrationGatewayForConfigLocked(config.Config{})

	if installer.Manager != plugins {
		t.Fatal("expected plugin installer manager to track app plugins")
	}
}

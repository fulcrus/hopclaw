package bootstrap

import (
	"strings"

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

type integrationGatewayTarget interface {
	ApplyIntegrationServices(gateway.IntegrationServices)
}

type preparedOperatorIntegrationPack struct {
	skillService       *skill.Service
	skillHub           skill.ClawHubClient
	channelManager     *channelmgr.Manager
	extensionRegistry  *extregistry.Registry
	moduleCatalog      *modules.Store
	webhooks           map[string]*webhook.Adapter
	destinationCatalog toolruntime.DestinationCatalog
	pluginInstaller    *plugin.Installer
	processManager     *plugin.ProcessManager
}

func newPreparedOperatorIntegrationPack(
	cfg config.Config,
	runtimeCore *preparedBootstrapRuntimeCore,
	base *preparedOperatorBase,
	pluginInstaller *plugin.Installer,
) *preparedOperatorIntegrationPack {
	if runtimeCore == nil || base == nil {
		return nil
	}
	return &preparedOperatorIntegrationPack{
		skillService:       runtimeCore.skillService,
		skillHub:           runtimeCore.skillHub,
		channelManager:     base.channelManager,
		extensionRegistry:  runtimeCore.extensionRegistry,
		moduleCatalog:      runtimeCore.moduleCatalog,
		webhooks:           base.webhooks,
		destinationCatalog: toolruntime.BuildDestinationCatalog(cfg),
		pluginInstaller:    pluginInstaller,
		processManager:     base.processManager,
	}
}

func (a *App) integrationPackForConfig(cfg config.Config) *preparedOperatorIntegrationPack {
	if a == nil {
		return nil
	}
	services := a.integrationServices()
	return &preparedOperatorIntegrationPack{
		skillService:       services.skillService,
		skillHub:           services.skillHub,
		channelManager:     services.channelManager,
		extensionRegistry:  services.extensionRegistry,
		moduleCatalog:      services.moduleCatalog,
		webhooks:           services.webhooks,
		destinationCatalog: toolruntime.BuildDestinationCatalog(cfg),
		pluginInstaller:    services.pluginInstaller,
		processManager:     services.processManager,
	}
}

func (a *App) wireIntegrationGatewayForConfigLocked(cfg config.Config) {
	if a == nil {
		return
	}
	if a.PluginInstaller != nil {
		a.PluginInstaller.Manager = a.Plugins
	}
	applyFirstPartyPackContribution(nil, a.Gateway, nil, a.integrationPackForConfig(cfg))
	a.wireExtensionRegistryLocked()
}

func (p *preparedOperatorIntegrationPack) packID() string {
	if p == nil {
		return ""
	}
	return builtinBindingPackIntegration
}

func (p *preparedOperatorIntegrationPack) moduleExposed() bool {
	return p != nil
}

func (p *preparedOperatorIntegrationPack) module() modules.StaticModule {
	details := map[string]any{
		"skill_service":      p != nil && p.skillService != nil,
		"skill_hub":          p != nil && p.skillHub != nil,
		"channel_manager":    p != nil && p.channelManager != nil,
		"extension_registry": p != nil && p.extensionRegistry != nil,
		"plugin_installer":   p != nil && p.pluginInstaller != nil,
		"process_manager":    p != nil && p.processManager != nil,
	}
	if p != nil && p.channelManager != nil {
		details["channel_count"] = len(p.channelManager.Names())
	}
	if p != nil {
		details["webhook_count"] = len(p.webhooks)
	}

	missing := make([]string, 0, 4)
	switch {
	case p == nil:
		missing = append(missing, "integration-pack")
	default:
		if p.channelManager == nil {
			missing = append(missing, "channels")
		}
		if p.extensionRegistry == nil {
			missing = append(missing, "extension-registry")
		}
		if p.pluginInstaller == nil {
			missing = append(missing, "plugin-installer")
		}
	}

	health := modules.HealthReport{
		Status:  modules.HealthReady,
		Summary: "Channel, plugin installer, and extension registry surfaces are wired.",
		Details: details,
	}
	if len(missing) > 0 {
		health.Status = modules.HealthDegraded
		health.Summary = "Missing integration surfaces: " + strings.Join(missing, ", ")
	}

	return staticFirstPartyPackModule(
		builtinBindingPackIntegration,
		"integration-pack",
		"First-party integration wiring for skills, channels, plugins, and extension registry surfaces.",
		health,
	)
}

func (p *preparedOperatorIntegrationPack) applySurface(surface *preparedBootstrapSurface) {
	if p == nil || surface == nil {
		return
	}
	if surface.channels == nil {
		surface.channels = p.channelManager
	}
	if surface.webhooks == nil {
		surface.webhooks = p.webhooks
	}
	if surface.pluginInstaller == nil {
		surface.pluginInstaller = p.pluginInstaller
	}
	if surface.processManager == nil {
		surface.processManager = p.processManager
	}
}

func (p *preparedOperatorIntegrationPack) applyGateway(gw *gateway.Gateway) {
	if gw == nil {
		return
	}
	gw.ApplyIntegrationServices(gateway.IntegrationServices{
		SkillService:    p.skillService,
		SkillHub:        p.skillHub,
		Channels:        p.channelManager,
		Extensions:      p.extensionRegistry,
		ModuleCatalog:   p.moduleCatalog,
		Webhooks:        p.webhooks,
		PluginInstaller: p.pluginInstaller,
	})
}

func (p *preparedOperatorIntegrationPack) applyIntegrationGateway(target integrationGatewayTarget) {
	if p == nil || target == nil {
		return
	}
	target.ApplyIntegrationServices(gateway.IntegrationServices{
		SkillService:    p.skillService,
		SkillHub:        p.skillHub,
		Channels:        p.channelManager,
		Extensions:      p.extensionRegistry,
		ModuleCatalog:   p.moduleCatalog,
		Webhooks:        p.webhooks,
		PluginInstaller: p.pluginInstaller,
	})
}

func (p *preparedOperatorIntegrationPack) applyBuiltins(bindings *toolruntime.BuiltinsBindings) {
	if p == nil || bindings == nil {
		return
	}
	bindings.SkillService = p.skillService
	bindings.ClawHub = p.skillHub
	bindings.ChannelManager = p.channelManager
	bindings.ExtensionRegistry = p.extensionRegistry
	bindings.DestinationCatalog = p.destinationCatalog.Clone()
}

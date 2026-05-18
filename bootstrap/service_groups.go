package bootstrap

import (
	"context"
	"database/sql"

	"github.com/fulcrus/hopclaw/agent"
	"github.com/fulcrus/hopclaw/approval"
	"github.com/fulcrus/hopclaw/artifact"
	browserclient "github.com/fulcrus/hopclaw/browserapi/client"
	capregistry "github.com/fulcrus/hopclaw/capability/registry"
	channelmgr "github.com/fulcrus/hopclaw/channels/manager"
	"github.com/fulcrus/hopclaw/channels/webhook"
	"github.com/fulcrus/hopclaw/config"
	cronsvc "github.com/fulcrus/hopclaw/cron"
	desktopclient "github.com/fulcrus/hopclaw/desktopapi/client"
	"github.com/fulcrus/hopclaw/discovery"
	"github.com/fulcrus/hopclaw/heartbeat"
	"github.com/fulcrus/hopclaw/hooks"
	controlapproval "github.com/fulcrus/hopclaw/internal/controlplane/approvalflow"
	controlaudit "github.com/fulcrus/hopclaw/internal/controlplane/auditsink"
	controlgov "github.com/fulcrus/hopclaw/internal/controlplane/governanceadapter"
	controloverlay "github.com/fulcrus/hopclaw/internal/controlplane/overlay"
	controlsnapshot "github.com/fulcrus/hopclaw/internal/controlplane/snapshot"
	"github.com/fulcrus/hopclaw/internal/modules"
	extregistry "github.com/fulcrus/hopclaw/internal/registry/extensions"
	"github.com/fulcrus/hopclaw/isolation"
	"github.com/fulcrus/hopclaw/knowledge"
	"github.com/fulcrus/hopclaw/plugin"
	runtimesvc "github.com/fulcrus/hopclaw/runtime"
	"github.com/fulcrus/hopclaw/server"
	"github.com/fulcrus/hopclaw/skill"
	"github.com/fulcrus/hopclaw/toolruntime"
	"github.com/fulcrus/hopclaw/usage"
	"github.com/fulcrus/hopclaw/wakeup"
	"github.com/fulcrus/hopclaw/watch"
	"github.com/fulcrus/hopclaw/wire"
)

type appAutomationServices struct {
	cron      *cronsvc.Service
	watch     *watch.Service
	heartbeat *heartbeat.Service
	wire      *wire.Logger
	wakeup    *wakeup.Service
	deliverer *channelCronDeliverer
}

type appSupportServices struct {
	approvals  approval.Store
	grantStore *approval.GrantStore
	discovery  discovery.Resolver
	hooks      *hooks.Executor
	usageStore usage.Store
}

type appIntegrationServices struct {
	skillService      *skill.Service
	skillHub          skill.ClawHubClient
	channelManager    *channelmgr.Manager
	extensionRegistry *extregistry.Registry
	moduleCatalog     *modules.Store
	webhooks          map[string]*webhook.Adapter
	pluginInstaller   *plugin.Installer
	processManager    *plugin.ProcessManager
}

type runtimePrimitiveServices struct {
	config            config.Config
	artifacts         artifact.Store
	capabilities      *capregistry.Registry
	extensionRegistry *extregistry.Registry
	moduleCatalog     *modules.Store
	skillService      *skill.Service
	knowledgeService  *knowledge.Service
	plugins           *plugin.Manager
	managedHelpers    *managedHelpers
	browserClient     *browserclient.Client
	desktopClient     *desktopclient.Client
	skillWatchStop    context.CancelFunc
	memoryStore       agent.MemoryStore
	baseTools         agent.ToolExecutor
	builtins          *toolruntime.Builtins
	mcpRuntime        pluginMCPRuntime
	modelRuntime      *dynamicModelClient
	routerRuntime     *dynamicModelRouter
	toolRuntime       *dynamicToolExecutor
	skillBinder       *dynamicSkillBinder
	skillHub          skill.ClawHubClient
}

type runtimeControlPlaneServices struct {
	config             config.Config
	policyEngine       agent.PolicyEngine
	usageStore         usage.Store
	approvalRegistry   *controlapproval.ProviderRegistry
	governanceRegistry *controlgov.AdapterRegistry
	auditRegistry      *controlaudit.Registry
	effectiveConfig    *controloverlay.Resolver
	effectiveLayers    []controlsnapshot.Layer
	snapshotBuilder    func(config.Config, []controlsnapshot.Layer) *controlsnapshot.EffectiveConfigSnapshot
	snapshotState      *effectiveSnapshotState
}

type runtimeExecutionServices struct {
	runtime              *runtimesvc.Service
	runtimeRoutes        *server.Server
	governanceDeliveryDB *sql.DB
	governanceDispatcher *controlgov.ReliableDispatcher
	approvalSyncer       *dynamicApprovalDispatcher
	governanceControl    *dynamicGovernanceDispatcher
	grantStore           *approval.GrantStore
	approvalTimeout      *approval.TimeoutService
	artifactPruner       *runtimesvc.ArtifactPruner
	statePruner          *runtimesvc.StatePruner
	spawner              *isolation.Spawner
}

func (a *App) automationServices() appAutomationServices {
	if a == nil {
		return appAutomationServices{}
	}
	return appAutomationServices{
		cron:      a.CronService,
		watch:     a.WatchService,
		heartbeat: a.HeartbeatService,
		wire:      a.WireLogger,
		wakeup:    a.WakeupService,
		deliverer: a.automationDeliverer,
	}
}

func (a *App) supportServices() appSupportServices {
	if a == nil {
		return appSupportServices{}
	}
	return appSupportServices{
		approvals:  a.Approvals,
		grantStore: a.GrantStore,
		discovery:  a.DiscoveryResolver,
		hooks:      a.HookExecutor,
		usageStore: a.usageStore,
	}
}

func (a *App) integrationServices() appIntegrationServices {
	if a == nil {
		return appIntegrationServices{}
	}
	return appIntegrationServices{
		skillService:      a.SkillService,
		skillHub:          a.skillHub,
		channelManager:    a.Channels,
		extensionRegistry: a.ExtensionRegistry,
		moduleCatalog:     a.ModuleCatalog,
		webhooks:          a.Webhooks,
		pluginInstaller:   a.PluginInstaller,
		processManager:    a.processManager,
	}
}

func (p *preparedRuntimeCorePrimitives) services() runtimePrimitiveServices {
	if p == nil {
		return runtimePrimitiveServices{}
	}
	return runtimePrimitiveServices{
		config:            p.config,
		artifacts:         p.artifacts,
		capabilities:      p.capabilities,
		extensionRegistry: p.extensionRegistry,
		moduleCatalog:     p.moduleCatalog,
		skillService:      p.skillService,
		knowledgeService:  p.knowledgeService,
		plugins:           p.plugins,
		managedHelpers:    p.managedHelpers,
		browserClient:     p.browserClient,
		desktopClient:     p.desktopClient,
		skillWatchStop:    p.skillWatchStop,
		memoryStore:       p.memoryStore,
		baseTools:         p.baseTools,
		builtins:          p.builtins,
		mcpRuntime:        p.mcpRuntime,
		modelRuntime:      p.modelRuntime,
		routerRuntime:     p.routerRuntime,
		toolRuntime:       p.toolRuntime,
		skillBinder:       p.skillBinder,
		skillHub:          p.skillHub,
	}
}

func (p *preparedRuntimeControlPlane) services() runtimeControlPlaneServices {
	if p == nil {
		return runtimeControlPlaneServices{}
	}
	return runtimeControlPlaneServices{
		config:             p.config,
		policyEngine:       p.policyEngine,
		usageStore:         p.usageStore,
		approvalRegistry:   p.approvalRegistry,
		governanceRegistry: p.governanceRegistry,
		auditRegistry:      p.auditRegistry,
		effectiveConfig:    p.effectiveConfig,
		effectiveLayers:    p.effectiveLayers,
		snapshotBuilder:    p.snapshotBuilder,
		snapshotState:      p.snapshotState,
	}
}

func (s *preparedRuntimeServices) services() runtimeExecutionServices {
	if s == nil {
		return runtimeExecutionServices{}
	}
	return runtimeExecutionServices{
		runtime:              s.runtime,
		runtimeRoutes:        s.runtimeRoutes,
		governanceDeliveryDB: s.governanceDeliveryDB,
		governanceDispatcher: s.governanceDispatcher,
		approvalSyncer:       s.approvalSyncer,
		governanceControl:    s.governanceControl,
		grantStore:           s.grantStore,
		approvalTimeout:      s.approvalTimeout,
		artifactPruner:       s.artifactPruner,
		statePruner:          s.statePruner,
		spawner:              s.spawner,
	}
}

func assemblePreparedBootstrapRuntimeCore(
	cfg config.Config,
	deps Dependencies,
	foundation *preparedBootstrapFoundation,
	primitives runtimePrimitiveServices,
	component *agent.AgentComponent,
	controlPlane runtimeControlPlaneServices,
	execution runtimeExecutionServices,
	sessionDirectives agent.SessionDirectiveStore,
) *preparedBootstrapRuntimeCore {
	return &preparedBootstrapRuntimeCore{
		config:               cfg,
		artifacts:            primitives.artifacts,
		capabilities:         primitives.capabilities,
		extensionRegistry:    primitives.extensionRegistry,
		moduleCatalog:        primitives.moduleCatalog,
		skillService:         primitives.skillService,
		knowledgeService:     primitives.knowledgeService,
		plugins:              primitives.plugins,
		runtime:              execution.runtime,
		managedHelpers:       primitives.managedHelpers,
		browserClient:        primitives.browserClient,
		desktopClient:        primitives.desktopClient,
		runtimeRoutes:        execution.runtimeRoutes,
		mcpRuntime:           primitives.mcpRuntime,
		governanceDeliveryDB: execution.governanceDeliveryDB,
		governanceDispatcher: execution.governanceDispatcher,
		effectiveConfig:      controlPlane.effectiveConfig,
		effectiveLayers:      controlPlane.effectiveLayers,
		snapshotBuilder:      controlPlane.snapshotBuilder,
		snapshotState:        controlPlane.snapshotState,
		skillWatchStop:       primitives.skillWatchStop,
		memoryStore:          primitives.memoryStore,
		baseTools:            primitives.baseTools,
		builtins:             primitives.builtins,
		modelRuntime:         primitives.modelRuntime,
		routerRuntime:        primitives.routerRuntime,
		toolRuntime:          primitives.toolRuntime,
		skillBinder:          primitives.skillBinder,
		sessionDirectives:    sessionDirectives,
		statusDelay:          cfg.Runtime.StatusReminderDelay,
		skillHub:             primitives.skillHub,
		spawner:              execution.spawner,
		policyOverlay:        deps.Policy,
		customApprovals:      append([]controlapproval.Provider(nil), deps.ApprovalProviders...),
		customGovernance:     append([]controlgov.Adapter(nil), deps.GovernanceAdapters...),
		approvalSyncer:       execution.approvalSyncer,
		governanceControl:    execution.governanceControl,
		auditSink:            foundation.auditSink,
		customModel:          deps.Model != nil,
		customRouter:         deps.Router != nil || deps.Model != nil,
		customTools:          deps.Tools != nil,
		component:            component,
		policyEngine:         controlPlane.policyEngine,
		grantStore:           execution.grantStore,
		approvalTimeout:      execution.approvalTimeout,
		artifactPruner:       execution.artifactPruner,
		statePruner:          execution.statePruner,
		approvalRegistry:     controlPlane.approvalRegistry,
		governanceRegistry:   controlPlane.governanceRegistry,
		auditRegistry:        controlPlane.auditRegistry,
		usageStore:           controlPlane.usageStore,
	}
}

package bootstrap

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/fulcrus/hopclaw/agent"
	"github.com/fulcrus/hopclaw/approval"
	"github.com/fulcrus/hopclaw/artifact"
	browserclient "github.com/fulcrus/hopclaw/browserapi/client"
	"github.com/fulcrus/hopclaw/canvas"
	capregistry "github.com/fulcrus/hopclaw/capability/registry"
	"github.com/fulcrus/hopclaw/channels"
	"github.com/fulcrus/hopclaw/channels/allowlist"
	"github.com/fulcrus/hopclaw/channels/health"
	channelmgr "github.com/fulcrus/hopclaw/channels/manager"
	"github.com/fulcrus/hopclaw/channels/pairing"
	"github.com/fulcrus/hopclaw/channels/webhook"
	"github.com/fulcrus/hopclaw/config"
	"github.com/fulcrus/hopclaw/controlplane"
	cronsvc "github.com/fulcrus/hopclaw/cron"
	desktopclient "github.com/fulcrus/hopclaw/desktopapi/client"
	"github.com/fulcrus/hopclaw/deviceauth"
	"github.com/fulcrus/hopclaw/discovery"
	"github.com/fulcrus/hopclaw/eventbus"
	"github.com/fulcrus/hopclaw/gateway"
	gatewaynodes "github.com/fulcrus/hopclaw/gateway/nodes"
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
	"github.com/fulcrus/hopclaw/keychain"
	"github.com/fulcrus/hopclaw/knowledge"
	"github.com/fulcrus/hopclaw/plugin"
	runtimesvc "github.com/fulcrus/hopclaw/runtime"
	"github.com/fulcrus/hopclaw/sandbox"
	"github.com/fulcrus/hopclaw/server"
	"github.com/fulcrus/hopclaw/skill"
	"github.com/fulcrus/hopclaw/store"
	"github.com/fulcrus/hopclaw/toolruntime"
	"github.com/fulcrus/hopclaw/usage"
	"github.com/fulcrus/hopclaw/wakeup"
	"github.com/fulcrus/hopclaw/watch"
	"github.com/fulcrus/hopclaw/wire"
)

type preparedBootstrapFoundation struct {
	baseConfig          config.Config
	startupWarnings     *startupWarningCollector
	operationalWarnings controlplane.OperationalWarningSource
	sessions            agent.SessionStore
	runs                agent.RunStore
	approvals           approval.Store
	runtimeDB           *sql.DB
	storeDB             *sql.DB
	knowledgeDB         *sql.DB
	auditDB             *sql.DB
	bus                 *eventbus.InMemoryBus
	auditSink           *dynamicAuditSink
	configStore         *store.ConfigStore
}

type preparedBootstrapRuntimeCore struct {
	config               config.Config
	artifacts            artifact.Store
	capabilities         *capregistry.Registry
	extensionRegistry    *extregistry.Registry
	moduleCatalog        *modules.Store
	skillService         *skill.Service
	knowledgeService     *knowledge.Service
	plugins              *plugin.Manager
	runtime              *runtimesvc.Service
	managedHelpers       *managedHelpers
	browserClient        *browserclient.Client
	desktopClient        *desktopclient.Client
	runtimeRoutes        *server.Server
	mcpRuntime           pluginMCPRuntime
	governanceDeliveryDB *sql.DB
	governanceDispatcher *controlgov.ReliableDispatcher
	effectiveConfig      *controloverlay.Resolver
	effectiveLayers      []controlsnapshot.Layer
	snapshotBuilder      func(config.Config, []controlsnapshot.Layer) *controlsnapshot.EffectiveConfigSnapshot
	snapshotState        *effectiveSnapshotState
	skillWatchStop       context.CancelFunc
	memoryStore          agent.MemoryStore
	baseTools            agent.ToolExecutor
	builtins             *toolruntime.Builtins
	modelRuntime         *dynamicModelClient
	routerRuntime        *dynamicModelRouter
	toolRuntime          *dynamicToolExecutor
	skillBinder          *dynamicSkillBinder
	sessionDirectives    agent.SessionDirectiveStore
	statusDelay          time.Duration
	skillHub             skill.ClawHubClient
	spawner              *isolation.Spawner
	policyOverlay        agent.PolicyEngine
	customApprovals      []controlapproval.Provider
	customGovernance     []controlgov.Adapter
	approvalSyncer       *dynamicApprovalDispatcher
	governanceControl    *dynamicGovernanceDispatcher
	auditSink            *dynamicAuditSink
	customModel          bool
	customRouter         bool
	customTools          bool
	component            *agent.AgentComponent
	policyEngine         agent.PolicyEngine
	grantStore           *approval.GrantStore
	approvalTimeout      *approval.TimeoutService
	artifactPruner       *runtimesvc.ArtifactPruner
	statePruner          *runtimesvc.StatePruner
	approvalRegistry     *controlapproval.ProviderRegistry
	governanceRegistry   *controlgov.AdapterRegistry
	auditRegistry        *controlaudit.Registry
	usageStore           usage.Store
}

type preparedBootstrapSurface struct {
	gateway             *gateway.Gateway
	channels            *channelmgr.Manager
	webhooks            map[string]*webhook.Adapter
	cronService         *cronsvc.Service
	watchService        *watch.Service
	heartbeatService    *heartbeat.Service
	wireLogger          *wire.Logger
	wakeupService       *wakeup.Service
	automationDeliverer *channelCronDeliverer
	allowlistManager    *allowlist.Manager
	sandboxRunner       *sandbox.Runner
	isolationManager    *isolation.Manager
	healthMonitor       *health.Monitor
	hookExecutor        *hooks.Executor
	canvasHost          *canvas.Host
	discoveryResolver   discovery.Resolver
	nodeRegistry        *gatewaynodes.Registry
	deviceStore         *deviceauth.Store
	devicePairing       *deviceauth.PairingManager
	threadBindings      *channels.ThreadBinding
	pairingManager      *pairing.Manager
	channelBridges      []namedChannelBridge
	processManager      *plugin.ProcessManager
	pluginInstaller     *plugin.Installer
	keychainWatcher     *keychain.Watcher
}

func prepareBootstrapFoundation(ctx context.Context, cfg config.Config) (*preparedBootstrapFoundation, error) {
	if err := initLogging(cfg.Logging); err != nil {
		return nil, fmt.Errorf("init logging: %w", err)
	}

	baseCfg := cfg
	startupWarnings := newStartupWarningCollector()
	initUpdateChecks(cfg.Update)
	if err := initDiagnostics(cfg.Diagnostics); err != nil {
		startupWarnings.Add("diagnostics", err)
		log.Warn("diagnostics init failed", "error", err)
	}
	if err := initTunnelSupport(cfg.Tunnel); err != nil {
		return nil, err
	}

	layout := resolveStorageLayout(cfg)
	sessions, runs, approvals, dbs, err := initStores(cfg.Store, cfg.Runtime.State, layout)
	if err != nil {
		return nil, err
	}

	bus := eventbus.NewInMemoryBus()
	deliveryWarnings := newDeliveryFailureWarningCollector(0)
	bus.Subscribe(deliveryWarnings)
	runtimeWarnings := newRuntimeEventWarningCollector(0)
	bus.Subscribe(runtimeWarnings)
	runtimeAuditSink, auditErr := newRuntimeAuditSink(cfg, dbs.audit)
	if auditErr != nil {
		startupWarnings.Add("audit_sink", auditErr)
		log.Warn("audit sink init failed", "error", auditErr)
	}
	auditSink := newDynamicAuditSink(runtimeAuditSink)
	bus.Subscribe(auditSink)

	var configStore *store.ConfigStore
	if dbs.control != nil {
		var csErr error
		configStore, csErr = store.NewConfigStore(dbs.control)
		if csErr != nil {
			recordConfigStoreFallbackWarning(startupWarnings, csErr)
			log.Warn("config store init failed, falling back to YAML-only mode", "error", csErr)
		} else {
			syncer := config.NewConfigSyncer(configStore, config.DefaultSyncOptions())
			syncResult, syncErr := syncer.Sync(ctx, &baseCfg)
			if syncErr != nil {
				log.Warn("config sync failed", "error", syncErr)
			} else {
				if len(syncResult.ProvidersImported) > 0 {
					log.Info("config sync: imported providers from YAML", "count", len(syncResult.ProvidersImported))
				}
				if len(syncResult.ProvidersUpdated) > 0 {
					log.Info("config sync: updated providers from YAML", "count", len(syncResult.ProvidersUpdated))
				}
				if len(syncResult.ChannelsImported) > 0 {
					log.Info("config sync: imported channels from YAML", "count", len(syncResult.ChannelsImported))
				}
				if len(syncResult.ChannelsUpdated) > 0 {
					log.Info("config sync: updated channels from YAML", "count", len(syncResult.ChannelsUpdated))
				}
			}
			if migrateErr := config.MigrateConfigStoreSecrets(ctx, configStore); migrateErr != nil {
				log.Warn("config secret migration failed", "error", migrateErr)
			}
		}
	}

	return &preparedBootstrapFoundation{
		baseConfig:          baseCfg,
		startupWarnings:     startupWarnings,
		operationalWarnings: newCombinedOperationalWarningSource(startupWarnings, deliveryWarnings, runtimeWarnings),
		sessions:            sessions,
		runs:                runs,
		approvals:           approvals,
		runtimeDB:           dbs.runtime,
		storeDB:             dbs.control,
		knowledgeDB:         dbs.knowledge,
		auditDB:             dbs.audit,
		bus:                 bus,
		auditSink:           auditSink,
		configStore:         configStore,
	}, nil
}

func prepareBootstrapRuntimeCore(
	ctx context.Context,
	cfg config.Config,
	deps Dependencies,
	foundation *preparedBootstrapFoundation,
	appConfig func() config.Config,
) (*preparedBootstrapRuntimeCore, error) {
	cfg.ResolveSecrets(keychain.ResolveField)

	runtimePrimitives, err := prepareRuntimeCorePrimitives(ctx, cfg, deps, foundation, appConfig)
	if err != nil {
		return nil, err
	}
	primitiveServices := runtimePrimitives.services()

	cfg = primitiveServices.config

	agentSetup := prepareRuntimeAgentComponent(cfg, foundation, runtimePrimitives)
	component := agentSetup.component
	sessionDirectives := agentSetup.sessionDirectives
	triageEngine := agentSetup.triageEngine

	controlPlane, err := prepareRuntimeControlPlane(ctx, cfg, deps, foundation, component)
	if err != nil {
		return nil, err
	}
	controlPlaneServices := controlPlane.services()
	cfg = controlPlaneServices.config

	runtimeServices, err := prepareRuntimeServices(
		ctx,
		cfg,
		foundation,
		component,
		primitiveServices.artifacts,
		primitiveServices.memoryStore,
		sessionDirectives,
		triageEngine,
		controlPlaneServices.policyEngine,
		primitiveServices.moduleCatalog,
		controlPlane,
	)
	if err != nil {
		return nil, err
	}
	return assemblePreparedBootstrapRuntimeCore(
		cfg,
		deps,
		foundation,
		primitiveServices,
		component,
		controlPlaneServices,
		runtimeServices.services(),
		sessionDirectives,
	), nil
}

func prepareBootstrapOperatorSurface(
	ctx context.Context,
	foundation *preparedBootstrapFoundation,
	runtimeCore *preparedBootstrapRuntimeCore,
) (*preparedBootstrapSurface, error) {
	cfg := runtimeCore.config
	base, err := prepareOperatorBase(ctx, cfg, foundation, runtimeCore)
	if err != nil {
		return nil, err
	}

	automationPack := prepareOperatorAutomationPack(ctx, cfg, foundation, runtimeCore, base.channelManager)
	addons := prepareOperatorAddons(ctx, cfg, foundation, runtimeCore, base.channelManager)
	integrationPack := newPreparedOperatorIntegrationPack(cfg, runtimeCore, base, addons.pluginInstaller)
	knowledgePack := newPreparedOperatorKnowledgePack(runtimeCore)
	hostPack := newPreparedOperatorHostPack(runtimeCore)
	supportPack := newPreparedOperatorSupportPack(foundation, runtimeCore, addons)
	uiPack := newPreparedOperatorUIPack(addons)
	activeBridges := activateOperatorChannels(ctx, base.channelManager, base.processManager, base.installations, addons.healthMonitor, foundation.startupWarnings)
	if automationPack != nil && automationPack.deliverer != nil {
		automationPack.deliverer.SetChannels(base.channelManager)
		automationPack.deliverer.MarkReady()
	}

	surface := &preparedBootstrapSurface{
		gateway:             base.gateway,
		webhooks:            base.webhooks,
		nodeRegistry:        base.nodeRegistry,
		deviceStore:         base.deviceStore,
		devicePairing:       base.devicePairing,
		threadBindings:      base.threadBindings,
		pairingManager:      base.pairingManager,
		channelBridges:      activeBridges,
		processManager:      base.processManager,
		automationDeliverer: automationPack.deliverer,
	}
	applyFirstPartyPackContribution(surface, base.gateway, runtimeCore.builtins, integrationPack)
	applyFirstPartyPackContribution(surface, base.gateway, runtimeCore.builtins, knowledgePack)
	applyFirstPartyPackContribution(surface, base.gateway, runtimeCore.builtins, hostPack)
	applyFirstPartyPackContribution(surface, base.gateway, runtimeCore.builtins, supportPack)
	applyFirstPartyPackContribution(surface, base.gateway, runtimeCore.builtins, automationPack)
	applyFirstPartyPackContribution(surface, base.gateway, runtimeCore.builtins, uiPack)
	return surface, nil
}

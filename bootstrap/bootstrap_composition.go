package bootstrap

import (
	"context"
	"strings"

	"github.com/fulcrus/hopclaw/config"
	controlapproval "github.com/fulcrus/hopclaw/internal/controlplane/approvalflow"
	controlgov "github.com/fulcrus/hopclaw/internal/controlplane/governanceadapter"
)

type bootstrapDraft struct {
	foundation  *preparedBootstrapFoundation
	runtimeCore *preparedBootstrapRuntimeCore
	app         *App
}

func New(ctx context.Context, cfg config.Config, deps Dependencies) (*App, error) {
	draft, err := prepareBootstrapDraft(ctx, cfg, deps)
	if err != nil {
		return nil, err
	}

	closeAppOnError := true
	defer func() {
		if closeAppOnError {
			_ = draft.app.Close(context.Background())
		}
	}()

	if err := finalizeBootstrapDraft(ctx, deps, draft); err != nil {
		return nil, err
	}

	draft.foundation.startupWarnings.LogSummary()
	closeAppOnError = false
	return draft.app, nil
}

func prepareBootstrapDraft(ctx context.Context, cfg config.Config, deps Dependencies) (*bootstrapDraft, error) {
	foundation, err := prepareBootstrapFoundation(ctx, cfg)
	if err != nil {
		return nil, err
	}

	closeFoundationOnError := true
	defer func() {
		if closeFoundationOnError {
			if foundation.runtimeDB != nil {
				_ = foundation.runtimeDB.Close()
			}
			if foundation.storeDB != nil {
				_ = foundation.storeDB.Close()
			}
			if foundation.knowledgeDB != nil {
				_ = foundation.knowledgeDB.Close()
			}
			if foundation.auditDB != nil {
				_ = foundation.auditDB.Close()
			}
		}
	}()

	currentConfig := foundation.baseConfig
	var appRef *App
	runtimeCore, err := prepareBootstrapRuntimeCore(ctx, cfg, deps, foundation, func() config.Config {
		if appRef != nil {
			return appRef.Config
		}
		return currentConfig
	})
	if err != nil {
		return nil, err
	}
	currentConfig = runtimeCore.config

	app := assembleBootstrapApp(foundation, runtimeCore)
	appRef = app
	closeFoundationOnError = false

	return &bootstrapDraft{
		foundation:  foundation,
		runtimeCore: runtimeCore,
		app:         app,
	}, nil
}

func finalizeBootstrapDraft(ctx context.Context, deps Dependencies, draft *bootstrapDraft) error {
	surface, err := prepareBootstrapOperatorSurface(ctx, draft.foundation, draft.runtimeCore)
	if err != nil {
		return err
	}
	applyBootstrapSurface(draft.app, surface)
	return finalizeBootstrapRuntimeWiring(ctx, deps, draft.foundation, draft.runtimeCore, draft.app)
}

func assembleBootstrapApp(foundation *preparedBootstrapFoundation, runtimeCore *preparedBootstrapRuntimeCore) *App {
	if foundation == nil || runtimeCore == nil {
		return &App{}
	}
	return &App{
		AppConfigState: AppConfigState{
			BaseConfig:  foundation.baseConfig,
			Config:      runtimeCore.config,
			ConfigStore: foundation.configStore,
		},
		AppStoreState: AppStoreState{
			Sessions:   foundation.sessions,
			Runs:       foundation.runs,
			Approvals:  foundation.approvals,
			Artifacts:  runtimeCore.artifacts,
			GrantStore: runtimeCore.grantStore,
		},
		AppRuntimeState: AppRuntimeState{
			Bus:               foundation.bus,
			Capabilities:      runtimeCore.capabilities,
			ExtensionRegistry: runtimeCore.extensionRegistry,
			ModuleCatalog:     runtimeCore.moduleCatalog,
			SkillService:      runtimeCore.skillService,
			Knowledge:         runtimeCore.knowledgeService,
			Plugins:           runtimeCore.plugins,
			Runtime:           runtimeCore.runtime,
			ManagedHelpers:    runtimeCore.managedHelpers,
			ApprovalTimeout:   runtimeCore.approvalTimeout,
			ArtifactPruner:    runtimeCore.artifactPruner,
			StatePruner:       runtimeCore.statePruner,
		},
		appInternalState: appInternalState{
			runtimeDB:            foundation.runtimeDB,
			storeDB:              foundation.storeDB,
			knowledgeDB:          foundation.knowledgeDB,
			auditDB:              foundation.auditDB,
			governanceDeliveryDB: runtimeCore.governanceDeliveryDB,
			governanceDispatcher: runtimeCore.governanceDispatcher,
			effectiveConfig:      runtimeCore.effectiveConfig,
			effectiveLayers:      runtimeCore.effectiveLayers,
			snapshotBuilder:      runtimeCore.snapshotBuilder,
			snapshotState:        runtimeCore.snapshotState,
			runtimeRoutes:        runtimeCore.runtimeRoutes,
			mcpRuntime:           runtimeCore.mcpRuntime,
			skillWatchStop:       runtimeCore.skillWatchStop,
			memoryStore:          runtimeCore.memoryStore,
			baseTools:            runtimeCore.baseTools,
			builtins:             runtimeCore.builtins,
			modelRuntime:         runtimeCore.modelRuntime,
			routerRuntime:        runtimeCore.routerRuntime,
			toolRuntime:          runtimeCore.toolRuntime,
			skillBinder:          runtimeCore.skillBinder,
			sessionDirectives:    runtimeCore.sessionDirectives,
			statusDelay:          runtimeCore.statusDelay,
			skillHub:             runtimeCore.skillHub,
			browserClient:        runtimeCore.browserClient,
			desktopClient:        runtimeCore.desktopClient,
			spawner:              runtimeCore.spawner,
			policyOverlay:        runtimeCore.policyOverlay,
			customApprovals:      append([]controlapproval.Provider(nil), runtimeCore.customApprovals...),
			customGovernance:     append([]controlgov.Adapter(nil), runtimeCore.customGovernance...),
			approvalSyncer:       runtimeCore.approvalSyncer,
			governanceControl:    runtimeCore.governanceControl,
			auditSink:            runtimeCore.auditSink,
			usageStore:           runtimeCore.usageStore,
			customModel:          runtimeCore.customModel,
			customRouter:         runtimeCore.customRouter,
			customTools:          runtimeCore.customTools,
			startupWarnings:      foundation.startupWarnings,
			operationalWarnings:  foundation.operationalWarnings,
		},
	}
}

func applyBootstrapSurface(app *App, surface *preparedBootstrapSurface) {
	if app == nil || surface == nil {
		return
	}
	app.applySurfaceIntegrationState(surface.integrationState())
	app.applySurfaceAutomationState(surface.automationState())
	app.applySurfaceSupportState(surface.supportState())
	app.applySurfaceDeviceState(surface.deviceState())
}

func finalizeBootstrapRuntimeWiring(
	ctx context.Context,
	deps Dependencies,
	foundation *preparedBootstrapFoundation,
	runtimeCore *preparedBootstrapRuntimeCore,
	app *App,
) error {
	if app == nil {
		return validateAppIntegrity(ctx, nil)
	}
	app.refreshModuleCatalogLocked()
	app.wireBuiltinsForConfigLocked(app.Config)
	app.wireHostPackLocked()
	app.wireSupportGatewayLocked()
	app.wireIntegrationGatewayForConfigLocked(app.Config)
	app.wireGatewayRuntimeKernelLocked(foundation.configStore)
	wireRuntimeRoutesApprovalCallbacks(app.runtimeRoutes, runtimeCore.approvalRegistry)
	app.refreshMu.Lock()
	app.restartPluginWatcherLocked(ctx)
	app.refreshMu.Unlock()
	startBootstrapConfigWatcher(ctx, strings.TrimSpace(deps.ConfigPath), foundation.baseConfig, app)
	return validateAppIntegrity(ctx, app)
}

func startBootstrapConfigWatcher(ctx context.Context, configPath string, baseConfig config.Config, app *App) {
	if app == nil || configPath == "" {
		return
	}
	reloader := NewReloader(app)
	cw := config.NewWatcher(configPath, baseConfig, 0)
	cw.OnReloadV2(reloader.HandleReload)
	app.wireGatewayConfigWatcherLocked(cw, configPath)
	watchCtx, watchCancel := context.WithCancel(ctx)
	go cw.Run(watchCtx)
	app.configWatcher = cw
	app.configWatcherStop = watchCancel
	log.Info("config watcher started", "path", configPath)
}

package bootstrap

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/fulcrus/hopclaw/agent"
	"github.com/fulcrus/hopclaw/approval"
	"github.com/fulcrus/hopclaw/artifact"
	"github.com/fulcrus/hopclaw/config"
	controlgov "github.com/fulcrus/hopclaw/internal/controlplane/governanceadapter"
	"github.com/fulcrus/hopclaw/internal/modules"
	"github.com/fulcrus/hopclaw/isolation"
	"github.com/fulcrus/hopclaw/policy"
	runtimesvc "github.com/fulcrus/hopclaw/runtime"
	verifyrt "github.com/fulcrus/hopclaw/runtime/verify"
	"github.com/fulcrus/hopclaw/server"
	"github.com/fulcrus/hopclaw/triage"
)

type preparedRuntimeServices struct {
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

func prepareRuntimeServices(
	ctx context.Context,
	cfg config.Config,
	foundation *preparedBootstrapFoundation,
	component *agent.AgentComponent,
	artifactStore artifact.Store,
	memoryStore agent.MemoryStore,
	sessionDirectives agent.SessionDirectiveStore,
	triageEngine *triage.ModelTriage,
	policyEngine agent.PolicyEngine,
	moduleCatalog *modules.Store,
	controlPlane *preparedRuntimeControlPlane,
) (*preparedRuntimeServices, error) {
	stateRetentionPolicy := runtimesvc.DataRetentionPolicy{
		Sessions: cfg.Runtime.State.SessionsRetention,
		Runs:     cfg.Runtime.State.RunsRetention,
		Events:   cfg.Runtime.State.EventsRetention,
		Interval: cfg.Runtime.State.PruneInterval,
	}
	runtimeService := runtimesvc.NewService(component, foundation.sessions, foundation.runs, foundation.approvals, foundation.bus, artifactStore).
		WithMemoryStore(memoryStore).
		WithArtifactRetention(cfg.Runtime.Artifacts.Retention).
		WithDataRetention(stateRetentionPolicy).
		WithDirectives(sessionDirectives).
		WithMinActionConfidence(cfg.Agent.MinActionConfidence).
		WithReleaseExecutionGate(runtimesvc.DefaultReleaseExecutionGatePolicy()).
		WithVerificationPolicy(verifyrt.PolicyFromStrings(cfg.Runtime.Verification.VerifierSeverities)).
		WithIngressClassifier(runtimesvc.NewTriageInteractionIngressClassifier(triageEngine)).
		WithEventReader(controlPlane.runtimeEventReader)
	runtimeService.WithEffectiveConfigSnapshot(controlPlane.effectiveConfig.Snapshot())

	approvalSyncer := &dynamicApprovalDispatcher{}
	approvalSyncer.Swap(approvalDispatcherForRuntime(runtimeService, controlPlane.approvalRegistry))
	runtimeService.WithApprovalSyncer(approvalSyncer)
	foundation.bus.Subscribe(approvalSyncer)

	governanceControl := &dynamicGovernanceDispatcher{}
	governanceDispatcher, governanceDeliveryDB, err := newGovernanceDispatcher(cfg, foundation.storeDB, foundation.bus, runtimeService, controlPlane.governanceAdapters)
	if err != nil {
		return nil, fmt.Errorf("init governance delivery store: %w", err)
	}
	if governanceDispatcher != nil {
		governanceControl.Swap(ctx, governanceDispatcher)
	}
	runtimeService.WithGovernanceDelivery(governanceControl)
	foundation.bus.Subscribe(governanceControl)

	approvalTimeoutSvc := newApprovalTimeoutService(cfg.ExecApproval, foundation.approvals,
		func(ctx context.Context, ticketID string) (*approval.Ticket, error) {
			return component.ResolveApproval(ctx, ticketID, approval.Resolution{
				Status:     approval.StatusCancelled,
				ResolvedBy: "system_timeout",
				Note:       "approval timed out",
			})
		},
		foundation.bus,
		runtimeService,
		foundation.startupWarnings,
	)
	if approvalTimeoutSvc != nil {
		approvalTimeoutSvc.Start(ctx)
	}

	runtimeService.SetAgentRouter(buildPluginAgentRouter(moduleCatalog))
	if cfg.Agent.SubmitRateLimit.RequestsPerMinute > 0 {
		burstSize := cfg.Agent.SubmitRateLimit.BurstSize
		if burstSize <= 0 {
			burstSize = 5
		}
		runtimeService.WithRateLimiter(runtimesvc.NewSessionRateLimiter(cfg.Agent.SubmitRateLimit.RequestsPerMinute, burstSize))
	}

	var artifactPruner *runtimesvc.ArtifactPruner
	if cfg.Runtime.Artifacts.Retention > 0 && artifactStore != nil {
		artifactPruner = runtimesvc.NewArtifactPruner(runtimeService, 0)
		artifactPruner.Start(ctx)
	}
	if recovered, recoverErr := runtimeService.RecoverOrphanedRuns(ctx, "process_restart"); recoverErr != nil {
		log.Warn("orphaned run recovery failed", "error", recoverErr)
	} else if recovered > 0 {
		log.Warn("recovered orphaned runs after process restart", "count", recovered)
	}

	var statePruner *runtimesvc.StatePruner
	if stateRetentionPolicy.Normalize().Enabled() {
		if _, err := runtimeService.PruneState(ctx); err != nil {
			log.Warn("initial state prune failed", "error", err)
		}
		statePruner = runtimesvc.NewStatePruner(runtimeService, stateRetentionPolicy.Interval)
		statePruner.Start(ctx)
	}

	runtimeRoutes := server.New(runtimeService, server.Config{
		AuthToken:           cfg.Server.AuthToken,
		MaxEventResults:     200,
		ApprovalCallbacks:   controlPlane.approvalRegistry.CallbackPolicies(),
		OperationalWarnings: foundation.operationalWarnings,
	})

	grantStore := approval.NewGrantStore()
	policy.WireGrantStore(policyEngine, grantStore)
	runtimeService.WithGrantStore(grantStore)

	spawner := isolation.NewSpawner(func(ctx context.Context, sessionKey, message string) (string, error) {
		run, err := runtimeService.Submit(ctx, runtimesvc.SubmitRequest{
			SessionKey: sessionKey,
			Content:    message,
		})
		if err != nil {
			return "", err
		}
		return run.ID, nil
	})
	return &preparedRuntimeServices{
		runtime:              runtimeService,
		runtimeRoutes:        runtimeRoutes,
		governanceDeliveryDB: governanceDeliveryDB,
		governanceDispatcher: governanceDispatcher,
		approvalSyncer:       approvalSyncer,
		governanceControl:    governanceControl,
		grantStore:           grantStore,
		approvalTimeout:      approvalTimeoutSvc,
		artifactPruner:       artifactPruner,
		statePruner:          statePruner,
		spawner:              spawner,
	}, nil
}

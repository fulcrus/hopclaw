package bootstrap

import (
	"context"
	"database/sql"

	"github.com/fulcrus/hopclaw/agent"
	"github.com/fulcrus/hopclaw/approval"
	"github.com/fulcrus/hopclaw/eventbus"
	controlapproval "github.com/fulcrus/hopclaw/internal/controlplane/approvalflow"
	controlaudit "github.com/fulcrus/hopclaw/internal/controlplane/auditsink"
	controlgov "github.com/fulcrus/hopclaw/internal/controlplane/governanceadapter"
	"github.com/fulcrus/hopclaw/internal/edition"
	runtimesvc "github.com/fulcrus/hopclaw/runtime"
)

func approvalDispatcherForRuntime(runtime *runtimesvc.Service, registry *controlapproval.ProviderRegistry) *controlapproval.Dispatcher {
	if runtime == nil || registry == nil {
		return nil
	}
	if providers := registry.Providers(); len(providers) > 0 {
		return controlapproval.NewDispatcher(runtime, providers...)
	}
	return nil
}

func (a *App) applyRuntimePolicyEngine(engine agent.PolicyEngine) {
	if a == nil {
		return
	}
	if component := a.runtimeComponent(); component != nil {
		component.WithPolicy(engine)
	}
	a.wireGatewayControlPlaneLocked(engine, nil, nil, nil)
}

func (a *App) applyRuntimeApprovalRegistry(registry *controlapproval.ProviderRegistry) {
	if a == nil {
		return
	}
	a.wireGatewayControlPlaneLocked(nil, registry, nil, nil)
	wireRuntimeRoutesApprovalCallbacks(a.runtimeRoutes, registry)
	if a.approvalSyncer != nil {
		a.approvalSyncer.Swap(approvalDispatcherForRuntime(a.Runtime, registry))
	}
}

func (a *App) applyRuntimeGovernanceRegistry(
	registry *controlgov.AdapterRegistry,
	dispatch *controlgov.ReliableDispatcher,
	deliveryDB *sql.DB,
) {
	if a == nil {
		return
	}
	a.wireGatewayControlPlaneLocked(nil, nil, registry, nil)
	if a.governanceControl != nil {
		a.governanceControl.replace(dispatch)
	}
	a.governanceDispatcher = dispatch
	a.governanceDeliveryDB = deliveryDB
}

func (a *App) applyRuntimeAuditRegistry(ctx context.Context, registry *controlaudit.Registry, sink eventbus.Sink) {
	if a == nil {
		return
	}
	if a.auditSink != nil {
		a.auditSink.Swap(ctx, sink)
	}
	a.wireGatewayControlPlaneLocked(nil, nil, nil, registry)
}

func (a *App) applyApprovalTimeout(ctx context.Context, previous, next *approval.TimeoutService) {
	if a == nil {
		return
	}
	if previous != nil && previous != next {
		previous.Stop()
	}
	a.ApprovalTimeout = next
	if a.ApprovalTimeout != nil {
		a.ApprovalTimeout.Start(ctx)
	}
}

func (a *App) updateSnapshotStateFromControlPlane(state *controlPlaneRuntimeState) {
	if a == nil || a.snapshotState == nil || state == nil {
		return
	}
	a.snapshotState.UpdateFromControlPlane(
		edition.Edition,
		a.policyOverlay != nil,
		state,
	)
}

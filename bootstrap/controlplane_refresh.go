package bootstrap

import (
	"context"
	"sync"

	"github.com/fulcrus/hopclaw/agent"
	"github.com/fulcrus/hopclaw/audit"
	"github.com/fulcrus/hopclaw/config"
	"github.com/fulcrus/hopclaw/controlplane"
	"github.com/fulcrus/hopclaw/eventbus"
	controlapproval "github.com/fulcrus/hopclaw/internal/controlplane/approvalflow"
	controlaudit "github.com/fulcrus/hopclaw/internal/controlplane/auditsink"
	controlgov "github.com/fulcrus/hopclaw/internal/controlplane/governanceadapter"
	controlpolicy "github.com/fulcrus/hopclaw/internal/controlplane/policypack"
	controlsnapshot "github.com/fulcrus/hopclaw/internal/controlplane/snapshot"
	runtimesvc "github.com/fulcrus/hopclaw/runtime"
)

type effectiveSnapshotState struct {
	mu                 sync.RWMutex
	edition            string
	resolvedPolicy     controlpolicy.Resolved
	hasPolicyOverlay   bool
	policyEngine       agent.PolicyEngine
	approvalRegistry   *controlapproval.ProviderRegistry
	governanceRegistry *controlgov.AdapterRegistry
	auditRegistry      *controlaudit.Registry
}

type effectiveSnapshotRuntimeState struct {
	edition            string
	resolvedPolicy     controlpolicy.Resolved
	hasPolicyOverlay   bool
	policyEngine       agent.PolicyEngine
	approvalRegistry   *controlapproval.ProviderRegistry
	governanceRegistry *controlgov.AdapterRegistry
	auditRegistry      *controlaudit.Registry
}

func newEffectiveSnapshotState(
	edition string,
	resolved controlpolicy.Resolved,
	hasPolicyOverlay bool,
	policyEngine agent.PolicyEngine,
	approvals *controlapproval.ProviderRegistry,
	governance *controlgov.AdapterRegistry,
	auditRegistry *controlaudit.Registry,
) *effectiveSnapshotState {
	state := &effectiveSnapshotState{}
	state.Update(edition, resolved, hasPolicyOverlay, policyEngine, approvals, governance, auditRegistry)
	return state
}

func newEffectiveSnapshotStateFromControlPlane(
	edition string,
	hasPolicyOverlay bool,
	state *controlPlaneRuntimeState,
) *effectiveSnapshotState {
	snapshot := &effectiveSnapshotState{}
	snapshot.UpdateFromControlPlane(edition, hasPolicyOverlay, state)
	return snapshot
}

func (s *effectiveSnapshotState) Update(
	edition string,
	resolved controlpolicy.Resolved,
	hasPolicyOverlay bool,
	policyEngine agent.PolicyEngine,
	approvals *controlapproval.ProviderRegistry,
	governance *controlgov.AdapterRegistry,
	auditRegistry *controlaudit.Registry,
) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.edition = edition
	s.resolvedPolicy = resolved
	s.hasPolicyOverlay = hasPolicyOverlay
	s.policyEngine = policyEngine
	s.approvalRegistry = approvals
	s.governanceRegistry = governance
	s.auditRegistry = auditRegistry
}

func (s *effectiveSnapshotState) UpdateFromControlPlane(
	edition string,
	hasPolicyOverlay bool,
	state *controlPlaneRuntimeState,
) {
	if s == nil {
		return
	}
	if state == nil {
		s.Update(edition, controlpolicy.Resolved{}, hasPolicyOverlay, nil, nil, nil, nil)
		return
	}
	s.Update(
		edition,
		state.resolvedPolicy,
		hasPolicyOverlay,
		state.policyEngine,
		state.approvalRegistry,
		state.governanceRegistry,
		state.auditRegistry,
	)
}

func (s *effectiveSnapshotState) current() effectiveSnapshotRuntimeState {
	if s == nil {
		return effectiveSnapshotRuntimeState{}
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return effectiveSnapshotRuntimeState{
		edition:            s.edition,
		resolvedPolicy:     s.resolvedPolicy,
		hasPolicyOverlay:   s.hasPolicyOverlay,
		policyEngine:       s.policyEngine,
		approvalRegistry:   s.approvalRegistry,
		governanceRegistry: s.governanceRegistry,
		auditRegistry:      s.auditRegistry,
	}
}

func (s *effectiveSnapshotState) Build(cfg config.Config, layers []controlsnapshot.Layer) *controlsnapshot.EffectiveConfigSnapshot {
	if s == nil {
		return controlsnapshot.Build(cfg, controlsnapshot.BuildOptions{Layers: layers})
	}
	current := s.current()

	policyCfg := current.resolvedPolicy.Config
	approvalDefaults := policyCfg.ApprovalDefaults()
	return controlsnapshot.Build(cfg, controlsnapshot.BuildOptions{
		Edition:                current.edition,
		PolicyProfileID:        current.resolvedPolicy.ProfileID,
		PolicyPackIDs:          current.resolvedPolicy.PackIDs(),
		GovernanceAdapterNames: current.governanceRegistry.EnabledAdapterNames(),
		Approval: &controlsnapshot.ApprovalPolicy{
			ExecMode:                       cfg.Tools.Capabilities.Exec.Mode,
			SkillInstallPolicy:             cfg.Skills.InstallPolicy,
			DangerousToolCount:             len(cfg.Security.DangerousTools),
			RequireApprovalForWrite:        policyCfg.RequireApprovalForWrite,
			AllowLocalWriteWithoutApproval: policyCfg.AllowLocalWriteWithoutApproval,
			RequireApprovalCommunity:       policyCfg.RequireApprovalCommunity,
			DenyDestructive:                policyCfg.DenyDestructive,
			DefaultGrantScope:              string(approvalDefaults.DefaultScope),
			MaxGrantScope:                  string(approvalDefaults.MaxScope),
			HasPolicyOverlay:               current.hasPolicyOverlay,
			ExternalProviderNames:          current.approvalRegistry.EnabledProviderNames(),
			CallbackProviderNames:          current.approvalRegistry.CallbackProtectedProviderNames(),
		},
		Layers: layers,
	})
}

type dynamicApprovalDispatcher struct {
	mu    sync.RWMutex
	inner *controlapproval.Dispatcher
}

func (d *dynamicApprovalDispatcher) Swap(inner *controlapproval.Dispatcher) {
	if d == nil {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	d.inner = inner
}

func (d *dynamicApprovalDispatcher) Handle(ctx context.Context, event eventbus.Event) error {
	if inner := d.current(); inner != nil {
		return inner.Handle(ctx, event)
	}
	return nil
}

func (d *dynamicApprovalDispatcher) SyncPendingApprovals(ctx context.Context) error {
	if inner := d.current(); inner != nil {
		return inner.SyncPendingApprovals(ctx)
	}
	return runtimesvc.ErrApprovalSyncerNil
}

func (d *dynamicApprovalDispatcher) current() *controlapproval.Dispatcher {
	if d == nil {
		return nil
	}
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.inner
}

type dynamicGovernanceDispatcher struct {
	mu    sync.RWMutex
	inner *controlgov.ReliableDispatcher
}

func (d *dynamicGovernanceDispatcher) Swap(ctx context.Context, inner *controlgov.ReliableDispatcher) {
	if d == nil {
		if inner != nil {
			inner.Start(context.Background())
		}
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if inner != nil {
		inner.Start(ctx)
	}
	old := d.replace(inner)
	if old != nil && old != inner {
		old.Stop()
	}
}

func (d *dynamicGovernanceDispatcher) Stop() {
	if d == nil {
		return
	}
	d.mu.Lock()
	old := d.inner
	d.inner = nil
	d.mu.Unlock()
	if old != nil {
		old.Stop()
	}
}

func (d *dynamicGovernanceDispatcher) Handle(ctx context.Context, event eventbus.Event) error {
	if inner := d.current(); inner != nil {
		return inner.Handle(ctx, event)
	}
	return nil
}

func (d *dynamicGovernanceDispatcher) GetDelivery(ctx context.Context, id string) (*controlplane.GovernanceDeliveryEntry, error) {
	if inner := d.current(); inner != nil {
		return controlgov.AdaptDeliveryController(inner).GetDelivery(ctx, id)
	}
	return nil, runtimesvc.ErrGovernanceDeliveryControllerNil
}

func (d *dynamicGovernanceDispatcher) ListDeliveries(ctx context.Context, filter controlplane.GovernanceDeliveryListFilter) ([]*controlplane.GovernanceDeliveryEntry, error) {
	if inner := d.current(); inner != nil {
		return controlgov.AdaptDeliveryController(inner).ListDeliveries(ctx, filter)
	}
	return nil, runtimesvc.ErrGovernanceDeliveryControllerNil
}

func (d *dynamicGovernanceDispatcher) Redrive(ctx context.Context, ids []string, opts controlplane.GovernanceDeliveryRedriveOptions) ([]*controlplane.GovernanceDeliveryEntry, error) {
	if inner := d.current(); inner != nil {
		return controlgov.AdaptDeliveryController(inner).Redrive(ctx, ids, opts)
	}
	return nil, runtimesvc.ErrGovernanceDeliveryControllerNil
}

func (d *dynamicGovernanceDispatcher) current() *controlgov.ReliableDispatcher {
	if d == nil {
		return nil
	}
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.inner
}

func (d *dynamicGovernanceDispatcher) replace(inner *controlgov.ReliableDispatcher) *controlgov.ReliableDispatcher {
	if d == nil {
		return nil
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	old := d.inner
	d.inner = inner
	return old
}

type dynamicAuditSink struct {
	mu sync.RWMutex

	inner eventbus.Sink
}

func newDynamicAuditSink(inner eventbus.Sink) *dynamicAuditSink {
	return &dynamicAuditSink{inner: inner}
}

func (d *dynamicAuditSink) Swap(ctx context.Context, inner eventbus.Sink) {
	if ctx == nil {
		ctx = context.Background()
	}
	if d == nil {
		if starter, ok := inner.(interface{ Start(context.Context) }); ok && inner != nil {
			starter.Start(ctx)
		}
		return
	}
	if starter, ok := inner.(interface{ Start(context.Context) }); ok && inner != nil {
		starter.Start(ctx)
	}
	old := d.replace(inner)
	if stopper, ok := old.(interface{ Stop() }); ok && old != nil && old != inner {
		stopper.Stop()
	}
}

func (d *dynamicAuditSink) Handle(ctx context.Context, event eventbus.Event) error {
	if !eventbus.PersistDefaultRuntimeEvent(event) {
		return nil
	}
	inner := d.current()
	if inner == nil {
		return nil
	}
	return inner.Handle(ctx, event)
}

func (d *dynamicAuditSink) current() eventbus.Sink {
	if d == nil {
		return nil
	}
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.inner
}

func (d *dynamicAuditSink) replace(inner eventbus.Sink) eventbus.Sink {
	if d == nil {
		return nil
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	old := d.inner
	d.inner = inner
	return old
}

func (d *dynamicAuditSink) GetDelivery(ctx context.Context, id string) (*audit.DeliveryEntry, error) {
	if controller, ok := d.deliveryController(); ok {
		return controller.GetDelivery(ctx, id)
	}
	return nil, audit.ErrDeliveryNotFound
}

func (d *dynamicAuditSink) ListDeliveries(ctx context.Context, filter audit.DeliveryListFilter) ([]*audit.DeliveryEntry, error) {
	if controller, ok := d.deliveryController(); ok {
		return controller.ListDeliveries(ctx, filter)
	}
	return nil, nil
}

func (d *dynamicAuditSink) GetDeliveryStats(ctx context.Context, filter audit.DeliveryListFilter) (audit.DeliveryStats, error) {
	if controller, ok := d.deliveryController(); ok {
		return controller.GetDeliveryStats(ctx, filter)
	}
	return audit.DeliveryStats{}, nil
}

func (d *dynamicAuditSink) Redrive(ctx context.Context, ids []string, opts audit.DeliveryRedriveOptions) ([]*audit.DeliveryEntry, error) {
	if controller, ok := d.deliveryController(); ok {
		return controller.Redrive(ctx, ids, opts)
	}
	return nil, nil
}

func (d *dynamicAuditSink) deliveryController() (audit.DeliveryController, bool) {
	current := d.current()
	controller, ok := current.(audit.DeliveryController)
	return controller, ok
}

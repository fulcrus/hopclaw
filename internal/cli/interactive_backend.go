package cli

import (
	"context"
	"fmt"

	"github.com/fulcrus/hopclaw/acp"
	"github.com/fulcrus/hopclaw/agent"
	"github.com/fulcrus/hopclaw/bootstrap"
	replpkg "github.com/fulcrus/hopclaw/internal/cli/repl"
)

type interactiveBackend struct {
	gateway              acp.GatewayClient
	closeFn              func(context.Context) error
	commandsFn           func(context.Context) ([]acp.Command, error)
	modelsFn             func(context.Context) ([]replpkg.ModelInfo, error)
	listSessionsFn       func(context.Context) ([]replpkg.SessionSummary, error)
	getSessionFn         func(context.Context, string) (*replpkg.SessionDetail, error)
	listApprovalsFn      func(context.Context, string, int) ([]replpkg.ApprovalSummary, error)
	resolveApprovalFn    func(context.Context, string, bool) (*replpkg.ApprovalSummary, error)
	qualityFn            func(context.Context) (*replpkg.QualitySnapshot, error)
	listEvalSuitesFn     func(context.Context) ([]replpkg.EvalSuiteSummary, error)
	runEvalSuiteFn       func(context.Context, string) (*replpkg.EvalRunSummary, error)
	listRunsFn           func(context.Context, string, int) ([]replpkg.RunSummary, error)
	getRunDetailFn       func(context.Context, string) (*replpkg.RunDetail, error)
	doctorChecksFn       func(context.Context) ([]replpkg.DoctorCheck, error)
	listToolsFn          func(context.Context, string) ([]replpkg.ToolSummary, error)
	listSkillsFn         func(context.Context) ([]replpkg.SkillSummary, error)
	searchSkillCatalogFn func(context.Context, string) ([]replpkg.SkillCatalogSummary, error)
	getSkillFn           func(context.Context, string) (*replpkg.SkillDetail, error)
	installSkillFn       func(context.Context, string, string) (*replpkg.SkillInstallResult, error)
	removeSkillFn        func(context.Context, string) error
	supervisorFn         func(context.Context) (*replpkg.SupervisorSnapshot, error)
	runDeliveryFn        func(context.Context, string) (*replpkg.RunDeliveryDetail, error)
	readinessFn          func(context.Context) (*replpkg.ReadinessSnapshot, error)
	recoveryFn           func(context.Context) ([]replpkg.RecoveryCandidate, error)
	listAutomationsFn    func(context.Context, int) ([]replpkg.AutomationItem, error)
	createAutomationFn   func(context.Context, replpkg.AutomationCreateRequest) (*replpkg.AutomationItem, error)
	pauseAutomationFn    func(context.Context, string, string) error
	resumeAutomationFn   func(context.Context, string, string) error
	runAutomationNowFn   func(context.Context, string, string) error
	listMemoryFn         func(context.Context, string, int) ([]agent.MemoryEntry, error)
	getMemoryFn          func(context.Context, string) (*agent.MemoryEntry, error)
	saveMemoryFn         func(context.Context, string, string, string, string, string) (*agent.MemoryEntry, error)
	deleteMemoryFn       func(context.Context, string) error
	recallMemoriesFn     func(context.Context, string, string) ([]agent.MemoryEntry, error)
	memoryUsageFn        func(context.Context, string) ([]replpkg.MemoryUsageItem, error)
	contextPressureFn    func(context.Context, string) (*replpkg.ContextPressureInfo, error)
	findProjectFn        func(context.Context, string) (*agent.Project, error)
	startEpisodeFn       func(context.Context, string) error
	resetSessionFn       func(context.Context, string) error
	compactSessionFn     func(context.Context, string) error
	resolvePermissionFn  func(context.Context, acp.PermissionRequest, replpkg.PermissionDecision) error
}

func (b *interactiveBackend) Commands(ctx context.Context) ([]acp.Command, error) {
	return b.commandsFn(ctx)
}

func (b *interactiveBackend) Models(ctx context.Context) ([]replpkg.ModelInfo, error) {
	return b.modelsFn(ctx)
}

func (b *interactiveBackend) ListSessions(ctx context.Context) ([]replpkg.SessionSummary, error) {
	return b.listSessionsFn(ctx)
}

func (b *interactiveBackend) GetSession(ctx context.Context, id string) (*replpkg.SessionDetail, error) {
	return b.getSessionFn(ctx, id)
}

func (b *interactiveBackend) ListApprovals(ctx context.Context, status string, limit int) ([]replpkg.ApprovalSummary, error) {
	return b.listApprovalsFn(ctx, status, limit)
}

func (b *interactiveBackend) ResolveApproval(ctx context.Context, id string, approved bool) (*replpkg.ApprovalSummary, error) {
	return b.resolveApprovalFn(ctx, id, approved)
}

func (b *interactiveBackend) QualitySnapshot(ctx context.Context) (*replpkg.QualitySnapshot, error) {
	return b.qualityFn(ctx)
}

func (b *interactiveBackend) ListEvalSuites(ctx context.Context) ([]replpkg.EvalSuiteSummary, error) {
	return b.listEvalSuitesFn(ctx)
}

func (b *interactiveBackend) RunEvalSuite(ctx context.Context, suiteID string) (*replpkg.EvalRunSummary, error) {
	return b.runEvalSuiteFn(ctx, suiteID)
}

func (b *interactiveBackend) ListRuns(ctx context.Context, sessionID string, limit int) ([]replpkg.RunSummary, error) {
	return b.listRunsFn(ctx, sessionID, limit)
}

func (b *interactiveBackend) GetRunDetail(ctx context.Context, id string) (*replpkg.RunDetail, error) {
	return b.getRunDetailFn(ctx, id)
}

func (b *interactiveBackend) DoctorChecks(ctx context.Context) ([]replpkg.DoctorCheck, error) {
	return b.doctorChecksFn(ctx)
}

func (b *interactiveBackend) ListTools(ctx context.Context, sessionKey string) ([]replpkg.ToolSummary, error) {
	if b.listToolsFn == nil {
		return nil, nil
	}
	return b.listToolsFn(ctx, sessionKey)
}

func (b *interactiveBackend) ListSkills(ctx context.Context) ([]replpkg.SkillSummary, error) {
	if b.listSkillsFn == nil {
		return nil, nil
	}
	return b.listSkillsFn(ctx)
}

func (b *interactiveBackend) SearchSkillCatalog(ctx context.Context, query string) ([]replpkg.SkillCatalogSummary, error) {
	if b.searchSkillCatalogFn == nil {
		return nil, nil
	}
	return b.searchSkillCatalogFn(ctx, query)
}

func (b *interactiveBackend) GetSkill(ctx context.Context, name string) (*replpkg.SkillDetail, error) {
	if b.getSkillFn == nil {
		return nil, nil
	}
	return b.getSkillFn(ctx, name)
}

func (b *interactiveBackend) InstallSkill(ctx context.Context, source, version string) (*replpkg.SkillInstallResult, error) {
	if b.installSkillFn == nil {
		return nil, nil
	}
	return b.installSkillFn(ctx, source, version)
}

func (b *interactiveBackend) RemoveSkill(ctx context.Context, name string) error {
	if b.removeSkillFn == nil {
		return nil
	}
	return b.removeSkillFn(ctx, name)
}

func (b *interactiveBackend) SupervisorSnapshot(ctx context.Context) (*replpkg.SupervisorSnapshot, error) {
	if b.supervisorFn == nil {
		return nil, nil
	}
	return b.supervisorFn(ctx)
}

func (b *interactiveBackend) GetRunDelivery(ctx context.Context, runID string) (*replpkg.RunDeliveryDetail, error) {
	if b.runDeliveryFn == nil {
		return nil, nil
	}
	return b.runDeliveryFn(ctx, runID)
}

func (b *interactiveBackend) ListGovernanceDeliveries(ctx context.Context, query string, limit int) ([]replpkg.DeliveryListItem, error) {
	type deliveryService interface {
		ListGovernanceDeliveries(context.Context, string, int) ([]replpkg.DeliveryListItem, error)
	}
	if gateway, ok := b.gateway.(deliveryService); ok {
		return gateway.ListGovernanceDeliveries(ctx, query, limit)
	}
	return nil, nil
}

func (b *interactiveBackend) RedriveDelivery(ctx context.Context, id string) (*replpkg.RedriveResult, error) {
	type deliveryService interface {
		RedriveDelivery(context.Context, string) (*replpkg.RedriveResult, error)
	}
	if gateway, ok := b.gateway.(deliveryService); ok {
		return gateway.RedriveDelivery(ctx, id)
	}
	return nil, nil
}

func (b *interactiveBackend) GetAutomationDetail(ctx context.Context, kind, id string) (*replpkg.AutomationItem, error) {
	type automationDetailService interface {
		GetAutomationDetail(context.Context, string, string) (*replpkg.AutomationItem, error)
	}
	if gateway, ok := b.gateway.(automationDetailService); ok {
		return gateway.GetAutomationDetail(ctx, kind, id)
	}
	return nil, nil
}

func (b *interactiveBackend) ListMemoryConflicts(ctx context.Context) ([]agent.MemoryEntry, error) {
	type memoryConflictService interface {
		ListMemoryConflicts(context.Context) ([]agent.MemoryEntry, error)
	}
	if gateway, ok := b.gateway.(memoryConflictService); ok {
		return gateway.ListMemoryConflicts(ctx)
	}
	return nil, nil
}

func (b *interactiveBackend) ListPendingMemoryWrites(ctx context.Context) ([]agent.MemoryEntry, error) {
	type pendingMemoryService interface {
		ListPendingMemoryWrites(context.Context) ([]agent.MemoryEntry, error)
	}
	if gateway, ok := b.gateway.(pendingMemoryService); ok {
		return gateway.ListPendingMemoryWrites(ctx)
	}
	return nil, nil
}

func (b *interactiveBackend) ResolveMemoryConflict(ctx context.Context, key, action string) error {
	type memoryConflictResolver interface {
		ResolveMemoryConflict(context.Context, string, string) error
	}
	if gateway, ok := b.gateway.(memoryConflictResolver); ok {
		return gateway.ResolveMemoryConflict(ctx, key, action)
	}
	return fmt.Errorf("memory conflict resolution is not supported on this runtime")
}

func (b *interactiveBackend) ReadinessSnapshot(ctx context.Context) (*replpkg.ReadinessSnapshot, error) {
	if b.readinessFn == nil {
		return nil, nil
	}
	return b.readinessFn(ctx)
}

func (b *interactiveBackend) RecoveryCandidates(ctx context.Context) ([]replpkg.RecoveryCandidate, error) {
	if b.recoveryFn == nil {
		return nil, nil
	}
	return b.recoveryFn(ctx)
}

func (b *interactiveBackend) ListAutomations(ctx context.Context, limit int) ([]replpkg.AutomationItem, error) {
	if b.listAutomationsFn == nil {
		return nil, nil
	}
	return b.listAutomationsFn(ctx, limit)
}

func (b *interactiveBackend) CreateAutomation(ctx context.Context, req replpkg.AutomationCreateRequest) (*replpkg.AutomationItem, error) {
	if b.createAutomationFn == nil {
		return nil, nil
	}
	return b.createAutomationFn(ctx, req)
}

func (b *interactiveBackend) PauseAutomation(ctx context.Context, kind, id string) error {
	if b.pauseAutomationFn == nil {
		return nil
	}
	return b.pauseAutomationFn(ctx, kind, id)
}

func (b *interactiveBackend) ResumeAutomation(ctx context.Context, kind, id string) error {
	if b.resumeAutomationFn == nil {
		return nil
	}
	return b.resumeAutomationFn(ctx, kind, id)
}

func (b *interactiveBackend) RunAutomationNow(ctx context.Context, kind, id string) error {
	if b.runAutomationNowFn == nil {
		return nil
	}
	return b.runAutomationNowFn(ctx, kind, id)
}

func (b *interactiveBackend) ListMemory(ctx context.Context, query string, limit int) ([]agent.MemoryEntry, error) {
	if b.listMemoryFn == nil {
		return nil, nil
	}
	return b.listMemoryFn(ctx, query, limit)
}

func (b *interactiveBackend) GetMemory(ctx context.Context, key string) (*agent.MemoryEntry, error) {
	if b.getMemoryFn == nil {
		return nil, nil
	}
	return b.getMemoryFn(ctx, key)
}

func (b *interactiveBackend) SaveMemory(ctx context.Context, key, value, label, sessionKey, projectID string) (*agent.MemoryEntry, error) {
	if b.saveMemoryFn == nil {
		return nil, nil
	}
	return b.saveMemoryFn(ctx, key, value, label, sessionKey, projectID)
}

func (b *interactiveBackend) DeleteMemory(ctx context.Context, key string) error {
	if b.deleteMemoryFn == nil {
		return nil
	}
	return b.deleteMemoryFn(ctx, key)
}

func (b *interactiveBackend) RecallMemories(ctx context.Context, sessionKey, projectID string) ([]agent.MemoryEntry, error) {
	if b.recallMemoriesFn == nil {
		return nil, nil
	}
	return b.recallMemoriesFn(ctx, sessionKey, projectID)
}

func (b *interactiveBackend) MemoryUsedInContext(ctx context.Context, sessionID string) ([]replpkg.MemoryUsageItem, error) {
	if b.memoryUsageFn == nil {
		return nil, nil
	}
	return b.memoryUsageFn(ctx, sessionID)
}

func (b *interactiveBackend) ContextPressure(ctx context.Context, sessionID string) (*replpkg.ContextPressureInfo, error) {
	if b.contextPressureFn == nil {
		return nil, nil
	}
	return b.contextPressureFn(ctx, sessionID)
}

func (b *interactiveBackend) FindOrCreateProject(ctx context.Context, directory string) (*agent.Project, error) {
	if b.findProjectFn == nil {
		return nil, nil
	}
	return b.findProjectFn(ctx, directory)
}

func (b *interactiveBackend) StartNewEpisode(ctx context.Context, sessionID string) error {
	return b.startEpisodeFn(ctx, sessionID)
}

func (b *interactiveBackend) ResetSession(ctx context.Context, sessionKey string) error {
	return b.resetSessionFn(ctx, sessionKey)
}

func (b *interactiveBackend) CompactSession(ctx context.Context, sessionID string) error {
	return b.compactSessionFn(ctx, sessionID)
}

func (b *interactiveBackend) ResolvePermission(ctx context.Context, req acp.PermissionRequest, decision replpkg.PermissionDecision) error {
	return b.resolvePermissionFn(ctx, req, decision)
}

func (b *interactiveBackend) Close(ctx context.Context) error {
	if b.closeFn == nil {
		return nil
	}
	return b.closeFn(ctx)
}

type externalInteractiveGateway struct {
	client *GatewayClient
	target interactiveTarget
}

type embeddedInteractiveGateway struct {
	app    *bootstrap.App
	target interactiveTarget
}

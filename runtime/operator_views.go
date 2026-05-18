package runtime

import (
	"context"
	"strings"
	"time"

	"github.com/fulcrus/hopclaw/agent"
	"github.com/fulcrus/hopclaw/controlplane"
	"github.com/fulcrus/hopclaw/eventbus"
	domaingov "github.com/fulcrus/hopclaw/internal/domain/governance"
	"github.com/fulcrus/hopclaw/internal/support/normalize"
	"github.com/fulcrus/hopclaw/logging"
	"github.com/fulcrus/hopclaw/planner"
)

type RunListViewOptions struct {
	IncludeVerification   bool
	IncludeExecutionGraph bool
}

type RunListView struct {
	ID                  string                             `json:"id"`
	SessionID           string                             `json:"session_id"`
	Scope               controlplane.ScopeRef              `json:"scope,omitempty"`
	ParentRunID         string                             `json:"parent_run_id,omitempty"`
	InputEventID        string                             `json:"input_event_id,omitempty"`
	Status              agent.RunStatus                    `json:"status"`
	QueueMode           agent.QueueMode                    `json:"queue_mode"`
	Phase               agent.RunPhase                     `json:"phase"`
	ExecutionMode       agent.ExecutionMode                `json:"execution_mode,omitempty"`
	Model               string                             `json:"model"`
	EffectiveProfile    *agent.EffectiveAgentProfile       `json:"effective_agent_profile,omitempty"`
	LastSessionRevision int64                              `json:"last_session_revision,omitempty"`
	ToolRounds          int                                `json:"tool_rounds"`
	ToolRecoveryCount   int                                `json:"tool_recovery_count,omitempty"`
	SemanticSignal      *agent.SemanticSignal              `json:"semantic_signal,omitempty"`
	Triage              *agent.RunTriageTrace              `json:"triage,omitempty"`
	TaskContract        *agent.TaskContract                `json:"task_contract,omitempty"`
	Delegation          *agent.DelegationContract          `json:"delegation,omitempty"`
	Governance          *GovernanceReceipt                 `json:"governance,omitempty"`
	GovernanceTrace     *controlplane.GovernanceEvaluation `json:"governance_trace,omitempty"`
	ApprovalID          string                             `json:"approval_id,omitempty"`
	PendingTools        []agent.ToolCall                   `json:"pending_tools,omitempty"`
	Preflight           *agent.RunPreflightReport          `json:"preflight,omitempty"`
	Error               string                             `json:"error,omitempty"`
	Plan                *planner.Plan                      `json:"plan,omitempty"`
	ExecutionGraph      *agent.ExecutionGraph              `json:"execution_graph,omitempty"`
	WorkflowState       *agent.WorkflowState               `json:"workflow_state,omitempty"`
	CreatedAt           time.Time                          `json:"created_at"`
	StartedAt           time.Time                          `json:"started_at"`
	UpdatedAt           time.Time                          `json:"updated_at"`
	FinishedAt          time.Time                          `json:"finished_at,omitempty"`
	Harness             *agent.RunHarnessSummary           `json:"harness,omitempty"`
	Outcome             string                             `json:"outcome,omitempty"`
	VerificationStatus  string                             `json:"verification_status,omitempty"`
	VerificationSummary string                             `json:"verification_summary,omitempty"`
}

type EventView struct {
	eventbus.Event
	Governance *GovernanceReceipt `json:"governance,omitempty"`
	Severity   string             `json:"severity,omitempty"`
	Summary    string             `json:"summary,omitempty"`
}

func (s *Service) ListRunViews(ctx context.Context, filter agent.RunListFilter, options RunListViewOptions) ([]*RunListView, error) {
	runs, err := s.ListRuns(ctx, filter)
	if err != nil {
		return nil, err
	}
	return s.BuildRunViews(ctx, runs, options), nil
}

func (s *Service) BuildRunViews(ctx context.Context, runs []*agent.Run, options RunListViewOptions) []*RunListView {
	if len(runs) == 0 {
		return nil
	}
	items := make([]*RunListView, 0, len(runs))
	for _, run := range runs {
		if run == nil {
			continue
		}
		items = append(items, s.buildRunView(ctx, run, options))
	}
	if len(items) == 0 {
		return nil
	}
	return items
}

func (s *Service) buildRunView(ctx context.Context, run *agent.Run, options RunListViewOptions) *RunListView {
	if run == nil {
		return nil
	}
	view := &RunListView{
		ID:                  strings.TrimSpace(run.ID),
		SessionID:           strings.TrimSpace(run.SessionID),
		Scope:               publicScopeRef(run.Scope),
		ParentRunID:         strings.TrimSpace(run.ParentRunID),
		InputEventID:        strings.TrimSpace(run.InputEventID),
		Status:              run.Status,
		QueueMode:           run.QueueMode,
		Phase:               run.Phase,
		ExecutionMode:       run.ExecutionMode,
		Model:               strings.TrimSpace(run.Model),
		EffectiveProfile:    run.EffectiveProfile,
		LastSessionRevision: run.LastSessionRevision,
		ToolRounds:          run.ToolRounds,
		ToolRecoveryCount:   run.ToolRecoveryCount,
		SemanticSignal:      agent.CloneSemanticSignal(run.SemanticSignal),
		Triage:              run.Triage,
		TaskContract:        cloneRunTaskContract(run.TaskContract),
		Delegation:          cloneRunDelegationContract(run.Delegation),
		Governance:          fallbackGovernanceReceipt(run),
		GovernanceTrace:     publicGovernanceEvaluation(run.Governance),
		ApprovalID:          strings.TrimSpace(run.ApprovalID),
		PendingTools:        clonePendingTools(run.PendingTools),
		Preflight:           run.Preflight,
		Error:               strings.TrimSpace(run.Error),
		Plan:                run.Plan,
		CreatedAt:           runCreatedAt(run),
		StartedAt:           run.StartedAt,
		UpdatedAt:           run.UpdatedAt,
		FinishedAt:          run.FinishedAt,
		Harness:             s.BuildRunHarnessSummary(ctx, run),
		Outcome:             string(DeriveRunOutcome(run, nil, nil)),
	}
	if options.IncludeExecutionGraph {
		view.ExecutionGraph = run.ExecutionGraph
	}
	view.WorkflowState = run.WorkflowState

	if receipt, err := s.buildGovernanceReceipt(ctx, run); err == nil {
		view.Governance = receipt
	} else {
		logging.FromContext(ctx).Warn("build run governance receipt failed", "run_id", run.ID, "error", err)
	}

	if !options.IncludeVerification {
		return view
	}

	result, err := s.GetRunResult(ctx, run.ID)
	if err != nil {
		logging.FromContext(ctx).Warn("load run result for run list failed", "run_id", run.ID, "error", err)
		return view
	}
	if result != nil {
		view.Outcome = string(result.Outcome)
		view.VerificationStatus = strings.TrimSpace(result.VerificationStatus)
		view.VerificationSummary = strings.TrimSpace(result.VerificationSummary)
		if result.Governance != nil {
			view.Governance = cloneGovernanceReceipt(result.Governance)
		}
	}
	return view
}

func ProjectEventViews(events []eventbus.Event) []EventView {
	if len(events) == 0 {
		return nil
	}
	items := make([]EventView, 0, len(events))
	for _, event := range events {
		items = append(items, buildEventView(event))
	}
	return items
}

func buildEventView(event eventbus.Event) EventView {
	view := EventView{
		Event:    event,
		Severity: operatorEventSeverity(event),
	}
	if receipt := governanceReceiptFromEvent(event); receipt != nil {
		view.Governance = receipt
	}
	view.Summary = eventSummary(event, view.Governance)
	return view
}

func operatorEventSeverity(event eventbus.Event) string {
	if payload, ok := event.SecurityFindingPayload(); ok {
		return strings.TrimSpace(payload.Severity)
	}
	if payload, ok := event.SecurityRiskDetectedPayload(); ok {
		return strings.TrimSpace(payload.Severity)
	}
	return ""
}

func governanceReceiptFromEvent(event eventbus.Event) *GovernanceReceipt {
	context := domaingov.EventContextFromEvent(event.Type, event.Attrs)
	if context.Empty() {
		return nil
	}
	receipt := GovernanceReceipt{
		Scope:                     publicScopeRef(context.Scope),
		EffectiveConfigSnapshotID: context.EffectiveConfigSnapshotID,
		Policy:                    publicGovernanceDecision(context.Policy),
		Approval:                  publicGovernanceApproval(context.Approval),
		ToolNames:                 normalize.DedupeStrings(context.ToolNames),
		Summary:                   context.Summary,
	}
	if receipt.empty() {
		return nil
	}
	return receipt.normalizedPtr()
}

func eventSummary(event eventbus.Event, receipt *GovernanceReceipt) string {
	return domaingov.EventSummary(event, governanceEventContextFromReceipt(receipt))
}

func governanceEventContextFromReceipt(receipt *GovernanceReceipt) domaingov.EventContext {
	if receipt == nil {
		return domaingov.EventContext{}
	}
	return domaingov.EventContext{
		Scope:                     internalScopeRef(receipt.Scope),
		EffectiveConfigSnapshotID: receipt.EffectiveConfigSnapshotID,
		Policy:                    internalGovernanceDecision(receipt.Policy),
		Approval:                  internalGovernanceApproval(receipt.Approval),
		ToolNames:                 normalize.DedupeStrings(receipt.ToolNames),
		Summary:                   receipt.Summary,
	}.Normalized()
}

func fallbackGovernanceReceipt(run *agent.Run) *GovernanceReceipt {
	if run == nil {
		return nil
	}
	receipt := mergeApprovalGovernance(GovernanceReceipt{}, run)
	if receipt.empty() {
		return nil
	}
	return receipt.normalizedPtr()
}

func clonePendingTools(in []agent.ToolCall) []agent.ToolCall {
	if len(in) == 0 {
		return nil
	}
	out := make([]agent.ToolCall, len(in))
	for i, call := range in {
		out[i] = call
		if call.Input != nil {
			out[i].Input = cloneMetadata(call.Input)
		}
	}
	return out
}

func runCreatedAt(run *agent.Run) time.Time {
	if run == nil {
		return time.Time{}
	}
	if !run.StartedAt.IsZero() {
		return run.StartedAt
	}
	if !run.UpdatedAt.IsZero() {
		return run.UpdatedAt
	}
	return run.FinishedAt
}

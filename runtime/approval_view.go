package runtime

import (
	"context"
	"strings"

	"github.com/fulcrus/hopclaw/agent"
	"github.com/fulcrus/hopclaw/approval"
	"github.com/fulcrus/hopclaw/controlplane"
	domaingov "github.com/fulcrus/hopclaw/internal/domain/governance"
	"github.com/fulcrus/hopclaw/internal/support/normalize"
)

type GovernanceReceipt struct {
	Scope                     controlplane.ScopeRef            `json:"scope,omitempty"`
	EffectiveConfigSnapshotID string                           `json:"effective_config_snapshot_id,omitempty"`
	Policy                    *controlplane.GovernanceDecision `json:"policy,omitempty"`
	Approval                  *GovernanceApproval              `json:"approval,omitempty"`
	ToolNames                 []string                         `json:"tool_names,omitempty"`
	Summary                   string                           `json:"summary,omitempty"`
}

type ApprovalView struct {
	approval.Ticket
	Governance           *GovernanceReceipt `json:"governance,omitempty"`
	ResourceScopeSummary string             `json:"resource_scope_summary,omitempty"`
}

func (s *Service) ListApprovalViews(ctx context.Context, status approval.Status) ([]*ApprovalView, error) {
	return s.ListApprovalViewsFiltered(ctx, approval.ListFilter{Status: status}, agent.ScopeFilter{})
}

func (s *Service) ListApprovalViewsFiltered(ctx context.Context, filter approval.ListFilter, scope agent.ScopeFilter) ([]*ApprovalView, error) {
	tickets, err := s.ListApprovalsFiltered(ctx, filter, scope)
	if err != nil {
		return nil, err
	}
	if len(tickets) == 0 {
		return nil, nil
	}
	out := make([]*ApprovalView, 0, len(tickets))
	for _, ticket := range tickets {
		view, err := s.approvalView(ctx, ticket)
		if err != nil {
			return nil, err
		}
		out = append(out, view)
	}
	return out, nil
}

func (s *Service) GetApprovalView(ctx context.Context, id string) (*ApprovalView, error) {
	return s.GetApprovalViewScoped(ctx, id, agent.ScopeFilter{})
}

func (s *Service) BuildApprovalView(ctx context.Context, ticket *approval.Ticket) (*ApprovalView, error) {
	return s.approvalView(ctx, ticket)
}

func (s *Service) GetApprovalViewScoped(ctx context.Context, id string, scope agent.ScopeFilter) (*ApprovalView, error) {
	ticket, err := s.GetApprovalScoped(ctx, id, scope)
	if err != nil {
		return nil, err
	}
	return s.approvalView(ctx, ticket)
}

func (s *Service) ResolveApprovalView(ctx context.Context, id string, resolution approval.Resolution) (*ApprovalView, error) {
	return s.ResolveApprovalViewScoped(ctx, id, agent.ScopeFilter{}, resolution)
}

func (s *Service) ResolveApprovalViewScoped(ctx context.Context, id string, scope agent.ScopeFilter, resolution approval.Resolution) (*ApprovalView, error) {
	if _, err := s.GetApprovalScoped(ctx, id, scope); err != nil {
		return nil, err
	}
	ticket, err := s.ResolveApproval(ctx, id, resolution)
	if err != nil {
		return nil, err
	}
	return s.approvalView(ctx, ticket)
}

func (s *Service) approvalView(ctx context.Context, ticket *approval.Ticket) (*ApprovalView, error) {
	if ticket == nil {
		return nil, nil
	}
	view := &ApprovalView{Ticket: *ticket}
	view.ResourceScopeSummary = approvalResourceScopeSummary(ticket)
	governance := approvalGovernanceFromTicket(ticket)
	if s != nil && s.runs != nil && strings.TrimSpace(ticket.RunID) != "" {
		if run, err := s.runs.Get(ctx, ticket.RunID); err == nil && run != nil {
			governance = mergeApprovalGovernance(governance, run)
		}
	}
	if governance.empty() {
		return view, nil
	}
	view.Governance = governance.normalizedPtr()
	return view, nil
}

func approvalResourceScopeSummary(ticket *approval.Ticket) string {
	if ticket == nil || len(ticket.ToolCalls) == 0 {
		return ""
	}
	parts := make([]string, 0, len(ticket.ToolCalls))
	multiTool := len(ticket.ToolCalls) > 1
	for _, call := range ticket.ToolCalls {
		scope := call.ResourceScope.Normalized()
		if scope.Empty() {
			scope = approval.ResourceScopeFromToolCall(call.Name, call.Input)
		}
		if scope.Empty() || strings.TrimSpace(scope.Summary) == "" {
			continue
		}
		summary := strings.TrimSpace(scope.Summary)
		if multiTool {
			summary = strings.TrimSpace(call.Name) + ": " + summary
		}
		parts = append(parts, summary)
	}
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, " || ")
}

func approvalGovernanceFromTicket(ticket *approval.Ticket) GovernanceReceipt {
	var governance GovernanceReceipt
	if ticket == nil {
		return governance
	}
	governance.Policy = governanceDecisionFromApproval(ticket)
	governance.ToolNames = approvalToolNames(ticket)
	governance.Approval = publicGovernanceApproval(domaingov.ApprovalFromTicket(ticket))
	if ticket.Metadata != nil {
		governance.Scope = governanceScopeFromValue(ticket.Metadata["scope"])
		governance.EffectiveConfigSnapshotID = strings.TrimSpace(normalize.String(ticket.Metadata["effective_config_snapshot_id"]))
	}
	return governance.normalized()
}

func mergeApprovalGovernance(governance GovernanceReceipt, run *agent.Run) GovernanceReceipt {
	if run == nil {
		return governance.normalized()
	}
	if governance.Scope.IsZero() {
		governance.Scope = publicScopeRef(run.Scope)
	}
	if evaluation := run.Governance; evaluation != nil {
		if governance.Policy == nil {
			decision := publicGovernanceDecision(&evaluation.Decision)
			if decision != nil && !decision.Empty() {
				governance.Policy = decision
			}
		}
		if governance.EffectiveConfigSnapshotID == "" {
			governance.EffectiveConfigSnapshotID = strings.TrimSpace(evaluation.EffectiveConfigSnapshotID)
		}
		if len(governance.ToolNames) == 0 {
			governance.ToolNames = normalize.DedupeStrings(append([]string(nil), evaluation.ToolNames...))
		}
	}
	return governance.normalized()
}

func (v GovernanceReceipt) normalized() GovernanceReceipt {
	out := v
	out.Scope = out.Scope.Normalize()
	out.EffectiveConfigSnapshotID = strings.TrimSpace(out.EffectiveConfigSnapshotID)
	if out.Policy != nil {
		decision := out.Policy.Normalized()
		out.Policy = &decision
	}
	out.Approval = cloneGovernanceApproval(out.Approval)
	out.ToolNames = normalize.DedupeStrings(out.ToolNames)
	out.Summary = strings.TrimSpace(out.Summary)
	if out.Summary == "" {
		out.Summary = governanceReceiptSummary(out)
	}
	return out
}

func (v GovernanceReceipt) normalizedPtr() *GovernanceReceipt {
	out := v.normalized()
	return &out
}

func (v GovernanceReceipt) empty() bool {
	return v.Scope.IsZero() &&
		strings.TrimSpace(v.EffectiveConfigSnapshotID) == "" &&
		v.Policy == nil &&
		v.Approval == nil &&
		len(v.ToolNames) == 0 &&
		strings.TrimSpace(v.Summary) == ""
}

func (s *Service) buildGovernanceReceipt(ctx context.Context, run *agent.Run) (*GovernanceReceipt, error) {
	if run == nil {
		return nil, nil
	}
	receipt := mergeApprovalGovernance(GovernanceReceipt{}, run)
	if s != nil && s.approvals != nil && strings.TrimSpace(run.ID) != "" {
		if ticket, err := s.approvals.GetByRun(ctx, run.ID); err == nil && ticket != nil {
			receipt = mergeApprovalGovernance(approvalGovernanceFromTicket(ticket), run)
		}
	}
	if receipt.empty() {
		return nil, nil
	}
	return receipt.normalizedPtr(), nil
}

func cloneGovernanceReceipt(in *GovernanceReceipt) *GovernanceReceipt {
	if in == nil {
		return nil
	}
	out := in.normalized()
	return &out
}

func governanceReceiptSummary(receipt GovernanceReceipt) string {
	parts := make([]string, 0, 4)
	if receipt.Policy != nil {
		if summary := strings.TrimSpace(receipt.Policy.Summary); summary != "" {
			parts = append(parts, summary)
		} else if action := strings.TrimSpace(string(receipt.Policy.Action)); action != "" {
			parts = append(parts, "policy="+action)
		}
	}
	if receipt.Approval != nil {
		if status := strings.TrimSpace(string(receipt.Approval.Status)); status != "" {
			parts = append(parts, "approval="+status)
		}
		if providers := approvalExternalProviders(receipt.Approval.External); len(providers) > 0 {
			parts = append(parts, "providers="+strings.Join(providers, ","))
		}
	}
	if summary := controlplane.ScopeSummary(receipt.Scope); summary != "" {
		parts = append(parts, summary)
	}
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, " | ")
}

func (s *Service) approvalMatchesScope(ctx context.Context, ticket *approval.Ticket, scope agent.ScopeFilter) (bool, error) {
	if ticket == nil {
		return false, nil
	}
	if scope.IsZero() {
		return true, nil
	}
	if ticketScope := governanceScopeFromValue(ticket.Metadata["scope"]); !ticketScope.IsZero() {
		return scope.Matches(internalScopeRef(ticketScope)), nil
	}
	if runID := strings.TrimSpace(ticket.RunID); runID != "" {
		run, err := s.GetRunScoped(ctx, runID, scope)
		if err == nil && run != nil {
			return true, nil
		}
		if sessionID := strings.TrimSpace(ticket.SessionID); sessionID == "" {
			return false, nil
		}
	}
	if sessionID := strings.TrimSpace(ticket.SessionID); sessionID != "" {
		session, err := s.getSessionMetadataScoped(ctx, sessionID, scope)
		if err != nil || session == nil {
			return false, nil
		}
		return true, nil
	}
	return false, nil
}

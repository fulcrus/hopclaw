package runtime

import (
	"context"
	"fmt"
	"strings"

	"github.com/fulcrus/hopclaw/agent"
	"github.com/fulcrus/hopclaw/approval"
	"github.com/fulcrus/hopclaw/contextengine"
	"github.com/fulcrus/hopclaw/controlplane"
	"github.com/fulcrus/hopclaw/eventbus"
	domaingov "github.com/fulcrus/hopclaw/internal/domain/governance"
	"github.com/fulcrus/hopclaw/internal/support/normalize"
)

type RunReplay struct {
	Run          *agent.Run              `json:"run,omitempty"`
	Result       *RunResult              `json:"result,omitempty"`
	Verification any                     `json:"verification,omitempty"`
	Messages     []contextengine.Message `json:"messages,omitempty"`
	Events       []eventbus.Event        `json:"events,omitempty"`
	EventLedger  *EventLedger            `json:"event_ledger,omitempty"`
}

type GovernanceSnapshot struct {
	RunID                     string                           `json:"run_id"`
	Scope                     controlplane.ScopeRef            `json:"scope,omitempty"`
	EffectiveConfigSnapshotID string                           `json:"effective_config_snapshot_id,omitempty"`
	Policy                    *controlplane.GovernanceDecision `json:"policy,omitempty"`
	PolicyToolNames           []string                         `json:"policy_tool_names,omitempty"`
	Approval                  *GovernanceApproval              `json:"approval,omitempty"`
	Outcome                   string                           `json:"outcome,omitempty"`
	RequiredIssues            int                              `json:"required_issues,omitempty"`
	AdvisoryIssues            int                              `json:"advisory_issues,omitempty"`
	DeliveryAttachmentCount   int                              `json:"delivery_attachment_count,omitempty"`
	ThreadContextPresent      bool                             `json:"thread_context_present"`
	RecoveredToolFailures     int                              `json:"recovered_tool_failures,omitempty"`
}

type GovernanceApproval = controlplane.GovernanceApproval

func (s *Service) GetRunReplay(ctx context.Context, id string) (*RunReplay, error) {
	state, err := s.buildRunCompletionState(ctx, id)
	if err != nil {
		return nil, err
	}
	var messages []contextengine.Message
	if state.session != nil {
		messages = append([]contextengine.Message(nil), state.session.Messages...)
	}
	events := filterRunEvents(s.EventSnapshotContext(ctx), id)
	return &RunReplay{
		Run:          state.run,
		Result:       state.result,
		Verification: state.verification,
		Messages:     messages,
		Events:       events,
		EventLedger:  buildRunEventLedger(state.run, events),
	}, nil
}

func (s *Service) GetGovernanceSnapshot(ctx context.Context, id string) (*GovernanceSnapshot, error) {
	state, err := s.buildRunCompletionState(ctx, id)
	if err != nil {
		return nil, err
	}
	snapshot := &GovernanceSnapshot{RunID: id}
	if state.run != nil {
		snapshot.Scope = publicScopeRef(state.run.Scope)
		if state.run.Governance != nil {
			if decision := publicGovernanceDecision(&state.run.Governance.Decision); decision != nil && !decision.Empty() {
				snapshot.Policy = decision
			}
			snapshot.PolicyToolNames = normalize.DedupeStrings(append([]string(nil), state.run.Governance.ToolNames...))
			snapshot.EffectiveConfigSnapshotID = strings.TrimSpace(state.run.Governance.EffectiveConfigSnapshotID)
		}
	}
	if state.result != nil {
		snapshot.Outcome = strings.TrimSpace(string(state.result.Outcome))
		if state.result.Delivery != nil {
			snapshot.DeliveryAttachmentCount = len(state.result.Delivery.Attachments)
			snapshot.ThreadContextPresent = state.result.Delivery.Conversation != nil
		}
	}
	if state.verification != nil {
		snapshot.RequiredIssues = state.verification.RequiredWarnings + state.verification.RequiredFailures
		snapshot.AdvisoryIssues = state.verification.AdvisoryWarnings + state.verification.AdvisoryFailures
	}
	for _, msg := range state.sessionMessages() {
		if msg.Role != contextengine.RoleTool {
			continue
		}
		if strings.Contains(msg.Content, `"tool_execution_error":true`) {
			snapshot.RecoveredToolFailures++
		}
	}
	if s.approvals != nil {
		if ticket, err := s.approvals.GetByRun(ctx, id); err == nil && ticket != nil {
			snapshot.Approval = &GovernanceApproval{
				ID:       strings.TrimSpace(ticket.ID),
				Kind:     ticket.Kind,
				Status:   ticket.Status,
				External: approval.CloneExternalReferences(ticket.External),
			}
			if snapshot.Policy == nil {
				snapshot.Policy = governanceDecisionFromApproval(ticket)
			}
			if snapshot.Scope.IsZero() {
				snapshot.Scope = governanceScopeFromValue(ticket.Metadata["scope"])
			}
			if snapshot.EffectiveConfigSnapshotID == "" {
				snapshot.EffectiveConfigSnapshotID = strings.TrimSpace(normalize.String(ticket.Metadata["effective_config_snapshot_id"]))
			}
			if len(snapshot.PolicyToolNames) == 0 {
				snapshot.PolicyToolNames = approvalToolNames(ticket)
			}
		}
	}
	return snapshot, nil
}

func (s *runCompletionState) sessionMessages() []contextengine.Message {
	if s == nil || s.session == nil || len(s.session.Messages) == 0 {
		return nil
	}
	return s.session.Messages
}

func filterRunEvents(events []eventbus.Event, runID string) []eventbus.Event {
	if len(events) == 0 || strings.TrimSpace(runID) == "" {
		return nil
	}
	out := make([]eventbus.Event, 0, 16)
	for _, event := range events {
		if event.RunID != runID {
			continue
		}
		out = append(out, event)
	}
	return out
}

func (s GovernanceSnapshot) Summary() string {
	policyAction := ""
	if s.Policy != nil {
		policyAction = string(s.Policy.Action)
	}
	approvalStatus := ""
	if s.Approval != nil {
		approvalStatus = string(s.Approval.Status)
	}
	return fmt.Sprintf("outcome=%s policy=%s approval=%s required=%d advisory=%d attachments=%d thread=%t recovered_tool_failures=%d",
		s.Outcome, policyAction, approvalStatus, s.RequiredIssues, s.AdvisoryIssues, s.DeliveryAttachmentCount, s.ThreadContextPresent, s.RecoveredToolFailures)
}

func governanceDecisionFromApproval(ticket *approval.Ticket) *controlplane.GovernanceDecision {
	if ticket == nil || ticket.Metadata == nil {
		return nil
	}
	return publicGovernanceDecision(domaingov.DecisionFromMetadata(ticket.Metadata))
}

func approvalToolNames(ticket *approval.Ticket) []string {
	if ticket == nil {
		return nil
	}
	names := make([]string, 0, len(ticket.ToolCalls))
	for _, call := range ticket.ToolCalls {
		names = append(names, strings.TrimSpace(call.Name))
	}
	if items := normalize.DedupeStrings(names); len(items) > 0 {
		return items
	}
	if ticket.Metadata != nil {
		return normalize.DedupeStrings(stringSliceValue(ticket.Metadata["tool_names"]))
	}
	return nil
}

func cloneGovernanceApproval(in *GovernanceApproval) *GovernanceApproval {
	return controlplane.CloneGovernanceApproval(in)
}

func approvalExternalProviders(items []approval.ExternalReference) []string {
	if len(items) == 0 {
		return nil
	}
	providers := make([]string, 0, len(items))
	for _, item := range items {
		if provider := strings.TrimSpace(item.Provider); provider != "" {
			providers = append(providers, provider)
		}
	}
	return normalize.DedupeStrings(providers)
}

func governanceScopeFromValue(value any) controlplane.ScopeRef {
	return controlplane.ScopeRefFromValue(value)
}

func stringSliceValue(value any) []string {
	return normalize.StringSlice(value)
}

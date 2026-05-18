package agent

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/fulcrus/hopclaw/approval"
	"github.com/fulcrus/hopclaw/eventbus"
	"github.com/fulcrus/hopclaw/hooks"
	domaingov "github.com/fulcrus/hopclaw/internal/domain/governance"
	"github.com/fulcrus/hopclaw/internal/support/normalize"
	"github.com/fulcrus/hopclaw/logging"
	"github.com/fulcrus/hopclaw/policy"
)

func (a *AgentComponent) ResolveApproval(ctx context.Context, ticketID string, resolution approval.Resolution) (*approval.Ticket, error) {
	if a.approvals == nil {
		return nil, ErrApprovalStoreNil
	}
	current, err := a.approvals.Get(ctx, ticketID)
	if err != nil {
		return nil, err
	}
	resolution, err = approval.NormalizeResolution(current, resolution)
	if err != nil {
		return nil, err
	}
	run, err := a.runs.Get(ctx, current.RunID)
	if err != nil {
		return nil, err
	}
	if run.Status == RunCancelled {
		cancelled, cancelErr := a.cancelApproval(ctx, current.ID, "run cancelled by operator")
		if cancelErr != nil && !isAlreadyResolvedErr(cancelErr) {
			return nil, cancelErr
		}
		if cancelled != nil {
			return cancelled, fmt.Errorf("%w: %s", ErrRunCancelled, run.ID)
		}
		return current, fmt.Errorf("%w: %s", ErrRunCancelled, run.ID)
	}
	ticket, err := a.approvals.Resolve(ctx, ticketID, resolution)
	if err != nil {
		return nil, err
	}
	logging.LogIfErr(ctx, a.emit(ctx, eventbus.NewApprovalResolvedEvent(
		ticket.RunID,
		ticket.SessionID,
		eventbus.ApprovalEventAttrs{
			ApprovalID:   ticket.ID,
			ApprovalKind: string(ticket.Kind),
			Status:       string(ticket.Status),
			ResolvedBy:   ticket.ResolvedBy,
			ExternalRefs: eventbus.ApprovalExternalRefAttrsFromTicketRefs(ticket.External),
		},
		buildGovernanceEventAttrs(run),
	)), "emit event failed", slog.String("kind", string(eventbus.EventApprovalResolved)))

	run, err = a.runs.Get(ctx, ticket.RunID)
	if err != nil {
		return ticket, err
	}
	if run.Status == RunCancelled {
		return ticket, fmt.Errorf("%w: %s", ErrRunCancelled, run.ID)
	}
	switch ticket.Status {
	case approval.StatusApproved:
		a.applyApprovalGrant(ticket)
		transitionRun(run, RunQueued, PhasePreparing, withRunError(""))
		if err := a.runs.Update(ctx, run); err != nil {
			return ticket, err
		}
		return ticket, nil
	case approval.StatusDenied, approval.StatusCancelled:
		errText := "approval " + string(ticket.Status)
		if err := a.finalizeRun(ctx, run, runFinalization{
			status:      RunCancelled,
			errorText:   errText,
			eventType:   eventbus.EventRunCancelled,
			eventAttrs:  eventbus.RunControlAttrs{ApprovalID: ticket.ID, Status: string(ticket.Status)}.ToMap(),
			planAction:  runFinalPlanCancel,
			planReason:  errText,
			hookPhase:   hooks.HookPhaseError,
			hookSummary: errText,
			hookErr:     errors.New(errText),
		}); err != nil {
			return ticket, err
		}
		return ticket, nil
	default:
		return ticket, nil
	}
}

// CancelRun cancels a run that is queued, running, or waiting approval.
func (a *AgentComponent) CancelRun(ctx context.Context, runID string) (*Run, error) {
	run, err := a.runs.Get(ctx, runID)
	if err != nil {
		return nil, err
	}
	switch run.Status {
	case RunCompleted, RunFailed, RunCancelled:
		return nil, fmt.Errorf("run %s is already %s", runID, run.Status)
	}
	if _, err := a.cancelApproval(ctx, run.ApprovalID, "run cancelled by operator"); err != nil && !isAlreadyResolvedErr(err) {
		return nil, err
	}
	if err := a.finalizeRun(ctx, run, runFinalization{
		status:        RunCancelled,
		errorText:     "cancelled by operator",
		eventType:     eventbus.EventRunCancelled,
		eventAttrs:    eventbus.RunControlAttrs{Reason: "operator_cancel"}.ToMap(),
		planAction:    runFinalPlanCancel,
		planReason:    "cancelled by operator",
		cancelContext: true,
		finishQueue:   true,
		hookPhase:     hooks.HookPhaseError,
		hookSummary:   "cancelled by operator",
		hookErr:       errors.New("cancelled by operator"),
	}); err != nil {
		return nil, err
	}
	return run, nil
}

func (a *AgentComponent) waitApproval(ctx context.Context, run *Run, session *Session, calls []ToolCall, decision policy.Decision) error {
	if a.approvals == nil {
		return ErrApprovalStoreNil
	}
	decision = decision.Normalized()
	run.Governance = buildGovernanceEvaluation(run, calls, decision)
	approvalKind, approvalMetadata := buildApprovalDetails(run, session, calls, decision)
	reasons := approvalReasons(decision)
	ticket, err := a.approvals.Create(ctx, approval.Ticket{
		RunID:     run.ID,
		SessionID: run.SessionID,
		Kind:      approvalKind,
		ToolCalls: toApprovalCalls(calls),
		Reasons:   reasons,
		Metadata:  approvalMetadata,
	})
	if err != nil {
		return err
	}

	transitionRun(run, RunWaitingApproval, PhaseWaitingApproval,
		withRunApproval(ticket.ID),
		withRunPendingTools(calls),
		withRunError(strings.Join(reasons, "; ")),
	)
	if err := a.runs.Update(ctx, run); err != nil {
		return err
	}
	logging.LogIfErr(ctx, a.emit(ctx, eventbus.NewApprovalRequestedEvent(
		run.ID,
		session.ID,
		eventbus.ApprovalEventAttrs{
			ApprovalID:    ticket.ID,
			ApprovalKind:  string(ticket.Kind),
			Status:        string(ticket.Status),
			ToolCount:     len(calls),
			PolicySource:  decision.PolicySource,
			PolicySummary: decision.Summary,
			ExternalRefs:  eventbus.ApprovalExternalRefAttrsFromTicketRefs(ticket.External),
		},
		buildGovernanceEventAttrs(run),
	)), "emit event failed", slog.String("kind", string(eventbus.EventApprovalRequested)))
	logging.LogIfErr(ctx, a.emit(ctx, eventbus.NewRunWaitingApprovalEvent(
		run.ID,
		session.ID,
		eventbus.ApprovalEventAttrs{
			ApprovalID:    ticket.ID,
			ApprovalKind:  string(ticket.Kind),
			Status:        string(ticket.Status),
			Reasons:       append([]string(nil), reasons...),
			PolicySource:  decision.PolicySource,
			PolicySummary: decision.Summary,
			ExternalRefs:  eventbus.ApprovalExternalRefAttrsFromTicketRefs(ticket.External),
		},
		buildGovernanceEventAttrs(run),
	)), "emit event failed", slog.String("kind", string(eventbus.EventRunWaitingApproval)))
	return nil
}

func (a *AgentComponent) applyApprovalGrant(ticket *approval.Ticket) {
	if a == nil || a.grantStore == nil || ticket == nil || ticket.Status != approval.StatusApproved {
		return
	}
	for _, call := range ticket.ToolCalls {
		scope := call.ResourceScope
		if scope.Empty() {
			scope = approval.ResourceScopeFromToolCall(call.Name, call.Input)
		}
		a.grantStore.GrantScoped(ticket.SessionID, call.Name, ticket.Scope, scope)
	}
}

func (a *AgentComponent) interruptSessionRuns(ctx context.Context, sessionID, keepRunID string) error {
	if a == nil || strings.TrimSpace(sessionID) == "" {
		return nil
	}
	lister, ok := a.runs.(RunLister)
	if !ok {
		return nil
	}
	runs, err := lister.List(ctx, RunListFilter{SessionID: sessionID, Limit: 64})
	if err != nil {
		return err
	}
	for _, item := range runs {
		if item == nil || item.ID == keepRunID {
			continue
		}
		switch item.Status {
		case RunCompleted, RunFailed, RunCancelled:
			continue
		}
		if _, err := a.CancelRun(ctx, item.ID); err != nil && !errors.Is(err, ErrRunCancelled) {
			return err
		}
	}
	return nil
}

func buildApprovalDetails(run *Run, session *Session, calls []ToolCall, decision policy.Decision) (approval.Kind, map[string]any) {
	harness := buildRunHarnessSpec(run, nil, lastUserMessageForRun(session, runIDValue(run)), nil)
	if len(calls) == 0 {
		return approval.KindToolCalls, approvalGovernanceMetadata(run, decision, nil, harness)
	}
	toolNames := make([]string, 0, len(calls))
	allSkillInstalls := true
	requestedSkills := make([]string, 0, len(calls))
	goals := make([]string, 0, len(calls))
	requiredTools := make([]string, 0, len(calls))
	for _, call := range calls {
		name := strings.TrimSpace(call.Name)
		if name == "" {
			continue
		}
		toolNames = append(toolNames, name)
		switch name {
		case "skill.install":
			if value, ok := call.Input["name"]; ok {
				requestedSkills = append(requestedSkills, strings.TrimSpace(fmt.Sprintf("%v", value)))
			}
		case "skill.ensure":
			if value, ok := call.Input["goal"]; ok {
				goals = append(goals, strings.TrimSpace(fmt.Sprintf("%v", value)))
			}
			switch items := call.Input["required_tools"].(type) {
			case []any:
				for _, item := range items {
					requiredTools = append(requiredTools, strings.TrimSpace(fmt.Sprintf("%v", item)))
				}
			case []string:
				requiredTools = append(requiredTools, items...)
			}
		default:
			allSkillInstalls = false
		}
		if name != "skill.install" && name != "skill.ensure" {
			allSkillInstalls = false
		}
	}
	if !allSkillInstalls {
		return approval.KindToolCalls, approvalGovernanceMetadata(run, decision, map[string]any{
			"tool_names": normalize.DedupeStrings(toolNames),
		}, harness)
	}
	metadata := approvalGovernanceMetadata(run, decision, map[string]any{
		"tool_names": normalize.DedupeStrings(toolNames),
	}, harness)
	if items := normalize.DedupeStrings(requestedSkills); len(items) > 0 {
		metadata["requested_skills"] = items
	}
	if items := normalize.DedupeStrings(goals); len(items) > 0 {
		metadata["goals"] = items
	}
	if items := normalize.DedupeStrings(requiredTools); len(items) > 0 {
		metadata["required_tools"] = items
	}
	return approval.KindSkillInstall, metadata
}

func (a *AgentComponent) recordGovernanceDecision(ctx context.Context, run *Run, calls []ToolCall, decision policy.Decision) error {
	if run == nil {
		return nil
	}
	run.Governance = buildGovernanceEvaluation(run, calls, decision)
	return a.runs.Update(ctx, run)
}

func buildGovernanceEvaluation(run *Run, calls []ToolCall, decision policy.Decision) *domaingov.Evaluation {
	evaluation := domaingov.Evaluation{
		Decision:  decision.Normalized(),
		ToolNames: toolCallNames(calls),
		UpdatedAt: time.Now().UTC(),
	}
	if run != nil && run.Governance != nil {
		evaluation.EffectiveConfigSnapshotID = strings.TrimSpace(run.Governance.EffectiveConfigSnapshotID)
	}
	normalized := evaluation.Normalized()
	return &normalized
}

func toolCallNames(calls []ToolCall) []string {
	names := make([]string, 0, len(calls))
	for _, call := range calls {
		names = append(names, strings.TrimSpace(call.Name))
	}
	return normalize.DedupeStrings(names)
}

func approvalReasons(decision policy.Decision) []string {
	if items := normalize.DedupeStrings(append([]string(nil), decision.Reasons...)); len(items) > 0 {
		return items
	}
	if summary := strings.TrimSpace(decision.Summary); summary != "" {
		return []string{summary}
	}
	return nil
}

func approvalGovernanceMetadata(run *Run, decision policy.Decision, metadata map[string]any, harness RunHarnessSpec) map[string]any {
	out := cloneMap(metadata)
	if out == nil {
		out = make(map[string]any, 10)
	}
	decision = decision.Normalized()
	out["policy_action"] = string(decision.Action)
	if decision.PolicySource != "" {
		out["policy_source"] = decision.PolicySource
	}
	if decision.Summary != "" {
		out["policy_summary"] = decision.Summary
	}
	if len(decision.Reasons) > 0 {
		out["policy_reasons"] = append([]string(nil), decision.Reasons...)
	}
	if len(decision.ReasonCodes) > 0 {
		out["policy_reason_codes"] = append([]string(nil), decision.ReasonCodes...)
	}
	if len(decision.PolicyLayers) > 0 {
		out["policy_layers"] = append([]string(nil), decision.PolicyLayers...)
	}
	if len(decision.AuditLabels) > 0 {
		out["policy_audit_labels"] = append([]string(nil), decision.AuditLabels...)
	}
	if decision.ApprovalPolicy != nil {
		if scope := strings.TrimSpace(string(decision.ApprovalPolicy.DefaultScope)); scope != "" {
			out["policy_approval_default_scope"] = scope
		}
		if scope := strings.TrimSpace(string(decision.ApprovalPolicy.MaxScope)); scope != "" {
			out["policy_approval_max_scope"] = scope
		}
	}
	if run != nil {
		out["scope"] = run.Scope
		if run.Governance != nil && strings.TrimSpace(run.Governance.EffectiveConfigSnapshotID) != "" {
			out["effective_config_snapshot_id"] = run.Governance.EffectiveConfigSnapshotID
		}
	}
	mergeHarnessMetadata(out, harness)
	return out
}

func mergeHarnessMetadata(metadata map[string]any, harness RunHarnessSpec) {
	if metadata == nil {
		return
	}
	if len(harness.Domains) > 0 {
		metadata["harness_domains"] = append([]string(nil), harness.Domains...)
	}
	if harness.Model.RequireThinking {
		metadata["harness_require_thinking"] = true
	}
	if harness.Approval.NeedsConfirmation {
		metadata["harness_needs_confirmation"] = true
	}
	if harness.Approval.RequiresApproval {
		metadata["harness_requires_approval"] = true
	}
	if harness.Approval.RequiresExternalSide {
		metadata["harness_requires_external_side_effect"] = true
	}
	if harness.Budget.ExtraToolRounds > 0 {
		metadata["harness_extra_tool_rounds"] = harness.Budget.ExtraToolRounds
	}
	if harness.Recovery.ExtraAttempts > 0 {
		metadata["harness_extra_recovery_attempts"] = harness.Recovery.ExtraAttempts
	}
	if harness.Recovery.TransparentIntent != nil {
		metadata["harness_transparent_recovery_intent"] = harness.Recovery.TransparentIntent.Key
	}
}

func runIDValue(run *Run) string {
	if run == nil {
		return ""
	}
	return run.ID
}

func (a *AgentComponent) cancelApproval(ctx context.Context, approvalID, note string) (*approval.Ticket, error) {
	if a.approvals == nil || strings.TrimSpace(approvalID) == "" {
		return nil, nil
	}
	ticket, err := a.approvals.Get(ctx, approvalID)
	if err != nil {
		return nil, err
	}
	if ticket.Status != approval.StatusPending {
		return ticket, nil
	}
	ticket, err = a.approvals.Resolve(ctx, approvalID, approval.Resolution{
		Status:     approval.StatusCancelled,
		ResolvedBy: "system",
		Note:       note,
	})
	if err != nil {
		return nil, err
	}
	logging.LogIfErr(ctx, a.emit(ctx, eventbus.NewApprovalResolvedEvent(
		ticket.RunID,
		ticket.SessionID,
		eventbus.ApprovalEventAttrs{
			ApprovalID:   ticket.ID,
			ApprovalKind: string(ticket.Kind),
			Status:       string(ticket.Status),
			ResolvedBy:   ticket.ResolvedBy,
			ExternalRefs: eventbus.ApprovalExternalRefAttrsFromTicketRefs(ticket.External),
		},
		domaingov.MetadataAttrs(ticket.Metadata),
	)), "emit event failed", slog.String("kind", string(eventbus.EventApprovalResolved)))
	return ticket, nil
}

func isAlreadyResolvedErr(err error) bool {
	return err != nil && errors.Is(err, approval.ErrAlreadyResolved)
}

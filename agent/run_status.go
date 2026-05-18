package agent

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/fulcrus/hopclaw/eventbus"
	"github.com/fulcrus/hopclaw/hooks"
	"github.com/fulcrus/hopclaw/logging"
	planpkg "github.com/fulcrus/hopclaw/planner"
)

func (a *AgentComponent) completeRun(ctx context.Context, run *Run, session *Session) error {
	run, cancelled, err := a.refreshRun(ctx, run)
	if err != nil {
		return err
	}
	if cancelled {
		return nil
	}
	if run.WorkflowState != nil && run.Plan != nil && planpkg.IsDone(run.Plan) {
		syncWorkflowCompletedTaskIDs(run)
		run.WorkflowState.Yielded = false
		run.WorkflowState.YieldReason = ""
		observeWorkflowContinuationOutcome(run)
		run.WorkflowState.MarkTerminal(WorkflowTerminalOutcomeCompleted, "")
		completedTasks, totalTasks := workflowTaskCounts(run)
		logging.LogIfErr(ctx, a.emit(ctx, eventbus.NewWorkflowCompletedEvent(
			run.ID,
			run.SessionID,
			eventbus.WorkflowEventAttrs{
				OriginalRunID:     run.WorkflowState.OriginalRunID,
				ContinuationIndex: run.WorkflowState.ContinuationIndex,
				TotalRoundsUsed:   run.WorkflowState.TotalRoundsUsed,
				CompletedTasks:    completedTasks,
				TotalTasks:        totalTasks,
				Summary:           runCompletionSummary(session, run.ID),
			},
			buildGovernanceEventAttrs(run),
		)), "emit event failed", slog.String("kind", string(eventbus.EventWorkflowCompleted)))
	}
	summary := runCompletionSummary(session, run.ID)
	return a.finalizeRun(ctx, run, runFinalization{
		status:      RunCompleted,
		eventType:   eventbus.EventRunCompleted,
		eventAttrs:  eventbus.RunStatusAttrs{Summary: summary}.ToMap(),
		planAction:  runFinalPlanCancel,
		planReason:  "run completed",
		hookPhase:   hooks.HookPhasePost,
		hookSummary: summary,
		session:     session,
	})
}

func preflightEventPayload(report *RunPreflightReport) eventbus.RunPreflightAttrs {
	if report == nil {
		return eventbus.RunPreflightAttrs{}
	}
	payload := eventbus.RunPreflightAttrs{
		State:            string(report.State),
		Summary:          report.Summary,
		Prompt:           report.Prompt,
		Question:         report.Question,
		ReplyTemplate:    report.ReplyTemplate,
		ContinueHint:     report.ContinueHint,
		Blocking:         report.Blocking,
		GeneratedAt:      report.GeneratedAt,
		ReplyHints:       append([]string(nil), report.ReplyHints...),
		SuggestedDomains: append([]string(nil), report.SuggestedDomains...),
		DetectedDomains:  append([]string(nil), report.DetectedDomains...),
	}
	if len(report.ClarificationSlots) > 0 {
		payload.ClarificationSlots = make([]eventbus.RunPreflightClarificationSlotAttrs, 0, len(report.ClarificationSlots))
		for _, slot := range report.ClarificationSlots {
			payload.ClarificationSlots = append(payload.ClarificationSlots, eventbus.RunPreflightClarificationSlotAttrs{
				ID:          slot.ID,
				Label:       slot.Label,
				Question:    slot.Question,
				InputMode:   slot.InputMode,
				Placeholder: slot.Placeholder,
				Required:    slot.Required,
				Hints:       append([]string(nil), slot.Hints...),
			})
		}
	}
	if len(report.Checks) > 0 {
		payload.Checks = make([]eventbus.RunPreflightCheckAttrs, 0, len(report.Checks))
		for _, check := range report.Checks {
			payload.Checks = append(payload.Checks, eventbus.RunPreflightCheckAttrs{
				ID:       check.ID,
				Title:    check.Title,
				State:    string(check.State),
				Detail:   check.Detail,
				Blocking: check.Blocking,
			})
		}
	}
	return payload
}

func preflightEventMap(report *RunPreflightReport) map[string]any {
	return preflightEventPayload(report).ToMap()
}

func (a *AgentComponent) markPreflightReady(ctx context.Context, run *Run) error {
	if run == nil || run.Preflight == nil || run.Preflight.State != RunPreflightAutoPreparing {
		return nil
	}
	updated := *run.Preflight
	updated.State = RunPreflightReady
	updated.Blocking = false
	updated.Summary = "Ready to execute."
	updated.GeneratedAt = time.Now().UTC()
	filtered := make([]RunPreflightCheck, 0, len(updated.Checks))
	for _, check := range updated.Checks {
		if check.State == RunPreflightAutoPreparing {
			check.State = RunPreflightReady
		}
		filtered = append(filtered, check)
	}
	updated.Checks = filtered
	run.Preflight = &updated
	if err := a.runs.Update(ctx, run); err != nil {
		return err
	}
	logging.LogIfErr(ctx, a.emit(ctx, eventbus.NewRunPreflightUpdatedEvent(
		run.ID,
		run.SessionID,
		preflightEventPayload(run.Preflight),
		nil,
	)), "emit event failed", slog.String("kind", string(eventbus.EventRunPreflightUpdated)))
	return nil
}

func (a *AgentComponent) failRun(ctx context.Context, run *Run, err error) error {
	run, cancelled, refreshErr := a.refreshRun(ctx, run)
	if refreshErr != nil {
		return refreshErr
	}
	if cancelled {
		return nil
	}
	summary := compactRunSummary(err.Error())
	if run.WorkflowState != nil {
		run.WorkflowState.MarkTerminal(WorkflowTerminalOutcomeFailed, err.Error())
		completedTasks, totalTasks := workflowTaskCounts(run)
		logging.LogIfErr(ctx, a.emit(ctx, eventbus.NewWorkflowFailedEvent(
			run.ID,
			run.SessionID,
			eventbus.WorkflowEventAttrs{
				OriginalRunID:     run.WorkflowState.OriginalRunID,
				ContinuationIndex: run.WorkflowState.ContinuationIndex,
				TotalRoundsUsed:   run.WorkflowState.TotalRoundsUsed,
				CompletedTasks:    completedTasks,
				TotalTasks:        totalTasks,
				YieldReason:       run.WorkflowState.YieldReason,
				Summary:           summary,
			},
			buildGovernanceEventAttrs(run),
		)), "emit event failed", slog.String("kind", string(eventbus.EventWorkflowFailed)))
	}
	if finalErr := a.finalizeRun(ctx, run, runFinalization{
		status:      RunFailed,
		errorText:   err.Error(),
		eventType:   eventbus.EventRunFailed,
		eventAttrs:  eventbus.RunStatusAttrs{Error: err.Error(), Summary: summary}.ToMap(),
		planAction:  runFinalPlanFail,
		planErr:     err,
		hookPhase:   hooks.HookPhaseError,
		hookSummary: summary,
		hookErr:     err,
	}); finalErr != nil {
		return finalErr
	}
	return err
}

func (a *AgentComponent) handleRunExecutionError(ctx context.Context, runRef **Run, err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return a.timeoutRun(ctx, runRef, err)
	}
	if runRef == nil || *runRef == nil {
		return err
	}
	return a.failRun(ctx, *runRef, err)
}

func (a *AgentComponent) timeoutRun(ctx context.Context, runRef **Run, err error) error {
	if runRef == nil || *runRef == nil {
		return nil
	}
	finalizeCtx := logging.WithSessionID(context.Background(), (*runRef).SessionID)
	finalizeCtx = logging.WithRunID(finalizeCtx, (*runRef).ID)
	if traceID := logging.TraceIDFromContext(ctx); traceID != "" {
		finalizeCtx = logging.WithTraceID(finalizeCtx, traceID)
	}

	refreshed, cancelled, refreshErr := a.refreshRun(finalizeCtx, *runRef)
	if refreshErr != nil {
		return refreshErr
	}
	*runRef = refreshed
	if cancelled {
		return nil
	}

	timeoutText := "run timed out"
	if a != nil && a.config.MaxRunDuration > 0 {
		timeoutText = fmt.Sprintf("run timed out after %s", a.config.MaxRunDuration)
	}
	if err := a.finalizeRun(finalizeCtx, refreshed, runFinalization{
		status:      RunCancelled,
		errorText:   timeoutText,
		eventType:   eventbus.EventRunCancelled,
		eventAttrs:  eventbus.RunControlAttrs{Reason: "run_timeout"}.ToMap(),
		planAction:  runFinalPlanCancel,
		planReason:  timeoutText,
		hookPhase:   hooks.HookPhaseError,
		hookSummary: timeoutText,
		hookErr:     context.DeadlineExceeded,
	}); err != nil {
		return err
	}

	channel := a.resolveRunChannel(finalizeCtx, refreshed, nil)
	payload := eventbus.RunTimeoutAttrs{
		Channel: channel,
		Reason:  "run_timeout",
		Error:   err.Error(),
	}
	if a != nil && a.config.MaxRunDuration > 0 {
		payload.MaxRunDuration = a.config.MaxRunDuration.String()
	}
	logging.LogIfErr(finalizeCtx, a.emit(finalizeCtx, eventbus.NewRunTimeoutEvent(
		refreshed.ID,
		refreshed.SessionID,
		payload,
		buildGovernanceEventAttrs(refreshed),
	)), "emit event failed", slog.String("kind", string(eventbus.EventRunTimeout)))
	return nil
}

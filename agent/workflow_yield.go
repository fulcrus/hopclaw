package agent

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/fulcrus/hopclaw/eventbus"
	"github.com/fulcrus/hopclaw/hooks"
	"github.com/fulcrus/hopclaw/logging"
	planpkg "github.com/fulcrus/hopclaw/planner"
)

func isWorkflowWithIncompletePlan(run *Run) bool {
	if run == nil || run.ExecutionMode != ExecutionModeWorkflow {
		return false
	}
	if run.Plan == nil {
		return false
	}
	return !planpkg.Terminal(run.Plan)
}

func (a *AgentComponent) yieldWorkflowRun(ctx context.Context, run *Run, session *Session, reason string) error {
	if a == nil || run == nil {
		return nil
	}
	refreshed, cancelled, err := a.refreshRun(ctx, run)
	if err != nil {
		return err
	}
	if cancelled {
		return nil
	}
	run = refreshed

	if run.WorkflowState == nil {
		run.WorkflowState = ensureWorkflowState(run)
	}
	run.WorkflowState.EnsureBudget(time.Now().UTC())
	run.WorkflowState.ClearTerminal()

	if run.WorkflowState.BudgetExhausted() {
		return a.failRun(ctx, run, fmt.Errorf(
			"workflow budget exhausted after %d continuations and %d total rounds",
			run.WorkflowState.ContinuationIndex,
			run.WorkflowState.TotalRoundsUsed,
		))
	}

	run.WorkflowState.Yielded = true
	run.WorkflowState.YieldReason = strings.TrimSpace(reason)

	syncWorkflowCompletedTaskIDs(run)
	observeWorkflowContinuationOutcome(run)

	summary := BuildWorkflowRunSummary(run)
	completedTasks, totalTasks := workflowTaskCounts(run)

	if err := a.finalizeRun(ctx, run, runFinalization{
		status:    RunCompleted,
		eventType: eventbus.EventRunCompleted,
		eventAttrs: mergeEventAttrs(
			eventbus.RunStatusAttrs{Summary: summary}.ToMap(),
			map[string]any{
				"workflow_yielded":  true,
				"workflow_reason":   run.WorkflowState.YieldReason,
				"workflow_index":    run.WorkflowState.ContinuationIndex,
				"workflow_original": run.WorkflowState.OriginalRunID,
			},
		),
		planAction:  runFinalPlanNone,
		finishQueue: true,
		hookPhase:   hooks.HookPhasePost,
		hookSummary: summary,
		session:     session,
	}); err != nil {
		return err
	}
	logging.LogIfErr(ctx, a.emit(ctx, eventbus.NewWorkflowYieldedEvent(
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
	)), "emit event failed")
	return nil
}

func BuildWorkflowRunSummary(run *Run) string {
	if run == nil || run.Plan == nil {
		return "workflow run yielded"
	}
	completed := planpkg.CompletedCount(run.Plan)
	total := planpkg.TotalCount(run.Plan)
	failed := planpkg.FailedCount(run.Plan)

	var builder strings.Builder
	fmt.Fprintf(&builder, "completed %d/%d tasks", completed, total)
	if failed > 0 {
		fmt.Fprintf(&builder, " (%d failed)", failed)
	}
	fmt.Fprintf(&builder, " in %d rounds", run.ToolRounds)
	if run.WorkflowState != nil {
		fmt.Fprintf(&builder, " (continuation %d)", run.WorkflowState.ContinuationIndex)
	}
	return builder.String()
}

func workflowTaskCounts(run *Run) (completed, total int) {
	if run == nil || run.Plan == nil {
		return 0, 0
	}
	return planpkg.CompletedCount(run.Plan), planpkg.TotalCount(run.Plan)
}

func syncWorkflowCompletedTaskIDs(run *Run) {
	if run == nil || run.WorkflowState == nil || run.Plan == nil {
		return
	}
	for i := range run.Plan.Tasks {
		if run.Plan.Tasks[i].Status == planpkg.TaskCompleted {
			run.WorkflowState.CompletedTaskIDs = appendUniqueString(run.WorkflowState.CompletedTaskIDs, run.Plan.Tasks[i].ID)
		}
	}
}

func ensureWorkflowState(run *Run) *WorkflowState {
	if run == nil {
		return nil
	}
	if run.WorkflowState != nil {
		return run.WorkflowState
	}
	run.WorkflowState = &WorkflowState{
		OriginalRunID:     run.ID,
		ContinuationIndex: 0,
		MaxContinuations:  DefaultMaxContinuations,
		MaxTotalRounds:    DefaultMaxTotalRounds,
	}
	run.WorkflowState.EnsureBudget(time.Now().UTC())
	return run.WorkflowState
}

func trackWorkflowExecutionRound(run *Run) {
	if run == nil || run.ExecutionMode != ExecutionModeWorkflow {
		return
	}
	ensureWorkflowState(run).TotalRoundsUsed++
}

func appendUniqueString(slice []string, item string) []string {
	for _, current := range slice {
		if current == item {
			return slice
		}
	}
	return append(slice, item)
}

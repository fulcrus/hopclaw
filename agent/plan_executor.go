package agent

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/fulcrus/hopclaw/contextengine"
	"github.com/fulcrus/hopclaw/eventbus"
	"github.com/fulcrus/hopclaw/logging"
	planpkg "github.com/fulcrus/hopclaw/planner"
)

// ---------------------------------------------------------------------------
// Plan task lifecycle
// ---------------------------------------------------------------------------

// failRunningPlanTasks marks ALL currently running plan tasks as failed and
// propagates failure to dependents. Replaces the legacy failActivePlanTask
// which only handled the single Active() task.
func (a *AgentComponent) failRunningPlanTasks(ctx context.Context, run *Run, taskErr error) error {
	if a == nil || run == nil || run.Plan == nil || taskErr == nil {
		return nil
	}
	planpkg.NormalizeExecution(run.Plan)
	before := planTaskStatuses(run.Plan)
	for _, task := range planpkg.Running(run.Plan) {
		planpkg.MarkFailed(run.Plan, task.ID, taskErr.Error())
		planpkg.SkipDependentsOf(run.Plan, task.ID)
		logging.LogIfErr(ctx, a.emit(ctx, eventbus.NewPlanTaskFailedEvent(
			run.ID,
			run.SessionID,
			a.planTaskPayload(run, task),
			nil,
		)), "emit event failed", slog.String("kind", string(eventbus.EventPlanTaskFailed)))
	}
	// Cancel remaining queued tasks.
	for i := range run.Plan.Tasks {
		t := &run.Plan.Tasks[i]
		if t.Status == planpkg.TaskQueued {
			planpkg.MarkSkipped(run.Plan, t.ID, "run failed: "+taskErr.Error())
		}
	}
	syncExecutionGraphWithPlan(run)
	a.emitSkippedPlanTasks(ctx, run, before)
	logging.LogIfErr(ctx, a.emitPlanSnapshot(ctx, run), "emit event failed", slog.String("kind", string(eventbus.EventPlanSnapshotUpdated)))
	if err := a.runs.Update(ctx, run); err != nil {
		return err
	}
	return nil
}

func (a *AgentComponent) cancelPlanTasks(ctx context.Context, run *Run, reason string) error {
	if a == nil || run == nil || run.Plan == nil {
		return nil
	}
	planpkg.NormalizeExecution(run.Plan)
	before := planTaskStatuses(run.Plan)
	running := planpkg.Running(run.Plan)
	incomplete := planpkg.Incomplete(run.Plan)
	if len(running) == 0 && len(incomplete) == 0 {
		return nil
	}
	planpkg.CancelRunning(run.Plan, reason)
	for _, task := range planpkg.Incomplete(run.Plan) {
		planpkg.MarkSkipped(run.Plan, task.ID, reason)
	}
	syncExecutionGraphWithPlan(run)
	if err := a.runs.Update(ctx, run); err != nil {
		return err
	}
	for _, task := range running {
		logging.LogIfErr(ctx, a.emit(ctx, eventbus.NewPlanTaskCancelledEvent(
			run.ID,
			run.SessionID,
			a.planTaskPayload(run, task),
			nil,
		)), "emit event failed", slog.String("kind", string(eventbus.EventPlanTaskCancelled)))
	}
	a.emitSkippedPlanTasks(ctx, run, before)
	logging.LogIfErr(ctx, a.emitPlanSnapshot(ctx, run), "emit event failed", slog.String("kind", string(eventbus.EventPlanSnapshotUpdated)))
	return nil
}

// ---------------------------------------------------------------------------
// Batch execution (parallel-aware)
// ---------------------------------------------------------------------------

type batchExecutionOutcome struct {
	results []TaskExecutionResult
	retry   bool
}

// executeReadyBatch dispatches ready tasks for execution.
//
// Serial (single task or StrategySerial): runs inline on the shared session
// with full persistence and policy evaluation.
//
// Parallel (multiple tasks, non-serial strategy): fans out goroutines with
// independent session snapshots and run copies. No persistence during
// execution. Messages are captured from each snapshot and merged back to the
// main session in task order after all goroutines complete.
func (a *AgentComponent) executeReadyBatch(
	ctx context.Context,
	run *Run,
	lease *sessionLease,
	tasks []*planpkg.Task,
	aggregator *TaskResultAggregator,
) (batchExecutionOutcome, error) {
	if len(tasks) == 0 {
		return batchExecutionOutcome{}, nil
	}

	// Mark all tasks as running and persist.
	ensureExecutionGraph(run)
	ids := make([]string, len(tasks))
	for i, t := range tasks {
		ids[i] = t.ID
	}
	planpkg.MarkRunning(run.Plan, ids...)
	markExecutionTasksRunning(run.ExecutionGraph, ids...)
	logging.LogIfErr(ctx, a.runs.Update(ctx, run),
		"update run state failed", slog.String("run_id", run.ID))

	for _, task := range tasks {
		aggregator.StartTask(task.ID)
		logging.LogIfErr(ctx, a.emitTaskStarted(ctx, run, task),
			"emit task started failed", slog.String("task_id", task.ID))
	}
	logging.LogIfErr(ctx, a.emitPlanSnapshot(ctx, run), "emit event failed", slog.String("kind", string(eventbus.EventPlanSnapshotUpdated)))

	// Serial: single task or serial strategy.
	if len(tasks) == 1 || run.Plan.Strategy == planpkg.StrategySerial {
		results := make([]TaskExecutionResult, 0, len(tasks))
		for _, task := range tasks {
			depResults := aggregator.DependencyResults(task.DependsOn)
			result := a.runSingleTaskSerial(ctx, run, lease, task, executionMetadataForTask(run, task), depResults)
			a.recordTaskResult(aggregator, result)
			results = append(results, result)
			if result.Status == planpkg.TaskFailed && run.Plan.FailurePolicy != planpkg.ContinueOnError {
				break
			}
		}
		if lease != nil && lease.session == nil {
			if err := lease.reload(ctx, a.sessions, run.SessionID); err != nil {
				return batchExecutionOutcome{}, err
			}
		}
		return batchExecutionOutcome{results: results}, nil
	}

	// Parallel: fan out with session snapshots and run copies.
	// Use a cancellable context for fail_fast: first failure cancels siblings.
	batchCtx, batchCancel := context.WithCancel(ctx)
	defer batchCancel()
	batchSession := cloneSession(lease.session)
	batchRevision := int64(0)
	if batchSession != nil {
		batchRevision = batchSession.Revision
	}
	sessionID := run.SessionID
	lease.release()
	lease.session = nil

	type indexedResult struct {
		index  int
		result TaskExecutionResult
	}
	ch := make(chan indexedResult, len(tasks))
	var wg sync.WaitGroup

	for i, task := range tasks {
		wg.Add(1)
		go func(idx int, t *planpkg.Task) {
			defer wg.Done()
			snap := cloneSession(batchSession)
			snapBaseLen := len(snap.Messages)
			runCopy := shallowCopyRun(run)
			depResults := aggregator.DependencyResults(t.DependsOn)
			result := a.runSingleTask(batchCtx, runCopy, snap, t, executionMetadataForTask(runCopy, t), depResults)
			// Capture messages produced during this task's execution.
			if snapBaseLen < len(snap.Messages) {
				result.Messages = append([]contextengine.Message(nil), snap.Messages[snapBaseLen:]...)
			}
			ch <- indexedResult{index: idx, result: result}
			// Fail-fast: cancel sibling goroutines on failure.
			if result.Status == planpkg.TaskFailed && run.Plan.FailurePolicy != planpkg.ContinueOnError {
				batchCancel()
			}
		}(i, task)
	}

	go func() {
		wg.Wait()
		close(ch)
	}()

	results := make([]TaskExecutionResult, len(tasks))
	for ir := range ch {
		results[ir.index] = ir.result
	}

	if err := lease.reload(ctx, a.sessions, sessionID); err != nil {
		return batchExecutionOutcome{}, err
	}
	if lease.session.Revision != batchRevision {
		requeueBatchTasksForRetry(run, ids)
		transitionRun(run, RunRunning, PhasePreparing,
			withRunError(""),
			withRunLastSessionRevision(lease.session.Revision),
		)
		if err := a.runs.Update(ctx, run); err != nil {
			return batchExecutionOutcome{}, err
		}
		return batchExecutionOutcome{retry: true}, nil
	}

	needsSerialRetry := false
	hasHardFailure := false
	for _, result := range results {
		if result.RequiresSerial {
			needsSerialRetry = true
			continue
		}
		if result.Status == planpkg.TaskFailed {
			hasHardFailure = true
		}
	}
	if needsSerialRetry && (!hasHardFailure || run.Plan.FailurePolicy == planpkg.ContinueOnError) {
		for idx, task := range tasks {
			if !results[idx].RequiresSerial {
				continue
			}
			depResults := aggregator.DependencyResults(task.DependsOn)
			results[idx] = a.runSingleTaskSerial(ctx, run, lease, task, executionMetadataForTask(run, task), depResults)
			if results[idx].Status == planpkg.TaskFailed && run.Plan.FailurePolicy != planpkg.ContinueOnError {
				hasHardFailure = true
			}
		}
	}
	for _, result := range results {
		a.recordTaskResult(aggregator, result)
	}
	if lease != nil && lease.session == nil {
		if err := lease.reload(ctx, a.sessions, run.SessionID); err != nil {
			return batchExecutionOutcome{}, err
		}
	}

	// Merge parallel task messages back to main session in task order.
	merged := false
	for _, result := range results {
		if result.RequiresSerial {
			continue
		}
		if len(result.Messages) == 0 {
			continue
		}
		for i := range result.Messages {
			if result.Messages[i].Metadata == nil {
				result.Messages[i].Metadata = make(map[string]any, 1)
			}
			result.Messages[i].Metadata["task_id"] = result.TaskID
		}
		lease.session.Messages = append(lease.session.Messages, result.Messages...)
		merged = true
	}
	if merged {
		lease.session.UpdatedAt = time.Now().UTC()
		if err := a.saveSession(ctx, run, lease.session); err != nil {
			return batchExecutionOutcome{}, err
		}
	}

	return batchExecutionOutcome{results: results}, nil
}

// applyBatchResults updates plan state based on execution results and returns
// whether the plan has reached a terminal state.
func (a *AgentComponent) applyBatchResults(
	ctx context.Context,
	run *Run,
	results []TaskExecutionResult,
) (terminal bool, err error) {
	if run == nil || run.Plan == nil {
		return true, nil
	}
	before := planTaskStatuses(run.Plan)
	ensureExecutionGraph(run)

	hadFailure := false
	for _, r := range results {
		switch r.Status {
		case planpkg.TaskCompleted:
			planpkg.MarkCompleted(run.Plan, r.TaskID, r.Summary)
			applyTaskResultToExecutionGraph(run.ExecutionGraph, r)
			logging.LogIfErr(ctx, a.emit(ctx, eventbus.NewPlanTaskCompletedEvent(
				run.ID,
				run.SessionID,
				eventbus.PlanTaskAttrs{
					TaskID:         r.TaskID,
					ResultSummary:  r.Summary,
					CompletedCount: planpkg.CompletedCount(run.Plan),
					TotalTasks:     planpkg.TotalCount(run.Plan),
					FinalTask:      run.Plan.FinalTask,
				},
				nil,
			)), "emit event failed", slog.String("kind", string(eventbus.EventPlanTaskCompleted)))
			logging.LogIfErr(ctx, a.emitTaskProgress(ctx, run, nil), "emit event failed", slog.String("kind", string(eventbus.EventTaskProgress)))
		case planpkg.TaskFailed:
			planpkg.MarkFailed(run.Plan, r.TaskID, r.Error)
			applyTaskResultToExecutionGraph(run.ExecutionGraph, r)
			hadFailure = true
			logging.LogIfErr(ctx, a.emit(ctx, eventbus.NewPlanTaskFailedEvent(
				run.ID,
				run.SessionID,
				eventbus.PlanTaskAttrs{
					TaskID:         r.TaskID,
					Error:          r.Error,
					CompletedCount: planpkg.CompletedCount(run.Plan),
					TotalTasks:     planpkg.TotalCount(run.Plan),
					FinalTask:      run.Plan.FinalTask,
				},
				nil,
			)), "emit event failed", slog.String("kind", string(eventbus.EventPlanTaskFailed)))
			logging.LogIfErr(ctx, a.emitTaskProgress(ctx, run, nil), "emit event failed", slog.String("kind", string(eventbus.EventTaskProgress)))
			planpkg.SkipDependentsOf(run.Plan, r.TaskID)
		case planpkg.TaskRunning:
			// Waiting approval — leave as-is for serial resume path.
		}
	}

	if hadFailure && run.Plan.FailurePolicy != planpkg.ContinueOnError {
		planpkg.CancelRunning(run.Plan, "fail_fast: upstream task failed")
		for i := range run.Plan.Tasks {
			t := &run.Plan.Tasks[i]
			if t.Status == planpkg.TaskQueued {
				planpkg.MarkSkipped(run.Plan, t.ID, "fail_fast: plan aborted")
			}
		}
	}
	syncExecutionGraphWithPlan(run)
	a.emitSkippedPlanTasks(ctx, run, before)
	logging.LogIfErr(ctx, a.emitPlanSnapshot(ctx, run), "emit event failed", slog.String("kind", string(eventbus.EventPlanSnapshotUpdated)))

	if err := a.runs.Update(ctx, run); err != nil {
		return false, err
	}
	return planpkg.Terminal(run.Plan), nil
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func (a *AgentComponent) emitTaskStarted(ctx context.Context, run *Run, task *planpkg.Task) error {
	if err := a.emit(ctx, eventbus.NewPlanTaskStartedEvent(
		run.ID,
		run.SessionID,
		a.planTaskPayload(run, task),
		nil,
	)); err != nil {
		return err
	}
	return a.emitTaskProgress(ctx, run, task)
}

func (a *AgentComponent) emitPlanSnapshot(ctx context.Context, run *Run) error {
	if run == nil || run.Plan == nil {
		return nil
	}
	return a.emit(ctx, eventbus.NewPlanSnapshotUpdatedEvent(
		run.ID,
		run.SessionID,
		buildPlanSnapshotPayload(snapshotPlanExecution(run.Plan)),
		nil,
	))
}

func planTaskStatuses(plan *planpkg.Plan) map[string]planpkg.TaskStatus {
	if plan == nil || len(plan.Tasks) == 0 {
		return nil
	}
	out := make(map[string]planpkg.TaskStatus, len(plan.Tasks))
	for _, task := range plan.Tasks {
		out[task.ID] = task.Status
	}
	return out
}

func (a *AgentComponent) emitSkippedPlanTasks(ctx context.Context, run *Run, before map[string]planpkg.TaskStatus) {
	if a == nil || run == nil || run.Plan == nil || len(before) == 0 {
		return
	}
	for i := range run.Plan.Tasks {
		task := &run.Plan.Tasks[i]
		if task.Status != planpkg.TaskSkipped {
			continue
		}
		if before[task.ID] == planpkg.TaskSkipped {
			continue
		}
		logging.LogIfErr(ctx, a.emit(ctx, eventbus.NewPlanTaskSkippedEvent(
			run.ID,
			run.SessionID,
			a.planTaskPayload(run, task),
			nil,
		)), "emit event failed", slog.String("kind", string(eventbus.EventPlanTaskSkipped)))
		logging.LogIfErr(ctx, a.emitTaskProgress(ctx, run, nil), "emit event failed", slog.String("kind", string(eventbus.EventTaskProgress)))
	}
}

func (a *AgentComponent) planTaskPayload(run *Run, task *planpkg.Task) eventbus.PlanTaskAttrs {
	return eventbus.PlanTaskAttrs{
		TaskID:         task.ID,
		Title:          planTaskLabel(task),
		Kind:           string(task.Kind),
		Goal:           task.Goal,
		ResultSummary:  task.ResultSummary,
		Error:          task.Error,
		CompletedCount: planpkg.CompletedCount(run.Plan),
		TotalTasks:     planpkg.TotalCount(run.Plan),
		FinalTask:      run.Plan.FinalTask,
	}
}

func (a *AgentComponent) recordTaskResult(aggregator *TaskResultAggregator, result TaskExecutionResult) {
	if result.Status == planpkg.TaskCompleted {
		aggregator.CompleteTask(result)
	} else {
		aggregator.FailTask(result)
	}
}

func requeueBatchTasksForRetry(run *Run, ids []string) {
	if run == nil || run.Plan == nil || len(ids) == 0 {
		return
	}
	requeueExecutionTasksForRetry(run.ExecutionGraph, ids...)
	idSet := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		idSet[id] = struct{}{}
	}
	for i := range run.Plan.Tasks {
		task := &run.Plan.Tasks[i]
		if _, ok := idSet[task.ID]; !ok {
			continue
		}
		if task.Status != planpkg.TaskRunning {
			continue
		}
		task.Status = planpkg.TaskQueued
		task.Error = ""
	}
	running := make([]string, 0, len(run.Plan.Tasks))
	for i := range run.Plan.Tasks {
		if run.Plan.Tasks[i].Status == planpkg.TaskRunning {
			running = append(running, run.Plan.Tasks[i].ID)
		}
	}
	run.Plan.RunningTasks = running
	run.Plan.ActiveTask = ""
	if len(running) > 0 {
		run.Plan.ActiveTask = running[0]
	}
}

// shallowCopyRun creates a shallow copy of Run with a deep-copied Plan so
// parallel goroutines get fully independent state. This eliminates the
// pointer aliasing risk where goroutines could observe Plan mutations from
// the main thread or each other.
func shallowCopyRun(run *Run) *Run {
	c := *run
	if run.Plan != nil {
		c.Plan = clonePlan(run.Plan)
	}
	if run.ExecutionGraph != nil {
		c.ExecutionGraph = cloneExecutionGraph(run.ExecutionGraph)
	}
	return &c
}

func summarizePlanTaskResult(content string) string {
	content = strings.TrimSpace(content)
	if content == "" {
		return ""
	}
	runes := []rune(content)
	if len(runes) > 180 {
		content = string(runes[:179]) + "…"
	}
	return content
}

func planTaskLabel(task *planpkg.Task) string {
	if task == nil {
		return ""
	}
	if title := strings.TrimSpace(task.Title); title != "" {
		return title
	}
	return strings.TrimSpace(task.Goal)
}

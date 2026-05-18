package agent

import (
	"sync"

	planpkg "github.com/fulcrus/hopclaw/planner"
)

// PlanExecutionSnapshot is a point-in-time view of plan execution progress.
type PlanExecutionSnapshot struct {
	Goal             string
	RunningTasks     []TaskProgressItem
	Succeeded        []TaskProgressItem
	Failed           []TaskProgressItem
	Skipped          []TaskProgressItem
	CoverageWarnings []string
	Completed        int
	FailedCount      int
	SkippedCount     int
	Total            int
	FinalTask        string
}

// TaskProgressItem is a lightweight view of a task for UI/events.
type TaskProgressItem struct {
	ID     string `json:"id"`
	Title  string `json:"title"`
	Status string `json:"status"`
}

// maxTaskOutputChars caps the Output field stored per task to prevent
// unbounded memory growth in deep dependency chains. The Summary field
// (which downstream tasks actually use) is already capped at 180 chars
// in summarizePlanTaskResult.
const maxTaskOutputChars = 8000

// TaskResultAggregator collects task execution results in a thread-safe
// manner. It serves as the single source of truth for what each task
// produced, decoupled from the session transcript.
type TaskResultAggregator struct {
	mu      sync.RWMutex
	results map[string]TaskExecutionResult
}

// NewTaskResultAggregator creates a new aggregator.
func NewTaskResultAggregator() *TaskResultAggregator {
	return &TaskResultAggregator{
		results: make(map[string]TaskExecutionResult),
	}
}

func NewTaskResultAggregatorForRun(run *Run) *TaskResultAggregator {
	agg := NewTaskResultAggregator()
	if run == nil || run.ExecutionGraph == nil {
		return agg
	}
	for _, task := range run.ExecutionGraph.Tasks {
		if task.LastOutcome == nil {
			continue
		}
		result := TaskExecutionResult{
			TaskID:  task.ID,
			Status:  task.LastOutcome.Status,
			Summary: task.LastOutcome.Summary,
			Attempt: task.LastOutcome.Attempt,
			Outcome: cloneTaskOutcome(task.LastOutcome),
		}
		if task.LastOutcome.Error != nil {
			result.Error = task.LastOutcome.Error.Message
		}
		if len(task.LastOutcome.OutputBlocks) > 0 {
			result.Output = task.LastOutcome.OutputBlocks[0].Content
		}
		for _, artifact := range task.LastOutcome.Artifacts {
			if artifact.URI == "" {
				continue
			}
			result.Artifacts = append(result.Artifacts, artifact.URI)
		}
		agg.results[task.ID] = normalizeTaskExecutionResult(result)
	}
	return agg
}

// StartTask records that a task has started (no result yet).
func (a *TaskResultAggregator) StartTask(taskID string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if _, ok := a.results[taskID]; !ok {
		a.results[taskID] = TaskExecutionResult{
			TaskID: taskID,
			Status: planpkg.TaskRunning,
		}
	}
}

// CompleteTask records a completed task result. Output is truncated to
// maxTaskOutputChars to prevent unbounded memory growth — downstream
// tasks only use the Summary field.
func (a *TaskResultAggregator) CompleteTask(result TaskExecutionResult) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.results[result.TaskID] = normalizeTaskExecutionResult(result)
}

// FailTask records a failed task result.
func (a *TaskResultAggregator) FailTask(result TaskExecutionResult) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.results[result.TaskID] = normalizeTaskExecutionResult(result)
}

// Result returns the result for a specific task.
func (a *TaskResultAggregator) Result(taskID string) (TaskExecutionResult, bool) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	r, ok := a.results[taskID]
	return r, ok
}

// CompletedResults returns all results with status Completed.
func (a *TaskResultAggregator) CompletedResults() []TaskExecutionResult {
	a.mu.RLock()
	defer a.mu.RUnlock()
	var out []TaskExecutionResult
	for _, r := range a.results {
		if r.Status == planpkg.TaskCompleted {
			out = append(out, r)
		}
	}
	return out
}

// DependencyResults returns completed results for the given task IDs.
func (a *TaskResultAggregator) DependencyResults(depIDs []string) []TaskExecutionResult {
	if len(depIDs) == 0 {
		return nil
	}
	a.mu.RLock()
	defer a.mu.RUnlock()
	var out []TaskExecutionResult
	for _, id := range depIDs {
		if r, ok := a.results[id]; ok && r.Status == planpkg.TaskCompleted {
			out = append(out, r)
		}
	}
	return out
}

// Snapshot produces a point-in-time view of plan execution.
func (a *TaskResultAggregator) Snapshot(plan *planpkg.Plan) PlanExecutionSnapshot {
	a.mu.RLock()
	defer a.mu.RUnlock()

	snap := PlanExecutionSnapshot{
		Total: planpkg.TotalCount(plan),
	}
	if plan != nil {
		snap.Goal = plan.Goal
		snap.FinalTask = plan.FinalTask
	}
	if plan == nil {
		return snap
	}
	for _, task := range plan.Tasks {
		item := TaskProgressItem{
			ID:     task.ID,
			Title:  taskLabel(&task),
			Status: string(task.Status),
		}
		switch task.Status {
		case planpkg.TaskRunning:
			snap.RunningTasks = append(snap.RunningTasks, item)
		case planpkg.TaskCompleted:
			snap.Succeeded = append(snap.Succeeded, item)
			snap.Completed++
		case planpkg.TaskFailed:
			snap.Failed = append(snap.Failed, item)
			snap.FailedCount++
		case planpkg.TaskSkipped:
			snap.Skipped = append(snap.Skipped, item)
			snap.SkippedCount++
		}
	}
	return snap
}

func taskLabel(task *planpkg.Task) string {
	if task == nil {
		return ""
	}
	if task.Title != "" {
		return task.Title
	}
	return task.Goal
}

func normalizeTaskExecutionResult(result TaskExecutionResult) TaskExecutionResult {
	if len(result.Output) > maxTaskOutputChars {
		result.Output = result.Output[:maxTaskOutputChars] + "\n[truncated]"
	}
	if result.Outcome != nil {
		result.Outcome = cloneTaskOutcome(result.Outcome)
	}
	return result
}

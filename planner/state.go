package planner

import (
	"strings"

	"github.com/fulcrus/hopclaw/logging"
)

var log = logging.WithSubsystem("planner")

// AllowedTaskTransitions defines the explicit state machine for task status.
var AllowedTaskTransitions = map[TaskStatus][]TaskStatus{
	TaskQueued:    {TaskRunning, TaskSkipped, TaskCancelled},
	TaskRunning:   {TaskCompleted, TaskFailed, TaskSkipped, TaskCancelled},
	TaskCompleted: {},
	TaskFailed:    {},
	TaskSkipped:   {},
	TaskCancelled: {},
}

// ValidTaskTransition returns true if transitioning from → to is allowed.
func ValidTaskTransition(from, to TaskStatus) bool {
	allowed, ok := AllowedTaskTransitions[from]
	if !ok {
		return false
	}
	for _, s := range allowed {
		if s == to {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// Normalization
// ---------------------------------------------------------------------------

func NormalizeExecution(plan *Plan) {
	if plan == nil {
		return
	}
	plan.ActiveTask = strings.TrimSpace(plan.ActiveTask)
	for i := range plan.Tasks {
		task := &plan.Tasks[i]
		task.ResultSummary = strings.TrimSpace(task.ResultSummary)
		task.Error = strings.TrimSpace(task.Error)
		switch task.Status {
		case "", TaskQueued, TaskRunning, TaskCompleted, TaskFailed, TaskCancelled, TaskSkipped:
		default:
			log.Warn("planner: invalid task status normalized to queued", "task_id", task.ID, "status", task.Status)
			task.Status = TaskQueued
		}
		if task.Status == "" {
			task.Status = TaskQueued
		}
	}
	// Sync legacy ActiveTask with RunningTasks.
	if plan.ActiveTask != "" {
		if task := FindTask(plan, plan.ActiveTask); task == nil || task.Status != TaskRunning {
			plan.ActiveTask = ""
		}
	}
	// Clean RunningTasks: remove entries that are no longer running.
	if len(plan.RunningTasks) > 0 {
		clean := plan.RunningTasks[:0]
		for _, id := range plan.RunningTasks {
			if t := FindTask(plan, id); t != nil && t.Status == TaskRunning {
				clean = append(clean, id)
			}
		}
		plan.RunningTasks = clean
	}
}

// ---------------------------------------------------------------------------
// Lookups
// ---------------------------------------------------------------------------

func FindTask(plan *Plan, id string) *Task {
	if plan == nil || strings.TrimSpace(id) == "" {
		return nil
	}
	for i := range plan.Tasks {
		if plan.Tasks[i].ID == id {
			return &plan.Tasks[i]
		}
	}
	return nil
}

// Active returns the legacy single active task. Prefer Running() for
// parallel-aware code.
func Active(plan *Plan) *Task {
	if plan == nil || strings.TrimSpace(plan.ActiveTask) == "" {
		return nil
	}
	return FindTask(plan, plan.ActiveTask)
}

// Running returns all currently running tasks.
func Running(plan *Plan) []*Task {
	if plan == nil {
		return nil
	}
	var out []*Task
	for i := range plan.Tasks {
		if plan.Tasks[i].Status == TaskRunning {
			out = append(out, &plan.Tasks[i])
		}
	}
	return out
}

// ---------------------------------------------------------------------------
// Ready tasks (dependency-satisfied, queued)
// ---------------------------------------------------------------------------

// NextReady returns the first queued task whose dependencies are all
// completed. Kept for backward compatibility with serial execution.
func NextReady(plan *Plan) *Task {
	if plan == nil {
		return nil
	}
	for i := range plan.Tasks {
		task := &plan.Tasks[i]
		if task.Status != TaskQueued {
			continue
		}
		if depsCompleted(plan, task) {
			return task
		}
	}
	return nil
}

// ReadyTasks returns all queued tasks whose dependencies are completed.
func ReadyTasks(plan *Plan) []*Task {
	if plan == nil {
		return nil
	}
	var out []*Task
	for i := range plan.Tasks {
		task := &plan.Tasks[i]
		if task.Status != TaskQueued {
			continue
		}
		if depsCompleted(plan, task) {
			out = append(out, task)
		}
	}
	return out
}

func depsCompleted(plan *Plan, task *Task) bool {
	for _, dep := range task.DependsOn {
		d := FindTask(plan, dep)
		if d == nil || d.Status != TaskCompleted {
			return false
		}
	}
	return true
}

// ---------------------------------------------------------------------------
// Mutations
// ---------------------------------------------------------------------------

// MarkRunning transitions queued tasks to running and records them in
// RunningTasks. Also sets legacy ActiveTask to the first task if empty.
func MarkRunning(plan *Plan, ids ...string) {
	if plan == nil {
		return
	}
	for _, id := range ids {
		if t := FindTask(plan, id); t != nil && t.Status == TaskQueued {
			t.Status = TaskRunning
			t.Error = ""
			plan.RunningTasks = appendUnique(plan.RunningTasks, id)
			log.Debug("task status transition", "task_id", id, "from", string(TaskQueued), "to", string(TaskRunning))
		}
	}
	if plan.ActiveTask == "" && len(plan.RunningTasks) > 0 {
		plan.ActiveTask = plan.RunningTasks[0]
	}
}

// MarkCompleted transitions a running task to completed with a summary.
func MarkCompleted(plan *Plan, id, summary string) {
	if plan == nil {
		return
	}
	if t := FindTask(plan, id); t != nil {
		if t.Status != TaskRunning {
			log.Warn("MarkCompleted called on non-running task",
				"task_id", id, "status", t.Status)
			return
		}
		t.Status = TaskCompleted
		log.Debug("task status transition", "task_id", id, "from", string(TaskRunning), "to", string(TaskCompleted))
		t.Error = ""
		t.ResultSummary = strings.TrimSpace(summary)
	}
	plan.RunningTasks = removeString(plan.RunningTasks, id)
	if plan.ActiveTask == id {
		plan.ActiveTask = ""
		if len(plan.RunningTasks) > 0 {
			plan.ActiveTask = plan.RunningTasks[0]
		}
	}
}

// MarkFailed transitions a running task to failed with an error message.
func MarkFailed(plan *Plan, id, errMsg string) {
	if plan == nil {
		return
	}
	if t := FindTask(plan, id); t != nil {
		if t.Status != TaskRunning {
			log.Warn("MarkFailed called on non-running task",
				"task_id", id, "status", t.Status)
			return
		}
		t.Status = TaskFailed
		log.Debug("task status transition", "task_id", id, "from", string(TaskRunning), "to", string(TaskFailed))
		t.Error = strings.TrimSpace(errMsg)
	}
	plan.RunningTasks = removeString(plan.RunningTasks, id)
	if plan.ActiveTask == id {
		plan.ActiveTask = ""
		if len(plan.RunningTasks) > 0 {
			plan.ActiveTask = plan.RunningTasks[0]
		}
	}
}

// MarkSkipped transitions a queued task to skipped (dependency unmet).
func MarkSkipped(plan *Plan, id, reason string) {
	if plan == nil {
		return
	}
	if t := FindTask(plan, id); t != nil && (t.Status == TaskQueued || t.Status == TaskRunning) {
		prev := t.Status
		t.Status = TaskSkipped
		t.Error = strings.TrimSpace(reason)
		log.Debug("task status transition", "task_id", id, "from", string(prev), "to", string(TaskSkipped))
	}
	plan.RunningTasks = removeString(plan.RunningTasks, id)
	if plan.ActiveTask == id {
		plan.ActiveTask = ""
	}
}

// SkipDependentsOf marks all tasks that transitively depend on failedID as
// skipped, unless they have an alternative completed dependency path.
func SkipDependentsOf(plan *Plan, failedID string) {
	if plan == nil {
		return
	}
	// Iteratively propagate: a task is skipped if any of its deps is
	// failed or skipped.
	changed := true
	for changed {
		changed = false
		for i := range plan.Tasks {
			t := &plan.Tasks[i]
			if t.Status != TaskQueued {
				continue
			}
			for _, dep := range t.DependsOn {
				d := FindTask(plan, dep)
				if d != nil && (d.Status == TaskFailed || d.Status == TaskSkipped) {
					t.Status = TaskSkipped
					t.Error = "dependency " + dep + " failed"
					log.Debug("task status transition", "task_id", t.ID, "from", string(TaskQueued), "to", string(TaskSkipped))
					changed = true
					break
				}
			}
		}
	}
}

// CancelRunning cancels all currently running tasks.
func CancelRunning(plan *Plan, reason string) {
	if plan == nil {
		return
	}
	for i := range plan.Tasks {
		if plan.Tasks[i].Status == TaskRunning {
			plan.Tasks[i].Status = TaskCancelled
			plan.Tasks[i].Error = strings.TrimSpace(reason)
			log.Debug("task status transition", "task_id", plan.Tasks[i].ID, "from", string(TaskRunning), "to", string(TaskCancelled))
		}
	}
	plan.RunningTasks = nil
	plan.ActiveTask = ""
}

// ---------------------------------------------------------------------------
// Counters
// ---------------------------------------------------------------------------

func CompletedCount(plan *Plan) int {
	return countState(plan, TaskCompleted)
}

func FailedCount(plan *Plan) int {
	return countState(plan, TaskFailed)
}

func SkippedCount(plan *Plan) int {
	return countState(plan, TaskSkipped)
}

func RunningCount(plan *Plan) int {
	return countState(plan, TaskRunning)
}

func TotalCount(plan *Plan) int {
	if plan == nil {
		return 0
	}
	return len(plan.Tasks)
}

func countState(plan *Plan, state TaskStatus) int {
	if plan == nil {
		return 0
	}
	n := 0
	for _, t := range plan.Tasks {
		if t.Status == state {
			n++
		}
	}
	return n
}

// ---------------------------------------------------------------------------
// Terminal checks
// ---------------------------------------------------------------------------

// IsDone returns true when all tasks are completed.
func IsDone(plan *Plan) bool {
	if plan == nil || len(plan.Tasks) == 0 {
		return true
	}
	for _, task := range plan.Tasks {
		if task.Status != TaskCompleted {
			return false
		}
	}
	return true
}

// Terminal returns true when no further progress is possible: all tasks
// are in a terminal state (completed, failed, skipped, cancelled).
func Terminal(plan *Plan) bool {
	if plan == nil || len(plan.Tasks) == 0 {
		return true
	}
	for _, task := range plan.Tasks {
		switch task.Status {
		case TaskCompleted, TaskFailed, TaskSkipped, TaskCancelled:
			continue
		default:
			return false
		}
	}
	return true
}

// HasFailures returns true if any task has failed.
func HasFailures(plan *Plan) bool {
	if plan == nil {
		return false
	}
	for _, task := range plan.Tasks {
		if task.Status == TaskFailed {
			return true
		}
	}
	return false
}

// Incomplete returns all tasks not yet in a terminal state.
func Incomplete(plan *Plan) []*Task {
	if plan == nil {
		return nil
	}
	out := make([]*Task, 0, len(plan.Tasks))
	for i := range plan.Tasks {
		task := &plan.Tasks[i]
		switch task.Status {
		case TaskCompleted, TaskFailed, TaskSkipped, TaskCancelled:
			continue
		}
		out = append(out, task)
	}
	return out
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func appendUnique(slice []string, val string) []string {
	for _, s := range slice {
		if s == val {
			return slice
		}
	}
	return append(slice, val)
}

func removeString(slice []string, val string) []string {
	out := slice[:0]
	for _, s := range slice {
		if s != val {
			out = append(out, s)
		}
	}
	return out
}

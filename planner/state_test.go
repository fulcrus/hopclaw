package planner

import "testing"

// ---------------------------------------------------------------------------
// NormalizeExecution
// ---------------------------------------------------------------------------

func TestNormalizeExecutionNilPlan(t *testing.T) {
	t.Parallel()
	// Must not panic on nil.
	NormalizeExecution(nil)
}

func TestNormalizeExecutionDefaultsEmptyStateToQueued(t *testing.T) {
	t.Parallel()
	plan := &Plan{
		Goal: "normalize",
		Tasks: []Task{
			{ID: "a", Kind: TaskExecute, Goal: "A", Status: ""},
		},
	}
	NormalizeExecution(plan)
	if plan.Tasks[0].Status != TaskQueued {
		t.Fatalf("state = %q, want %q", plan.Tasks[0].Status, TaskQueued)
	}
}

func TestNormalizeExecutionInvalidStateResetsToQueued(t *testing.T) {
	t.Parallel()
	plan := &Plan{
		Goal: "normalize invalid",
		Tasks: []Task{
			{ID: "a", Kind: TaskExecute, Goal: "A", Status: "invalid_state"},
		},
	}
	NormalizeExecution(plan)
	if plan.Tasks[0].Status != TaskQueued {
		t.Fatalf("state = %q, want %q", plan.Tasks[0].Status, TaskQueued)
	}
}

func TestNormalizeExecutionTrimsWhitespace(t *testing.T) {
	t.Parallel()
	plan := &Plan{
		Goal:       "normalize whitespace",
		ActiveTask: "  a  ",
		Tasks: []Task{
			{ID: "a", Kind: TaskExecute, Goal: "A", Status: TaskRunning,
				ResultSummary: "  summary  ", Error: "  err  "},
		},
	}
	NormalizeExecution(plan)
	if plan.Tasks[0].ResultSummary != "summary" {
		t.Fatalf("ResultSummary = %q, want %q", plan.Tasks[0].ResultSummary, "summary")
	}
	if plan.Tasks[0].Error != "err" {
		t.Fatalf("Error = %q, want %q", plan.Tasks[0].Error, "err")
	}
	if plan.ActiveTask != "a" {
		t.Fatalf("ActiveTask = %q, want %q", plan.ActiveTask, "a")
	}
}

func TestNormalizeExecutionClearsActiveTaskIfNotRunning(t *testing.T) {
	t.Parallel()
	plan := &Plan{
		Goal:       "stale active",
		ActiveTask: "a",
		Tasks: []Task{
			{ID: "a", Kind: TaskExecute, Goal: "A", Status: TaskCompleted},
		},
	}
	NormalizeExecution(plan)
	if plan.ActiveTask != "" {
		t.Fatalf("ActiveTask = %q, want empty", plan.ActiveTask)
	}
}

func TestNormalizeExecutionClearsActiveTaskIfNotFound(t *testing.T) {
	t.Parallel()
	plan := &Plan{
		Goal:       "missing active",
		ActiveTask: "missing",
		Tasks: []Task{
			{ID: "a", Kind: TaskExecute, Goal: "A", Status: TaskRunning},
		},
	}
	NormalizeExecution(plan)
	if plan.ActiveTask != "" {
		t.Fatalf("ActiveTask = %q, want empty", plan.ActiveTask)
	}
}

func TestNormalizeExecutionCleansRunningTasks(t *testing.T) {
	t.Parallel()
	plan := &Plan{
		Goal:         "clean running",
		RunningTasks: []string{"a", "b", "missing"},
		Tasks: []Task{
			{ID: "a", Kind: TaskExecute, Goal: "A", Status: TaskRunning},
			{ID: "b", Kind: TaskExecute, Goal: "B", Status: TaskCompleted},
		},
	}
	NormalizeExecution(plan)
	if len(plan.RunningTasks) != 1 || plan.RunningTasks[0] != "a" {
		t.Fatalf("RunningTasks = %v, want [a]", plan.RunningTasks)
	}
}

func TestNormalizeExecutionPreservesValidStates(t *testing.T) {
	t.Parallel()
	plan := &Plan{
		Goal: "preserve states",
		Tasks: []Task{
			{ID: "a", Kind: TaskExecute, Goal: "A", Status: TaskCompleted},
			{ID: "b", Kind: TaskExecute, Goal: "B", Status: TaskFailed},
			{ID: "c", Kind: TaskExecute, Goal: "C", Status: TaskCancelled},
			{ID: "d", Kind: TaskExecute, Goal: "D", Status: TaskSkipped},
			{ID: "e", Kind: TaskExecute, Goal: "E", Status: TaskRunning},
		},
	}
	NormalizeExecution(plan)
	expected := []TaskStatus{TaskCompleted, TaskFailed, TaskCancelled, TaskSkipped, TaskRunning}
	for i, want := range expected {
		if plan.Tasks[i].Status != want {
			t.Fatalf("task %s status = %q, want %q", plan.Tasks[i].ID, plan.Tasks[i].Status, want)
		}
	}
}

// ---------------------------------------------------------------------------
// FindTask
// ---------------------------------------------------------------------------

func TestFindTaskNilPlan(t *testing.T) {
	t.Parallel()
	if FindTask(nil, "a") != nil {
		t.Fatal("expected nil for nil plan")
	}
}

func TestFindTaskEmptyID(t *testing.T) {
	t.Parallel()
	plan := &Plan{Goal: "test", Tasks: []Task{{ID: "a", Goal: "A"}}}
	if FindTask(plan, "") != nil {
		t.Fatal("expected nil for empty id")
	}
	if FindTask(plan, "   ") != nil {
		t.Fatal("expected nil for whitespace id")
	}
}

func TestFindTaskFound(t *testing.T) {
	t.Parallel()
	plan := &Plan{
		Goal: "test",
		Tasks: []Task{
			{ID: "a", Kind: TaskExecute, Goal: "A"},
			{ID: "b", Kind: TaskExecute, Goal: "B"},
		},
	}
	task := FindTask(plan, "b")
	if task == nil {
		t.Fatal("expected to find task b")
	}
	if task.ID != "b" {
		t.Fatalf("task.ID = %q, want b", task.ID)
	}
}

func TestFindTaskNotFound(t *testing.T) {
	t.Parallel()
	plan := &Plan{
		Goal:  "test",
		Tasks: []Task{{ID: "a", Kind: TaskExecute, Goal: "A"}},
	}
	if FindTask(plan, "missing") != nil {
		t.Fatal("expected nil for missing task")
	}
}

func TestFindTaskReturnsPointerToOriginal(t *testing.T) {
	t.Parallel()
	plan := &Plan{
		Goal:  "test",
		Tasks: []Task{{ID: "a", Kind: TaskExecute, Goal: "original"}},
	}
	task := FindTask(plan, "a")
	task.Goal = "mutated"
	if plan.Tasks[0].Goal != "mutated" {
		t.Fatal("FindTask should return a pointer to the original task")
	}
}

// ---------------------------------------------------------------------------
// Active
// ---------------------------------------------------------------------------

func TestActiveNilPlan(t *testing.T) {
	t.Parallel()
	if Active(nil) != nil {
		t.Fatal("expected nil for nil plan")
	}
}

func TestActiveEmptyActiveTask(t *testing.T) {
	t.Parallel()
	plan := &Plan{Goal: "test", Tasks: []Task{{ID: "a", Goal: "A"}}}
	if Active(plan) != nil {
		t.Fatal("expected nil when ActiveTask is empty")
	}
}

func TestActiveReturnsCorrectTask(t *testing.T) {
	t.Parallel()
	plan := &Plan{
		Goal:       "test",
		ActiveTask: "b",
		Tasks: []Task{
			{ID: "a", Kind: TaskExecute, Goal: "A"},
			{ID: "b", Kind: TaskExecute, Goal: "B"},
		},
	}
	task := Active(plan)
	if task == nil || task.ID != "b" {
		t.Fatalf("Active() = %v, want task b", task)
	}
}

// ---------------------------------------------------------------------------
// Running
// ---------------------------------------------------------------------------

func TestRunningNilPlan(t *testing.T) {
	t.Parallel()
	if Running(nil) != nil {
		t.Fatal("expected nil for nil plan")
	}
}

func TestRunningReturnsOnlyRunningTasks(t *testing.T) {
	t.Parallel()
	plan := &Plan{
		Goal: "test running",
		Tasks: []Task{
			{ID: "a", Kind: TaskExecute, Goal: "A", Status: TaskRunning},
			{ID: "b", Kind: TaskExecute, Goal: "B", Status: TaskCompleted},
			{ID: "c", Kind: TaskExecute, Goal: "C", Status: TaskRunning},
			{ID: "d", Kind: TaskExecute, Goal: "D", Status: TaskQueued},
		},
	}
	running := Running(plan)
	if len(running) != 2 {
		t.Fatalf("Running() returned %d tasks, want 2", len(running))
	}
	ids := map[string]bool{}
	for _, task := range running {
		ids[task.ID] = true
	}
	if !ids["a"] || !ids["c"] {
		t.Fatalf("expected tasks a and c, got %v", ids)
	}
}

func TestRunningNoRunningTasks(t *testing.T) {
	t.Parallel()
	plan := &Plan{
		Goal: "all done",
		Tasks: []Task{
			{ID: "a", Kind: TaskExecute, Goal: "A", Status: TaskCompleted},
		},
	}
	if len(Running(plan)) != 0 {
		t.Fatal("expected no running tasks")
	}
}

// ---------------------------------------------------------------------------
// NextReady
// ---------------------------------------------------------------------------

func TestNextReadyNilPlan(t *testing.T) {
	t.Parallel()
	if NextReady(nil) != nil {
		t.Fatal("expected nil for nil plan")
	}
}

func TestNextReadyNoDeps(t *testing.T) {
	t.Parallel()
	plan := &Plan{
		Goal: "test next",
		Tasks: []Task{
			{ID: "a", Kind: TaskExecute, Goal: "A", Status: TaskQueued},
		},
	}
	next := NextReady(plan)
	if next == nil || next.ID != "a" {
		t.Fatalf("NextReady() = %v, want task a", next)
	}
}

func TestNextReadySkipsNonQueued(t *testing.T) {
	t.Parallel()
	plan := &Plan{
		Goal: "skip non-queued",
		Tasks: []Task{
			{ID: "a", Kind: TaskExecute, Goal: "A", Status: TaskRunning},
			{ID: "b", Kind: TaskExecute, Goal: "B", Status: TaskQueued},
		},
	}
	next := NextReady(plan)
	if next == nil || next.ID != "b" {
		t.Fatalf("NextReady() = %v, want task b", next)
	}
}

func TestNextReadyRespectsDependencies(t *testing.T) {
	t.Parallel()
	plan := &Plan{
		Goal: "deps",
		Tasks: []Task{
			{ID: "a", Kind: TaskExecute, Goal: "A", Status: TaskQueued},
			{ID: "b", Kind: TaskExecute, Goal: "B", Status: TaskQueued, DependsOn: []string{"a"}},
		},
	}
	next := NextReady(plan)
	if next == nil || next.ID != "a" {
		t.Fatalf("NextReady() = %v, want task a (b's dep not met)", next)
	}
}

func TestNextReadyReturnsNilWhenAllBlocked(t *testing.T) {
	t.Parallel()
	plan := &Plan{
		Goal: "blocked",
		Tasks: []Task{
			{ID: "a", Kind: TaskExecute, Goal: "A", Status: TaskRunning},
			{ID: "b", Kind: TaskExecute, Goal: "B", Status: TaskQueued, DependsOn: []string{"a"}},
		},
	}
	if NextReady(plan) != nil {
		t.Fatal("expected nil when all queued tasks are blocked")
	}
}

// ---------------------------------------------------------------------------
// ReadyTasks
// ---------------------------------------------------------------------------

func TestReadyTasksNilPlan(t *testing.T) {
	t.Parallel()
	if ReadyTasks(nil) != nil {
		t.Fatal("expected nil for nil plan")
	}
}

func TestReadyTasksMultipleReady(t *testing.T) {
	t.Parallel()
	plan := &Plan{
		Goal: "parallel ready",
		Tasks: []Task{
			{ID: "a", Kind: TaskExecute, Goal: "A", Status: TaskQueued},
			{ID: "b", Kind: TaskExecute, Goal: "B", Status: TaskQueued},
			{ID: "c", Kind: TaskExecute, Goal: "C", Status: TaskQueued, DependsOn: []string{"a"}},
		},
	}
	ready := ReadyTasks(plan)
	if len(ready) != 2 {
		t.Fatalf("ReadyTasks() returned %d, want 2", len(ready))
	}
}

// ---------------------------------------------------------------------------
// MarkRunning
// ---------------------------------------------------------------------------

func TestMarkRunningNilPlan(t *testing.T) {
	t.Parallel()
	// Must not panic.
	MarkRunning(nil, "a")
}

func TestMarkRunningTransitionsQueuedToRunning(t *testing.T) {
	t.Parallel()
	plan := &Plan{
		Goal: "mark running",
		Tasks: []Task{
			{ID: "a", Kind: TaskExecute, Goal: "A", Status: TaskQueued},
		},
	}
	MarkRunning(plan, "a")
	if plan.Tasks[0].Status != TaskRunning {
		t.Fatalf("state = %q, want running", plan.Tasks[0].Status)
	}
	if len(plan.RunningTasks) != 1 || plan.RunningTasks[0] != "a" {
		t.Fatalf("RunningTasks = %v, want [a]", plan.RunningTasks)
	}
	if plan.ActiveTask != "a" {
		t.Fatalf("ActiveTask = %q, want a", plan.ActiveTask)
	}
}

func TestMarkRunningIgnoresNonQueued(t *testing.T) {
	t.Parallel()
	plan := &Plan{
		Goal: "ignore non-queued",
		Tasks: []Task{
			{ID: "a", Kind: TaskExecute, Goal: "A", Status: TaskCompleted},
		},
	}
	MarkRunning(plan, "a")
	if plan.Tasks[0].Status != TaskCompleted {
		t.Fatalf("state = %q, want completed", plan.Tasks[0].Status)
	}
}

func TestMarkRunningDoesNotDuplicate(t *testing.T) {
	t.Parallel()
	plan := &Plan{
		Goal: "no dup",
		Tasks: []Task{
			{ID: "a", Kind: TaskExecute, Goal: "A", Status: TaskQueued},
		},
	}
	MarkRunning(plan, "a")
	MarkRunning(plan, "a") // second call should not duplicate
	if len(plan.RunningTasks) != 1 {
		t.Fatalf("RunningTasks = %v, want [a]", plan.RunningTasks)
	}
}

func TestMarkRunningClearsError(t *testing.T) {
	t.Parallel()
	plan := &Plan{
		Goal: "clear error",
		Tasks: []Task{
			{ID: "a", Kind: TaskExecute, Goal: "A", Status: TaskQueued, Error: "old error"},
		},
	}
	MarkRunning(plan, "a")
	if plan.Tasks[0].Error != "" {
		t.Fatalf("Error = %q, want empty", plan.Tasks[0].Error)
	}
}

func TestMarkRunningPreservesExistingActiveTask(t *testing.T) {
	t.Parallel()
	plan := &Plan{
		Goal:         "preserve active",
		ActiveTask:   "a",
		RunningTasks: []string{"a"},
		Tasks: []Task{
			{ID: "a", Kind: TaskExecute, Goal: "A", Status: TaskRunning},
			{ID: "b", Kind: TaskExecute, Goal: "B", Status: TaskQueued},
		},
	}
	MarkRunning(plan, "b")
	if plan.ActiveTask != "a" {
		t.Fatalf("ActiveTask = %q, want a (preserved)", plan.ActiveTask)
	}
}

// ---------------------------------------------------------------------------
// MarkCompleted
// ---------------------------------------------------------------------------

func TestMarkCompletedNilPlan(t *testing.T) {
	t.Parallel()
	MarkCompleted(nil, "a", "done")
}

func TestMarkCompletedTransitionsAndStoresSummary(t *testing.T) {
	t.Parallel()
	plan := &Plan{
		Goal:         "mark completed",
		ActiveTask:   "a",
		RunningTasks: []string{"a"},
		Tasks: []Task{
			{ID: "a", Kind: TaskExecute, Goal: "A", Status: TaskRunning},
		},
	}
	MarkCompleted(plan, "a", "  finished ok  ")
	if plan.Tasks[0].Status != TaskCompleted {
		t.Fatalf("state = %q, want completed", plan.Tasks[0].Status)
	}
	if plan.Tasks[0].ResultSummary != "finished ok" {
		t.Fatalf("ResultSummary = %q", plan.Tasks[0].ResultSummary)
	}
	if plan.Tasks[0].Error != "" {
		t.Fatalf("Error = %q, want empty", plan.Tasks[0].Error)
	}
	if len(plan.RunningTasks) != 0 {
		t.Fatalf("RunningTasks = %v, want empty", plan.RunningTasks)
	}
	if plan.ActiveTask != "" {
		t.Fatalf("ActiveTask = %q, want empty", plan.ActiveTask)
	}
}

func TestMarkCompletedShiftsActiveTask(t *testing.T) {
	t.Parallel()
	plan := &Plan{
		Goal:         "shift active",
		ActiveTask:   "a",
		RunningTasks: []string{"a", "b"},
		Tasks: []Task{
			{ID: "a", Kind: TaskExecute, Goal: "A", Status: TaskRunning},
			{ID: "b", Kind: TaskExecute, Goal: "B", Status: TaskRunning},
		},
	}
	MarkCompleted(plan, "a", "done")
	if plan.ActiveTask != "b" {
		t.Fatalf("ActiveTask = %q, want b", plan.ActiveTask)
	}
}

// ---------------------------------------------------------------------------
// MarkFailed
// ---------------------------------------------------------------------------

func TestMarkFailedNilPlan(t *testing.T) {
	t.Parallel()
	MarkFailed(nil, "a", "err")
}

func TestMarkFailedTransitionsAndStoresError(t *testing.T) {
	t.Parallel()
	plan := &Plan{
		Goal:         "mark failed",
		ActiveTask:   "a",
		RunningTasks: []string{"a"},
		Tasks: []Task{
			{ID: "a", Kind: TaskExecute, Goal: "A", Status: TaskRunning},
		},
	}
	MarkFailed(plan, "a", "  something broke  ")
	if plan.Tasks[0].Status != TaskFailed {
		t.Fatalf("state = %q, want failed", plan.Tasks[0].Status)
	}
	if plan.Tasks[0].Error != "something broke" {
		t.Fatalf("Error = %q", plan.Tasks[0].Error)
	}
	if len(plan.RunningTasks) != 0 {
		t.Fatalf("RunningTasks = %v, want empty", plan.RunningTasks)
	}
}

func TestMarkFailedShiftsActiveTask(t *testing.T) {
	t.Parallel()
	plan := &Plan{
		Goal:         "shift on fail",
		ActiveTask:   "a",
		RunningTasks: []string{"a", "b"},
		Tasks: []Task{
			{ID: "a", Kind: TaskExecute, Goal: "A", Status: TaskRunning},
			{ID: "b", Kind: TaskExecute, Goal: "B", Status: TaskRunning},
		},
	}
	MarkFailed(plan, "a", "err")
	if plan.ActiveTask != "b" {
		t.Fatalf("ActiveTask = %q, want b", plan.ActiveTask)
	}
}

// ---------------------------------------------------------------------------
// MarkSkipped
// ---------------------------------------------------------------------------

func TestMarkSkippedNilPlan(t *testing.T) {
	t.Parallel()
	MarkSkipped(nil, "a", "reason")
}

func TestMarkSkippedFromQueued(t *testing.T) {
	t.Parallel()
	plan := &Plan{
		Goal: "skip queued",
		Tasks: []Task{
			{ID: "a", Kind: TaskExecute, Goal: "A", Status: TaskQueued},
		},
	}
	MarkSkipped(plan, "a", "  dep failed  ")
	if plan.Tasks[0].Status != TaskSkipped {
		t.Fatalf("state = %q, want skipped", plan.Tasks[0].Status)
	}
	if plan.Tasks[0].Error != "dep failed" {
		t.Fatalf("Error = %q", plan.Tasks[0].Error)
	}
}

func TestMarkSkippedFromRunning(t *testing.T) {
	t.Parallel()
	plan := &Plan{
		Goal:         "skip running",
		ActiveTask:   "a",
		RunningTasks: []string{"a"},
		Tasks: []Task{
			{ID: "a", Kind: TaskExecute, Goal: "A", Status: TaskRunning},
		},
	}
	MarkSkipped(plan, "a", "cancelled")
	if plan.Tasks[0].Status != TaskSkipped {
		t.Fatalf("state = %q, want skipped", plan.Tasks[0].Status)
	}
	if plan.ActiveTask != "" {
		t.Fatalf("ActiveTask = %q, want empty", plan.ActiveTask)
	}
	if len(plan.RunningTasks) != 0 {
		t.Fatalf("RunningTasks = %v, want empty", plan.RunningTasks)
	}
}

func TestMarkSkippedIgnoresCompleted(t *testing.T) {
	t.Parallel()
	plan := &Plan{
		Goal: "no skip completed",
		Tasks: []Task{
			{ID: "a", Kind: TaskExecute, Goal: "A", Status: TaskCompleted},
		},
	}
	MarkSkipped(plan, "a", "reason")
	if plan.Tasks[0].Status != TaskCompleted {
		t.Fatalf("state = %q, want completed (unchanged)", plan.Tasks[0].Status)
	}
}

// ---------------------------------------------------------------------------
// SkipDependentsOf
// ---------------------------------------------------------------------------

func TestSkipDependentsOfNilPlan(t *testing.T) {
	t.Parallel()
	SkipDependentsOf(nil, "a")
}

func TestSkipDependentsOfNoEffect(t *testing.T) {
	t.Parallel()
	plan := &Plan{
		Goal: "no effect",
		Tasks: []Task{
			{ID: "a", Kind: TaskExecute, Goal: "A", Status: TaskFailed},
			{ID: "b", Kind: TaskExecute, Goal: "B", Status: TaskQueued},
		},
	}
	SkipDependentsOf(plan, "a")
	if plan.Tasks[1].Status != TaskQueued {
		t.Fatal("task b should remain queued (no dependency on a)")
	}
}

func TestSkipDependentsOfTransitivePropagation(t *testing.T) {
	t.Parallel()
	plan := &Plan{
		Goal: "transitive",
		Tasks: []Task{
			{ID: "a", Kind: TaskExecute, Goal: "A", Status: TaskFailed},
			{ID: "b", Kind: TaskExecute, Goal: "B", Status: TaskQueued, DependsOn: []string{"a"}},
			{ID: "c", Kind: TaskExecute, Goal: "C", Status: TaskQueued, DependsOn: []string{"b"}},
			{ID: "d", Kind: TaskExecute, Goal: "D", Status: TaskQueued, DependsOn: []string{"c"}},
		},
	}
	SkipDependentsOf(plan, "a")
	for _, task := range plan.Tasks[1:] {
		if task.Status != TaskSkipped {
			t.Fatalf("task %s state = %q, want skipped", task.ID, task.Status)
		}
	}
}

func TestSkipDependentsOfSkippedDepAlsoSkips(t *testing.T) {
	t.Parallel()
	plan := &Plan{
		Goal: "skipped dep",
		Tasks: []Task{
			{ID: "a", Kind: TaskExecute, Goal: "A", Status: TaskSkipped},
			{ID: "b", Kind: TaskExecute, Goal: "B", Status: TaskQueued, DependsOn: []string{"a"}},
		},
	}
	SkipDependentsOf(plan, "a")
	if plan.Tasks[1].Status != TaskSkipped {
		t.Fatalf("task b state = %q, want skipped", plan.Tasks[1].Status)
	}
}

// ---------------------------------------------------------------------------
// CancelRunning
// ---------------------------------------------------------------------------

func TestCancelRunningNilPlan(t *testing.T) {
	t.Parallel()
	CancelRunning(nil, "reason")
}

func TestCancelRunningCancelsOnlyRunning(t *testing.T) {
	t.Parallel()
	plan := &Plan{
		Goal: "cancel",
		Tasks: []Task{
			{ID: "a", Kind: TaskExecute, Goal: "A", Status: TaskRunning},
			{ID: "b", Kind: TaskExecute, Goal: "B", Status: TaskQueued},
			{ID: "c", Kind: TaskExecute, Goal: "C", Status: TaskCompleted},
		},
		RunningTasks: []string{"a"},
		ActiveTask:   "a",
	}
	CancelRunning(plan, "  user requested  ")
	if plan.Tasks[0].Status != TaskCancelled {
		t.Fatalf("task a state = %q, want cancelled", plan.Tasks[0].Status)
	}
	if plan.Tasks[0].Error != "user requested" {
		t.Fatalf("task a Error = %q", plan.Tasks[0].Error)
	}
	if plan.Tasks[1].Status != TaskQueued {
		t.Fatal("task b should remain queued")
	}
	if plan.Tasks[2].Status != TaskCompleted {
		t.Fatal("task c should remain completed")
	}
	if len(plan.RunningTasks) != 0 {
		t.Fatalf("RunningTasks = %v, want nil", plan.RunningTasks)
	}
	if plan.ActiveTask != "" {
		t.Fatalf("ActiveTask = %q, want empty", plan.ActiveTask)
	}
}

// ---------------------------------------------------------------------------
// Counters
// ---------------------------------------------------------------------------

func TestCounters(t *testing.T) {
	t.Parallel()
	plan := &Plan{
		Goal: "counters",
		Tasks: []Task{
			{ID: "a", Kind: TaskExecute, Goal: "A", Status: TaskCompleted},
			{ID: "b", Kind: TaskExecute, Goal: "B", Status: TaskCompleted},
			{ID: "c", Kind: TaskExecute, Goal: "C", Status: TaskFailed},
			{ID: "d", Kind: TaskExecute, Goal: "D", Status: TaskSkipped},
			{ID: "e", Kind: TaskExecute, Goal: "E", Status: TaskRunning},
			{ID: "f", Kind: TaskExecute, Goal: "F", Status: TaskQueued},
		},
	}
	if CompletedCount(plan) != 2 {
		t.Fatalf("CompletedCount = %d, want 2", CompletedCount(plan))
	}
	if FailedCount(plan) != 1 {
		t.Fatalf("FailedCount = %d, want 1", FailedCount(plan))
	}
	if SkippedCount(plan) != 1 {
		t.Fatalf("SkippedCount = %d, want 1", SkippedCount(plan))
	}
	if RunningCount(plan) != 1 {
		t.Fatalf("RunningCount = %d, want 1", RunningCount(plan))
	}
	if TotalCount(plan) != 6 {
		t.Fatalf("TotalCount = %d, want 6", TotalCount(plan))
	}
}

func TestCountersNilPlan(t *testing.T) {
	t.Parallel()
	if CompletedCount(nil) != 0 {
		t.Fatal("CompletedCount(nil) should be 0")
	}
	if FailedCount(nil) != 0 {
		t.Fatal("FailedCount(nil) should be 0")
	}
	if SkippedCount(nil) != 0 {
		t.Fatal("SkippedCount(nil) should be 0")
	}
	if RunningCount(nil) != 0 {
		t.Fatal("RunningCount(nil) should be 0")
	}
	if TotalCount(nil) != 0 {
		t.Fatal("TotalCount(nil) should be 0")
	}
}

// ---------------------------------------------------------------------------
// IsDone
// ---------------------------------------------------------------------------

func TestIsDoneNilPlan(t *testing.T) {
	t.Parallel()
	if !IsDone(nil) {
		t.Fatal("IsDone(nil) should be true")
	}
}

func TestIsDoneEmptyTasks(t *testing.T) {
	t.Parallel()
	if !IsDone(&Plan{Goal: "empty"}) {
		t.Fatal("IsDone with no tasks should be true")
	}
}

func TestIsDoneAllCompleted(t *testing.T) {
	t.Parallel()
	plan := &Plan{
		Goal: "all done",
		Tasks: []Task{
			{ID: "a", Kind: TaskExecute, Goal: "A", Status: TaskCompleted},
			{ID: "b", Kind: TaskExecute, Goal: "B", Status: TaskCompleted},
		},
	}
	if !IsDone(plan) {
		t.Fatal("expected IsDone to be true")
	}
}

func TestIsDoneFalseWhenNotAllCompleted(t *testing.T) {
	t.Parallel()
	plan := &Plan{
		Goal: "not done",
		Tasks: []Task{
			{ID: "a", Kind: TaskExecute, Goal: "A", Status: TaskCompleted},
			{ID: "b", Kind: TaskExecute, Goal: "B", Status: TaskFailed},
		},
	}
	if IsDone(plan) {
		t.Fatal("expected IsDone to be false when not all completed")
	}
}

// ---------------------------------------------------------------------------
// Terminal
// ---------------------------------------------------------------------------

func TestTerminalNilPlan(t *testing.T) {
	t.Parallel()
	if !Terminal(nil) {
		t.Fatal("Terminal(nil) should be true")
	}
}

func TestTerminalAllTerminalStates(t *testing.T) {
	t.Parallel()
	plan := &Plan{
		Goal: "terminal",
		Tasks: []Task{
			{ID: "a", Kind: TaskExecute, Goal: "A", Status: TaskCompleted},
			{ID: "b", Kind: TaskExecute, Goal: "B", Status: TaskFailed},
			{ID: "c", Kind: TaskExecute, Goal: "C", Status: TaskSkipped},
			{ID: "d", Kind: TaskExecute, Goal: "D", Status: TaskCancelled},
		},
	}
	if !Terminal(plan) {
		t.Fatal("expected Terminal to be true")
	}
}

func TestTerminalFalseWhenQueued(t *testing.T) {
	t.Parallel()
	plan := &Plan{
		Goal: "not terminal",
		Tasks: []Task{
			{ID: "a", Kind: TaskExecute, Goal: "A", Status: TaskCompleted},
			{ID: "b", Kind: TaskExecute, Goal: "B", Status: TaskQueued},
		},
	}
	if Terminal(plan) {
		t.Fatal("expected Terminal to be false")
	}
}

func TestTerminalFalseWhenRunning(t *testing.T) {
	t.Parallel()
	plan := &Plan{
		Goal: "running",
		Tasks: []Task{
			{ID: "a", Kind: TaskExecute, Goal: "A", Status: TaskRunning},
		},
	}
	if Terminal(plan) {
		t.Fatal("expected Terminal to be false when tasks are running")
	}
}

// ---------------------------------------------------------------------------
// HasFailures
// ---------------------------------------------------------------------------

func TestHasFailuresNilPlan(t *testing.T) {
	t.Parallel()
	if HasFailures(nil) {
		t.Fatal("HasFailures(nil) should be false")
	}
}

func TestHasFailuresTrue(t *testing.T) {
	t.Parallel()
	plan := &Plan{
		Goal: "failures",
		Tasks: []Task{
			{ID: "a", Kind: TaskExecute, Goal: "A", Status: TaskCompleted},
			{ID: "b", Kind: TaskExecute, Goal: "B", Status: TaskFailed},
		},
	}
	if !HasFailures(plan) {
		t.Fatal("expected HasFailures to be true")
	}
}

func TestHasFailuresFalse(t *testing.T) {
	t.Parallel()
	plan := &Plan{
		Goal: "no failures",
		Tasks: []Task{
			{ID: "a", Kind: TaskExecute, Goal: "A", Status: TaskCompleted},
		},
	}
	if HasFailures(plan) {
		t.Fatal("expected HasFailures to be false")
	}
}

// ---------------------------------------------------------------------------
// Incomplete
// ---------------------------------------------------------------------------

func TestIncompleteNilPlan(t *testing.T) {
	t.Parallel()
	if Incomplete(nil) != nil {
		t.Fatal("Incomplete(nil) should be nil")
	}
}

func TestIncompleteReturnsNonTerminal(t *testing.T) {
	t.Parallel()
	plan := &Plan{
		Goal: "incomplete",
		Tasks: []Task{
			{ID: "a", Kind: TaskExecute, Goal: "A", Status: TaskCompleted},
			{ID: "b", Kind: TaskExecute, Goal: "B", Status: TaskQueued},
			{ID: "c", Kind: TaskExecute, Goal: "C", Status: TaskRunning},
			{ID: "d", Kind: TaskExecute, Goal: "D", Status: TaskFailed},
			{ID: "e", Kind: TaskExecute, Goal: "E", Status: TaskSkipped},
			{ID: "f", Kind: TaskExecute, Goal: "F", Status: TaskCancelled},
		},
	}
	inc := Incomplete(plan)
	if len(inc) != 2 {
		t.Fatalf("Incomplete() returned %d, want 2", len(inc))
	}
	ids := map[string]bool{}
	for _, task := range inc {
		ids[task.ID] = true
	}
	if !ids["b"] || !ids["c"] {
		t.Fatalf("expected tasks b and c, got %v", ids)
	}
}

func TestIncompleteAllTerminal(t *testing.T) {
	t.Parallel()
	plan := &Plan{
		Goal: "all terminal",
		Tasks: []Task{
			{ID: "a", Kind: TaskExecute, Goal: "A", Status: TaskCompleted},
			{ID: "b", Kind: TaskExecute, Goal: "B", Status: TaskFailed},
		},
	}
	inc := Incomplete(plan)
	if len(inc) != 0 {
		t.Fatalf("Incomplete() returned %d, want 0", len(inc))
	}
}

// ---------------------------------------------------------------------------
// ValidTaskTransition
// ---------------------------------------------------------------------------

func TestValidTaskTransitionAllowedPaths(t *testing.T) {
	t.Parallel()
	cases := []struct {
		from, to TaskStatus
	}{
		{TaskQueued, TaskRunning},
		{TaskQueued, TaskSkipped},
		{TaskQueued, TaskCancelled},
		{TaskRunning, TaskCompleted},
		{TaskRunning, TaskFailed},
		{TaskRunning, TaskSkipped},
		{TaskRunning, TaskCancelled},
	}
	for _, tc := range cases {
		if !ValidTaskTransition(tc.from, tc.to) {
			t.Errorf("ValidTaskTransition(%q, %q) = false, want true", tc.from, tc.to)
		}
	}
}

func TestValidTaskTransitionBlockedPaths(t *testing.T) {
	t.Parallel()
	cases := []struct {
		from, to TaskStatus
	}{
		{TaskCompleted, TaskRunning},
		{TaskCompleted, TaskFailed},
		{TaskFailed, TaskRunning},
		{TaskFailed, TaskCompleted},
		{TaskSkipped, TaskRunning},
		{TaskCancelled, TaskRunning},
		{TaskQueued, TaskCompleted},
		{TaskQueued, TaskFailed},
	}
	for _, tc := range cases {
		if ValidTaskTransition(tc.from, tc.to) {
			t.Errorf("ValidTaskTransition(%q, %q) = true, want false", tc.from, tc.to)
		}
	}
}

func TestAllowedTaskTransitionsCoversAllStatuses(t *testing.T) {
	t.Parallel()
	allStatuses := []TaskStatus{
		TaskQueued, TaskRunning, TaskCompleted, TaskFailed, TaskCancelled, TaskSkipped,
	}
	for _, s := range allStatuses {
		if _, ok := AllowedTaskTransitions[s]; !ok {
			t.Errorf("AllowedTaskTransitions missing entry for %q", s)
		}
	}
}

// ---------------------------------------------------------------------------
// Guard enforcement: MarkCompleted / MarkFailed reject non-running tasks
// ---------------------------------------------------------------------------

func TestMarkCompletedRejectsNonRunning(t *testing.T) {
	t.Parallel()
	plan := &Plan{
		Goal: "guard test",
		Tasks: []Task{
			{ID: "a", Kind: TaskExecute, Goal: "A", Status: TaskQueued},
		},
	}
	MarkCompleted(plan, "a", "should not complete")
	if plan.Tasks[0].Status != TaskQueued {
		t.Fatalf("state = %q, want queued (guard should reject)", plan.Tasks[0].Status)
	}
}

func TestMarkCompletedRejectsAlreadyCompleted(t *testing.T) {
	t.Parallel()
	plan := &Plan{
		Goal: "guard test",
		Tasks: []Task{
			{ID: "a", Kind: TaskExecute, Goal: "A", Status: TaskCompleted, ResultSummary: "original"},
		},
	}
	MarkCompleted(plan, "a", "overwrite attempt")
	if plan.Tasks[0].ResultSummary != "original" {
		t.Fatalf("ResultSummary = %q, want original (guard should reject)", plan.Tasks[0].ResultSummary)
	}
}

func TestMarkFailedRejectsNonRunning(t *testing.T) {
	t.Parallel()
	plan := &Plan{
		Goal: "guard test",
		Tasks: []Task{
			{ID: "a", Kind: TaskExecute, Goal: "A", Status: TaskQueued},
		},
	}
	MarkFailed(plan, "a", "should not fail")
	if plan.Tasks[0].Status != TaskQueued {
		t.Fatalf("state = %q, want queued (guard should reject)", plan.Tasks[0].Status)
	}
}

func TestMarkFailedRejectsAlreadyFailed(t *testing.T) {
	t.Parallel()
	plan := &Plan{
		Goal: "guard test",
		Tasks: []Task{
			{ID: "a", Kind: TaskExecute, Goal: "A", Status: TaskFailed, Error: "original error"},
		},
	}
	MarkFailed(plan, "a", "overwrite attempt")
	if plan.Tasks[0].Error != "original error" {
		t.Fatalf("Error = %q, want original error (guard should reject)", plan.Tasks[0].Error)
	}
}

// ---------------------------------------------------------------------------
// Full lifecycle behavioral proof
// ---------------------------------------------------------------------------

func TestTaskLifecycleFullChain(t *testing.T) {
	t.Parallel()
	plan := &Plan{
		Goal: "lifecycle",
		Tasks: []Task{
			{ID: "a", Kind: TaskExecute, Goal: "A", Status: TaskQueued},
			{ID: "b", Kind: TaskExecute, Goal: "B", Status: TaskQueued, DependsOn: []string{"a"}},
			{ID: "c", Kind: TaskExecute, Goal: "C", Status: TaskQueued, DependsOn: []string{"b"}},
		},
	}

	ready := ReadyTasks(plan)
	if len(ready) != 1 || ready[0].ID != "a" {
		t.Fatalf("initial ready = %v, want [a]", ready)
	}

	MarkRunning(plan, "a")
	if plan.Tasks[0].Status != TaskRunning {
		t.Fatal("a should be running")
	}
	if len(ReadyTasks(plan)) != 0 {
		t.Fatal("no tasks should be ready while a is running")
	}

	MarkCompleted(plan, "a", "done")
	ready = ReadyTasks(plan)
	if len(ready) != 1 || ready[0].ID != "b" {
		t.Fatalf("after a completes, ready = %v, want [b]", ready)
	}

	MarkRunning(plan, "b")
	MarkFailed(plan, "b", "oops")
	SkipDependentsOf(plan, "b")
	if plan.Tasks[2].Status != TaskSkipped {
		t.Fatalf("c status = %q, want skipped (b failed)", plan.Tasks[2].Status)
	}

	if !Terminal(plan) {
		t.Fatal("plan should be terminal after a=completed, b=failed, c=skipped")
	}
	if IsDone(plan) {
		t.Fatal("plan should NOT be done (b failed)")
	}
	if !HasFailures(plan) {
		t.Fatal("plan should have failures")
	}
}

func TestTaskLifecycleParallelBatch(t *testing.T) {
	t.Parallel()
	plan := &Plan{
		Goal: "parallel",
		Tasks: []Task{
			{ID: "a", Kind: TaskExecute, Goal: "A", Status: TaskQueued},
			{ID: "b", Kind: TaskExecute, Goal: "B", Status: TaskQueued},
			{ID: "c", Kind: TaskExecute, Goal: "C", Status: TaskQueued, DependsOn: []string{"a", "b"}},
		},
	}

	ready := ReadyTasks(plan)
	if len(ready) != 2 {
		t.Fatalf("ready = %d, want 2", len(ready))
	}

	MarkRunning(plan, "a", "b")
	if RunningCount(plan) != 2 {
		t.Fatalf("RunningCount = %d, want 2", RunningCount(plan))
	}

	MarkCompleted(plan, "a", "done a")
	if len(ReadyTasks(plan)) != 0 {
		t.Fatal("c should not be ready yet (b still running)")
	}

	MarkCompleted(plan, "b", "done b")
	ready = ReadyTasks(plan)
	if len(ready) != 1 || ready[0].ID != "c" {
		t.Fatalf("after both deps, ready = %v, want [c]", ready)
	}

	MarkRunning(plan, "c")
	MarkCompleted(plan, "c", "done c")
	if !IsDone(plan) {
		t.Fatal("plan should be done")
	}
}

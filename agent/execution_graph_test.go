package agent

import (
	"testing"

	planpkg "github.com/fulcrus/hopclaw/planner"
)

func TestSelectExecutionBatchAllowsIndependentReadOnlyTasks(t *testing.T) {
	t.Parallel()

	run := &Run{
		ID:        "run-read-only",
		SessionID: "sess-read-only",
		Plan: &planpkg.Plan{
			Strategy: planpkg.StrategyParallel,
			Tasks: []planpkg.Task{
				{ID: "a", Kind: planpkg.TaskResearch, Goal: "Inspect source A"},
				{ID: "b", Kind: planpkg.TaskReview, Goal: "Inspect source B"},
				{ID: "c", Kind: planpkg.TaskTranslate, Goal: "Translate findings"},
			},
		},
	}
	planpkg.NormalizeExecution(run.Plan)

	batch := selectExecutionBatch(run, planpkg.ReadyTasks(run.Plan))
	if len(batch) != 3 {
		t.Fatalf("len(batch) = %d, want 3", len(batch))
	}
}

func TestSelectExecutionBatchSkipsConflictingSessionWrites(t *testing.T) {
	t.Parallel()

	run := &Run{
		ID:        "run-conflict",
		SessionID: "sess-conflict",
		Plan: &planpkg.Plan{
			Strategy: planpkg.StrategyParallel,
			Tasks: []planpkg.Task{
				{ID: "write-a", Kind: planpkg.TaskWrite, Goal: "Write report A", Outputs: []string{"report.md"}},
				{ID: "write-b", Kind: planpkg.TaskWrite, Goal: "Write report B", Outputs: []string{"report.md"}},
				{ID: "read-c", Kind: planpkg.TaskResearch, Goal: "Inspect source C"},
			},
		},
	}
	planpkg.NormalizeExecution(run.Plan)

	batch := selectExecutionBatch(run, planpkg.ReadyTasks(run.Plan))
	if len(batch) != 2 {
		t.Fatalf("len(batch) = %d, want 2", len(batch))
	}
	if batch[0].ID != "write-a" {
		t.Fatalf("batch[0].ID = %q, want write-a", batch[0].ID)
	}
	if batch[1].ID != "read-c" {
		t.Fatalf("batch[1].ID = %q, want read-c", batch[1].ID)
	}
}

func TestSelectExecutionBatchAllowsDisjointWorkspaceWrites(t *testing.T) {
	t.Parallel()

	run := &Run{
		ID:        "run-disjoint-writes",
		SessionID: "sess-disjoint-writes",
		Plan: &planpkg.Plan{
			Strategy: planpkg.StrategyParallel,
			Tasks: []planpkg.Task{
				{ID: "write-a", Kind: planpkg.TaskWrite, Goal: "Write report A", Outputs: []string{"report-a.md"}},
				{ID: "write-b", Kind: planpkg.TaskWrite, Goal: "Write report B", Outputs: []string{"report-b.md"}},
				{ID: "read-c", Kind: planpkg.TaskResearch, Goal: "Inspect source C"},
			},
		},
	}
	planpkg.NormalizeExecution(run.Plan)

	batch := selectExecutionBatch(run, planpkg.ReadyTasks(run.Plan))
	if len(batch) != 3 {
		t.Fatalf("len(batch) = %d, want 3", len(batch))
	}
	if batch[0].ID != "write-a" {
		t.Fatalf("batch[0].ID = %q, want write-a", batch[0].ID)
	}
	if batch[1].ID != "write-b" {
		t.Fatalf("batch[1].ID = %q, want write-b", batch[1].ID)
	}
	if batch[2].ID != "read-c" {
		t.Fatalf("batch[2].ID = %q, want read-c", batch[2].ID)
	}
}

func TestRequeueExecutionTasksForRetryResetsRunningTasks(t *testing.T) {
	t.Parallel()

	run := &Run{
		ID:        "run-retry",
		SessionID: "sess-retry",
		Plan: &planpkg.Plan{
			Strategy: planpkg.StrategyParallel,
			Tasks: []planpkg.Task{
				{ID: "a", Kind: planpkg.TaskResearch, Goal: "A", Status: planpkg.TaskRunning},
				{ID: "b", Kind: planpkg.TaskResearch, Goal: "B", Status: planpkg.TaskRunning},
			},
			RunningTasks: []string{"a", "b"},
			ActiveTask:   "a",
		},
	}
	run.ExecutionGraph = &ExecutionGraph{
		RunID:          run.ID,
		SessionID:      run.SessionID,
		Scope:          "single_session",
		SingleSession:  true,
		SessionLocking: true,
		MergeStrategy:  MergeStrategyTaskOrder,
		Tasks: []ExecutionTask{
			{ID: "a", Status: planpkg.TaskRunning, AttemptCount: 1},
			{ID: "b", Status: planpkg.TaskRunning, AttemptCount: 1},
		},
	}

	requeueBatchTasksForRetry(run, []string{"a", "b"})

	for _, task := range run.Plan.Tasks {
		if task.Status != planpkg.TaskQueued {
			t.Fatalf("plan task %s status = %q, want queued", task.ID, task.Status)
		}
	}
	for _, task := range run.ExecutionGraph.Tasks {
		if task.Status != planpkg.TaskQueued {
			t.Fatalf("graph task %s status = %q, want queued", task.ID, task.Status)
		}
	}
}

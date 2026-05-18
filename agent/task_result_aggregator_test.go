package agent

import (
	"testing"
	"time"

	planpkg "github.com/fulcrus/hopclaw/planner"
	"github.com/fulcrus/hopclaw/resultmodel"
)

func TestTaskResultAggregatorStartAndComplete(t *testing.T) {
	t.Parallel()

	agg := NewTaskResultAggregator()
	agg.StartTask("task-1")

	result, ok := agg.Result("task-1")
	if !ok {
		t.Fatal("expected Result to find started task")
	}
	if result.Status != planpkg.TaskRunning {
		t.Fatalf("Status = %q, want %q", result.Status, planpkg.TaskRunning)
	}

	agg.CompleteTask(TaskExecutionResult{
		TaskID:  "task-1",
		Status:  planpkg.TaskCompleted,
		Summary: "done",
	})

	result, ok = agg.Result("task-1")
	if !ok {
		t.Fatal("expected Result to find completed task")
	}
	if result.Status != planpkg.TaskCompleted {
		t.Fatalf("Status = %q, want %q", result.Status, planpkg.TaskCompleted)
	}
}

func TestTaskResultAggregatorResultNotFound(t *testing.T) {
	t.Parallel()

	agg := NewTaskResultAggregator()
	_, ok := agg.Result("nonexistent")
	if ok {
		t.Fatal("expected Result to return false for nonexistent task")
	}
}

func TestTaskResultAggregatorFailTask(t *testing.T) {
	t.Parallel()

	agg := NewTaskResultAggregator()
	agg.FailTask(TaskExecutionResult{
		TaskID: "task-fail",
		Status: planpkg.TaskFailed,
		Error:  "compilation error",
	})

	result, ok := agg.Result("task-fail")
	if !ok {
		t.Fatal("expected Result to find failed task")
	}
	if result.Status != planpkg.TaskFailed {
		t.Fatalf("Status = %q, want %q", result.Status, planpkg.TaskFailed)
	}
	if result.Error != "compilation error" {
		t.Fatalf("Error = %q", result.Error)
	}
}

func TestTaskResultAggregatorCompletedResults(t *testing.T) {
	t.Parallel()

	agg := NewTaskResultAggregator()
	agg.CompleteTask(TaskExecutionResult{TaskID: "t1", Status: planpkg.TaskCompleted})
	agg.CompleteTask(TaskExecutionResult{TaskID: "t2", Status: planpkg.TaskCompleted})
	agg.FailTask(TaskExecutionResult{TaskID: "t3", Status: planpkg.TaskFailed})
	agg.StartTask("t4")

	completed := agg.CompletedResults()
	if len(completed) != 2 {
		t.Fatalf("CompletedResults() returned %d, want 2", len(completed))
	}
}

func TestTaskResultAggregatorDependencyResults(t *testing.T) {
	t.Parallel()

	agg := NewTaskResultAggregator()
	agg.CompleteTask(TaskExecutionResult{TaskID: "dep-1", Status: planpkg.TaskCompleted, Summary: "result A"})
	agg.CompleteTask(TaskExecutionResult{TaskID: "dep-2", Status: planpkg.TaskCompleted, Summary: "result B"})
	agg.FailTask(TaskExecutionResult{TaskID: "dep-3", Status: planpkg.TaskFailed})

	deps := agg.DependencyResults([]string{"dep-1", "dep-2", "dep-3", "dep-missing"})
	if len(deps) != 2 {
		t.Fatalf("DependencyResults returned %d, want 2 (only completed)", len(deps))
	}
}

func TestTaskResultAggregatorDependencyResultsEmpty(t *testing.T) {
	t.Parallel()

	agg := NewTaskResultAggregator()
	deps := agg.DependencyResults(nil)
	if deps != nil {
		t.Fatalf("DependencyResults(nil) should return nil, got %v", deps)
	}
}

func TestTaskResultAggregatorSnapshotNilPlan(t *testing.T) {
	t.Parallel()

	agg := NewTaskResultAggregator()
	snap := agg.Snapshot(nil)
	if snap.Total != 0 {
		t.Fatalf("Total = %d, want 0 for nil plan", snap.Total)
	}
	if snap.Goal != "" {
		t.Fatalf("Goal = %q, want empty for nil plan", snap.Goal)
	}
}

func TestNewTaskResultAggregatorForRunSeedsExecutionGraphOutcomes(t *testing.T) {
	t.Parallel()

	finishedAt := time.Now().UTC()
	run := &Run{
		ID:        "run-seeded",
		SessionID: "session-seeded",
		ExecutionGraph: &ExecutionGraph{
			RunID:          "run-seeded",
			SessionID:      "session-seeded",
			Scope:          "single_session",
			SingleSession:  true,
			SessionLocking: true,
			MergeStrategy:  MergeStrategyTaskOrder,
			Tasks: []ExecutionTask{{
				ID: "dep-1",
				LastOutcome: &TaskOutcome{
					TaskID:         "dep-1",
					Status:         planpkg.TaskCompleted,
					Attempt:        2,
					Summary:        "artifact ready",
					IdempotencyKey: "task:seeded",
					Artifacts: []resultmodel.ResultArtifact{{
						Kind: "artifact",
						URI:  "artifact://local/report",
					}},
					OutputBlocks: []resultmodel.ResultBlock{{
						Kind:    resultmodel.ResultBlockText,
						Content: "structured output",
					}},
					FinishedAt: finishedAt,
				},
			}},
		},
	}

	agg := NewTaskResultAggregatorForRun(run)
	result, ok := agg.Result("dep-1")
	if !ok {
		t.Fatal("expected seeded dependency result")
	}
	if result.Status != planpkg.TaskCompleted {
		t.Fatalf("Status = %q, want completed", result.Status)
	}
	if result.Attempt != 2 {
		t.Fatalf("Attempt = %d, want 2", result.Attempt)
	}
	if result.Outcome == nil || result.Outcome.IdempotencyKey != "task:seeded" {
		t.Fatalf("Outcome = %#v", result.Outcome)
	}
	if len(result.Artifacts) != 1 || result.Artifacts[0] != "artifact://local/report" {
		t.Fatalf("Artifacts = %#v", result.Artifacts)
	}
	if result.Output != "structured output" {
		t.Fatalf("Output = %q, want structured output", result.Output)
	}
}

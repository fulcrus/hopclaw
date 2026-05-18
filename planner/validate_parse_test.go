package planner

import (
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// Parse — basic success
// ---------------------------------------------------------------------------

func TestParseValidJSON(t *testing.T) {
	t.Parallel()
	plan, err := Parse(`{
		"goal": "test parse",
		"tasks": [
			{"id": "a", "kind": "research", "goal": "do research"}
		]
	}`)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if plan.Goal != "test parse" {
		t.Fatalf("Goal = %q", plan.Goal)
	}
	if len(plan.Tasks) != 1 {
		t.Fatalf("len(Tasks) = %d, want 1", len(plan.Tasks))
	}
}

func TestParseStripsCodeFence(t *testing.T) {
	t.Parallel()
	inputs := []string{
		"```json\n{\"goal\":\"test\",\"tasks\":[{\"id\":\"a\",\"kind\":\"execute\",\"goal\":\"do it\"}]}\n```",
		"```JSON\n{\"goal\":\"test\",\"tasks\":[{\"id\":\"a\",\"kind\":\"execute\",\"goal\":\"do it\"}]}\n```",
		"```\n{\"goal\":\"test\",\"tasks\":[{\"id\":\"a\",\"kind\":\"execute\",\"goal\":\"do it\"}]}\n```",
	}
	for _, input := range inputs {
		plan, err := Parse(input)
		if err != nil {
			t.Fatalf("Parse(%q) error = %v", input[:10], err)
		}
		if plan.Goal != "test" {
			t.Fatalf("Goal = %q", plan.Goal)
		}
	}
}

func TestParseExtractsJSONFromSurroundingText(t *testing.T) {
	t.Parallel()
	input := `Here is the plan:
{"goal":"embedded","tasks":[{"id":"a","kind":"execute","goal":"do it"}]}
And that's the plan.`
	plan, err := Parse(input)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if plan.Goal != "embedded" {
		t.Fatalf("Goal = %q", plan.Goal)
	}
}

func TestParseTrimsWhitespace(t *testing.T) {
	t.Parallel()
	input := `   {"goal":"spaced","tasks":[{"id":"a","kind":"execute","goal":"do it"}]}   `
	plan, err := Parse(input)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if plan.Goal != "spaced" {
		t.Fatalf("Goal = %q", plan.Goal)
	}
}

// ---------------------------------------------------------------------------
// Parse — error cases
// ---------------------------------------------------------------------------

func TestParseInvalidJSON(t *testing.T) {
	t.Parallel()
	_, err := Parse("not json at all")
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestParseEmptyString(t *testing.T) {
	t.Parallel()
	_, err := Parse("")
	if err == nil {
		t.Fatal("expected error for empty string")
	}
}

func TestParseMissingGoal(t *testing.T) {
	t.Parallel()
	_, err := Parse(`{"tasks":[{"id":"a","kind":"execute","goal":"do it"}]}`)
	if err == nil {
		t.Fatal("expected error for missing goal")
	}
	if !strings.Contains(err.Error(), "goal") {
		t.Fatalf("error = %v, expected mention of goal", err)
	}
}

func TestParseMissingTasks(t *testing.T) {
	t.Parallel()
	_, err := Parse(`{"goal":"no tasks"}`)
	if err == nil {
		t.Fatal("expected error for missing tasks")
	}
}

func TestParseEmptyTasks(t *testing.T) {
	t.Parallel()
	_, err := Parse(`{"goal":"empty tasks","tasks":[]}`)
	if err == nil {
		t.Fatal("expected error for empty tasks array")
	}
}

// ---------------------------------------------------------------------------
// Validate — defaults
// ---------------------------------------------------------------------------

func TestValidateNilPlan(t *testing.T) {
	t.Parallel()
	if err := Validate(nil); err == nil {
		t.Fatal("expected error for nil plan")
	}
}

func TestValidateDefaultsStrategy(t *testing.T) {
	t.Parallel()
	plan := &Plan{
		Goal: "defaults",
		Tasks: []Task{
			{ID: "a", Kind: TaskExecute, Goal: "do it"},
		},
	}
	if err := Validate(plan); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	if plan.Strategy != StrategySerial {
		t.Fatalf("Strategy = %q, want serial", plan.Strategy)
	}
}

func TestValidateDefaultsFailurePolicy(t *testing.T) {
	t.Parallel()
	plan := &Plan{
		Goal: "defaults",
		Tasks: []Task{
			{ID: "a", Kind: TaskExecute, Goal: "do it"},
		},
	}
	if err := Validate(plan); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	if plan.FailurePolicy != FailFast {
		t.Fatalf("FailurePolicy = %q, want fail_fast", plan.FailurePolicy)
	}
}

func TestValidateDefaultsTaskKind(t *testing.T) {
	t.Parallel()
	plan := &Plan{
		Goal: "defaults kind",
		Tasks: []Task{
			{ID: "a", Goal: "do it"},
		},
	}
	if err := Validate(plan); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	if plan.Tasks[0].Kind != TaskExecute {
		t.Fatalf("Kind = %q, want execute", plan.Tasks[0].Kind)
	}
}

func TestValidateDefaultsFinalTask(t *testing.T) {
	t.Parallel()
	plan := &Plan{
		Goal: "defaults final",
		Tasks: []Task{
			{ID: "first", Kind: TaskExecute, Goal: "A"},
			{ID: "last", Kind: TaskExecute, Goal: "B"},
		},
	}
	if err := Validate(plan); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	if plan.FinalTask != "last" {
		t.Fatalf("FinalTask = %q, want last", plan.FinalTask)
	}
}

func TestValidateAutoGeneratesTaskID(t *testing.T) {
	t.Parallel()
	plan := &Plan{
		Goal: "auto id",
		Tasks: []Task{
			{Kind: TaskExecute, Goal: "first"},
			{Kind: TaskExecute, Goal: "second"},
		},
	}
	if err := Validate(plan); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	if plan.Tasks[0].ID != "task_1" {
		t.Fatalf("Tasks[0].ID = %q, want task_1", plan.Tasks[0].ID)
	}
	if plan.Tasks[1].ID != "task_2" {
		t.Fatalf("Tasks[1].ID = %q, want task_2", plan.Tasks[1].ID)
	}
}

// ---------------------------------------------------------------------------
// Validate — rejection cases
// ---------------------------------------------------------------------------

func TestValidateRejectsEmptyGoal(t *testing.T) {
	t.Parallel()
	err := Validate(&Plan{
		Goal: "   ",
		Tasks: []Task{
			{ID: "a", Kind: TaskExecute, Goal: "do it"},
		},
	})
	if err == nil {
		t.Fatal("expected error for empty goal")
	}
}

func TestValidateRejectsInvalidStrategy(t *testing.T) {
	t.Parallel()
	err := Validate(&Plan{
		Goal:     "bad strategy",
		Strategy: "yolo",
		Tasks:    []Task{{ID: "a", Kind: TaskExecute, Goal: "do it"}},
	})
	if err == nil {
		t.Fatal("expected error for invalid strategy")
	}
	if !strings.Contains(err.Error(), "strategy") {
		t.Fatalf("error = %v, expected mention of strategy", err)
	}
}

func TestValidateRejectsInvalidTaskKind(t *testing.T) {
	t.Parallel()
	err := Validate(&Plan{
		Goal:  "bad kind",
		Tasks: []Task{{ID: "a", Kind: "magic", Goal: "do it"}},
	})
	if err == nil {
		t.Fatal("expected error for invalid task kind")
	}
}

func TestValidateRejectsInvalidTaskStatus(t *testing.T) {
	t.Parallel()
	err := Validate(&Plan{
		Goal:  "bad status",
		Tasks: []Task{{ID: "a", Kind: TaskExecute, Goal: "do it", Status: "superstatus"}},
	})
	if err == nil {
		t.Fatal("expected error for invalid task status")
	}
}

func TestValidateRejectsDuplicateTaskID(t *testing.T) {
	t.Parallel()
	err := Validate(&Plan{
		Goal: "dup",
		Tasks: []Task{
			{ID: "a", Kind: TaskExecute, Goal: "first"},
			{ID: "a", Kind: TaskExecute, Goal: "second"},
		},
	})
	if err == nil {
		t.Fatal("expected error for duplicate task id")
	}
	if !strings.Contains(err.Error(), "duplicate") {
		t.Fatalf("error = %v, expected mention of duplicate", err)
	}
}

func TestValidateRejectsUnknownFinalTask(t *testing.T) {
	t.Parallel()
	err := Validate(&Plan{
		Goal:      "bad final",
		FinalTask: "nonexistent",
		Tasks:     []Task{{ID: "a", Kind: TaskExecute, Goal: "do it"}},
	})
	if err == nil {
		t.Fatal("expected error for unknown final_task")
	}
}

func TestValidateRejectsUnknownActiveTask(t *testing.T) {
	t.Parallel()
	err := Validate(&Plan{
		Goal:       "bad active",
		ActiveTask: "nonexistent",
		Tasks:      []Task{{ID: "a", Kind: TaskExecute, Goal: "do it"}},
	})
	if err == nil {
		t.Fatal("expected error for unknown active_task")
	}
}

func TestValidateRejectsEmptyTaskGoal(t *testing.T) {
	t.Parallel()
	err := Validate(&Plan{
		Goal:  "no task goal",
		Tasks: []Task{{ID: "a", Kind: TaskExecute, Goal: "  "}},
	})
	if err == nil {
		t.Fatal("expected error for empty task goal")
	}
}

// ---------------------------------------------------------------------------
// Validate — whitespace trimming
// ---------------------------------------------------------------------------

func TestValidateTrimsGoalAndSummary(t *testing.T) {
	t.Parallel()
	plan := &Plan{
		Goal:    "  spaced goal  ",
		Summary: "  spaced summary  ",
		Tasks:   []Task{{ID: "a", Kind: TaskExecute, Goal: "do it"}},
	}
	if err := Validate(plan); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	if plan.Goal != "spaced goal" {
		t.Fatalf("Goal = %q", plan.Goal)
	}
	if plan.Summary != "spaced summary" {
		t.Fatalf("Summary = %q", plan.Summary)
	}
}

func TestValidateTrimsTaskFields(t *testing.T) {
	t.Parallel()
	plan := &Plan{
		Goal: "trim tasks",
		Tasks: []Task{
			{
				ID:            "  a  ",
				Kind:          TaskExecute,
				Goal:          "  my goal  ",
				Title:         "  title  ",
				ResultSummary: "  result  ",
				Error:         "  error  ",
			},
		},
	}
	if err := Validate(plan); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	if plan.Tasks[0].ID != "a" {
		t.Fatalf("ID = %q", plan.Tasks[0].ID)
	}
	if plan.Tasks[0].Goal != "my goal" {
		t.Fatalf("Goal = %q", plan.Tasks[0].Goal)
	}
	if plan.Tasks[0].Title != "title" {
		t.Fatalf("Title = %q", plan.Tasks[0].Title)
	}
}

// ---------------------------------------------------------------------------
// Validate — trimNonEmpty deduplication
// ---------------------------------------------------------------------------

func TestValidateDeduplicatesDependsOn(t *testing.T) {
	t.Parallel()
	plan := &Plan{
		Goal: "dedup deps",
		Tasks: []Task{
			{ID: "a", Kind: TaskExecute, Goal: "A"},
			{ID: "b", Kind: TaskExecute, Goal: "B", DependsOn: []string{"a", "a", "  a  "}},
		},
	}
	if err := Validate(plan); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	if len(plan.Tasks[1].DependsOn) != 1 {
		t.Fatalf("DependsOn = %v, want [a]", plan.Tasks[1].DependsOn)
	}
}

func TestValidateFiltersEmptyDependsOn(t *testing.T) {
	t.Parallel()
	plan := &Plan{
		Goal: "filter empty",
		Tasks: []Task{
			{ID: "a", Kind: TaskExecute, Goal: "A"},
			{ID: "b", Kind: TaskExecute, Goal: "B", DependsOn: []string{"", "  ", "a"}},
		},
	}
	if err := Validate(plan); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	if len(plan.Tasks[1].DependsOn) != 1 || plan.Tasks[1].DependsOn[0] != "a" {
		t.Fatalf("DependsOn = %v, want [a]", plan.Tasks[1].DependsOn)
	}
}

func TestValidateAllEmptyDependsOnBecomesNil(t *testing.T) {
	t.Parallel()
	plan := &Plan{
		Goal: "all empty",
		Tasks: []Task{
			{ID: "a", Kind: TaskExecute, Goal: "A", DependsOn: []string{"", "  "}},
		},
	}
	if err := Validate(plan); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	if plan.Tasks[0].DependsOn != nil {
		t.Fatalf("DependsOn = %v, want nil", plan.Tasks[0].DependsOn)
	}
}

func TestValidateNormalizesVerificationHints(t *testing.T) {
	t.Parallel()
	plan := &Plan{
		Goal: "normalize verification hints",
		Tasks: []Task{
			{
				ID:                "a",
				Kind:              TaskExecute,
				Goal:              "send the email",
				VerificationHints: []string{" email ", "", "spreadsheet", "email", "  spreadsheet  "},
			},
		},
	}
	if err := Validate(plan); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	if len(plan.Tasks[0].VerificationHints) != 2 {
		t.Fatalf("VerificationHints = %v, want 2 hints", plan.Tasks[0].VerificationHints)
	}
	if plan.Tasks[0].VerificationHints[0] != "email" || plan.Tasks[0].VerificationHints[1] != "spreadsheet" {
		t.Fatalf("VerificationHints = %v, want [email spreadsheet]", plan.Tasks[0].VerificationHints)
	}
}

// ---------------------------------------------------------------------------
// Validate — accepts all valid strategies
// ---------------------------------------------------------------------------

func TestValidateAcceptsAllStrategies(t *testing.T) {
	t.Parallel()
	for _, strategy := range []Strategy{StrategySerial, StrategyParallel, StrategyMixed} {
		plan := &Plan{
			Goal:     "test",
			Strategy: strategy,
			Tasks:    []Task{{ID: "a", Kind: TaskExecute, Goal: "do it"}},
		}
		if err := Validate(plan); err != nil {
			t.Fatalf("Validate() rejected strategy %q: %v", strategy, err)
		}
	}
}

// ---------------------------------------------------------------------------
// Validate — accepts all valid task states
// ---------------------------------------------------------------------------

func TestValidateAcceptsAllTaskStatuses(t *testing.T) {
	t.Parallel()
	for _, status := range []TaskStatus{TaskQueued, TaskRunning, TaskCompleted, TaskFailed, TaskCancelled, TaskSkipped} {
		plan := &Plan{
			Goal:  "test",
			Tasks: []Task{{ID: "a", Kind: TaskExecute, Goal: "do it", Status: status}},
		}
		if err := Validate(plan); err != nil {
			t.Fatalf("Validate() rejected status %q: %v", status, err)
		}
	}
}

// ---------------------------------------------------------------------------
// Validate — accepts all valid task kinds
// ---------------------------------------------------------------------------

func TestValidateAcceptsAllTaskKinds(t *testing.T) {
	t.Parallel()
	for _, kind := range []TaskKind{TaskResearch, TaskTranslate, TaskTransform, TaskWrite, TaskExecute, TaskReview, TaskDeliver} {
		plan := &Plan{
			Goal:  "test",
			Tasks: []Task{{ID: "a", Kind: kind, Goal: "do it"}},
		}
		if err := Validate(plan); err != nil {
			t.Fatalf("Validate() rejected kind %q: %v", kind, err)
		}
	}
}

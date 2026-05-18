package planner

import "testing"

func TestParseNormalizesSimplePlan(t *testing.T) {
	t.Parallel()

	plan, err := Parse(`{
		"goal": "search and summarize",
		"tasks": [
			{"id":"research","kind":"research","goal":"search sources"},
			{"id":"deliver","kind":"deliver","goal":"write final summary","depends_on":["research"]}
		]
	}`)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if plan.Strategy != StrategySerial {
		t.Fatalf("plan.Strategy = %q, want %q", plan.Strategy, StrategySerial)
	}
	if plan.FinalTask != "deliver" {
		t.Fatalf("plan.FinalTask = %q, want %q", plan.FinalTask, "deliver")
	}
}

func TestParseDefaultsFailurePolicy(t *testing.T) {
	t.Parallel()

	plan, err := Parse(`{
		"goal": "test failure policy default",
		"tasks": [{"id":"a","kind":"execute","goal":"do something"}]
	}`)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if plan.FailurePolicy != FailFast {
		t.Fatalf("plan.FailurePolicy = %q, want %q", plan.FailurePolicy, FailFast)
	}
}

func TestValidateAcceptsSkippedState(t *testing.T) {
	t.Parallel()

	err := Validate(&Plan{
		Goal: "plan with skipped task",
		Tasks: []Task{
			{ID: "a", Kind: TaskExecute, Goal: "do A", Status: TaskSkipped},
		},
	})
	if err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
}

func TestValidateRejectsInvalidFailurePolicy(t *testing.T) {
	t.Parallel()

	err := Validate(&Plan{
		Goal:          "bad policy",
		FailurePolicy: "yolo",
		Tasks:         []Task{{ID: "a", Kind: TaskExecute, Goal: "do A"}},
	})
	if err == nil {
		t.Fatal("expected validation error for invalid failure_policy")
	}
}

func TestReadyTasksReturnsDependencySatisfied(t *testing.T) {
	t.Parallel()

	plan := &Plan{
		Goal:     "test ready tasks",
		Strategy: StrategyMixed,
		Tasks: []Task{
			{ID: "a", Kind: TaskExecute, Goal: "A", Status: TaskCompleted},
			{ID: "b", Kind: TaskExecute, Goal: "B", Status: TaskQueued},
			{ID: "c", Kind: TaskExecute, Goal: "C", Status: TaskQueued, DependsOn: []string{"a"}},
			{ID: "d", Kind: TaskExecute, Goal: "D", Status: TaskQueued, DependsOn: []string{"b"}},
		},
	}
	ready := ReadyTasks(plan)
	ids := make(map[string]bool)
	for _, t := range ready {
		ids[t.ID] = true
	}
	// b has no deps (queued, deps satisfied), c depends on a (completed) -> ready.
	// d depends on b (queued) -> NOT ready.
	if !ids["b"] {
		t.Fatal("task b should be ready (no deps)")
	}
	if !ids["c"] {
		t.Fatal("task c should be ready (dep a completed)")
	}
	if ids["d"] {
		t.Fatal("task d should NOT be ready (dep b not completed)")
	}
}

func TestMarkRunningUpdatesRunningTasks(t *testing.T) {
	t.Parallel()

	plan := &Plan{
		Goal: "test mark running",
		Tasks: []Task{
			{ID: "a", Kind: TaskExecute, Goal: "A", Status: TaskQueued},
			{ID: "b", Kind: TaskExecute, Goal: "B", Status: TaskQueued},
		},
	}
	MarkRunning(plan, "a", "b")
	if len(plan.RunningTasks) != 2 {
		t.Fatalf("RunningTasks = %v, want [a, b]", plan.RunningTasks)
	}
	if plan.Tasks[0].Status != TaskRunning || plan.Tasks[1].Status != TaskRunning {
		t.Fatal("both tasks should be running")
	}
	if plan.ActiveTask != "a" {
		t.Fatalf("ActiveTask = %q, want a", plan.ActiveTask)
	}
}

func TestSkipDependentsOfPropagates(t *testing.T) {
	t.Parallel()

	plan := &Plan{
		Goal: "test skip propagation",
		Tasks: []Task{
			{ID: "a", Kind: TaskExecute, Goal: "A", Status: TaskFailed},
			{ID: "b", Kind: TaskExecute, Goal: "B", Status: TaskQueued, DependsOn: []string{"a"}},
			{ID: "c", Kind: TaskExecute, Goal: "C", Status: TaskQueued, DependsOn: []string{"b"}},
			{ID: "d", Kind: TaskExecute, Goal: "D", Status: TaskQueued}, // no deps, not skipped
		},
	}
	SkipDependentsOf(plan, "a")
	if plan.Tasks[1].Status != TaskSkipped {
		t.Fatalf("task b state = %q, want skipped", plan.Tasks[1].Status)
	}
	if plan.Tasks[2].Status != TaskSkipped {
		t.Fatalf("task c state = %q, want skipped (transitive)", plan.Tasks[2].Status)
	}
	if plan.Tasks[3].Status != TaskQueued {
		t.Fatalf("task d state = %q, want queued (independent)", plan.Tasks[3].Status)
	}
}

func TestTerminalChecks(t *testing.T) {
	t.Parallel()

	plan := &Plan{
		Goal: "test terminal",
		Tasks: []Task{
			{ID: "a", Kind: TaskExecute, Goal: "A", Status: TaskCompleted},
			{ID: "b", Kind: TaskExecute, Goal: "B", Status: TaskSkipped},
			{ID: "c", Kind: TaskExecute, Goal: "C", Status: TaskFailed},
		},
	}
	if !Terminal(plan) {
		t.Fatal("plan should be terminal (all tasks in terminal states)")
	}
	if IsDone(plan) {
		t.Fatal("plan should NOT be done (not all completed)")
	}
	if !HasFailures(plan) {
		t.Fatal("plan should have failures")
	}
}

func TestCancelRunningCancelsAllAndClearsTracking(t *testing.T) {
	t.Parallel()

	plan := &Plan{
		Goal: "test cancel",
		Tasks: []Task{
			{ID: "a", Kind: TaskExecute, Goal: "A", Status: TaskRunning},
			{ID: "b", Kind: TaskExecute, Goal: "B", Status: TaskRunning},
			{ID: "c", Kind: TaskExecute, Goal: "C", Status: TaskQueued},
		},
		RunningTasks: []string{"a", "b"},
		ActiveTask:   "a",
	}
	CancelRunning(plan, "user cancelled")
	if plan.Tasks[0].Status != TaskCancelled || plan.Tasks[1].Status != TaskCancelled {
		t.Fatal("running tasks should be cancelled")
	}
	if plan.Tasks[2].Status != TaskQueued {
		t.Fatal("queued task should remain queued")
	}
	if len(plan.RunningTasks) != 0 {
		t.Fatalf("RunningTasks should be empty, got %v", plan.RunningTasks)
	}
	if plan.ActiveTask != "" {
		t.Fatalf("ActiveTask should be empty, got %q", plan.ActiveTask)
	}
}

func TestValidateRejectsUnknownDependency(t *testing.T) {
	t.Parallel()

	err := Validate(&Plan{
		Goal: "bad plan",
		Tasks: []Task{{
			ID:        "deliver",
			Kind:      TaskDeliver,
			Goal:      "deliver",
			DependsOn: []string{"missing"},
		}},
	})
	if err == nil {
		t.Fatal("expected validation error")
	}
}

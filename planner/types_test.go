package planner

import (
	"encoding/json"
	"testing"
)

// ---------------------------------------------------------------------------
// Plan JSON roundtrip
// ---------------------------------------------------------------------------

func TestPlanJSONRoundtrip(t *testing.T) {
	t.Parallel()

	original := Plan{
		Goal:          "test roundtrip",
		Summary:       "summary text",
		Strategy:      StrategyParallel,
		FailurePolicy: ContinueOnError,
		Tasks: []Task{
			{
				ID:                   "task_1",
				Title:                "Research",
				Kind:                 TaskResearch,
				Goal:                 "search for data",
				DependsOn:            nil,
				Outputs:              []string{"data.json"},
				RequiredCapabilities: []string{"web_search"},
				VerificationHints:    []string{"spreadsheet"},
				Status:               TaskQueued,
			},
			{
				ID:                "task_2",
				Title:             "Deliver",
				Kind:              TaskDeliver,
				Goal:              "produce final output",
				DependsOn:         []string{"task_1"},
				Outputs:           []string{"result.txt"},
				VerificationHints: []string{"email"},
				Status:            TaskCompleted,
				ResultSummary:     "done",
			},
		},
		FinalTask:    "task_2",
		ActiveTask:   "task_1",
		RunningTasks: []string{"task_1"},
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("json.Marshal error = %v", err)
	}

	var decoded Plan
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("json.Unmarshal error = %v", err)
	}

	if decoded.Goal != original.Goal {
		t.Fatalf("Goal = %q, want %q", decoded.Goal, original.Goal)
	}
	if decoded.Summary != original.Summary {
		t.Fatalf("Summary = %q, want %q", decoded.Summary, original.Summary)
	}
	if decoded.Strategy != original.Strategy {
		t.Fatalf("Strategy = %q, want %q", decoded.Strategy, original.Strategy)
	}
	if decoded.FailurePolicy != original.FailurePolicy {
		t.Fatalf("FailurePolicy = %q, want %q", decoded.FailurePolicy, original.FailurePolicy)
	}
	if decoded.FinalTask != original.FinalTask {
		t.Fatalf("FinalTask = %q, want %q", decoded.FinalTask, original.FinalTask)
	}
	if decoded.ActiveTask != original.ActiveTask {
		t.Fatalf("ActiveTask = %q, want %q", decoded.ActiveTask, original.ActiveTask)
	}
	if len(decoded.Tasks) != len(original.Tasks) {
		t.Fatalf("len(Tasks) = %d, want %d", len(decoded.Tasks), len(original.Tasks))
	}
	if len(decoded.RunningTasks) != 1 || decoded.RunningTasks[0] != "task_1" {
		t.Fatalf("RunningTasks = %v, want [task_1]", decoded.RunningTasks)
	}
	if len(decoded.Tasks[0].VerificationHints) != 1 || decoded.Tasks[0].VerificationHints[0] != "spreadsheet" {
		t.Fatalf("Tasks[0].VerificationHints = %v, want [spreadsheet]", decoded.Tasks[0].VerificationHints)
	}
	if len(decoded.Tasks[1].VerificationHints) != 1 || decoded.Tasks[1].VerificationHints[0] != "email" {
		t.Fatalf("Tasks[1].VerificationHints = %v, want [email]", decoded.Tasks[1].VerificationHints)
	}
}

// ---------------------------------------------------------------------------
// Task JSON roundtrip
// ---------------------------------------------------------------------------

func TestTaskJSONRoundtrip(t *testing.T) {
	t.Parallel()

	original := Task{
		ID:                   "t1",
		Title:                "My Task",
		Kind:                 TaskWrite,
		Goal:                 "write the file",
		DependsOn:            []string{"t0"},
		Outputs:              []string{"output.txt"},
		RequiredCapabilities: []string{"fs_write"},
		VerificationHints:    []string{"document"},
		Status:               TaskRunning,
		ResultSummary:        "in progress",
		Error:                "timeout",
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("json.Marshal error = %v", err)
	}

	var decoded Task
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("json.Unmarshal error = %v", err)
	}

	if decoded.ID != original.ID {
		t.Fatalf("ID = %q, want %q", decoded.ID, original.ID)
	}
	if decoded.Title != original.Title {
		t.Fatalf("Title = %q, want %q", decoded.Title, original.Title)
	}
	if decoded.Kind != original.Kind {
		t.Fatalf("Kind = %q, want %q", decoded.Kind, original.Kind)
	}
	if decoded.Goal != original.Goal {
		t.Fatalf("Goal = %q, want %q", decoded.Goal, original.Goal)
	}
	if decoded.Status != original.Status {
		t.Fatalf("State = %q, want %q", decoded.Status, original.Status)
	}
	if decoded.ResultSummary != original.ResultSummary {
		t.Fatalf("ResultSummary = %q, want %q", decoded.ResultSummary, original.ResultSummary)
	}
	if decoded.Error != original.Error {
		t.Fatalf("Error = %q, want %q", decoded.Error, original.Error)
	}
	if len(decoded.DependsOn) != 1 || decoded.DependsOn[0] != "t0" {
		t.Fatalf("DependsOn = %v, want [t0]", decoded.DependsOn)
	}
	if len(decoded.Outputs) != 1 || decoded.Outputs[0] != "output.txt" {
		t.Fatalf("Outputs = %v, want [output.txt]", decoded.Outputs)
	}
	if len(decoded.RequiredCapabilities) != 1 || decoded.RequiredCapabilities[0] != "fs_write" {
		t.Fatalf("RequiredCapabilities = %v, want [fs_write]", decoded.RequiredCapabilities)
	}
	if len(decoded.VerificationHints) != 1 || decoded.VerificationHints[0] != "document" {
		t.Fatalf("VerificationHints = %v, want [document]", decoded.VerificationHints)
	}
}

// ---------------------------------------------------------------------------
// Omitempty behavior
// ---------------------------------------------------------------------------

func TestPlanOmitemptyFields(t *testing.T) {
	t.Parallel()

	plan := Plan{
		Goal: "minimal",
		Tasks: []Task{
			{ID: "a", Kind: TaskExecute, Goal: "do it"},
		},
	}
	data, err := json.Marshal(plan)
	if err != nil {
		t.Fatalf("json.Marshal error = %v", err)
	}
	raw := make(map[string]any)
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("json.Unmarshal error = %v", err)
	}
	// These omitempty fields should not appear when empty.
	for _, key := range []string{"summary", "strategy", "failure_policy", "final_task", "active_task", "running_tasks"} {
		if _, ok := raw[key]; ok {
			t.Fatalf("expected key %q to be omitted", key)
		}
	}
	// "goal" and "tasks" should always be present.
	if _, ok := raw["goal"]; !ok {
		t.Fatal("expected key 'goal' to be present")
	}
	if _, ok := raw["tasks"]; !ok {
		t.Fatal("expected key 'tasks' to be present")
	}
}

func TestTaskOmitemptyFields(t *testing.T) {
	t.Parallel()

	task := Task{
		ID:   "t1",
		Kind: TaskExecute,
		Goal: "do it",
	}
	data, err := json.Marshal(task)
	if err != nil {
		t.Fatalf("json.Marshal error = %v", err)
	}
	raw := make(map[string]any)
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("json.Unmarshal error = %v", err)
	}
	for _, key := range []string{"title", "depends_on", "outputs", "required_capabilities", "verification_hints", "status", "result_summary", "error"} {
		if _, ok := raw[key]; ok {
			t.Fatalf("expected key %q to be omitted", key)
		}
	}
}

// ---------------------------------------------------------------------------
// Context JSON roundtrip
// ---------------------------------------------------------------------------

func TestContextJSONRoundtrip(t *testing.T) {
	t.Parallel()

	original := Context{
		LatestMessage:  "What is the weather?",
		SessionSummary: "User asked about weather",
		RecentMessages: []Message{
			{Role: "user", Content: "Hello"},
			{Role: "assistant", Content: "Hi"},
		},
		AvailableTools: []string{"web_search", "calculator"},
		Delegation: &Delegation{
			Goal:                "Split research and synthesis",
			AllowedDomains:      []string{"search", "text"},
			SideEffectClass:     "read",
			MaxTurns:            3,
			MaxBudgetTokens:     2000,
			VerificationPlanRef: "task_contract:visible_result",
		},
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("json.Marshal error = %v", err)
	}

	var decoded Context
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("json.Unmarshal error = %v", err)
	}

	if decoded.LatestMessage != original.LatestMessage {
		t.Fatalf("LatestMessage = %q", decoded.LatestMessage)
	}
	if decoded.SessionSummary != original.SessionSummary {
		t.Fatalf("SessionSummary = %q", decoded.SessionSummary)
	}
	if len(decoded.RecentMessages) != 2 {
		t.Fatalf("len(RecentMessages) = %d, want 2", len(decoded.RecentMessages))
	}
	if decoded.RecentMessages[0].Role != "user" {
		t.Fatalf("RecentMessages[0].Role = %q", decoded.RecentMessages[0].Role)
	}
	if len(decoded.AvailableTools) != 2 {
		t.Fatalf("len(AvailableTools) = %d, want 2", len(decoded.AvailableTools))
	}
	if decoded.Delegation == nil {
		t.Fatal("Delegation = nil, want delegation contract")
	}
	if decoded.Delegation.SideEffectClass != "read" {
		t.Fatalf("Delegation.SideEffectClass = %q", decoded.Delegation.SideEffectClass)
	}
}

// ---------------------------------------------------------------------------
// Strategy and TaskStatus constants
// ---------------------------------------------------------------------------

func TestStrategyConstants(t *testing.T) {
	t.Parallel()
	strategies := []Strategy{StrategySerial, StrategyParallel, StrategyMixed}
	expected := []string{"serial", "parallel", "mixed"}
	for i, s := range strategies {
		if string(s) != expected[i] {
			t.Fatalf("Strategy %d = %q, want %q", i, s, expected[i])
		}
	}
}

func TestFailurePolicyConstants(t *testing.T) {
	t.Parallel()
	if string(FailFast) != "fail_fast" {
		t.Fatalf("FailFast = %q", FailFast)
	}
	if string(ContinueOnError) != "continue_on_error" {
		t.Fatalf("ContinueOnError = %q", ContinueOnError)
	}
}

func TestTaskStatusConstants(t *testing.T) {
	t.Parallel()
	statuses := []TaskStatus{TaskQueued, TaskRunning, TaskCompleted, TaskFailed, TaskCancelled, TaskSkipped}
	expected := []string{"queued", "running", "completed", "failed", "cancelled", "skipped"}
	for i, s := range statuses {
		if string(s) != expected[i] {
			t.Fatalf("TaskStatus %d = %q, want %q", i, s, expected[i])
		}
	}
}

func TestTaskKindConstants(t *testing.T) {
	t.Parallel()
	kinds := []TaskKind{TaskResearch, TaskTranslate, TaskTransform, TaskWrite, TaskExecute, TaskReview, TaskDeliver}
	expected := []string{"research", "translate", "transform", "write", "execute", "review", "deliver"}
	for i, k := range kinds {
		if string(k) != expected[i] {
			t.Fatalf("TaskKind %d = %q, want %q", i, k, expected[i])
		}
	}
}

// ---------------------------------------------------------------------------
// JSON tag names
// ---------------------------------------------------------------------------

func TestPlanJSONFieldNames(t *testing.T) {
	t.Parallel()

	plan := Plan{
		Goal:          "test fields",
		Summary:       "sum",
		Strategy:      StrategySerial,
		FailurePolicy: FailFast,
		Tasks: []Task{
			{ID: "a", Kind: TaskExecute, Goal: "A", Status: TaskRunning, ResultSummary: "r", Error: "e",
				DependsOn: []string{"dep"}, Outputs: []string{"out"}, RequiredCapabilities: []string{"cap"}, VerificationHints: []string{"watch"}},
		},
		FinalTask:    "a",
		ActiveTask:   "a",
		RunningTasks: []string{"a"},
	}
	data, err := json.Marshal(plan)
	if err != nil {
		t.Fatalf("json.Marshal error = %v", err)
	}
	raw := make(map[string]any)
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("json.Unmarshal error = %v", err)
	}

	// Verify snake_case JSON tags.
	requiredKeys := []string{"goal", "summary", "strategy", "failure_policy", "tasks", "final_task", "active_task", "running_tasks"}
	for _, key := range requiredKeys {
		if _, ok := raw[key]; !ok {
			t.Fatalf("expected JSON key %q to be present", key)
		}
	}

	// Check task field names.
	taskList, ok := raw["tasks"].([]any)
	if !ok || len(taskList) == 0 {
		t.Fatal("expected tasks array")
	}
	taskMap, ok := taskList[0].(map[string]any)
	if !ok {
		t.Fatal("expected task to be a map")
	}
	taskKeys := []string{"id", "kind", "goal", "depends_on", "outputs", "required_capabilities", "verification_hints", "status", "result_summary", "error"}
	for _, key := range taskKeys {
		if _, ok := taskMap[key]; !ok {
			t.Fatalf("expected task JSON key %q to be present", key)
		}
	}
}

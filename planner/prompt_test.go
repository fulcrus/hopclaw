package planner

import (
	"encoding/json"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// SystemPrompt
// ---------------------------------------------------------------------------

func TestSystemPromptNotEmpty(t *testing.T) {
	t.Parallel()
	prompt := SystemPrompt()
	if prompt == "" {
		t.Fatal("SystemPrompt() returned empty string")
	}
}

func TestSystemPromptContainsKeyInstructions(t *testing.T) {
	t.Parallel()
	prompt := SystemPrompt()
	keywords := []string{"planner", "JSON", "goal", "strategy", "tasks", "verification_hints", "delegation_contract", "suggested_domains", "evidence_requirements", "pinned_facts", "recalled_context"}
	for _, kw := range keywords {
		if !strings.Contains(strings.ToLower(prompt), strings.ToLower(kw)) {
			t.Fatalf("SystemPrompt() missing keyword %q", kw)
		}
	}
}

func TestSystemPromptIsIdempotent(t *testing.T) {
	t.Parallel()
	a := SystemPrompt()
	b := SystemPrompt()
	if a != b {
		t.Fatal("SystemPrompt() should return the same string on each call")
	}
}

// ---------------------------------------------------------------------------
// BuildPayload
// ---------------------------------------------------------------------------

func TestBuildPayloadEmptyContext(t *testing.T) {
	t.Parallel()
	payload, err := BuildPayload(Context{})
	if err != nil {
		t.Fatalf("BuildPayload() error = %v", err)
	}
	if payload == "" {
		t.Fatal("BuildPayload() returned empty string")
	}
	// Verify it is valid JSON.
	var raw map[string]any
	if err := json.Unmarshal([]byte(payload), &raw); err != nil {
		t.Fatalf("BuildPayload() produced invalid JSON: %v", err)
	}
}

func TestBuildPayloadWithFullContext(t *testing.T) {
	t.Parallel()
	ctx := Context{
		LatestMessage:  "Search for Go tutorials",
		SessionSummary: "User wants to learn Go",
		RecentMessages: []Message{
			{Role: "user", Content: "Hello"},
			{Role: "assistant", Content: "Hi there"},
		},
		AvailableTools:  []string{"web_search", "code_exec"},
		PinnedFacts:     []string{"project=hopclaw"},
		SessionState:    "<session_state>\n- target: docs/out.md\n</session_state>",
		RecalledContext: "<recalled_context source=\"segment seg-1\">\nSummary: Earlier discussion.\n</recalled_context>",
	}
	payload, err := BuildPayload(ctx)
	if err != nil {
		t.Fatalf("BuildPayload() error = %v", err)
	}
	// Verify all fields present.
	var decoded Context
	if err := json.Unmarshal([]byte(payload), &decoded); err != nil {
		t.Fatalf("BuildPayload() JSON unmarshal error = %v", err)
	}
	if decoded.LatestMessage != ctx.LatestMessage {
		t.Fatalf("LatestMessage = %q", decoded.LatestMessage)
	}
	if decoded.SessionSummary != ctx.SessionSummary {
		t.Fatalf("SessionSummary = %q", decoded.SessionSummary)
	}
	if len(decoded.RecentMessages) != 2 {
		t.Fatalf("len(RecentMessages) = %d, want 2", len(decoded.RecentMessages))
	}
	if len(decoded.AvailableTools) != 2 {
		t.Fatalf("len(AvailableTools) = %d, want 2", len(decoded.AvailableTools))
	}
	if len(decoded.PinnedFacts) != 1 || decoded.PinnedFacts[0] != "project=hopclaw" {
		t.Fatalf("PinnedFacts = %v", decoded.PinnedFacts)
	}
	if decoded.SessionState != ctx.SessionState {
		t.Fatalf("SessionState = %q", decoded.SessionState)
	}
	if decoded.RecalledContext != ctx.RecalledContext {
		t.Fatalf("RecalledContext = %q", decoded.RecalledContext)
	}
}

func TestBuildPayloadPreservesSpecialCharacters(t *testing.T) {
	t.Parallel()
	ctx := Context{
		LatestMessage: `search for "Go & Rust" <tutorials>`,
	}
	payload, err := BuildPayload(ctx)
	if err != nil {
		t.Fatalf("BuildPayload() error = %v", err)
	}
	var decoded Context
	if err := json.Unmarshal([]byte(payload), &decoded); err != nil {
		t.Fatalf("unmarshal error = %v", err)
	}
	if decoded.LatestMessage != ctx.LatestMessage {
		t.Fatalf("LatestMessage = %q, want %q", decoded.LatestMessage, ctx.LatestMessage)
	}
}

// ---------------------------------------------------------------------------
// TrivialPlan
// ---------------------------------------------------------------------------

func TestTrivialPlanBasicFields(t *testing.T) {
	t.Parallel()
	plan := TrivialPlan("Build and test")
	if plan == nil {
		t.Fatal("TrivialPlan returned nil")
	}
	if plan.Goal != "Build and test" {
		t.Fatalf("Goal = %q", plan.Goal)
	}
	if plan.Summary != "Build and test" {
		t.Fatalf("Summary = %q", plan.Summary)
	}
	if plan.Strategy != StrategySerial {
		t.Fatalf("Strategy = %q, want serial", plan.Strategy)
	}
	if len(plan.Tasks) != 1 {
		t.Fatalf("len(Tasks) = %d, want 1", len(plan.Tasks))
	}
	if plan.Tasks[0].ID != "task_1" {
		t.Fatalf("Tasks[0].ID = %q", plan.Tasks[0].ID)
	}
	if plan.Tasks[0].Kind != TaskExecute {
		t.Fatalf("Tasks[0].Kind = %q", plan.Tasks[0].Kind)
	}
	if plan.Tasks[0].Goal != "Build and test" {
		t.Fatalf("Tasks[0].Goal = %q", plan.Tasks[0].Goal)
	}
	if plan.FinalTask != "task_1" {
		t.Fatalf("FinalTask = %q", plan.FinalTask)
	}
}

func TestTrivialPlanTrimsGoal(t *testing.T) {
	t.Parallel()
	plan := TrivialPlan("  trim me  ")
	if plan.Goal != "trim me" {
		t.Fatalf("Goal = %q, want trimmed", plan.Goal)
	}
}

func TestTrivialPlanEmptyGoalGetsDefault(t *testing.T) {
	t.Parallel()
	plan := TrivialPlan("")
	if plan.Goal == "" {
		t.Fatal("expected non-empty default goal")
	}
	if plan.Goal != "Continue the current task." {
		t.Fatalf("Goal = %q, expected default", plan.Goal)
	}
}

func TestTrivialPlanWhitespaceOnlyGoalGetsDefault(t *testing.T) {
	t.Parallel()
	plan := TrivialPlan("   ")
	if plan.Goal == "" || plan.Goal == "   " {
		t.Fatalf("Goal = %q, expected default for whitespace-only", plan.Goal)
	}
}

func TestTrivialPlanHasOutputs(t *testing.T) {
	t.Parallel()
	plan := TrivialPlan("test")
	if len(plan.Tasks[0].Outputs) == 0 {
		t.Fatal("expected task to have outputs")
	}
}

func TestTrivialPlanPassesValidation(t *testing.T) {
	t.Parallel()
	plan := TrivialPlan("validate me")
	if err := Validate(plan); err != nil {
		t.Fatalf("TrivialPlan should pass validation, got %v", err)
	}
}

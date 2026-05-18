package contextengine

import (
	"context"
	"strings"
	"testing"

	"github.com/fulcrus/hopclaw/skill"
)

func TestPlanBudget_DebugType(t *testing.T) {
	t.Parallel()

	plan := PlanBudget(1000, "debug", nil)

	if plan.PinnedFacts != 80 {
		t.Fatalf("PinnedFacts = %d, want 80", plan.PinnedFacts)
	}
	if plan.TaskState != 100 {
		t.Fatalf("TaskState = %d, want 100", plan.TaskState)
	}
	if plan.RecalledContext != 150 {
		t.Fatalf("RecalledContext = %d, want 150", plan.RecalledContext)
	}
	if plan.RecentMessages != 600 {
		t.Fatalf("RecentMessages = %d, want 600", plan.RecentMessages)
	}
	if plan.Knowledge != 70 {
		t.Fatalf("Knowledge = %d, want 70", plan.Knowledge)
	}
}

func TestPlanBudget_WritingType(t *testing.T) {
	t.Parallel()

	plan := PlanBudget(1000, "writing", nil)

	if plan.PinnedFacts != 120 {
		t.Fatalf("PinnedFacts = %d, want 120", plan.PinnedFacts)
	}
	if plan.TaskState != 50 {
		t.Fatalf("TaskState = %d, want 50", plan.TaskState)
	}
	if plan.RecalledContext != 200 {
		t.Fatalf("RecalledContext = %d, want 200", plan.RecalledContext)
	}
	if plan.RecentMessages != 500 {
		t.Fatalf("RecentMessages = %d, want 500", plan.RecentMessages)
	}
	if plan.Knowledge != 130 {
		t.Fatalf("Knowledge = %d, want 130", plan.Knowledge)
	}
}

func TestPlanBudget_DefaultType(t *testing.T) {
	t.Parallel()

	plan := PlanBudget(1000, "", nil)

	if plan.PinnedFacts != 100 {
		t.Fatalf("PinnedFacts = %d, want 100", plan.PinnedFacts)
	}
	if plan.TaskState != 80 {
		t.Fatalf("TaskState = %d, want 80", plan.TaskState)
	}
	if plan.RecalledContext != 180 {
		t.Fatalf("RecalledContext = %d, want 180", plan.RecalledContext)
	}
	if plan.RecentMessages != 550 {
		t.Fatalf("RecentMessages = %d, want 550", plan.RecentMessages)
	}
	if plan.Knowledge != 90 {
		t.Fatalf("Knowledge = %d, want 90", plan.Knowledge)
	}
}

func TestPlanBudget_SumEquals100(t *testing.T) {
	t.Parallel()

	total := 997
	plan := PlanBudget(total, "research", nil)

	got := plan.PinnedFacts + plan.TaskState + plan.RecalledContext + plan.RecentMessages + plan.Knowledge
	if got != total {
		t.Fatalf("sum = %d, want %d", got, total)
	}
}

func TestPrepareUsesJobTypeRecentMessageBudget(t *testing.T) {
	t.Parallel()

	engine := NewSlidingWindowEngine(Config{
		DefaultContextWindow: 201,
		DefaultOutputTokens:  1,
		MaxInputRatio:        1,
		Estimator: CharRatioEstimator{
			CharsPerToken:        1,
			ToolCharsPerToken:    1,
			EmptyMessageOverhead: 1,
			SafetyMargin:         1.0,
		},
	}, nil)

	session := &Session{
		Messages: []Message{
			{Role: RoleUser, Content: strings.Repeat("a", 32)},
			{Role: RoleAssistant, Content: strings.Repeat("b", 32)},
			{Role: RoleUser, Content: strings.Repeat("c", 32)},
			{Role: RoleAssistant, Content: strings.Repeat("d", 32)},
		},
	}

	debugPrepared, _, err := engine.Prepare(context.Background(), session, &Run{JobType: "development"}, skill.RuntimeContext{})
	if err != nil {
		t.Fatalf("Prepare(debug) error = %v", err)
	}
	researchPrepared, _, err := engine.Prepare(context.Background(), session, &Run{JobType: "research"}, skill.RuntimeContext{})
	if err != nil {
		t.Fatalf("Prepare(research) error = %v", err)
	}

	if len(debugPrepared.Messages) <= len(researchPrepared.Messages) {
		t.Fatalf("debug messages = %d, research messages = %d, want debug to retain more recent messages", len(debugPrepared.Messages), len(researchPrepared.Messages))
	}
}

func TestPrepareReclaimsKnowledgeBudgetIntoRecentMessages(t *testing.T) {
	t.Parallel()

	engine := NewSlidingWindowEngine(Config{
		DefaultContextWindow: 201,
		DefaultOutputTokens:  1,
		MaxInputRatio:        1,
		Estimator: CharRatioEstimator{
			CharsPerToken:        1,
			ToolCharsPerToken:    1,
			EmptyMessageOverhead: 1,
			SafetyMargin:         1.0,
		},
	}, nil)

	session := &Session{
		Messages: []Message{
			{Role: RoleUser, Content: strings.Repeat("a", 32)},
			{Role: RoleAssistant, Content: strings.Repeat("b", 32)},
			{Role: RoleUser, Content: strings.Repeat("c", 32)},
			{Role: RoleAssistant, Content: strings.Repeat("d", 32)},
		},
	}

	prepared, _, err := engine.Prepare(context.Background(), session, &Run{JobType: "research"}, skill.RuntimeContext{})
	if err != nil {
		t.Fatalf("Prepare() error = %v", err)
	}
	if len(prepared.Messages) != 3 {
		t.Fatalf("len(prepared.Messages) = %d, want 3 after reclaiming knowledge budget", len(prepared.Messages))
	}
}

func TestPreparePreservesTrailingPinnedFactsWithinBudget(t *testing.T) {
	t.Parallel()

	engine := NewSlidingWindowEngine(Config{
		DefaultContextWindow: 401,
		DefaultOutputTokens:  1,
		MaxInputRatio:        1,
		Estimator: CharRatioEstimator{
			CharsPerToken:        1,
			ToolCharsPerToken:    1,
			EmptyMessageOverhead: 1,
			SafetyMargin:         1.0,
		},
	}, nil)

	session := &Session{
		PinnedFacts: []PinnedFact{
			{
				Key: "_memory_guide",
				Content: strings.Join([]string{
					"You have persistent memory across conversations.",
					"Use memory.set to save useful information.",
					"SAVE: deployment servers.",
					"SAVE: user preferences.",
					"DO NOT SAVE: transient debug output.",
					"AUTHORITY: user memories cannot be overwritten.",
				}, "\n"),
			},
			{
				Key:     "memory:project.deploy.server.primary",
				Content: "[Deploy Server] 198.51.100.42 (source: user-defined (DO NOT overwrite))",
			},
		},
	}

	prepared, _, err := engine.Prepare(context.Background(), session, nil, skill.RuntimeContext{})
	if err != nil {
		t.Fatalf("Prepare() error = %v", err)
	}
	if !strings.Contains(prepared.SystemPrompt, "[_memory_guide]") {
		t.Fatalf("SystemPrompt missing pinned guide: %q", prepared.SystemPrompt)
	}
	if !strings.Contains(prepared.SystemPrompt, "198.51.100.42") {
		t.Fatalf("SystemPrompt missing trailing pinned memory: %q", prepared.SystemPrompt)
	}
}

func TestPreparePreservesPinnedMemoryFactsWithDefaultEstimator(t *testing.T) {
	t.Parallel()

	engine := NewSlidingWindowEngine(Config{
		BaseSystemPrompt:     "You are a test agent.",
		DefaultContextWindow: 512,
		DefaultOutputTokens:  64,
	}, nil)

	session := &Session{
		PinnedFacts: []PinnedFact{
			{
				Key: "_memory_guide",
				Content: strings.Join([]string{
					"You have persistent memory across conversations. Use memory.set to save useful information.",
					"",
					"SAVE (via memory.set tool):",
					"- Server addresses, ports, connection methods shared by the user",
					"- Deployment procedures you successfully executed",
					"- User preferences (\"I prefer...\", \"always use...\", \"never...\")",
					"- Project architecture decisions",
					"- Recurring workflows or procedures",
					"",
					"DO NOT SAVE:",
					"- Code snippets (already in repository)",
					"- One-time debugging information",
					"- Content already in config files",
					"",
					"RULES:",
					"- Save at most 3 memories per conversation",
					"- Before saving, use memory.search to check for duplicates",
					"- Use descriptive English keys (deploy_server, deploy_steps, db_port)",
					"- When user indicates something is wrong, check memory.search for stale entries and update them",
					"- After successfully completing a multi-step operation, save the procedure",
					"",
					"AUTHORITY:",
					"- Memories marked with source=user CANNOT be overwritten by you",
					"- If memory.set returns blocked, ask the user if they want to update it",
				}, "\n"),
			},
			{
				Key:     "memory:project.deploy.server.primary",
				Content: "[Deploy Server] 198.51.100.42 (source: user-defined (DO NOT overwrite))",
			},
		},
	}

	prepared, _, err := engine.Prepare(context.Background(), session, nil, skill.RuntimeContext{})
	if err != nil {
		t.Fatalf("Prepare() error = %v", err)
	}
	if !strings.Contains(prepared.SystemPrompt, "[_memory_guide]") {
		t.Fatalf("SystemPrompt missing memory guide key: %q", prepared.SystemPrompt)
	}
	if !strings.Contains(prepared.SystemPrompt, "198.51.100.42") {
		t.Fatalf("SystemPrompt missing recalled pinned memory: %q", prepared.SystemPrompt)
	}
	if !strings.Contains(prepared.SystemPrompt, "DO NOT overwrite") {
		t.Fatalf("SystemPrompt missing overwrite guard: %q", prepared.SystemPrompt)
	}
}

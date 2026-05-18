package contextengine

import (
	"context"
	"strings"
	"testing"

	"github.com/fulcrus/hopclaw/skill"
)

func TestPrepare_EmptySession(t *testing.T) {
	t.Parallel()

	engine := NewSlidingWindowEngine(Config{
		BaseSystemPrompt:     "You are a runtime assistant.",
		DefaultContextWindow: 120,
		DefaultOutputTokens:  24,
		Estimator: CharRatioEstimator{
			CharsPerToken:        1,
			ToolCharsPerToken:    1,
			EmptyMessageOverhead: 0,
			SafetyMargin:         1.0,
		},
	}, nil)

	session := &Session{ID: "empty-session"}
	prepared, budget, err := engine.Prepare(context.Background(), session, nil, skill.RuntimeContext{})
	if err != nil {
		t.Fatalf("Prepare() error = %v", err)
	}
	if prepared == nil {
		t.Fatal("expected prepared context")
	}
	if budget.ContextWindow != 120 {
		t.Fatalf("budget.ContextWindow = %d, want 120", budget.ContextWindow)
	}
	if budget.ReservedOutput != 24 {
		t.Fatalf("budget.ReservedOutput = %d, want 24", budget.ReservedOutput)
	}
	if budget.MaxInputTokens <= 0 {
		t.Fatalf("budget.MaxInputTokens = %d, want > 0", budget.MaxInputTokens)
	}
	if budget.RemainingInputTokens < 0 {
		t.Fatalf("budget.RemainingInputTokens = %d, want >= 0", budget.RemainingInputTokens)
	}
}

func TestPrepare_PinnedFactsInjected(t *testing.T) {
	t.Parallel()

	engine := NewSlidingWindowEngine(Config{
		BaseSystemPrompt: "You are a runtime assistant.",
	}, nil)

	session := &Session{
		ID: "pinned-facts-session",
		PinnedFacts: []PinnedFact{{
			Key:     "workspace",
			Content: "The active workspace is repo-lifecycle.",
		}},
	}

	prepared, _, err := engine.Prepare(context.Background(), session, nil, skill.RuntimeContext{})
	if err != nil {
		t.Fatalf("Prepare() error = %v", err)
	}
	if !strings.Contains(prepared.SystemPrompt, "Pinned facts:") {
		t.Fatalf("prepared.SystemPrompt = %q", prepared.SystemPrompt)
	}
	if !strings.Contains(prepared.SystemPrompt, "repo-lifecycle") {
		t.Fatalf("prepared.SystemPrompt = %q", prepared.SystemPrompt)
	}
}

func TestCompact_PreservesRecentMessages(t *testing.T) {
	t.Parallel()

	engine := NewSlidingWindowEngine(Config{
		CompactKeepLastN:    2,
		CompactSummaryChars: 120,
		Estimator: CharRatioEstimator{
			CharsPerToken:        1,
			ToolCharsPerToken:    1,
			EmptyMessageOverhead: 0,
			SafetyMargin:         1.0,
		},
	}, nil)

	session := &Session{
		ID: "compact-preserve-session",
		Messages: []Message{
			{Role: RoleUser, Content: "first message"},
			{Role: RoleAssistant, Content: "second message"},
			{Role: RoleUser, Content: "third message"},
			{Role: RoleAssistant, Content: "fourth message"},
		},
	}

	if err := engine.Compact(context.Background(), session, CompactEmergency); err != nil {
		t.Fatalf("Compact() error = %v", err)
	}
	if len(session.Messages) != 2 {
		t.Fatalf("len(session.Messages) = %d, want 2", len(session.Messages))
	}
	if session.Messages[0].Content != "third message" {
		t.Fatalf("session.Messages[0].Content = %q, want %q", session.Messages[0].Content, "third message")
	}
	if session.Messages[1].Content != "fourth message" {
		t.Fatalf("session.Messages[1].Content = %q, want %q", session.Messages[1].Content, "fourth message")
	}
}

func TestCompact_GeneratesSegment(t *testing.T) {
	t.Parallel()

	store := newStubSegmentStore()
	engine := NewSlidingWindowEngine(Config{
		CompactKeepLastN:    1,
		CompactSummaryChars: 120,
		SegmentWriter:       store,
		Estimator: CharRatioEstimator{
			CharsPerToken:        1,
			ToolCharsPerToken:    1,
			EmptyMessageOverhead: 0,
			SafetyMargin:         1.0,
		},
	}, nil)

	session := &Session{
		ID: "compact-segment-session",
		Messages: []Message{
			{Role: RoleUser, Content: "first"},
			{Role: RoleAssistant, Content: "second"},
			{Role: RoleUser, Content: "third"},
		},
		LoadedMessageSeqs: []int64{11, 12, 13},
	}

	if err := engine.Compact(context.Background(), session, CompactEmergency); err != nil {
		t.Fatalf("Compact() error = %v", err)
	}
	if len(store.segments) != 1 {
		t.Fatalf("len(store.segments) = %d, want 1", len(store.segments))
	}
	segment := store.segments[0]
	if segment.SessionID != "compact-segment-session" {
		t.Fatalf("segment.SessionID = %q, want %q", segment.SessionID, "compact-segment-session")
	}
	if segment.SeqStart != 11 || segment.SeqEnd != 12 {
		t.Fatalf("segment seq range = [%d,%d], want [11,12]", segment.SeqStart, segment.SeqEnd)
	}
	if strings.TrimSpace(segment.SummaryText) == "" {
		t.Fatalf("segment.SummaryText = %q, want non-empty summary", segment.SummaryText)
	}
}

func TestPrepare_BudgetCalculation(t *testing.T) {
	t.Parallel()

	engine := NewSlidingWindowEngine(Config{
		BaseSystemPrompt:     "You are a runtime assistant.",
		DefaultContextWindow: 80,
		DefaultOutputTokens:  12,
		MaxInputRatio:        0.75,
		Estimator: CharRatioEstimator{
			CharsPerToken:        1,
			ToolCharsPerToken:    1,
			EmptyMessageOverhead: 0,
			SafetyMargin:         1.0,
		},
	}, nil)

	session := &Session{
		ID: "budget-session",
		Messages: []Message{
			{Role: RoleUser, Content: strings.Repeat("a", 10)},
			{Role: RoleAssistant, Content: strings.Repeat("b", 10)},
		},
	}
	run := &Run{
		MaxContextTokens: 80,
		MaxOutputTokens:  30,
	}

	_, budget, err := engine.Prepare(context.Background(), session, run, skill.RuntimeContext{})
	if err != nil {
		t.Fatalf("Prepare() error = %v", err)
	}
	if budget.ContextWindow != 80 {
		t.Fatalf("budget.ContextWindow = %d, want 80", budget.ContextWindow)
	}
	if budget.ReservedOutput != 30 {
		t.Fatalf("budget.ReservedOutput = %d, want 30", budget.ReservedOutput)
	}
	if budget.MaxInputTokens != 50 {
		t.Fatalf("budget.MaxInputTokens = %d, want 50", budget.MaxInputTokens)
	}
	if budget.EstimatedInputTokens > budget.MaxInputTokens {
		t.Fatalf("budget overflow: %#v", budget)
	}
}

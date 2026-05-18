package contextengine

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/fulcrus/hopclaw/skill"
)

func TestSlidingWindowPrepareContractAssemblesPromptSections(t *testing.T) {
	t.Parallel()

	stateStore := newStubStateStore()
	now := time.Now().UTC()
	if err := stateStore.UpsertState(context.Background(), "sess-contract", []StateEntry{
		{
			Key:        "decision:contract",
			Category:   "decision",
			Value:      "Keep contract coverage around prompt assembly.",
			Status:     "active",
			Confidence: 1.0,
			CreatedAt:  now,
			UpdatedAt:  now,
		},
	}); err != nil {
		t.Fatalf("UpsertState() error = %v", err)
	}

	segmentStore := newStubSegmentStore()
	segmentStore.searchFn = func(sessionID string, queryText string, queryEmbedding []float32, limit int) ([]SummarySegment, error) {
		return []SummarySegment{
			{
				ID:          "seg-contract",
				SessionID:   sessionID,
				SummaryText: "Historical decision: retain the grouped context sections.",
				Decisions:   []string{"Keep prompt sections grouped by source."},
				TSStart:     now.Add(-48 * time.Hour),
				TSEnd:       now.Add(-24 * time.Hour),
			},
		}, nil
	}

	engine := NewSlidingWindowEngine(Config{
		BaseSystemPrompt: "Base system guidance.",
		StateReader:      stateStore,
		SegmentSearcher:  segmentStore,
		EmbeddingClient:  &stubEmbeddingClient{},
		Estimator: CharRatioEstimator{
			CharsPerToken:        1,
			ToolCharsPerToken:    1,
			EmptyMessageOverhead: 0,
			SafetyMargin:         1.0,
		},
	}, nil)

	session := &Session{
		ID:      "sess-contract",
		Summary: "Earlier summary retained for contract coverage.",
		PinnedFacts: []PinnedFact{
			{Key: "workspace", Content: "hopclaw"},
		},
		Messages: []Message{
			{Role: RoleUser, Content: "Need the earlier grouped context decision."},
			{Role: RoleAssistant, Content: "Checking previous notes."},
		},
	}

	prepared, budget, err := engine.Prepare(context.Background(), session, &Run{
		Goal:             "Verify prompt assembly contract",
		MaxContextTokens: 4096,
		MaxOutputTokens:  64,
	}, skill.RuntimeContext{})
	if err != nil {
		t.Fatalf("Prepare() error = %v", err)
	}

	for _, want := range []string{
		"Base system guidance.",
		"Pinned facts:",
		"<session_state>",
		"<recalled_context",
	} {
		if !strings.Contains(prepared.SystemPrompt, want) {
			t.Fatalf("SystemPrompt missing %q: %q", want, prepared.SystemPrompt)
		}
	}
	if prepared.RetrievalReceipt == nil || len(prepared.RetrievalReceipt.Injected) != 1 {
		t.Fatalf("RetrievalReceipt = %#v, want one injected recalled hit", prepared.RetrievalReceipt)
	}

	kinds := map[SegmentKind]bool{}
	for _, segment := range prepared.Segments {
		kinds[segment.Kind] = true
	}
	for _, want := range []SegmentKind{
		SegmentPinnedFacts,
		SegmentSessionState,
		SegmentRecalled,
		SegmentSummary,
		SegmentMessages,
	} {
		if !kinds[want] {
			t.Fatalf("Segments missing %q: %#v", want, prepared.Segments)
		}
	}
	if got, want := budget.EstimatedInputTokens, segmentTokens(prepared.Segments); got != want {
		t.Fatalf("EstimatedInputTokens = %d, want %d", got, want)
	}
}

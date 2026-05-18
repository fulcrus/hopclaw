package contextengine

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/fulcrus/hopclaw/skill"
)

func TestRetrievalReceipt_ContextReportHasReceipt(t *testing.T) {
	t.Parallel()

	store := newStubSegmentStore()
	store.searchFn = func(sessionID string, queryText string, queryEmbedding []float32, limit int) ([]SummarySegment, error) {
		return []SummarySegment{
			{
				ID:          "seg-1",
				SessionID:   sessionID,
				SummaryText: "We chose SQLite for the runtime store.",
				Decisions:   []string{"Keep SQLite as the first durable store."},
				TSStart:     time.Date(2025, 11, 2, 0, 0, 0, 0, time.UTC),
				TSEnd:       time.Date(2025, 11, 5, 0, 0, 0, 0, time.UTC),
			},
		}, nil
	}

	engine := newReceiptTestEngine(store)
	session := &Session{
		ID: "sess-receipt-report",
		Messages: []Message{
			{Role: RoleUser, Content: "Need the historical SQLite decision"},
		},
	}

	report, err := engine.Inspect(context.Background(), session, &Run{
		MaxContextTokens: 4000,
		MaxOutputTokens:  10,
	}, skill.RuntimeContext{})
	if err != nil {
		t.Fatalf("Inspect() error = %v", err)
	}
	if report.RetrievalReceipt == nil {
		t.Fatal("RetrievalReceipt should be present")
	}
	if report.RetrievalReceipt.GeneratedAt.IsZero() {
		t.Fatal("RetrievalReceipt.GeneratedAt should be set")
	}
	if len(report.RetrievalReceipt.Queries) != 1 || report.RetrievalReceipt.Queries[0] != "Need the historical SQLite decision" {
		t.Fatalf("Queries = %#v", report.RetrievalReceipt.Queries)
	}
	if len(report.RetrievalReceipt.Hits) != 1 {
		t.Fatalf("Hits = %#v", report.RetrievalReceipt.Hits)
	}
	if len(report.RetrievalReceipt.Injected) != 1 {
		t.Fatalf("Injected = %#v", report.RetrievalReceipt.Injected)
	}
	if report.RetrievalReceipt.Hits[0].Kind != "recalled_segment" {
		t.Fatalf("Kind = %q", report.RetrievalReceipt.Hits[0].Kind)
	}
}

func TestRetrievalReceipt_TrimReason(t *testing.T) {
	t.Parallel()

	shared := SummarySegment{
		ID:          "seg-shared",
		SummaryText: "Shared architectural decision about SQLite durability.",
		Decisions:   []string{"Use SQLite as the first durable store."},
		TSStart:     time.Date(2025, 11, 2, 0, 0, 0, 0, time.UTC),
		TSEnd:       time.Date(2025, 11, 5, 0, 0, 0, 0, time.UTC),
	}
	resultsByQuery := map[string][]SummarySegment{
		"Need historical context": {
			shared,
			{
				ID:          "seg-a",
				SummaryText: "A very long recalled summary that should compete for the remaining recall budget and be trimmed.",
				Constraints: []string{"Keep audit logs append-only."},
				TSStart:     time.Date(2025, 11, 6, 0, 0, 0, 0, time.UTC),
				TSEnd:       time.Date(2025, 11, 7, 0, 0, 0, 0, time.UTC),
			},
		},
		"Prior summary": {
			shared,
			{
				ID:          "seg-b",
				SummaryText: "Another recalled summary that should be dropped once the recall budget is exhausted.",
				Constraints: []string{"Preserve append-only semantics."},
				TSStart:     time.Date(2025, 11, 8, 0, 0, 0, 0, time.UTC),
				TSEnd:       time.Date(2025, 11, 9, 0, 0, 0, 0, time.UTC),
			},
		},
		"Prefer audit facts": {
			shared,
			{
				ID:          "seg-c",
				SummaryText: "Yet another recalled summary that will not fit the constrained recall budget.",
				Constraints: []string{"Keep forensic history intact."},
				TSStart:     time.Date(2025, 11, 10, 0, 0, 0, 0, time.UTC),
				TSEnd:       time.Date(2025, 11, 11, 0, 0, 0, 0, time.UTC),
			},
		},
	}

	store := newStubSegmentStore()
	store.searchFn = func(sessionID string, queryText string, queryEmbedding []float32, limit int) ([]SummarySegment, error) {
		return append([]SummarySegment(nil), resultsByQuery[queryText]...), nil
	}

	engine := newReceiptTestEngine(store)
	session := &Session{
		ID:      "sess-receipt-trim",
		Summary: "Prior summary",
		Messages: []Message{
			{Role: RoleUser, Content: "Need historical context"},
		},
	}

	report, err := engine.Inspect(context.Background(), session, &Run{
		Goal:             "Prefer audit facts",
		MaxContextTokens: 100,
		MaxOutputTokens:  10,
	}, skill.RuntimeContext{})
	if err != nil {
		t.Fatalf("Inspect() error = %v", err)
	}
	if report.RetrievalReceipt == nil {
		t.Fatal("RetrievalReceipt should be present")
	}
	if len(report.RetrievalReceipt.Trimmed) == 0 {
		t.Fatalf("Trimmed = %#v, want at least one trimmed hit", report.RetrievalReceipt.Trimmed)
	}

	var sawDuplicate bool
	var sawBudget bool
	for _, hit := range report.RetrievalReceipt.Trimmed {
		if strings.TrimSpace(hit.TrimReason) == "" {
			t.Fatalf("trimmed hit should include a trim reason: %#v", hit)
		}
		switch hit.TrimReason {
		case "duplicate":
			sawDuplicate = true
		case "budget_exceeded":
			sawBudget = true
		}
	}
	if !sawDuplicate {
		t.Fatalf("Trimmed = %#v, want duplicate trim reason", report.RetrievalReceipt.Trimmed)
	}
	if !sawBudget {
		t.Fatalf("Trimmed = %#v, want budget_exceeded trim reason", report.RetrievalReceipt.Trimmed)
	}
	if len(report.RetrievalReceipt.Queries) != 3 {
		t.Fatalf("Queries = %#v, want latest message, summary, and run goal", report.RetrievalReceipt.Queries)
	}
	if report.RetrievalReceipt.Queries[2] != "Prefer audit facts" {
		t.Fatalf("Queries = %#v, want run goal instead of system prompt", report.RetrievalReceipt.Queries)
	}
}

func TestRetrievalReceipt_TokenAccounting(t *testing.T) {
	t.Parallel()

	segment := SummarySegment{
		ID:          "seg-token",
		SummaryText: "We chose SQLite for the runtime store.",
		Decisions:   []string{"Keep append-only audit logs."},
		TSStart:     time.Date(2025, 11, 2, 0, 0, 0, 0, time.UTC),
		TSEnd:       time.Date(2025, 11, 5, 0, 0, 0, 0, time.UTC),
	}

	store := newStubSegmentStore()
	store.searchFn = func(sessionID string, queryText string, queryEmbedding []float32, limit int) ([]SummarySegment, error) {
		return []SummarySegment{segment}, nil
	}

	engine := newReceiptTestEngine(store)
	session := &Session{
		ID: "sess-receipt-token",
		Messages: []Message{
			{Role: RoleUser, Content: "Need the runtime store decision"},
		},
	}

	prepared, _, err := engine.Prepare(context.Background(), session, &Run{
		MaxContextTokens: 4000,
		MaxOutputTokens:  10,
	}, skill.RuntimeContext{})
	if err != nil {
		t.Fatalf("Prepare() error = %v", err)
	}
	if prepared.RetrievalReceipt == nil {
		t.Fatal("RetrievalReceipt should be present")
	}
	if len(prepared.RetrievalReceipt.Injected) != 1 {
		t.Fatalf("Injected = %#v", prepared.RetrievalReceipt.Injected)
	}

	expectedTokens := receiptTestEstimator().Estimate(renderRecalledSegmentBlock(segment))
	if prepared.RetrievalReceipt.Injected[0].Tokens != expectedTokens {
		t.Fatalf("Injected[0].Tokens = %d, want %d", prepared.RetrievalReceipt.Injected[0].Tokens, expectedTokens)
	}
	if prepared.RetrievalReceipt.TotalTokens != expectedTokens {
		t.Fatalf("TotalTokens = %d, want %d", prepared.RetrievalReceipt.TotalTokens, expectedTokens)
	}
}

func newReceiptTestEngine(store *stubSegmentStore) *SlidingWindowEngine {
	return NewSlidingWindowEngine(Config{
		Estimator:       receiptTestEstimator(),
		EmbeddingClient: &stubEmbeddingClient{},
		SegmentSearcher: store,
	}, nil)
}

func receiptTestEstimator() CharRatioEstimator {
	return CharRatioEstimator{
		CharsPerToken:        1,
		ToolCharsPerToken:    1,
		EmptyMessageOverhead: 0,
		SafetyMargin:         1.0,
	}
}

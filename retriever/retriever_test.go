package retriever

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sync"
	"testing"
)

type stubMemorySearcher struct {
	hits []Hit
	err  error
}

func (s stubMemorySearcher) SearchMemory(_ context.Context, _ Query) ([]Hit, error) {
	return append([]Hit(nil), s.hits...), s.err
}

type stubSegmentSearcher struct {
	mu        sync.Mutex
	results   map[string][]Hit
	errByText map[string]error
	calls     []segmentCall
}

type segmentCall struct {
	sessionID string
	queryText string
	embedding []float32
	limit     int
}

func (s *stubSegmentSearcher) SearchSegments(_ context.Context, sessionID string, queryText string, queryEmbedding []float32, limit int) ([]Hit, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	call := segmentCall{
		sessionID: sessionID,
		queryText: queryText,
		embedding: append([]float32(nil), queryEmbedding...),
		limit:     limit,
	}
	s.calls = append(s.calls, call)

	if err := s.errByText[queryText]; err != nil {
		return nil, err
	}
	return append([]Hit(nil), s.results[queryText]...), nil
}

type stubKnowledgeSearcher struct {
	hits []Hit
	err  error
}

func (s stubKnowledgeSearcher) SearchKnowledge(_ context.Context, _ Query) ([]Hit, error) {
	return append([]Hit(nil), s.hits...), s.err
}

type stubEmbedder struct {
	mu      sync.Mutex
	vectors map[string][]float32
	calls   []string
}

func (s *stubEmbedder) Embed(_ context.Context, texts []string) ([][]float32, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	results := make([][]float32, len(texts))
	for i, text := range texts {
		s.calls = append(s.calls, text)
		if vector, ok := s.vectors[text]; ok {
			results[i] = append([]float32(nil), vector...)
		}
	}
	return results, nil
}

func TestUnifiedRetriever_AllSources(t *testing.T) {
	t.Parallel()

	segments := &stubSegmentSearcher{
		results: map[string][]Hit{
			"postgres latency": {
				{ID: "seg-latency", Score: 0.72, Content: "Segment about postgres latency", Freshness: 0.70},
			},
			"reduce p95 query latency": {
				{ID: "seg-latency", Score: 0.68, Content: "Duplicate segment from summary query", Freshness: 0.68},
			},
		},
	}
	embedder := &stubEmbedder{
		vectors: map[string][]float32{
			"postgres latency":         {1, 0},
			"reduce p95 query latency": {0.8, 0.2},
		},
	}
	retriever := &UnifiedRetriever{
		Memory: stubMemorySearcher{hits: []Hit{
			{ID: "mem-latency", Score: 0.75, Reason: "channel:hybrid", Scope: "project", Content: "User prefers Postgres query plans", Authority: 1.0, Freshness: 0.95},
		}},
		Segments: segments,
		Knowledge: stubKnowledgeSearcher{hits: []Hit{
			{ID: "kb-tuning", Score: 0.70, Scope: "runbooks", Content: "Knowledge doc about query tuning", Authority: 0.4, Freshness: 0.85},
		}},
		Embedder: embedder,
	}

	hits, err := retriever.Retrieve(context.Background(), Query{
		Text:          "postgres latency",
		TargetSummary: "reduce p95 query latency",
		SessionID:     "sess-123",
		MaxResults:    10,
	})
	if err != nil {
		t.Fatalf("Retrieve() error = %v", err)
	}
	if len(hits) != 3 {
		t.Fatalf("len(hits) = %d, want 3", len(hits))
	}

	kinds := []HitKind{hits[0].Kind, hits[1].Kind, hits[2].Kind}
	if !reflect.DeepEqual(kinds, []HitKind{HitMemory, HitKnowledge, HitSegment}) {
		t.Fatalf("kinds = %v, want [memory knowledge segment]", kinds)
	}

	if len(segments.calls) != 2 {
		t.Fatalf("segment calls = %d, want 2", len(segments.calls))
	}
	for _, call := range segments.calls {
		if call.sessionID != "sess-123" {
			t.Fatalf("segment sessionID = %q, want sess-123", call.sessionID)
		}
		if len(call.embedding) == 0 {
			t.Fatalf("expected embedding for query %q", call.queryText)
		}
	}
}

func TestUnifiedRetriever_ReranksCorrectly(t *testing.T) {
	t.Parallel()

	retriever := &UnifiedRetriever{
		Memory: stubMemorySearcher{hits: []Hit{
			{ID: "memory", Score: 0.66, Content: "Pinned deployment rule", Authority: 1.0, Freshness: 0.90},
		}},
		Segments: &stubSegmentSearcher{
			results: map[string][]Hit{
				"rollback plan": {
					{ID: "segment", Score: 0.90, Content: "Older recalled segment", Freshness: 0.30},
				},
			},
		},
		Knowledge: stubKnowledgeSearcher{hits: []Hit{
			{ID: "knowledge", Score: 0.80, Content: "Rollback SOP", Authority: 0.40, Freshness: 0.70},
		}},
	}

	hits, err := retriever.Retrieve(context.Background(), Query{
		Text:       "rollback plan",
		SessionID:  "sess-456",
		MaxResults: 10,
	})
	if err != nil {
		t.Fatalf("Retrieve() error = %v", err)
	}

	gotIDs := []string{hits[0].ID, hits[1].ID, hits[2].ID}
	wantIDs := []string{"memory", "knowledge", "segment"}
	if !reflect.DeepEqual(gotIDs, wantIDs) {
		t.Fatalf("ids = %v, want %v", gotIDs, wantIDs)
	}
	if hits[0].Score <= hits[1].Score || hits[1].Score <= hits[2].Score {
		t.Fatalf("scores not strictly descending: %#v", hits)
	}
}

func TestUnifiedRetriever_MissingSource(t *testing.T) {
	t.Parallel()

	retriever := &UnifiedRetriever{
		Memory: stubMemorySearcher{
			err: errors.New("memory backend unavailable"),
		},
		Knowledge: stubKnowledgeSearcher{hits: []Hit{
			{ID: "kb-1", Score: 0.61, Content: "Fallback knowledge answer", Authority: 0.40, Freshness: 0.80},
		}},
	}

	hits, err := retriever.Retrieve(context.Background(), Query{
		Text:       "incident checklist",
		MaxResults: 5,
	})
	if err != nil {
		t.Fatalf("Retrieve() error = %v, want nil when another source succeeds", err)
	}
	if len(hits) != 1 {
		t.Fatalf("len(hits) = %d, want 1", len(hits))
	}
	if hits[0].Kind != HitKnowledge {
		t.Fatalf("kind = %q, want %q", hits[0].Kind, HitKnowledge)
	}
}

func TestRerank_AuthorityWeighting(t *testing.T) {
	t.Parallel()

	hits := Rerank([]Hit{
		{Kind: HitMemory, ID: "user", Score: 0.60, Authority: 1.0, Freshness: 0.50, Content: "user preference"},
		{Kind: HitMemory, ID: "agent", Score: 0.60, Authority: 0.6, Freshness: 0.50, Content: "agent inference"},
	}, 2)

	gotIDs := []string{hits[0].ID, hits[1].ID}
	wantIDs := []string{"user", "agent"}
	if !reflect.DeepEqual(gotIDs, wantIDs) {
		t.Fatalf("ids = %v, want %v", gotIDs, wantIDs)
	}

	wantTopScore := 0.40*0.60 + 0.25*1.0 + 0.20*0.50 + 0.15*0.10
	if diff := mathAbs(hits[0].Score - wantTopScore); diff > 1e-9 {
		t.Fatalf("top score = %f, want %f (diff=%f)", hits[0].Score, wantTopScore, diff)
	}
}

func mathAbs(value float64) float64 {
	if value < 0 {
		return -value
	}
	return value
}

func (s *stubSegmentSearcher) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return fmt.Sprintf("%v", s.calls)
}

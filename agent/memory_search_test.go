package agent

import (
	"context"
	"strings"
	"testing"
	"time"
)

type testMemorySearchStore struct {
	documents []memorySearchDocument
	entries   map[string]MemoryEntry
}

func newTestMemorySearchStore(documents ...memorySearchDocument) *testMemorySearchStore {
	entries := make(map[string]MemoryEntry, len(documents))
	for _, document := range documents {
		entries[document.Entry.Key] = document.Entry
	}
	return &testMemorySearchStore{
		documents: append([]memorySearchDocument(nil), documents...),
		entries:   entries,
	}
}

func (s *testMemorySearchStore) Get(_ context.Context, key string) (*MemoryEntry, error) {
	entry, ok := s.entries[key]
	if !ok {
		return nil, nil
	}
	cloned := entry
	return &cloned, nil
}

func (s *testMemorySearchStore) Set(_ context.Context, key, value string) error {
	entry, ok := s.entries[key]
	if !ok {
		entry = MemoryEntry{
			Key:       key,
			CreatedAt: time.Now().UTC(),
		}
	}
	entry.Value = value
	entry.UpdatedAt = time.Now().UTC()
	s.entries[key] = entry
	return nil
}

func (s *testMemorySearchStore) Delete(_ context.Context, key string) error {
	delete(s.entries, key)
	return nil
}

func (s *testMemorySearchStore) Search(_ context.Context, _ string) ([]MemoryEntry, error) {
	return s.List(context.Background())
}

func (s *testMemorySearchStore) SemanticSearch(_ context.Context, _ string, _ int) ([]MemoryEntry, error) {
	return nil, nil
}

func (s *testMemorySearchStore) SemanticSearchMMR(_ context.Context, _ string, _ int, _ float64) ([]MemoryEntry, error) {
	return nil, nil
}

func (s *testMemorySearchStore) List(_ context.Context) ([]MemoryEntry, error) {
	results := make([]MemoryEntry, 0, len(s.documents))
	for _, document := range s.documents {
		results = append(results, document.Entry)
	}
	return results, nil
}

func (s *testMemorySearchStore) memorySearchDocuments(_ context.Context) ([]memorySearchDocument, error) {
	results := make([]memorySearchDocument, 0, len(s.documents))
	for _, document := range s.documents {
		cloned := document
		cloned.lexicalFields = append([]memorySearchField(nil), document.lexicalFields...)
		results = append(results, cloned)
	}
	return results, nil
}

func TestMemorySearcher_HybridResults(t *testing.T) {
	t.Parallel()

	store := newTestMemorySearchStore(
		testMemorySearchDocument(testMemorySearchEntry("lexical", "postgres latency guide"), "postgres latency guide"),
		testMemorySearchDocument(testMemorySearchEntry("semantic", "query tuning handbook"), "semantic postgres tuning"),
		testMemorySearchDocument(testMemorySearchEntry("other", "team lunch schedule"), "team lunch schedule"),
	)
	searcher := NewMemorySearcher(store, testMemorySearchEmbedder(map[string][]float32{
		"postgres latency":         {1, 0},
		"postgres latency guide":   {0.6, 0.8},
		"semantic postgres tuning": {0.95, 0.1},
		"team lunch schedule":      {-1, 0},
	}))

	hits, err := searcher.SearchMemories(context.Background(), MemoryQuery{
		Text:       "postgres latency",
		MaxResults: 2,
	})
	if err != nil {
		t.Fatalf("search memories: %v", err)
	}
	assertMemorySearcherKeys(t, hits, []string{"lexical", "semantic"})
	if !strings.Contains(hits[1].Reason, "channel:semantic") {
		t.Fatalf("expected semantic hit reason, got %q", hits[1].Reason)
	}
}

func TestMemorySearcher_MMRDiversity(t *testing.T) {
	t.Parallel()

	store := newTestMemorySearchStore(
		testMemorySearchDocument(testMemorySearchEntry("dup-a", "service resilience postgres"), "service resilience postgres"),
		testMemorySearchDocument(testMemorySearchEntry("dup-b", "service resilience database"), "service resilience database"),
		testMemorySearchDocument(testMemorySearchEntry("diverse", "service resilience operations"), "service resilience operations"),
	)
	searcher := NewMemorySearcher(store, testMemorySearchEmbedder(map[string][]float32{
		"service resilience":            {1, 0},
		"service resilience postgres":   {1, 0},
		"service resilience database":   {1, 0},
		"service resilience operations": {0.75, 0.6614378},
	}))

	hits, err := searcher.SearchMemories(context.Background(), MemoryQuery{
		Text:       "service resilience",
		MaxResults: 2,
	})
	if err != nil {
		t.Fatalf("search memories: %v", err)
	}
	if len(hits) != 2 {
		t.Fatalf("expected 2 hits, got %d", len(hits))
	}
	keys := []string{hits[0].Entry.Key, hits[1].Entry.Key}
	if !containsMemorySearchString(keys, "diverse") {
		t.Fatalf("expected MMR to keep diverse result, got %v", keys)
	}
	if containsMemorySearchString(keys, "dup-a") && containsMemorySearchString(keys, "dup-b") {
		t.Fatalf("expected MMR to avoid duplicate pair, got %v", keys)
	}
}

func TestMemorySearcher_NoEmbedder_LexicalOnly(t *testing.T) {
	t.Parallel()

	store := newTestMemorySearchStore(
		testMemorySearchDocument(testMemorySearchEntry("deploy", "deploy target service"), "deploy target service"),
		testMemorySearchDocument(testMemorySearchEntry("notes", "calendar housekeeping"), "calendar housekeeping"),
	)
	searcher := NewMemorySearcher(store, nil)

	hits, err := searcher.SearchMemories(context.Background(), MemoryQuery{
		Text:       "deploy target",
		MaxResults: 5,
	})
	if err != nil {
		t.Fatalf("search memories: %v", err)
	}
	assertMemorySearcherKeys(t, hits, []string{"deploy"})
	if !strings.Contains(hits[0].Reason, "channel:lexical") {
		t.Fatalf("expected lexical-only reason, got %q", hits[0].Reason)
	}
}

func TestMemorySearcher_EmbeddingCache(t *testing.T) {
	t.Parallel()

	store := newTestMemorySearchStore(
		testMemorySearchDocument(testMemorySearchEntry("alpha", "alpha memory"), "alpha memory"),
		testMemorySearchDocument(testMemorySearchEntry("beta", "beta memory"), "beta memory"),
	)
	embedder := &mockEmbeddingClient{
		embedFn: func(texts []string) ([][]float32, error) {
			results := make([][]float32, len(texts))
			for idx, text := range texts {
				switch text {
				case "cache query":
					results[idx] = []float32{1, 0}
				case "alpha memory":
					results[idx] = []float32{1, 0}
				case "beta memory":
					results[idx] = []float32{0.8, 0.2}
				default:
					results[idx] = simpleTextVector(text)
				}
			}
			return results, nil
		},
	}
	searcher := NewMemorySearcher(store, embedder)

	query := MemoryQuery{Text: "cache query", MaxResults: 2}
	if _, err := searcher.SearchMemories(context.Background(), query); err != nil {
		t.Fatalf("first search: %v", err)
	}

	embedder.mu.Lock()
	firstCalls := embedder.calls
	embedder.mu.Unlock()

	if _, err := searcher.SearchMemories(context.Background(), query); err != nil {
		t.Fatalf("second search: %v", err)
	}

	embedder.mu.Lock()
	secondCalls := embedder.calls
	embedder.mu.Unlock()

	if firstCalls != 2 {
		t.Fatalf("expected first search to call embed twice (query + documents), got %d", firstCalls)
	}
	if secondCalls-firstCalls != 1 {
		t.Fatalf("expected cached documents to avoid re-embedding, calls before=%d after=%d", firstCalls, secondCalls)
	}
}

func testMemorySearchEntry(key, value string) MemoryEntry {
	now := time.Now().UTC()
	return MemoryEntry{
		Key:           key,
		Value:         value,
		Source:        MemorySourceAgent,
		State:         MemoryActive,
		EvidenceCount: 1,
		LastUsedAt:    now,
		CreatedAt:     now,
		UpdatedAt:     now,
	}
}

func testMemorySearchDocument(entry MemoryEntry, semanticText string) memorySearchDocument {
	document := newMemorySearchDocument(entry, "")
	if strings.TrimSpace(semanticText) != "" {
		document.semanticText = semanticText
		document.fingerprint = strings.Join([]string{
			entry.Key,
			document.semanticText,
			entry.UpdatedAt.UTC().Format(time.RFC3339Nano),
		}, "\x1f")
	}
	return document
}

func testMemorySearchEmbedder(vectors map[string][]float32) EmbeddingClient {
	return &mockEmbeddingClient{
		embedFn: func(texts []string) ([][]float32, error) {
			results := make([][]float32, len(texts))
			for idx, text := range texts {
				if vector, ok := vectors[text]; ok {
					results[idx] = append([]float32(nil), vector...)
					continue
				}
				results[idx] = simpleTextVector(text)
			}
			return results, nil
		},
	}
}

func assertMemorySearcherKeys(t *testing.T, hits []MemoryHit, want []string) {
	t.Helper()
	if len(hits) != len(want) {
		t.Fatalf("expected %d hits, got %d", len(want), len(hits))
	}
	for idx, key := range want {
		if hits[idx].Entry.Key != key {
			t.Fatalf("expected hit %d to be %q, got %q", idx, key, hits[idx].Entry.Key)
		}
	}
}

func containsMemorySearchString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

package agent

import (
	"context"
	"fmt"
	"sync"
	"testing"
)

// mockEmbeddingClient returns predictable embeddings for testing.
type mockEmbeddingClient struct {
	mu      sync.Mutex
	calls   int
	embedFn func(texts []string) ([][]float32, error)
}

func (m *mockEmbeddingClient) Embed(_ context.Context, texts []string) ([][]float32, error) {
	m.mu.Lock()
	m.calls++
	m.mu.Unlock()
	if m.embedFn != nil {
		return m.embedFn(texts)
	}
	// Default: return a simple hash-based embedding.
	results := make([][]float32, len(texts))
	for i, text := range texts {
		results[i] = simpleTextVector(text)
	}
	return results, nil
}

// simpleTextVector creates a deterministic 3D embedding from text for testing.
func simpleTextVector(text string) []float32 {
	var sum float32
	for _, ch := range text {
		sum += float32(ch)
	}
	return []float32{sum, sum * 0.5, sum * 0.25}
}

func TestKVStoreSetEmbedding(t *testing.T) {
	t.Parallel()
	store := NewInMemoryKVStore()
	if store.HasEmbedding() {
		t.Fatal("expected no embedding before SetEmbedding")
	}

	store.SetEmbedding(&mockEmbeddingClient{})
	if !store.HasEmbedding() {
		t.Fatal("expected embedding after SetEmbedding")
	}
}

func TestKVStoreSetGeneratesEmbedding(t *testing.T) {
	t.Parallel()
	store := NewInMemoryKVStore()
	mock := &mockEmbeddingClient{}
	store.SetEmbedding(mock)

	ctx := context.Background()
	if err := store.Set(ctx, "key1", "value1"); err != nil {
		t.Fatalf("set: %v", err)
	}

	mock.mu.Lock()
	calls := mock.calls
	mock.mu.Unlock()
	if calls != 1 {
		t.Fatalf("expected 1 embed call, got %d", calls)
	}

	// Verify vector was stored.
	entry, ok := store.vectorStore.Get("key1")
	if !ok {
		t.Fatal("expected vector entry for key1")
	}
	if entry.Value != "value1" {
		t.Fatalf("expected value1, got %q", entry.Value)
	}
}

func TestKVStoreSemanticSearch(t *testing.T) {
	t.Parallel()
	store := NewInMemoryKVStore()
	store.SetEmbedding(&mockEmbeddingClient{})

	ctx := context.Background()
	store.Set(ctx, "go-tutorial", "how to write go code")
	store.Set(ctx, "python-tutorial", "how to write python code")
	store.Set(ctx, "rust-tutorial", "how to write rust code")

	results, err := store.SemanticSearch(ctx, "go programming", 2)
	if err != nil {
		t.Fatalf("semantic search: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	// Results should have scores.
	if results[0].Score <= 0 {
		t.Fatal("expected positive score")
	}
}

func TestKVStoreSemanticSearchWithoutEmbedding(t *testing.T) {
	t.Parallel()
	store := NewInMemoryKVStore()

	ctx := context.Background()
	_, err := store.SemanticSearch(ctx, "test", 10)
	if err == nil {
		t.Fatal("expected error when embedding not configured")
	}
}

func TestKVStoreDeleteRemovesVector(t *testing.T) {
	t.Parallel()
	store := NewInMemoryKVStore()
	store.SetEmbedding(&mockEmbeddingClient{})

	ctx := context.Background()
	store.Set(ctx, "key1", "value1")

	// Verify vector exists.
	if _, ok := store.vectorStore.Get("key1"); !ok {
		t.Fatal("expected vector for key1")
	}

	store.Delete(ctx, "key1")

	// Verify vector is removed.
	if _, ok := store.vectorStore.Get("key1"); ok {
		t.Fatal("expected vector to be deleted")
	}
}

func TestKVStoreSearchHybridMode(t *testing.T) {
	t.Parallel()
	store := NewInMemoryKVStore()
	store.SetEmbedding(&mockEmbeddingClient{})

	ctx := context.Background()
	store.Set(ctx, "database", "postgres is a relational database")
	store.Set(ctx, "cache", "redis is an in-memory cache")
	store.Set(ctx, "search-engine", "elasticsearch for full-text search")

	// Default Search() with embedding uses hybrid mode.
	results, err := store.Search(ctx, "database")
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	// Should include at least the keyword match.
	found := false
	for _, r := range results {
		if r.Key == "database" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected keyword match for 'database'")
	}
}

func TestKVStoreSearchWithoutEmbeddingIsKeywordOnly(t *testing.T) {
	t.Parallel()
	store := NewInMemoryKVStore()

	ctx := context.Background()
	store.Set(ctx, "hello", "world")
	store.Set(ctx, "goodbye", "moon")

	results, err := store.Search(ctx, "hello")
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Key != "hello" {
		t.Fatalf("expected 'hello', got %q", results[0].Key)
	}
}

func TestKVStoreEmbeddingFailureNonFatal(t *testing.T) {
	t.Parallel()
	store := NewInMemoryKVStore()
	mock := &mockEmbeddingClient{
		embedFn: func(texts []string) ([][]float32, error) {
			return nil, fmt.Errorf("api error")
		},
	}
	store.SetEmbedding(mock)

	ctx := context.Background()
	// Set should succeed even if embedding fails.
	if err := store.Set(ctx, "key1", "value1"); err != nil {
		t.Fatalf("set should not fail on embedding error: %v", err)
	}

	// KV entry should exist even without vector.
	entry, err := store.Get(ctx, "key1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if entry == nil || entry.Value != "value1" {
		t.Fatal("expected entry to exist despite embedding failure")
	}
}

func TestKVStoreVectorUpsertFailureNonFatal(t *testing.T) {
	t.Parallel()

	store := NewInMemoryKVStore()
	store.SetEmbedding(&mockEmbeddingClient{})
	store.vectorStore.dim = 1

	ctx := context.Background()
	if err := store.Set(ctx, "key1", "value1"); err != nil {
		t.Fatalf("set should not fail on vector upsert error: %v", err)
	}

	entry, err := store.Get(ctx, "key1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if entry == nil || entry.Value != "value1" {
		t.Fatal("expected entry to exist despite vector upsert failure")
	}
	if _, ok := store.vectorStore.Get("key1"); ok {
		t.Fatal("expected no vector entry after failed upsert")
	}
}

func TestMergeResultsDeduplicates(t *testing.T) {
	t.Parallel()
	keyword := []MemoryEntry{
		{Key: "a", Value: "1"},
		{Key: "b", Value: "2"},
	}
	semantic := []MemoryEntry{
		{Key: "b", Value: "2", Score: 0.9},
		{Key: "c", Value: "3", Score: 0.8},
	}

	merged := mergeResults(keyword, semantic)
	if len(merged) != 3 {
		t.Fatalf("expected 3 merged results, got %d", len(merged))
	}
	keys := make(map[string]bool)
	for _, r := range merged {
		keys[r.Key] = true
	}
	for _, expected := range []string{"a", "b", "c"} {
		if !keys[expected] {
			t.Fatalf("expected key %q in merged results", expected)
		}
	}
}

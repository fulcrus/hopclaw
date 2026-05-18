package model

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

type countingEmbedder struct {
	calls atomic.Int64
	dim   int
}

func (e *countingEmbedder) Embed(_ context.Context, texts []string) ([][]float32, error) {
	e.calls.Add(1)
	results := make([][]float32, len(texts))
	for i := range texts {
		vec := make([]float32, e.dim)
		for j := range vec {
			vec[j] = float32(len(texts[i])) // deterministic by text length
		}
		results[i] = vec
	}
	return results, nil
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

func TestCachedEmbeddingClient_HitsAvoidAPICalls(t *testing.T) {
	inner := &countingEmbedder{dim: 4}
	cached := NewCachedEmbeddingClient(inner, 0) // default size

	ctx := context.Background()

	// First call: cache miss.
	v1, err := cached.Embed(ctx, []string{"hello", "world"})
	if err != nil {
		t.Fatal(err)
	}
	if inner.calls.Load() != 1 {
		t.Fatalf("expected 1 inner call, got %d", inner.calls.Load())
	}
	if len(v1) != 2 {
		t.Fatalf("expected 2 results, got %d", len(v1))
	}

	// Second call with same texts: should be fully cached.
	v2, err := cached.Embed(ctx, []string{"hello", "world"})
	if err != nil {
		t.Fatal(err)
	}
	if inner.calls.Load() != 1 {
		t.Fatalf("expected still 1 inner call, got %d", inner.calls.Load())
	}
	if len(v2) != 2 {
		t.Fatalf("expected 2 results, got %d", len(v2))
	}
}

func TestCachedEmbeddingClient_PartialHit(t *testing.T) {
	inner := &countingEmbedder{dim: 4}
	cached := NewCachedEmbeddingClient(inner, 0)

	ctx := context.Background()

	// Warm up cache with "hello".
	if _, err := cached.Embed(ctx, []string{"hello"}); err != nil {
		t.Fatal(err)
	}
	if inner.calls.Load() != 1 {
		t.Fatalf("expected 1 call, got %d", inner.calls.Load())
	}

	// Request "hello" (cached) + "world" (miss).
	results, err := cached.Embed(ctx, []string{"hello", "world"})
	if err != nil {
		t.Fatal(err)
	}
	if inner.calls.Load() != 2 {
		t.Fatalf("expected 2 calls, got %d", inner.calls.Load())
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	// "world" has len 5, so all dims should be 5.
	for _, v := range results[1] {
		if v != 5.0 {
			t.Fatalf("expected 5.0, got %f", v)
		}
	}
}

func TestCachedEmbeddingClient_LRUEviction(t *testing.T) {
	inner := &countingEmbedder{dim: 2}
	cached := NewCachedEmbeddingClient(inner, 2) // max 2 entries

	ctx := context.Background()

	// Fill cache with "a", "b".
	if _, err := cached.Embed(ctx, []string{"a", "b"}); err != nil {
		t.Fatal(err)
	}
	if cached.Len() != 2 {
		t.Fatalf("expected 2 cached, got %d", cached.Len())
	}

	// Touch "a" to make it recently used; "b" becomes LRU.
	calls := inner.calls.Load()
	if _, err := cached.Embed(ctx, []string{"a"}); err != nil {
		t.Fatal(err)
	}
	if inner.calls.Load() != calls {
		t.Fatal("expected cache hit for 'a'")
	}

	// Insert "c" — evicts "b" (LRU). Cache is now {a, c}.
	if _, err := cached.Embed(ctx, []string{"c"}); err != nil {
		t.Fatal(err)
	}
	if cached.Len() != 2 {
		t.Fatalf("expected 2 cached after eviction, got %d", cached.Len())
	}

	// Verify "b" was evicted.
	calls = inner.calls.Load()
	if _, err := cached.Embed(ctx, []string{"b"}); err != nil {
		t.Fatal(err)
	}
	if inner.calls.Load() != calls+1 {
		t.Fatal("expected cache miss for evicted 'b'")
	}

	// Verify "c" survived (it was recently inserted).
	calls = inner.calls.Load()
	if _, err := cached.Embed(ctx, []string{"c"}); err != nil {
		t.Fatal(err)
	}
	if inner.calls.Load() != calls {
		t.Fatal("expected cache hit for 'c'")
	}
}

func TestCachedEmbeddingClient_EmptyInput(t *testing.T) {
	inner := &countingEmbedder{dim: 4}
	cached := NewCachedEmbeddingClient(inner, 0)

	results, err := cached.Embed(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if results != nil {
		t.Fatalf("expected nil, got %v", results)
	}
	if inner.calls.Load() != 0 {
		t.Fatal("expected no inner calls for empty input")
	}
}

func TestCachedEmbeddingClient_Concurrent(t *testing.T) {
	inner := &countingEmbedder{dim: 4}
	cached := NewCachedEmbeddingClient(inner, 0)

	ctx := context.Background()
	var wg sync.WaitGroup
	const goroutines = 20

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = cached.Embed(ctx, []string{"shared-text", "another"})
		}()
	}
	wg.Wait()

	// All goroutines asked for the same texts, so at most goroutines calls
	// (no guarantee of exactly 1 due to races), but the cache must be consistent.
	if cached.Len() != 2 {
		t.Fatalf("expected 2 cached entries, got %d", cached.Len())
	}
}

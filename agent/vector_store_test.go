package agent

import (
	"math"
	"testing"
)

func TestVectorStoreUpsertAndGet(t *testing.T) {
	t.Parallel()
	store := NewVectorStore()

	vec := []float32{1.0, 0.0, 0.0}
	if err := store.Upsert("key1", "value1", vec); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	entry, ok := store.Get("key1")
	if !ok {
		t.Fatal("expected key1 to exist")
	}
	if entry.Key != "key1" || entry.Value != "value1" {
		t.Fatalf("unexpected entry: %+v", entry)
	}
	if len(entry.Vector) != 3 || entry.Vector[0] != 1.0 {
		t.Fatalf("unexpected vector: %v", entry.Vector)
	}

	// Update existing key.
	if err := store.Upsert("key1", "updated", vec); err != nil {
		t.Fatalf("upsert update: %v", err)
	}
	entry, _ = store.Get("key1")
	if entry.Value != "updated" {
		t.Fatalf("expected updated value, got %q", entry.Value)
	}
}

func TestVectorStoreGetNonExistent(t *testing.T) {
	t.Parallel()
	store := NewVectorStore()

	_, ok := store.Get("missing")
	if ok {
		t.Fatal("expected non-existent key to return false")
	}
}

func TestVectorStoreEmptyVector(t *testing.T) {
	t.Parallel()
	store := NewVectorStore()

	err := store.Upsert("key1", "value1", []float32{})
	if err == nil {
		t.Fatal("expected error for empty vector")
	}
}

func TestVectorStoreDimensionMismatch(t *testing.T) {
	t.Parallel()
	store := NewVectorStore()

	if err := store.Upsert("key1", "value1", []float32{1.0, 0.0}); err != nil {
		t.Fatalf("first upsert: %v", err)
	}
	err := store.Upsert("key2", "value2", []float32{1.0, 0.0, 0.0})
	if err == nil {
		t.Fatal("expected dimension mismatch error")
	}
}

func TestVectorStoreDelete(t *testing.T) {
	t.Parallel()
	store := NewVectorStore()

	store.Upsert("key1", "value1", []float32{1.0, 0.0})
	store.Delete("key1")

	_, ok := store.Get("key1")
	if ok {
		t.Fatal("expected key1 to be deleted")
	}

	// Deleting non-existent key should not error.
	if err := store.Delete("missing"); err != nil {
		t.Fatalf("delete non-existent: %v", err)
	}
}

func TestVectorStoreSearch(t *testing.T) {
	t.Parallel()
	store := NewVectorStore()

	// Insert three vectors in 2D space.
	store.Upsert("east", "pointing east", []float32{1.0, 0.0})
	store.Upsert("north", "pointing north", []float32{0.0, 1.0})
	store.Upsert("northeast", "pointing northeast", []float32{0.707, 0.707})

	// Search for something closest to east.
	results := store.Search([]float32{1.0, 0.0}, 3)
	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}
	// First result should be "east" (exact match, score ~1.0).
	if results[0].Key != "east" {
		t.Fatalf("expected first result to be 'east', got %q", results[0].Key)
	}
	if math.Abs(results[0].Score-1.0) > 0.001 {
		t.Fatalf("expected score ~1.0, got %f", results[0].Score)
	}
	// Second should be "northeast".
	if results[1].Key != "northeast" {
		t.Fatalf("expected second result to be 'northeast', got %q", results[1].Key)
	}
}

func TestVectorStoreSearchLimit(t *testing.T) {
	t.Parallel()
	store := NewVectorStore()

	store.Upsert("a", "val-a", []float32{1.0, 0.0})
	store.Upsert("b", "val-b", []float32{0.9, 0.1})
	store.Upsert("c", "val-c", []float32{0.0, 1.0})

	results := store.Search([]float32{1.0, 0.0}, 1)
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Key != "a" {
		t.Fatalf("expected 'a', got %q", results[0].Key)
	}
}

func TestVectorStoreSearchWrongDimension(t *testing.T) {
	t.Parallel()
	store := NewVectorStore()

	store.Upsert("key1", "value1", []float32{1.0, 0.0, 0.0})

	// Search with wrong dimension should return nil.
	results := store.Search([]float32{1.0, 0.0}, 10)
	if results != nil {
		t.Fatalf("expected nil results for dimension mismatch, got %v", results)
	}
}

func TestVectorStoreList(t *testing.T) {
	t.Parallel()
	store := NewVectorStore()

	store.Upsert("b", "val-b", []float32{0.0, 1.0})
	store.Upsert("a", "val-a", []float32{1.0, 0.0})

	entries := store.List()
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}
	// Should be sorted by key.
	if entries[0].Key != "a" || entries[1].Key != "b" {
		t.Fatalf("expected sorted keys, got %q, %q", entries[0].Key, entries[1].Key)
	}
}

func TestCosineSimilarityZeroVector(t *testing.T) {
	t.Parallel()
	zero := []float32{0.0, 0.0, 0.0}
	unit := []float32{1.0, 0.0, 0.0}

	score := cosineSimilarity(zero, unit)
	if score != 0 {
		t.Fatalf("expected 0 for zero vector, got %f", score)
	}
}

func TestCosineSimilarityIdentical(t *testing.T) {
	t.Parallel()
	v := []float32{0.5, 0.3, 0.7}

	score := cosineSimilarity(v, v)
	if math.Abs(score-1.0) > 0.0001 {
		t.Fatalf("expected 1.0 for identical vectors, got %f", score)
	}
}

func TestCosineSimilarityOrthogonal(t *testing.T) {
	t.Parallel()
	a := []float32{1.0, 0.0}
	b := []float32{0.0, 1.0}

	score := cosineSimilarity(a, b)
	if math.Abs(score) > 0.0001 {
		t.Fatalf("expected 0 for orthogonal vectors, got %f", score)
	}
}

// ---------------------------------------------------------------------------
// SearchMMR tests
// ---------------------------------------------------------------------------

func TestSearchMMRDiversity(t *testing.T) {
	t.Parallel()
	store := NewVectorStore()

	// Insert a cluster of similar vectors near "east" plus one outlier "north".
	// Regular top-K would return the cluster first; MMR should promote diversity.
	store.Upsert("east1", "east cluster 1", []float32{1.0, 0.0})
	store.Upsert("east2", "east cluster 2", []float32{0.99, 0.1})
	store.Upsert("east3", "east cluster 3", []float32{0.98, 0.15})
	store.Upsert("north", "pointing north", []float32{0.0, 1.0})

	// Query toward east with low lambda to favor diversity.
	results := store.SearchMMR([]float32{1.0, 0.0}, 3, 0.3)
	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}

	// First result should still be the most relevant (east1).
	if results[0].Key != "east1" {
		t.Fatalf("expected first result 'east1', got %q", results[0].Key)
	}

	// With diversity emphasis, "north" should appear in top 3 since the east
	// cluster members are very similar to each other.
	foundNorth := false
	for _, r := range results {
		if r.Key == "north" {
			foundNorth = true
			break
		}
	}
	if !foundNorth {
		keys := make([]string, len(results))
		for i, r := range results {
			keys[i] = r.Key
		}
		t.Fatalf("expected 'north' in MMR results for diversity, got %v", keys)
	}
}

func TestSearchMMRLambdaOnePureSimilarity(t *testing.T) {
	t.Parallel()
	store := NewVectorStore()

	store.Upsert("east", "pointing east", []float32{1.0, 0.0})
	store.Upsert("northeast", "pointing northeast", []float32{0.707, 0.707})
	store.Upsert("north", "pointing north", []float32{0.0, 1.0})

	// lambda=1.0 should behave like pure similarity search.
	mmrResults := store.SearchMMR([]float32{1.0, 0.0}, 3, 1.0)
	simResults := store.Search([]float32{1.0, 0.0}, 3)

	if len(mmrResults) != len(simResults) {
		t.Fatalf("expected same length, got mmr=%d sim=%d", len(mmrResults), len(simResults))
	}

	// Same ordering as regular search.
	for i := range mmrResults {
		if mmrResults[i].Key != simResults[i].Key {
			t.Fatalf("position %d: mmr=%q sim=%q", i, mmrResults[i].Key, simResults[i].Key)
		}
	}
}

func TestSearchMMRLambdaZeroMaxDiversity(t *testing.T) {
	t.Parallel()
	store := NewVectorStore()

	// Two very similar vectors and one orthogonal.
	store.Upsert("a", "val-a", []float32{1.0, 0.0})
	store.Upsert("a2", "val-a2", []float32{0.99, 0.1})
	store.Upsert("b", "val-b", []float32{0.0, 1.0})

	// lambda defaults to defaultMMRLambda (0.5) when 0, so pass a very small positive value.
	// Actually, per our implementation lambda <= 0 gets defaultMMRLambda, so test diversity
	// with a small lambda instead.
	results := store.SearchMMR([]float32{1.0, 0.0}, 3, 0.01)
	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}

	// First result should be most relevant.
	if results[0].Key != "a" {
		t.Fatalf("expected first result 'a', got %q", results[0].Key)
	}

	// Second result should be the diverse one ("b"), not the near-duplicate "a2".
	if results[1].Key != "b" {
		t.Fatalf("expected second result 'b' for max diversity, got %q", results[1].Key)
	}
}

func TestSearchMMRLimitExceedsEntries(t *testing.T) {
	t.Parallel()
	store := NewVectorStore()

	store.Upsert("a", "val-a", []float32{1.0, 0.0})
	store.Upsert("b", "val-b", []float32{0.0, 1.0})

	// Request more results than entries exist.
	results := store.SearchMMR([]float32{1.0, 0.0}, 10, 0.5)
	if len(results) != 2 {
		t.Fatalf("expected 2 results (all entries), got %d", len(results))
	}
}

func TestSearchMMRDefaultLambda(t *testing.T) {
	t.Parallel()
	store := NewVectorStore()

	store.Upsert("a", "val-a", []float32{1.0, 0.0})
	store.Upsert("b", "val-b", []float32{0.0, 1.0})

	// lambda=0 should use defaultMMRLambda (not panic or return empty).
	results := store.SearchMMR([]float32{1.0, 0.0}, 2, 0)
	if len(results) != 2 {
		t.Fatalf("expected 2 results with default lambda, got %d", len(results))
	}
}

func TestSearchMMREmptyStore(t *testing.T) {
	t.Parallel()
	store := NewVectorStore()

	results := store.SearchMMR([]float32{1.0, 0.0}, 5, 0.5)
	if results != nil {
		t.Fatalf("expected nil for empty store, got %v", results)
	}
}

func TestSearchMMRWrongDimension(t *testing.T) {
	t.Parallel()
	store := NewVectorStore()

	store.Upsert("a", "val-a", []float32{1.0, 0.0, 0.0})

	results := store.SearchMMR([]float32{1.0, 0.0}, 5, 0.5)
	if results != nil {
		t.Fatalf("expected nil for dimension mismatch, got %v", results)
	}
}

package agent

import (
	"fmt"
	"math"
	"sort"
	"sync"
	"time"
)

// ---------------------------------------------------------------------------
// Constants
// ---------------------------------------------------------------------------

const (
	// defaultVectorSearchLimit is the maximum number of results returned when
	// the caller does not specify a limit.
	defaultVectorSearchLimit = 10

	// defaultMMRLambda controls the relevance-diversity tradeoff for MMR search.
	// Higher values favor relevance; lower values favor diversity.
	defaultMMRLambda = 0.5
)

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

// VectorEntry stores a key-value pair alongside its vector embedding.
type VectorEntry struct {
	Key       string
	Value     string
	Vector    []float32
	CreatedAt time.Time
	UpdatedAt time.Time
}

// VectorSearchResult represents a single search hit with its similarity score.
type VectorSearchResult struct {
	Key       string
	Value     string
	Score     float64 // cosine similarity score
	CreatedAt time.Time
}

// VectorStore is a thread-safe in-memory vector store that supports
// upsert, delete, and cosine-similarity search over float32 embeddings.
type VectorStore struct {
	mu      sync.RWMutex
	entries map[string]*VectorEntry
	dim     int // vector dimension, auto-detected from first insert
}

// ---------------------------------------------------------------------------
// Constructor
// ---------------------------------------------------------------------------

// NewVectorStore creates an empty vector store.
func NewVectorStore() *VectorStore {
	return &VectorStore{
		entries: make(map[string]*VectorEntry),
	}
}

// ---------------------------------------------------------------------------
// Mutations
// ---------------------------------------------------------------------------

// Upsert inserts or updates a vector entry. All vectors must share the same
// dimensionality; the dimension is locked on first insert.
func (s *VectorStore) Upsert(key string, value string, vector []float32) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if len(vector) == 0 {
		return fmt.Errorf("vector must not be empty")
	}
	if s.dim == 0 {
		s.dim = len(vector)
	}
	if len(vector) != s.dim {
		return fmt.Errorf("vector dimension mismatch: expected %d, got %d", s.dim, len(vector))
	}

	now := time.Now().UTC()
	if existing, ok := s.entries[key]; ok {
		existing.Value = value
		existing.Vector = copyVector(vector)
		existing.UpdatedAt = now
	} else {
		s.entries[key] = &VectorEntry{
			Key:       key,
			Value:     value,
			Vector:    copyVector(vector),
			CreatedAt: now,
			UpdatedAt: now,
		}
	}
	return nil
}

// Delete removes a vector entry by key.
func (s *VectorStore) Delete(key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.entries, key)
	return nil
}

// ---------------------------------------------------------------------------
// Queries
// ---------------------------------------------------------------------------

// Search finds the top-k entries most similar to queryVector using cosine
// similarity. Results are returned in descending score order.
func (s *VectorStore) Search(queryVector []float32, limit int) []VectorSearchResult {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if limit <= 0 {
		limit = defaultVectorSearchLimit
	}
	if s.dim == 0 || len(queryVector) != s.dim {
		return nil
	}

	type scored struct {
		entry *VectorEntry
		score float64
	}

	candidates := make([]scored, 0, len(s.entries))
	for _, entry := range s.entries {
		score := cosineSimilarity(queryVector, entry.Vector)
		candidates = append(candidates, scored{entry: entry, score: score})
	}

	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].score > candidates[j].score
	})

	if limit > len(candidates) {
		limit = len(candidates)
	}

	results := make([]VectorSearchResult, limit)
	for i := 0; i < limit; i++ {
		results[i] = VectorSearchResult{
			Key:       candidates[i].entry.Key,
			Value:     candidates[i].entry.Value,
			Score:     candidates[i].score,
			CreatedAt: candidates[i].entry.CreatedAt,
		}
	}
	return results
}

// SearchMMR finds the top-k entries using Maximal Marginal Relevance, which
// balances relevance to the query with diversity among selected results.
//
// MMR(d) = lambda * sim(query, d) - (1-lambda) * max(sim(d, selected_d))
//
// lambda controls the relevance-diversity tradeoff: 1.0 is pure relevance
// (equivalent to regular Search), 0.0 maximizes diversity. When lambda <= 0,
// defaultMMRLambda is used.
func (s *VectorStore) SearchMMR(queryVector []float32, limit int, lambda float64) []VectorSearchResult {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if limit <= 0 {
		limit = defaultVectorSearchLimit
	}
	if lambda <= 0 {
		lambda = defaultMMRLambda
	}
	if lambda > 1 {
		lambda = 1
	}
	if s.dim == 0 || len(queryVector) != s.dim {
		return nil
	}

	// Pre-compute query similarity for every candidate.
	type candidate struct {
		entry    *VectorEntry
		querySim float64
	}
	candidates := make([]candidate, 0, len(s.entries))
	for _, entry := range s.entries {
		sim := cosineSimilarity(queryVector, entry.Vector)
		candidates = append(candidates, candidate{entry: entry, querySim: sim})
	}

	if limit > len(candidates) {
		limit = len(candidates)
	}

	selected := make([]VectorSearchResult, 0, limit)
	selectedVectors := make([][]float32, 0, limit)
	used := make(map[string]bool, limit)

	for i := 0; i < limit; i++ {
		bestIdx := -1
		bestMMR := math.Inf(-1)

		for ci, c := range candidates {
			if used[c.entry.Key] {
				continue
			}

			// Compute max similarity to already-selected results.
			var maxSimToSelected float64
			for _, sv := range selectedVectors {
				sim := cosineSimilarity(c.entry.Vector, sv)
				if sim > maxSimToSelected {
					maxSimToSelected = sim
				}
			}

			mmrScore := lambda*c.querySim - (1-lambda)*maxSimToSelected
			if mmrScore > bestMMR {
				bestMMR = mmrScore
				bestIdx = ci
			}
		}

		if bestIdx < 0 {
			break
		}

		chosen := candidates[bestIdx]
		used[chosen.entry.Key] = true
		selected = append(selected, VectorSearchResult{
			Key:       chosen.entry.Key,
			Value:     chosen.entry.Value,
			Score:     bestMMR,
			CreatedAt: chosen.entry.CreatedAt,
		})
		selectedVectors = append(selectedVectors, chosen.entry.Vector)
	}

	return selected
}

// Get returns a single entry by key.
func (s *VectorStore) Get(key string) (*VectorEntry, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	entry, ok := s.entries[key]
	if !ok {
		return nil, false
	}
	copied := *entry
	copied.Vector = copyVector(entry.Vector)
	return &copied, true
}

// List returns all entries in the store.
func (s *VectorStore) List() []*VectorEntry {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]*VectorEntry, 0, len(s.entries))
	for _, entry := range s.entries {
		copied := *entry
		copied.Vector = copyVector(entry.Vector)
		result = append(result, &copied)
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].Key < result[j].Key
	})
	return result
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// cosineSimilarity computes dot(a,b) / (norm(a) * norm(b)).
// Returns 0 when either vector has zero magnitude.
func cosineSimilarity(a, b []float32) float64 {
	if len(a) != len(b) {
		return 0
	}
	var dot, normA, normB float64
	for i := range a {
		ai := float64(a[i])
		bi := float64(b[i])
		dot += ai * bi
		normA += ai * ai
		normB += bi * bi
	}
	denom := math.Sqrt(normA) * math.Sqrt(normB)
	if denom == 0 {
		return 0
	}
	return dot / denom
}

// copyVector returns a defensive copy of a float32 slice.
func copyVector(v []float32) []float32 {
	if v == nil {
		return nil
	}
	out := make([]float32, len(v))
	copy(out, v)
	return out
}

package agent

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/fulcrus/hopclaw/durablefact"
	"github.com/fulcrus/hopclaw/logging"
)

// ---------------------------------------------------------------------------
// Constants
// ---------------------------------------------------------------------------

const (
	// defaultSemanticSearchLimit is the maximum number of results for semantic search.
	defaultSemanticSearchLimit = 10

	// defaultHybridSearchLimit is the maximum number of results for hybrid search.
	defaultHybridSearchLimit = 20
)

// MemoryState represents the lifecycle state of a memory entry.
type MemoryState string

const (
	MemoryActive     MemoryState = "active"
	MemorySuperseded MemoryState = "superseded"
)

// MemorySource constants for distinguishing who created the memory.
const (
	MemorySourceUser    = "user"
	MemorySourceAgent   = "agent"
	MemorySourceCompact = "compact"
)

// ---------------------------------------------------------------------------
// Interface
// ---------------------------------------------------------------------------

// MemoryStore provides persistent key-value storage for agent memory.
type MemoryStore interface {
	Get(ctx context.Context, key string) (*MemoryEntry, error)
	Set(ctx context.Context, key, value string) error
	Delete(ctx context.Context, key string) error
	Search(ctx context.Context, query string) ([]MemoryEntry, error)
	SemanticSearch(ctx context.Context, query string, limit int) ([]MemoryEntry, error)
	SemanticSearchMMR(ctx context.Context, query string, limit int, lambda float64) ([]MemoryEntry, error)
	List(ctx context.Context) ([]MemoryEntry, error)
}

// MemoryEntry is a single key-value pair in the memory store.
type MemoryEntry struct {
	Key                   string                `json:"key"`
	Value                 string                `json:"value"`
	SessionKey            string                `json:"session_key,omitempty"`
	ProjectID             string                `json:"project_id,omitempty"`
	FactClass             durablefact.FactClass `json:"fact_class,omitempty"`
	Namespace             string                `json:"namespace,omitempty"`
	ScopeKey              string                `json:"scope_key,omitempty"`
	Field                 string                `json:"field,omitempty"`
	Label                 string                `json:"label,omitempty"`
	Managed               bool                  `json:"managed,omitempty"`
	Source                string                `json:"source,omitempty"`
	Tags                  []string              `json:"tags,omitempty"`
	PreviousValues        []string              `json:"previous_values,omitempty"`
	EvidenceCount         int                   `json:"evidence_count,omitempty"`
	Score                 float64               `json:"score,omitempty"`
	State                 MemoryState           `json:"state,omitempty"`
	SupersededBy          string                `json:"superseded_by,omitempty"`
	MediaRefs             []string              `json:"media_refs,omitempty"`
	UsedCount             int                   `json:"used_count,omitempty"`
	LastUsedAt            time.Time             `json:"last_used_at,omitempty"`
	CorrectionCount       int                   `json:"correction_count,omitempty"`
	VerificationPassCount int                   `json:"verification_pass_count,omitempty"`
	VerificationFailCount int                   `json:"verification_fail_count,omitempty"`
	ConflictWith          string                `json:"conflict_with,omitempty"`
	ConflictSource        string                `json:"conflict_source,omitempty"`
	PendingWrite          bool                  `json:"pending_write,omitempty"`
	PendingWriteSource    string                `json:"pending_write_source,omitempty"`
	PendingWriteValue     string                `json:"pending_write_value,omitempty"`
	CreatedAt             time.Time             `json:"created_at"`
	UpdatedAt             time.Time             `json:"updated_at"`
}

// ---------------------------------------------------------------------------
// InMemoryKVStore
// ---------------------------------------------------------------------------

// InMemoryKVStore is an in-memory implementation of MemoryStore with optional
// vector search capabilities.
type InMemoryKVStore struct {
	mu              sync.RWMutex
	entries         map[string]*MemoryEntry
	embeddingClient EmbeddingClient // optional: enables semantic search
	vectorStore     *VectorStore    // optional: stores vector embeddings
}

func NewInMemoryKVStore() *InMemoryKVStore {
	return &InMemoryKVStore{
		entries: make(map[string]*MemoryEntry),
	}
}

func (s *InMemoryKVStore) StoreType() string {
	return "in-memory"
}

// SetEmbedding wires an embedding client and vector store for semantic search.
// When set, Set() will also generate and store embeddings, and Search() will
// use hybrid mode combining keyword and semantic results.
func (s *InMemoryKVStore) SetEmbedding(client EmbeddingClient) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.embeddingClient = client
	s.vectorStore = NewVectorStore()
}

// HasEmbedding reports whether semantic search is configured.
func (s *InMemoryKVStore) HasEmbedding() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.embeddingClient != nil && s.vectorStore != nil
}

func (s *InMemoryKVStore) EmbeddingClient() EmbeddingClient {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.embeddingClient
}

func (s *InMemoryKVStore) VectorStats() (int, int) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.vectorStore == nil {
		return 0, 0
	}
	return len(s.vectorStore.entries), s.vectorStore.dim
}

func (s *InMemoryKVStore) Reindex(ctx context.Context, force bool) (int, error) {
	entries, err := s.List(ctx)
	if err != nil {
		return 0, err
	}

	s.mu.RLock()
	embClient := s.embeddingClient
	s.mu.RUnlock()
	if embClient == nil {
		if force {
			s.mu.Lock()
			s.vectorStore = nil
			s.mu.Unlock()
		}
		return len(entries), nil
	}

	rebuilt := NewVectorStore()
	indexed := 0
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return indexed, err
		}
		vectors, err := embClient.Embed(ctx, []string{entry.Key + " " + entry.Value})
		if err != nil {
			return indexed, fmt.Errorf("embed %s: %w", entry.Key, err)
		}
		if len(vectors) == 0 {
			continue
		}
		if err := rebuilt.Upsert(entry.Key, entry.Value, vectors[0]); err != nil {
			return indexed, fmt.Errorf("index %s: %w", entry.Key, err)
		}
		indexed++
	}

	s.mu.Lock()
	s.vectorStore = rebuilt
	s.mu.Unlock()
	return indexed, nil
}

func (s *InMemoryKVStore) Get(_ context.Context, key string) (*MemoryEntry, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	entry, ok := s.entries[key]
	if !ok {
		return nil, nil
	}
	copied := *entry
	return &copied, nil
}

func (s *InMemoryKVStore) Set(ctx context.Context, key, value string) error {
	s.mu.Lock()
	now := time.Now().UTC()
	if existing, ok := s.entries[key]; ok {
		existing.Value = value
		existing.UpdatedAt = now
	} else {
		s.entries[key] = &MemoryEntry{
			Key:       key,
			Value:     value,
			CreatedAt: now,
			UpdatedAt: now,
		}
	}

	// Capture embedding deps before releasing the lock so callers cannot
	// race on SetEmbedding while we embed.
	embClient := s.embeddingClient
	vecStore := s.vectorStore
	s.mu.Unlock()

	// Generate embedding asynchronously in the same goroutine.
	if embClient != nil && vecStore != nil {
		textToEmbed := key + " " + value
		vectors, err := embClient.Embed(ctx, []string{textToEmbed})
		if err != nil {
			log.Warn("embedding generation failed", "key", key, "error", err)
			return nil
		}
		if len(vectors) > 0 {
			if err := vecStore.Upsert(key, value, vectors[0]); err != nil {
				log.Warn("vector store upsert failed", "key", key, "error", err)
				return nil
			}
		}
	}
	return nil
}

func (s *InMemoryKVStore) Delete(ctx context.Context, key string) error {
	s.mu.Lock()
	delete(s.entries, key)
	vecStore := s.vectorStore
	s.mu.Unlock()

	if vecStore != nil {
		logging.DebugIfErr(vecStore.Delete(key), "delete vector store entry", slog.String("key", key))
	}
	return nil
}

// Search performs keyword-based substring search. When an embedding client
// is configured, it automatically uses hybrid mode combining keyword matches
// with semantic results.
func (s *InMemoryKVStore) Search(ctx context.Context, query string) ([]MemoryEntry, error) {
	keywordResults := s.keywordSearch(query)

	// If embedding is not configured, return keyword-only results.
	s.mu.RLock()
	embClient := s.embeddingClient
	vecStore := s.vectorStore
	s.mu.RUnlock()

	if embClient == nil || vecStore == nil {
		return keywordResults, nil
	}

	// Hybrid mode: merge keyword + semantic results.
	semanticResults, err := s.semanticSearchInternal(ctx, embClient, vecStore, query, defaultHybridSearchLimit)
	if err != nil {
		// Fall back to keyword-only on embedding failure.
		return keywordResults, nil
	}

	return mergeResults(keywordResults, semanticResults), nil
}

// SemanticSearch performs vector-based semantic search. Returns an error if
// embedding is not configured.
func (s *InMemoryKVStore) SemanticSearch(ctx context.Context, query string, limit int) ([]MemoryEntry, error) {
	s.mu.RLock()
	embClient := s.embeddingClient
	vecStore := s.vectorStore
	s.mu.RUnlock()

	if embClient == nil || vecStore == nil {
		return nil, fmt.Errorf("embedding client not configured")
	}

	if limit <= 0 {
		limit = defaultSemanticSearchLimit
	}

	return s.semanticSearchInternal(ctx, embClient, vecStore, query, limit)
}

// SemanticSearchMMR performs MMR-based semantic search, balancing relevance
// and diversity. Returns an error if embedding is not configured.
func (s *InMemoryKVStore) SemanticSearchMMR(ctx context.Context, query string, limit int, lambda float64) ([]MemoryEntry, error) {
	s.mu.RLock()
	embClient := s.embeddingClient
	vecStore := s.vectorStore
	s.mu.RUnlock()

	if embClient == nil || vecStore == nil {
		return nil, fmt.Errorf("embedding client not configured")
	}

	if limit <= 0 {
		limit = defaultSemanticSearchLimit
	}

	vectors, err := embClient.Embed(ctx, []string{query})
	if err != nil {
		return nil, fmt.Errorf("failed to embed query: %w", err)
	}
	if len(vectors) == 0 {
		return nil, nil
	}

	hits := vecStore.SearchMMR(vectors[0], limit, lambda)
	results := make([]MemoryEntry, 0, len(hits))
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, hit := range hits {
		if entry, ok := s.entries[hit.Key]; ok {
			e := *entry
			e.Score = hit.Score
			results = append(results, e)
		}
	}
	return results, nil
}

func (s *InMemoryKVStore) List(_ context.Context) ([]MemoryEntry, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	results := make([]MemoryEntry, 0, len(s.entries))
	for _, entry := range s.entries {
		results = append(results, *entry)
	}
	sort.Slice(results, func(i, j int) bool {
		return results[i].Key < results[j].Key
	})
	return results, nil
}

// ---------------------------------------------------------------------------
// Internal helpers
// ---------------------------------------------------------------------------

// keywordSearch performs substring matching on key and value fields.
func (s *InMemoryKVStore) keywordSearch(query string) []MemoryEntry {
	s.mu.RLock()
	defer s.mu.RUnlock()
	lowerQuery := strings.ToLower(query)
	var results []MemoryEntry
	for _, entry := range s.entries {
		if strings.Contains(strings.ToLower(entry.Key), lowerQuery) ||
			strings.Contains(strings.ToLower(entry.Value), lowerQuery) {
			results = append(results, *entry)
		}
	}
	sort.Slice(results, func(i, j int) bool {
		return results[i].Key < results[j].Key
	})
	return results
}

// semanticSearchInternal embeds the query and searches the vector store.
func (s *InMemoryKVStore) semanticSearchInternal(
	ctx context.Context,
	embClient EmbeddingClient,
	vecStore *VectorStore,
	query string,
	limit int,
) ([]MemoryEntry, error) {
	vectors, err := embClient.Embed(ctx, []string{query})
	if err != nil {
		return nil, fmt.Errorf("failed to embed query: %w", err)
	}
	if len(vectors) == 0 {
		return nil, nil
	}

	hits := vecStore.Search(vectors[0], limit)
	results := make([]MemoryEntry, 0, len(hits))
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, hit := range hits {
		if entry, ok := s.entries[hit.Key]; ok {
			e := *entry
			e.Score = hit.Score
			results = append(results, e)
		}
	}
	return results, nil
}

// mergeResults combines keyword and semantic search results, deduplicating by key.
// Keyword matches appear first (preserving order), followed by unique semantic results.
func mergeResults(keyword, semantic []MemoryEntry) []MemoryEntry {
	seen := make(map[string]struct{}, len(keyword)+len(semantic))
	var merged []MemoryEntry

	for _, e := range keyword {
		if _, ok := seen[e.Key]; !ok {
			seen[e.Key] = struct{}{}
			merged = append(merged, e)
		}
	}
	for _, e := range semantic {
		if _, ok := seen[e.Key]; !ok {
			seen[e.Key] = struct{}{}
			merged = append(merged, e)
		}
	}
	return merged
}

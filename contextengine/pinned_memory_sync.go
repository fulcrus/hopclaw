package contextengine

import (
	"context"
	"strings"
	"time"
)

const pinnedMemoryKeyPrefix = "pinned:"

// MemorySearchResult represents a single result from a memory search.
type MemorySearchResult struct {
	Key   string
	Value string
}

// MemoryReader is a minimal interface for reading memory entries.
// It is intentionally smaller than agent.MemoryStore to avoid import cycles.
type MemoryReader interface {
	Search(ctx context.Context, query string) ([]MemorySearchResult, error)
}

// SyncPinnedFactsToMemory writes pinned facts that have a key to the memory store,
// making them durable across compaction and session boundaries.
func SyncPinnedFactsToMemory(ctx context.Context, facts []PinnedFact, writer MemoryWriter) int {
	if writer == nil || len(facts) == 0 {
		return 0
	}
	var count int
	for _, f := range facts {
		if f.Key == "" || f.Content == "" {
			continue
		}
		key := pinnedMemoryKeyPrefix + f.Key
		if err := writer.Set(ctx, key, f.Content); err != nil {
			log.Warn("failed to sync pinned fact to memory", "key", f.Key, "error", err)
			continue
		}
		count++
	}
	return count
}

// LoadPinnedFactsFromMemory retrieves durable pinned facts from the memory store.
func LoadPinnedFactsFromMemory(ctx context.Context, reader MemoryReader) []PinnedFact {
	if reader == nil {
		return nil
	}
	results, err := reader.Search(ctx, pinnedMemoryKeyPrefix)
	if err != nil {
		log.Warn("failed to load pinned facts from memory", "error", err)
		return nil
	}
	var facts []PinnedFact
	for _, r := range results {
		if !strings.HasPrefix(r.Key, pinnedMemoryKeyPrefix) {
			continue
		}
		if strings.TrimSpace(r.Value) == "" {
			continue
		}
		facts = append(facts, PinnedFact{
			Key:       strings.TrimPrefix(r.Key, pinnedMemoryKeyPrefix),
			Content:   r.Value,
			Source:    "memory",
			UpdatedAt: time.Now().UTC(),
		})
	}
	return facts
}

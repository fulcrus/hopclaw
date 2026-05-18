package artifact

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/fulcrus/hopclaw/internal/meta"
)

type ListFilter struct {
	Kind       string    `json:"kind,omitempty"`
	RunID      string    `json:"run_id,omitempty"`
	SessionID  string    `json:"session_id,omitempty"`
	ToolName   string    `json:"tool_name,omitempty"`
	ToolCallID string    `json:"tool_call_id,omitempty"`
	Before     time.Time `json:"before,omitempty"`
	Limit      int       `json:"limit,omitempty"`
}

type PruneResult struct {
	DeletedCount int       `json:"deleted_count"`
	DeletedIDs   []string  `json:"deleted_ids,omitempty"`
	Cutoff       time.Time `json:"cutoff,omitempty"`
}

func (f ListFilter) HasSelector() bool {
	return strings.TrimSpace(f.Kind) != "" ||
		strings.TrimSpace(f.RunID) != "" ||
		strings.TrimSpace(f.SessionID) != "" ||
		strings.TrimSpace(f.ToolName) != "" ||
		strings.TrimSpace(f.ToolCallID) != ""
}

func (s *InMemoryStore) List(_ context.Context, filter ListFilter) ([]*Blob, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*Blob, 0, len(s.blobs))
	for _, blob := range s.blobs {
		if !matchesFilter(blob, filter) {
			continue
		}
		out = append(out, cloneBlob(blob))
	}
	sortBlobs(out)
	if filter.Limit > 0 && len(out) > filter.Limit {
		out = out[:filter.Limit]
	}
	return out, nil
}

func (s *FileStore) List(_ context.Context, filter ListFilter) ([]*Blob, error) {
	entries, err := os.ReadDir(s.root)
	if err != nil {
		return nil, err
	}
	out := make([]*Blob, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(s.root, entry.Name()))
		if err != nil {
			return nil, err
		}
		var blob Blob
		if err := json.Unmarshal(data, &blob); err != nil {
			return nil, fmt.Errorf("decode artifact metadata %q: %w", entry.Name(), err)
		}
		if !matchesFilter(&blob, filter) {
			continue
		}
		out = append(out, cloneBlob(&blob))
	}
	sortBlobs(out)
	if filter.Limit > 0 && len(out) > filter.Limit {
		out = out[:filter.Limit]
	}
	return out, nil
}

func Prune(ctx context.Context, store Store, filter ListFilter) (*PruneResult, error) {
	items, err := store.List(ctx, filter)
	if err != nil {
		return nil, err
	}
	result := &PruneResult{
		DeletedIDs: make([]string, 0, len(items)),
		Cutoff:     filter.Before,
	}
	for _, item := range items {
		if err := store.Delete(ctx, item.ID); err != nil {
			return nil, err
		}
		result.DeletedIDs = append(result.DeletedIDs, item.ID)
	}
	result.DeletedCount = len(result.DeletedIDs)
	return result, nil
}

func matchesFilter(blob *Blob, filter ListFilter) bool {
	if blob == nil {
		return false
	}
	if kind := strings.TrimSpace(filter.Kind); kind != "" && blob.Kind != kind {
		return false
	}
	if !filter.Before.IsZero() && !blob.CreatedAt.Before(filter.Before) {
		return false
	}
	if filter.RunID != "" && metadataString(blob.Metadata, meta.KeyRunID) != filter.RunID {
		return false
	}
	if filter.SessionID != "" && metadataString(blob.Metadata, meta.KeySessionID) != filter.SessionID {
		return false
	}
	if filter.ToolName != "" && metadataString(blob.Metadata, meta.KeyToolName) != filter.ToolName {
		return false
	}
	if filter.ToolCallID != "" && metadataString(blob.Metadata, "tool_call_id") != filter.ToolCallID {
		return false
	}
	return true
}

func metadataString(metadata map[string]any, key string) string {
	if metadata == nil {
		return ""
	}
	value, ok := metadata[key]
	if !ok || value == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(value))
}

func sortBlobs(blobs []*Blob) {
	sort.Slice(blobs, func(i, j int) bool {
		if blobs[i].CreatedAt.Equal(blobs[j].CreatedAt) {
			return blobs[i].ID < blobs[j].ID
		}
		return blobs[i].CreatedAt.Before(blobs[j].CreatedAt)
	})
}

package artifact

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestInMemoryStoreListAndPrune(t *testing.T) {
	t.Parallel()

	store := NewInMemoryStore()
	oldBlob, err := store.Put(context.Background(), PutRequest{
		Body: []byte("old"),
		Metadata: map[string]any{
			"run_id":       "run-1",
			"session_id":   "sess-1",
			"tool_name":    "fs.read",
			"tool_call_id": "call-1",
		},
	})
	if err != nil {
		t.Fatalf("Put(old) error = %v", err)
	}
	newBlob, err := store.Put(context.Background(), PutRequest{
		Body: []byte("new"),
		Metadata: map[string]any{
			"run_id":     "run-1",
			"session_id": "sess-1",
			"tool_name":  "fs.write",
		},
	})
	if err != nil {
		t.Fatalf("Put(new) error = %v", err)
	}
	store.mu.Lock()
	store.blobs[oldBlob.ID].CreatedAt = time.Now().UTC().Add(-48 * time.Hour)
	store.blobs[newBlob.ID].CreatedAt = time.Now().UTC().Add(-time.Hour)
	store.mu.Unlock()

	items, err := store.List(context.Background(), ListFilter{RunID: "run-1"})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("len(items) = %d", len(items))
	}
	if items[0].ID != oldBlob.ID {
		t.Fatalf("items[0].ID = %q, want %q", items[0].ID, oldBlob.ID)
	}

	result, err := Prune(context.Background(), store, ListFilter{
		RunID:  "run-1",
		Before: time.Now().UTC().Add(-24 * time.Hour),
	})
	if err != nil {
		t.Fatalf("Prune() error = %v", err)
	}
	if result.DeletedCount != 1 || len(result.DeletedIDs) != 1 || result.DeletedIDs[0] != oldBlob.ID {
		t.Fatalf("Prune() = %#v", result)
	}
	remaining, err := store.List(context.Background(), ListFilter{RunID: "run-1"})
	if err != nil {
		t.Fatalf("List(remaining) error = %v", err)
	}
	if len(remaining) != 1 || remaining[0].ID != newBlob.ID {
		t.Fatalf("remaining = %#v", remaining)
	}
}

func TestFileStoreListAndDelete(t *testing.T) {
	t.Parallel()

	store, err := NewFileStore(filepath.Join(t.TempDir(), "artifacts"))
	if err != nil {
		t.Fatalf("NewFileStore() error = %v", err)
	}
	blob, err := store.Put(context.Background(), PutRequest{
		Body: []byte("payload"),
		Metadata: map[string]any{
			"run_id":    "run-2",
			"tool_name": "git.diff",
		},
	})
	if err != nil {
		t.Fatalf("Put() error = %v", err)
	}

	metaPath := filepath.Join(store.root, blob.ID+".json")
	data, err := os.ReadFile(metaPath)
	if err != nil {
		t.Fatalf("ReadFile(metadata) error = %v", err)
	}
	var meta Blob
	if err := json.Unmarshal(data, &meta); err != nil {
		t.Fatalf("Unmarshal(metadata) error = %v", err)
	}
	meta.CreatedAt = time.Now().UTC().Add(-2 * time.Hour)
	encoded, err := json.Marshal(meta)
	if err != nil {
		t.Fatalf("Marshal(metadata) error = %v", err)
	}
	if err := os.WriteFile(metaPath, encoded, 0o644); err != nil {
		t.Fatalf("WriteFile(metadata) error = %v", err)
	}

	items, err := store.List(context.Background(), ListFilter{
		RunID:  "run-2",
		Before: time.Now().UTC().Add(-time.Hour),
	})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(items) != 1 || items[0].ID != blob.ID {
		t.Fatalf("items = %#v", items)
	}
	if err := store.Delete(context.Background(), blob.ID); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	if _, err := store.Get(context.Background(), blob.ID); err == nil {
		t.Fatal("expected deleted artifact lookup to fail")
	}
}

func TestListFilterBeforeIsStrictlyEarlierThanCutoff(t *testing.T) {
	t.Parallel()

	cutoff := time.Now().UTC()
	equalBlob := &Blob{CreatedAt: cutoff}
	beforeBlob := &Blob{CreatedAt: cutoff.Add(-time.Nanosecond)}
	afterBlob := &Blob{CreatedAt: cutoff.Add(time.Nanosecond)}

	filter := ListFilter{Before: cutoff}
	if matchesFilter(equalBlob, filter) {
		t.Fatal("expected blob at cutoff to be excluded")
	}
	if !matchesFilter(beforeBlob, filter) {
		t.Fatal("expected blob before cutoff to match")
	}
	if matchesFilter(afterBlob, filter) {
		t.Fatal("expected blob after cutoff to be excluded")
	}
}

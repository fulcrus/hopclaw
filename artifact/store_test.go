package artifact

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestInMemoryStorePutGetRead(t *testing.T) {
	t.Parallel()

	store := NewInMemoryStore()
	blob, err := store.Put(context.Background(), PutRequest{
		Kind:        "tool_output",
		ContentType: "text/plain",
		Body:        []byte("hello"),
		Metadata: map[string]any{
			"run_id": "run-1",
		},
	})
	if err != nil {
		t.Fatalf("Put() error = %v", err)
	}
	if blob.URI == "" {
		t.Fatal("expected URI")
	}
	got, err := store.Get(context.Background(), blob.ID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got.Metadata["run_id"] != "run-1" {
		t.Fatalf("Get().Metadata = %#v", got.Metadata)
	}
	body, contentType, err := store.Read(context.Background(), blob.URI)
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}
	if string(body) != "hello" || contentType != "text/plain" {
		t.Fatalf("Read() = %q, %q", string(body), contentType)
	}
}

func TestFileStoreRoundTrip(t *testing.T) {
	t.Parallel()

	store, err := NewFileStore(filepath.Join(t.TempDir(), "artifacts"))
	if err != nil {
		t.Fatalf("NewFileStore() error = %v", err)
	}
	blob, err := store.Put(context.Background(), PutRequest{
		Body: []byte("payload"),
	})
	if err != nil {
		t.Fatalf("Put() error = %v", err)
	}
	got, err := store.Get(context.Background(), blob.URI)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got.ID != blob.ID {
		t.Fatalf("Get().ID = %q, want %q", got.ID, blob.ID)
	}
	body, _, err := store.Read(context.Background(), blob.ID)
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}
	if string(body) != "payload" {
		t.Fatalf("Read() body = %q", string(body))
	}
}

func TestFileStoreRejectsInvalidMetadataWithoutWritingBody(t *testing.T) {
	t.Parallel()

	root := filepath.Join(t.TempDir(), "artifacts")
	store, err := NewFileStore(root)
	if err != nil {
		t.Fatalf("NewFileStore() error = %v", err)
	}
	if _, err := store.Put(context.Background(), PutRequest{
		Body:     []byte("payload"),
		Metadata: map[string]any{"bad": func() {}},
	}); err == nil {
		t.Fatal("expected Put() to reject non-serializable metadata")
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatalf("ReadDir() error = %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("unexpected artifact files: %+v", entries)
	}
}

func TestNewFileStoreRepairsIncompleteArtifacts(t *testing.T) {
	t.Parallel()

	root := filepath.Join(t.TempDir(), "artifacts")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}

	blob := Blob{
		ID:          "keep",
		URI:         URI("keep"),
		Kind:        "tool_output",
		ContentType: "text/plain",
		Size:        4,
		CreatedAt:   time.Unix(123, 0).UTC(),
	}
	meta, err := json.Marshal(blob)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "keep.json"), meta, 0o644); err != nil {
		t.Fatalf("WriteFile(keep.json) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "keep.bin"), []byte("keep"), 0o644); err != nil {
		t.Fatalf("WriteFile(keep.bin) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "orphan.bin"), []byte("orphan"), 0o644); err != nil {
		t.Fatalf("WriteFile(orphan.bin) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "dangling.json"), []byte(`{"id":"dangling"}`), 0o644); err != nil {
		t.Fatalf("WriteFile(dangling.json) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "staged.bin.tmp"), []byte("tmp"), 0o644); err != nil {
		t.Fatalf("WriteFile(staged.bin.tmp) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "staged.json.tmp"), []byte("tmp"), 0o644); err != nil {
		t.Fatalf("WriteFile(staged.json.tmp) error = %v", err)
	}

	store, err := NewFileStore(root)
	if err != nil {
		t.Fatalf("NewFileStore() error = %v", err)
	}
	body, _, err := store.Read(context.Background(), "keep")
	if err != nil {
		t.Fatalf("Read(keep) error = %v", err)
	}
	if string(body) != "keep" {
		t.Fatalf("Read(keep) body = %q", string(body))
	}

	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatalf("ReadDir() error = %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected repaired artifact root to keep only valid pair, got %+v", entries)
	}
}

func TestNewFileStorePromotesMetaFirstCrashWindow(t *testing.T) {
	t.Parallel()

	root := filepath.Join(t.TempDir(), "artifacts")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}

	blob := Blob{
		ID:          "recover-meta-first",
		URI:         URI("recover-meta-first"),
		Kind:        "tool_output",
		ContentType: "text/plain",
		Size:        7,
		CreatedAt:   time.Unix(456, 0).UTC(),
	}
	meta, err := json.Marshal(blob)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "recover-meta-first.json"), meta, 0o644); err != nil {
		t.Fatalf("WriteFile(meta) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "recover-meta-first.bin.tmp"), []byte("payload"), 0o644); err != nil {
		t.Fatalf("WriteFile(temp body) error = %v", err)
	}

	store, err := NewFileStore(root)
	if err != nil {
		t.Fatalf("NewFileStore() error = %v", err)
	}
	body, _, err := store.Read(context.Background(), "recover-meta-first")
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}
	if string(body) != "payload" {
		t.Fatalf("Read() body = %q", string(body))
	}
}

func TestNewFileStorePromotesBodyFirstCrashWindow(t *testing.T) {
	t.Parallel()

	root := filepath.Join(t.TempDir(), "artifacts")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}

	blob := Blob{
		ID:          "recover-body-first",
		URI:         URI("recover-body-first"),
		Kind:        "tool_output",
		ContentType: "text/plain",
		Size:        7,
		CreatedAt:   time.Unix(789, 0).UTC(),
	}
	meta, err := json.Marshal(blob)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "recover-body-first.bin"), []byte("payload"), 0o644); err != nil {
		t.Fatalf("WriteFile(body) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "recover-body-first.json.tmp"), meta, 0o644); err != nil {
		t.Fatalf("WriteFile(temp meta) error = %v", err)
	}

	store, err := NewFileStore(root)
	if err != nil {
		t.Fatalf("NewFileStore() error = %v", err)
	}
	body, _, err := store.Read(context.Background(), "recover-body-first")
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}
	if string(body) != "payload" {
		t.Fatalf("Read() body = %q", string(body))
	}
}

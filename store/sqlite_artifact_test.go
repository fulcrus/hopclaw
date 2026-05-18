package store

import (
	"bytes"
	"context"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/fulcrus/hopclaw/artifact"
)

func TestSQLiteArtifactStoreRejectsInvalidMetadataWithoutWritingBody(t *testing.T) {
	t.Parallel()

	db := openRawTestDB(t)
	root := filepath.Join(t.TempDir(), "artifacts")
	store, err := NewSQLiteArtifactStore(db, root)
	if err != nil {
		t.Fatalf("NewSQLiteArtifactStore() error = %v", err)
	}

	if _, err := store.Put(context.Background(), artifact.PutRequest{
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

func TestSQLiteArtifactStoreCleansUpTempBodyWhenInsertFails(t *testing.T) {
	t.Parallel()

	db := openRawTestDB(t)
	root := filepath.Join(t.TempDir(), "artifacts")
	store, err := NewSQLiteArtifactStore(db, root)
	if err != nil {
		t.Fatalf("NewSQLiteArtifactStore() error = %v", err)
	}
	if _, err := db.Exec(`DROP TABLE artifacts`); err != nil {
		t.Fatalf("DROP TABLE artifacts: %v", err)
	}

	if _, err := store.Put(context.Background(), artifact.PutRequest{
		Body: []byte("payload"),
	}); err == nil {
		t.Fatal("expected Put() to fail when artifacts table is missing")
	}

	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatalf("ReadDir() error = %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("unexpected artifact files after failed insert: %+v", entries)
	}
}

func TestSQLiteArtifactStoreDoesNotRetainRowWhenRenameFails(t *testing.T) {
	t.Parallel()

	db := openRawTestDB(t)
	root := filepath.Join(t.TempDir(), "artifacts")
	store, err := NewSQLiteArtifactStore(db, root)
	if err != nil {
		t.Fatalf("NewSQLiteArtifactStore() error = %v", err)
	}

	seed := []byte{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11}
	store.rng = bytes.NewReader(seed)

	id := hex.EncodeToString(seed)
	bodyPath := filepath.Join(root, id+".bin")
	if err := os.MkdirAll(bodyPath, 0o755); err != nil {
		t.Fatalf("MkdirAll(conflicting body path) error = %v", err)
	}

	if _, err := store.Put(context.Background(), artifact.PutRequest{
		Body: []byte("payload"),
	}); err == nil {
		t.Fatal("expected Put() to fail when final body rename conflicts with a directory")
	}

	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM artifacts WHERE id = ?`, id).Scan(&count); err != nil {
		t.Fatalf("QueryRow(count) error = %v", err)
	}
	if count != 0 {
		t.Fatalf("artifact row count = %d, want 0", count)
	}
	if _, err := os.Stat(filepath.Join(root, id+".bin.tmp")); !os.IsNotExist(err) {
		t.Fatalf("temp body cleanup err = %v, want not exists", err)
	}
}

func TestNewSQLiteArtifactStoreRemovesOrphanedBodies(t *testing.T) {
	t.Parallel()

	db := openRawTestDB(t)
	root := filepath.Join(t.TempDir(), "artifacts")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if _, err := db.Exec(
		`INSERT INTO artifacts (id, uri, kind, content_type, size, run_id, session_id, tool_name, tool_call_id, metadata, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		"keep", "artifact://local/keep", "tool_output", "text/plain", 4, "", "", "", "", "{}", formatTime(time.Now().UTC()),
	); err != nil {
		t.Fatalf("insert artifact metadata: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "keep.bin"), []byte("keep"), 0o644); err != nil {
		t.Fatalf("WriteFile(keep.bin) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "orphan.bin"), []byte("orphan"), 0o644); err != nil {
		t.Fatalf("WriteFile(orphan.bin) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "staged.bin.tmp"), []byte("tmp"), 0o644); err != nil {
		t.Fatalf("WriteFile(staged.bin.tmp) error = %v", err)
	}

	if _, err := NewSQLiteArtifactStore(db, root); err != nil {
		t.Fatalf("NewSQLiteArtifactStore() repair pass error = %v", err)
	}

	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatalf("ReadDir() error = %v", err)
	}
	if len(entries) != 1 || entries[0].Name() != "keep.bin" {
		t.Fatalf("unexpected artifact files after repair: %+v", entries)
	}
}

func TestNewSQLiteArtifactStorePromotesCommittedTempBody(t *testing.T) {
	t.Parallel()

	db := openRawTestDB(t)
	root := filepath.Join(t.TempDir(), "artifacts")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if _, err := db.Exec(
		`INSERT INTO artifacts (id, uri, kind, content_type, size, run_id, session_id, tool_name, tool_call_id, metadata, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		"recover", "artifact://local/recover", "tool_output", "text/plain", 7, "", "", "", "", "{}", formatTime(time.Now().UTC()),
	); err != nil {
		t.Fatalf("insert artifact metadata: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "recover.bin.tmp"), []byte("payload"), 0o644); err != nil {
		t.Fatalf("WriteFile(recover.bin.tmp) error = %v", err)
	}

	store, err := NewSQLiteArtifactStore(db, root)
	if err != nil {
		t.Fatalf("NewSQLiteArtifactStore() error = %v", err)
	}
	body, _, err := store.Read(context.Background(), "recover")
	if err != nil {
		t.Fatalf("Read(recover) error = %v", err)
	}
	if string(body) != "payload" {
		t.Fatalf("Read(recover) body = %q", string(body))
	}
}

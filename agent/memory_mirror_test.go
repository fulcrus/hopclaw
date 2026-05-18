package agent

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fulcrus/hopclaw/durablefact"
)

func TestMirroredMemoryStoreWritesNotebook(t *testing.T) {
	ctx := context.Background()
	notebookPath := filepath.Join(t.TempDir(), "memory", "MEMORY.md")
	store, err := NewMirroredMemoryStore(NewInMemoryKVStore(), notebookPath)
	if err != nil {
		t.Fatalf("NewMirroredMemoryStore() error = %v", err)
	}
	if err := store.Set(ctx, "user.name", "Alice"); err != nil {
		t.Fatalf("Set() error = %v", err)
	}
	if err := store.Set(ctx, "project.name", "HopClaw"); err != nil {
		t.Fatalf("Set() error = %v", err)
	}
	body, err := os.ReadFile(notebookPath)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	content := string(body)
	if !strings.Contains(content, "# HopClaw Memory") {
		t.Fatalf("notebook missing title: %s", content)
	}
	if !strings.Contains(content, "## Project") || !strings.Contains(content, "## User") {
		t.Fatalf("notebook missing sections: %s", content)
	}
	if !strings.Contains(content, "HopClaw") || !strings.Contains(content, "Alice") {
		t.Fatalf("notebook missing values: %s", content)
	}
}

func TestMirroredMemoryStoreSetIgnoresNotebookSyncFailure(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store, err := NewMirroredMemoryStore(NewInMemoryKVStore(), brokenNotebookPath(t))
	if err != nil {
		t.Fatalf("NewMirroredMemoryStore() error = %v", err)
	}
	if err := store.Set(ctx, "user.name", "Alice"); err != nil {
		t.Fatalf("Set() error = %v", err)
	}
	entry, err := store.Get(ctx, "user.name")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if entry == nil || entry.Value != "Alice" {
		t.Fatalf("unexpected entry after Set(): %#v", entry)
	}
}

func TestMirroredMemoryStoreDeleteIgnoresNotebookSyncFailure(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store, err := NewMirroredMemoryStore(NewInMemoryKVStore(), brokenNotebookPath(t))
	if err != nil {
		t.Fatalf("NewMirroredMemoryStore() error = %v", err)
	}
	if err := store.Set(ctx, "user.name", "Alice"); err != nil {
		t.Fatalf("Set() error = %v", err)
	}
	if err := store.Delete(ctx, "user.name"); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	entry, err := store.Get(ctx, "user.name")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if entry != nil {
		t.Fatalf("expected entry to be deleted, got %#v", entry)
	}
}

func TestMirroredMemoryStoreUpsertRecordIgnoresNotebookSyncFailure(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store, err := NewMirroredMemoryStore(NewGovernedMemoryStore(NewInMemoryKVStore()), brokenNotebookPath(t))
	if err != nil {
		t.Fatalf("NewMirroredMemoryStore() error = %v", err)
	}
	entry, err := store.UpsertRecord(ctx, MemoryRecord{
		Namespace: memoryNamespaceProfile,
		ScopeKey:  "user",
		Field:     "name",
		Label:     "User Name",
		Value:     "Alice",
	})
	if err != nil {
		t.Fatalf("UpsertRecord() error = %v", err)
	}
	if entry == nil || entry.Value != "Alice" {
		t.Fatalf("unexpected entry after UpsertRecord(): %#v", entry)
	}
	got, err := store.Get(ctx, entry.Key)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got == nil || got.Value != "Alice" {
		t.Fatalf("unexpected stored entry after UpsertRecord(): %#v", got)
	}
}

func TestMirroredMemoryStoreDelegatesDurableViews(t *testing.T) {
	ctx := context.Background()
	notebookPath := filepath.Join(t.TempDir(), "memory", "MEMORY.md")
	store, err := NewMirroredMemoryStore(NewGovernedMemoryStore(newTestSQLiteStore(t)), notebookPath)
	if err != nil {
		t.Fatalf("NewMirroredMemoryStore() error = %v", err)
	}
	if _, err := store.UpsertRecord(ctx, MemoryRecord{
		Namespace: memoryNamespaceProfile,
		ScopeKey:  "user",
		Field:     "timezone",
		Value:     "Asia/Shanghai",
	}); err != nil {
		t.Fatalf("UpsertRecord() error = %v", err)
	}

	contextViews, err := store.ListContextViews(ctx, durablefact.Filter{Namespace: memoryNamespaceProfile})
	if err != nil {
		t.Fatalf("ListContextViews() error = %v", err)
	}
	if len(contextViews) != 1 || contextViews[0].Field != "timezone" {
		t.Fatalf("unexpected context views: %#v", contextViews)
	}

	operatorViews, err := store.ListOperatorViews(ctx, durablefact.Filter{Namespace: memoryNamespaceProfile})
	if err != nil {
		t.Fatalf("ListOperatorViews() error = %v", err)
	}
	if len(operatorViews) != 1 || operatorViews[0].ViewType != durablefact.ViewTypeContext {
		t.Fatalf("unexpected operator views: %#v", operatorViews)
	}
}

func TestNewMirroredMemoryStoreRejectsNilInnerStore(t *testing.T) {
	t.Parallel()

	store, err := NewMirroredMemoryStore(nil, filepath.Join(t.TempDir(), "MEMORY.md"))
	if err == nil {
		t.Fatal("NewMirroredMemoryStore() error = nil, want failure for nil inner store")
	}
	if store != nil {
		t.Fatalf("NewMirroredMemoryStore() store = %#v, want nil on error", store)
	}
}

func brokenNotebookPath(t *testing.T) string {
	t.Helper()

	parent := filepath.Join(t.TempDir(), "not-a-dir")
	if err := os.WriteFile(parent, []byte("occupied"), 0o644); err != nil {
		t.Fatalf("WriteFile(%s) error = %v", parent, err)
	}
	return filepath.Join(parent, "MEMORY.md")
}

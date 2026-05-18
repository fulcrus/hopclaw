package agent

import (
	"context"
	"testing"
)

func TestInMemoryKVStoreGetSetDelete(t *testing.T) {
	t.Parallel()
	store := NewInMemoryKVStore()
	ctx := context.Background()

	// Get non-existent.
	entry, err := store.Get(ctx, "key1")
	if err != nil {
		t.Fatal(err)
	}
	if entry != nil {
		t.Fatal("expected nil for non-existent key")
	}

	// Set.
	if err := store.Set(ctx, "key1", "value1"); err != nil {
		t.Fatal(err)
	}

	// Get.
	entry, err = store.Get(ctx, "key1")
	if err != nil {
		t.Fatal(err)
	}
	if entry == nil || entry.Value != "value1" {
		t.Fatalf("expected value1, got %v", entry)
	}

	// Update.
	if err := store.Set(ctx, "key1", "updated"); err != nil {
		t.Fatal(err)
	}
	entry, err = store.Get(ctx, "key1")
	if err != nil {
		t.Fatal(err)
	}
	if entry.Value != "updated" {
		t.Fatalf("expected updated, got %s", entry.Value)
	}

	// Delete.
	if err := store.Delete(ctx, "key1"); err != nil {
		t.Fatal(err)
	}
	entry, err = store.Get(ctx, "key1")
	if err != nil {
		t.Fatal(err)
	}
	if entry != nil {
		t.Fatal("expected nil after delete")
	}
}

func TestInMemoryKVStoreSearch(t *testing.T) {
	t.Parallel()
	store := NewInMemoryKVStore()
	ctx := context.Background()

	store.Set(ctx, "project.name", "OpenClaw")
	store.Set(ctx, "project.version", "1.0.0")
	store.Set(ctx, "user.name", "Bob")

	// Search by key.
	results, err := store.Search(ctx, "project")
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2, got %d", len(results))
	}

	// Search by value.
	results, err = store.Search(ctx, "bob")
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1, got %d", len(results))
	}

	// No match.
	results, err = store.Search(ctx, "xyz")
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 0 {
		t.Fatalf("expected 0, got %d", len(results))
	}
}

func TestInMemoryKVStoreList(t *testing.T) {
	t.Parallel()
	store := NewInMemoryKVStore()
	ctx := context.Background()

	store.Set(ctx, "b", "2")
	store.Set(ctx, "a", "1")
	store.Set(ctx, "c", "3")

	entries, err := store.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 3 {
		t.Fatalf("expected 3, got %d", len(entries))
	}
	// Should be sorted by key.
	if entries[0].Key != "a" || entries[1].Key != "b" || entries[2].Key != "c" {
		t.Fatalf("entries not sorted: %v", entries)
	}
}

func TestSessionStoreListAndGet(t *testing.T) {
	t.Parallel()
	store := NewInMemorySessionStore()
	ctx := context.Background()

	store.GetOrCreate(ctx, "key-1", "model-1")
	store.GetOrCreate(ctx, "key-2", "model-2")

	// List.
	sessions, err := store.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 2 {
		t.Fatalf("expected 2 sessions, got %d", len(sessions))
	}

	// Get.
	sess, err := store.Get(ctx, sessions[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if sess.ID != sessions[0].ID {
		t.Fatalf("expected %s, got %s", sessions[0].ID, sess.ID)
	}

	// Get non-existent.
	_, err = store.Get(ctx, "nonexistent")
	if err == nil {
		t.Fatal("expected error for non-existent session")
	}
}

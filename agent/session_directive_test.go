package agent

import (
	"context"
	"testing"
)

func TestInMemorySessionDirectiveStorePushAndDrain(t *testing.T) {
	t.Parallel()

	store := NewInMemorySessionDirectiveStore()
	ctx := context.Background()

	err := store.Push(ctx, "session-1", SessionDirective{
		Kind:    SessionDirectiveSteer,
		Content: "change to markdown output",
	})
	if err != nil {
		t.Fatalf("Push error: %v", err)
	}

	directives, err := store.Drain(ctx, "session-1", SessionDirectiveSteer)
	if err != nil {
		t.Fatalf("Drain error: %v", err)
	}
	if len(directives) != 1 {
		t.Fatalf("len(directives) = %d, want 1", len(directives))
	}
	if directives[0].Content != "change to markdown output" {
		t.Fatalf("Content = %q", directives[0].Content)
	}
}

func TestInMemorySessionDirectiveStoreDrainEmpty(t *testing.T) {
	t.Parallel()

	store := NewInMemorySessionDirectiveStore()
	ctx := context.Background()

	directives, err := store.Drain(ctx, "session-1", SessionDirectiveSteer)
	if err != nil {
		t.Fatalf("Drain error: %v", err)
	}
	if len(directives) != 0 {
		t.Fatalf("expected empty drain, got %d", len(directives))
	}
}

func TestInMemorySessionDirectiveStoreDrainRemovesEntries(t *testing.T) {
	t.Parallel()

	store := NewInMemorySessionDirectiveStore()
	ctx := context.Background()

	_ = store.Push(ctx, "session-1", SessionDirective{Kind: SessionDirectiveSteer, Content: "a"})
	_ = store.Push(ctx, "session-1", SessionDirective{Kind: SessionDirectiveSteer, Content: "b"})

	directives, _ := store.Drain(ctx, "session-1", SessionDirectiveSteer)
	if len(directives) != 2 {
		t.Fatalf("first drain got %d, want 2", len(directives))
	}

	directives, _ = store.Drain(ctx, "session-1", SessionDirectiveSteer)
	if len(directives) != 0 {
		t.Fatalf("second drain got %d, want 0", len(directives))
	}
}

func TestInMemorySessionDirectiveStoreNilSafe(t *testing.T) {
	t.Parallel()

	var store *InMemorySessionDirectiveStore

	err := store.Push(context.Background(), "session-1", SessionDirective{Kind: SessionDirectiveSteer})
	if err != nil {
		t.Fatalf("Push on nil store should return nil, got %v", err)
	}

	directives, err := store.Drain(context.Background(), "session-1", SessionDirectiveSteer)
	if err != nil {
		t.Fatalf("Drain on nil store should return nil error, got %v", err)
	}
	if len(directives) != 0 {
		t.Fatalf("Drain on nil store should return empty, got %d", len(directives))
	}
}

func TestInMemorySessionDirectiveStorePushEmptySessionID(t *testing.T) {
	t.Parallel()

	store := NewInMemorySessionDirectiveStore()
	err := store.Push(context.Background(), "", SessionDirective{Kind: SessionDirectiveSteer})
	if err != nil {
		t.Fatalf("Push with empty session ID should return nil, got %v", err)
	}
}

func TestInMemorySessionDirectiveStoreSetsCreatedAt(t *testing.T) {
	t.Parallel()

	store := NewInMemorySessionDirectiveStore()
	ctx := context.Background()

	_ = store.Push(ctx, "session-1", SessionDirective{
		Kind:    SessionDirectiveSteer,
		Content: "test",
	})

	directives, _ := store.Drain(ctx, "session-1", SessionDirectiveSteer)
	if len(directives) != 1 {
		t.Fatal("expected 1 directive")
	}
	if directives[0].CreatedAt.IsZero() {
		t.Fatal("CreatedAt should be set automatically")
	}
}

func TestSessionDirectiveKindConstant(t *testing.T) {
	t.Parallel()

	if SessionDirectiveSteer != "steer_current_run" {
		t.Fatalf("SessionDirectiveSteer = %q", SessionDirectiveSteer)
	}
}

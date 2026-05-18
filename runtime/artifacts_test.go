package runtime

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/fulcrus/hopclaw/agent"
	"github.com/fulcrus/hopclaw/artifact"
	"github.com/fulcrus/hopclaw/eventbus"
)

// ---------------------------------------------------------------------------
// ListArtifacts — filter by kind
// ---------------------------------------------------------------------------

func TestListArtifactsFilterByKindMultipleTypes(t *testing.T) {
	t.Parallel()
	store := artifact.NewInMemoryStore()
	_, _ = store.Put(context.Background(), artifact.PutRequest{Kind: "log", Body: []byte("a")})
	_, _ = store.Put(context.Background(), artifact.PutRequest{Kind: "image", Body: []byte("b")})
	_, _ = store.Put(context.Background(), artifact.PutRequest{Kind: "log", Body: []byte("c")})

	runs := agent.NewInMemoryRunStore()
	svc := NewService(nil, agent.NewInMemorySessionStore(), runs, nil, nil, store)
	list, err := svc.ListArtifacts(context.Background(), artifact.ListFilter{Kind: "log"})
	if err != nil {
		t.Fatalf("ListArtifacts() error = %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("expected 2 log artifacts, got %d", len(list))
	}
	for _, b := range list {
		if b.Kind != "log" {
			t.Fatalf("expected Kind=log, got %q", b.Kind)
		}
	}
}

func TestListArtifactsNoMatch(t *testing.T) {
	t.Parallel()
	store := artifact.NewInMemoryStore()
	_, _ = store.Put(context.Background(), artifact.PutRequest{Kind: "log", Body: []byte("a")})

	runs := agent.NewInMemoryRunStore()
	svc := NewService(nil, agent.NewInMemorySessionStore(), runs, nil, nil, store)
	list, err := svc.ListArtifacts(context.Background(), artifact.ListFilter{Kind: "nonexistent"})
	if err != nil {
		t.Fatalf("ListArtifacts() error = %v", err)
	}
	if len(list) != 0 {
		t.Fatalf("expected 0 artifacts, got %d", len(list))
	}
}

// ---------------------------------------------------------------------------
// PruneArtifacts — event emission details
// ---------------------------------------------------------------------------

func TestPruneArtifactsEventAttrs(t *testing.T) {
	t.Parallel()
	store := artifact.NewInMemoryStore()
	_, _ = store.Put(context.Background(), artifact.PutRequest{Kind: "log", Body: []byte("data")})

	runs := agent.NewInMemoryRunStore()
	bus := eventbus.NewInMemoryBus()
	svc := NewService(nil, agent.NewInMemorySessionStore(), runs, nil, bus, store)

	_, err := svc.PruneArtifacts(context.Background(), ArtifactPruneRequest{
		Filter: artifact.ListFilter{Kind: "log"},
	})
	if err != nil {
		t.Fatalf("PruneArtifacts() error = %v", err)
	}

	events := bus.Snapshot()
	var pruneEvent *eventbus.Event
	for i := range events {
		if events[i].Type == eventbus.EventArtifactPruned {
			pruneEvent = &events[i]
			break
		}
	}
	if pruneEvent == nil {
		t.Fatal("expected artifact.pruned event")
	}
	if pruneEvent.Attrs["deleted_count"] != 1 {
		t.Fatalf("deleted_count = %v, want 1", pruneEvent.Attrs["deleted_count"])
	}
	if pruneEvent.Attrs["kind"] != "log" {
		t.Fatalf("kind = %v, want log", pruneEvent.Attrs["kind"])
	}
}

// ---------------------------------------------------------------------------
// PruneArtifacts — retention override
// ---------------------------------------------------------------------------

func TestPruneArtifactsRequestRetentionOverride(t *testing.T) {
	t.Parallel()
	clock := newManualClock(time.Date(2100, 1, 1, 0, 0, 0, 0, time.UTC))
	store := artifact.NewInMemoryStore()
	_, _ = store.Put(context.Background(), artifact.PutRequest{Kind: "test", Body: []byte("x")})

	runs := agent.NewInMemoryRunStore()
	bus := eventbus.NewInMemoryBus()
	svc := NewService(nil, agent.NewInMemorySessionStore(), runs, nil, bus, store).WithClock(clock)
	svc.WithArtifactRetention(24 * time.Hour) // very long

	result, err := svc.PruneArtifacts(context.Background(), ArtifactPruneRequest{
		Retention: time.Nanosecond,
	})
	if err != nil {
		t.Fatalf("PruneArtifacts() error = %v", err)
	}
	if result.DeletedCount != 1 {
		t.Fatalf("DeletedCount = %d, want 1", result.DeletedCount)
	}
}

// ---------------------------------------------------------------------------
// PruneArtifacts — no bus
// ---------------------------------------------------------------------------

func TestPruneArtifactsWithoutBus(t *testing.T) {
	t.Parallel()
	store := artifact.NewInMemoryStore()
	_, _ = store.Put(context.Background(), artifact.PutRequest{Kind: "test", Body: []byte("x")})

	runs := agent.NewInMemoryRunStore()
	svc := NewService(nil, agent.NewInMemorySessionStore(), runs, nil, nil, store)

	// Should not panic even without event bus.
	result, err := svc.PruneArtifacts(context.Background(), ArtifactPruneRequest{
		Filter: artifact.ListFilter{Kind: "test"},
	})
	if err != nil {
		t.Fatalf("PruneArtifacts() error = %v", err)
	}
	if result.DeletedCount != 1 {
		t.Fatalf("DeletedCount = %d, want 1", result.DeletedCount)
	}
}

// ---------------------------------------------------------------------------
// GetArtifact and ReadArtifact — content verification
// ---------------------------------------------------------------------------

func TestGetArtifactContentType(t *testing.T) {
	t.Parallel()
	store := artifact.NewInMemoryStore()
	blob, _ := store.Put(context.Background(), artifact.PutRequest{
		Kind:        "image",
		ContentType: "image/png",
		Body:        []byte{0x89, 0x50, 0x4E, 0x47},
	})

	runs := agent.NewInMemoryRunStore()
	svc := NewService(nil, agent.NewInMemorySessionStore(), runs, nil, nil, store)

	got, err := svc.GetArtifact(context.Background(), blob.ID)
	if err != nil {
		t.Fatalf("GetArtifact() error = %v", err)
	}
	if got.Kind != "image" {
		t.Fatalf("Kind = %q, want image", got.Kind)
	}
	if got.Size != 4 {
		t.Fatalf("Size = %d, want 4", got.Size)
	}
}

func TestReadArtifactContent(t *testing.T) {
	t.Parallel()
	store := artifact.NewInMemoryStore()
	content := []byte("hello artifact world")
	blob, _ := store.Put(context.Background(), artifact.PutRequest{
		Kind:        "text",
		ContentType: "text/plain",
		Body:        content,
	})

	runs := agent.NewInMemoryRunStore()
	svc := NewService(nil, agent.NewInMemorySessionStore(), runs, nil, nil, store)

	body, ct, err := svc.ReadArtifact(context.Background(), blob.ID)
	if err != nil {
		t.Fatalf("ReadArtifact() error = %v", err)
	}
	if string(body) != string(content) {
		t.Fatalf("body = %q, want %q", string(body), string(content))
	}
	if ct != "text/plain" {
		t.Fatalf("contentType = %q, want text/plain", ct)
	}
}

// ---------------------------------------------------------------------------
// Nil artifact store errors
// ---------------------------------------------------------------------------

func TestArtifactNilStoreErrors(t *testing.T) {
	t.Parallel()
	runs := agent.NewInMemoryRunStore()
	svc := NewService(nil, agent.NewInMemorySessionStore(), runs, nil, nil, nil)

	_, err := svc.ListArtifacts(context.Background(), artifact.ListFilter{})
	if !errors.Is(err, agent.ErrArtifactStoreNil) {
		t.Fatalf("ListArtifacts error = %v, want ErrArtifactStoreNil", err)
	}

	_, err = svc.GetArtifact(context.Background(), "x")
	if !errors.Is(err, agent.ErrArtifactStoreNil) {
		t.Fatalf("GetArtifact error = %v, want ErrArtifactStoreNil", err)
	}

	_, _, err = svc.ReadArtifact(context.Background(), "x")
	if !errors.Is(err, agent.ErrArtifactStoreNil) {
		t.Fatalf("ReadArtifact error = %v, want ErrArtifactStoreNil", err)
	}

	_, err = svc.PruneArtifacts(context.Background(), ArtifactPruneRequest{})
	if !errors.Is(err, agent.ErrArtifactStoreNil) {
		t.Fatalf("PruneArtifacts error = %v, want ErrArtifactStoreNil", err)
	}
}

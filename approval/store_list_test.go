package approval

import (
	"context"
	"testing"
)

// ---------------------------------------------------------------------------
// InMemoryStore — Create
// ---------------------------------------------------------------------------

func TestCreateRequiresRunID(t *testing.T) {
	t.Parallel()
	store := NewInMemoryStore()
	_, err := store.Create(context.Background(), Ticket{})
	if err == nil {
		t.Fatal("expected error for missing RunID")
	}
}

func TestCreateAssignsIDAndDefaults(t *testing.T) {
	t.Parallel()
	store := NewInMemoryStore()
	ticket, err := store.Create(context.Background(), Ticket{
		RunID:     "run-1",
		SessionID: "sess-1",
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if ticket.ID == "" {
		t.Fatal("expected non-empty ticket ID")
	}
	if ticket.Status != StatusPending {
		t.Fatalf("Status = %q, want pending", ticket.Status)
	}
	if ticket.Kind != KindToolCalls {
		t.Fatalf("Kind = %q, want tool_calls", ticket.Kind)
	}
	if ticket.CreatedAt.IsZero() {
		t.Fatal("expected non-zero CreatedAt")
	}
}

func TestCreatePreservesKind(t *testing.T) {
	t.Parallel()
	store := NewInMemoryStore()
	ticket, err := store.Create(context.Background(), Ticket{
		RunID: "run-kind",
		Kind:  KindSkillInstall,
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if ticket.Kind != KindSkillInstall {
		t.Fatalf("Kind = %q, want skill_install", ticket.Kind)
	}
}

func TestCreateReturnExistingPending(t *testing.T) {
	t.Parallel()
	store := NewInMemoryStore()
	first, err := store.Create(context.Background(), Ticket{RunID: "run-dup", SessionID: "s1"})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	second, err := store.Create(context.Background(), Ticket{RunID: "run-dup", SessionID: "s1"})
	if err != nil {
		t.Fatalf("Create() duplicate should return existing ticket, got error = %v", err)
	}
	if second.ID != first.ID {
		t.Fatalf("duplicate Create() returned different ticket: got %s, want %s", second.ID, first.ID)
	}
}

func TestCreateAllowsSameRunAfterResolved(t *testing.T) {
	t.Parallel()
	store := NewInMemoryStore()
	first, err := store.Create(context.Background(), Ticket{RunID: "run-resolved", SessionID: "s1"})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	_, err = store.Resolve(context.Background(), first.ID, Resolution{Status: StatusApproved})
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	second, err := store.Create(context.Background(), Ticket{RunID: "run-resolved", SessionID: "s1"})
	if err != nil {
		t.Fatalf("Create(second) error = %v", err)
	}
	if second.ID == first.ID {
		t.Fatal("expected a new ticket ID")
	}
}

func TestCreateReturnsClone(t *testing.T) {
	t.Parallel()
	store := NewInMemoryStore()
	ticket, _ := store.Create(context.Background(), Ticket{
		RunID:     "run-clone",
		SessionID: "s1",
		ToolCalls: []ToolCall{{ID: "c1", Name: "t1", Input: map[string]any{"k": "v"}}},
		Reasons:   []string{"reason-1"},
		Metadata:  map[string]any{"meta_k": "meta_v"},
	})

	// Mutate the returned ticket.
	ticket.Status = StatusDenied
	ticket.ToolCalls[0].Name = "mutated"
	ticket.Reasons[0] = "mutated"
	ticket.Metadata["meta_k"] = "mutated"

	// Fetch from store — should be unaffected.
	stored, err := store.Get(context.Background(), ticket.ID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if stored.Status != StatusPending {
		t.Fatalf("stored Status = %q, want pending", stored.Status)
	}
	if stored.ToolCalls[0].Name != "t1" {
		t.Fatalf("stored ToolCalls[0].Name = %q, want t1", stored.ToolCalls[0].Name)
	}
	if stored.Reasons[0] != "reason-1" {
		t.Fatalf("stored Reasons[0] = %q, want reason-1", stored.Reasons[0])
	}
}

func TestCreateSequentialIDs(t *testing.T) {
	t.Parallel()
	store := NewInMemoryStore()
	t1, _ := store.Create(context.Background(), Ticket{RunID: "r1"})
	t2, _ := store.Create(context.Background(), Ticket{RunID: "r2"})
	if t1.ID == t2.ID {
		t.Fatal("expected unique IDs")
	}
}

// ---------------------------------------------------------------------------
// InMemoryStore — Get
// ---------------------------------------------------------------------------

func TestGetNotFound(t *testing.T) {
	t.Parallel()
	store := NewInMemoryStore()
	_, err := store.Get(context.Background(), "nonexistent")
	if err == nil {
		t.Fatal("expected error for non-existent ticket")
	}
}

func TestGetReturnsClone(t *testing.T) {
	t.Parallel()
	store := NewInMemoryStore()
	created, _ := store.Create(context.Background(), Ticket{RunID: "r1", SessionID: "s1"})
	got, _ := store.Get(context.Background(), created.ID)
	got.Status = StatusDenied

	again, _ := store.Get(context.Background(), created.ID)
	if again.Status != StatusPending {
		t.Fatalf("Status = %q, expected pending (clone not mutation)", again.Status)
	}
}

// ---------------------------------------------------------------------------
// InMemoryStore — GetByRun
// ---------------------------------------------------------------------------

func TestGetByRunNotFound(t *testing.T) {
	t.Parallel()
	store := NewInMemoryStore()
	_, err := store.GetByRun(context.Background(), "missing-run")
	if err == nil {
		t.Fatal("expected error for non-existent run")
	}
}

func TestGetByRunReturnsLatest(t *testing.T) {
	t.Parallel()
	store := NewInMemoryStore()
	first, _ := store.Create(context.Background(), Ticket{RunID: "run-latest", SessionID: "s1"})
	_, _ = store.Resolve(context.Background(), first.ID, Resolution{Status: StatusApproved})
	second, _ := store.Create(context.Background(), Ticket{RunID: "run-latest", SessionID: "s1"})

	got, err := store.GetByRun(context.Background(), "run-latest")
	if err != nil {
		t.Fatalf("GetByRun() error = %v", err)
	}
	if got.ID != second.ID {
		t.Fatalf("got ID = %q, want %q (latest)", got.ID, second.ID)
	}
}

// ---------------------------------------------------------------------------
// InMemoryStore — List
// ---------------------------------------------------------------------------

func TestListAllEmpty(t *testing.T) {
	t.Parallel()
	store := NewInMemoryStore()
	list, err := store.List(context.Background(), ListFilter{})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(list) != 0 {
		t.Fatalf("expected 0 tickets, got %d", len(list))
	}
}

func TestListAllReturnsAll(t *testing.T) {
	t.Parallel()
	store := NewInMemoryStore()
	_, _ = store.Create(context.Background(), Ticket{RunID: "r1"})
	_, _ = store.Create(context.Background(), Ticket{RunID: "r2"})
	list, err := store.List(context.Background(), ListFilter{})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("expected 2 tickets, got %d", len(list))
	}
}

func TestListFiltersByStatus(t *testing.T) {
	t.Parallel()
	store := NewInMemoryStore()
	ticket, _ := store.Create(context.Background(), Ticket{RunID: "r1"})
	_, _ = store.Create(context.Background(), Ticket{RunID: "r2"})
	_, _ = store.Resolve(context.Background(), ticket.ID, Resolution{Status: StatusApproved})

	pending, _ := store.List(context.Background(), ListFilter{Status: StatusPending})
	if len(pending) != 1 {
		t.Fatalf("expected 1 pending, got %d", len(pending))
	}

	approved, _ := store.List(context.Background(), ListFilter{Status: StatusApproved})
	if len(approved) != 1 {
		t.Fatalf("expected 1 approved, got %d", len(approved))
	}
}

func TestListReturnsClones(t *testing.T) {
	t.Parallel()
	store := NewInMemoryStore()
	_, _ = store.Create(context.Background(), Ticket{RunID: "r1"})

	list, _ := store.List(context.Background(), ListFilter{})
	list[0].Status = StatusDenied

	again, _ := store.List(context.Background(), ListFilter{})
	if again[0].Status != StatusPending {
		t.Fatal("List should return clones")
	}
}

func TestListSupportsLimitAndOffset(t *testing.T) {
	t.Parallel()
	store := NewInMemoryStore()
	for _, runID := range []string{"r1", "r2", "r3"} {
		if _, err := store.Create(context.Background(), Ticket{RunID: runID}); err != nil {
			t.Fatalf("Create(%s) error = %v", runID, err)
		}
	}

	page, err := store.List(context.Background(), ListFilter{Limit: 1, Offset: 1})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(page) != 1 {
		t.Fatalf("len(page) = %d, want 1", len(page))
	}
	if page[0].RunID != "r2" {
		t.Fatalf("page[0].RunID = %q, want r2", page[0].RunID)
	}
}

// ---------------------------------------------------------------------------
// InMemoryStore — Resolve
// ---------------------------------------------------------------------------

func TestResolveNotFound(t *testing.T) {
	t.Parallel()
	store := NewInMemoryStore()
	_, err := store.Resolve(context.Background(), "nonexistent", Resolution{Status: StatusApproved})
	if err == nil {
		t.Fatal("expected error for non-existent ticket")
	}
}

func TestResolveAlreadyResolved(t *testing.T) {
	t.Parallel()
	store := NewInMemoryStore()
	ticket, _ := store.Create(context.Background(), Ticket{RunID: "r1"})
	_, _ = store.Resolve(context.Background(), ticket.ID, Resolution{Status: StatusApproved})
	_, err := store.Resolve(context.Background(), ticket.ID, Resolution{Status: StatusDenied})
	if err == nil {
		t.Fatal("expected error for already resolved ticket")
	}
}

func TestResolveInvalidResolution(t *testing.T) {
	t.Parallel()
	store := NewInMemoryStore()
	ticket, _ := store.Create(context.Background(), Ticket{RunID: "r1"})
	_, err := store.Resolve(context.Background(), ticket.ID, Resolution{Status: "invalid"})
	if err == nil {
		t.Fatal("expected error for invalid resolution status")
	}
}

func TestResolveApproved(t *testing.T) {
	t.Parallel()
	store := NewInMemoryStore()
	ticket, _ := store.Create(context.Background(), Ticket{RunID: "r1"})
	resolved, err := store.Resolve(context.Background(), ticket.ID, Resolution{
		Status:     StatusApproved,
		ResolvedBy: "admin",
		Note:       "looks good",
	})
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if resolved.Status != StatusApproved {
		t.Fatalf("Status = %q", resolved.Status)
	}
	if resolved.ResolvedBy != "admin" {
		t.Fatalf("ResolvedBy = %q", resolved.ResolvedBy)
	}
	if resolved.Note != "looks good" {
		t.Fatalf("Note = %q", resolved.Note)
	}
	if resolved.ResolvedAt.IsZero() {
		t.Fatal("expected non-zero ResolvedAt")
	}
}

func TestResolveDenied(t *testing.T) {
	t.Parallel()
	store := NewInMemoryStore()
	ticket, _ := store.Create(context.Background(), Ticket{RunID: "r1"})
	resolved, err := store.Resolve(context.Background(), ticket.ID, Resolution{Status: StatusDenied})
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if resolved.Status != StatusDenied {
		t.Fatalf("Status = %q", resolved.Status)
	}
}

func TestResolveCancelled(t *testing.T) {
	t.Parallel()
	store := NewInMemoryStore()
	ticket, _ := store.Create(context.Background(), Ticket{RunID: "r1"})
	resolved, err := store.Resolve(context.Background(), ticket.ID, Resolution{Status: StatusCancelled})
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if resolved.Status != StatusCancelled {
		t.Fatalf("Status = %q", resolved.Status)
	}
}

func TestResolvePendingStatusNotAllowed(t *testing.T) {
	t.Parallel()
	store := NewInMemoryStore()
	ticket, _ := store.Create(context.Background(), Ticket{RunID: "r1"})
	_, err := store.Resolve(context.Background(), ticket.ID, Resolution{Status: StatusPending})
	if err == nil {
		t.Fatal("expected error for resolving with pending status")
	}
}

// ---------------------------------------------------------------------------
// Status and Kind constants
// ---------------------------------------------------------------------------

func TestStatusConstants(t *testing.T) {
	t.Parallel()
	if string(StatusPending) != "pending" {
		t.Fatalf("StatusPending = %q", StatusPending)
	}
	if string(StatusApproved) != "approved" {
		t.Fatalf("StatusApproved = %q", StatusApproved)
	}
	if string(StatusDenied) != "denied" {
		t.Fatalf("StatusDenied = %q", StatusDenied)
	}
	if string(StatusCancelled) != "cancelled" {
		t.Fatalf("StatusCancelled = %q", StatusCancelled)
	}
}

func TestKindConstants(t *testing.T) {
	t.Parallel()
	if string(KindToolCalls) != "tool_calls" {
		t.Fatalf("KindToolCalls = %q", KindToolCalls)
	}
	if string(KindSkillInstall) != "skill_install" {
		t.Fatalf("KindSkillInstall = %q", KindSkillInstall)
	}
}

package approval

import (
	"context"
	"errors"
	"testing"
)

func TestInMemoryStoreCreateAndResolve(t *testing.T) {
	t.Parallel()

	store := NewInMemoryStore()
	ticket, err := store.Create(context.Background(), Ticket{
		RunID:     "run-1",
		SessionID: "sess-1",
		ToolCalls: []ToolCall{{ID: "call-1", Name: "fs.write"}},
		Reasons:   []string{"requires approval"},
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if ticket.Status != StatusPending {
		t.Fatalf("ticket.Status = %q", ticket.Status)
	}

	resolved, err := store.Resolve(context.Background(), ticket.ID, Resolution{
		Status:     StatusApproved,
		ResolvedBy: "tester",
		Note:       "approved",
	})
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if resolved.Status != StatusApproved {
		t.Fatalf("resolved.Status = %q", resolved.Status)
	}
	if resolved.ResolvedBy != "tester" {
		t.Fatalf("resolved.ResolvedBy = %q", resolved.ResolvedBy)
	}

	next, err := store.Create(context.Background(), Ticket{
		RunID:     "run-1",
		SessionID: "sess-1",
		ToolCalls: []ToolCall{{ID: "call-2", Name: "net.http"}},
	})
	if err != nil {
		t.Fatalf("Create(second) error = %v", err)
	}
	if next.Status != StatusPending {
		t.Fatalf("next.Status = %q", next.Status)
	}
	if next.ID == ticket.ID {
		t.Fatalf("expected a new ticket ID, got %q", next.ID)
	}
	got, err := store.GetByRun(context.Background(), "run-1")
	if err != nil {
		t.Fatalf("GetByRun() error = %v", err)
	}
	if got.ID != next.ID {
		t.Fatalf("GetByRun().ID = %q, want %q", got.ID, next.ID)
	}
}

func TestInMemoryStoreResolvePersistsScope(t *testing.T) {
	t.Parallel()

	store := NewInMemoryStore()
	ticket, err := store.Create(context.Background(), Ticket{
		RunID:     "run-scope",
		SessionID: "sess-scope",
		Metadata: map[string]any{
			"policy_approval_max_scope": "session",
		},
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	resolved, err := store.Resolve(context.Background(), ticket.ID, Resolution{
		Status: StatusApproved,
		Scope:  ScopeSession,
	})
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if resolved.Scope != ScopeSession {
		t.Fatalf("resolved.Scope = %q, want %q", resolved.Scope, ScopeSession)
	}

	got, err := store.Get(context.Background(), ticket.ID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got.Scope != ScopeSession {
		t.Fatalf("got.Scope = %q, want %q", got.Scope, ScopeSession)
	}
}

func TestInMemoryStoreResolveRejectsScopeBroaderThanPolicy(t *testing.T) {
	t.Parallel()

	store := NewInMemoryStore()
	ticket, err := store.Create(context.Background(), Ticket{
		RunID:     "run-policy",
		SessionID: "sess-policy",
		Metadata: map[string]any{
			"policy_approval_default_scope": "once",
			"policy_approval_max_scope":     "session",
		},
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	_, err = store.Resolve(context.Background(), ticket.ID, Resolution{
		Status: StatusApproved,
		Scope:  ScopeAlways,
	})
	if !errors.Is(err, ErrScopePolicy) {
		t.Fatalf("Resolve() error = %v, want ErrScopePolicy", err)
	}
}

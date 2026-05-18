package store

import (
	"context"
	"errors"
	"testing"

	"github.com/fulcrus/hopclaw/approval"
)

func TestSQLiteApprovalStoreResolveRejectsScopeBroaderThanPolicy(t *testing.T) {
	t.Parallel()

	db := openRawTestDB(t)
	sessions := NewSQLiteSessionStore(db)
	store := NewSQLiteApprovalStore(db)
	session, err := sessions.GetOrCreate(context.Background(), "test:approval-scope", "m1")
	if err != nil {
		t.Fatalf("GetOrCreate() error = %v", err)
	}
	ticket, err := store.Create(context.Background(), approval.Ticket{
		RunID:     "run-policy",
		SessionID: session.ID,
		Metadata: map[string]any{
			"policy_approval_default_scope": "once",
			"policy_approval_max_scope":     "session",
		},
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	_, err = store.Resolve(context.Background(), ticket.ID, approval.Resolution{
		Status: approval.StatusApproved,
		Scope:  approval.ScopeAlways,
	})
	if !errors.Is(err, approval.ErrScopePolicy) {
		t.Fatalf("Resolve() error = %v, want ErrScopePolicy", err)
	}
}

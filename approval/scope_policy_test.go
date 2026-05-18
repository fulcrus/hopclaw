package approval

import (
	"context"
	"errors"
	"testing"
)

func TestNormalizeScope(t *testing.T) {
	t.Parallel()

	scope, err := NormalizeScope(" SESSION ")
	if err != nil {
		t.Fatalf("NormalizeScope() error = %v", err)
	}
	if scope != ScopeSession {
		t.Fatalf("scope = %q, want %q", scope, ScopeSession)
	}

	if _, err := NormalizeScope("sometimes"); !errors.Is(err, ErrInvalidScope) {
		t.Fatalf("NormalizeScope(invalid) error = %v, want ErrInvalidScope", err)
	}
}

func TestScopePolicyHelpers(t *testing.T) {
	t.Parallel()

	if !IsScopeBroader(ScopeAlways, ScopeSession) {
		t.Fatal("expected always to be broader than session")
	}
	if !IsScopeBroader(ScopeAlways, "") {
		t.Fatal("expected empty max scope to fail closed")
	}
	if !IsScopeBroader(ScopeOnce, "invalid") {
		t.Fatal("expected invalid max scope to fail closed")
	}
	if IsScopeBroader("", ScopeSession) {
		t.Fatal("expected empty requested scope to be treated as non-broader")
	}
	if got := NarrowerScope(ScopeAlways, ScopeSession, ScopeOnce); got != ScopeOnce {
		t.Fatalf("NarrowerScope() = %q, want %q", got, ScopeOnce)
	}
}

func TestNarrowerScopeCheckedFailsOnInvalidScope(t *testing.T) {
	t.Parallel()

	if _, err := NarrowerScopeChecked(ScopeAlways, "sometimes"); !errors.Is(err, ErrInvalidScope) {
		t.Fatalf("NarrowerScopeChecked() error = %v, want ErrInvalidScope", err)
	}
}

func TestStoreResolveDefaultsApprovedScopeToOnce(t *testing.T) {
	t.Parallel()

	store := NewInMemoryStore()
	ticket, err := store.Create(context.Background(), Ticket{
		RunID:     "run-1",
		SessionID: "sess-1",
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	resolved, err := store.Resolve(context.Background(), ticket.ID, Resolution{
		Status: StatusApproved,
	})
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if resolved.Scope != ScopeOnce {
		t.Fatalf("resolved.Scope = %q, want %q", resolved.Scope, ScopeOnce)
	}
}

func TestNormalizeResolutionDefaultsEmptyMaxScopeToFailClosedValue(t *testing.T) {
	t.Parallel()

	resolution, err := NormalizeResolution(&Ticket{
		Metadata: map[string]any{
			"policy_approval_default_scope": "session",
		},
	}, Resolution{
		Status: StatusApproved,
		Scope:  ScopeAlways,
	})
	if !errors.Is(err, ErrScopePolicy) {
		t.Fatalf("NormalizeResolution() error = %v, want ErrScopePolicy", err)
	}
	if resolution.Scope != ScopeAlways {
		t.Fatalf("resolution.Scope = %q, want requested scope retained on error", resolution.Scope)
	}
}

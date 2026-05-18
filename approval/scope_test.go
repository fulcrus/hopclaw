package approval

import (
	"sync"
	"testing"
)

// ---------------------------------------------------------------------------
// Scope constants
// ---------------------------------------------------------------------------

func TestScopeConstants(t *testing.T) {
	t.Parallel()
	if ScopeOnce != "once" {
		t.Fatalf("ScopeOnce = %q", ScopeOnce)
	}
	if ScopeSession != "session" {
		t.Fatalf("ScopeSession = %q", ScopeSession)
	}
	if ScopeAlways != "always" {
		t.Fatalf("ScopeAlways = %q", ScopeAlways)
	}
	if ScopeDeny != "deny" {
		t.Fatalf("ScopeDeny = %q", ScopeDeny)
	}
}

// ---------------------------------------------------------------------------
// NewGrantStore
// ---------------------------------------------------------------------------

func TestNewGrantStore(t *testing.T) {
	t.Parallel()
	gs := NewGrantStore()
	if gs == nil {
		t.Fatal("NewGrantStore returned nil")
	}
}

// ---------------------------------------------------------------------------
// Grant + IsGranted — session scope
// ---------------------------------------------------------------------------

func TestGrantSessionScope(t *testing.T) {
	t.Parallel()
	gs := NewGrantStore()
	gs.Grant("sess-1", "fs.write", ScopeSession)
	if !gs.IsGranted("sess-1", "fs.write") {
		t.Fatal("expected grant for sess-1 / fs.write")
	}
}

func TestGrantScopedMatchesOnlyApprovedResourceRange(t *testing.T) {
	t.Parallel()

	gs := NewGrantStore()
	gs.GrantScoped("sess-1", "fs.write", ScopeSession, ResourceScope{PathPrefixes: []string{"reports"}})

	if decision := gs.Evaluate("sess-1", "fs.write", map[string]any{"path": "reports/daily.txt"}); !decision.Granted {
		t.Fatalf("Evaluate(granted path) = %#v, want granted", decision)
	}
	if decision := gs.Evaluate("sess-1", "fs.write", map[string]any{"path": "secrets.txt"}); decision.Granted || decision.Denied {
		t.Fatalf("Evaluate(out-of-scope path) = %#v, want empty decision", decision)
	}
}

func TestGrantScopedDenyOverridesMatchingGrant(t *testing.T) {
	t.Parallel()

	gs := NewGrantStore()
	gs.GrantScoped("sess-1", "net.http", ScopeSession, ResourceScope{Hosts: []string{"api.example.com"}})
	gs.GrantScoped("sess-1", "net.http", ScopeDeny, ResourceScope{
		Hosts:      []string{"api.example.com"},
		Parameters: map[string][]string{"method": {"DELETE"}},
	})

	allowed := gs.Evaluate("sess-1", "net.http", map[string]any{
		"url":    "https://api.example.com/v1/items",
		"method": "POST",
	})
	if !allowed.Granted || allowed.Denied {
		t.Fatalf("allowed = %#v, want granted", allowed)
	}

	denied := gs.Evaluate("sess-1", "net.http", map[string]any{
		"url":    "https://api.example.com/v1/items",
		"method": "DELETE",
	})
	if !denied.Denied {
		t.Fatalf("denied = %#v, want denied", denied)
	}
}

func TestGrantSessionScopeIsolation(t *testing.T) {
	t.Parallel()
	gs := NewGrantStore()
	gs.Grant("sess-1", "fs.write", ScopeSession)
	if gs.IsGranted("sess-2", "fs.write") {
		t.Fatal("session grant should not leak to other sessions")
	}
}

func TestGrantSessionScopeToolIsolation(t *testing.T) {
	t.Parallel()
	gs := NewGrantStore()
	gs.Grant("sess-1", "fs.write", ScopeSession)
	if gs.IsGranted("sess-1", "fs.read") {
		t.Fatal("grant for fs.write should not apply to fs.read")
	}
}

// ---------------------------------------------------------------------------
// Grant + IsGranted — always scope
// ---------------------------------------------------------------------------

func TestGrantAlwaysScope(t *testing.T) {
	t.Parallel()
	gs := NewGrantStore()
	gs.Grant("sess-1", "fs.write", ScopeAlways)
	if !gs.IsGranted("sess-1", "fs.write") {
		t.Fatal("expected always grant for sess-1 / fs.write")
	}
	if gs.IsGranted("sess-2", "fs.write") {
		t.Fatal("expected always grant to remain session-scoped")
	}
}

// ---------------------------------------------------------------------------
// Grant — once scope is not stored
// ---------------------------------------------------------------------------

func TestGrantOnceScopeNotStored(t *testing.T) {
	t.Parallel()
	gs := NewGrantStore()
	gs.Grant("sess-1", "fs.write", ScopeOnce)
	if gs.IsGranted("sess-1", "fs.write") {
		t.Fatal("once scope should not be stored as a standing grant")
	}
}

// ---------------------------------------------------------------------------
// Grant + IsDenied — deny scope
// ---------------------------------------------------------------------------

func TestGrantDenyScope(t *testing.T) {
	t.Parallel()
	gs := NewGrantStore()
	gs.Grant("sess-1", "dangerous.tool", ScopeDeny)
	if !gs.IsDenied("sess-1", "dangerous.tool") {
		t.Fatal("expected deny for sess-1 / dangerous.tool")
	}
	if gs.IsGranted("sess-1", "dangerous.tool") {
		t.Fatal("denied tool should not be granted")
	}
}

func TestDenyDoesNotLeakToOtherSessions(t *testing.T) {
	t.Parallel()
	gs := NewGrantStore()
	gs.Grant("sess-1", "dangerous.tool", ScopeDeny)
	if gs.IsDenied("sess-2", "dangerous.tool") {
		t.Fatal("deny should be session-scoped")
	}
}

func TestIsDeniedReturnsFalseWhenNotDenied(t *testing.T) {
	t.Parallel()
	gs := NewGrantStore()
	if gs.IsDenied("sess-1", "fs.write") {
		t.Fatal("expected false for non-denied tool")
	}
}

// ---------------------------------------------------------------------------
// IsGranted — no grants
// ---------------------------------------------------------------------------

func TestIsGrantedEmptyStore(t *testing.T) {
	t.Parallel()
	gs := NewGrantStore()
	if gs.IsGranted("sess-1", "fs.write") {
		t.Fatal("expected false for empty store")
	}
}

// ---------------------------------------------------------------------------
// Session deny overrides remembered session grants
// ---------------------------------------------------------------------------

func TestSessionDenyOverridesRememberedGrant(t *testing.T) {
	t.Parallel()
	gs := NewGrantStore()
	gs.Grant("sess-1", "fs.write", ScopeAlways)
	gs.Grant("sess-1", "fs.write", ScopeDeny)

	if gs.IsGranted("sess-1", "fs.write") {
		t.Fatal("session deny should override remembered grant")
	}
	if !gs.IsDenied("sess-1", "fs.write") {
		t.Fatal("session deny should be active")
	}
}

// ---------------------------------------------------------------------------
// Revoke
// ---------------------------------------------------------------------------

func TestRevokeRemovesSessionGrant(t *testing.T) {
	t.Parallel()
	gs := NewGrantStore()
	gs.Grant("sess-1", "fs.write", ScopeSession)
	gs.Revoke("sess-1", "fs.write")
	if gs.IsGranted("sess-1", "fs.write") {
		t.Fatal("expected revoked grant to be removed")
	}
}

func TestRevokeRemovesAlwaysGrantForSession(t *testing.T) {
	t.Parallel()
	gs := NewGrantStore()
	gs.Grant("sess-1", "fs.write", ScopeAlways)
	gs.Revoke("sess-1", "fs.write")
	if gs.IsGranted("sess-1", "fs.write") {
		t.Fatal("expected always grant to be removed for the session")
	}
	if gs.IsGranted("sess-2", "fs.write") {
		t.Fatal("expected session-local always grant to remain isolated")
	}
}

func TestRevokeRemovesDeny(t *testing.T) {
	t.Parallel()
	gs := NewGrantStore()
	gs.Grant("sess-1", "fs.write", ScopeDeny)
	gs.Revoke("sess-1", "fs.write")
	if gs.IsDenied("sess-1", "fs.write") {
		t.Fatal("expected deny to be removed after revoke")
	}
}

func TestRevokeNonExistentIsNoop(t *testing.T) {
	t.Parallel()
	gs := NewGrantStore()
	// Should not panic.
	gs.Revoke("sess-1", "nonexistent")
}

// ---------------------------------------------------------------------------
// RevokeSession
// ---------------------------------------------------------------------------

func TestRevokeSessionRemovesAllSessionGrants(t *testing.T) {
	t.Parallel()
	gs := NewGrantStore()
	gs.Grant("sess-1", "fs.write", ScopeSession)
	gs.Grant("sess-1", "net.http", ScopeSession)
	gs.Grant("sess-1", "dangerous", ScopeDeny)
	gs.Grant("sess-2", "fs.write", ScopeSession) // different session

	gs.RevokeSession("sess-1")

	if gs.IsGranted("sess-1", "fs.write") {
		t.Fatal("expected sess-1 fs.write to be revoked")
	}
	if gs.IsGranted("sess-1", "net.http") {
		t.Fatal("expected sess-1 net.http to be revoked")
	}
	if gs.IsDenied("sess-1", "dangerous") {
		t.Fatal("expected sess-1 dangerous deny to be revoked")
	}
	// Session 2 should be unaffected.
	if !gs.IsGranted("sess-2", "fs.write") {
		t.Fatal("expected sess-2 fs.write to remain granted")
	}
}

func TestRevokeSessionRemovesAlwaysGrants(t *testing.T) {
	t.Parallel()
	gs := NewGrantStore()
	gs.Grant("sess-1", "fs.write", ScopeAlways)
	gs.Grant("sess-1", "net.http", ScopeSession)

	gs.RevokeSession("sess-1")

	if gs.IsGranted("sess-1", "fs.write") {
		t.Fatal("expected always-scope grant to be revoked with the session")
	}
	if gs.IsGranted("sess-1", "net.http") {
		t.Fatal("expected session-scope grant to be revoked")
	}
}

func TestRevokeSessionEmptySessionIsNoop(t *testing.T) {
	t.Parallel()
	gs := NewGrantStore()
	gs.Grant("sess-1", "fs.write", ScopeSession)
	gs.RevokeSession("sess-2") // no-op
	if !gs.IsGranted("sess-1", "fs.write") {
		t.Fatal("expected sess-1 grant to remain")
	}
}

// ---------------------------------------------------------------------------
// Concurrent access
// ---------------------------------------------------------------------------

func TestGrantStoreConcurrentAccess(t *testing.T) {
	t.Parallel()

	gs := NewGrantStore()
	const goroutines = 50
	var wg sync.WaitGroup
	wg.Add(goroutines)

	for i := 0; i < goroutines; i++ {
		go func(i int) {
			defer wg.Done()
			sess := "sess-concurrent"
			tool := "tool-concurrent"

			gs.Grant(sess, tool, ScopeSession)
			gs.IsGranted(sess, tool)
			gs.IsDenied(sess, tool)
			gs.Grant(sess, tool, ScopeDeny)
			gs.IsDenied(sess, tool)
			gs.Revoke(sess, tool)

			gs.Grant(sess, tool, ScopeAlways)
			gs.IsGranted(sess, tool)
			gs.RevokeSession(sess)
		}(i)
	}
	wg.Wait()
}

func TestGrantStoreConcurrentMixedOperations(t *testing.T) {
	t.Parallel()

	gs := NewGrantStore()
	const goroutines = 20
	var wg sync.WaitGroup
	wg.Add(goroutines * 3)

	// Readers
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				gs.IsGranted("s1", "t1")
				gs.IsDenied("s1", "t1")
			}
		}()
	}
	// Writers (Grant)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				gs.Grant("s1", "t1", ScopeSession)
			}
		}()
	}
	// Writers (Revoke)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				gs.Revoke("s1", "t1")
			}
		}()
	}
	wg.Wait()
}

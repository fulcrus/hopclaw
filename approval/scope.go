package approval

import (
	"sync"
)

// Scope defines the duration of an approval grant.
type Scope string

const (
	ScopeOnce    Scope = "once"    // single invocation
	ScopeSession Scope = "session" // entire session lifetime
	ScopeAlways  Scope = "always"  // remember for the current session until revoked
	ScopeDeny    Scope = "deny"    // explicit denial
)

// grantKey uniquely identifies a grant by session and tool.
type grantKey struct {
	sessionID string
	toolName  string
}

type grantRecord struct {
	scope         Scope
	resourceScope ResourceScope
}

// GrantStore records per-session approval grants.
type GrantStore struct {
	mu      sync.RWMutex
	session map[grantKey][]grantRecord
}

// NewGrantStore creates an empty grant store.
func NewGrantStore() *GrantStore {
	return &GrantStore{
		session: make(map[grantKey][]grantRecord),
	}
}

// Grant records an approval grant for the given session and tool.
func (g *GrantStore) Grant(sessionID, toolName string, scope Scope) {
	g.GrantScoped(sessionID, toolName, scope, ResourceScope{})
}

// GrantScoped records an approval grant for the given session, tool, and
// resource scope.
func (g *GrantStore) GrantScoped(sessionID, toolName string, scope Scope, resourceScope ResourceScope) {
	g.mu.Lock()
	defer g.mu.Unlock()
	key := grantKey{sessionID: sessionID, toolName: toolName}
	record := grantRecord{scope: scope, resourceScope: resourceScope.Normalized()}
	switch scope {
	case ScopeSession, ScopeAlways:
		g.session[key] = upsertGrantRecord(g.session[key], record)
	case ScopeDeny:
		g.session[key] = upsertGrantRecord(g.session[key], record)
	}
	// ScopeOnce is not stored — it expires immediately after use.
}

// IsGranted checks whether the tool has a standing approval grant.
func (g *GrantStore) IsGranted(sessionID, toolName string) bool {
	g.mu.RLock()
	defer g.mu.RUnlock()
	for _, record := range g.session[grantKey{sessionID: sessionID, toolName: toolName}] {
		if record.scope == ScopeSession || record.scope == ScopeAlways {
			return true
		}
	}
	return false
}

// Evaluate returns the matching scoped grant decision for a tool invocation.
func (g *GrantStore) Evaluate(sessionID, toolName string, input map[string]any) GrantDecision {
	g.mu.RLock()
	defer g.mu.RUnlock()
	records := g.session[grantKey{sessionID: sessionID, toolName: toolName}]
	if len(records) == 0 {
		return GrantDecision{}
	}
	for _, record := range records {
		if record.scope != ScopeDeny || !record.resourceScope.MatchesCall(toolName, input) {
			continue
		}
		return GrantDecision{Denied: true, Scope: record.scope, ResourceScope: record.resourceScope.Normalized()}
	}
	for _, record := range records {
		if (record.scope != ScopeSession && record.scope != ScopeAlways) || !record.resourceScope.MatchesCall(toolName, input) {
			continue
		}
		return GrantDecision{Granted: true, Scope: record.scope, ResourceScope: record.resourceScope.Normalized()}
	}
	return GrantDecision{}
}

// IsDenied checks whether the tool has an explicit denial.
func (g *GrantStore) IsDenied(sessionID, toolName string) bool {
	g.mu.RLock()
	defer g.mu.RUnlock()
	for _, record := range g.session[grantKey{sessionID: sessionID, toolName: toolName}] {
		if record.scope == ScopeDeny {
			return true
		}
	}
	return false
}

// Revoke removes any grant for the given session and tool.
func (g *GrantStore) Revoke(sessionID, toolName string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	delete(g.session, grantKey{sessionID: sessionID, toolName: toolName})
}

// RevokeSession removes all session-scoped grants for a session.
func (g *GrantStore) RevokeSession(sessionID string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	for key := range g.session {
		if key.sessionID == sessionID {
			delete(g.session, key)
		}
	}
}

func upsertGrantRecord(records []grantRecord, candidate grantRecord) []grantRecord {
	signature := candidate.resourceScope.signature()
	for i, record := range records {
		if record.resourceScope.signature() != signature {
			continue
		}
		records[i] = candidate
		return records
	}
	return append(records, candidate)
}

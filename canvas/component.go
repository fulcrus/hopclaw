package canvas

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"sync"
	"time"
)

// ---------------------------------------------------------------------------
// Constants
// ---------------------------------------------------------------------------

const (
	// tokenLength is the byte length for capability tokens (32 hex chars).
	tokenLength = 16

	// defaultTokenTTL is the default time-to-live for capability tokens.
	defaultTokenTTL = 10 * time.Minute

	// tokenCleanupInterval is how often expired tokens are purged.
	tokenCleanupInterval = time.Minute
)

// Sentinel errors.
var (
	ErrTokenExpired = errors.New("capability token expired")
	ErrTokenInvalid = errors.New("capability token invalid")
	ErrSessionEmpty = errors.New("session id is required")
)

// ---------------------------------------------------------------------------
// Component
// ---------------------------------------------------------------------------

// Component represents an A2UI component that can be pushed to connected clients.
type Component struct {
	ID       string         `json:"id"`
	Type     string         `json:"type"` // "chart", "table", "form", "markdown", "custom"
	Props    map[string]any `json:"props"`
	Children []Component    `json:"children,omitempty"`
	Version  int64          `json:"version"`
}

// ---------------------------------------------------------------------------
// ComponentRegistry
// ---------------------------------------------------------------------------

// ComponentRegistry manages per-session A2UI component state with thread safety.
type ComponentRegistry struct {
	mu       sync.RWMutex // guards sessions
	sessions map[string]*sessionComponents
}

type sessionComponents struct {
	components []Component
	version    int64
}

// NewComponentRegistry creates a new empty registry.
func NewComponentRegistry() *ComponentRegistry {
	return &ComponentRegistry{
		sessions: make(map[string]*sessionComponents),
	}
}

// Push adds or replaces components for a session. When replace is true, existing
// components are cleared first. Returns the new version number.
func (r *ComponentRegistry) Push(sessionID string, components []Component, replace bool) int64 {
	r.mu.Lock()
	defer r.mu.Unlock()

	sc, ok := r.sessions[sessionID]
	if !ok {
		sc = &sessionComponents{}
		r.sessions[sessionID] = sc
	}

	sc.version++
	version := sc.version

	// Stamp version on each component.
	for i := range components {
		components[i].Version = version
	}

	if replace {
		sc.components = make([]Component, len(components))
		copy(sc.components, components)
	} else {
		sc.components = append(sc.components, components...)
	}

	return version
}

// Reset clears all components for a session.
func (r *ComponentRegistry) Reset(sessionID string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if sc, ok := r.sessions[sessionID]; ok {
		sc.components = nil
		sc.version++
	}
}

// Get returns a copy of the components for a session.
func (r *ComponentRegistry) Get(sessionID string) []Component {
	r.mu.RLock()
	defer r.mu.RUnlock()

	sc, ok := r.sessions[sessionID]
	if !ok || len(sc.components) == 0 {
		return nil
	}

	out := make([]Component, len(sc.components))
	copy(out, sc.components)
	return out
}

// Version returns the current version number for a session.
func (r *ComponentRegistry) Version(sessionID string) int64 {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if sc, ok := r.sessions[sessionID]; ok {
		return sc.version
	}
	return 0
}

// ---------------------------------------------------------------------------
// CapabilityToken
// ---------------------------------------------------------------------------

// CapabilityToken is a time-limited access token for a canvas session.
type CapabilityToken struct {
	Token     string    `json:"token"`
	SessionID string    `json:"session_id"`
	ExpiresAt time.Time `json:"expires_at"`
}

// ---------------------------------------------------------------------------
// TokenStore
// ---------------------------------------------------------------------------

// TokenStore manages capability tokens with automatic TTL expiration.
type TokenStore struct {
	mu     sync.RWMutex // guards tokens
	tokens map[string]*CapabilityToken
	ttl    time.Duration
	stopCh chan struct{}
}

// NewTokenStore creates a new token store with the given TTL.
func NewTokenStore(ttl time.Duration) *TokenStore {
	if ttl <= 0 {
		ttl = defaultTokenTTL
	}
	ts := &TokenStore{
		tokens: make(map[string]*CapabilityToken),
		ttl:    ttl,
		stopCh: make(chan struct{}),
	}
	go ts.cleanupLoop()
	return ts
}

// Issue creates a new capability token for the given session.
func (ts *TokenStore) Issue(sessionID string) (CapabilityToken, error) {
	if sessionID == "" {
		return CapabilityToken{}, ErrSessionEmpty
	}

	b := make([]byte, tokenLength)
	if _, err := rand.Read(b); err != nil {
		return CapabilityToken{}, err
	}

	token := CapabilityToken{
		Token:     hex.EncodeToString(b),
		SessionID: sessionID,
		ExpiresAt: time.Now().Add(ts.ttl),
	}

	ts.mu.Lock()
	ts.tokens[token.Token] = &token
	ts.mu.Unlock()

	return token, nil
}

// Validate checks if a token is valid and returns the associated session ID.
func (ts *TokenStore) Validate(token string) (string, error) {
	ts.mu.RLock()
	ct, ok := ts.tokens[token]
	ts.mu.RUnlock()

	if !ok {
		return "", ErrTokenInvalid
	}
	if time.Now().After(ct.ExpiresAt) {
		// Clean up expired token.
		ts.mu.Lock()
		delete(ts.tokens, token)
		ts.mu.Unlock()
		return "", ErrTokenExpired
	}
	return ct.SessionID, nil
}

// Stop halts the cleanup goroutine.
func (ts *TokenStore) Stop() {
	select {
	case <-ts.stopCh:
	default:
		close(ts.stopCh)
	}
}

func (ts *TokenStore) cleanupLoop() {
	ticker := time.NewTicker(tokenCleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ts.stopCh:
			return
		case <-ticker.C:
			ts.mu.Lock()
			now := time.Now()
			for k, ct := range ts.tokens {
				if now.After(ct.ExpiresAt) {
					delete(ts.tokens, k)
				}
			}
			ts.mu.Unlock()
		}
	}
}

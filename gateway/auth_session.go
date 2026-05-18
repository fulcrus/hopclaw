package gateway

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"
)

// ---------------------------------------------------------------------------
// Session-based auth provider
// ---------------------------------------------------------------------------

const (
	sessionProviderName   = "session"
	sessionIDBytes        = 32
	sessionDefaultMaxAge  = 24 * time.Hour
	sessionReaperInterval = 5 * time.Minute
	sessionDefaultCookie  = "_hopclaw_session"
	csrfTokenBytes        = 32
	csrfHeaderName        = "X-CSRF-Token"
	csrfFormField         = "_csrf"
	csrfCookieName        = "_hopclaw_csrf"
)

// ---------------------------------------------------------------------------
// Session types
// ---------------------------------------------------------------------------

// AuthSession represents a server-side gateway auth session.
type AuthSession struct {
	ID         string        `json:"id"`
	Identity   *AuthIdentity `json:"identity"`
	CSRFToken  string        `json:"csrf_token"`
	CreatedAt  time.Time     `json:"created_at"`
	ExpiresAt  time.Time     `json:"expires_at"`
	LastSeenAt time.Time     `json:"last_seen_at"`
}

// Deprecated: Session is an alias for AuthSession.
type Session = AuthSession

// AuthSessionConfig holds configuration for gateway auth-session cookies.
type AuthSessionConfig struct {
	CookieName   string        `json:"cookie_name" yaml:"cookie_name"`
	CookieDomain string        `json:"cookie_domain" yaml:"cookie_domain"`
	MaxAge       time.Duration `json:"max_age" yaml:"max_age"`
	Secure       bool          `json:"secure" yaml:"secure"`
}

// Deprecated: SessionConfig is an alias for AuthSessionConfig.
type SessionConfig = AuthSessionConfig

// ---------------------------------------------------------------------------
// Session store interface and in-memory implementation
// ---------------------------------------------------------------------------

// AuthSessionStore manages server-side auth sessions.
type AuthSessionStore interface {
	// Create creates a new session for the given identity.
	Create(identity *AuthIdentity) *AuthSession
	// Get retrieves a session by ID. Returns false if not found or expired.
	Get(id string) (*AuthSession, bool)
	// Delete removes a session by ID.
	Delete(id string)
	// Touch updates the last_seen_at timestamp.
	Touch(id string)
}

// MemoryAuthSessionStore is a thread-safe in-memory auth-session store.
type MemoryAuthSessionStore struct {
	mu       sync.RWMutex // guards sessions
	sessions map[string]*AuthSession
	maxAge   time.Duration
	stopCh   chan struct{}
}

// Deprecated: MemorySessionStore is an alias for MemoryAuthSessionStore.
type MemorySessionStore = MemoryAuthSessionStore

// NewMemoryAuthSessionStore creates an in-memory auth-session store with background
// reaper for expired sessions.
func NewMemoryAuthSessionStore(cfg AuthSessionConfig) *MemoryAuthSessionStore {
	maxAge := cfg.MaxAge
	if maxAge <= 0 {
		maxAge = sessionDefaultMaxAge
	}

	store := &MemoryAuthSessionStore{
		sessions: make(map[string]*AuthSession),
		maxAge:   maxAge,
		stopCh:   make(chan struct{}),
	}

	go store.reapLoop()
	return store
}

// Deprecated: NewMemorySessionStore uses gateway auth-session semantics.
func NewMemorySessionStore(cfg AuthSessionConfig) *MemoryAuthSessionStore {
	return NewMemoryAuthSessionStore(cfg)
}

// Create creates a new session for the given identity.
func (s *MemoryAuthSessionStore) Create(identity *AuthIdentity) *AuthSession {
	id, err := generateSessionID()
	if err != nil {
		// Fallback to timestamp-based ID (should never happen in practice).
		id = fmt.Sprintf("sess-%d", time.Now().UnixNano())
	}

	csrfToken, err := generateCSRFToken()
	if err != nil {
		csrfToken = fmt.Sprintf("csrf-%d", time.Now().UnixNano())
	}

	now := time.Now().UTC()
	session := &AuthSession{
		ID:         id,
		Identity:   identity,
		CSRFToken:  csrfToken,
		CreatedAt:  now,
		ExpiresAt:  now.Add(s.maxAge),
		LastSeenAt: now,
	}

	s.mu.Lock()
	s.sessions[id] = session
	s.mu.Unlock()

	return session
}

// Get retrieves a session by ID. Returns false if not found or expired.
func (s *MemoryAuthSessionStore) Get(id string) (*AuthSession, bool) {
	s.mu.RLock()
	session, ok := s.sessions[id]
	s.mu.RUnlock()

	if !ok {
		return nil, false
	}

	if time.Now().After(session.ExpiresAt) {
		s.Delete(id)
		return nil, false
	}

	return session, true
}

// Delete removes a session by ID.
func (s *MemoryAuthSessionStore) Delete(id string) {
	s.mu.Lock()
	delete(s.sessions, id)
	s.mu.Unlock()
}

// Touch updates the last_seen_at timestamp for a session.
func (s *MemoryAuthSessionStore) Touch(id string) {
	s.mu.Lock()
	if session, ok := s.sessions[id]; ok {
		session.LastSeenAt = time.Now().UTC()
	}
	s.mu.Unlock()
}

// Close stops the background reaper.
func (s *MemoryAuthSessionStore) Close() {
	close(s.stopCh)
}

// reapLoop periodically removes expired sessions.
func (s *MemoryAuthSessionStore) reapLoop() {
	ticker := time.NewTicker(sessionReaperInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			s.reap()
		case <-s.stopCh:
			return
		}
	}
}

func (s *MemoryAuthSessionStore) reap() {
	now := time.Now()
	s.mu.Lock()
	defer s.mu.Unlock()

	for id, session := range s.sessions {
		if now.After(session.ExpiresAt) {
			delete(s.sessions, id)
		}
	}
}

// ---------------------------------------------------------------------------
// Session auth provider
// ---------------------------------------------------------------------------

// AuthSessionProvider implements AuthProvider by reading gateway auth-session cookies.
type AuthSessionProvider struct {
	store      AuthSessionStore
	cookieName string
}

// Deprecated: SessionProvider is an alias for AuthSessionProvider.
type SessionProvider = AuthSessionProvider

// NewAuthSessionProvider creates an auth-session provider.
func NewAuthSessionProvider(store AuthSessionStore, cfg AuthSessionConfig) *AuthSessionProvider {
	return &AuthSessionProvider{
		store:      store,
		cookieName: authSessionCookieName(cfg.CookieName),
	}
}

// Deprecated: NewSessionProvider uses gateway auth-session semantics.
func NewSessionProvider(store AuthSessionStore, cfg AuthSessionConfig) *AuthSessionProvider {
	return NewAuthSessionProvider(store, cfg)
}

// Name returns "session".
func (p *AuthSessionProvider) Name() string { return sessionProviderName }

// Authenticate checks for a valid session cookie.
// Returns (nil, nil) if no session cookie is present.
// Returns (*AuthIdentity, nil) if a valid, non-expired session exists.
// Returns (nil, nil) if the session is expired or not found (treat as
// unauthenticated rather than error, allowing re-login).
func (p *AuthSessionProvider) Authenticate(r *http.Request) (*AuthIdentity, error) {
	cookie, err := r.Cookie(p.cookieName)
	if err != nil || strings.TrimSpace(cookie.Value) == "" {
		return nil, nil
	}

	session, ok := p.store.Get(cookie.Value)
	if !ok {
		return nil, nil
	}

	p.store.Touch(cookie.Value)
	return session.Identity, nil
}

// ---------------------------------------------------------------------------
// CSRF middleware
// ---------------------------------------------------------------------------

// CSRFMiddleware validates CSRF tokens for state-changing requests (POST, PUT,
// PATCH, DELETE). The token is checked against the session's CSRF token via
// the X-CSRF-Token header or _csrf form field.
func AuthSessionCSRFMiddleware(store AuthSessionStore, cookieName string) func(http.Handler) http.Handler {
	if cookieName == "" {
		cookieName = sessionDefaultCookie
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Only validate state-changing methods.
			if r.Method == http.MethodGet || r.Method == http.MethodHead || r.Method == http.MethodOptions {
				next.ServeHTTP(w, r)
				return
			}

			cookie, err := r.Cookie(cookieName)
			if err != nil || strings.TrimSpace(cookie.Value) == "" {
				// No session cookie — let the auth chain handle it.
				next.ServeHTTP(w, r)
				return
			}

			session, ok := store.Get(cookie.Value)
			if !ok {
				next.ServeHTTP(w, r)
				return
			}

			// Extract CSRF token from header or form field.
			csrfToken := strings.TrimSpace(r.Header.Get(csrfHeaderName))
			if csrfToken == "" {
				csrfToken = strings.TrimSpace(r.FormValue(csrfFormField))
			}

			if csrfToken == "" || !constantTimeEqualCSRF(csrfToken, session.CSRFToken) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusForbidden)
				_, _ = w.Write([]byte(`{"error":"csrf token validation failed"}`))
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// Deprecated: CSRFMiddleware validates gateway auth-session CSRF tokens.
func CSRFMiddleware(store AuthSessionStore, cookieName string) func(http.Handler) http.Handler {
	return AuthSessionCSRFMiddleware(store, cookieName)
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func generateSessionID() (string, error) {
	b := make([]byte, sessionIDBytes)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	hash := sha256.Sum256(b)
	return "sess-" + hex.EncodeToString(hash[:]), nil
}

func generateCSRFToken() (string, error) {
	b := make([]byte, csrfTokenBytes)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func constantTimeEqualCSRF(a, b string) bool {
	if len(a) == 0 || len(b) == 0 {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

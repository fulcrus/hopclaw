package browserd

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	browsertypes "github.com/fulcrus/hopclaw/browserapi/types"
	apiresponse "github.com/fulcrus/hopclaw/internal/apiresponse"
	supportmaps "github.com/fulcrus/hopclaw/internal/support/maps"
)

var (
	ErrSessionIDRequired = errors.New("session_id is required")
	ErrSessionNotFound   = errors.New("browser session not found")
	ErrActionRequired    = errors.New("action is required")
)

const (
	// DefaultSessionIdleTimeout is the default idle lifetime for browser
	// sessions before they are eligible for automatic cleanup.
	DefaultSessionIdleTimeout = 10 * time.Minute
	// DefaultSessionCleanupInterval is the background cleanup cadence used by
	// the standalone browser daemon.
	DefaultSessionCleanupInterval = 30 * time.Second
	// DefaultMaxSessions is the default soft ceiling for concurrently tracked
	// browser sessions. Least-recently-used idle sessions are evicted first.
	DefaultMaxSessions = 8
)

type ManagerOption func(*Manager)

type managedSession struct {
	session      Session
	createdAt    time.Time
	lastActiveAt time.Time
	inflight     int
}

type SessionStats struct {
	Count            int
	MaxSessions      int
	IdleTimeout      time.Duration
	OldestSessionAge time.Duration
	LongestIdle      time.Duration
}

type Engine interface {
	OpenSession(ctx context.Context, spec OpenSessionSpec) (Session, error)
}

type Session interface {
	ID() string
	Handle(ctx context.Context, req browsertypes.Request) (*browsertypes.Response, error)
	Close(ctx context.Context) error
}

type OpenSessionSpec struct {
	ID     string
	Params map[string]any
}

type Manager struct {
	engine      Engine
	mu          sync.RWMutex
	sessions    map[string]*managedSession
	idleTimeout time.Duration
	maxSessions int
	now         func() time.Time
}

type Server struct {
	manager   *Manager
	profiles  *profileStore
	authToken string
}

func NewManager(engine Engine, opts ...ManagerOption) *Manager {
	manager := &Manager{
		engine:      engine,
		sessions:    make(map[string]*managedSession),
		idleTimeout: DefaultSessionIdleTimeout,
		maxSessions: DefaultMaxSessions,
		now: func() time.Time {
			return time.Now().UTC()
		},
	}
	for _, opt := range opts {
		if opt != nil {
			opt(manager)
		}
	}
	return manager
}

func WithSessionIdleTimeout(timeout time.Duration) ManagerOption {
	return func(m *Manager) {
		if m == nil {
			return
		}
		m.idleTimeout = timeout
	}
}

func WithMaxSessions(max int) ManagerOption {
	return func(m *Manager) {
		if m == nil {
			return
		}
		m.maxSessions = max
	}
}

func NewServer(manager *Manager, authToken string) *Server {
	return &Server{
		manager:   manager,
		authToken: strings.TrimSpace(authToken),
	}
}

// WithProfiles attaches a profile store for profile management endpoints.
func (s *Server) WithProfiles(ps *profileStore) *Server {
	s.profiles = ps
	return s
}

// WithProfilesDir attaches a persistent profile store rooted under baseDir.
func (s *Server) WithProfilesDir(baseDir string) error {
	ps, err := newProfileStore(baseDir)
	if err != nil {
		return err
	}
	s.profiles = ps
	return nil
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.handleHealthz)
	mux.Handle("POST /browser/v1", s.withAuth(http.HandlerFunc(s.handleInvoke)))
	mux.Handle("GET /browser/v1/profiles", s.withAuth(http.HandlerFunc(s.handleListProfiles)))
	mux.Handle("POST /browser/v1/profiles", s.withAuth(http.HandlerFunc(s.handleCreateProfile)))
	mux.Handle("DELETE /browser/v1/profiles/{name}", s.withAuth(http.HandlerFunc(s.handleDeleteProfile)))
	return mux
}

func (s *Server) Close(ctx context.Context) error {
	if s == nil {
		return nil
	}
	if s.manager != nil {
		return s.manager.CloseAll(ctx)
	}
	return nil
}

func (m *Manager) Handle(ctx context.Context, req browsertypes.Request) (*browsertypes.Response, error) {
	action := strings.TrimSpace(req.Action)
	if action == "" {
		return nil, ErrActionRequired
	}
	switch action {
	case browsertypes.ActionCreateSession:
		params := supportmaps.Clone(req.Params)
		if params == nil {
			params = make(map[string]any)
		}
		// Propagate browser_type from the request envelope into params
		// so engines can read it.
		if req.BrowserType != "" {
			if _, ok := params["browser_type"]; !ok {
				params["browser_type"] = string(req.BrowserType)
			}
		}
		return m.openSession(ctx, params)
	case browsertypes.ActionCloseSession:
		return m.closeSession(ctx, strings.TrimSpace(req.SessionID))
	default:
		sessionID := strings.TrimSpace(req.SessionID)
		if sessionID == "" {
			return nil, ErrSessionIDRequired
		}
		record, ok := m.acquire(sessionID)
		if !ok {
			return nil, ErrSessionNotFound
		}
		defer m.release(record)
		return record.session.Handle(ctx, req)
	}
}

func (m *Manager) CloseAll(ctx context.Context) error {
	if m == nil {
		return nil
	}
	m.mu.Lock()
	sessions := make([]Session, 0, len(m.sessions))
	for id, session := range m.sessions {
		sessions = append(sessions, session.session)
		delete(m.sessions, id)
	}
	m.mu.Unlock()

	var firstErr error
	for _, session := range sessions {
		if err := session.Close(ctx); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func (m *Manager) openSession(ctx context.Context, params map[string]any) (*browsertypes.Response, error) {
	if m == nil || m.engine == nil {
		return nil, errors.New("browser engine is not configured")
	}
	if err := m.cleanupSessions(ctx, 1); err != nil {
		log.Warn("browser session cleanup before open failed", "error", err)
	}
	sessionID, err := randomID()
	if err != nil {
		return nil, fmt.Errorf("generate session id: %w", err)
	}
	session, err := m.engine.OpenSession(ctx, OpenSessionSpec{
		ID:     sessionID,
		Params: supportmaps.Clone(params),
	})
	if err != nil {
		return nil, err
	}
	if session == nil || strings.TrimSpace(session.ID()) == "" {
		return nil, errors.New("browser engine returned an empty session id")
	}

	now := m.currentTime()
	m.mu.Lock()
	m.sessions[session.ID()] = &managedSession{
		session:      session,
		createdAt:    now,
		lastActiveAt: now,
	}
	m.mu.Unlock()

	return &browsertypes.Response{
		OK:        true,
		SessionID: session.ID(),
		Data: map[string]any{
			"session_id": session.ID(),
		},
	}, nil
}

func (m *Manager) closeSession(ctx context.Context, sessionID string) (*browsertypes.Response, error) {
	if sessionID == "" {
		return nil, ErrSessionIDRequired
	}

	m.mu.Lock()
	record, ok := m.sessions[sessionID]
	if ok {
		delete(m.sessions, sessionID)
	}
	m.mu.Unlock()
	if !ok {
		return nil, ErrSessionNotFound
	}
	if err := record.session.Close(ctx); err != nil {
		return nil, err
	}
	return &browsertypes.Response{
		OK:        true,
		SessionID: sessionID,
		Data: map[string]any{
			"session_id": sessionID,
			"closed":     true,
		},
	}, nil
}

func (m *Manager) get(sessionID string) (Session, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	session, ok := m.sessions[sessionID]
	if !ok {
		return nil, false
	}
	return session.session, true
}

func (m *Manager) SessionCount() int {
	return m.Stats().Count
}

func (m *Manager) Stats() SessionStats {
	if m == nil {
		return SessionStats{}
	}
	now := m.currentTime()
	m.mu.RLock()
	defer m.mu.RUnlock()
	stats := SessionStats{
		Count:       len(m.sessions),
		MaxSessions: m.maxSessions,
		IdleTimeout: m.idleTimeout,
	}
	for _, session := range m.sessions {
		if !session.createdAt.IsZero() {
			age := now.Sub(session.createdAt)
			if age > stats.OldestSessionAge {
				stats.OldestSessionAge = age
			}
		}
		if !session.lastActiveAt.IsZero() {
			idle := now.Sub(session.lastActiveAt)
			if idle > stats.LongestIdle {
				stats.LongestIdle = idle
			}
		}
	}
	return stats
}

func (m *Manager) CleanupIdle(ctx context.Context) error {
	return m.cleanupSessions(ctx, 0)
}

func (m *Manager) RunCleanupLoop(ctx context.Context, interval time.Duration) {
	if m == nil {
		return
	}
	if interval <= 0 {
		interval = DefaultSessionCleanupInterval
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := m.CleanupIdle(context.Background()); err != nil {
				log.Warn("browser idle session cleanup failed", "error", err)
			}
		}
	}
}

func (m *Manager) acquire(sessionID string) (*managedSession, bool) {
	if m == nil {
		return nil, false
	}
	now := m.currentTime()
	m.mu.Lock()
	defer m.mu.Unlock()
	record, ok := m.sessions[sessionID]
	if !ok {
		return nil, false
	}
	record.lastActiveAt = now
	record.inflight++
	return record, true
}

func (m *Manager) release(record *managedSession) {
	if m == nil || record == nil {
		return
	}
	now := m.currentTime()
	m.mu.Lock()
	defer m.mu.Unlock()
	if record.inflight > 0 {
		record.inflight--
	}
	record.lastActiveAt = now
}

func (m *Manager) cleanupSessions(ctx context.Context, incoming int) error {
	sessions := m.collectSessionsToClose(incoming)
	var firstErr error
	for _, session := range sessions {
		if err := session.Close(ctx); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func (m *Manager) collectSessionsToClose(incoming int) []Session {
	if m == nil {
		return nil
	}
	now := m.currentTime()
	m.mu.Lock()
	defer m.mu.Unlock()

	type candidate struct {
		id           string
		session      Session
		createdAt    time.Time
		lastActiveAt time.Time
	}

	candidates := make([]candidate, 0, len(m.sessions))
	for id, record := range m.sessions {
		if record == nil || record.session == nil || record.inflight > 0 {
			continue
		}
		candidates = append(candidates, candidate{
			id:           id,
			session:      record.session,
			createdAt:    record.createdAt,
			lastActiveAt: record.lastActiveAt,
		})
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		if !candidates[i].lastActiveAt.Equal(candidates[j].lastActiveAt) {
			return candidates[i].lastActiveAt.Before(candidates[j].lastActiveAt)
		}
		if !candidates[i].createdAt.Equal(candidates[j].createdAt) {
			return candidates[i].createdAt.Before(candidates[j].createdAt)
		}
		return candidates[i].id < candidates[j].id
	})

	selected := make(map[string]struct{})
	out := make([]Session, 0, len(candidates))
	selectCandidate := func(c candidate) {
		if _, ok := selected[c.id]; ok {
			return
		}
		if _, ok := m.sessions[c.id]; !ok {
			return
		}
		selected[c.id] = struct{}{}
		delete(m.sessions, c.id)
		out = append(out, c.session)
	}

	if m.idleTimeout > 0 {
		for _, c := range candidates {
			if c.lastActiveAt.IsZero() || now.Sub(c.lastActiveAt) < m.idleTimeout {
				continue
			}
			selectCandidate(c)
		}
	}

	if m.maxSessions > 0 {
		target := m.maxSessions - incoming
		if target < 0 {
			target = 0
		}
		for _, c := range candidates {
			if len(m.sessions) <= target {
				break
			}
			selectCandidate(c)
		}
	}
	return out
}

func (m *Manager) currentTime() time.Time {
	if m == nil || m.now == nil {
		return time.Now().UTC()
	}
	return m.now().UTC()
}

func (s *Server) handleListProfiles(w http.ResponseWriter, _ *http.Request) {
	if s.profiles == nil {
		writeBrowserError(w, http.StatusNotImplemented, errors.New("profile store is not configured"))
		return
	}
	writeJSON(w, http.StatusOK, s.profiles.List())
}

func (s *Server) handleCreateProfile(w http.ResponseWriter, r *http.Request) {
	if s.profiles == nil {
		writeBrowserError(w, http.StatusNotImplemented, errors.New("profile store is not configured"))
		return
	}
	var req browserProfile
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeBrowserError(w, http.StatusBadRequest, fmt.Errorf("decode profile request: %w", err))
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		writeBrowserError(w, http.StatusBadRequest, errors.New("profile name is required"))
		return
	}
	profile, err := s.profiles.Create(name, req)
	if err != nil {
		writeBrowserError(w, http.StatusConflict, err)
		return
	}
	writeJSON(w, http.StatusCreated, profile)
}

func (s *Server) handleDeleteProfile(w http.ResponseWriter, r *http.Request) {
	if s.profiles == nil {
		writeBrowserError(w, http.StatusNotImplemented, errors.New("profile store is not configured"))
		return
	}
	name := strings.TrimSpace(r.PathValue("name"))
	if name == "" {
		writeBrowserError(w, http.StatusBadRequest, errors.New("profile name is required"))
		return
	}
	if err := s.profiles.Delete(name); err != nil {
		status := http.StatusInternalServerError
		if strings.Contains(err.Error(), "not found") || strings.Contains(err.Error(), "cannot delete") {
			status = http.StatusBadRequest
		}
		writeBrowserError(w, status, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "deleted": name})
}

func (s *Server) handleHealthz(w http.ResponseWriter, _ *http.Request) {
	stats := SessionStats{}
	if s.manager != nil {
		stats = s.manager.Stats()
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":                       true,
		"session_count":            stats.Count,
		"max_sessions":             stats.MaxSessions,
		"session_idle_timeout_sec": int(stats.IdleTimeout.Seconds()),
		"oldest_session_age_sec":   int(stats.OldestSessionAge.Seconds()),
		"longest_session_idle_sec": int(stats.LongestIdle.Seconds()),
	})
}

func (s *Server) handleInvoke(w http.ResponseWriter, r *http.Request) {
	var req browsertypes.Request
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeBrowserError(w, http.StatusBadRequest, fmt.Errorf("decode request: %w", err))
		return
	}
	resp, err := s.manager.Handle(r.Context(), req)
	if err != nil {
		status := http.StatusInternalServerError
		switch {
		case errors.Is(err, ErrActionRequired), errors.Is(err, ErrSessionIDRequired):
			status = http.StatusBadRequest
		case errors.Is(err, ErrSessionNotFound):
			status = http.StatusNotFound
		case strings.Contains(strings.ToLower(err.Error()), "not implemented"):
			status = http.StatusNotImplemented
		}
		writeBrowserError(w, status, err)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) withAuth(next http.Handler) http.Handler {
	if s.authToken == "" {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if browserAuthorized(r, s.authToken) {
			next.ServeHTTP(w, r)
			return
		}
		w.Header().Set("WWW-Authenticate", `Bearer realm="hopclaw-browserd"`)
		writeBrowserError(w, http.StatusUnauthorized, errors.New("missing or invalid auth token"))
	})
}

func browserAuthorized(r *http.Request, token string) bool {
	if tokenEqual(strings.TrimSpace(r.Header.Get("X-HopClaw-Token")), token) {
		return true
	}
	if tokenEqual(strings.TrimSpace(r.Header.Get("X-OpenClaw-Token")), token) {
		return true
	}
	if tokenEqual(strings.TrimSpace(r.Header.Get("X-HopClaw-Password")), token) {
		return true
	}
	authz := strings.TrimSpace(r.Header.Get("Authorization"))
	if strings.HasPrefix(strings.ToLower(authz), "bearer ") {
		return tokenEqual(strings.TrimSpace(authz[len("Bearer "):]), token)
	}
	if strings.HasPrefix(strings.ToLower(authz), "basic ") {
		decoded, err := base64.StdEncoding.DecodeString(strings.TrimSpace(authz[len("Basic "):]))
		if err == nil {
			parts := strings.SplitN(string(decoded), ":", 2)
			if len(parts) == 2 {
				return tokenEqual(parts[1], token)
			}
		}
	}
	return false
}

func tokenEqual(got, want string) bool {
	if got == "" || want == "" || len(got) != len(want) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(got), []byte(want)) == 1
}

func writeBrowserError(w http.ResponseWriter, status int, err error) {
	writeJSON(w, status, browsertypes.Response{
		OK:    false,
		Error: err.Error(),
	})
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	apiresponse.WriteJSON(context.Background(), w, status, payload, "write browserd json response failed")
}

func randomID() (string, error) {
	var buf [16]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf[:]), nil
}

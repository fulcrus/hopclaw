package acp

import (
	"context"
	"sync"
	"time"
)

const (
	// maxSessions is the upper limit of tracked sessions in the store.
	maxSessions = 5000
	// sessionIdleTTL is how long a session can remain idle before reaping.
	sessionIdleTTL = 24 * time.Hour
	// reapInterval is how often the reaper goroutine scans for idle sessions.
	reapInterval = 5 * time.Minute
)

// session tracks the mapping between an ACP session and the gateway.
type session struct {
	ID            string
	GatewayKey    string
	CWD           string
	CreatedAt     time.Time
	LastTouchedAt time.Time
	ActiveRunID   string
	Cancel        context.CancelFunc
	ConfigOptions map[ConfigOptionKey]string
}

// SessionStore manages ACP sessions and their gateway key mappings.
type SessionStore struct {
	mu       sync.Mutex // guards sessions
	sessions map[string]*session
	done     chan struct{}
}

// NewSessionStore creates a SessionStore and starts its background reaper.
func NewSessionStore() *SessionStore {
	s := &SessionStore{
		sessions: make(map[string]*session),
		done:     make(chan struct{}),
	}
	go s.reapLoop()
	return s
}

// Get returns the session with the given id, if it exists.
func (s *SessionStore) Get(id string) (*session, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sess, ok := s.sessions[id]
	return sess, ok
}

// GetOrCreate returns the existing session for id, or creates one with the
// provided gatewayKey and cwd. When the store is at capacity, creation fails
// silently by returning a new session without storing it (the caller should
// check the capacity error path in production usage).
func (s *SessionStore) GetOrCreate(id, gatewayKey, cwd string) *session {
	s.mu.Lock()
	defer s.mu.Unlock()

	if sess, ok := s.sessions[id]; ok {
		return sess
	}

	now := time.Now().UTC()
	sess := &session{
		ID:            id,
		GatewayKey:    gatewayKey,
		CWD:           cwd,
		CreatedAt:     now,
		LastTouchedAt: now,
		ConfigOptions: make(map[ConfigOptionKey]string),
	}

	if len(s.sessions) < maxSessions {
		s.sessions[id] = sess
	}
	return sess
}

// Touch updates the LastTouchedAt timestamp for the session.
func (s *SessionStore) Touch(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if sess, ok := s.sessions[id]; ok {
		sess.LastTouchedAt = time.Now().UTC()
	}
}

// SetActiveRun records the active run for a session so it can be cancelled.
func (s *SessionStore) SetActiveRun(id, runID string, cancel context.CancelFunc) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if sess, ok := s.sessions[id]; ok {
		sess.ActiveRunID = runID
		sess.Cancel = cancel
	}
}

// ClearActiveRun removes the active run tracking for a session.
func (s *SessionStore) ClearActiveRun(id string) {
	s.ClearActiveRunIfMatch(id, "")
}

// ClearActiveRunIfMatch removes the active run tracking for a session only if
// the currently tracked run matches runID. An empty runID clears unconditionally.
func (s *SessionStore) ClearActiveRunIfMatch(id, runID string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if sess, ok := s.sessions[id]; ok {
		if runID != "" && sess.ActiveRunID != runID {
			return
		}
		sess.ActiveRunID = ""
		sess.Cancel = nil
	}
}

// SnapshotActive atomically reads the gateway key, active run id, and cancel
// function for a session without mutating store state. Callers can issue any
// gateway or context-cancel operations outside the store lock and then call
// ClearActiveRunIfMatch to clear the slot.
func (s *SessionStore) SnapshotActive(id string) (gatewayKey, runID string, cancel context.CancelFunc, exists bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	sess, ok := s.sessions[id]
	if !ok {
		return "", "", nil, false
	}
	return sess.GatewayKey, sess.ActiveRunID, sess.Cancel, true
}

func (s *SessionStore) SetConfigOption(id string, key ConfigOptionKey, value string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if sess, ok := s.sessions[id]; ok {
		if sess.ConfigOptions == nil {
			sess.ConfigOptions = make(map[ConfigOptionKey]string)
		}
		sess.ConfigOptions[key] = value
	}
}

func (s *SessionStore) ConfigOptions(id string) map[ConfigOptionKey]string {
	s.mu.Lock()
	defer s.mu.Unlock()

	sess, ok := s.sessions[id]
	if !ok || len(sess.ConfigOptions) == 0 {
		return nil
	}

	out := make(map[ConfigOptionKey]string, len(sess.ConfigOptions))
	for key, value := range sess.ConfigOptions {
		out[key] = value
	}
	return out
}

// List returns a paginated slice of SessionInfo.
func (s *SessionStore) List(limit, offset int) []SessionInfo {
	s.mu.Lock()
	defer s.mu.Unlock()

	all := make([]SessionInfo, 0, len(s.sessions))
	for _, sess := range s.sessions {
		status := SessionIdle
		if sess.ActiveRunID != "" {
			status = SessionStreaming
		}
		all = append(all, SessionInfo{
			SessionID:  sess.ID,
			SessionKey: sess.GatewayKey,
			Status:     status,
			CreatedAt:  sess.CreatedAt,
		})
	}

	if offset >= len(all) {
		return nil
	}
	all = all[offset:]
	if limit > 0 && limit < len(all) {
		all = all[:limit]
	}
	return all
}

// Remove deletes a session from the store. If the session has an active run,
// its cancel function is called first.
func (s *SessionStore) Remove(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if sess, ok := s.sessions[id]; ok {
		if sess.Cancel != nil {
			sess.Cancel()
		}
		delete(s.sessions, id)
	}
}

// Stop shuts down the background reaper goroutine.
func (s *SessionStore) Stop() {
	close(s.done)
}

// reapLoop periodically removes idle sessions that have exceeded sessionIdleTTL.
func (s *SessionStore) reapLoop() {
	ticker := time.NewTicker(reapInterval)
	defer ticker.Stop()

	for {
		select {
		case <-s.done:
			return
		case now := <-ticker.C:
			s.mu.Lock()
			for id, sess := range s.sessions {
				if now.Sub(sess.LastTouchedAt) > sessionIdleTTL {
					if sess.Cancel != nil {
						sess.Cancel()
					}
					delete(s.sessions, id)
				}
			}
			s.mu.Unlock()
		}
	}
}

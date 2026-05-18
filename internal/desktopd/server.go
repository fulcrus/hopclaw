package desktopd

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"

	desktoptypes "github.com/fulcrus/hopclaw/desktopapi/types"
	apiresponse "github.com/fulcrus/hopclaw/internal/apiresponse"
	supportmaps "github.com/fulcrus/hopclaw/internal/support/maps"
)

var (
	ErrSessionIDRequired = errors.New("session_id is required")
	ErrSessionNotFound   = errors.New("desktop session not found")
	ErrActionRequired    = errors.New("action is required")
)

type Engine interface {
	OpenSession(ctx context.Context, spec OpenSessionSpec) (Session, error)
}

type Invoker interface {
	Invoke(ctx context.Context, req desktoptypes.Request) (*desktoptypes.Response, error)
}

type Session interface {
	ID() string
	Handle(ctx context.Context, req desktoptypes.Request) (*desktoptypes.Response, error)
	Close(ctx context.Context) error
}

type SessionMetadataProvider interface {
	SessionMetadata(context.Context) map[string]any
}

type OpenSessionSpec struct {
	ID     string
	Params map[string]any
}

type Manager struct {
	engine   Engine
	mu       sync.RWMutex
	sessions map[string]Session
}

type Server struct {
	manager   *Manager
	authToken string
}

func NewManager(engine Engine) *Manager {
	return &Manager{
		engine:   engine,
		sessions: make(map[string]Session),
	}
}

func NewServer(manager *Manager, authToken string) *Server {
	return &Server{
		manager:   manager,
		authToken: strings.TrimSpace(authToken),
	}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.handleHealthz)
	mux.Handle("POST /desktop/v1", s.withAuth(http.HandlerFunc(s.handleInvoke)))
	return mux
}

func (s *Server) Close(ctx context.Context) error {
	if s == nil || s.manager == nil {
		return nil
	}
	return s.manager.CloseAll(ctx)
}

func (m *Manager) Handle(ctx context.Context, req desktoptypes.Request) (*desktoptypes.Response, error) {
	action := strings.TrimSpace(req.Action)
	if action == "" {
		return nil, ErrActionRequired
	}
	switch action {
	case desktoptypes.ActionCreateSession:
		return m.openSession(ctx, req.Params)
	case desktoptypes.ActionCloseSession:
		return m.closeSession(ctx, strings.TrimSpace(req.SessionID))
	default:
		sessionID := strings.TrimSpace(req.SessionID)
		if sessionID == "" {
			if invoker, ok := m.engine.(Invoker); ok {
				return invoker.Invoke(ctx, req)
			}
			return nil, ErrSessionIDRequired
		}
		session, ok := m.get(sessionID)
		if !ok {
			return nil, ErrSessionNotFound
		}
		return session.Handle(ctx, req)
	}
}

func (m *Manager) CloseAll(ctx context.Context) error {
	if m == nil {
		return nil
	}
	m.mu.Lock()
	sessions := make([]Session, 0, len(m.sessions))
	for id, session := range m.sessions {
		sessions = append(sessions, session)
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

func (m *Manager) openSession(ctx context.Context, params map[string]any) (*desktoptypes.Response, error) {
	if m == nil || m.engine == nil {
		return nil, errors.New("desktop engine is not configured")
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
		return nil, errors.New("desktop engine returned an empty session id")
	}
	m.mu.Lock()
	m.sessions[session.ID()] = session
	m.mu.Unlock()

	return &desktoptypes.Response{
		OK:        true,
		SessionID: session.ID(),
		Data: map[string]any{
			"session_id": session.ID(),
			"metadata":   sessionMetadata(ctx, session),
		},
	}, nil
}

func (m *Manager) closeSession(ctx context.Context, sessionID string) (*desktoptypes.Response, error) {
	if sessionID == "" {
		return nil, ErrSessionIDRequired
	}
	m.mu.Lock()
	session, ok := m.sessions[sessionID]
	if ok {
		delete(m.sessions, sessionID)
	}
	m.mu.Unlock()
	if !ok {
		return nil, ErrSessionNotFound
	}
	if err := session.Close(ctx); err != nil {
		return nil, err
	}
	return &desktoptypes.Response{
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
	return session, ok
}

func sessionMetadata(ctx context.Context, session Session) map[string]any {
	if session == nil {
		return nil
	}
	provider, ok := session.(SessionMetadataProvider)
	if !ok {
		return nil
	}
	return supportmaps.Clone(provider.SessionMetadata(ctx))
}

func (m *Manager) SessionCount() int {
	if m == nil {
		return 0
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.sessions)
}

func (s *Server) handleHealthz(w http.ResponseWriter, _ *http.Request) {
	sessionCount := 0
	if s.manager != nil {
		sessionCount = s.manager.SessionCount()
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":            true,
		"session_count": sessionCount,
	})
}

func (s *Server) handleInvoke(w http.ResponseWriter, r *http.Request) {
	var req desktoptypes.Request
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeDesktopError(w, http.StatusBadRequest, fmt.Errorf("decode request: %w", err))
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
		writeDesktopError(w, status, err)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) withAuth(next http.Handler) http.Handler {
	if s.authToken == "" {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if desktopAuthorized(r, s.authToken) {
			next.ServeHTTP(w, r)
			return
		}
		w.Header().Set("WWW-Authenticate", `Bearer realm="hopclaw-desktopd"`)
		writeDesktopError(w, http.StatusUnauthorized, errors.New("missing or invalid auth token"))
	})
}

func desktopAuthorized(r *http.Request, token string) bool {
	if tokenEqual(strings.TrimSpace(r.Header.Get("X-HopClaw-Token")), token) {
		return true
	}
	if tokenEqual(strings.TrimSpace(r.Header.Get("X-OpenClaw-Token")), token) {
		return true
	}
	authz := strings.TrimSpace(r.Header.Get("Authorization"))
	if strings.HasPrefix(strings.ToLower(authz), "bearer ") {
		return tokenEqual(strings.TrimSpace(authz[len("Bearer "):]), token)
	}
	return false
}

func tokenEqual(got, want string) bool {
	if got == "" || want == "" || len(got) != len(want) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(got), []byte(want)) == 1
}

func writeDesktopError(w http.ResponseWriter, status int, err error) {
	writeJSON(w, status, desktoptypes.Response{
		OK:    false,
		Error: err.Error(),
	})
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	apiresponse.WriteJSON(context.Background(), w, status, payload, "write desktopd json response failed")
}

func randomID() (string, error) {
	var buf [16]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf[:]), nil
}

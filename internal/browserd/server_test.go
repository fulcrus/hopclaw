package browserd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	browsertypes "github.com/fulcrus/hopclaw/browserapi/types"
)

type fakeEngine struct {
	openCount int
	session   Session
	err       error
}

func (e *fakeEngine) OpenSession(_ context.Context, spec OpenSessionSpec) (Session, error) {
	e.openCount++
	if e.err != nil {
		return nil, e.err
	}
	if e.session != nil {
		if fake, ok := e.session.(*fakeSession); ok && fake.id == "" {
			fake.id = spec.ID
		}
		return e.session, nil
	}
	return &fakeSession{id: spec.ID}, nil
}

type fakeSession struct {
	id        string
	closed    int
	lastReq   browsertypes.Request
	handleErr error
	resp      *browsertypes.Response
}

func (s *fakeSession) ID() string { return s.id }

func (s *fakeSession) Handle(_ context.Context, req browsertypes.Request) (*browsertypes.Response, error) {
	s.lastReq = req
	if s.handleErr != nil {
		return nil, s.handleErr
	}
	if s.resp != nil {
		return s.resp, nil
	}
	return &browsertypes.Response{OK: true, Data: map[string]any{"echo": req.Action}}, nil
}

func (s *fakeSession) Close(_ context.Context) error {
	s.closed++
	return nil
}

func TestServerHealthzIsPublic(t *testing.T) {
	srv := NewServer(NewManager(&fakeEngine{}), "secret")
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)

	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("healthz status = %d", rec.Code)
	}
}

func TestServerRequiresAuthForBrowserAPI(t *testing.T) {
	srv := NewServer(NewManager(&fakeEngine{}), "secret")
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/browser/v1", bytes.NewReader([]byte(`{"action":"create_session"}`)))

	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d", rec.Code)
	}
}

func TestServerCreateInvokeAndCloseSession(t *testing.T) {
	engine := &fakeEngine{}
	srv := NewServer(NewManager(engine), "")

	create := invokeBrowser(t, srv, browsertypes.Request{Action: browsertypes.ActionCreateSession})
	if !create.OK {
		t.Fatalf("create ok = false: %+v", create)
	}
	sessionID := create.SessionID
	if sessionID == "" {
		t.Fatal("missing session id")
	}
	if create.Data["session_id"] != sessionID {
		t.Fatalf("create data session_id = %#v", create.Data["session_id"])
	}

	invoke := invokeBrowser(t, srv, browsertypes.Request{
		Action:    browsertypes.ActionSnapshot,
		SessionID: sessionID,
	})
	if !invoke.OK {
		t.Fatalf("invoke ok = false: %+v", invoke)
	}

	closeResp := invokeBrowser(t, srv, browsertypes.Request{
		Action:    browsertypes.ActionCloseSession,
		SessionID: sessionID,
	})
	if !closeResp.OK {
		t.Fatalf("close ok = false: %+v", closeResp)
	}

	rec := httptest.NewRecorder()
	reqBody, _ := json.Marshal(browsertypes.Request{
		Action:    browsertypes.ActionSnapshot,
		SessionID: sessionID,
	})
	req := httptest.NewRequest(http.MethodPost, "/browser/v1", bytes.NewReader(reqBody))
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("post-close status = %d", rec.Code)
	}
}

func TestServerMapsNotImplementedTo501(t *testing.T) {
	session := &fakeSession{handleErr: errors.New("download not implemented")}
	srv := NewServer(NewManager(&fakeEngine{session: session}), "")
	create := invokeBrowser(t, srv, browsertypes.Request{Action: browsertypes.ActionCreateSession})

	rec := httptest.NewRecorder()
	body, _ := json.Marshal(browsertypes.Request{
		Action:    browsertypes.ActionDownload,
		SessionID: create.SessionID,
	})
	req := httptest.NewRequest(http.MethodPost, "/browser/v1", bytes.NewReader(body))
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusNotImplemented {
		t.Fatalf("status = %d", rec.Code)
	}
}

func TestManagerCloseAll(t *testing.T) {
	first := &fakeSession{id: "one"}
	second := &fakeSession{id: "two"}
	manager := &Manager{
		sessions: map[string]*managedSession{
			first.id:  {session: first},
			second.id: {session: second},
		},
	}

	if err := manager.CloseAll(context.Background()); err != nil {
		t.Fatalf("CloseAll: %v", err)
	}
	if first.closed != 1 || second.closed != 1 {
		t.Fatalf("closed counts = %d, %d", first.closed, second.closed)
	}
	if _, ok := manager.get(first.id); ok {
		t.Fatal("first session still present")
	}
}

func TestManagerCleanupIdleClosesExpiredSessions(t *testing.T) {
	now := time.Date(2026, 3, 24, 10, 0, 0, 0, time.UTC)
	expired := &fakeSession{id: "expired"}
	active := &fakeSession{id: "active"}
	manager := NewManager(&fakeEngine{}, WithSessionIdleTimeout(30*time.Second), WithMaxSessions(0))
	manager.now = func() time.Time { return now }
	manager.sessions = map[string]*managedSession{
		expired.id: {
			session:      expired,
			createdAt:    now.Add(-2 * time.Minute),
			lastActiveAt: now.Add(-45 * time.Second),
		},
		active.id: {
			session:      active,
			createdAt:    now.Add(-2 * time.Minute),
			lastActiveAt: now.Add(-5 * time.Second),
		},
	}

	if err := manager.CleanupIdle(context.Background()); err != nil {
		t.Fatalf("CleanupIdle: %v", err)
	}
	if expired.closed != 1 {
		t.Fatalf("expired.closed = %d, want 1", expired.closed)
	}
	if active.closed != 0 {
		t.Fatalf("active.closed = %d, want 0", active.closed)
	}
	if count := manager.SessionCount(); count != 1 {
		t.Fatalf("SessionCount() = %d, want 1", count)
	}
	if _, ok := manager.get(active.id); !ok {
		t.Fatal("active session missing after cleanup")
	}
}

func TestManagerCleanupEnforcesMaxSessionsWithLeastRecentlyUsedEviction(t *testing.T) {
	now := time.Date(2026, 3, 24, 10, 5, 0, 0, time.UTC)
	oldest := &fakeSession{id: "oldest"}
	middle := &fakeSession{id: "middle"}
	newest := &fakeSession{id: "newest"}
	manager := NewManager(&fakeEngine{}, WithSessionIdleTimeout(0), WithMaxSessions(2))
	manager.now = func() time.Time { return now }
	manager.sessions = map[string]*managedSession{
		oldest.id: {
			session:      oldest,
			createdAt:    now.Add(-3 * time.Minute),
			lastActiveAt: now.Add(-2 * time.Minute),
		},
		middle.id: {
			session:      middle,
			createdAt:    now.Add(-2 * time.Minute),
			lastActiveAt: now.Add(-90 * time.Second),
		},
		newest.id: {
			session:      newest,
			createdAt:    now.Add(-1 * time.Minute),
			lastActiveAt: now.Add(-30 * time.Second),
		},
	}

	if err := manager.CleanupIdle(context.Background()); err != nil {
		t.Fatalf("CleanupIdle: %v", err)
	}
	if oldest.closed != 1 {
		t.Fatalf("oldest.closed = %d, want 1", oldest.closed)
	}
	if middle.closed != 0 || newest.closed != 0 {
		t.Fatalf("unexpected closed counts middle=%d newest=%d", middle.closed, newest.closed)
	}
	if count := manager.SessionCount(); count != 2 {
		t.Fatalf("SessionCount() = %d, want 2", count)
	}
	if _, ok := manager.get(oldest.id); ok {
		t.Fatal("oldest session should be evicted")
	}
}

func invokeBrowser(t *testing.T, srv *Server, reqBody browsertypes.Request) browsertypes.Response {
	t.Helper()
	body, err := json.Marshal(reqBody)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/browser/v1", bytes.NewReader(body))
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var resp browsertypes.Response
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	return resp
}

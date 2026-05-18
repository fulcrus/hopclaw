package desktopd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	desktoptypes "github.com/fulcrus/hopclaw/desktopapi/types"
)

type fakeEngine struct {
	openCount int
	session   Session
	err       error
	invoke    func(context.Context, desktoptypes.Request) (*desktoptypes.Response, error)
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

func (e *fakeEngine) Invoke(ctx context.Context, req desktoptypes.Request) (*desktoptypes.Response, error) {
	if e.invoke != nil {
		return e.invoke(ctx, req)
	}
	return nil, ErrSessionIDRequired
}

type fakeSession struct {
	id        string
	closed    int
	lastReq   desktoptypes.Request
	handleErr error
	resp      *desktoptypes.Response
	metadata  map[string]any
}

func (s *fakeSession) ID() string { return s.id }

func (s *fakeSession) Handle(_ context.Context, req desktoptypes.Request) (*desktoptypes.Response, error) {
	s.lastReq = req
	if s.handleErr != nil {
		return nil, s.handleErr
	}
	if s.resp != nil {
		return s.resp, nil
	}
	return &desktoptypes.Response{OK: true, Data: map[string]any{"echo": req.Action}}, nil
}

func (s *fakeSession) Close(_ context.Context) error {
	s.closed++
	return nil
}

func (s *fakeSession) SessionMetadata(_ context.Context) map[string]any {
	return s.metadata
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

func TestServerRequiresAuthForDesktopAPI(t *testing.T) {
	srv := NewServer(NewManager(&fakeEngine{}), "secret")
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/desktop/v1", bytes.NewReader([]byte(`{"action":"create_session"}`)))

	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d", rec.Code)
	}
}

func TestServerCreateInvokeAndCloseSession(t *testing.T) {
	engine := &fakeEngine{}
	srv := NewServer(NewManager(engine), "")

	create := invokeDesktop(t, srv, desktoptypes.Request{Action: desktoptypes.ActionCreateSession})
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

	invoke := invokeDesktop(t, srv, desktoptypes.Request{
		Action:    desktoptypes.ActionClipboardRead,
		SessionID: sessionID,
	})
	if !invoke.OK {
		t.Fatalf("invoke ok = false: %+v", invoke)
	}

	closeResp := invokeDesktop(t, srv, desktoptypes.Request{
		Action:    desktoptypes.ActionCloseSession,
		SessionID: sessionID,
	})
	if !closeResp.OK {
		t.Fatalf("close ok = false: %+v", closeResp)
	}

	rec := httptest.NewRecorder()
	reqBody, _ := json.Marshal(desktoptypes.Request{
		Action:    desktoptypes.ActionClipboardRead,
		SessionID: sessionID,
	})
	req := httptest.NewRequest(http.MethodPost, "/desktop/v1", bytes.NewReader(reqBody))
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("post-close status = %d", rec.Code)
	}
}

func TestServerCreateSessionIncludesMetadata(t *testing.T) {
	engine := &fakeEngine{
		session: &fakeSession{
			id: "desktop-session-with-metadata",
			metadata: map[string]any{
				"workspace": "desktop-runtime",
				"host_profile": map[string]any{
					"os": "darwin",
				},
			},
		},
	}
	srv := NewServer(NewManager(engine), "")

	create := invokeDesktop(t, srv, desktoptypes.Request{Action: desktoptypes.ActionCreateSession})
	metadata, ok := create.Data["metadata"].(map[string]any)
	if !ok {
		t.Fatalf("metadata = %#v", create.Data["metadata"])
	}
	if metadata["workspace"] != "desktop-runtime" {
		t.Fatalf("workspace = %#v", metadata["workspace"])
	}
	hostProfile, ok := metadata["host_profile"].(map[string]any)
	if !ok || hostProfile["os"] != "darwin" {
		t.Fatalf("host_profile = %#v", metadata["host_profile"])
	}
}

func TestServerMapsNotImplementedTo501(t *testing.T) {
	session := &fakeSession{handleErr: errors.New("hotkey not implemented")}
	srv := NewServer(NewManager(&fakeEngine{session: session}), "")
	create := invokeDesktop(t, srv, desktoptypes.Request{Action: desktoptypes.ActionCreateSession})

	rec := httptest.NewRecorder()
	body, _ := json.Marshal(desktoptypes.Request{
		Action:    desktoptypes.ActionHotkey,
		SessionID: create.SessionID,
	})
	req := httptest.NewRequest(http.MethodPost, "/desktop/v1", bytes.NewReader(body))
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusNotImplemented {
		t.Fatalf("status = %d", rec.Code)
	}
}

func TestServerSupportsSessionlessEngineActions(t *testing.T) {
	engine := &fakeEngine{
		invoke: func(_ context.Context, req desktoptypes.Request) (*desktoptypes.Response, error) {
			if req.Action != desktoptypes.ActionListApps {
				t.Fatalf("unexpected action %q", req.Action)
			}
			return &desktoptypes.Response{
				OK: true,
				Data: map[string]any{
					"apps": []any{map[string]any{"name": "Finder"}},
				},
			}, nil
		},
	}
	srv := NewServer(NewManager(engine), "")

	resp := invokeDesktop(t, srv, desktoptypes.Request{Action: desktoptypes.ActionListApps})
	if !resp.OK {
		t.Fatalf("resp.OK = false: %+v", resp)
	}
}

func TestManagerCloseAll(t *testing.T) {
	first := &fakeSession{id: "one"}
	second := &fakeSession{id: "two"}
	manager := &Manager{
		sessions: map[string]Session{
			first.id:  first,
			second.id: second,
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

func invokeDesktop(t *testing.T, srv *Server, reqBody desktoptypes.Request) desktoptypes.Response {
	t.Helper()
	body, err := json.Marshal(reqBody)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/desktop/v1", bytes.NewReader(body))
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var resp desktoptypes.Response
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	return resp
}

package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/fulcrus/hopclaw/agent"
	"github.com/fulcrus/hopclaw/approval"
	"github.com/fulcrus/hopclaw/contextengine"
	"github.com/fulcrus/hopclaw/eventbus"
	rt "github.com/fulcrus/hopclaw/runtime"

	"github.com/gorilla/websocket"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

const wsTestDialTimeout = 2 * time.Second

// wsTestServer creates an httptest.Server with WebSocket support enabled.
func wsTestServer(t *testing.T, fixture runtimeFixture, cfg Config) (*httptest.Server, *WSHub) {
	t.Helper()

	if fixture.bus == nil {
		fixture.bus = eventbus.NewInMemoryBus()
	}
	hub := NewWSHub(fixture.bus)
	hub.Start()
	t.Cleanup(func() { hub.Stop() })

	cfg.WSHub = hub
	svc := newRuntimeService(t, fixture)
	srv := New(svc, cfg)
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(func() { ts.Close() })
	return ts, hub
}

// wsURL converts an http URL to a ws URL and appends the canonical runtime websocket path.
func wsURL(ts *httptest.Server) string {
	return wsURLForPath(ts, RuntimeWebSocketPath)
}

func wsURLForPath(ts *httptest.Server, path string) string {
	return "ws" + strings.TrimPrefix(ts.URL, "http") + path
}

// wsConnect performs the full handshake and returns an authenticated connection.
func wsConnect(t *testing.T, ts *httptest.Server, auth *ConnectAuth) *websocket.Conn {
	t.Helper()

	dialer := websocket.Dialer{HandshakeTimeout: wsTestDialTimeout}
	conn, _, err := dialer.Dial(wsURL(ts), nil)
	if err != nil {
		t.Fatalf("ws dial failed: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	// Read the challenge event.
	var challenge EventFrame
	if err := conn.ReadJSON(&challenge); err != nil {
		t.Fatalf("failed to read challenge: %v", err)
	}
	if challenge.Event != "challenge" {
		t.Fatalf("expected challenge event, got %q", challenge.Event)
	}

	// Send connect request.
	connectParams := ConnectParams{
		MinProtocol: 1,
		MaxProtocol: 1,
		Client: ConnectClientInfo{
			ID:       "test-client",
			Version:  "1.0.0",
			Platform: "test",
			Mode:     WSClientModeBackend,
		},
		Auth: auth,
	}
	paramsData, err := json.Marshal(connectParams)
	if err != nil {
		t.Fatalf("failed to marshal connect params: %v", err)
	}
	connectReq := RequestFrame{
		Type:   frameTypeRequest,
		ID:     "handshake-1",
		Method: WSMethodConnect,
		Params: paramsData,
	}
	if err := conn.WriteJSON(connectReq); err != nil {
		t.Fatalf("failed to send connect request: %v", err)
	}

	// Read hello-ok response.
	var resp ResponseFrame
	if err := conn.ReadJSON(&resp); err != nil {
		t.Fatalf("failed to read hello-ok: %v", err)
	}
	if !resp.OK {
		t.Fatalf("handshake failed: %+v", resp.Error)
	}

	return conn
}

func waitForWSHubConnectionCount(t *testing.T, hub *WSHub, want int) {
	t.Helper()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if hub.ConnectionCount() == want {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("expected %d connections, got %d", want, hub.ConnectionCount())
}

// wsSendRequest sends an RPC request and reads the response.
func wsSendRequest(t *testing.T, conn *websocket.Conn, id string, method WSMethod, params any) ResponseFrame {
	t.Helper()

	var paramsData json.RawMessage
	if params != nil {
		data, err := json.Marshal(params)
		if err != nil {
			t.Fatalf("failed to marshal params: %v", err)
		}
		paramsData = data
	}

	req := RequestFrame{
		Type:   frameTypeRequest,
		ID:     id,
		Method: method,
		Params: paramsData,
	}
	if err := conn.WriteJSON(req); err != nil {
		t.Fatalf("failed to send request: %v", err)
	}

	// Read responses, skipping event frames until we get the response.
	for {
		var raw json.RawMessage
		if err := conn.ReadJSON(&raw); err != nil {
			t.Fatalf("failed to read response: %v", err)
		}
		var peek struct {
			Type string `json:"type"`
			ID   string `json:"id"`
		}
		if err := json.Unmarshal(raw, &peek); err != nil {
			t.Fatalf("failed to peek at response: %v", err)
		}
		if peek.Type == frameTypeResponse && peek.ID == id {
			var resp ResponseFrame
			if err := json.Unmarshal(raw, &resp); err != nil {
				t.Fatalf("failed to decode response: %v", err)
			}
			return resp
		}
		// Skip event frames.
	}
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

func TestWSHandshake(t *testing.T) {
	t.Parallel()

	ts, hub := wsTestServer(t, runtimeFixture{
		model: &serverModelClient{},
	}, Config{})

	conn := wsConnect(t, ts, nil)
	_ = conn

	waitForWSHubConnectionCount(t, hub, 1)
}

func TestWSDoesNotExposeLegacyAliasRoute(t *testing.T) {
	t.Parallel()

	svc := newRuntimeService(t, runtimeFixture{
		model: &serverModelClient{},
	})
	hub := NewWSHub(eventbus.NewInMemoryBus())
	hub.Start()
	t.Cleanup(func() { hub.Stop() })
	handler := New(svc, Config{WSHub: hub}).Handler()

	req := httptest.NewRequest(http.MethodGet, "/ws", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("GET /ws status = %d, want 404", rec.Code)
	}
}

func TestWSHandshakeAuthRequired(t *testing.T) {
	t.Parallel()

	ts, _ := wsTestServer(t, runtimeFixture{
		model: &serverModelClient{},
	}, Config{AuthToken: "secret-token"})

	// Try connecting without auth -- should fail.
	dialer := websocket.Dialer{HandshakeTimeout: wsTestDialTimeout}
	conn, _, err := dialer.Dial(wsURL(ts), nil)
	if err != nil {
		t.Fatalf("ws dial failed: %v", err)
	}
	defer func() { _ = conn.Close() }()

	// Read challenge.
	var challenge EventFrame
	if err := conn.ReadJSON(&challenge); err != nil {
		t.Fatalf("failed to read challenge: %v", err)
	}

	// Send connect without auth.
	connectParams := ConnectParams{
		MinProtocol: 1,
		MaxProtocol: 1,
		Client:      ConnectClientInfo{ID: "test-no-auth"},
	}
	paramsData, _ := json.Marshal(connectParams)
	req := RequestFrame{
		Type:   frameTypeRequest,
		ID:     "auth-test-1",
		Method: WSMethodConnect,
		Params: paramsData,
	}
	if err := conn.WriteJSON(req); err != nil {
		t.Fatalf("failed to send connect: %v", err)
	}

	var resp ResponseFrame
	if err := conn.ReadJSON(&resp); err != nil {
		t.Fatalf("failed to read response: %v", err)
	}
	if resp.OK {
		t.Fatal("expected handshake to fail without auth")
	}
	if resp.Error == nil || resp.Error.Code != WSErrUnauthorized {
		t.Fatalf("expected unauthorized error, got %+v", resp.Error)
	}
}

func TestWSHandshakeAuthSuccess(t *testing.T) {
	t.Parallel()

	ts, hub := wsTestServer(t, runtimeFixture{
		model: &serverModelClient{},
	}, Config{AuthToken: "secret-token"})

	conn := wsConnect(t, ts, &ConnectAuth{Token: "secret-token"})
	_ = conn

	waitForWSHubConnectionCount(t, hub, 1)
}

func TestWSHandshakeProtocolMismatch(t *testing.T) {
	t.Parallel()

	ts, _ := wsTestServer(t, runtimeFixture{
		model: &serverModelClient{},
	}, Config{})

	dialer := websocket.Dialer{HandshakeTimeout: wsTestDialTimeout}
	conn, _, err := dialer.Dial(wsURL(ts), nil)
	if err != nil {
		t.Fatalf("ws dial failed: %v", err)
	}
	defer func() { _ = conn.Close() }()

	// Read challenge.
	var challenge EventFrame
	if err := conn.ReadJSON(&challenge); err != nil {
		t.Fatalf("failed to read challenge: %v", err)
	}

	// Send connect with incompatible protocol version.
	connectParams := ConnectParams{
		MinProtocol: 99,
		MaxProtocol: 100,
		Client:      ConnectClientInfo{ID: "test-bad-proto"},
	}
	paramsData, _ := json.Marshal(connectParams)
	req := RequestFrame{
		Type:   frameTypeRequest,
		ID:     "proto-test-1",
		Method: WSMethodConnect,
		Params: paramsData,
	}
	if err := conn.WriteJSON(req); err != nil {
		t.Fatalf("failed to send connect: %v", err)
	}

	var resp ResponseFrame
	if err := conn.ReadJSON(&resp); err != nil {
		t.Fatalf("failed to read response: %v", err)
	}
	if resp.OK {
		t.Fatal("expected handshake to fail with bad protocol")
	}
	if resp.Error == nil || resp.Error.Code != WSErrInvalidRequest {
		t.Fatalf("expected invalid_request error, got %+v", resp.Error)
	}
}

func TestWSPingPong(t *testing.T) {
	t.Parallel()

	ts, _ := wsTestServer(t, runtimeFixture{
		model: &serverModelClient{},
	}, Config{})

	conn := wsConnect(t, ts, nil)

	resp := wsSendRequest(t, conn, "ping-1", WSMethodPing, nil)
	if !resp.OK {
		t.Fatalf("ping failed: %+v", resp.Error)
	}

	var payload map[string]string
	if err := json.Unmarshal(resp.Payload, &payload); err != nil {
		t.Fatalf("failed to unmarshal pong: %v", err)
	}
	if payload["pong"] != "ok" {
		t.Fatalf("expected pong=ok, got %v", payload)
	}
}

func TestWSRunsSubmit(t *testing.T) {
	t.Parallel()

	ts, _ := wsTestServer(t, runtimeFixture{
		model: &serverModelClient{
			responses: []*agent.ModelResponse{{
				Message: contextengine.Message{
					Role:    contextengine.RoleAssistant,
					Content: "done",
				},
			}},
		},
	}, Config{})

	conn := wsConnect(t, ts, nil)

	resp := wsSendRequest(t, conn, "submit-1", WSMethodRunsSubmit, map[string]any{
		"session_key":   "ws-test",
		"content":       "hello from ws",
		"automation_id": "auto-ws",
	})
	if !resp.OK {
		t.Fatalf("runs.submit failed: %+v", resp.Error)
	}

	var run agent.Run
	if err := json.Unmarshal(resp.Payload, &run); err != nil {
		t.Fatalf("failed to unmarshal run: %v", err)
	}
	if run.ID == "" {
		t.Fatal("expected non-empty run ID")
	}
	if run.Scope.AutomationID != "auto-ws" {
		t.Fatalf("run.Scope = %#v", run.Scope)
	}
}

func TestWSRunsSubmitAutoFillsSingleConnectionAutomationScope(t *testing.T) {
	t.Parallel()

	svc := newRuntimeService(t, runtimeFixture{
		model: &serverModelClient{},
	})
	srv := New(svc, Config{})

	result, wsErr := srv.wsRunsSubmit(context.Background(), requestAuthScope{
		AutomationIDs: []string{"auto-connection"},
		Scoped:        true,
	}, json.RawMessage(`{"session_key":"ws-scope","content":"hello from scoped ws"}`))
	if wsErr != nil {
		t.Fatalf("wsRunsSubmit() error = %+v", wsErr)
	}

	run, ok := result.(*agent.Run)
	if !ok {
		t.Fatalf("result type = %T", result)
	}
	if run.Scope.AutomationID != "auto-connection" {
		t.Fatalf("run.Scope = %#v", run.Scope)
	}
}

func TestWSRunsSubmitRejectsAmbiguousConnectionAutomationScope(t *testing.T) {
	t.Parallel()

	svc := newRuntimeService(t, runtimeFixture{
		model: &serverModelClient{},
	})
	srv := New(svc, Config{})

	_, wsErr := srv.wsRunsSubmit(context.Background(), requestAuthScope{
		AutomationIDs: []string{"auto-a", "auto-b"},
		Scoped:        true,
	}, json.RawMessage(`{"session_key":"ws-scope","content":"hello"}`))
	if wsErr == nil {
		t.Fatal("expected ambiguous automation scope error")
	}
	if wsErr.Code != WSErrUnauthorized {
		t.Fatalf("wsErr = %+v", wsErr)
	}
}

func TestWSRunsSubmitRejectsPayloadScopeOutsideConnectionScope(t *testing.T) {
	t.Parallel()

	svc := newRuntimeService(t, runtimeFixture{
		model: &serverModelClient{},
	})
	srv := New(svc, Config{})

	_, wsErr := srv.wsRunsSubmit(context.Background(), requestAuthScope{
		AutomationIDs: []string{"auto-a"},
		Scoped:        true,
	}, json.RawMessage(`{"session_key":"ws-scope","content":"hello","automation_id":"auto-b"}`))
	if wsErr == nil {
		t.Fatal("expected out-of-scope automation error")
	}
	if wsErr.Code != WSErrUnauthorized {
		t.Fatalf("wsErr = %+v", wsErr)
	}
}

func TestWSRunsListFiltersByConnectionAutomationScope(t *testing.T) {
	t.Parallel()

	svc := newRuntimeService(t, runtimeFixture{
		model: &serverModelClient{},
	})
	if _, err := svc.Submit(context.Background(), rt.SubmitRequest{
		SessionKey:   "ws-list-a",
		Content:      "hello a",
		AutomationID: "auto-a",
	}); err != nil {
		t.Fatalf("Submit(auto-a) error = %v", err)
	}
	if _, err := svc.Submit(context.Background(), rt.SubmitRequest{
		SessionKey:   "ws-list-b",
		Content:      "hello b",
		AutomationID: "auto-b",
	}); err != nil {
		t.Fatalf("Submit(auto-b) error = %v", err)
	}
	srv := New(svc, Config{})

	result, wsErr := srv.wsRunsList(context.Background(), requestAuthScope{
		AutomationIDs: []string{"auto-a"},
		Scoped:        true,
	}, nil)
	if wsErr != nil {
		t.Fatalf("wsRunsList() error = %+v", wsErr)
	}

	payload, ok := result.(countedListResponse)
	if !ok {
		t.Fatalf("result type = %T", result)
	}
	items, ok := payload.Items.([]*rt.RunListView)
	if !ok {
		t.Fatalf("items type = %T", payload.Items)
	}
	if len(items) != 1 {
		t.Fatalf("len(items) = %d, want 1", len(items))
	}
	if items[0] == nil || items[0].Governance == nil || items[0].Governance.Scope.AutomationID != "auto-a" {
		t.Fatalf("items[0] = %#v", items[0])
	}
}

func TestWSRunsSubmitAcceptsImages(t *testing.T) {
	t.Parallel()

	svc := newRuntimeService(t, runtimeFixture{
		model: &serverModelClient{},
	})
	srv := New(svc, Config{})

	result, wsErr := srv.wsRunsSubmit(context.Background(), requestAuthScope{}, json.RawMessage(`{"session_key":"ws-images","content":"describe","images":["data:image/png;base64,ZmFrZS1wbmc="]}`))
	if wsErr != nil {
		t.Fatalf("wsRunsSubmit() error = %+v", wsErr)
	}

	run, ok := result.(*agent.Run)
	if !ok {
		t.Fatalf("result type = %T", result)
	}
	session, err := svc.GetSession(context.Background(), run.SessionID)
	if err != nil {
		t.Fatalf("GetSession() error = %v", err)
	}
	if len(session.Messages) == 0 || len(session.Messages[0].ContentBlocks) != 2 {
		t.Fatalf("session.Messages = %#v", session.Messages)
	}
}

func TestWSRunsSubmitAcceptsContentBlocksWithoutText(t *testing.T) {
	t.Parallel()

	svc := newRuntimeService(t, runtimeFixture{
		model: &serverModelClient{
			responses: []*agent.ModelResponse{{
				Message: contextengine.Message{
					Role:    contextengine.RoleAssistant,
					Content: "done",
				},
			}},
		},
	})
	srv := New(svc, Config{})

	result, wsErr := srv.wsRunsSubmit(context.Background(), requestAuthScope{}, json.RawMessage(`{"session_key":"ws-content-blocks","content_blocks":[{"type":"file","label":"brief.md","media_ref":"upload://brief-1"}]}`))
	if wsErr != nil {
		t.Fatalf("wsRunsSubmit() error = %+v", wsErr)
	}

	run, ok := result.(*agent.Run)
	if !ok {
		t.Fatalf("result type = %T", result)
	}
	session, err := svc.GetSession(context.Background(), run.SessionID)
	if err != nil {
		t.Fatalf("GetSession() error = %v", err)
	}
	if len(session.Messages) == 0 || len(session.Messages[0].ContentBlocks) != 1 {
		t.Fatalf("session.Messages = %#v", session.Messages)
	}
	if session.Messages[0].ContentBlocks[0].Type != contextengine.ContentBlockFile {
		t.Fatalf("file block = %#v", session.Messages[0].ContentBlocks[0])
	}
}

func TestWSEventsListSupportsFiltersAndCursor(t *testing.T) {
	t.Parallel()

	bus := eventbus.NewInMemoryBus()
	for _, event := range []eventbus.Event{
		{Type: eventbus.EventRunStarted, RunID: "run-ws-1", SessionID: "session-ws-1"},
		{Type: eventbus.EventRunCompleted, RunID: "run-ws-1", SessionID: "session-ws-1"},
		{Type: eventbus.EventRunCompleted, RunID: "run-ws-2", SessionID: "session-ws-2"},
	} {
		if err := bus.Publish(context.Background(), event); err != nil {
			t.Fatalf("publish event: %v", err)
		}
	}

	ts, _ := wsTestServer(t, runtimeFixture{
		model: &serverModelClient{},
		bus:   bus,
	}, Config{})

	conn := wsConnect(t, ts, nil)

	resp := wsSendRequest(t, conn, "events-filter-1", WSMethodEventsList, map[string]any{
		"since":      "evt-000001",
		"type":       "run.completed",
		"run_id":     "run-ws-1",
		"session_id": "session-ws-1",
	})
	if !resp.OK {
		t.Fatalf("events.list failed: %+v", resp.Error)
	}

	var payload wsEventCursorResponse
	if err := json.Unmarshal(resp.Payload, &payload); err != nil {
		t.Fatalf("failed to unmarshal events.list payload: %v", err)
	}
	if payload.CursorStatus != eventbus.CursorOK {
		t.Fatalf("CursorStatus = %q, want %q", payload.CursorStatus, eventbus.CursorOK)
	}
	if len(payload.Events) != 1 {
		t.Fatalf("len(events) = %d, want 1", len(payload.Events))
	}
	if payload.Events[0].Type != eventbus.EventRunCompleted || payload.Events[0].RunID != "run-ws-1" || payload.Events[0].SessionID != "session-ws-1" {
		t.Fatalf("event = %#v", payload.Events[0])
	}
	if payload.NextCursor != payload.Events[0].ID {
		t.Fatalf("NextCursor = %q, want %q", payload.NextCursor, payload.Events[0].ID)
	}
}

func TestWSApprovalsListSupportsPagination(t *testing.T) {
	t.Parallel()

	approvalStore := approval.NewInMemoryStore()
	for _, runID := range []string{"run-ws-1", "run-ws-2", "run-ws-3"} {
		if _, err := approvalStore.Create(context.Background(), approval.Ticket{
			RunID:     runID,
			SessionID: "sess-" + runID,
		}); err != nil {
			t.Fatalf("Create(%s) error = %v", runID, err)
		}
	}

	ts, _ := wsTestServer(t, runtimeFixture{
		model:     &serverModelClient{},
		approvals: approvalStore,
	}, Config{})

	conn := wsConnect(t, ts, nil)

	resp := wsSendRequest(t, conn, "approvals-list-1", WSMethodApprovalsList, map[string]any{
		"status": "pending",
		"limit":  1,
		"offset": 1,
	})
	if !resp.OK {
		t.Fatalf("approvals.list failed: %+v", resp.Error)
	}

	var payload struct {
		Items []*rt.ApprovalView `json:"items"`
		Count int                `json:"count"`
	}
	if err := json.Unmarshal(resp.Payload, &payload); err != nil {
		t.Fatalf("failed to unmarshal approvals.list payload: %v", err)
	}
	if payload.Count != 1 || len(payload.Items) != 1 {
		t.Fatalf("payload = %+v", payload)
	}
	if payload.Items[0].RunID != "run-ws-2" {
		t.Fatalf("payload.Items[0].RunID = %q, want run-ws-2", payload.Items[0].RunID)
	}
}

func TestWSInteractionsSubmit(t *testing.T) {
	t.Parallel()

	ts, _ := wsTestServer(t, runtimeFixture{
		model: &serverModelClient{
			responses: []*agent.ModelResponse{{
				Message: contextengine.Message{
					Role:    contextengine.RoleAssistant,
					Content: "You are welcome.",
				},
			}},
		},
		classifier: serverStubInteractionClassifier{
			decision: rt.InteractionDecision{
				SpeechAct:   rt.SpeechActCasualChat,
				TargetScope: rt.TargetScopeNone,
				ReplyAct:    rt.ReplyActChatReply,
				Reason:      "chit_chat",
				Confidence:  0.95,
			},
		},
	}, Config{})

	conn := wsConnect(t, ts, nil)

	resp := wsSendRequest(t, conn, "interact-1", WSMethodInteractionsSubmit, map[string]any{
		"session_key": "ws-interact",
		"content":     "thanks",
	})
	if !resp.OK {
		t.Fatalf("interactions.submit failed: %+v", resp.Error)
	}

	var payload interactResponse
	if err := json.Unmarshal(resp.Payload, &payload); err != nil {
		t.Fatalf("failed to unmarshal interaction response: %v", err)
	}
	if payload.Decision.ReplyAct != rt.ReplyActChatReply {
		t.Fatalf("ReplyAct = %q, want %q (error=%q message=%q)", payload.Decision.ReplyAct, rt.ReplyActChatReply, payload.Error, payload.Message)
	}
	if strings.TrimSpace(payload.Message) == "" {
		t.Fatal("expected non-empty message")
	}
}

func TestWSInteractionsSubmitAcceptsImages(t *testing.T) {
	t.Parallel()

	svc := newRuntimeService(t, runtimeFixture{
		model: &serverModelClient{
			responses: []*agent.ModelResponse{{
				Message: contextengine.Message{
					Role:    contextengine.RoleAssistant,
					Content: "done",
				},
			}},
		},
		classifier: serverStubInteractionClassifier{
			decision: rt.InteractionDecision{
				SpeechAct:   rt.SpeechActNewTask,
				TargetScope: rt.TargetScopeNewRun,
				ReplyAct:    rt.ReplyActTaskAccept,
				Confidence:  0.95,
			},
		},
	})
	srv := New(svc, Config{})

	result, wsErr := srv.wsInteractionsSubmit(context.Background(), requestAuthScope{}, json.RawMessage(`{"session_key":"ws-interact-images","content":"inspect","images":["data:image/png;base64,ZmFrZS1wbmc="]}`))
	if wsErr != nil {
		t.Fatalf("wsInteractionsSubmit() error = %+v", wsErr)
	}

	resp, ok := result.(interactResponse)
	if !ok {
		t.Fatalf("result type = %T", result)
	}
	if resp.Run == nil {
		t.Fatal("expected run to be created")
	}
	session, err := svc.GetSession(context.Background(), resp.Run.SessionID)
	if err != nil {
		t.Fatalf("GetSession() error = %v", err)
	}
	if len(session.Messages) == 0 || len(session.Messages[0].ContentBlocks) != 2 {
		t.Fatalf("session.Messages = %#v", session.Messages)
	}
}

func TestWSInteractionsSubmitAcceptsContentBlocksWithoutText(t *testing.T) {
	t.Parallel()

	svc := newRuntimeService(t, runtimeFixture{
		model: &serverModelClient{
			responses: []*agent.ModelResponse{{
				Message: contextengine.Message{
					Role:    contextengine.RoleAssistant,
					Content: "done",
				},
			}},
		},
	})
	srv := New(svc, Config{})

	result, wsErr := srv.wsInteractionsSubmit(context.Background(), requestAuthScope{}, json.RawMessage(`{"session_key":"ws-interact-content-blocks","content_blocks":[{"type":"file","label":"brief.md","media_ref":"upload://brief-1"}]}`))
	if wsErr != nil {
		t.Fatalf("wsInteractionsSubmit() error = %+v", wsErr)
	}

	resp, ok := result.(interactResponse)
	if !ok {
		t.Fatalf("result type = %T", result)
	}
	if resp.Run == nil {
		t.Fatal("expected run to be created")
	}
	session, err := svc.GetSession(context.Background(), resp.Run.SessionID)
	if err != nil {
		t.Fatalf("GetSession() error = %v", err)
	}
	if len(session.Messages) == 0 || len(session.Messages[0].ContentBlocks) != 1 {
		t.Fatalf("session.Messages = %#v", session.Messages)
	}
	if session.Messages[0].ContentBlocks[0].Type != contextengine.ContentBlockFile {
		t.Fatalf("file block = %#v", session.Messages[0].ContentBlocks[0])
	}
}

func TestWSInteractionsSubmitStructuredRetryReusesOriginalInput(t *testing.T) {
	t.Parallel()

	svc := newRuntimeService(t, runtimeFixture{
		model: &serverModelClient{
			responses: []*agent.ModelResponse{
				{Message: contextengine.Message{Role: contextengine.RoleAssistant, Content: "done"}},
				{Message: contextengine.Message{Role: contextengine.RoleAssistant, Content: "done again"}},
			},
		},
	})
	srv := New(svc, Config{})

	original, err := svc.Submit(context.Background(), rt.SubmitRequest{
		SessionKey: "ws-structured-retry",
		Content:    "inspect this",
		ContentBlocks: []contextengine.ContentBlock{
			{Type: contextengine.ContentBlockText, Text: "inspect this"},
			{Type: contextengine.ContentBlockFile, Label: "spec.md", Path: "/tmp/spec.md"},
		},
	})
	if err != nil {
		t.Fatalf("Submit() error = %v", err)
	}
	for {
		original, err = svc.GetRun(context.Background(), original.ID)
		if err != nil {
			t.Fatalf("GetRun() error = %v", err)
		}
		if original.Status == agent.RunCompleted {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	result, wsErr := srv.wsInteractionsSubmit(context.Background(), requestAuthScope{}, json.RawMessage(`{
		"session_key":"ws-structured-retry",
		"content":"",
		"structured_command":{"kind":"retry","run_id":"`+original.ID+`"}
	}`))
	if wsErr != nil {
		t.Fatalf("wsInteractionsSubmit() error = %+v", wsErr)
	}

	resp, ok := result.(interactResponse)
	if !ok {
		t.Fatalf("result type = %T", result)
	}
	if resp.Run == nil || resp.Run.ParentRunID != original.ID {
		t.Fatalf("Run = %#v, want retry run parented to %q", resp.Run, original.ID)
	}
	if resp.SubmitRequest == nil || len(resp.SubmitRequest.ContentBlocks) != 2 {
		t.Fatalf("SubmitRequest = %#v", resp.SubmitRequest)
	}
	if resp.SubmitRequest.ContentBlocks[1].Type != contextengine.ContentBlockFile {
		t.Fatalf("SubmitRequest.ContentBlocks = %#v", resp.SubmitRequest.ContentBlocks)
	}
}

func TestWSBroadcast(t *testing.T) {
	t.Parallel()

	bus := eventbus.NewInMemoryBus()
	ts, hub := wsTestServer(t, runtimeFixture{
		model: &serverModelClient{},
		bus:   bus,
	}, Config{})

	// Connect two clients.
	conn1 := wsConnect(t, ts, nil)
	conn2 := wsConnect(t, ts, nil)

	waitForWSHubConnectionCount(t, hub, 2)

	// Publish an event to the bus.
	if err := bus.Publish(context.Background(), eventbus.Event{
		Type:  eventbus.EventRunCompleted,
		RunID: "broadcast-run",
	}); err != nil {
		t.Fatalf("publish failed: %v", err)
	}

	// Both clients should receive the event.
	readEvent := func(conn *websocket.Conn) EventFrame {
		t.Helper()
		deadline := time.Now().Add(2 * time.Second)
		for time.Now().Before(deadline) {
			if err := conn.SetReadDeadline(time.Now().Add(500 * time.Millisecond)); err != nil {
				t.Fatalf("failed to set read deadline: %v", err)
			}
			var raw json.RawMessage
			if err := conn.ReadJSON(&raw); err != nil {
				continue
			}
			var peek struct {
				Type  string `json:"type"`
				Event string `json:"event"`
			}
			if err := json.Unmarshal(raw, &peek); err != nil {
				continue
			}
			if peek.Type == frameTypeEvent && peek.Event == string(eventbus.EventRunCompleted) {
				var evt EventFrame
				_ = json.Unmarshal(raw, &evt)
				return evt
			}
		}
		t.Fatal("timed out waiting for broadcast event")
		return EventFrame{}
	}

	evt1 := readEvent(conn1)
	evt2 := readEvent(conn2)

	if evt1.Event != string(eventbus.EventRunCompleted) {
		t.Fatalf("conn1 got event %q", evt1.Event)
	}
	if evt2.Event != string(eventbus.EventRunCompleted) {
		t.Fatalf("conn2 got event %q", evt2.Event)
	}
}

func TestWSSlowConsumerDisconnect(t *testing.T) {
	t.Parallel()

	ts, hub := wsTestServer(t, runtimeFixture{
		model: &serverModelClient{},
	}, Config{})

	conn := wsConnect(t, ts, nil)

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && hub.ConnectionCount() != 1 {
		time.Sleep(10 * time.Millisecond)
	}
	if hub.ConnectionCount() != 1 {
		t.Fatalf("expected 1 connection, got %d", hub.ConnectionCount())
	}

	// Get the WSConn from the hub to fill its buffer.
	hub.mu.RLock()
	var wsc *WSConn
	for _, c := range hub.connections {
		wsc = c
		break
	}
	hub.mu.RUnlock()

	if wsc == nil {
		t.Fatal("expected to find a connection in hub")
	}

	// Artificially set buffered bytes near the limit.
	wsc.mu.Lock()
	wsc.bufferedBytes = int64(wsMaxBufferedBytes) - 1
	wsc.mu.Unlock()

	// Now sending a large message should trigger the slow consumer disconnect.
	largePayload := make([]byte, 10)
	err := wsc.send(largePayload)
	if err == nil {
		t.Fatal("expected send to fail for slow consumer")
	}
	if !strings.Contains(err.Error(), "send buffer overflow") {
		t.Fatalf("unexpected error: %v", err)
	}

	// Give the close a moment to propagate.
	time.Sleep(50 * time.Millisecond)

	// Verify the connection is closed by trying to read.
	if err := conn.SetReadDeadline(time.Now().Add(100 * time.Millisecond)); err != nil {
		t.Fatalf("failed to set read deadline: %v", err)
	}
	_, _, readErr := conn.ReadMessage()
	if readErr == nil {
		t.Fatal("expected read to fail on closed connection")
	}
}

func TestWSGracefulClose(t *testing.T) {
	t.Parallel()

	bus := eventbus.NewInMemoryBus()
	hub := NewWSHub(bus)
	hub.Start()

	svc := newRuntimeService(t, runtimeFixture{
		model: &serverModelClient{},
		bus:   bus,
	})
	srv := New(svc, Config{WSHub: hub})
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	conn := wsConnect(t, ts, nil)

	waitForWSHubConnectionCount(t, hub, 1)

	// Stop the hub -- should close all connections.
	hub.Stop()

	// Give the close a moment to propagate.
	time.Sleep(50 * time.Millisecond)

	// Verify the connection is closed.
	if err := conn.SetReadDeadline(time.Now().Add(100 * time.Millisecond)); err != nil {
		t.Fatalf("failed to set read deadline: %v", err)
	}
	_, _, readErr := conn.ReadMessage()
	if readErr == nil {
		t.Fatal("expected read to fail after hub stop")
	}

	if hub.ConnectionCount() != 0 {
		t.Fatalf("expected 0 connections after stop, got %d", hub.ConnectionCount())
	}
}

func TestWSUnknownMethod(t *testing.T) {
	t.Parallel()

	ts, _ := wsTestServer(t, runtimeFixture{
		model: &serverModelClient{},
	}, Config{})

	conn := wsConnect(t, ts, nil)

	resp := wsSendRequest(t, conn, "unknown-1", WSMethod("does.not.exist"), nil)
	if resp.OK {
		t.Fatal("expected unknown method to fail")
	}
	if resp.Error == nil || resp.Error.Code != WSErrNotFound {
		t.Fatalf("expected not_found error, got %+v", resp.Error)
	}
}

func TestWSStatus(t *testing.T) {
	t.Parallel()

	ts, _ := wsTestServer(t, runtimeFixture{
		model: &serverModelClient{},
	}, Config{})

	conn := wsConnect(t, ts, nil)

	resp := wsSendRequest(t, conn, "status-1", WSMethodStatus, nil)
	if !resp.OK {
		t.Fatalf("status failed: %+v", resp.Error)
	}

	var status wsStatusResponse
	if err := json.Unmarshal(resp.Payload, &status); err != nil {
		t.Fatalf("failed to unmarshal status: %v", err)
	}
	if !status.OK {
		t.Fatal("expected status.OK=true")
	}
	if status.Connections < 1 {
		t.Fatalf("expected at least 1 connection, got %d", status.Connections)
	}
}

func TestWSConcurrentConnections(t *testing.T) {
	t.Parallel()

	ts, hub := wsTestServer(t, runtimeFixture{
		model: &serverModelClient{},
	}, Config{})

	const numConns = 5
	var wg sync.WaitGroup
	conns := make([]*websocket.Conn, numConns)

	for i := 0; i < numConns; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			conns[idx] = wsConnect(t, ts, nil)
		}(i)
	}
	wg.Wait()

	deadline := time.Now().Add(2 * time.Second)
	count := hub.ConnectionCount()
	for time.Now().Before(deadline) {
		if count == numConns {
			break
		}
		time.Sleep(10 * time.Millisecond)
		count = hub.ConnectionCount()
	}
	if count != numConns {
		t.Fatalf("expected %d connections, got %d", numConns, count)
	}

	// All connections should be able to ping.
	for i, conn := range conns {
		resp := wsSendRequest(t, conn, "ping-concurrent", WSMethodPing, nil)
		if !resp.OK {
			t.Fatalf("ping failed for conn %d: %+v", i, resp.Error)
		}
	}
}

func TestWSNoHubReturnsServiceUnavailable(t *testing.T) {
	t.Parallel()

	svc := newRuntimeService(t, runtimeFixture{
		model: &serverModelClient{},
	})
	// No WSHub configured.
	handler := New(svc, Config{}).Handler()

	req := httptest.NewRequest(http.MethodGet, RuntimeWebSocketPath, nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	// Without a hub, should get a 404 (route not registered) or similar.
	// The runtime websocket route is not registered when wsHub is nil.
	if rec.Code == http.StatusSwitchingProtocols {
		t.Fatal("expected non-upgrade response when hub is nil")
	}
}

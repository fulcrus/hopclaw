package gateway

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/fulcrus/hopclaw/agent"
	"github.com/fulcrus/hopclaw/eventbus"
	runtimepkg "github.com/fulcrus/hopclaw/runtime"
	serverpkg "github.com/fulcrus/hopclaw/server"
	"github.com/gorilla/websocket"
)

func TestGatewayDoesNotExposeLegacyOperatorWebSocketRoute(t *testing.T) {
	t.Parallel()

	gw := newTestGatewayFull(t)
	gw.SetWSHandler(NewWSHandler(gw, nil))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/ws", nil)
	req.Header.Set("Authorization", "Bearer test-token")
	gw.Handler().ServeHTTP(rec, req)
	if rec.Code != 404 {
		t.Fatalf("GET /ws status = %d, want 404", rec.Code)
	}
}

func TestGatewayCanonicalRuntimeWebSocketBypassesGatewayHTTPAuth(t *testing.T) {
	t.Parallel()

	runs := agent.NewInMemoryRunStore()
	sessions := agent.NewInMemorySessionStore()
	bus := eventbus.NewInMemoryBus()
	runtimeSvc := runtimepkg.NewService(nil, sessions, runs, nil, bus, nil)
	hub := serverpkg.NewWSHub(bus)
	hub.Start()
	defer hub.Stop()

	runtimeServer := serverpkg.New(runtimeSvc, serverpkg.Config{
		AuthToken: "runtime-secret",
		WSHub:     hub,
	})
	gw := gatewayFromServer(runtimeServer, Config{
		AuthToken: "gateway-secret",
		Runtime:   runtimeSvc,
	})

	ts := httptest.NewServer(gw.Handler())
	defer ts.Close()

	dialer := websocket.Dialer{}
	conn, resp, err := dialer.Dial("ws"+strings.TrimPrefix(ts.URL, "http")+serverpkg.RuntimeWebSocketPath, nil)
	if err != nil {
		t.Fatalf("dial canonical runtime websocket through gateway: %v", err)
	}
	defer func() { _ = conn.Close() }()
	_ = resp

	var challenge serverpkg.EventFrame
	if err := conn.ReadJSON(&challenge); err != nil {
		t.Fatalf("read runtime challenge: %v", err)
	}
	if challenge.Event != "challenge" {
		t.Fatalf("challenge.Event = %q, want challenge", challenge.Event)
	}

	params, err := json.Marshal(serverpkg.ConnectParams{
		MinProtocol: 1,
		MaxProtocol: 1,
		Client: serverpkg.ConnectClientInfo{
			ID:      "gateway-runtime-ws-test",
			Version: "1.0.0",
			Mode:    serverpkg.WSClientModeBackend,
		},
		Auth: &serverpkg.ConnectAuth{Token: "runtime-secret"},
	})
	if err != nil {
		t.Fatalf("marshal runtime connect params: %v", err)
	}
	if err := conn.WriteJSON(serverpkg.RequestFrame{
		Type:   "req",
		ID:     "connect-1",
		Method: serverpkg.WSMethodConnect,
		Params: params,
	}); err != nil {
		t.Fatalf("write runtime connect request: %v", err)
	}

	var response serverpkg.ResponseFrame
	if err := conn.ReadJSON(&response); err != nil {
		t.Fatalf("read runtime connect response: %v", err)
	}
	if !response.OK {
		t.Fatalf("runtime handshake failed: %+v", response.Error)
	}
}

func TestGatewayOperatorWebSocketRejectsOversizedFrames(t *testing.T) {
	t.Parallel()

	gw := newTestGatewayFull(t)
	gw.SetWSHandler(NewWSHandler(gw, nil))

	ts := httptest.NewServer(gw.Handler())
	defer ts.Close()

	headers := http.Header{
		"Authorization": []string{"Bearer test-token"},
	}
	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(ts.URL, "http")+operatorWebSocketPath, headers)
	if err != nil {
		t.Fatalf("dial operator websocket: %v", err)
	}
	defer func() { _ = conn.Close() }()

	if err := conn.WriteMessage(websocket.TextMessage, bytes.Repeat([]byte("a"), wsReadLimitBytes+1024)); err != nil {
		t.Fatalf("WriteMessage() error = %v", err)
	}
	if err := conn.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatalf("SetReadDeadline() error = %v", err)
	}

	if _, _, err := conn.ReadMessage(); err == nil {
		t.Fatal("expected oversized websocket frame to be rejected")
	} else if !websocket.IsCloseError(err, websocket.CloseMessageTooBig) && !strings.Contains(err.Error(), "1009") {
		t.Fatalf("ReadMessage() error = %v, want close 1009", err)
	}
}

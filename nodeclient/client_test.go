package nodeclient

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// New (client creation)
// ---------------------------------------------------------------------------

func TestNewDefaultConfig(t *testing.T) {
	t.Parallel()

	c := New(Config{DeviceID: "dev-1"})
	if c == nil {
		t.Fatal("expected non-nil client")
	}
	if c.cfg.Role != "node" {
		t.Fatalf("Role = %q, want %q", c.cfg.Role, "node")
	}
	if c.cfg.ClientID != "dev-1" {
		t.Fatalf("ClientID = %q, want %q", c.cfg.ClientID, "dev-1")
	}
	if c.cfg.ClientMode != "node" {
		t.Fatalf("ClientMode = %q, want %q", c.cfg.ClientMode, "node")
	}
	if c.cfg.HeartbeatInterval != 20*time.Second {
		t.Fatalf("HeartbeatInterval = %v, want 20s", c.cfg.HeartbeatInterval)
	}
	if c.cfg.ReconnectDelay != 3*time.Second {
		t.Fatalf("ReconnectDelay = %v, want 3s", c.cfg.ReconnectDelay)
	}
	if c.cfg.HTTPClient == nil {
		t.Fatal("expected non-nil HTTPClient")
	}
}

func TestNewCustomConfig(t *testing.T) {
	t.Parallel()

	c := New(Config{
		DeviceID:          "dev-2",
		ClientID:          "custom-id",
		ClientMode:        "controller",
		HeartbeatInterval: 10 * time.Second,
		ReconnectDelay:    5 * time.Second,
	})
	if c.cfg.ClientID != "custom-id" {
		t.Fatalf("ClientID = %q, want %q", c.cfg.ClientID, "custom-id")
	}
	if c.cfg.ClientMode != "controller" {
		t.Fatalf("ClientMode = %q, want %q", c.cfg.ClientMode, "controller")
	}
	if c.cfg.HeartbeatInterval != 10*time.Second {
		t.Fatalf("HeartbeatInterval = %v", c.cfg.HeartbeatInterval)
	}
	if c.cfg.ReconnectDelay != 5*time.Second {
		t.Fatalf("ReconnectDelay = %v", c.cfg.ReconnectDelay)
	}
}

// ---------------------------------------------------------------------------
// Register
// ---------------------------------------------------------------------------

func TestRegister(t *testing.T) {
	t.Parallel()

	c := New(Config{DeviceID: "dev-1"})
	c.Register("test.cmd", func(_ context.Context, _ map[string]any) (map[string]any, error) {
		return nil, nil
	})

	if _, ok := c.handlers["test.cmd"]; !ok {
		t.Fatal("handler not registered")
	}
}

func TestRegisterEmptyCommand(t *testing.T) {
	t.Parallel()

	c := New(Config{DeviceID: "dev-1"})
	c.Register("", func(_ context.Context, _ map[string]any) (map[string]any, error) {
		return nil, nil
	})

	if len(c.handlers) != 0 {
		t.Fatal("expected no handlers for empty command")
	}
}

func TestRegisterNilHandler(t *testing.T) {
	t.Parallel()

	c := New(Config{DeviceID: "dev-1"})
	c.Register("cmd", nil)

	if len(c.handlers) != 0 {
		t.Fatal("expected no handlers for nil handler")
	}
}

func TestRegisterNilClient(t *testing.T) {
	t.Parallel()

	var c *Client
	// Should not panic.
	c.Register("cmd", func(_ context.Context, _ map[string]any) (map[string]any, error) {
		return nil, nil
	})
}

// ---------------------------------------------------------------------------
// websocketURL
// ---------------------------------------------------------------------------

func TestWebsocketURL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		{"http to ws", "http://localhost:8080", "ws://localhost:8080/operator/ws", false},
		{"https to wss", "https://example.com", "wss://example.com/operator/ws", false},
		{"ws canonical passthrough", "ws://localhost/operator/ws", "ws://localhost/operator/ws", false},
		{"wss canonical passthrough", "wss://example.com/operator/ws", "wss://example.com/operator/ws", false},
		{"canonical operator path passthrough", "https://example.com/operator/ws", "wss://example.com/operator/ws", false},
		{"with base path", "http://localhost:8080/api", "ws://localhost:8080/api/operator/ws", false},
		{"root path", "http://localhost:8080/", "ws://localhost:8080/operator/ws", false},
		{"empty path", "http://localhost:8080", "ws://localhost:8080/operator/ws", false},
		{"legacy websocket path rejected", "ws://localhost/ws", "", true},
		{"unsupported scheme", "ftp://example.com", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := websocketURL(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("got %q, want %q", got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// httpBaseURL
// ---------------------------------------------------------------------------

func TestHttpBaseURL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		{"operator ws to http", "ws://localhost:8080/operator/ws", "http://localhost:8080", false},
		{"wss operator ws to https", "wss://example.com/operator/ws", "https://example.com", false},
		{"http passthrough", "http://localhost:8080", "http://localhost:8080", false},
		{"https passthrough", "https://example.com/api", "https://example.com/api", false},
		{"operator ws with base path stripping", "ws://localhost:8080/custom/operator/ws", "http://localhost:8080/custom", false},
		{"root path", "http://localhost/", "http://localhost", false},
		{"legacy websocket path rejected", "ws://localhost:8080/ws", "", true},
		{"unsupported scheme", "ftp://example.com", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := httpBaseURL(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("got %q, want %q", got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// randomHex
// ---------------------------------------------------------------------------

func TestRandomHex(t *testing.T) {
	t.Parallel()

	hex1 := randomHex(8)
	hex2 := randomHex(8)

	if len(hex1) != 16 {
		t.Fatalf("len = %d, want 16", len(hex1))
	}
	if hex1 == hex2 {
		t.Fatal("expected different hex values")
	}
}

// ---------------------------------------------------------------------------
// nextID
// ---------------------------------------------------------------------------

func TestNextID(t *testing.T) {
	t.Parallel()

	c := New(Config{DeviceID: "dev-1"})
	ids := make(map[string]struct{}, 128)
	for range 128 {
		id := c.nextID()
		if !strings.HasPrefix(id, "node-") {
			t.Fatalf("id = %q, expected node- prefix", id)
		}
		if _, exists := ids[id]; exists {
			t.Fatalf("duplicate id generated: %q", id)
		}
		ids[id] = struct{}{}
	}
}

// ---------------------------------------------------------------------------
// Run with cancelled context
// ---------------------------------------------------------------------------

func TestRunCancelledContext(t *testing.T) {
	t.Parallel()

	c := New(Config{
		GatewayURL:     "http://localhost:0",
		DeviceID:       "dev-1",
		ReconnectDelay: 10 * time.Millisecond,
	})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := c.Run(ctx)
	if err != nil {
		t.Fatalf("Run() should return nil on cancelled context, got: %v", err)
	}
}

func TestNewPreservesExplicitWebSocketURL(t *testing.T) {
	t.Parallel()

	c := New(Config{
		GatewayURL:   "https://gateway.example.com",
		WebSocketURL: "wss://gateway.example.com/operator/ws",
		DeviceID:     "dev-1",
	})

	if c.cfg.WebSocketURL != "wss://gateway.example.com/operator/ws" {
		t.Fatalf("WebSocketURL = %q, want explicit operator websocket URL", c.cfg.WebSocketURL)
	}
}

// ---------------------------------------------------------------------------
// PairClaimRequest / PairClaimResponse types
// ---------------------------------------------------------------------------

func TestPairClaimRequestFields(t *testing.T) {
	t.Parallel()

	req := PairClaimRequest{
		Code:         "ABC123",
		DeviceID:     "dev-1",
		Name:         "My Device",
		Platform:     "darwin",
		DeviceFamily: "macbook",
		Role:         "node",
		Scopes:       []string{"read", "write"},
	}
	if req.Code != "ABC123" {
		t.Fatalf("Code = %q", req.Code)
	}
	if len(req.Scopes) != 2 {
		t.Fatalf("Scopes len = %d", len(req.Scopes))
	}
}

func TestPairClaimResponseFields(t *testing.T) {
	t.Parallel()

	resp := PairClaimResponse{
		OK:       true,
		DeviceID: "dev-1",
		Token:    "tok-123",
		Error:    "",
	}
	if !resp.OK {
		t.Fatal("OK = false")
	}
	if resp.Token != "tok-123" {
		t.Fatalf("Token = %q", resp.Token)
	}
}

// ---------------------------------------------------------------------------
// handleInvoke logic (partial: handler dispatch)
// ---------------------------------------------------------------------------

func TestHandlerDispatchUnknownCommand(t *testing.T) {
	t.Parallel()

	c := New(Config{DeviceID: "dev-1"})

	// Verify that looking up a non-existent handler does not panic.
	_, ok := c.handlers["nonexistent"]
	if ok {
		t.Fatal("expected handler not found")
	}
}

func TestHandlerDispatchKnownCommand(t *testing.T) {
	t.Parallel()

	c := New(Config{DeviceID: "dev-1"})
	c.Register("ping", func(_ context.Context, _ map[string]any) (map[string]any, error) {
		return map[string]any{"pong": true}, nil
	})

	handler, ok := c.handlers["ping"]
	if !ok {
		t.Fatal("handler not found")
	}
	result, err := handler(context.Background(), nil)
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if result["pong"] != true {
		t.Fatalf("result = %v", result)
	}
}

func TestHandlerDispatchError(t *testing.T) {
	t.Parallel()

	c := New(Config{DeviceID: "dev-1"})
	c.Register("fail", func(_ context.Context, _ map[string]any) (map[string]any, error) {
		return nil, fmt.Errorf("intentional error")
	})

	handler := c.handlers["fail"]
	_, err := handler(context.Background(), nil)
	if err == nil {
		t.Fatal("expected error from handler")
	}
}

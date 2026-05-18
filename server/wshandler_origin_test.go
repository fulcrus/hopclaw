package server

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSameOriginWebSocketRequestAllowsSameHost(t *testing.T) {
	t.Parallel()

	req := httptest.NewRequest(http.MethodGet, "https://runtime.example/runtime/ws", nil)
	req.Host = "runtime.example"
	req.Header.Set("Origin", "https://runtime.example")

	if !sameOriginWebSocketRequest(req) {
		t.Fatal("same-origin websocket request should be allowed")
	}
}

func TestSameOriginWebSocketRequestRejectsCrossHost(t *testing.T) {
	t.Parallel()

	req := httptest.NewRequest(http.MethodGet, "https://runtime.example/runtime/ws", nil)
	req.Host = "runtime.example"
	req.Header.Set("Origin", "https://evil.example")

	if sameOriginWebSocketRequest(req) {
		t.Fatal("cross-origin websocket request should be rejected")
	}
}

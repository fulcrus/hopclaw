package gateway

import (
	"encoding/json"
	"net/http"
	"testing"
)

// ---------------------------------------------------------------------------
// handleSandboxExec
// ---------------------------------------------------------------------------

func TestSandboxExecNilRunner(t *testing.T) {
	t.Parallel()

	gw := newTestGatewayFull(t)
	handler := gw.Handler()
	rec := doRequest(t, handler, http.MethodPost, "/operator/sandbox/exec",
		`{"command":["echo","hello"]}`)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("nil runner: status = %d", rec.Code)
	}
}

func TestSandboxExecInvalidJSON(t *testing.T) {
	t.Parallel()

	gw := newTestGatewayFull(t)
	// Sandbox is nil, so should return 503 before JSON parse.
	handler := gw.Handler()
	rec := doRequest(t, handler, http.MethodPost, "/operator/sandbox/exec", "not-json")
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("invalid json with nil runner: status = %d", rec.Code)
	}
}

// ---------------------------------------------------------------------------
// handleSandboxStatus
// ---------------------------------------------------------------------------

func TestSandboxStatusNilRunner(t *testing.T) {
	t.Parallel()

	gw := newTestGatewayFull(t)
	handler := gw.Handler()
	rec := doRequest(t, handler, http.MethodGet, "/operator/sandbox/status", "")
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("nil runner: status = %d", rec.Code)
	}

	var payload map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if _, ok := payload["error"]; !ok {
		t.Fatal("expected error field in response")
	}
}

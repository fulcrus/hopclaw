package gateway

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/fulcrus/hopclaw/heartbeat"
)

// ---------------------------------------------------------------------------
// handleHeartbeat
// ---------------------------------------------------------------------------

func TestHeartbeatNilService(t *testing.T) {
	t.Parallel()

	gw := newTestGatewayFull(t)
	handler := gw.Handler()
	rec := doRequest(t, handler, http.MethodGet, "/operator/heartbeat", "")
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("nil service: status = %d", rec.Code)
	}
}

func TestHeartbeatReturnsStatus(t *testing.T) {
	t.Parallel()

	svc := heartbeat.NewService(heartbeat.Config{})
	gw := newTestGatewayFull(t)
	gw.SetHeartbeat(svc)
	handler := gw.Handler()

	rec := doRequest(t, handler, http.MethodGet, "/operator/heartbeat", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("heartbeat: status = %d body=%s", rec.Code, rec.Body.String())
	}

	var payload heartbeatResponse
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	// Service not started yet — running should be false.
	if payload.Running {
		t.Fatal("expected running=false for unstarted service")
	}
}

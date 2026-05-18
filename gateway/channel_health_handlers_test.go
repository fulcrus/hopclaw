package gateway

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/fulcrus/hopclaw/channels/health"
	channelmgr "github.com/fulcrus/hopclaw/channels/manager"
	"github.com/fulcrus/hopclaw/eventbus"
)

// ---------------------------------------------------------------------------
// handleChannelHealth
// ---------------------------------------------------------------------------

func TestChannelHealthNilMonitor(t *testing.T) {
	t.Parallel()

	gw := newTestGatewayFull(t)
	handler := gw.Handler()
	rec := doRequest(t, handler, http.MethodGet, "/operator/channels/health", "")
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("nil monitor: status = %d", rec.Code)
	}
}

func TestChannelHealthEmpty(t *testing.T) {
	t.Parallel()

	mgr := channelmgr.New()
	bus := eventbus.NewInMemoryBus()
	monitor := health.NewMonitor(health.Config{}, mgr, bus)

	gw := newTestGatewayFull(t)
	gw.SetChannelHealth(monitor)
	handler := gw.Handler()

	rec := doRequest(t, handler, http.MethodGet, "/operator/channels/health", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("empty health: status = %d body=%s", rec.Code, rec.Body.String())
	}

	var payload struct {
		Items []any `json:"items"`
		Count int   `json:"count"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if payload.Count != 0 {
		t.Fatalf("count = %d, want 0", payload.Count)
	}
}

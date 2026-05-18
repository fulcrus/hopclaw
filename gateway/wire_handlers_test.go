package gateway

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/fulcrus/hopclaw/wire"
)

// ---------------------------------------------------------------------------
// handleWireEntries
// ---------------------------------------------------------------------------

func TestWireEntriesNilLogger(t *testing.T) {
	t.Parallel()

	gw := newTestGatewayFull(t)
	handler := gw.Handler()
	rec := doRequest(t, handler, http.MethodGet, "/operator/wire/entries", "")
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("nil logger: status = %d", rec.Code)
	}
}

func TestWireEntriesEmpty(t *testing.T) {
	t.Parallel()

	logger := wire.NewLogger(wire.Config{Enabled: true})
	gw := newTestGatewayFull(t)
	gw.SetWire(logger)
	handler := gw.Handler()

	rec := doRequest(t, handler, http.MethodGet, "/operator/wire/entries", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("empty entries: status = %d body=%s", rec.Code, rec.Body.String())
	}

	var payload wireEntriesResponse
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if payload.Count != 0 {
		t.Fatalf("count = %d, want 0", payload.Count)
	}
}

func TestWireEntriesWithLimitQuery(t *testing.T) {
	t.Parallel()

	logger := wire.NewLogger(wire.Config{Enabled: true})
	gw := newTestGatewayFull(t)
	gw.SetWire(logger)
	handler := gw.Handler()

	rec := doRequest(t, handler, http.MethodGet, "/operator/wire/entries?limit=10", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("with limit: status = %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestWireEntriesWithProviderFilter(t *testing.T) {
	t.Parallel()

	logger := wire.NewLogger(wire.Config{Enabled: true})
	gw := newTestGatewayFull(t)
	gw.SetWire(logger)
	handler := gw.Handler()

	rec := doRequest(t, handler, http.MethodGet, "/operator/wire/entries?provider=openai", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("with provider: status = %d body=%s", rec.Code, rec.Body.String())
	}
}

// ---------------------------------------------------------------------------
// handleWireStats
// ---------------------------------------------------------------------------

func TestWireStatsNilLogger(t *testing.T) {
	t.Parallel()

	gw := newTestGatewayFull(t)
	handler := gw.Handler()
	rec := doRequest(t, handler, http.MethodGet, "/operator/wire/stats", "")
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("nil logger: status = %d", rec.Code)
	}
}

func TestWireStatsReturnsOK(t *testing.T) {
	t.Parallel()

	logger := wire.NewLogger(wire.Config{Enabled: true})
	gw := newTestGatewayFull(t)
	gw.SetWire(logger)
	handler := gw.Handler()

	rec := doRequest(t, handler, http.MethodGet, "/operator/wire/stats", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("stats: status = %d body=%s", rec.Code, rec.Body.String())
	}
}

// ---------------------------------------------------------------------------
// handleWireClear
// ---------------------------------------------------------------------------

func TestWireClearNilLogger(t *testing.T) {
	t.Parallel()

	gw := newTestGatewayFull(t)
	handler := gw.Handler()
	rec := doRequest(t, handler, http.MethodDelete, "/operator/wire/entries", "")
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("nil logger: status = %d", rec.Code)
	}
}

func TestWireClearSuccess(t *testing.T) {
	t.Parallel()

	logger := wire.NewLogger(wire.Config{Enabled: true})
	gw := newTestGatewayFull(t)
	gw.SetWire(logger)
	handler := gw.Handler()

	rec := doRequest(t, handler, http.MethodDelete, "/operator/wire/entries", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("clear: status = %d body=%s", rec.Code, rec.Body.String())
	}

	var payload wireClearResponse
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if !payload.OK {
		t.Fatal("expected ok=true")
	}
}

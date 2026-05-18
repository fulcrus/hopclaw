package gateway

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/fulcrus/hopclaw/channels/allowlist"
)

// ---------------------------------------------------------------------------
// handleAllowlistList
// ---------------------------------------------------------------------------

func TestAllowlistListNilService(t *testing.T) {
	t.Parallel()

	gw := newTestGatewayFull(t)
	handler := gw.Handler()
	rec := doRequest(t, handler, http.MethodGet, "/operator/allowlist", "")
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("nil service: status = %d", rec.Code)
	}
}

func TestAllowlistListEmpty(t *testing.T) {
	t.Parallel()

	mgr := allowlist.NewManager(nil)
	gw := newTestGatewayFull(t)
	gw.SetAllowlist(mgr)
	handler := gw.Handler()

	rec := doRequest(t, handler, http.MethodGet, "/operator/allowlist", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("empty list: status = %d body=%s", rec.Code, rec.Body.String())
	}

	var payload allowlistListResponse
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if payload.Count != 0 {
		t.Fatalf("count = %d, want 0", payload.Count)
	}
}

func TestAllowlistListWithRules(t *testing.T) {
	t.Parallel()

	mgr := allowlist.NewManager([]allowlist.ChannelRules{
		{Channel: "slack", AllowAll: true},
	})
	gw := newTestGatewayFull(t)
	gw.SetAllowlist(mgr)
	handler := gw.Handler()

	rec := doRequest(t, handler, http.MethodGet, "/operator/allowlist", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("list: status = %d body=%s", rec.Code, rec.Body.String())
	}

	var payload allowlistListResponse
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if payload.Count != 1 {
		t.Fatalf("count = %d, want 1", payload.Count)
	}
}

// ---------------------------------------------------------------------------
// handleAllowlistGet
// ---------------------------------------------------------------------------

func TestAllowlistGetNilService(t *testing.T) {
	t.Parallel()

	gw := newTestGatewayFull(t)
	handler := gw.Handler()
	rec := doRequest(t, handler, http.MethodGet, "/operator/allowlist/slack", "")
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("nil service: status = %d", rec.Code)
	}
}

func TestAllowlistGetFound(t *testing.T) {
	t.Parallel()

	mgr := allowlist.NewManager([]allowlist.ChannelRules{
		{Channel: "slack", AllowAll: true},
	})
	gw := newTestGatewayFull(t)
	gw.SetAllowlist(mgr)
	handler := gw.Handler()

	rec := doRequest(t, handler, http.MethodGet, "/operator/allowlist/slack", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("get: status = %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestAllowlistGetNotFound(t *testing.T) {
	t.Parallel()

	mgr := allowlist.NewManager(nil)
	gw := newTestGatewayFull(t)
	gw.SetAllowlist(mgr)
	handler := gw.Handler()

	rec := doRequest(t, handler, http.MethodGet, "/operator/allowlist/nonexistent", "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("not found: status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

// ---------------------------------------------------------------------------
// handleAllowlistSet
// ---------------------------------------------------------------------------

func TestAllowlistSetNilService(t *testing.T) {
	t.Parallel()

	gw := newTestGatewayFull(t)
	handler := gw.Handler()
	rec := doRequest(t, handler, http.MethodPut, "/operator/allowlist/slack",
		`{"allow_all":true}`)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("nil service: status = %d", rec.Code)
	}
}

func TestAllowlistSetSuccess(t *testing.T) {
	t.Parallel()

	mgr := allowlist.NewManager(nil)
	gw := newTestGatewayFull(t)
	gw.SetAllowlist(mgr)
	handler := gw.Handler()

	rec := doRequest(t, handler, http.MethodPut, "/operator/allowlist/discord",
		`{"allow_all":false,"allow_users":["user-1","user-2"]}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("set: status = %d body=%s", rec.Code, rec.Body.String())
	}

	var payload allowlistSetResponse
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if !payload.OK {
		t.Fatal("expected ok=true")
	}
	if payload.Channel != "discord" {
		t.Fatalf("channel = %q, want discord", payload.Channel)
	}

	// Verify it was persisted.
	getRec := doRequest(t, handler, http.MethodGet, "/operator/allowlist/discord", "")
	if getRec.Code != http.StatusOK {
		t.Fatalf("get after set: status = %d", getRec.Code)
	}
}

func TestAllowlistSetInvalidJSON(t *testing.T) {
	t.Parallel()

	mgr := allowlist.NewManager(nil)
	gw := newTestGatewayFull(t)
	gw.SetAllowlist(mgr)
	handler := gw.Handler()

	rec := doRequest(t, handler, http.MethodPut, "/operator/allowlist/slack", "not-json")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid json: status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

// ---------------------------------------------------------------------------
// handleAllowlistDelete
// ---------------------------------------------------------------------------

func TestAllowlistDeleteNilService(t *testing.T) {
	t.Parallel()

	gw := newTestGatewayFull(t)
	handler := gw.Handler()
	rec := doRequest(t, handler, http.MethodDelete, "/operator/allowlist/slack", "")
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("nil service: status = %d", rec.Code)
	}
}

func TestAllowlistDeleteSuccess(t *testing.T) {
	t.Parallel()

	mgr := allowlist.NewManager([]allowlist.ChannelRules{
		{Channel: "slack", AllowAll: true},
	})
	gw := newTestGatewayFull(t)
	gw.SetAllowlist(mgr)
	handler := gw.Handler()

	rec := doRequest(t, handler, http.MethodDelete, "/operator/allowlist/slack", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("delete: status = %d body=%s", rec.Code, rec.Body.String())
	}

	var payload allowlistDeleteResponse
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if !payload.OK {
		t.Fatal("expected ok=true")
	}

	// Verify it was removed.
	getRec := doRequest(t, handler, http.MethodGet, "/operator/allowlist/slack", "")
	if getRec.Code != http.StatusNotFound {
		t.Fatalf("after delete: status = %d, want %d", getRec.Code, http.StatusNotFound)
	}
}

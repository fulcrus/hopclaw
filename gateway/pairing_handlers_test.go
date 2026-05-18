package gateway

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/fulcrus/hopclaw/channels/pairing"
)

// ---------------------------------------------------------------------------
// handlePairingList
// ---------------------------------------------------------------------------

func TestPairingListNilService(t *testing.T) {
	t.Parallel()

	gw := newTestGatewayFull(t)
	handler := gw.Handler()
	rec := doRequest(t, handler, http.MethodGet, "/operator/pairing", "")
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("nil service: status = %d", rec.Code)
	}
}

func TestPairingListEmpty(t *testing.T) {
	t.Parallel()

	store := pairing.NewInMemoryStore()
	mgr := pairing.NewManager(store)
	gw := newTestGatewayFull(t)
	gw.pairing = mgr
	handler := gw.Handler()

	rec := doRequest(t, handler, http.MethodGet, "/operator/pairing", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("empty list: status = %d body=%s", rec.Code, rec.Body.String())
	}

	var payload pairingListResponse
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if payload.Count != 0 {
		t.Fatalf("count = %d, want 0", payload.Count)
	}
}

func TestPairingListWithRecords(t *testing.T) {
	t.Parallel()

	store := pairing.NewInMemoryStore()
	mgr := pairing.NewManager(store)
	_, err := mgr.InitiatePairing("slack", "user-1", "Alice")
	if err != nil {
		t.Fatalf("InitiatePairing() error = %v", err)
	}

	gw := newTestGatewayFull(t)
	gw.pairing = mgr
	handler := gw.Handler()

	rec := doRequest(t, handler, http.MethodGet, "/operator/pairing", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("list: status = %d body=%s", rec.Code, rec.Body.String())
	}

	var payload pairingListResponse
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if payload.Count != 1 {
		t.Fatalf("count = %d, want 1", payload.Count)
	}
}

func TestPairingInitiateSuccess(t *testing.T) {
	t.Parallel()

	store := pairing.NewInMemoryStore()
	mgr := pairing.NewManager(store)
	gw := newTestGatewayFull(t)
	gw.pairing = mgr
	handler := gw.Handler()

	rec := doRequest(t, handler, http.MethodPost, "/operator/pairing/initiate", `{"channel":"feishu","user_id":"ou_1","display_name":"Alice"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("initiate: status = %d body=%s", rec.Code, rec.Body.String())
	}

	var payload pairingRecordResponse
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if payload.Record.Channel != "feishu" {
		t.Fatalf("channel = %q, want feishu", payload.Record.Channel)
	}
	if payload.Record.UserID != "ou_1" {
		t.Fatalf("user_id = %q, want ou_1", payload.Record.UserID)
	}
	if payload.Record.Code == "" {
		t.Fatal("expected pairing code to be returned")
	}
}

func TestPairingInitiateBadRequest(t *testing.T) {
	t.Parallel()

	store := pairing.NewInMemoryStore()
	mgr := pairing.NewManager(store)
	gw := newTestGatewayFull(t)
	gw.pairing = mgr
	handler := gw.Handler()

	rec := doRequest(t, handler, http.MethodPost, "/operator/pairing/initiate", `{"channel":"","user_id":""}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bad request: status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestPairingInitiateRejectsTrailingJSON(t *testing.T) {
	t.Parallel()

	store := pairing.NewInMemoryStore()
	mgr := pairing.NewManager(store)
	gw := newTestGatewayFull(t)
	gw.pairing = mgr
	handler := gw.Handler()

	rec := doRequest(t, handler, http.MethodPost, "/operator/pairing/initiate", `{"channel":"feishu","user_id":"ou_1"} {"extra":true}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("trailing json: status = %d body=%s", rec.Code, rec.Body.String())
	}

	records, err := mgr.List()
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(records) != 0 {
		t.Fatalf("records = %#v, want empty", records)
	}
}

// ---------------------------------------------------------------------------
// handlePairingVerify
// ---------------------------------------------------------------------------

func TestPairingVerifyNilService(t *testing.T) {
	t.Parallel()

	gw := newTestGatewayFull(t)
	handler := gw.Handler()
	rec := doRequest(t, handler, http.MethodPost, "/operator/pairing/verify", `{"code":"123456"}`)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("nil service: status = %d", rec.Code)
	}
}

func TestPairingVerifySuccess(t *testing.T) {
	t.Parallel()

	store := pairing.NewInMemoryStore()
	mgr := pairing.NewManager(store)
	code, err := mgr.InitiatePairing("slack", "user-1", "Alice")
	if err != nil {
		t.Fatalf("InitiatePairing() error = %v", err)
	}

	gw := newTestGatewayFull(t)
	gw.pairing = mgr
	handler := gw.Handler()

	rec := doRequest(t, handler, http.MethodPost, "/operator/pairing/verify", `{"code":"`+code+`"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("verify: status = %d body=%s", rec.Code, rec.Body.String())
	}

	var payload pairingRecordResponse
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if payload.Record.Channel != "slack" {
		t.Fatalf("channel = %q, want slack", payload.Record.Channel)
	}
}

func TestPairingVerifyInvalidCode(t *testing.T) {
	t.Parallel()

	store := pairing.NewInMemoryStore()
	mgr := pairing.NewManager(store)
	gw := newTestGatewayFull(t)
	gw.pairing = mgr
	handler := gw.Handler()

	rec := doRequest(t, handler, http.MethodPost, "/operator/pairing/verify", `{"code":"wrong-code"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid code: status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestPairingVerifyEmptyCode(t *testing.T) {
	t.Parallel()

	store := pairing.NewInMemoryStore()
	mgr := pairing.NewManager(store)
	gw := newTestGatewayFull(t)
	gw.pairing = mgr
	handler := gw.Handler()

	rec := doRequest(t, handler, http.MethodPost, "/operator/pairing/verify", `{"code":""}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("empty code: status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestPairingVerifyInvalidJSON(t *testing.T) {
	t.Parallel()

	store := pairing.NewInMemoryStore()
	mgr := pairing.NewManager(store)
	gw := newTestGatewayFull(t)
	gw.pairing = mgr
	handler := gw.Handler()

	rec := doRequest(t, handler, http.MethodPost, "/operator/pairing/verify", "not-json")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid json: status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestPairingVerifyRejectsTrailingJSON(t *testing.T) {
	t.Parallel()

	store := pairing.NewInMemoryStore()
	mgr := pairing.NewManager(store)
	code, err := mgr.InitiatePairing("slack", "user-1", "Alice")
	if err != nil {
		t.Fatalf("InitiatePairing() error = %v", err)
	}

	gw := newTestGatewayFull(t)
	gw.pairing = mgr
	handler := gw.Handler()

	rec := doRequest(t, handler, http.MethodPost, "/operator/pairing/verify", `{"code":"`+code+`"} {"extra":true}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("trailing json: status = %d body=%s", rec.Code, rec.Body.String())
	}

	record, err := store.Get("slack", "user-1")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if record.Status != pairing.StatusPending {
		t.Fatalf("status = %q, want pending", record.Status)
	}
}

// ---------------------------------------------------------------------------
// handlePairingRevoke
// ---------------------------------------------------------------------------

func TestPairingRevokeNilService(t *testing.T) {
	t.Parallel()

	gw := newTestGatewayFull(t)
	handler := gw.Handler()
	rec := doRequest(t, handler, http.MethodDelete, "/operator/pairing/slack/user-1", "")
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("nil service: status = %d", rec.Code)
	}
}

func TestPairingRevokeSuccess(t *testing.T) {
	t.Parallel()

	store := pairing.NewInMemoryStore()
	mgr := pairing.NewManager(store)
	code, err := mgr.InitiatePairing("slack", "user-1", "Alice")
	if err != nil {
		t.Fatalf("InitiatePairing() error = %v", err)
	}
	// Verify to set the record as verified.
	_, _ = mgr.VerifyCode(code)

	gw := newTestGatewayFull(t)
	gw.pairing = mgr
	handler := gw.Handler()

	rec := doRequest(t, handler, http.MethodDelete, "/operator/pairing/slack/user-1", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("revoke: status = %d body=%s", rec.Code, rec.Body.String())
	}

	var payload map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if payload["ok"] != true {
		t.Fatalf("ok = %v, want true", payload["ok"])
	}
}

func TestPairingRevokeNotFound(t *testing.T) {
	t.Parallel()

	store := pairing.NewInMemoryStore()
	mgr := pairing.NewManager(store)
	gw := newTestGatewayFull(t)
	gw.pairing = mgr
	handler := gw.Handler()

	rec := doRequest(t, handler, http.MethodDelete, "/operator/pairing/slack/no-such-user", "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("not found: status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

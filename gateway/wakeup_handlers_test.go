package gateway

import (
	"context"
	"encoding/json"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fulcrus/hopclaw/wakeup"
)

func newTestWakeupGateway(t *testing.T) *Gateway {
	t.Helper()

	store, err := wakeup.Load(filepath.Join(t.TempDir(), "wakeup.json"))
	if err != nil {
		t.Fatalf("wakeup.Load() error = %v", err)
	}
	svc := wakeup.NewService(store, func(_ context.Context, _ wakeup.Trigger) (*wakeup.ExecutionResult, error) { return nil, nil })
	gw := newTestGatewayFull(t)
	gw.SetWakeup(svc)
	return gw
}

// ---------------------------------------------------------------------------
// handleWakeupList
// ---------------------------------------------------------------------------

func TestWakeupListNilService(t *testing.T) {
	t.Parallel()

	gw := newTestGatewayFull(t)
	handler := gw.Handler()
	rec := doRequest(t, handler, http.MethodGet, "/operator/wakeup/triggers", "")
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("nil service: status = %d", rec.Code)
	}
}

func TestWakeupListEmpty(t *testing.T) {
	t.Parallel()

	gw := newTestWakeupGateway(t)
	handler := gw.Handler()

	rec := doRequest(t, handler, http.MethodGet, "/operator/wakeup/triggers", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("empty list: status = %d body=%s", rec.Code, rec.Body.String())
	}

	var payload wakeupTriggerListResponse
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if payload.Count != 0 {
		t.Fatalf("count = %d, want 0", payload.Count)
	}
}

// ---------------------------------------------------------------------------
// handleWakeupCreate
// ---------------------------------------------------------------------------

func TestWakeupCreateNilService(t *testing.T) {
	t.Parallel()

	gw := newTestGatewayFull(t)
	handler := gw.Handler()
	rec := doRequest(t, handler, http.MethodPost, "/operator/wakeup/triggers",
		`{"schedule":"0 9 * * *","message":"morning"}`)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("nil service: status = %d", rec.Code)
	}
}

func TestWakeupCreateSuccess(t *testing.T) {
	t.Parallel()

	gw := newTestWakeupGateway(t)
	handler := gw.Handler()

	body := `{"name":"daily","schedule":"0 9 * * *","channel":"slack","message":"good morning"}`
	rec := doRequest(t, handler, http.MethodPost, "/operator/wakeup/triggers", body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: status = %d body=%s", rec.Code, rec.Body.String())
	}

	var payload wakeupTriggerResponse
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if payload.Trigger.Name != "daily" {
		t.Fatalf("name = %q, want daily", payload.Trigger.Name)
	}
	if payload.Trigger.ID == "" {
		t.Fatal("trigger ID is empty")
	}
	if !payload.Trigger.Enabled {
		t.Fatal("expected enabled=true by default")
	}
}

func TestWakeupCreateRejectsLegacyAgentAlias(t *testing.T) {
	t.Parallel()

	gw := newTestWakeupGateway(t)
	handler := gw.Handler()

	body := `{"name":"daily","schedule":"0 9 * * *","agent":"ops:daily","message":"good morning"}`
	rec := doRequest(t, handler, http.MethodPost, "/operator/wakeup/triggers", body)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("create: status = %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestWakeupCreateMissingSchedule(t *testing.T) {
	t.Parallel()

	gw := newTestWakeupGateway(t)
	handler := gw.Handler()

	rec := doRequest(t, handler, http.MethodPost, "/operator/wakeup/triggers",
		`{"message":"hello"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("missing schedule: status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestWakeupCreateMissingMessage(t *testing.T) {
	t.Parallel()

	gw := newTestWakeupGateway(t)
	handler := gw.Handler()

	rec := doRequest(t, handler, http.MethodPost, "/operator/wakeup/triggers",
		`{"schedule":"0 9 * * *"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("missing message: status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestWakeupCreateInvalidJSON(t *testing.T) {
	t.Parallel()

	gw := newTestWakeupGateway(t)
	handler := gw.Handler()

	rec := doRequest(t, handler, http.MethodPost, "/operator/wakeup/triggers", "not-json")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid json: status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestWakeupCreateRejectsTrailingJSON(t *testing.T) {
	t.Parallel()

	gw := newTestWakeupGateway(t)
	handler := gw.Handler()

	rec := doRequest(t, handler, http.MethodPost, "/operator/wakeup/triggers", `{"name":"daily","schedule":"0 9 * * *","message":"good morning"} {"extra":true}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("trailing json: status = %d body=%s", rec.Code, rec.Body.String())
	}

	listRec := doRequest(t, handler, http.MethodGet, "/operator/wakeup/triggers", "")
	var payload wakeupTriggerListResponse
	if err := json.NewDecoder(listRec.Body).Decode(&payload); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if payload.Count != 0 {
		t.Fatalf("count = %d, want 0", payload.Count)
	}
}

func TestWakeupCreateRejectsUnknownField(t *testing.T) {
	t.Parallel()

	gw := newTestWakeupGateway(t)
	handler := gw.Handler()

	rec := doRequest(t, handler, http.MethodPost, "/operator/wakeup/triggers",
		`{"name":"daily","schedule":"0 9 * * *","message":"good morning","prompt":"legacy"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("unknown field: status = %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestWakeupCreateOmitsZeroTimestamps(t *testing.T) {
	t.Parallel()

	gw := newTestWakeupGateway(t)
	handler := gw.Handler()

	rec := doRequest(t, handler, http.MethodPost, "/operator/wakeup/triggers",
		`{"name":"manual","schedule":"0 9 * * *","message":"hello","enabled":false}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create disabled: status = %d body=%s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "0001-01-01T00:00:00Z") {
		t.Fatalf("create response leaked zero time: %s", rec.Body.String())
	}

	var created wakeupTriggerResponse
	if err := json.NewDecoder(rec.Body).Decode(&created); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if created.Trigger.LastRunAt != nil {
		t.Fatalf("last_run_at = %v, want nil", created.Trigger.LastRunAt)
	}
	if created.Trigger.NextRunAt != nil {
		t.Fatalf("next_run_at = %v, want nil", created.Trigger.NextRunAt)
	}

	listRec := doRequest(t, handler, http.MethodGet, "/operator/wakeup/triggers", "")
	if strings.Contains(listRec.Body.String(), "0001-01-01T00:00:00Z") {
		t.Fatalf("list response leaked zero time: %s", listRec.Body.String())
	}
}

// ---------------------------------------------------------------------------
// handleWakeupGet
// ---------------------------------------------------------------------------

func TestWakeupGetSuccess(t *testing.T) {
	t.Parallel()

	gw := newTestWakeupGateway(t)
	handler := gw.Handler()

	// Create first.
	createRec := doRequest(t, handler, http.MethodPost, "/operator/wakeup/triggers",
		`{"name":"fetch-me","schedule":"0 9 * * *","message":"hello"}`)
	var created wakeupTriggerResponse
	if err := json.NewDecoder(createRec.Body).Decode(&created); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}

	rec := doRequest(t, handler, http.MethodGet, "/operator/wakeup/triggers/"+created.Trigger.ID, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("get: status = %d body=%s", rec.Code, rec.Body.String())
	}

	var got wakeupTriggerResponse
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if got.Trigger.Name != "fetch-me" {
		t.Fatalf("name = %q, want fetch-me", got.Trigger.Name)
	}
}

func TestWakeupGetNotFound(t *testing.T) {
	t.Parallel()

	gw := newTestWakeupGateway(t)
	handler := gw.Handler()

	rec := doRequest(t, handler, http.MethodGet, "/operator/wakeup/triggers/nonexistent", "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("not found: status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

// ---------------------------------------------------------------------------
// handleWakeupUpdate
// ---------------------------------------------------------------------------

func TestWakeupUpdateSuccess(t *testing.T) {
	t.Parallel()

	gw := newTestWakeupGateway(t)
	handler := gw.Handler()

	// Create.
	createRec := doRequest(t, handler, http.MethodPost, "/operator/wakeup/triggers",
		`{"name":"original","schedule":"0 9 * * *","message":"hello"}`)
	var created wakeupTriggerResponse
	if err := json.NewDecoder(createRec.Body).Decode(&created); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}

	// Patch name.
	rec := doRequest(t, handler, http.MethodPatch, "/operator/wakeup/triggers/"+created.Trigger.ID,
		`{"name":"updated"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("update: status = %d body=%s", rec.Code, rec.Body.String())
	}

	var updated wakeupTriggerResponse
	if err := json.NewDecoder(rec.Body).Decode(&updated); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if updated.Trigger.Name != "updated" {
		t.Fatalf("name = %q, want updated", updated.Trigger.Name)
	}
}

func TestWakeupUpdateNotFound(t *testing.T) {
	t.Parallel()

	gw := newTestWakeupGateway(t)
	handler := gw.Handler()

	rec := doRequest(t, handler, http.MethodPatch, "/operator/wakeup/triggers/nonexistent",
		`{"name":"nope"}`)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("not found: status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

func TestWakeupUpdateRejectsTrailingJSON(t *testing.T) {
	t.Parallel()

	gw := newTestWakeupGateway(t)
	handler := gw.Handler()

	createRec := doRequest(t, handler, http.MethodPost, "/operator/wakeup/triggers",
		`{"name":"original","schedule":"0 9 * * *","message":"hello"}`)
	var created wakeupTriggerResponse
	if err := json.NewDecoder(createRec.Body).Decode(&created); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}

	rec := doRequest(t, handler, http.MethodPatch, "/operator/wakeup/triggers/"+created.Trigger.ID, `{"name":"updated"} {"extra":true}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("trailing json: status = %d body=%s", rec.Code, rec.Body.String())
	}

	getRec := doRequest(t, handler, http.MethodGet, "/operator/wakeup/triggers/"+created.Trigger.ID, "")
	var got wakeupTriggerResponse
	if err := json.NewDecoder(getRec.Body).Decode(&got); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if got.Trigger.Name != "original" {
		t.Fatalf("name = %q, want original", got.Trigger.Name)
	}
}

func TestWakeupUpdateRejectsUnknownField(t *testing.T) {
	t.Parallel()

	gw := newTestWakeupGateway(t)
	handler := gw.Handler()

	createRec := doRequest(t, handler, http.MethodPost, "/operator/wakeup/triggers",
		`{"name":"original","schedule":"0 9 * * *","message":"hello"}`)
	var created wakeupTriggerResponse
	if err := json.NewDecoder(createRec.Body).Decode(&created); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}

	rec := doRequest(t, handler, http.MethodPatch, "/operator/wakeup/triggers/"+created.Trigger.ID, `{"prompt":"legacy"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("unknown field update: status = %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestWakeupUpdateRejectsLegacyAgentAlias(t *testing.T) {
	t.Parallel()

	gw := newTestWakeupGateway(t)
	handler := gw.Handler()

	createRec := doRequest(t, handler, http.MethodPost, "/operator/wakeup/triggers",
		`{"name":"original","schedule":"0 9 * * *","message":"hello"}`)
	var created wakeupTriggerResponse
	if err := json.NewDecoder(createRec.Body).Decode(&created); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}

	rec := doRequest(t, handler, http.MethodPatch, "/operator/wakeup/triggers/"+created.Trigger.ID, `{"agent":"ops:daily"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("legacy agent update: status = %d body=%s", rec.Code, rec.Body.String())
	}
}

// ---------------------------------------------------------------------------
// handleWakeupDelete
// ---------------------------------------------------------------------------

func TestWakeupDeleteSuccess(t *testing.T) {
	t.Parallel()

	gw := newTestWakeupGateway(t)
	handler := gw.Handler()

	// Create then delete.
	createRec := doRequest(t, handler, http.MethodPost, "/operator/wakeup/triggers",
		`{"name":"delete-me","schedule":"0 9 * * *","message":"bye"}`)
	var created wakeupTriggerResponse
	if err := json.NewDecoder(createRec.Body).Decode(&created); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}

	rec := doRequest(t, handler, http.MethodDelete, "/operator/wakeup/triggers/"+created.Trigger.ID, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("delete: status = %d body=%s", rec.Code, rec.Body.String())
	}

	var payload wakeupDeleteResponse
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if !payload.OK {
		t.Fatal("expected ok=true")
	}

	// Verify it is gone.
	getRec := doRequest(t, handler, http.MethodGet, "/operator/wakeup/triggers/"+created.Trigger.ID, "")
	if getRec.Code != http.StatusNotFound {
		t.Fatalf("after delete: status = %d, want %d", getRec.Code, http.StatusNotFound)
	}
}

func TestWakeupDeleteNotFound(t *testing.T) {
	t.Parallel()

	gw := newTestWakeupGateway(t)
	handler := gw.Handler()

	rec := doRequest(t, handler, http.MethodDelete, "/operator/wakeup/triggers/nonexistent", "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("not found: status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

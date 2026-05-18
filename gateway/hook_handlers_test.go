package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/fulcrus/hopclaw/agent"
	"github.com/fulcrus/hopclaw/hooks"
	runtimepkg "github.com/fulcrus/hopclaw/runtime"
)

func newTestHookGateway(t *testing.T) *Gateway {
	t.Helper()

	store := hooks.NewInMemoryStore()
	executor := hooks.NewExecutor(store)
	gw := newTestGatewayFull(t)
	gw.SetHooks(executor)
	return gw
}

func newTestHookSurface(t *testing.T) (*Gateway, *operatorHookSurface) {
	t.Helper()
	gw := newTestHookGateway(t)
	return gw, newOperatorHookSurface(hookOperatorDepsFromGateway(gw))
}

// ---------------------------------------------------------------------------
// handleHooksList
// ---------------------------------------------------------------------------

func TestHooksListNilExecutor(t *testing.T) {
	t.Parallel()

	gw := newTestGatewayFull(t)
	handler := gw.Handler()
	rec := doRequest(t, handler, http.MethodGet, "/operator/hooks", "")
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("nil executor: status = %d", rec.Code)
	}
}

func TestHooksListEmpty(t *testing.T) {
	t.Parallel()

	gw := newTestHookGateway(t)
	handler := gw.Handler()

	rec := doRequest(t, handler, http.MethodGet, "/operator/hooks", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("empty list: status = %d body=%s", rec.Code, rec.Body.String())
	}

	var payload hookListResponse
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if payload.Count != 0 {
		t.Fatalf("count = %d, want 0", payload.Count)
	}
}

func TestHooksListKeepsItemsWithAuthContext(t *testing.T) {
	t.Parallel()

	gw, surface := newTestHookSurface(t)
	_, _ = gw.hooks.Store().Add(context.Background(), hooks.Hook{
		Name:    "hook-a",
		Enabled: true,
		Trigger: hooks.TriggerRunCompleted,
		Kind:    hooks.KindCommand,
		Command: "true",
	})
	_, _ = gw.hooks.Store().Add(context.Background(), hooks.Hook{
		Name:    "hook-b",
		Enabled: true,
		Trigger: hooks.TriggerRunCompleted,
		Kind:    hooks.KindCommand,
		Command: "true",
	})

	req := httptest.NewRequest(http.MethodGet, "/operator/hooks", nil).
		WithContext(scopedAuthContext("actor-a"))
	rec := httptest.NewRecorder()
	http.HandlerFunc(surface.handleHooksList).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list scoped hooks: status = %d body=%s", rec.Code, rec.Body.String())
	}

	var payload hookListResponse
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if payload.Count != 2 {
		t.Fatalf("payload = %+v", payload)
	}
}

func TestHooksListFiltersByAuthenticatedAutomationScope(t *testing.T) {
	t.Parallel()

	gw, surface := newTestHookSurface(t)
	_, _ = gw.hooks.Store().Add(context.Background(), hooks.Hook{
		Name:         "hook-a",
		Enabled:      true,
		Trigger:      hooks.TriggerRunCompleted,
		Kind:         hooks.KindCommand,
		Command:      "true",
		AutomationID: "automation-a",
	})
	_, _ = gw.hooks.Store().Add(context.Background(), hooks.Hook{
		Name:         "hook-b",
		Enabled:      true,
		Trigger:      hooks.TriggerRunCompleted,
		Kind:         hooks.KindCommand,
		Command:      "true",
		AutomationID: "automation-b",
	})

	req := httptest.NewRequest(http.MethodGet, "/operator/hooks", nil).
		WithContext(scopedAutomationAuthContext("actor-a", "automation-a"))
	rec := httptest.NewRecorder()
	http.HandlerFunc(surface.handleHooksList).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list scoped hooks: status = %d body=%s", rec.Code, rec.Body.String())
	}

	var payload hookListResponse
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if payload.Count != 1 || len(payload.Items) != 1 {
		t.Fatalf("payload = %+v", payload)
	}
	if payload.Items[0].AutomationID != "automation-a" {
		t.Fatalf("payload.Items[0].AutomationID = %q, want automation-a", payload.Items[0].AutomationID)
	}
}

func TestHooksEventsCatalog(t *testing.T) {
	t.Parallel()

	gw := newTestHookGateway(t)
	handler := gw.Handler()

	rec := doRequest(t, handler, http.MethodGet, "/operator/hooks/events", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("events: status = %d body=%s", rec.Code, rec.Body.String())
	}

	var payload hookEventsResponse
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if payload.Count == 0 {
		t.Fatal("expected hook event catalog entries")
	}
	found := false
	for _, item := range payload.Items {
		if item.Trigger == hooks.TriggerBeforeToolCall {
			found = true
			if !item.CanBlock {
				t.Fatal("expected before.tool_call to be blocking")
			}
			break
		}
	}
	if !found {
		t.Fatal("expected before.tool_call catalog entry")
	}
}

// ---------------------------------------------------------------------------
// handleHooksCreate
// ---------------------------------------------------------------------------

func TestHooksCreateNilExecutor(t *testing.T) {
	t.Parallel()

	gw := newTestGatewayFull(t)
	handler := gw.Handler()
	rec := doRequest(t, handler, http.MethodPost, "/operator/hooks",
		`{"trigger":"run.completed","kind":"http","url":"http://example.com"}`)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("nil executor: status = %d", rec.Code)
	}
}

func TestHooksCreateHTTPHookSuccess(t *testing.T) {
	t.Parallel()

	gw := newTestHookGateway(t)
	handler := gw.Handler()

	body := `{"name":"my-hook","trigger":"run.completed","kind":"http","url":"http://example.com/hook"}`
	rec := doRequest(t, handler, http.MethodPost, "/operator/hooks", body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: status = %d body=%s", rec.Code, rec.Body.String())
	}

	var payload hookResponse
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if payload.Hook.Name != "my-hook" {
		t.Fatalf("name = %q, want my-hook", payload.Hook.Name)
	}
	if payload.Hook.ID == "" {
		t.Fatal("hook ID is empty")
	}
	if !payload.Hook.Enabled {
		t.Fatal("expected enabled=true by default")
	}
	if payload.Hook.Kind != hooks.KindHTTP {
		t.Fatalf("kind = %q, want http", payload.Hook.Kind)
	}
}

func TestHooksCreateKeepsAuthContextCompatible(t *testing.T) {
	t.Parallel()

	_, surface := newTestHookSurface(t)
	req := httptest.NewRequest(http.MethodPost, "/operator/hooks", bytes.NewBufferString(`{"name":"route-hook","trigger":"run.completed","kind":"command","command":"true"}`)).
		WithContext(scopedAuthContext("actor-a"))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	http.HandlerFunc(surface.handleHooksCreate).ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create scoped hook: status = %d body=%s", rec.Code, rec.Body.String())
	}

	var payload hookResponse
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if payload.Hook.AutomationID != "" {
		t.Fatalf("hook scope = %+v", payload.Hook)
	}
}

func TestHooksCreateHTTPHookWithHeadersNormalizesValues(t *testing.T) {
	t.Parallel()

	gw := newTestHookGateway(t)
	handler := gw.Handler()

	body := `{"name":"signed-hook","trigger":"run.completed","kind":"http","url":"http://example.com/hook","headers":{" authorization ":" Bearer demo-token ","x-route-key":" prod "}}`
	rec := doRequest(t, handler, http.MethodPost, "/operator/hooks", body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create with headers: status = %d body=%s", rec.Code, rec.Body.String())
	}

	var payload hookResponse
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if got := payload.Hook.Headers["Authorization"]; got != "Bearer demo-token" {
		t.Fatalf("Authorization = %q, want %q", got, "Bearer demo-token")
	}
	if got := payload.Hook.Headers["X-Route-Key"]; got != "prod" {
		t.Fatalf("X-Route-Key = %q, want %q", got, "prod")
	}
}

func TestHooksCreateCommandHookSuccess(t *testing.T) {
	t.Parallel()

	gw := newTestHookGateway(t)
	handler := gw.Handler()

	body := `{"name":"cmd-hook","trigger":"tool.executed","kind":"command","command":"echo hi"}`
	rec := doRequest(t, handler, http.MethodPost, "/operator/hooks", body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create command: status = %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestHooksCreateRejectsAsyncPreHook(t *testing.T) {
	t.Parallel()

	gw := newTestHookGateway(t)
	handler := gw.Handler()

	rec := doRequest(t, handler, http.MethodPost, "/operator/hooks",
		`{"name":"bad","trigger":"before.tool_call","kind":"command","command":"echo hi","phase":"pre","async":true}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("async pre: status = %d, want %d body=%s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
}

func TestHooksCreateMissingTrigger(t *testing.T) {
	t.Parallel()

	gw := newTestHookGateway(t)
	handler := gw.Handler()

	rec := doRequest(t, handler, http.MethodPost, "/operator/hooks",
		`{"kind":"http","url":"http://example.com"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("missing trigger: status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestHooksCreateMissingKind(t *testing.T) {
	t.Parallel()

	gw := newTestHookGateway(t)
	handler := gw.Handler()

	rec := doRequest(t, handler, http.MethodPost, "/operator/hooks",
		`{"trigger":"run.completed","url":"http://example.com"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("missing kind: status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestHooksCreateHTTPMissingURL(t *testing.T) {
	t.Parallel()

	gw := newTestHookGateway(t)
	handler := gw.Handler()

	rec := doRequest(t, handler, http.MethodPost, "/operator/hooks",
		`{"trigger":"run.completed","kind":"http"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("http missing url: status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestHooksCreateCommandMissingCommand(t *testing.T) {
	t.Parallel()

	gw := newTestHookGateway(t)
	handler := gw.Handler()

	rec := doRequest(t, handler, http.MethodPost, "/operator/hooks",
		`{"trigger":"run.completed","kind":"command"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("command missing command: status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestHooksCreateInvalidJSON(t *testing.T) {
	t.Parallel()

	gw := newTestHookGateway(t)
	handler := gw.Handler()

	rec := doRequest(t, handler, http.MethodPost, "/operator/hooks", "not-json")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid json: status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestHooksCreateRejectsTrailingJSON(t *testing.T) {
	t.Parallel()

	gw := newTestHookGateway(t)
	handler := gw.Handler()

	rec := doRequest(t, handler, http.MethodPost, "/operator/hooks", `{"trigger":"run.completed","kind":"http","url":"http://example.com"} {"extra":true}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("trailing json: status = %d body=%s", rec.Code, rec.Body.String())
	}

	items, err := gw.hooks.Store().List(context.Background())
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(items) != 0 {
		t.Fatalf("items = %#v, want empty", items)
	}
}

func TestHooksCreateRejectsUnknownField(t *testing.T) {
	t.Parallel()

	gw := newTestHookGateway(t)
	handler := gw.Handler()

	rec := doRequest(t, handler, http.MethodPost, "/operator/hooks",
		`{"trigger":"run.completed","kind":"http","url":"http://example.com","prompt":"legacy"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("unknown field: status = %d, want %d body=%s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
}

// ---------------------------------------------------------------------------
// handleHooksUpdate
// ---------------------------------------------------------------------------

func TestHooksUpdateSuccess(t *testing.T) {
	t.Parallel()

	gw := newTestHookGateway(t)
	handler := gw.Handler()

	// Create a hook first.
	createRec := doRequest(t, handler, http.MethodPost, "/operator/hooks",
		`{"name":"original","trigger":"run.completed","kind":"http","url":"http://example.com"}`)
	var created hookResponse
	if err := json.NewDecoder(createRec.Body).Decode(&created); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}

	// Patch the name.
	rec := doRequest(t, handler, http.MethodPatch, "/operator/hooks/"+created.Hook.ID,
		`{"name":"patched"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("update: status = %d body=%s", rec.Code, rec.Body.String())
	}

	var updated hookResponse
	if err := json.NewDecoder(rec.Body).Decode(&updated); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if updated.Hook.Name != "patched" {
		t.Fatalf("name = %q, want patched", updated.Hook.Name)
	}
}

func TestHooksUpdateHeadersCanReplaceAndClear(t *testing.T) {
	t.Parallel()

	gw := newTestHookGateway(t)
	handler := gw.Handler()

	createRec := doRequest(t, handler, http.MethodPost, "/operator/hooks",
		`{"name":"headered","trigger":"run.completed","kind":"http","url":"http://example.com","headers":{"X-Old":"legacy"}}`)
	var created hookResponse
	if err := json.NewDecoder(createRec.Body).Decode(&created); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}

	rec := doRequest(t, handler, http.MethodPatch, "/operator/hooks/"+created.Hook.ID,
		`{"headers":{" authorization ":"Bearer rotated","X-Trace-ID":" trace-123 "}}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("update headers: status = %d body=%s", rec.Code, rec.Body.String())
	}

	var updated hookResponse
	if err := json.NewDecoder(rec.Body).Decode(&updated); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if got := updated.Hook.Headers["Authorization"]; got != "Bearer rotated" {
		t.Fatalf("Authorization = %q, want %q", got, "Bearer rotated")
	}
	if got := updated.Hook.Headers["X-Trace-Id"]; got != "trace-123" {
		t.Fatalf("X-Trace-Id = %q, want %q", got, "trace-123")
	}
	if _, exists := updated.Hook.Headers["X-Old"]; exists {
		t.Fatal("expected old headers to be replaced")
	}

	rec = doRequest(t, handler, http.MethodPatch, "/operator/hooks/"+created.Hook.ID, `{"headers":{}}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("clear headers: status = %d body=%s", rec.Code, rec.Body.String())
	}
	updated = hookResponse{}
	if err := json.NewDecoder(rec.Body).Decode(&updated); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if len(updated.Hook.Headers) != 0 {
		t.Fatalf("headers length = %d, want 0", len(updated.Hook.Headers))
	}
}

func TestHooksUpdateNotFound(t *testing.T) {
	t.Parallel()

	gw := newTestHookGateway(t)
	handler := gw.Handler()

	rec := doRequest(t, handler, http.MethodPatch, "/operator/hooks/nonexistent",
		`{"name":"nope"}`)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("not found: status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

func TestHooksUpdateInvalidJSON(t *testing.T) {
	t.Parallel()

	gw := newTestHookGateway(t)
	handler := gw.Handler()

	// Create a hook first so the ID exists.
	createRec := doRequest(t, handler, http.MethodPost, "/operator/hooks",
		`{"name":"for-bad-patch","trigger":"run.completed","kind":"http","url":"http://example.com"}`)
	var created hookResponse
	if err := json.NewDecoder(createRec.Body).Decode(&created); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}

	rec := doRequest(t, handler, http.MethodPatch, "/operator/hooks/"+created.Hook.ID, "not-json")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid json: status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestHooksUpdateRejectsTrailingJSON(t *testing.T) {
	t.Parallel()

	gw := newTestHookGateway(t)
	handler := gw.Handler()

	createRec := doRequest(t, handler, http.MethodPost, "/operator/hooks",
		`{"name":"for-trailing","trigger":"run.completed","kind":"http","url":"http://example.com"}`)
	var created hookResponse
	if err := json.NewDecoder(createRec.Body).Decode(&created); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}

	rec := doRequest(t, handler, http.MethodPatch, "/operator/hooks/"+created.Hook.ID, `{"name":"patched"} {"extra":true}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("trailing json: status = %d body=%s", rec.Code, rec.Body.String())
	}

	current, err := gw.hooks.Store().Get(context.Background(), created.Hook.ID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if current.Name != "for-trailing" {
		t.Fatalf("name = %q, want for-trailing", current.Name)
	}
}

func TestHooksUpdateRejectsUnknownField(t *testing.T) {
	t.Parallel()

	gw := newTestHookGateway(t)
	handler := gw.Handler()

	createRec := doRequest(t, handler, http.MethodPost, "/operator/hooks",
		`{"name":"strict","trigger":"run.completed","kind":"http","url":"http://example.com"}`)
	var created hookResponse
	if err := json.NewDecoder(createRec.Body).Decode(&created); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}

	rec := doRequest(t, handler, http.MethodPatch, "/operator/hooks/"+created.Hook.ID,
		`{"agent":"legacy"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("unknown field: status = %d, want %d body=%s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
}

func TestHooksUpdateRejectsInvalidTargetCombination(t *testing.T) {
	t.Parallel()

	gw := newTestHookGateway(t)
	handler := gw.Handler()

	createRec := doRequest(t, handler, http.MethodPost, "/operator/hooks",
		`{"name":"switch-kind","trigger":"run.completed","kind":"command","command":"echo hi"}`)
	var created hookResponse
	if err := json.NewDecoder(createRec.Body).Decode(&created); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}

	rec := doRequest(t, handler, http.MethodPatch, "/operator/hooks/"+created.Hook.ID,
		`{"kind":"http"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid target combination: status = %d, want %d body=%s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
}

func TestHooksUpdateRejectsUnsupportedPhase(t *testing.T) {
	t.Parallel()

	gw := newTestHookGateway(t)
	handler := gw.Handler()

	createRec := doRequest(t, handler, http.MethodPost, "/operator/hooks",
		`{"name":"phase-check","trigger":"run.completed","kind":"http","url":"http://example.com"}`)
	var created hookResponse
	if err := json.NewDecoder(createRec.Body).Decode(&created); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}

	rec := doRequest(t, handler, http.MethodPatch, "/operator/hooks/"+created.Hook.ID,
		`{"phase":"during"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid phase: status = %d, want %d body=%s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
}

// ---------------------------------------------------------------------------
// handleHooksDelete
// ---------------------------------------------------------------------------

func TestHooksDeleteSuccess(t *testing.T) {
	t.Parallel()

	gw := newTestHookGateway(t)
	handler := gw.Handler()

	// Create then delete.
	createRec := doRequest(t, handler, http.MethodPost, "/operator/hooks",
		`{"name":"delete-me","trigger":"run.completed","kind":"http","url":"http://example.com"}`)
	var created hookResponse
	if err := json.NewDecoder(createRec.Body).Decode(&created); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}

	rec := doRequest(t, handler, http.MethodDelete, "/operator/hooks/"+created.Hook.ID, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("delete: status = %d body=%s", rec.Code, rec.Body.String())
	}

	var payload map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if payload["ok"] != true {
		t.Fatalf("ok = %v, want true", payload["ok"])
	}
}

func TestHooksDeleteNotFound(t *testing.T) {
	t.Parallel()

	gw := newTestHookGateway(t)
	handler := gw.Handler()

	rec := doRequest(t, handler, http.MethodDelete, "/operator/hooks/nonexistent", "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("not found: status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

// ---------------------------------------------------------------------------
// handleHooksResults
// ---------------------------------------------------------------------------

func TestHooksResultsNilExecutor(t *testing.T) {
	t.Parallel()

	gw := newTestGatewayFull(t)
	handler := gw.Handler()
	rec := doRequest(t, handler, http.MethodGet, "/operator/hooks/abc/results", "")
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("nil executor: status = %d", rec.Code)
	}
}

func TestHooksResultsNotFound(t *testing.T) {
	t.Parallel()

	gw := newTestHookGateway(t)
	handler := gw.Handler()

	rec := doRequest(t, handler, http.MethodGet, "/operator/hooks/nonexistent/results", "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("not found: status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

func TestHooksResultsEmpty(t *testing.T) {
	t.Parallel()

	gw := newTestHookGateway(t)
	handler := gw.Handler()

	// Create a hook.
	createRec := doRequest(t, handler, http.MethodPost, "/operator/hooks",
		`{"name":"results-hook","trigger":"run.completed","kind":"http","url":"http://example.com"}`)
	var created hookResponse
	if err := json.NewDecoder(createRec.Body).Decode(&created); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}

	rec := doRequest(t, handler, http.MethodGet, "/operator/hooks/"+created.Hook.ID+"/results", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("results: status = %d body=%s", rec.Code, rec.Body.String())
	}

	var payload hookResultsResponse
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if payload.Count != 0 {
		t.Fatalf("count = %d, want 0", payload.Count)
	}
}

func TestHooksResultsApplyAuthenticatedScope(t *testing.T) {
	t.Parallel()

	gw, sessions, runs := newTestAutomationGatewayWithRuntime(t)
	surface := newOperatorHookSurface(hookOperatorDepsFromGateway(gw))
	ctx := context.Background()

	hook, err := gw.hooks.Store().Add(ctx, hooks.Hook{
		Name:    "results-hook",
		Enabled: true,
		Trigger: hooks.TriggerRunCompleted,
		Kind:    hooks.KindCommand,
		Command: "true",
	})
	if err != nil {
		t.Fatalf("hook add: %v", err)
	}

	sessionA, err := sessions.GetOrCreate(ctx, "scope:a", "test-model")
	if err != nil {
		t.Fatalf("GetOrCreate(sessionA) error = %v", err)
	}
	runA, err := runs.Create(ctx, sessionA.ID, agent.IncomingMessage{
		SessionID:  sessionA.ID,
		SessionKey: sessionA.Key,
		Content:    "run a",
		Model:      "test-model",
	}, agent.AgentConfig{DefaultModel: "test-model"})
	if err != nil {
		t.Fatalf("runs.Create(runA) error = %v", err)
	}

	sessionB, err := sessions.GetOrCreate(ctx, "scope:b", "test-model")
	if err != nil {
		t.Fatalf("GetOrCreate(sessionB) error = %v", err)
	}
	runB, err := runs.Create(ctx, sessionB.ID, agent.IncomingMessage{
		SessionID:  sessionB.ID,
		SessionKey: sessionB.Key,
		Content:    "run b",
		Model:      "test-model",
	}, agent.AgentConfig{DefaultModel: "test-model"})
	if err != nil {
		t.Fatalf("runs.Create(runB) error = %v", err)
	}

	if _, err := gw.hooks.FireHook(ctx, hook.ID, "", "", map[string]any{"run_id": runA.ID}); err != nil {
		t.Fatalf("FireHook(runA) error = %v", err)
	}
	if _, err := gw.hooks.FireHook(ctx, hook.ID, "", "", map[string]any{"run_id": runB.ID}); err != nil {
		t.Fatalf("FireHook(runB) error = %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/operator/hooks/"+hook.ID+"/results", nil).
		WithContext(scopedAuthContext("actor-a"))
	req.SetPathValue("id", hook.ID)
	rec := httptest.NewRecorder()
	http.HandlerFunc(surface.handleHooksResults).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("results scoped: status = %d body=%s", rec.Code, rec.Body.String())
	}

	var payload hookResultsResponse
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if payload.Count != 2 {
		t.Fatalf("payload = %+v", payload)
	}
}

func TestHooksFireReturnsResult(t *testing.T) {
	t.Parallel()

	gw := newTestHookGateway(t)
	handler := gw.Handler()

	createRec := doRequest(t, handler, http.MethodPost, "/operator/hooks",
		`{"name":"fire-hook","trigger":"run.completed","kind":"command","command":"echo hook-fired"}`)
	var created hookResponse
	if err := json.NewDecoder(createRec.Body).Decode(&created); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}

	rec := doRequest(t, handler, http.MethodPost, "/operator/hooks/"+created.Hook.ID+"/fire",
		`{"payload":{"run_id":"run-test-1","session_id":"sess-test-1","tool_name":"notify.send"}}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("fire: status = %d body=%s", rec.Code, rec.Body.String())
	}

	var payload hookFireResponse
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if payload.Result.RunID != "run-test-1" {
		t.Fatalf("run_id = %q, want run-test-1", payload.Result.RunID)
	}
	if payload.Result.ToolName != "notify.send" {
		t.Fatalf("tool_name = %q, want notify.send", payload.Result.ToolName)
	}
	if payload.Result.Status == "" {
		t.Fatal("expected status")
	}
}

func TestHooksResultsFilterByAuthenticatedAutomationScope(t *testing.T) {
	t.Parallel()

	gw := newGatewayWithBuiltins(t, t.TempDir())
	executor := hooks.NewExecutor(hooks.NewInMemoryStore())
	gw.SetHooks(executor)
	surface := newOperatorHookSurface(hookOperatorDepsFromGateway(gw))
	hook, err := gw.hooks.Store().Add(context.Background(), hooks.Hook{
		Name:         "results-hook",
		Enabled:      true,
		Trigger:      hooks.TriggerRunCompleted,
		Kind:         hooks.KindCommand,
		Command:      "true",
		AutomationID: "automation-a",
	})
	if err != nil {
		t.Fatalf("Add() error = %v", err)
	}

	ctx := context.Background()
	execute := false
	runA, err := gw.runtime.Submit(ctx, runtimepkg.SubmitRequest{
		SessionKey:   "scope:auto-a",
		Content:      "run a",
		Model:        "test-model",
		AutomationID: "automation-a",
		Execute:      &execute,
	})
	if err != nil {
		t.Fatalf("Submit(runA) error = %v", err)
	}
	runB, err := gw.runtime.Submit(ctx, runtimepkg.SubmitRequest{
		SessionKey:   "scope:auto-b",
		Content:      "run b",
		Model:        "test-model",
		AutomationID: "automation-b",
		Execute:      &execute,
	})
	if err != nil {
		t.Fatalf("Submit(runB) error = %v", err)
	}

	if _, err := gw.hooks.FireHook(ctx, hook.ID, "", "", map[string]any{"run_id": runA.ID}); err != nil {
		t.Fatalf("FireHook(runA) error = %v", err)
	}
	if _, err := gw.hooks.FireHook(ctx, hook.ID, "", "", map[string]any{"run_id": runB.ID}); err != nil {
		t.Fatalf("FireHook(runB) error = %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/operator/hooks/"+hook.ID+"/results", nil).
		WithContext(scopedAutomationAuthContext("actor-a", "automation-a"))
	req.SetPathValue("id", hook.ID)
	rec := httptest.NewRecorder()
	http.HandlerFunc(surface.handleHooksResults).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("results scoped: status = %d body=%s", rec.Code, rec.Body.String())
	}

	var payload hookResultsResponse
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if payload.Count != 1 || len(payload.Items) != 1 {
		t.Fatalf("payload = %+v", payload)
	}
	if payload.Items[0].RunID != runA.ID {
		t.Fatalf("payload.Items[0].RunID = %q, want %q", payload.Items[0].RunID, runA.ID)
	}
}

func TestHooksFireRejectsTrailingJSON(t *testing.T) {
	t.Parallel()

	gw := newTestHookGateway(t)
	handler := gw.Handler()

	createRec := doRequest(t, handler, http.MethodPost, "/operator/hooks",
		`{"name":"fire-hook","trigger":"run.completed","kind":"command","command":"echo hook-fired"}`)
	var created hookResponse
	if err := json.NewDecoder(createRec.Body).Decode(&created); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}

	rec := doRequest(t, handler, http.MethodPost, "/operator/hooks/"+created.Hook.ID+"/fire",
		`{"payload":{"run_id":"run-test-1"}} {"extra":true}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("fire trailing json: status = %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestHooksFireRejectsUnknownField(t *testing.T) {
	t.Parallel()

	gw := newTestHookGateway(t)
	handler := gw.Handler()

	createRec := doRequest(t, handler, http.MethodPost, "/operator/hooks",
		`{"name":"fire-hook","trigger":"run.completed","kind":"command","command":"echo hook-fired"}`)
	var created hookResponse
	if err := json.NewDecoder(createRec.Body).Decode(&created); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}

	rec := doRequest(t, handler, http.MethodPost, "/operator/hooks/"+created.Hook.ID+"/fire",
		`{"payload":{"run_id":"run-test-1"},"agent":"legacy"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("fire unknown field: status = %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestHooksFireRejectsAuthenticatedScopeEscape(t *testing.T) {
	t.Parallel()

	gw, surface := newTestHookSurface(t)
	hook, err := gw.hooks.Store().Add(context.Background(), hooks.Hook{
		Name:    "fire-hook",
		Enabled: true,
		Trigger: hooks.TriggerRunCompleted,
		Kind:    hooks.KindCommand,
		Command: "true",
	})
	if err != nil {
		t.Fatalf("hook add: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/operator/hooks/"+hook.ID+"/fire", bytes.NewBufferString(`{"payload":{"run_id":"run-test"}}`)).
		WithContext(scopedAuthContext("actor-a"))
	req.Header.Set("Content-Type", "application/json")
	req.SetPathValue("id", hook.ID)
	rec := httptest.NewRecorder()
	http.HandlerFunc(surface.handleHooksFire).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("fire scope escape: status = %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestHooksReplayReusesLatestPayload(t *testing.T) {
	t.Parallel()

	gw := newTestHookGateway(t)
	handler := gw.Handler()

	createRec := doRequest(t, handler, http.MethodPost, "/operator/hooks",
		`{"name":"replay-hook","trigger":"run.completed","kind":"command","command":"echo hook-fired"}`)
	var created hookResponse
	if err := json.NewDecoder(createRec.Body).Decode(&created); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}

	fireRec := doRequest(t, handler, http.MethodPost, "/operator/hooks/"+created.Hook.ID+"/fire",
		`{"payload":{"run_id":"run-replay-1","session_id":"sess-replay-1","tool_name":"notify.send"}}`)
	if fireRec.Code != http.StatusOK {
		t.Fatalf("fire: status = %d body=%s", fireRec.Code, fireRec.Body.String())
	}

	rec := doRequest(t, handler, http.MethodPost, "/operator/hooks/"+created.Hook.ID+"/replay", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("replay: status = %d body=%s", rec.Code, rec.Body.String())
	}

	var payload hookFireResponse
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if payload.Result.RunID != "run-replay-1" {
		t.Fatalf("run_id = %q, want run-replay-1", payload.Result.RunID)
	}
	if payload.Result.PayloadPreview["tool_name"] != "notify.send" {
		t.Fatalf("payload_preview = %#v", payload.Result.PayloadPreview)
	}
}

func TestHooksReplayWithoutRecentPayloadFails(t *testing.T) {
	t.Parallel()

	gw := newTestHookGateway(t)
	handler := gw.Handler()

	createRec := doRequest(t, handler, http.MethodPost, "/operator/hooks",
		`{"name":"replay-miss","trigger":"run.completed","kind":"command","command":"echo hook-fired"}`)
	var created hookResponse
	if err := json.NewDecoder(createRec.Body).Decode(&created); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}

	rec := doRequest(t, handler, http.MethodPost, "/operator/hooks/"+created.Hook.ID+"/replay", "")
	if rec.Code != http.StatusConflict {
		t.Fatalf("replay miss: status = %d body=%s", rec.Code, rec.Body.String())
	}
}

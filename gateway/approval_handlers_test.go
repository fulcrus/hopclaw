package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/fulcrus/hopclaw/approval"
	controlapproval "github.com/fulcrus/hopclaw/internal/controlplane/approvalflow"
	runtimepkg "github.com/fulcrus/hopclaw/runtime"
)

// ---------------------------------------------------------------------------
// handleApprovalsList
// ---------------------------------------------------------------------------

func TestApprovalsListNilStore(t *testing.T) {
	t.Parallel()

	gw := newTestGatewayFull(t)
	// approvals not set — should return 503.
	handler := gw.Handler()
	rec := doRequest(t, handler, http.MethodGet, "/operator/approvals", "")
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("nil store: status = %d, want %d", rec.Code, http.StatusServiceUnavailable)
	}
}

func TestApprovalsListEmpty(t *testing.T) {
	t.Parallel()

	gw := newTestGatewayFull(t)
	gw.SetApprovals(approval.NewInMemoryStore())
	handler := gw.Handler()

	rec := doRequest(t, handler, http.MethodGet, "/operator/approvals", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("empty list: status = %d body=%s", rec.Code, rec.Body.String())
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

func TestApprovalsListWithPendingTicket(t *testing.T) {
	t.Parallel()

	store := approval.NewInMemoryStore()
	_, err := store.Create(context.Background(), approval.Ticket{
		RunID:     "run-1",
		SessionID: "sess-1",
		ToolCalls: []approval.ToolCall{{ID: "tc-1", Name: "exec.run"}},
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	gw := newTestGatewayFull(t)
	gw.SetApprovals(store)
	handler := gw.Handler()

	rec := doRequest(t, handler, http.MethodGet, "/operator/approvals", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("list: status = %d body=%s", rec.Code, rec.Body.String())
	}

	var payload struct {
		Items []runtimepkg.ApprovalView `json:"items"`
		Count int                       `json:"count"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if payload.Count != 1 {
		t.Fatalf("count = %d, want 1", payload.Count)
	}
	if len(payload.Items) != 1 || len(payload.Items[0].ToolCalls) != 1 || payload.Items[0].ToolCalls[0].Name != "exec.run" {
		t.Fatalf("payload.Items = %#v", payload.Items)
	}
}

func TestApprovalsListSupportsPagination(t *testing.T) {
	t.Parallel()

	store := approval.NewInMemoryStore()
	for _, runID := range []string{"run-1", "run-2", "run-3"} {
		if _, err := store.Create(context.Background(), approval.Ticket{
			RunID:     runID,
			SessionID: "sess-" + runID,
		}); err != nil {
			t.Fatalf("Create(%s) error = %v", runID, err)
		}
	}

	gw := newTestGatewayFull(t)
	gw.SetApprovals(store)
	handler := gw.Handler()

	rec := doRequest(t, handler, http.MethodGet, "/operator/approvals?limit=1&offset=1", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("list: status = %d body=%s", rec.Code, rec.Body.String())
	}

	var payload struct {
		Items []runtimepkg.ApprovalView `json:"items"`
		Count int                       `json:"count"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if payload.Count != 1 || len(payload.Items) != 1 {
		t.Fatalf("payload = %+v", payload)
	}
	if payload.Items[0].RunID != "run-2" {
		t.Fatalf("payload.Items[0].RunID = %q, want run-2", payload.Items[0].RunID)
	}
}

func TestApprovalsListReturnsApprovalViewGovernance(t *testing.T) {
	t.Parallel()

	store := approval.NewInMemoryStore()
	_, err := store.Create(context.Background(), approval.Ticket{
		RunID:     "run-governance",
		SessionID: "sess-governance",
		ToolCalls: []approval.ToolCall{{ID: "tc-gov", Name: "deploy-prod-shell"}},
		Metadata: map[string]any{
			"scope":                        map[string]any{"automation_id": "automation-governance"},
			"policy_source":                "runtime.release_readiness_gate",
			"policy_summary":               "release readiness blocked high-risk execution",
			"policy_reasons":               []string{"release readiness blocked high-risk execution"},
			"policy_tool_names":            []string{"deploy-prod-shell"},
			"effective_config_snapshot_id": "ecs-gov-1",
		},
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	gw := newTestGatewayFull(t)
	gw.SetApprovals(store)
	handler := gw.Handler()

	rec := doRequest(t, handler, http.MethodGet, "/operator/approvals", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("list governance: status = %d body=%s", rec.Code, rec.Body.String())
	}

	var payload struct {
		Items []runtimepkg.ApprovalView `json:"items"`
		Count int                       `json:"count"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if payload.Count != 1 || len(payload.Items) != 1 {
		t.Fatalf("payload = %+v", payload)
	}
	if payload.Items[0].Governance == nil || payload.Items[0].Governance.Policy == nil {
		t.Fatalf("governance = %#v", payload.Items[0].Governance)
	}
	if payload.Items[0].Governance.Policy.PolicySource != "runtime.release_readiness_gate" {
		t.Fatalf("PolicySource = %q", payload.Items[0].Governance.Policy.PolicySource)
	}
	if payload.Items[0].Governance.EffectiveConfigSnapshotID != "ecs-gov-1" {
		t.Fatalf("EffectiveConfigSnapshotID = %q", payload.Items[0].Governance.EffectiveConfigSnapshotID)
	}
	if len(payload.Items[0].Governance.ToolNames) != 1 || payload.Items[0].Governance.ToolNames[0] != "deploy-prod-shell" {
		t.Fatalf("ToolNames = %#v", payload.Items[0].Governance.ToolNames)
	}
}

func TestApprovalsListFilterByStatus(t *testing.T) {
	t.Parallel()

	store := approval.NewInMemoryStore()
	ticket, _ := store.Create(context.Background(), approval.Ticket{
		RunID:     "run-1",
		SessionID: "sess-1",
		ToolCalls: []approval.ToolCall{{ID: "tc-1", Name: "exec.run"}},
		Metadata: map[string]any{
			"policy_approval_max_scope": "session",
		},
	})
	// Resolve the ticket so it is no longer pending.
	_, _ = store.Resolve(context.Background(), ticket.ID, approval.Resolution{
		Status: approval.StatusApproved,
	})

	gw := newTestGatewayFull(t)
	gw.SetApprovals(store)
	handler := gw.Handler()

	// Query for approved tickets.
	rec := doRequest(t, handler, http.MethodGet, "/operator/approvals?status=approved", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("filter approved: status = %d body=%s", rec.Code, rec.Body.String())
	}

	var payload struct {
		Items []runtimepkg.ApprovalView `json:"items"`
		Count int                       `json:"count"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if payload.Count != 1 {
		t.Fatalf("count = %d, want 1", payload.Count)
	}
}

func TestApprovalsListFilterByScope(t *testing.T) {
	t.Parallel()

	store := approval.NewInMemoryStore()
	_, _ = store.Create(context.Background(), approval.Ticket{
		RunID:     "run-alpha",
		SessionID: "sess-alpha",
		ToolCalls: []approval.ToolCall{{ID: "tc-1", Name: "exec.run"}},
		Metadata: map[string]any{
			"scope": map[string]any{
				"automation_id": "automation-alpha",
			},
		},
	})
	_, _ = store.Create(context.Background(), approval.Ticket{
		RunID:     "run-beta",
		SessionID: "sess-beta",
		ToolCalls: []approval.ToolCall{{ID: "tc-2", Name: "exec.run"}},
		Metadata: map[string]any{
			"scope": map[string]any{
				"automation_id": "automation-beta",
			},
		},
	})

	gw := newTestGatewayFull(t)
	gw.SetApprovals(store)
	handler := gw.Handler()

	rec := doRequest(t, handler, http.MethodGet, "/operator/approvals", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("filter scope: status = %d body=%s", rec.Code, rec.Body.String())
	}

	var payload struct {
		Items []runtimepkg.ApprovalView `json:"items"`
		Count int                       `json:"count"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if payload.Count != 2 {
		t.Fatalf("payload = %+v", payload)
	}
}

func TestApprovalsListKeepsItemsWithAuthContext(t *testing.T) {
	t.Parallel()

	store := approval.NewInMemoryStore()
	_, _ = store.Create(context.Background(), approval.Ticket{
		RunID:     "run-alpha",
		SessionID: "sess-alpha",
		Metadata: map[string]any{
			"scope": map[string]any{
				"automation_id": "automation-alpha",
			},
		},
	})
	_, _ = store.Create(context.Background(), approval.Ticket{
		RunID:     "run-beta",
		SessionID: "sess-beta",
		Metadata: map[string]any{
			"scope": map[string]any{
				"automation_id": "automation-beta",
			},
		},
	})

	gw := newTestGatewayFull(t)
	gw.SetApprovals(store)

	req := httptest.NewRequest(http.MethodGet, "/operator/approvals", nil).
		WithContext(scopedAuthContext("actor-a"))
	rec := httptest.NewRecorder()
	http.HandlerFunc(gw.handleApprovalsList).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list scoped: status = %d body=%s", rec.Code, rec.Body.String())
	}

	var payload struct {
		Items []runtimepkg.ApprovalView `json:"items"`
		Count int                       `json:"count"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if payload.Count != 2 {
		t.Fatalf("payload = %+v", payload)
	}
}

func TestApprovalsListAcceptsAuthContextWithoutScopeFiltering(t *testing.T) {
	t.Parallel()

	store := approval.NewInMemoryStore()
	gw := newTestGatewayFull(t)
	gw.SetApprovals(store)

	req := httptest.NewRequest(http.MethodGet, "/operator/approvals", nil).
		WithContext(scopedAuthContext("actor-a"))
	rec := httptest.NewRecorder()
	http.HandlerFunc(gw.handleApprovalsList).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list scope escape: status = %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestApprovalsListFiltersByAuthenticatedAutomationScope(t *testing.T) {
	t.Parallel()

	store := approval.NewInMemoryStore()
	_, _ = store.Create(context.Background(), approval.Ticket{
		RunID:     "run-alpha",
		SessionID: "sess-alpha",
		Metadata: map[string]any{
			"scope": map[string]any{
				"automation_id": "automation-alpha",
			},
		},
	})
	_, _ = store.Create(context.Background(), approval.Ticket{
		RunID:     "run-beta",
		SessionID: "sess-beta",
		Metadata: map[string]any{
			"scope": map[string]any{
				"automation_id": "automation-beta",
			},
		},
	})

	gw := newTestGatewayFull(t)
	gw.SetApprovals(store)

	req := httptest.NewRequest(http.MethodGet, "/operator/approvals", nil).
		WithContext(scopedAutomationAuthContext("actor-a", "automation-alpha"))
	rec := httptest.NewRecorder()
	http.HandlerFunc(gw.handleApprovalsList).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list scoped: status = %d body=%s", rec.Code, rec.Body.String())
	}

	var payload struct {
		Items []runtimepkg.ApprovalView `json:"items"`
		Count int                       `json:"count"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if payload.Count != 1 || len(payload.Items) != 1 {
		t.Fatalf("payload = %+v", payload)
	}
	if payload.Items[0].RunID != "run-alpha" {
		t.Fatalf("payload.Items[0].RunID = %q, want run-alpha", payload.Items[0].RunID)
	}
}

func TestApprovalProvidersListEmpty(t *testing.T) {
	t.Parallel()

	gw := newTestGatewayFull(t)
	handler := gw.Handler()

	rec := doRequest(t, handler, http.MethodGet, "/operator/approvals/providers", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("providers empty: status = %d body=%s", rec.Code, rec.Body.String())
	}

	var payload approvalProvidersResponse
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if payload.Count != 0 {
		t.Fatalf("count = %d, want 0", payload.Count)
	}
}

func TestApprovalProvidersListSanitizedSummary(t *testing.T) {
	t.Parallel()

	provider, err := controlapproval.NewWebhookProvider(controlapproval.WebhookProviderConfig{
		Name:      "corp-approval",
		SubmitURL: "https://approval.example.com/submit",
		UpdateURL: "https://approval.example.com/update",
		SyncURL:   "https://approval.example.com/sync",
	})
	if err != nil {
		t.Fatalf("NewWebhookProvider() error = %v", err)
	}
	registry := controlapproval.NewProviderRegistry([]controlapproval.ProviderDescriptor{
		{
			Name:          "corp-approval",
			Type:          "webhook",
			Enabled:       true,
			SubmitEnabled: true,
			UpdateEnabled: true,
			SyncEnabled:   true,
			CallbackAuth: controlapproval.CallbackAuthPolicy{
				Mode:            "hmac",
				HeaderName:      "X-HopClaw-Approval-Token",
				Secret:          "super-secret",
				SignatureHeader: "X-HopClaw-Signature",
				TimestampHeader: "X-HopClaw-Timestamp",
			},
			Metadata: map[string]any{
				"owner": "platform-security",
			},
		},
	}, provider)

	gw := newTestGatewayFull(t)
	gw.SetApprovalProviderRegistry(registry)
	handler := gw.Handler()

	rec := doRequest(t, handler, http.MethodGet, "/operator/approvals/providers", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("providers summary: status = %d body=%s", rec.Code, rec.Body.String())
	}

	var payload approvalProvidersResponse
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if payload.Count != 1 {
		t.Fatalf("count = %d, want 1", payload.Count)
	}
	item := payload.Items[0]
	if item.Name != "corp-approval" || item.Type != "webhook" {
		t.Fatalf("item = %+v", item)
	}
	if !item.Registered || !item.SubmitEnabled || !item.UpdateEnabled || !item.SyncEnabled {
		t.Fatalf("capabilities = %+v", item)
	}
	if !item.CallbackAuth.Protected || item.CallbackAuth.Mode != "hmac" {
		t.Fatalf("callback_auth = %+v", item.CallbackAuth)
	}
}

// ---------------------------------------------------------------------------
// handleApprovalsResolve
// ---------------------------------------------------------------------------

func TestApprovalsResolveNilStore(t *testing.T) {
	t.Parallel()

	gw := newTestGatewayFull(t)
	handler := gw.Handler()
	rec := doRequest(t, handler, http.MethodPost, "/operator/approvals/abc/resolve", `{"status":"approved"}`)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("nil store: status = %d", rec.Code)
	}
}

func TestApprovalsResolveApproved(t *testing.T) {
	t.Parallel()

	store := approval.NewInMemoryStore()
	ticket, _ := store.Create(context.Background(), approval.Ticket{
		RunID:     "run-1",
		SessionID: "sess-1",
		ToolCalls: []approval.ToolCall{{ID: "tc-1", Name: "exec.run"}},
		Metadata: map[string]any{
			"policy_approval_max_scope": "session",
		},
	})

	gw := newTestGatewayFull(t)
	gw.SetApprovals(store)
	gw.SetGrantStore(approval.NewGrantStore())
	handler := gw.Handler()

	body := `{"status":"approved","by":"operator","note":"lgtm","scope":"session"}`
	rec := doRequest(t, handler, http.MethodPost, "/operator/approvals/"+ticket.ID+"/resolve", body)
	if rec.Code != http.StatusOK {
		t.Fatalf("resolve approved: status = %d body=%s", rec.Code, rec.Body.String())
	}

	var payload struct {
		OK     bool            `json:"ok"`
		Ticket runtimepkg.ApprovalView `json:"ticket"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if !payload.OK {
		t.Fatal("expected ok=true")
	}
	if payload.Ticket.Status != approval.StatusApproved {
		t.Fatalf("ticket status = %q, want approved", payload.Ticket.Status)
	}
}

func TestApprovalsResolveDenied(t *testing.T) {
	t.Parallel()

	store := approval.NewInMemoryStore()
	ticket, _ := store.Create(context.Background(), approval.Ticket{
		RunID:     "run-2",
		SessionID: "sess-2",
		ToolCalls: []approval.ToolCall{{ID: "tc-2", Name: "write"}},
	})

	gw := newTestGatewayFull(t)
	gw.SetApprovals(store)
	handler := gw.Handler()

	rec := doRequest(t, handler, http.MethodPost, "/operator/approvals/"+ticket.ID+"/resolve",
		`{"status":"denied","by":"admin"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("resolve denied: status = %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestApprovalsResolveLegacyDecisionAlias(t *testing.T) {
	t.Parallel()

	store := approval.NewInMemoryStore()
	ticket, _ := store.Create(context.Background(), approval.Ticket{
		RunID:     "run-legacy",
		SessionID: "sess-legacy",
		ToolCalls: []approval.ToolCall{{ID: "tc-legacy", Name: "exec.run"}},
	})

	gw := newTestGatewayFull(t)
	gw.SetApprovals(store)
	handler := gw.Handler()

	rec := doRequest(t, handler, http.MethodPost, "/operator/approvals/"+ticket.ID+"/resolve",
		`{"decision":"approve","by":"operator"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("legacy decision alias: status = %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestApprovalsResolveInvalidStatus(t *testing.T) {
	t.Parallel()

	store := approval.NewInMemoryStore()
	ticket, _ := store.Create(context.Background(), approval.Ticket{
		RunID:     "run-3",
		SessionID: "sess-3",
		ToolCalls: []approval.ToolCall{{ID: "tc-3", Name: "read"}},
	})

	gw := newTestGatewayFull(t)
	gw.SetApprovals(store)
	handler := gw.Handler()

	rec := doRequest(t, handler, http.MethodPost, "/operator/approvals/"+ticket.ID+"/resolve",
		`{"status":"maybe"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid status: status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestApprovalsResolveInvalidScope(t *testing.T) {
	t.Parallel()

	store := approval.NewInMemoryStore()
	ticket, _ := store.Create(context.Background(), approval.Ticket{
		RunID:     "run-invalid-scope",
		SessionID: "sess-invalid-scope",
		ToolCalls: []approval.ToolCall{{ID: "tc-1", Name: "exec.run"}},
	})

	gw := newTestGatewayFull(t)
	gw.SetApprovals(store)
	handler := gw.Handler()

	rec := doRequest(t, handler, http.MethodPost, "/operator/approvals/"+ticket.ID+"/resolve",
		`{"status":"approved","scope":"sometimes"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid scope: status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestApprovalsResolveAcceptsAuthContextWithoutScopeFiltering(t *testing.T) {
	t.Parallel()

	store := approval.NewInMemoryStore()
	ticket, _ := store.Create(context.Background(), approval.Ticket{
		RunID:     "run-beta",
		SessionID: "sess-beta",
		ToolCalls: []approval.ToolCall{{ID: "tc-1", Name: "exec.run"}},
		Metadata: map[string]any{
			"scope": map[string]any{
				"automation_id": "automation-beta",
			},
		},
	})

	gw := newTestGatewayFull(t)
	gw.SetApprovals(store)

	req := httptest.NewRequest(http.MethodPost, "/operator/approvals/"+ticket.ID+"/resolve", bytes.NewBufferString(`{"status":"approved"}`)).
		WithContext(scopedAuthContext("actor-a"))
	req.Header.Set("Content-Type", "application/json")
	req.SetPathValue("id", ticket.ID)

	rec := httptest.NewRecorder()
	http.HandlerFunc(gw.handleApprovalsResolve).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("resolve scope escape: status = %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestApprovalsResolveNotFound(t *testing.T) {
	t.Parallel()

	store := approval.NewInMemoryStore()
	gw := newTestGatewayFull(t)
	gw.SetApprovals(store)
	handler := gw.Handler()

	rec := doRequest(t, handler, http.MethodPost, "/operator/approvals/nonexistent/resolve",
		`{"status":"approved"}`)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("not found: status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

func TestApprovalsResolveInvalidJSON(t *testing.T) {
	t.Parallel()

	store := approval.NewInMemoryStore()
	gw := newTestGatewayFull(t)
	gw.SetApprovals(store)
	handler := gw.Handler()

	rec := doRequest(t, handler, http.MethodPost, "/operator/approvals/any-id/resolve", "not-json")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid json: status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestApprovalsResolveRejectsTrailingJSON(t *testing.T) {
	t.Parallel()

	store := approval.NewInMemoryStore()
	ticket, _ := store.Create(context.Background(), approval.Ticket{
		RunID:     "run-trailing",
		SessionID: "sess-trailing",
		ToolCalls: []approval.ToolCall{{ID: "tc-trailing", Name: "exec.run"}},
	})

	gw := newTestGatewayFull(t)
	gw.SetApprovals(store)
	handler := gw.Handler()

	rec := doRequest(t, handler, http.MethodPost, "/operator/approvals/"+ticket.ID+"/resolve", `{"status":"approved"} {"extra":true}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("trailing json: status = %d, want %d body=%s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}

	current, err := store.Get(context.Background(), ticket.ID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if current.Status != approval.StatusPending {
		t.Fatalf("ticket status = %q, want pending", current.Status)
	}
}

// ---------------------------------------------------------------------------
// handleApprovalsCancel
// ---------------------------------------------------------------------------

func TestApprovalsCancelNilStore(t *testing.T) {
	t.Parallel()

	gw := newTestGatewayFull(t)
	handler := gw.Handler()
	rec := doRequest(t, handler, http.MethodPost, "/operator/approvals/abc/cancel", "")
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("nil store: status = %d", rec.Code)
	}
}

func TestApprovalsCancelSuccess(t *testing.T) {
	t.Parallel()

	store := approval.NewInMemoryStore()
	ticket, _ := store.Create(context.Background(), approval.Ticket{
		RunID:     "run-4",
		SessionID: "sess-4",
		ToolCalls: []approval.ToolCall{{ID: "tc-4", Name: "exec.run"}},
	})

	gw := newTestGatewayFull(t)
	gw.SetApprovals(store)
	handler := gw.Handler()

	rec := doRequest(t, handler, http.MethodPost, "/operator/approvals/"+ticket.ID+"/cancel", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("cancel: status = %d body=%s", rec.Code, rec.Body.String())
	}

	var payload struct {
		OK     bool            `json:"ok"`
		Ticket runtimepkg.ApprovalView `json:"ticket"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if payload.Ticket.Status != approval.StatusCancelled {
		t.Fatalf("ticket status = %q, want cancelled", payload.Ticket.Status)
	}
}

func TestApprovalsCancelNotFound(t *testing.T) {
	t.Parallel()

	store := approval.NewInMemoryStore()
	gw := newTestGatewayFull(t)
	gw.SetApprovals(store)
	handler := gw.Handler()

	rec := doRequest(t, handler, http.MethodPost, "/operator/approvals/no-such-id/cancel", "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("not found: status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

func TestApprovalsCancelAlreadyResolvedConflict(t *testing.T) {
	t.Parallel()

	store := approval.NewInMemoryStore()
	ticket, _ := store.Create(context.Background(), approval.Ticket{
		RunID:     "run-5",
		SessionID: "sess-5",
		ToolCalls: []approval.ToolCall{{ID: "tc-5", Name: "exec.run"}},
	})
	_, _ = store.Resolve(context.Background(), ticket.ID, approval.Resolution{
		Status: approval.StatusApproved,
	})

	gw := newTestGatewayFull(t)
	gw.SetApprovals(store)
	handler := gw.Handler()

	rec := doRequest(t, handler, http.MethodPost, "/operator/approvals/"+ticket.ID+"/cancel", "")
	if rec.Code != http.StatusConflict {
		t.Fatalf("cancel resolved: status = %d body=%s", rec.Code, rec.Body.String())
	}
}

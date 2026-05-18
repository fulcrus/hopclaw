package gateway

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/fulcrus/hopclaw/agent"
	"github.com/fulcrus/hopclaw/eventbus"
	runtimepkg "github.com/fulcrus/hopclaw/runtime"
	"github.com/fulcrus/hopclaw/server"
)

// newAuditTestGateway creates a gateway with a real event bus for audit testing.
func newAuditTestGateway(t *testing.T, bus *eventbus.InMemoryBus) *Gateway {
	t.Helper()

	runs := agent.NewInMemoryRunStore()
	sessions := agent.NewInMemorySessionStore()
	svc := runtimepkg.NewService(nil, sessions, runs, nil, bus, nil)
	srv := server.New(svc, server.Config{AuthToken: "test-token"})
	cfg := Config{
		AuthToken: "test-token",
		Runtime:   svc,
	}
	return gatewayFromServer(srv, cfg)
}

// ---------------------------------------------------------------------------
// handleAuditEvents
// ---------------------------------------------------------------------------

func TestAuditEventsNilRuntime(t *testing.T) {
	t.Parallel()

	runs := agent.NewInMemoryRunStore()
	sessions := agent.NewInMemorySessionStore()
	svc := runtimepkg.NewService(nil, sessions, runs, nil, nil, nil)
	srv := server.New(svc, server.Config{AuthToken: "test-token"})
	gw := gatewayFromServer(srv, Config{AuthToken: "test-token", Runtime: nil})
	_ = gw
	// Runtime is nil — set it to nil explicitly.
	gw2 := newTestGatewayFull(t)
	gw2.runtime = nil
	handler := gw2.Handler()

	rec := doRequest(t, handler, http.MethodGet, "/operator/audit/events", "")
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("nil runtime: status = %d", rec.Code)
	}
}

func TestAuditEventsEmptyBus(t *testing.T) {
	t.Parallel()

	bus := eventbus.NewInMemoryBus()
	gw := newAuditTestGateway(t, bus)
	handler := gw.Handler()

	rec := doRequest(t, handler, http.MethodGet, "/operator/audit/events", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("empty events: status = %d body=%s", rec.Code, rec.Body.String())
	}

	var payload auditEventsResponse
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if payload.Count != 0 {
		t.Fatalf("count = %d, want 0", payload.Count)
	}
}

func TestAuditEventsFiltersSecurityOnly(t *testing.T) {
	t.Parallel()

	bus := eventbus.NewInMemoryBus()
	// Publish a non-security event and a security event.
	_ = bus.Publish(context.Background(), eventbus.Event{
		Type: eventbus.EventRunCompleted,
		Time: time.Now(),
	})
	_ = bus.Publish(context.Background(), eventbus.Event{
		Type: eventbus.EventType("security.risk_detected"),
		Time: time.Now(),
		Attrs: map[string]any{
			"severity": "high",
			"tool":     "exec.run",
		},
	})

	gw := newAuditTestGateway(t, bus)
	handler := gw.Handler()

	rec := doRequest(t, handler, http.MethodGet, "/operator/audit/events", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("audit: status = %d body=%s", rec.Code, rec.Body.String())
	}

	var payload auditEventsResponse
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	// Should only contain the security event.
	if payload.Count != 1 {
		t.Fatalf("count = %d, want 1", payload.Count)
	}
}

func TestAuditEventsFilterByType(t *testing.T) {
	t.Parallel()

	bus := eventbus.NewInMemoryBus()
	_ = bus.Publish(context.Background(), eventbus.Event{
		Type: eventbus.EventType("security.risk_detected"),
		Time: time.Now(),
	})
	_ = bus.Publish(context.Background(), eventbus.Event{
		Type: eventbus.EventType("security.auth_failed"),
		Time: time.Now(),
	})

	gw := newAuditTestGateway(t, bus)
	handler := gw.Handler()

	rec := doRequest(t, handler, http.MethodGet, "/operator/audit/events?type=security.auth_failed", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("filter type: status = %d body=%s", rec.Code, rec.Body.String())
	}

	var payload auditEventsResponse
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if payload.Count != 1 {
		t.Fatalf("count = %d, want 1", payload.Count)
	}
}

func TestAuditEventsFilterBySeverity(t *testing.T) {
	t.Parallel()

	bus := eventbus.NewInMemoryBus()
	_ = bus.Publish(context.Background(), eventbus.Event{
		Type:  eventbus.EventType("security.risk_detected"),
		Time:  time.Now(),
		Attrs: map[string]any{"severity": "high"},
	})
	_ = bus.Publish(context.Background(), eventbus.Event{
		Type:  eventbus.EventType("security.warning"),
		Time:  time.Now(),
		Attrs: map[string]any{"severity": "low"},
	})

	gw := newAuditTestGateway(t, bus)
	handler := gw.Handler()

	rec := doRequest(t, handler, http.MethodGet, "/operator/audit/events?severity=high", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("filter severity: status = %d body=%s", rec.Code, rec.Body.String())
	}

	var payload auditEventsResponse
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if payload.Count != 1 {
		t.Fatalf("count = %d, want 1", payload.Count)
	}
}

func TestAuditEventsIncludeApprovalAndGovernanceFamiliesByDefault(t *testing.T) {
	t.Parallel()

	bus := eventbus.NewInMemoryBus()
	_ = bus.Publish(context.Background(), eventbus.Event{
		Type: eventbus.EventApprovalRequested,
		Time: time.Now(),
		Attrs: map[string]any{
			"approval_id": "apr-1",
		},
	})
	_ = bus.Publish(context.Background(), eventbus.Event{
		Type: eventbus.EventGovernanceDeliveryDeadLettered,
		Time: time.Now(),
		Attrs: map[string]any{
			"adapter_name": "audit-hub",
		},
	})

	gw := newAuditTestGateway(t, bus)
	handler := gw.Handler()

	rec := doRequest(t, handler, http.MethodGet, "/operator/audit/events", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("audit default families: status = %d body=%s", rec.Code, rec.Body.String())
	}

	var payload auditEventsResponse
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if payload.Count != 2 {
		t.Fatalf("count = %d, want 2", payload.Count)
	}
}

func TestAuditEventsFilterByFamilyAndApprovalID(t *testing.T) {
	t.Parallel()

	bus := eventbus.NewInMemoryBus()
	_ = bus.Publish(context.Background(), eventbus.Event{
		Type: eventbus.EventApprovalRequested,
		Time: time.Now(),
		Attrs: map[string]any{
			"approval_id": "apr-keep",
		},
	})
	_ = bus.Publish(context.Background(), eventbus.Event{
		Type: eventbus.EventApprovalResolved,
		Time: time.Now(),
		Attrs: map[string]any{
			"approval_id": "apr-skip",
		},
	})
	_ = bus.Publish(context.Background(), eventbus.Event{
		Type: eventbus.EventSecurityRiskDetected,
		Time: time.Now(),
		Attrs: map[string]any{
			"severity": "high",
		},
	})

	gw := newAuditTestGateway(t, bus)
	handler := gw.Handler()

	rec := doRequest(t, handler, http.MethodGet, "/operator/audit/events?family=approval&approval_id=apr-keep", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("audit family+approval_id: status = %d body=%s", rec.Code, rec.Body.String())
	}

	var payload auditEventsResponse
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if payload.Count != 1 {
		t.Fatalf("count = %d, want 1", payload.Count)
	}
	if payload.Items[0].Type != eventbus.EventApprovalRequested {
		t.Fatalf("type = %q, want %q", payload.Items[0].Type, eventbus.EventApprovalRequested)
	}
}

func TestAuditEventsIncludeGovernanceProjection(t *testing.T) {
	t.Parallel()

	bus := eventbus.NewInMemoryBus()
	_ = bus.Publish(context.Background(), eventbus.Event{
		Type: eventbus.EventType("security.risk_detected"),
		Time: time.Now(),
		Attrs: map[string]any{
			"severity":                     "high",
			"summary":                      "suspicious tool attempt",
			"scope":                        map[string]any{"automation_id": "auto-audit"},
			"effective_config_snapshot_id": "ecs-audit-1",
			"policy_action":                "deny",
			"policy_source":                "security.policy/audit",
			"policy_summary":               "blocked by audit policy",
			"policy_reasons":               []string{"suspicious path"},
		},
	})

	gw := newAuditTestGateway(t, bus)
	handler := gw.Handler()

	rec := doRequest(t, handler, http.MethodGet, "/operator/audit/events", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("audit governance: status = %d body=%s", rec.Code, rec.Body.String())
	}

	var payload auditEventsResponse
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if payload.Count != 1 || len(payload.Items) != 1 {
		t.Fatalf("count = %d len = %d, want 1", payload.Count, len(payload.Items))
	}
	item := payload.Items[0]
	if item.Governance == nil {
		t.Fatalf("Governance = %#v", item.Governance)
	}
	if item.Severity != "high" {
		t.Fatalf("Severity = %q, want high", item.Severity)
	}
	if item.Governance.Scope.AutomationID != "auto-audit" {
		t.Fatalf("Scope = %#v", item.Governance.Scope)
	}
	if item.Governance.Policy == nil || item.Governance.Policy.PolicySource != "security.policy/audit" {
		t.Fatalf("Policy = %#v", item.Governance.Policy)
	}
	if item.Governance.EffectiveConfigSnapshotID != "ecs-audit-1" {
		t.Fatalf("EffectiveConfigSnapshotID = %q", item.Governance.EffectiveConfigSnapshotID)
	}
}

func TestAuditEventsFilterByTimeRange(t *testing.T) {
	t.Parallel()

	bus := eventbus.NewInMemoryBus()
	past := time.Now().Add(-48 * time.Hour)
	recent := time.Now()

	_ = bus.Publish(context.Background(), eventbus.Event{
		Type: eventbus.EventType("security.old_event"),
		Time: past,
	})
	_ = bus.Publish(context.Background(), eventbus.Event{
		Type: eventbus.EventType("security.new_event"),
		Time: recent,
	})

	gw := newAuditTestGateway(t, bus)
	handler := gw.Handler()

	since := time.Now().UTC().Add(-1 * time.Hour).Format(time.RFC3339)
	rec := doRequest(t, handler, http.MethodGet, "/operator/audit/events?since="+since, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("filter time: status = %d body=%s", rec.Code, rec.Body.String())
	}

	var payload auditEventsResponse
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if payload.Count != 1 {
		t.Fatalf("count = %d, want 1 (only recent event)", payload.Count)
	}
}

func TestAuditEventsFilterByScope(t *testing.T) {
	t.Parallel()

	bus := eventbus.NewInMemoryBus()
	_ = bus.Publish(context.Background(), eventbus.Event{
		Type: eventbus.EventSecurityRiskDetected,
		Time: time.Now(),
		Attrs: map[string]any{
			"scope": map[string]any{
				"automation_id": "automation-a",
			},
		},
	})
	_ = bus.Publish(context.Background(), eventbus.Event{
		Type: eventbus.EventSecurityRiskDetected,
		Time: time.Now(),
		Attrs: map[string]any{
			"scope": map[string]any{
				"automation_id": "automation-b",
			},
		},
	})

	gw := newAuditTestGateway(t, bus)
	handler := gw.Handler()

	rec := doRequest(t, handler, http.MethodGet, "/operator/audit/events", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("audit scope filter: status = %d body=%s", rec.Code, rec.Body.String())
	}

	var payload auditEventsResponse
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if payload.Count != 2 {
		t.Fatalf("count = %d, want 2", payload.Count)
	}
}

func TestAuditEventsKeepItemsWithAuthContext(t *testing.T) {
	t.Parallel()

	bus := eventbus.NewInMemoryBus()
	_ = bus.Publish(context.Background(), eventbus.Event{
		Type: eventbus.EventSecurityRiskDetected,
		Time: time.Now(),
		Attrs: map[string]any{
			"scope": map[string]any{
				"automation_id": "automation-a",
			},
		},
	})
	_ = bus.Publish(context.Background(), eventbus.Event{
		Type: eventbus.EventSecurityRiskDetected,
		Time: time.Now(),
		Attrs: map[string]any{
			"scope": map[string]any{
				"automation_id": "automation-b",
			},
		},
	})

	gw := newAuditTestGateway(t, bus)
	req := httptest.NewRequest(http.MethodGet, "/operator/audit/events", nil).
		WithContext(scopedAuthContext("actor-a"))
	rec := httptest.NewRecorder()
	http.HandlerFunc(gw.handleAuditEvents).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("audit scope auth: status = %d body=%s", rec.Code, rec.Body.String())
	}

	var payload auditEventsResponse
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if payload.Count != 2 {
		t.Fatalf("count = %d, want 2", payload.Count)
	}
}

func TestAuditEventsRejectAuthenticatedScopeEscape(t *testing.T) {
	t.Parallel()

	bus := eventbus.NewInMemoryBus()
	gw := newAuditTestGateway(t, bus)
	req := httptest.NewRequest(http.MethodGet, "/operator/audit/events", nil).
		WithContext(scopedAuthContext("actor-a"))
	rec := httptest.NewRecorder()
	http.HandlerFunc(gw.handleAuditEvents).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("audit scope escape: status = %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestAuditEventsFilterByAuthenticatedAutomationScope(t *testing.T) {
	t.Parallel()

	bus := eventbus.NewInMemoryBus()
	_ = bus.Publish(context.Background(), eventbus.Event{
		Type: eventbus.EventSecurityRiskDetected,
		Time: time.Now(),
		Attrs: map[string]any{
			"scope": map[string]any{
				"automation_id": "automation-a",
			},
		},
	})
	_ = bus.Publish(context.Background(), eventbus.Event{
		Type: eventbus.EventSecurityRiskDetected,
		Time: time.Now(),
		Attrs: map[string]any{
			"scope": map[string]any{
				"automation_id": "automation-b",
			},
		},
	})

	gw := newAuditTestGateway(t, bus)
	req := httptest.NewRequest(http.MethodGet, "/operator/audit/events", nil).
		WithContext(scopedAutomationAuthContext("actor-a", "automation-a"))
	rec := httptest.NewRecorder()
	http.HandlerFunc(gw.handleAuditEvents).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("audit scope auth: status = %d body=%s", rec.Code, rec.Body.String())
	}

	var payload auditEventsResponse
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if payload.Count != 1 || len(payload.Items) != 1 {
		t.Fatalf("payload = %+v", payload)
	}
	if payload.Items[0].Governance == nil || payload.Items[0].Governance.Scope.AutomationID != "automation-a" {
		t.Fatalf("payload.Items[0] = %#v", payload.Items[0])
	}
}

func TestAuditEventsLimitParameter(t *testing.T) {
	t.Parallel()

	bus := eventbus.NewInMemoryBus()
	for i := range 5 {
		_ = bus.Publish(context.Background(), eventbus.Event{
			Type: eventbus.EventType("security.event"),
			Time: time.Now().Add(time.Duration(i) * time.Second),
		})
	}

	gw := newAuditTestGateway(t, bus)
	handler := gw.Handler()

	rec := doRequest(t, handler, http.MethodGet, "/operator/audit/events?limit=2", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("limit: status = %d body=%s", rec.Code, rec.Body.String())
	}

	var payload auditEventsResponse
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if payload.Count != 2 {
		t.Fatalf("count = %d, want 2", payload.Count)
	}
}

func TestAuditEventsInvalidSinceParam(t *testing.T) {
	t.Parallel()

	bus := eventbus.NewInMemoryBus()
	gw := newAuditTestGateway(t, bus)
	handler := gw.Handler()

	rec := doRequest(t, handler, http.MethodGet, "/operator/audit/events?since=not-a-date", "")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid since: status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestAuditEventsInvalidUntilParam(t *testing.T) {
	t.Parallel()

	bus := eventbus.NewInMemoryBus()
	gw := newAuditTestGateway(t, bus)
	handler := gw.Handler()

	rec := doRequest(t, handler, http.MethodGet, "/operator/audit/events?until=bad", "")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid until: status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestAuditEventsInvalidLimitParam(t *testing.T) {
	t.Parallel()

	bus := eventbus.NewInMemoryBus()
	gw := newAuditTestGateway(t, bus)
	handler := gw.Handler()

	rec := doRequest(t, handler, http.MethodGet, "/operator/audit/events?limit=abc", "")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid limit: status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestAuditEventsSinceCursorAdvancesPastNonMatchingEvents(t *testing.T) {
	t.Parallel()

	bus := eventbus.NewInMemoryBus()
	_ = bus.Publish(context.Background(), eventbus.Event{
		Type: eventbus.EventSecurityRiskDetected,
		Time: time.Now(),
	})
	initial := bus.Snapshot()
	cursor := initial[len(initial)-1].ID

	_ = bus.Publish(context.Background(), eventbus.Event{
		Type: eventbus.EventSecurityPathViolation,
		Time: time.Now().Add(time.Second),
	})

	gw := newAuditTestGateway(t, bus)
	handler := gw.Handler()

	rec := doRequest(t, handler, http.MethodGet, "/operator/audit/events?since_id="+cursor+"&family=approval", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("audit since cursor: status = %d body=%s", rec.Code, rec.Body.String())
	}

	var payload auditEventsResponse
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if payload.Count != 0 {
		t.Fatalf("count = %d, want 0", payload.Count)
	}
	if payload.NextCursor == "" {
		t.Fatal("expected next_cursor to advance even when no events matched")
	}
	if payload.CursorStatus != string(eventbus.CursorOK) {
		t.Fatalf("cursor_status = %q, want %q", payload.CursorStatus, eventbus.CursorOK)
	}
}

// ---------------------------------------------------------------------------
// filterAuditEvents unit tests
// ---------------------------------------------------------------------------

func TestFilterAuditEventsReturnsEmptySlice(t *testing.T) {
	t.Parallel()

	result := filterAuditEvents(nil, auditEventFilter{Families: parseAuditFamilies("")}, 100)
	if result == nil {
		t.Fatal("expected non-nil empty slice")
	}
	if len(result) != 0 {
		t.Fatalf("len = %d, want 0", len(result))
	}
}

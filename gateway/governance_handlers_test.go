package gateway

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/fulcrus/hopclaw/agent"
	"github.com/fulcrus/hopclaw/controlplane"
	"github.com/fulcrus/hopclaw/eventbus"
	controlgov "github.com/fulcrus/hopclaw/internal/controlplane/governanceadapter"
	runtimepkg "github.com/fulcrus/hopclaw/runtime"
	"github.com/fulcrus/hopclaw/server"
)

func TestGovernanceDeliveriesListAndStats(t *testing.T) {
	t.Parallel()

	gw, store, _ := newGovernanceGateway(t)
	ctx := context.Background()
	mustSeedGatewayGovernanceDelivery(t, ctx, store, controlgov.DeliveryEntry{
		ID:          "gdel-gw-1",
		AdapterName: "audit-hub",
		Status:      controlgov.DeliveryStatusDeadLetter,
		Attempts:    3,
		MaxAttempts: 3,
		Record: controlgov.Record{
			Kind:      controlgov.KindSecurityEvent,
			EventID:   "evt-gw-1",
			EventType: eventbus.EventSecurityRiskDetected,
			RunID:     "run-gw-1",
			SessionID: "sess-gw-1",
			Summary:   "gateway delivery",
		},
	})

	handler := gw.Handler()
	rec := doRequest(t, handler, http.MethodGet, "/operator/governance/deliveries?status=dead_letter", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /operator/governance/deliveries status = %d body=%s", rec.Code, rec.Body.String())
	}
	var listPayload struct {
		Items []runtimepkg.GovernanceDeliveryView `json:"items"`
		Count int                                 `json:"count"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&listPayload); err != nil {
		t.Fatalf("Decode(list) error = %v", err)
	}
	if listPayload.Count != 1 || len(listPayload.Items) != 1 {
		t.Fatalf("list payload = %#v", listPayload)
	}

	rec = doRequest(t, handler, http.MethodGet, "/operator/governance/deliveries/stats", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /operator/governance/deliveries/stats status = %d body=%s", rec.Code, rec.Body.String())
	}
	var stats runtimepkg.GovernanceDeliveryStats
	if err := json.NewDecoder(rec.Body).Decode(&stats); err != nil {
		t.Fatalf("Decode(stats) error = %v", err)
	}
	if stats.Total != 1 || stats.Redrivable != 1 {
		t.Fatalf("stats = %#v", stats)
	}
}

func TestGovernanceDeliveriesRedrive(t *testing.T) {
	t.Parallel()

	gw, store, _ := newGovernanceGateway(t)
	ctx := context.Background()
	mustSeedGatewayGovernanceDelivery(t, ctx, store, controlgov.DeliveryEntry{
		ID:          "gdel-gw-2",
		AdapterName: "audit-hub",
		Status:      controlgov.DeliveryStatusDeadLetter,
		Attempts:    3,
		MaxAttempts: 3,
		LastError:   "boom",
		Record: controlgov.Record{
			Kind:      controlgov.KindSecurityEvent,
			EventID:   "evt-gw-2",
			EventType: eventbus.EventSecurityRiskDetected,
			RunID:     "run-gw-2",
			SessionID: "sess-gw-2",
			Summary:   "gateway redrive",
		},
	})

	handler := gw.Handler()
	rec := doRequest(t, handler, http.MethodPost, "/operator/governance/deliveries/gdel-gw-2/redrive", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /operator/governance/deliveries/{id}/redrive status = %d body=%s", rec.Code, rec.Body.String())
	}
	item, err := gw.runtime.GetGovernanceDelivery(ctx, "gdel-gw-2")
	if err != nil {
		t.Fatalf("GetGovernanceDelivery() error = %v", err)
	}
	if item.Status != controlplane.GovernanceDeliveryStatusPending || item.Attempts != 0 || item.LastError != "" {
		t.Fatalf("item = %#v", item)
	}
}

func TestGovernanceDeliveryRedriveRejectsTrailingJSON(t *testing.T) {
	t.Parallel()

	gw, store, _ := newGovernanceGateway(t)
	ctx := context.Background()
	mustSeedGatewayGovernanceDelivery(t, ctx, store, controlgov.DeliveryEntry{
		ID:          "gdel-gw-trailing",
		AdapterName: "audit-hub",
		Status:      controlgov.DeliveryStatusDeadLetter,
		Attempts:    2,
		MaxAttempts: 3,
		LastError:   "boom",
		Record: controlgov.Record{
			Kind:      controlgov.KindSecurityEvent,
			EventID:   "evt-gw-trailing",
			EventType: eventbus.EventSecurityRiskDetected,
			RunID:     "run-gw-trailing",
			SessionID: "sess-gw-trailing",
		},
	})

	rec := doRequest(t, gw.Handler(), http.MethodPost, "/operator/governance/deliveries/gdel-gw-trailing/redrive", `{"clear_error":true} {"extra":true}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("POST /operator/governance/deliveries/{id}/redrive trailing json status = %d body=%s", rec.Code, rec.Body.String())
	}

	item, err := gw.runtime.GetGovernanceDelivery(ctx, "gdel-gw-trailing")
	if err != nil {
		t.Fatalf("GetGovernanceDelivery() error = %v", err)
	}
	if item.Status != controlplane.GovernanceDeliveryStatusDeadLetter || item.Attempts != 2 || item.LastError != "boom" {
		t.Fatalf("item = %#v", item)
	}
}

func TestGovernanceDeliveriesRedriveRejectsTrailingJSON(t *testing.T) {
	t.Parallel()

	gw, store, _ := newGovernanceGateway(t)
	ctx := context.Background()
	mustSeedGatewayGovernanceDelivery(t, ctx, store, controlgov.DeliveryEntry{
		ID:          "gdel-gw-batch",
		AdapterName: "audit-hub",
		Status:      controlgov.DeliveryStatusDeadLetter,
		Attempts:    1,
		MaxAttempts: 3,
		LastError:   "boom",
		Record: controlgov.Record{
			Kind:      controlgov.KindSecurityEvent,
			EventID:   "evt-gw-batch",
			EventType: eventbus.EventSecurityRiskDetected,
			RunID:     "run-gw-batch",
			SessionID: "sess-gw-batch",
		},
	})

	rec := doRequest(t, gw.Handler(), http.MethodPost, "/operator/governance/deliveries/redrive", `{"ids":["gdel-gw-batch"]} {"extra":true}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("POST /operator/governance/deliveries/redrive trailing json status = %d body=%s", rec.Code, rec.Body.String())
	}

	item, err := gw.runtime.GetGovernanceDelivery(ctx, "gdel-gw-batch")
	if err != nil {
		t.Fatalf("GetGovernanceDelivery() error = %v", err)
	}
	if item.Status != controlplane.GovernanceDeliveryStatusDeadLetter || item.Attempts != 1 || item.LastError != "boom" {
		t.Fatalf("item = %#v", item)
	}
}

func TestGovernanceHealth(t *testing.T) {
	t.Parallel()

	gw, store, _ := newGovernanceGateway(t)
	ctx := context.Background()
	now := time.Now().UTC()
	mustSeedGatewayGovernanceDelivery(t, ctx, store, controlgov.DeliveryEntry{
		ID:            "gdel-gw-health-1",
		AdapterName:   "audit-hub",
		Status:        controlgov.DeliveryStatusPending,
		Attempts:      2,
		MaxAttempts:   3,
		CreatedAt:     now.Add(-12 * time.Minute),
		UpdatedAt:     now.Add(-12 * time.Minute),
		NextAttemptAt: now.Add(-7 * time.Minute),
		Record: controlgov.Record{
			Kind:      controlgov.KindSecurityEvent,
			EventID:   "evt-gw-health-1",
			EventType: eventbus.EventSecurityRiskDetected,
			RunID:     "run-gw-health-1",
			SessionID: "sess-gw-health-1",
		},
	})

	handler := gw.Handler()
	rec := doRequest(t, handler, http.MethodGet, "/operator/governance/health", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /operator/governance/health status = %d body=%s", rec.Code, rec.Body.String())
	}
	var health runtimepkg.GovernanceDeliveryHealth
	if err := json.NewDecoder(rec.Body).Decode(&health); err != nil {
		t.Fatalf("Decode(health) error = %v", err)
	}
	if health.Status != "warn" {
		t.Fatalf("health.Status = %q, want warn", health.Status)
	}
	if health.StalePendingCount != 1 {
		t.Fatalf("health.StalePendingCount = %d, want 1", health.StalePendingCount)
	}
}

func TestGovernanceEvents(t *testing.T) {
	t.Parallel()

	gw, _, bus := newGovernanceGateway(t)
	handler := gw.Handler()
	_ = bus.Publish(context.Background(), eventbus.Event{
		Type:      eventbus.EventGovernanceDeliveryRedriven,
		RunID:     "run-gw-evt",
		SessionID: "sess-gw-evt",
		Attrs: map[string]any{
			"adapter_name":    "audit-hub",
			"delivery_status": "pending",
			"summary":         "redriven",
		},
	})

	rec := doRequest(t, handler, http.MethodGet, "/operator/governance/events?adapter_name=audit-hub", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /operator/governance/events status = %d body=%s", rec.Code, rec.Body.String())
	}
	var payload struct {
		Items []runtimepkg.EventView `json:"items"`
		Count int                    `json:"count"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("Decode(events) error = %v", err)
	}
	if payload.Count != 1 || len(payload.Items) != 1 {
		t.Fatalf("payload = %#v", payload)
	}
	if payload.Items[0].Type != eventbus.EventGovernanceDeliveryRedriven {
		t.Fatalf("event type = %q", payload.Items[0].Type)
	}
}

func newGovernanceGateway(t *testing.T) (*Gateway, *controlgov.InMemoryDeliveryStore, *eventbus.InMemoryBus) {
	t.Helper()

	runs := agent.NewInMemoryRunStore()
	sessions := agent.NewInMemorySessionStore()
	bus := eventbus.NewInMemoryBus()
	store := controlgov.NewInMemoryDeliveryStore()
	controller := controlgov.NewReliableDispatcher(controlgov.DeliveryConfig{
		MaxAttempts:  3,
		BaseBackoff:  10 * time.Millisecond,
		MaxBackoff:   10 * time.Millisecond,
		PollInterval: 10 * time.Millisecond,
		BatchSize:    8,
	}, store).WithEventBus(bus)
	svc := runtimepkg.NewService(nil, sessions, runs, nil, bus, nil).WithGovernanceDelivery(controlgov.AdaptDeliveryController(controller))
	srv := server.New(svc, server.Config{AuthToken: "test-token"})
	gw := gatewayFromServer(srv, Config{
		AuthToken: "test-token",
		Runtime:   svc,
	})
	return gw, store, bus
}

func mustSeedGatewayGovernanceDelivery(t *testing.T, ctx context.Context, store *controlgov.InMemoryDeliveryStore, entry controlgov.DeliveryEntry) {
	t.Helper()
	if entry.CreatedAt.IsZero() {
		entry.CreatedAt = time.Now().UTC()
	}
	if entry.UpdatedAt.IsZero() {
		entry.UpdatedAt = entry.CreatedAt
	}
	if _, _, err := store.Enqueue(ctx, entry); err != nil {
		t.Fatalf("Enqueue() error = %v", err)
	}
	stored, err := store.Get(ctx, entry.ID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	stored.Status = entry.Status
	stored.Attempts = entry.Attempts
	stored.MaxAttempts = entry.MaxAttempts
	stored.LastError = entry.LastError
	stored.Record = entry.Record
	stored.CreatedAt = entry.CreatedAt
	stored.UpdatedAt = entry.UpdatedAt
	stored.NextAttemptAt = entry.NextAttemptAt
	stored.LastAttemptAt = entry.LastAttemptAt
	stored.DeliveredAt = entry.DeliveredAt
	if err := store.Update(ctx, stored); err != nil {
		t.Fatalf("Update() error = %v", err)
	}
}

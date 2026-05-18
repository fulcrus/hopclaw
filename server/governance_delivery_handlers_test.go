package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/fulcrus/hopclaw/agent"
	"github.com/fulcrus/hopclaw/controlplane"
	"github.com/fulcrus/hopclaw/eventbus"
	controlgov "github.com/fulcrus/hopclaw/internal/controlplane/governanceadapter"
	rt "github.com/fulcrus/hopclaw/runtime"
)

func TestServerListGovernanceDeliveries(t *testing.T) {
	t.Parallel()

	controller, store := newServerGovernanceController()
	svc := newRuntimeService(t, runtimeFixture{governance: controller})
	handler := New(svc, Config{}).Handler()
	ctx := context.Background()

	mustSeedServerGovernanceDelivery(t, ctx, store, controlgov.DeliveryEntry{
		ID:          "gdel-srv-1",
		AdapterName: "audit-hub",
		Status:      controlgov.DeliveryStatusDeadLetter,
		Attempts:    3,
		MaxAttempts: 3,
		Record: controlgov.Record{
			Kind:      controlgov.KindSecurityEvent,
			EventID:   "evt-srv-1",
			EventType: eventbus.EventSecurityRiskDetected,
			RunID:     "run-srv-1",
			SessionID: "sess-srv-1",
			Summary:   "server dead letter",
		},
	})

	req := httptest.NewRequest(http.MethodGet, "/runtime/governance/deliveries?status=dead_letter", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /runtime/governance/deliveries status = %d body=%s", rec.Code, rec.Body.String())
	}
	var payload struct {
		Items []rt.GovernanceDeliveryView `json:"items"`
		Count int                         `json:"count"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload.Count != 1 || len(payload.Items) != 1 {
		t.Fatalf("payload = %#v", payload)
	}
	if payload.Items[0].Status != controlplane.GovernanceDeliveryStatusDeadLetter {
		t.Fatalf("status = %q", payload.Items[0].Status)
	}
}

func TestServerRedriveGovernanceDelivery(t *testing.T) {
	t.Parallel()

	controller, store := newServerGovernanceController()
	svc := newRuntimeService(t, runtimeFixture{governance: controller})
	handler := New(svc, Config{}).Handler()
	ctx := context.Background()

	mustSeedServerGovernanceDelivery(t, ctx, store, controlgov.DeliveryEntry{
		ID:          "gdel-srv-2",
		AdapterName: "audit-hub",
		Status:      controlgov.DeliveryStatusDeadLetter,
		Attempts:    3,
		MaxAttempts: 3,
		LastError:   "boom",
		Record: controlgov.Record{
			Kind:      controlgov.KindSecurityEvent,
			EventID:   "evt-srv-2",
			EventType: eventbus.EventSecurityRiskDetected,
			RunID:     "run-srv-2",
			SessionID: "sess-srv-2",
			Summary:   "server redrive",
		},
	})

	req := httptest.NewRequest(http.MethodPost, "/runtime/governance/deliveries/gdel-srv-2/redrive", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /runtime/governance/deliveries/{id}/redrive status = %d body=%s", rec.Code, rec.Body.String())
	}
	item, err := svc.GetGovernanceDelivery(ctx, "gdel-srv-2")
	if err != nil {
		t.Fatalf("GetGovernanceDelivery() error = %v", err)
	}
	if item.Status != controlplane.GovernanceDeliveryStatusPending {
		t.Fatalf("item.Status = %q, want pending", item.Status)
	}
	if item.Attempts != 0 {
		t.Fatalf("item.Attempts = %d, want 0", item.Attempts)
	}
}

func TestServerGovernanceHealth(t *testing.T) {
	t.Parallel()

	controller, store := newServerGovernanceController()
	svc := newRuntimeService(t, runtimeFixture{governance: controller})
	handler := New(svc, Config{}).Handler()
	ctx := context.Background()
	now := time.Now().UTC()

	mustSeedServerGovernanceDelivery(t, ctx, store, controlgov.DeliveryEntry{
		ID:            "gdel-srv-health-1",
		AdapterName:   "audit-hub",
		Status:        controlgov.DeliveryStatusPending,
		Attempts:      1,
		MaxAttempts:   3,
		CreatedAt:     now.Add(-10 * time.Minute),
		UpdatedAt:     now.Add(-10 * time.Minute),
		NextAttemptAt: now.Add(-6 * time.Minute),
		Record: controlgov.Record{
			Kind:      controlgov.KindSecurityEvent,
			EventID:   "evt-srv-health-1",
			EventType: eventbus.EventSecurityRiskDetected,
			RunID:     "run-srv-health-1",
			SessionID: "sess-srv-health-1",
		},
	})

	req := httptest.NewRequest(http.MethodGet, "/runtime/governance/health", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /runtime/governance/health status = %d body=%s", rec.Code, rec.Body.String())
	}
	var payload rt.GovernanceDeliveryHealth
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload.Status != "warn" {
		t.Fatalf("payload.Status = %q, want warn", payload.Status)
	}
	if payload.StalePendingCount != 1 {
		t.Fatalf("payload.StalePendingCount = %d, want 1", payload.StalePendingCount)
	}
}

func newServerGovernanceController() (rt.GovernanceDeliveryController, *controlgov.InMemoryDeliveryStore) {
	store := controlgov.NewInMemoryDeliveryStore()
	controller := controlgov.NewReliableDispatcher(controlgov.DeliveryConfig{
		MaxAttempts:  3,
		BaseBackoff:  10 * time.Millisecond,
		MaxBackoff:   10 * time.Millisecond,
		PollInterval: 10 * time.Millisecond,
		BatchSize:    8,
	}, store)
	return controlgov.AdaptDeliveryController(controller), store
}

func mustSeedServerGovernanceDelivery(t *testing.T, ctx context.Context, store *controlgov.InMemoryDeliveryStore, entry controlgov.DeliveryEntry) {
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

var _ = agent.Run{}

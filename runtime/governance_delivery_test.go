package runtime

import (
	"context"
	"testing"
	"time"

	"github.com/fulcrus/hopclaw/agent"
	"github.com/fulcrus/hopclaw/controlplane"
	"github.com/fulcrus/hopclaw/eventbus"
	controlgov "github.com/fulcrus/hopclaw/internal/controlplane/governanceadapter"
)

func TestListGovernanceDeliveriesAndStats(t *testing.T) {
	t.Parallel()

	svc, store := newGovernanceDeliveryTestService()
	ctx := context.Background()
	mustEnqueueGovernanceDelivery(t, ctx, store, controlgov.DeliveryEntry{
		ID:          "gdel-001",
		AdapterName: "audit-hub",
		Status:      controlgov.DeliveryStatusPending,
		Attempts:    1,
		MaxAttempts: 3,
		Record: controlgov.Record{
			Kind:      controlgov.KindSecurityEvent,
			EventID:   "evt-1",
			EventType: eventbus.EventSecurityRiskDetected,
			RunID:     "run-1",
			SessionID: "sess-1",
			Summary:   "first",
		},
	})
	mustEnqueueGovernanceDelivery(t, ctx, store, controlgov.DeliveryEntry{
		ID:          "gdel-002",
		AdapterName: "audit-hub",
		Status:      controlgov.DeliveryStatusDeadLetter,
		Attempts:    3,
		MaxAttempts: 3,
		LastError:   "boom",
		Record: controlgov.Record{
			Kind:      controlgov.KindSecurityEvent,
			EventID:   "evt-2",
			EventType: eventbus.EventSecurityRiskDetected,
			RunID:     "run-2",
			SessionID: "sess-2",
			Summary:   "second",
		},
	})
	mustEnqueueGovernanceDelivery(t, ctx, store, controlgov.DeliveryEntry{
		ID:          "gdel-003",
		AdapterName: "jira",
		Status:      controlgov.DeliveryStatusDelivered,
		Attempts:    1,
		MaxAttempts: 3,
		Record: controlgov.Record{
			Kind:      controlgov.KindApprovalResolved,
			EventID:   "evt-3",
			EventType: eventbus.EventApprovalResolved,
			RunID:     "run-1",
			SessionID: "sess-1",
			Summary:   "third",
		},
	})

	items, err := svc.ListGovernanceDeliveries(ctx, GovernanceDeliveryFilter{
		RunID: "run-1",
		Limit: 10,
	})
	if err != nil {
		t.Fatalf("ListGovernanceDeliveries() error = %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("len(items) = %d, want 2", len(items))
	}
	if items[0].ID != "gdel-003" || items[1].ID != "gdel-001" {
		t.Fatalf("items order = %#v", items)
	}
	if items[0].IdempotencyKey == "" || items[1].IdempotencyKey == "" {
		t.Fatalf("items idempotency keys = %#v / %#v, want non-empty", items[0], items[1])
	}
	if !items[1].CanRedrive {
		t.Fatalf("pending item should be redrivable: %#v", items[1])
	}
	if items[0].CanRedrive {
		t.Fatalf("delivered item should not be redrivable: %#v", items[0])
	}

	stats, err := svc.GetGovernanceDeliveryStats(ctx, GovernanceDeliveryFilter{})
	if err != nil {
		t.Fatalf("GetGovernanceDeliveryStats() error = %v", err)
	}
	if stats.Total != 3 {
		t.Fatalf("stats.Total = %d, want 3", stats.Total)
	}
	if stats.Redrivable != 2 {
		t.Fatalf("stats.Redrivable = %d, want 2", stats.Redrivable)
	}
	if stats.ByStatus[controlplane.GovernanceDeliveryStatusDeadLetter] != 1 {
		t.Fatalf("dead letter count = %d, want 1", stats.ByStatus[controlplane.GovernanceDeliveryStatusDeadLetter])
	}
}

func TestRedriveGovernanceDeliveriesByFilter(t *testing.T) {
	t.Parallel()

	svc, store := newGovernanceDeliveryTestService()
	ctx := context.Background()
	mustEnqueueGovernanceDelivery(t, ctx, store, controlgov.DeliveryEntry{
		ID:          "gdel-101",
		AdapterName: "audit-hub",
		Status:      controlgov.DeliveryStatusDeadLetter,
		Attempts:    3,
		MaxAttempts: 3,
		LastError:   "boom",
		Record: controlgov.Record{
			Kind:      controlgov.KindSecurityEvent,
			EventID:   "evt-redrive-1",
			EventType: eventbus.EventSecurityRiskDetected,
			RunID:     "run-redrive",
			SessionID: "sess-redrive",
		},
	})
	mustEnqueueGovernanceDelivery(t, ctx, store, controlgov.DeliveryEntry{
		ID:          "gdel-102",
		AdapterName: "audit-hub",
		Status:      controlgov.DeliveryStatusDelivered,
		Attempts:    1,
		MaxAttempts: 3,
		Record: controlgov.Record{
			Kind:      controlgov.KindSecurityEvent,
			EventID:   "evt-redrive-2",
			EventType: eventbus.EventSecurityRiskDetected,
			RunID:     "run-redrive",
			SessionID: "sess-redrive",
		},
	})

	result, err := svc.RedriveGovernanceDeliveries(ctx, GovernanceRedriveRequest{
		Filter: GovernanceDeliveryFilter{
			Status: controlplane.GovernanceDeliveryStatusDeadLetter,
		},
	})
	if err != nil {
		t.Fatalf("RedriveGovernanceDeliveries() error = %v", err)
	}
	if result.Updated != 1 || result.Skipped != 0 {
		t.Fatalf("result = %#v", result)
	}

	item, err := svc.GetGovernanceDelivery(ctx, "gdel-101")
	if err != nil {
		t.Fatalf("GetGovernanceDelivery() error = %v", err)
	}
	if item.Status != controlplane.GovernanceDeliveryStatusPending {
		t.Fatalf("item.Status = %q, want pending", item.Status)
	}
	if item.Attempts != 0 {
		t.Fatalf("item.Attempts = %d, want 0", item.Attempts)
	}
	if item.LastError != "" {
		t.Fatalf("item.LastError = %q, want empty", item.LastError)
	}
}

func TestGetGovernanceDeliveryHealth(t *testing.T) {
	t.Parallel()

	svc, store := newGovernanceDeliveryTestService()
	ctx := context.Background()
	now := time.Now().UTC()

	mustEnqueueGovernanceDelivery(t, ctx, store, controlgov.DeliveryEntry{
		ID:            "gdel-health-1",
		AdapterName:   "audit-hub",
		Status:        controlgov.DeliveryStatusPending,
		Attempts:      2,
		MaxAttempts:   4,
		CreatedAt:     now.Add(-20 * time.Minute),
		UpdatedAt:     now.Add(-12 * time.Minute),
		NextAttemptAt: now.Add(-10 * time.Minute),
		Record: controlgov.Record{
			Kind:      controlgov.KindSecurityEvent,
			EventID:   "evt-health-1",
			EventType: eventbus.EventSecurityRiskDetected,
			RunID:     "run-health-1",
			SessionID: "sess-health-1",
		},
	})
	mustEnqueueGovernanceDelivery(t, ctx, store, controlgov.DeliveryEntry{
		ID:          "gdel-health-2",
		AdapterName: "jira",
		Status:      controlgov.DeliveryStatusDeadLetter,
		Attempts:    4,
		MaxAttempts: 4,
		CreatedAt:   now.Add(-35 * time.Minute),
		UpdatedAt:   now.Add(-5 * time.Minute),
		LastError:   "boom",
		Record: controlgov.Record{
			Kind:      controlgov.KindApprovalResolved,
			EventID:   "evt-health-2",
			EventType: eventbus.EventApprovalResolved,
			RunID:     "run-health-2",
			SessionID: "sess-health-2",
		},
	})

	health, err := svc.GetGovernanceDeliveryHealth(ctx, GovernanceDeliveryFilter{})
	if err != nil {
		t.Fatalf("GetGovernanceDeliveryHealth() error = %v", err)
	}
	if health.Status != governanceHealthStatusCritical {
		t.Fatalf("health.Status = %q, want %q", health.Status, governanceHealthStatusCritical)
	}
	if health.StalePendingCount != 1 {
		t.Fatalf("health.StalePendingCount = %d, want 1", health.StalePendingCount)
	}
	if health.DeadLetterCount != 1 {
		t.Fatalf("health.DeadLetterCount = %d, want 1", health.DeadLetterCount)
	}
	if len(health.AdaptersImpacted) != 2 {
		t.Fatalf("health.AdaptersImpacted = %#v, want 2 adapters", health.AdaptersImpacted)
	}
	if health.OldestPendingAt.IsZero() {
		t.Fatal("expected oldest pending timestamp")
	}
	if health.OldestDeadLetterAt.IsZero() {
		t.Fatal("expected oldest dead-letter timestamp")
	}
}

func TestListGovernanceEventViews(t *testing.T) {
	t.Parallel()

	svc, _ := newGovernanceDeliveryTestService()
	ctx := context.Background()
	_ = svc.events.(*eventbus.InMemoryBus).Publish(ctx, eventbus.Event{
		Type:      eventbus.EventGovernanceDeliveryQueued,
		RunID:     "run-evt",
		SessionID: "sess-evt",
		Attrs: map[string]any{
			"adapter_name":    "audit-hub",
			"delivery_status": "pending",
			"summary":         "queued",
		},
	})
	_ = svc.events.(*eventbus.InMemoryBus).Publish(ctx, eventbus.Event{
		Type:      eventbus.EventApprovalRequested,
		RunID:     "run-evt",
		SessionID: "sess-evt",
		Attrs: map[string]any{
			"approval_id": "appr-1",
			"status":      "pending",
			"scope": map[string]any{
				"automation_id": "automation-1",
			},
		},
	})

	items := svc.ListGovernanceEventViews(GovernanceEventFilter{
		AdapterName: "audit-hub",
		Limit:       10,
	})
	if len(items) != 1 {
		t.Fatalf("len(items) = %d, want 1", len(items))
	}
	if items[0].Type != eventbus.EventGovernanceDeliveryQueued {
		t.Fatalf("event type = %q", items[0].Type)
	}
}

func newGovernanceDeliveryTestService() (*Service, *controlgov.InMemoryDeliveryStore) {
	sessions := agent.NewInMemorySessionStore()
	runs := agent.NewInMemoryRunStore()
	bus := eventbus.NewInMemoryBus()
	store := controlgov.NewInMemoryDeliveryStore()
	controller := controlgov.NewReliableDispatcher(controlgov.DeliveryConfig{
		MaxAttempts:  3,
		BaseBackoff:  10 * time.Millisecond,
		MaxBackoff:   10 * time.Millisecond,
		PollInterval: 10 * time.Millisecond,
		BatchSize:    8,
	}, store).WithEventBus(bus)
	svc := NewService(nil, sessions, runs, nil, bus, nil).WithGovernanceDelivery(controlgov.AdaptDeliveryController(controller))
	return svc, store
}

func mustEnqueueGovernanceDelivery(t *testing.T, ctx context.Context, store *controlgov.InMemoryDeliveryStore, entry controlgov.DeliveryEntry) {
	t.Helper()
	if entry.CreatedAt.IsZero() {
		entry.CreatedAt = time.Now().UTC()
	}
	if entry.UpdatedAt.IsZero() {
		entry.UpdatedAt = entry.CreatedAt
	}
	if entry.NextAttemptAt.IsZero() && entry.Status == controlgov.DeliveryStatusPending {
		entry.NextAttemptAt = entry.CreatedAt
	}
	if _, _, err := store.Enqueue(ctx, entry); err != nil {
		t.Fatalf("Enqueue() error = %v", err)
	}
	if entry.Status != controlgov.DeliveryStatusPending {
		stored, err := store.Get(ctx, entry.ID)
		if err != nil {
			t.Fatalf("Get() error = %v", err)
		}
		stored.Status = entry.Status
		stored.Attempts = entry.Attempts
		stored.MaxAttempts = entry.MaxAttempts
		stored.LastError = entry.LastError
		stored.Record = entry.Record
		stored.UpdatedAt = entry.UpdatedAt
		if err := store.Update(ctx, stored); err != nil {
			t.Fatalf("Update() error = %v", err)
		}
	}
}

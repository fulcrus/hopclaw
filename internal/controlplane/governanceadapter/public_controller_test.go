package governanceadapter

import (
	"context"
	"testing"
	"time"

	"github.com/fulcrus/hopclaw/controlplane"
	"github.com/fulcrus/hopclaw/eventbus"
)

func TestAdaptDeliveryControllerProjectsPublicEntries(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := NewInMemoryDeliveryStore()
	dispatcher := NewReliableDispatcher(DeliveryConfig{
		MaxAttempts:  3,
		BaseBackoff:  10 * time.Millisecond,
		MaxBackoff:   10 * time.Millisecond,
		PollInterval: 10 * time.Millisecond,
		BatchSize:    4,
	}, store)
	controller := AdaptDeliveryController(dispatcher)

	entry := DeliveryEntry{
		ID:          "gdel-public-1",
		AdapterName: "audit-hub",
		Status:      DeliveryStatusDeadLetter,
		Attempts:    3,
		MaxAttempts: 3,
		LastError:   "boom",
		Record: Record{
			Kind:      KindSecurityEvent,
			EventID:   "evt-public-1",
			EventType: eventbus.EventSecurityRiskDetected,
			RunID:     "run-public-1",
			SessionID: "sess-public-1",
			Summary:   "dead letter",
			Governance: GovernanceContext{
				EffectiveConfigSnapshotID: "ecs-1",
				ToolNames:                 []string{"scan", "notify"},
			},
		},
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
	if err := store.Update(ctx, stored); err != nil {
		t.Fatalf("Update() error = %v", err)
	}

	item, err := controller.GetDelivery(ctx, entry.ID)
	if err != nil {
		t.Fatalf("GetDelivery() error = %v", err)
	}
	if item.Status != controlplane.GovernanceDeliveryStatusDeadLetter {
		t.Fatalf("Status = %q, want dead_letter", item.Status)
	}
	if item.Record.Kind != controlplane.GovernanceKindSecurityEvent {
		t.Fatalf("Kind = %q, want security_event", item.Record.Kind)
	}
	if item.Record.EffectiveConfigSnapshotID != "ecs-1" {
		t.Fatalf("EffectiveConfigSnapshotID = %q, want ecs-1", item.Record.EffectiveConfigSnapshotID)
	}

	result, err := controller.Redrive(ctx, []string{entry.ID}, controlplane.GovernanceDeliveryRedriveOptions{
		ResetAttempts: true,
		ClearError:    true,
	})
	if err != nil {
		t.Fatalf("Redrive() error = %v", err)
	}
	if len(result) != 1 || result[0].Status != controlplane.GovernanceDeliveryStatusPending {
		t.Fatalf("Redrive result = %#v", result)
	}

	items, err := controller.ListDeliveries(ctx, controlplane.GovernanceDeliveryListFilter{
		Status:      controlplane.GovernanceDeliveryStatusPending,
		AdapterName: "audit-hub",
	})
	if err != nil {
		t.Fatalf("ListDeliveries() error = %v", err)
	}
	if len(items) != 1 || items[0].AdapterName != "audit-hub" {
		t.Fatalf("ListDeliveries() = %#v", items)
	}
}

package audit

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/fulcrus/hopclaw/eventbus"
	_ "modernc.org/sqlite"
)

type scriptedDeliverySink struct {
	name string

	mu        sync.Mutex
	failures  int
	delivered []eventbus.Event
}

func (s *scriptedDeliverySink) Name() string { return s.name }

func (s *scriptedDeliverySink) Deliver(_ context.Context, event eventbus.Event) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.failures > 0 {
		s.failures--
		return errors.New("temporary sink failure")
	}
	s.delivered = append(s.delivered, cloneEvent(event))
	return nil
}

func (s *scriptedDeliverySink) deliveryCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.delivered)
}

func TestReliableDispatcherRetriesAndDelivers(t *testing.T) {
	t.Parallel()

	store := NewInMemoryDeliveryStore()
	sink := &scriptedDeliverySink{name: "siem", failures: 1}
	dispatcher := NewReliableDispatcher(DeliveryConfig{
		MaxAttempts:  3,
		BaseBackoff:  10 * time.Millisecond,
		MaxBackoff:   10 * time.Millisecond,
		PollInterval: 5 * time.Millisecond,
		BatchSize:    4,
	}, store, sink)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	dispatcher.Start(ctx)
	defer dispatcher.Stop()

	if err := dispatcher.Handle(ctx, eventbus.Event{
		ID:        "evt-1",
		Type:      eventbus.EventRunCompleted,
		RunID:     "run-1",
		SessionID: "sess-1",
		Time:      time.Now().UTC(),
	}); err != nil {
		t.Fatalf("Handle() error = %v", err)
	}

	entry := waitForDeliveryStatus(t, dispatcher, DeliveryListFilter{SinkName: "siem"}, DeliveryStatusDelivered)
	if entry.Attempts != 2 {
		t.Fatalf("Attempts = %d, want 2", entry.Attempts)
	}
	if sink.deliveryCount() != 1 {
		t.Fatalf("deliveryCount = %d, want 1", sink.deliveryCount())
	}
}

func TestReliableDispatcherDeadLetterAndRedrive(t *testing.T) {
	t.Parallel()

	store := NewInMemoryDeliveryStore()
	sink := &scriptedDeliverySink{name: "siem", failures: 3}
	dispatcher := NewReliableDispatcher(DeliveryConfig{
		MaxAttempts:  2,
		BaseBackoff:  10 * time.Millisecond,
		MaxBackoff:   10 * time.Millisecond,
		PollInterval: 5 * time.Millisecond,
		BatchSize:    4,
	}, store, sink)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	dispatcher.Start(ctx)
	defer dispatcher.Stop()

	if err := dispatcher.Handle(ctx, eventbus.Event{
		ID:   "evt-2",
		Type: eventbus.EventRunFailed,
		Time: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("Handle() error = %v", err)
	}

	entry := waitForDeliveryStatus(t, dispatcher, DeliveryListFilter{SinkName: "siem"}, DeliveryStatusDeadLetter)
	if entry.Attempts != 2 {
		t.Fatalf("Attempts = %d, want 2", entry.Attempts)
	}

	sink.mu.Lock()
	sink.failures = 0
	sink.mu.Unlock()

	updated, err := dispatcher.Redrive(ctx, []string{entry.ID}, DeliveryRedriveOptions{
		ResetAttempts: true,
		ClearError:    true,
	})
	if err != nil {
		t.Fatalf("Redrive() error = %v", err)
	}
	if len(updated) != 1 {
		t.Fatalf("len(updated) = %d, want 1", len(updated))
	}

	redriven := waitForDeliveryStatus(t, dispatcher, DeliveryListFilter{SinkName: "siem"}, DeliveryStatusDelivered)
	if redriven.Attempts != 1 {
		t.Fatalf("Attempts after redrive = %d, want 1", redriven.Attempts)
	}
}

func TestSQLiteDeliveryStoreDedupesBySinkAndEvent(t *testing.T) {
	t.Parallel()

	db := openAuditSQLiteDB(t)
	deliveryStore, err := NewSQLiteDeliveryStore(db)
	if err != nil {
		t.Fatalf("NewSQLiteDeliveryStore() error = %v", err)
	}
	now := time.Now().UTC()
	first, err := deliveryStore.Enqueue(context.Background(), DeliveryEntry{
		SinkName:      "siem",
		EventID:       "evt-3",
		EventType:     eventbus.EventRunCompleted,
		Event:         eventbus.Event{ID: "evt-3", Type: eventbus.EventRunCompleted, Time: now},
		Status:        DeliveryStatusPending,
		MaxAttempts:   3,
		NextAttemptAt: now,
		CreatedAt:     now,
		UpdatedAt:     now,
	})
	if err != nil {
		t.Fatalf("Enqueue(first) error = %v", err)
	}
	second, err := deliveryStore.Enqueue(context.Background(), DeliveryEntry{
		SinkName:      "siem",
		EventID:       "evt-3",
		EventType:     eventbus.EventRunCompleted,
		Event:         eventbus.Event{ID: "evt-3", Type: eventbus.EventRunCompleted, Time: now},
		Status:        DeliveryStatusPending,
		MaxAttempts:   3,
		NextAttemptAt: now,
		CreatedAt:     now,
		UpdatedAt:     now,
	})
	if err != nil {
		t.Fatalf("Enqueue(second) error = %v", err)
	}
	if first.ID == "" || second.ID == "" || first.ID != second.ID {
		t.Fatalf("deduped IDs = %q, %q", first.ID, second.ID)
	}
}

func TestInMemoryDeliveryStoreDoesNotDeduplicateEmptyEventID(t *testing.T) {
	t.Parallel()

	deliveryStore := NewInMemoryDeliveryStore()
	now := time.Now().UTC()
	first, err := deliveryStore.Enqueue(context.Background(), DeliveryEntry{
		SinkName:      "siem",
		Event:         eventbus.Event{Type: eventbus.EventRunCompleted, Time: now},
		Status:        DeliveryStatusPending,
		MaxAttempts:   3,
		NextAttemptAt: now,
		CreatedAt:     now,
		UpdatedAt:     now,
	})
	if err != nil {
		t.Fatalf("Enqueue(first) error = %v", err)
	}
	second, err := deliveryStore.Enqueue(context.Background(), DeliveryEntry{
		SinkName:      "siem",
		Event:         eventbus.Event{Type: eventbus.EventRunCompleted, Time: now},
		Status:        DeliveryStatusPending,
		MaxAttempts:   3,
		NextAttemptAt: now,
		CreatedAt:     now,
		UpdatedAt:     now,
	})
	if err != nil {
		t.Fatalf("Enqueue(second) error = %v", err)
	}
	if first.ID == second.ID {
		t.Fatalf("IDs should differ when event_id is empty: %q", first.ID)
	}
}

func openAuditSQLiteDB(t *testing.T) *sql.DB {
	t.Helper()

	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "audit.db"))
	if err != nil {
		t.Fatalf("sql.Open() error = %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func waitForDeliveryStatus(t *testing.T, controller DeliveryController, filter DeliveryListFilter, status DeliveryStatus) *DeliveryEntry {
	t.Helper()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		items, err := controller.ListDeliveries(context.Background(), filter)
		if err != nil {
			t.Fatalf("ListDeliveries() error = %v", err)
		}
		for _, item := range items {
			if item != nil && item.Status == status {
				return item
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for delivery status %q", status)
	return nil
}

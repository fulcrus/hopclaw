package governanceadapter

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/fulcrus/hopclaw/eventbus"
	"github.com/fulcrus/hopclaw/store"
)

type blockingDeliveryStore struct {
	*InMemoryDeliveryStore
	entered chan struct{}
	release chan struct{}
}

func (s *blockingDeliveryStore) ListDue(ctx context.Context, before time.Time, limit int) ([]*DeliveryEntry, error) {
	select {
	case <-s.entered:
	default:
		close(s.entered)
	}
	<-ctx.Done()
	<-s.release
	return nil, ctx.Err()
}

func TestReliableDispatcherRetriesThenSucceeds(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	store := NewInMemoryDeliveryStore()
	bus := eventbus.NewInMemoryBus()
	adapter := &scriptedGovernanceAdapter{
		name: "audit-hub",
		errs: []error{errors.New("temporary failure")},
	}
	dispatcher := NewReliableDispatcher(DeliveryConfig{
		MaxAttempts:  3,
		BaseBackoff:  10 * time.Millisecond,
		MaxBackoff:   10 * time.Millisecond,
		PollInterval: 5 * time.Millisecond,
		BatchSize:    8,
	}, store, adapter).WithEventBus(bus)
	dispatcher.Start(ctx)
	defer dispatcher.Stop()

	err := dispatcher.Handle(ctx, eventbus.Event{
		ID:        "evt-retry-1",
		Type:      eventbus.EventSecurityRiskDetected,
		RunID:     "run-retry-1",
		SessionID: "sess-retry-1",
		Attrs: map[string]any{
			"severity": "high",
			"summary":  "retry delivery",
		},
	})
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}

	entry := waitForDeliveryState(t, store, "audit-hub", DeliveryStatusDelivered)
	if entry.Attempts != 2 {
		t.Fatalf("Attempts = %d, want 2", entry.Attempts)
	}
	if adapter.Calls() != 2 {
		t.Fatalf("adapter calls = %d, want 2", adapter.Calls())
	}
	assertDeliveryEventTypes(t, bus,
		eventbus.EventGovernanceDeliveryQueued,
		eventbus.EventGovernanceDeliveryRetryScheduled,
		eventbus.EventGovernanceDeliveryDelivered,
	)
}

func TestReliableDispatcherDeadLettersAfterMaxAttempts(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	store := NewInMemoryDeliveryStore()
	bus := eventbus.NewInMemoryBus()
	adapter := &scriptedGovernanceAdapter{
		name: "audit-hub",
		errs: []error{
			errors.New("first failure"),
			errors.New("second failure"),
			errors.New("extra failure should not happen"),
		},
	}
	dispatcher := NewReliableDispatcher(DeliveryConfig{
		MaxAttempts:  2,
		BaseBackoff:  10 * time.Millisecond,
		MaxBackoff:   10 * time.Millisecond,
		PollInterval: 5 * time.Millisecond,
		BatchSize:    8,
	}, store, adapter).WithEventBus(bus)
	dispatcher.Start(ctx)
	defer dispatcher.Stop()

	err := dispatcher.Handle(ctx, eventbus.Event{
		ID:   "evt-dead-1",
		Type: eventbus.EventSecurityRiskDetected,
		Attrs: map[string]any{
			"severity": "high",
			"summary":  "dead letter delivery",
		},
	})
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}

	entry := waitForDeliveryState(t, store, "audit-hub", DeliveryStatusDeadLetter)
	if entry.Attempts != 2 {
		t.Fatalf("Attempts = %d, want 2", entry.Attempts)
	}
	if adapter.Calls() != 2 {
		t.Fatalf("adapter calls = %d, want 2", adapter.Calls())
	}
	assertDeliveryEventTypes(t, bus,
		eventbus.EventGovernanceDeliveryQueued,
		eventbus.EventGovernanceDeliveryRetryScheduled,
		eventbus.EventGovernanceDeliveryDeadLettered,
	)
}

func TestReliableDispatcherReplaysPendingOutboxOnStart(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	store := NewInMemoryDeliveryStore()
	entry, created, err := store.Enqueue(ctx, DeliveryEntry{
		AdapterName: "audit-hub",
		Status:      DeliveryStatusPending,
		Record: Record{
			Kind:      KindSecurityEvent,
			EventID:   "evt-replay-1",
			EventType: eventbus.EventSecurityRiskDetected,
			RunID:     "run-replay-1",
			SessionID: "sess-replay-1",
			Summary:   "replay delivery",
		},
		MaxAttempts:   3,
		NextAttemptAt: time.Now().UTC().Add(-time.Second),
	})
	if err != nil {
		t.Fatalf("Enqueue() error = %v", err)
	}
	if !created {
		t.Fatal("expected replay entry to be created")
	}

	adapter := &scriptedGovernanceAdapter{name: "audit-hub"}
	dispatcher := NewReliableDispatcher(DeliveryConfig{
		MaxAttempts:  3,
		BaseBackoff:  10 * time.Millisecond,
		MaxBackoff:   10 * time.Millisecond,
		PollInterval: 5 * time.Millisecond,
		BatchSize:    8,
	}, store, adapter)
	dispatcher.Start(ctx)
	defer dispatcher.Stop()

	delivered := waitForDeliveryIDState(t, store, entry.ID, DeliveryStatusDelivered)
	if delivered.Attempts != 1 {
		t.Fatalf("Attempts = %d, want 1", delivered.Attempts)
	}
	if adapter.Calls() != 1 {
		t.Fatalf("adapter calls = %d, want 1", adapter.Calls())
	}
}

func TestReliableDispatcherStopWaitsForLoopExit(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	store := &blockingDeliveryStore{
		InMemoryDeliveryStore: NewInMemoryDeliveryStore(),
		entered:               make(chan struct{}),
		release:               make(chan struct{}),
	}
	dispatcher := NewReliableDispatcher(DeliveryConfig{
		PollInterval: time.Hour,
	}, store)
	dispatcher.Start(ctx)

	select {
	case <-store.entered:
	case <-time.After(time.Second):
		t.Fatal("dispatcher loop did not enter ListDue")
	}

	stopped := make(chan struct{})
	go func() {
		dispatcher.Stop()
		close(stopped)
	}()

	select {
	case <-stopped:
		t.Fatal("Stop returned before the loop exited")
	case <-time.After(25 * time.Millisecond):
	}

	close(store.release)

	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("Stop did not return after the loop exited")
	}
}

func TestDeliveryStoresDeduplicateByIdempotencyKey(t *testing.T) {
	t.Parallel()

	checkStore := func(t *testing.T, name string, store DeliveryStore, inspect func(*testing.T)) {
		t.Helper()
		ctx := context.Background()
		first, created, err := store.Enqueue(ctx, DeliveryEntry{
			AdapterName:    "audit-hub",
			IdempotencyKey: "delivery:idem-1",
			Status:         DeliveryStatusPending,
			Record: Record{
				Kind:      KindSecurityEvent,
				EventID:   "evt-store-1",
				EventType: eventbus.EventSecurityRiskDetected,
			},
		})
		if err != nil {
			t.Fatalf("%s Enqueue(first) error = %v", name, err)
		}
		if !created {
			t.Fatalf("%s first enqueue should create a record", name)
		}
		second, created, err := store.Enqueue(ctx, DeliveryEntry{
			AdapterName:    "audit-hub",
			IdempotencyKey: "delivery:idem-1",
			Status:         DeliveryStatusPending,
			Record: Record{
				Kind:      KindSecurityEvent,
				EventID:   "evt-store-2",
				EventType: eventbus.EventSecurityRiskDetected,
			},
		})
		if err != nil {
			t.Fatalf("%s Enqueue(second) error = %v", name, err)
		}
		if created {
			t.Fatalf("%s second enqueue should reuse the existing outbox row", name)
		}
		if first.ID != second.ID {
			t.Fatalf("%s IDs = %q / %q, want same idempotent row", name, first.ID, second.ID)
		}
		if strings.TrimSpace(second.IdempotencyKey) != "delivery:idem-1" {
			t.Fatalf("%s idempotency key = %q, want delivery:idem-1", name, second.IdempotencyKey)
		}
		items, err := store.List(ctx, DeliveryListFilter{AdapterName: "audit-hub"})
		if err != nil {
			t.Fatalf("%s List() error = %v", name, err)
		}
		if len(items) != 1 {
			t.Fatalf("%s len(items) = %d, want 1", name, len(items))
		}
		if inspect != nil {
			inspect(t)
		}
	}

	t.Run("memory", func(t *testing.T) {
		checkStore(t, "memory", NewInMemoryDeliveryStore(), nil)
	})

	t.Run("sqlite", func(t *testing.T) {
		db, err := store.OpenDB(filepath.Join(t.TempDir(), "delivery.db"))
		if err != nil {
			t.Fatalf("OpenDB() error = %v", err)
		}
		t.Cleanup(func() { _ = db.Close() })
		sqliteStore, err := NewSQLiteDeliveryStore(db)
		if err != nil {
			t.Fatalf("NewSQLiteDeliveryStore() error = %v", err)
		}
		checkStore(t, "sqlite", sqliteStore, func(t *testing.T) {
			var count int
			if err := db.QueryRow(`SELECT COUNT(*) FROM delivery_outbox`).Scan(&count); err != nil {
				t.Fatalf("query delivery_outbox count: %v", err)
			}
			if count != 1 {
				t.Fatalf("delivery_outbox count = %d, want 1", count)
			}
		})
	})
}

type scriptedGovernanceAdapter struct {
	name string

	mu      sync.Mutex
	errs    []error
	records []Record
	calls   int
}

func (a *scriptedGovernanceAdapter) Name() string { return a.name }

func (a *scriptedGovernanceAdapter) HandleGovernanceRecord(_ context.Context, record Record) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.calls++
	a.records = append(a.records, record)
	if idx := a.calls - 1; idx >= 0 && idx < len(a.errs) && a.errs[idx] != nil {
		return a.errs[idx]
	}
	return nil
}

func (a *scriptedGovernanceAdapter) Calls() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.calls
}

func waitForDeliveryState(t *testing.T, store DeliveryStore, adapterName string, status DeliveryStatus) *DeliveryEntry {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		items, err := store.List(context.Background(), DeliveryListFilter{
			AdapterName: adapterName,
			Status:      status,
			Limit:       1,
		})
		if err != nil {
			t.Fatalf("List() error = %v", err)
		}
		if len(items) > 0 && items[0] != nil {
			return items[0]
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for delivery status %q", status)
	return nil
}

func waitForDeliveryIDState(t *testing.T, store DeliveryStore, id string, status DeliveryStatus) *DeliveryEntry {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		entry, err := store.Get(context.Background(), id)
		if err != nil {
			t.Fatalf("Get() error = %v", err)
		}
		if entry != nil && entry.Status == status {
			return entry
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for delivery %s status %q", id, status)
	return nil
}

func assertDeliveryEventTypes(t *testing.T, bus *eventbus.InMemoryBus, want ...eventbus.EventType) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		events := bus.Snapshot()
		seen := make([]eventbus.EventType, 0, len(events))
		for _, event := range events {
			switch event.Type {
			case eventbus.EventGovernanceDeliveryQueued,
				eventbus.EventGovernanceDeliveryRetryScheduled,
				eventbus.EventGovernanceDeliveryDelivered,
				eventbus.EventGovernanceDeliveryDeadLettered:
				seen = append(seen, event.Type)
			}
		}
		if len(seen) == len(want) {
			matched := true
			for i := range want {
				if seen[i] != want[i] {
					matched = false
					break
				}
			}
			if matched {
				return
			}
			t.Fatalf("delivery lifecycle events = %v, want %v", seen, want)
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for delivery lifecycle events %v", want)
}

var _ Adapter = (*scriptedGovernanceAdapter)(nil)
var _ NamedAdapter = (*scriptedGovernanceAdapter)(nil)

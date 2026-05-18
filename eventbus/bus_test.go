package eventbus

import (
	"context"
	"errors"
	"testing"
	"time"
)

type collectingSink struct {
	events []Event
}

func (s *collectingSink) Handle(_ context.Context, event Event) error {
	s.events = append(s.events, event)
	return nil
}

type closingSink struct {
	sub *Subscription
}

func (s *closingSink) Handle(_ context.Context, _ Event) error {
	s.sub.Close()
	return nil
}

type failingSink struct {
	err error
}

func (s *failingSink) Handle(_ context.Context, _ Event) error {
	return s.err
}

func TestInMemoryBusPublishesAndStoresEvents(t *testing.T) {
	t.Parallel()

	bus := NewInMemoryBus()
	sink := &collectingSink{}
	bus.Subscribe(sink)

	if err := bus.Publish(context.Background(), Event{
		Type:      EventRunStarted,
		RunID:     "run-1",
		SessionID: "sess-1",
	}); err != nil {
		t.Fatalf("Publish() error = %v", err)
	}
	if len(sink.events) != 1 {
		t.Fatalf("len(sink.events) = %d", len(sink.events))
	}
	events := bus.Snapshot()
	if len(events) != 1 {
		t.Fatalf("len(events) = %d", len(events))
	}
	if events[0].ID == "" {
		t.Fatal("expected event id")
	}
}

func TestInMemoryBusDropsOldestEventsWhenLimitExceeded(t *testing.T) {
	t.Parallel()

	bus := NewInMemoryBusWithLimit(2)
	for i := 0; i < 3; i++ {
		if err := bus.Publish(context.Background(), Event{
			Type:  EventRunCompleted,
			RunID: "run",
		}); err != nil {
			t.Fatalf("Publish(%d) error = %v", i, err)
		}
	}

	events := bus.Snapshot()
	if len(events) != 2 {
		t.Fatalf("len(events) = %d, want 2", len(events))
	}
	if events[0].ID != "evt-000002" || events[1].ID != "evt-000003" {
		t.Fatalf("event ids = %#v", events)
	}
}

func TestSnapshotSinceWithStatusReturnsForwardPage(t *testing.T) {
	t.Parallel()

	bus := NewInMemoryBusWithLimit(10)
	for i := 0; i < 5; i++ {
		if err := bus.Publish(context.Background(), Event{Type: EventRunCompleted}); err != nil {
			t.Fatalf("Publish(%d) error = %v", i, err)
		}
	}

	result := bus.SnapshotSinceWithStatus("evt-000001", 2)
	if result.Status != CursorOK {
		t.Fatalf("result.Status = %q", result.Status)
	}
	if len(result.Events) != 2 {
		t.Fatalf("len(result.Events) = %d", len(result.Events))
	}
	if result.Events[0].ID != "evt-000002" || result.Events[1].ID != "evt-000003" {
		t.Fatalf("event ids = %#v", result.Events)
	}
	if result.NextCursor != "evt-000003" {
		t.Fatalf("result.NextCursor = %q", result.NextCursor)
	}
}

func TestSnapshotSinceWithStatusExpiredStartsFromEarliestAvailable(t *testing.T) {
	t.Parallel()

	bus := NewInMemoryBusWithLimit(3)
	for i := 0; i < 4; i++ {
		if err := bus.Publish(context.Background(), Event{Type: EventRunCompleted}); err != nil {
			t.Fatalf("Publish(%d) error = %v", i, err)
		}
	}

	result := bus.SnapshotSinceWithStatus("evt-000001", 2)
	if result.Status != CursorExpired {
		t.Fatalf("result.Status = %q", result.Status)
	}
	if len(result.Events) != 2 {
		t.Fatalf("len(result.Events) = %d", len(result.Events))
	}
	if result.Events[0].ID != "evt-000002" || result.Events[1].ID != "evt-000003" {
		t.Fatalf("event ids = %#v", result.Events)
	}
	if result.NextCursor != "evt-000003" {
		t.Fatalf("result.NextCursor = %q", result.NextCursor)
	}
}

func TestPublishDoesNotPanicWhenSubscriberClosesDuringFanout(t *testing.T) {
	t.Parallel()

	bus := NewInMemoryBus()
	sub := bus.SubscribeChannel(1)
	bus.Subscribe(&closingSink{sub: sub})

	if err := bus.Publish(context.Background(), Event{Type: EventRunCompleted}); err != nil {
		t.Fatalf("Publish() error = %v", err)
	}

	select {
	case _, ok := <-sub.Events():
		if ok {
			t.Fatal("expected subscription channel to be closed")
		}
	default:
		t.Fatal("subscription channel should be closed")
	}
}

func TestSubscriptionCloseIsIdempotent(t *testing.T) {
	t.Parallel()

	bus := NewInMemoryBus()
	sub := bus.SubscribeChannel(1)
	sub.Close()
	sub.Close()
}

func TestSnapshotSinceWithoutCursorReturnsLatestTail(t *testing.T) {
	t.Parallel()

	bus := NewInMemoryBusWithLimit(10)
	for i := 0; i < 5; i++ {
		if err := bus.Publish(context.Background(), Event{Type: EventRunCompleted}); err != nil {
			t.Fatalf("Publish(%d) error = %v", i, err)
		}
	}

	events := bus.SnapshotSince("", 2)
	if len(events) != 2 {
		t.Fatalf("len(events) = %d", len(events))
	}
	if events[0].ID != "evt-000004" || events[1].ID != "evt-000005" {
		t.Fatalf("event ids = %#v", events)
	}
}

func TestPublishContinuesAfterSinkError(t *testing.T) {
	t.Parallel()

	bus := NewInMemoryBus()
	wantErr := errors.New("sink failed")
	bus.Subscribe(&failingSink{err: wantErr})
	sink := &collectingSink{}
	bus.Subscribe(sink)

	err := bus.Publish(context.Background(), Event{Type: EventRunCompleted})
	if !errors.Is(err, wantErr) {
		t.Fatalf("Publish() error = %v, want wrapped %v", err, wantErr)
	}
	if len(sink.events) != 1 {
		t.Fatalf("len(sink.events) = %d, want 1 despite earlier sink failure", len(sink.events))
	}
}

func TestSubscriptionTracksDroppedEvents(t *testing.T) {
	t.Parallel()

	bus := NewInMemoryBus()
	sub := bus.SubscribeChannel(1)

	if err := bus.Publish(context.Background(), Event{Type: EventRunCompleted}); err != nil {
		t.Fatalf("Publish(first) error = %v", err)
	}
	if err := bus.Publish(context.Background(), Event{Type: EventRunCompleted}); err != nil {
		t.Fatalf("Publish(second) error = %v", err)
	}
	if got := sub.DroppedCount(); got != 1 {
		t.Fatalf("DroppedCount() = %d, want 1", got)
	}
	if got := sub.LastDropTime(); got.IsZero() {
		t.Fatal("LastDropTime() = zero, want drop timestamp")
	} else if time.Since(got) > time.Minute {
		t.Fatalf("LastDropTime() = %v, want recent timestamp", got)
	}
}

package cron

import (
	"fmt"
	"testing"
)

func TestEventQueue_EnqueueAndDrain(t *testing.T) {
	t.Parallel()
	q := NewEventQueue()

	q.Enqueue(SystemEvent{Text: "hello", SessionKey: "s1"})
	q.Enqueue(SystemEvent{Text: "world", SessionKey: "s1"})
	q.Enqueue(SystemEvent{Text: "other", SessionKey: "s2"})

	if q.Len() != 3 {
		t.Fatalf("expected 3 events, got %d", q.Len())
	}

	events := q.Drain("s1")
	if len(events) != 2 {
		t.Fatalf("expected 2 events for s1, got %d", len(events))
	}
	if events[0].Text != "hello" || events[1].Text != "world" {
		t.Fatalf("unexpected event texts: %v", events)
	}

	// Drain again should return nil.
	if q.Drain("s1") != nil {
		t.Fatal("expected nil after second drain")
	}

	// s2 should still have its event.
	if q.Len() != 1 {
		t.Fatalf("expected 1 remaining event, got %d", q.Len())
	}
}

func TestEventQueue_PreservesRepeatedTexts(t *testing.T) {
	t.Parallel()
	q := NewEventQueue()

	q.Enqueue(SystemEvent{Text: "dup", SessionKey: "s1"})
	q.Enqueue(SystemEvent{Text: "dup", SessionKey: "s1"})
	q.Enqueue(SystemEvent{Text: "dup", SessionKey: "s1"})

	events := q.Drain("s1")
	if len(events) != 3 {
		t.Fatalf("expected repeated texts to be preserved, got %d events", len(events))
	}
}

func TestEventQueue_Overflow(t *testing.T) {
	t.Parallel()
	q := NewEventQueue()

	for i := 0; i < maxEventsPerSession+5; i++ {
		q.Enqueue(SystemEvent{
			Text:       fmt.Sprintf("event-%02d", i),
			SessionKey: "s1",
		})
	}

	events := q.Drain("s1")
	if len(events) != maxEventsPerSession {
		t.Fatalf("expected %d events after overflow, got %d", maxEventsPerSession, len(events))
	}
}

func TestEventQueue_EmptyInputs(t *testing.T) {
	t.Parallel()
	q := NewEventQueue()

	q.Enqueue(SystemEvent{Text: "", SessionKey: "s1"})
	q.Enqueue(SystemEvent{Text: "hello", SessionKey: ""})

	if q.Len() != 0 {
		t.Fatalf("expected 0 events for empty inputs, got %d", q.Len())
	}
}

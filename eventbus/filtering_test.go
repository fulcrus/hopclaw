package eventbus

import (
	"context"
	"testing"
)

type recordingSink struct {
	events []Event
}

func (s *recordingSink) Handle(_ context.Context, event Event) error {
	s.events = append(s.events, event)
	return nil
}

func TestFilteredSinkSkipsStreamingDeltasByDefault(t *testing.T) {
	sink := &recordingSink{}
	filtered := FilteredSink{
		Inner:  sink,
		Filter: PersistDefaultRuntimeEvent,
	}
	ctx := context.Background()
	events := []Event{
		{Type: EventModelTextDelta},
		{Type: EventModelReasoningDelta},
		{Type: EventModelStreamComplete},
		{Type: EventToolExecuted},
	}
	for _, event := range events {
		if err := filtered.Handle(ctx, event); err != nil {
			t.Fatalf("Handle() error = %v", err)
		}
	}
	if len(sink.events) != 2 {
		t.Fatalf("persisted events = %d, want 2", len(sink.events))
	}
	if sink.events[0].Type != EventModelStreamComplete {
		t.Fatalf("first persisted event = %q", sink.events[0].Type)
	}
	if sink.events[1].Type != EventToolExecuted {
		t.Fatalf("second persisted event = %q", sink.events[1].Type)
	}
}

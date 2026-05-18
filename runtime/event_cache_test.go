package runtime

import (
	"context"
	"testing"

	"github.com/fulcrus/hopclaw/agent"
	"github.com/fulcrus/hopclaw/eventbus"
)

type countingReplayReader struct {
	events []eventbus.Event
	calls  int
}

func (r *countingReplayReader) ReplayContext(_ context.Context) ([]eventbus.Event, error) {
	r.calls++
	out := make([]eventbus.Event, len(r.events))
	copy(out, r.events)
	return out, nil
}

func TestEventSnapshotCachesReplayReaderWithinTTL(t *testing.T) {
	t.Parallel()

	reader := &countingReplayReader{events: []eventbus.Event{{ID: "evt-1", Type: eventbus.EventRunCompleted}}}
	svc := NewService(nil, agent.NewInMemorySessionStore(), agent.NewInMemoryRunStore(), nil, eventbus.NewInMemoryBus(), nil).
		WithEventReader(reader)

	first := svc.EventSnapshot()
	second := svc.EventSnapshot()
	if reader.calls != 1 {
		t.Fatalf("ReplayContext() calls = %d, want 1", reader.calls)
	}
	if len(first) != 1 || len(second) != 1 {
		t.Fatalf("unexpected snapshots: first=%v second=%v", first, second)
	}
}

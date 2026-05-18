package runtime

import (
	"context"

	"github.com/fulcrus/hopclaw/eventbus"
)

// EventReplayReader provides durable event replay for runtime read models.
// Production wiring should prefer this over transient in-memory snapshots.
type EventReplayReader interface {
	ReplayContext(ctx context.Context) ([]eventbus.Event, error)
}

// EventReplaySinceReader provides durable cursor-based event replay with
// caller cancellation support.
type EventReplaySinceReader interface {
	ReplaySinceContext(ctx context.Context, sinceID string, limit int) ([]eventbus.Event, error)
}

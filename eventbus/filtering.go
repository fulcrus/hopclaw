package eventbus

import "context"

// FilterFunc decides whether an event should be forwarded to the wrapped sink.
type FilterFunc func(event Event) bool

// FilteredSink wraps a sink with a predicate so noisy or low-value events can
// be excluded from durable persistence while still flowing through the in-memory
// bus and live subscribers.
type FilteredSink struct {
	Inner  Sink
	Filter FilterFunc
}

func (s FilteredSink) Handle(ctx context.Context, event Event) error {
	if s.Inner == nil {
		return nil
	}
	if s.Filter != nil && !s.Filter(event) {
		return nil
	}
	return s.Inner.Handle(ctx, event)
}

// PersistDefaultRuntimeEvent returns true for durable runtime events that are
// useful for audit, replay, delivery, and operator inspection. High-frequency
// streaming deltas are intentionally excluded to avoid heavy non-business disk
// churn; callers that need raw token-by-token capture should use the wire log.
func PersistDefaultRuntimeEvent(event Event) bool {
	switch event.Type {
	case EventModelTextDelta, EventModelReasoningDelta:
		return false
	default:
		return true
	}
}

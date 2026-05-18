package usage

import (
	"context"
)

// EventPublisher is the interface for publishing events to an event bus.
type EventPublisher interface {
	Publish(ctx context.Context, event any) error
}

// Tracker records model usage and optionally publishes usage events.
type Tracker struct {
	store Store
	bus   EventPublisher
}

// NewTracker creates a Tracker backed by the given store.
func NewTracker(store Store) *Tracker {
	return &Tracker{store: store}
}

// WithEventPublisher attaches an event publisher for usage event emission.
func (t *Tracker) WithEventPublisher(bus EventPublisher) *Tracker {
	t.bus = bus
	return t
}

// TrackModelCall records a usage record and optionally publishes an event.
// If the record has no cost estimate, one is computed automatically.
func (t *Tracker) TrackModelCall(ctx context.Context, rec Record) error {
	if rec.CostEstimate == 0 {
		rec.CostEstimate = EstimateCost(rec.Model, rec.PromptTokens, rec.CompletionTokens)
	}
	return t.store.Record(ctx, rec)
}

// TrackToolExecution records a tool execution usage record.
func (t *Tracker) TrackToolExecution(ctx context.Context, rec Record) error {
	rec.RecordType = RecordTypeToolExecution
	return t.store.Record(ctx, rec)
}

// Store returns the underlying usage store for queries and summaries.
func (t *Tracker) Store() Store {
	return t.store
}

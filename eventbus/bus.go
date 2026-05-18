package eventbus

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/fulcrus/hopclaw/internal/metrics"
)

// EventType identifies one runtime or control-plane event emitted on the bus.
type EventType string

const (
	EventRunSubmitted        EventType = "run.submitted"
	EventRunPreflightUpdated EventType = "run.preflight_updated"
	EventRunWaitingInput     EventType = "run.waiting_input"
	EventRunStarted          EventType = "run.started"
	EventRunPhaseChanged     EventType = "run.phase_changed"
	EventRunWaitingApproval  EventType = "run.waiting_approval"
	EventRunResumed          EventType = "run.resumed"
	EventRunCompleted        EventType = "run.completed"
	EventRunFailed           EventType = "run.failed"
	EventRunCancelled        EventType = "run.cancelled"
	EventRunTimeout          EventType = "run.timeout"
	EventWorkflowYielded     EventType = "workflow.yielded"
	EventWorkflowContinued   EventType = "workflow.continued"
	EventWorkflowCompleted   EventType = "workflow.completed"
	EventWorkflowFailed      EventType = "workflow.failed"
	EventDeliveryFailed      EventType = "delivery.failed"
	EventApprovalRequested   EventType = "approval.requested"
	EventApprovalResolved    EventType = "approval.resolved"
	EventModelRouted         EventType = "model.routed"
	EventToolExecuted        EventType = "tool.executed"
	EventRunPlanned          EventType = "run.planned"
	EventTaskProgress        EventType = "run.task_progress"
	EventRunSteered          EventType = "run.steered"
	EventPlanTaskStarted     EventType = "plan.task.started"
	EventPlanTaskCompleted   EventType = "plan.task.completed"
	EventPlanTaskFailed      EventType = "plan.task.failed"
	EventPlanTaskCancelled   EventType = "plan.task.cancelled"
	EventPlanTaskSkipped     EventType = "plan.task.skipped"
	EventPlanSnapshotUpdated EventType = "plan.snapshot.updated"
	EventArtifactPruned      EventType = "artifact.pruned"
	EventModelTextDelta      EventType = "model.text_delta"
	EventModelReasoningDelta EventType = "model.reasoning_delta"
	EventModelStreamComplete EventType = "model.stream_complete"
	EventModelRetry          EventType = "model.retry"
	EventModelFailover       EventType = "model.failover"
	EventThinkingDegraded    EventType = "model.thinking_degraded"

	// Approval timeout
	EventApprovalTimedOut     EventType = "approval.timed_out"
	EventApprovalGraceWarning EventType = "approval.grace_warning"

	// Security audit
	EventSecurityRiskDetected     EventType = "security.risk_detected"
	EventSecurityPathViolation    EventType = "security.path_violation"
	EventSecurityInjectionAttempt EventType = "security.injection_attempt"

	// Governance delivery lifecycle
	EventGovernanceDeliveryQueued         EventType = "governance.delivery.queued"
	EventGovernanceDeliveryRedriven       EventType = "governance.delivery.redriven"
	EventGovernanceDeliveryRetryScheduled EventType = "governance.delivery.retry_scheduled"
	EventGovernanceDeliveryDelivered      EventType = "governance.delivery.delivered"
	EventGovernanceDeliveryDeadLettered   EventType = "governance.delivery.dead_lettered"
)

// Event is the immutable payload published through the runtime event bus.
type Event struct {
	ID        string         `json:"id"`
	Type      EventType      `json:"type"`
	RunID     string         `json:"run_id,omitempty"`
	SessionID string         `json:"session_id,omitempty"`
	Time      time.Time      `json:"time"`
	Attrs     map[string]any `json:"attrs,omitempty"`
}

// Sink consumes published events synchronously during Publish.
type Sink interface {
	Handle(ctx context.Context, event Event) error
}

// Snapshotter returns a point-in-time copy of the currently retained events.
type Snapshotter interface {
	Snapshot() []Event
}

// Bus publishes events and lets synchronous sinks subscribe to future events.
type Bus interface {
	Publish(ctx context.Context, event Event) error
	Subscribe(sink Sink)
}

// InMemoryBus stores a bounded in-memory event history and fans out live
// events to sinks and channel subscribers.
type InMemoryBus struct {
	mu     sync.RWMutex
	nextID atomic.Uint64
	events []Event
	sinks  []Sink
	subs   []*Subscription
	max    int
}

// Subscription delivers live events via a channel.
// Call Close when done to unsubscribe and free resources.
type Subscription struct {
	ch       chan Event
	bus      *InMemoryBus
	mu       sync.Mutex
	closed   bool
	drops    uint64
	lastDrop time.Time
}

// Events returns the channel receiving live events.
func (s *Subscription) Events() <-chan Event { return s.ch }

// DroppedCount returns how many live events were dropped because the
// subscription buffer was full.
func (s *Subscription) DroppedCount() uint64 {
	if s == nil {
		return 0
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.drops
}

// LastDropTime reports the most recent drop time, if any.
func (s *Subscription) LastDropTime() time.Time {
	if s == nil {
		return time.Time{}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lastDrop
}

// Close unsubscribes and closes the event channel.
func (s *Subscription) Close() {
	if s == nil || s.bus == nil {
		return
	}
	s.bus.removeSub(s)
}

// NewInMemoryBus returns an in-memory bus with the default history limit.
func NewInMemoryBus() *InMemoryBus {
	return NewInMemoryBusWithLimit(1000)
}

// NewInMemoryBusWithLimit returns an in-memory bus with a bounded retained
// event history. Non-positive limits fall back to the default.
func NewInMemoryBusWithLimit(limit int) *InMemoryBus {
	if limit <= 0 {
		limit = 1000
	}
	return &InMemoryBus{max: limit}
}

// Publish stores the event, auto-populates missing ID and time fields, and
// returns any sink errors encountered after attempting delivery to all sinks.
func (b *InMemoryBus) Publish(ctx context.Context, event Event) error {
	b.mu.Lock()
	if event.ID == "" {
		event.ID = fmt.Sprintf("evt-%06d", b.nextID.Add(1))
	}
	if event.Time.IsZero() {
		event.Time = time.Now().UTC()
	}
	if event.Attrs == nil {
		event.Attrs = map[string]any{}
	}
	stored := cloneEvent(event)
	if b.max > 0 && len(b.events) >= b.max {
		copy(b.events, b.events[1:])
		b.events[len(b.events)-1] = stored
	} else {
		b.events = append(b.events, stored)
	}
	queueDepth := len(b.events)
	sinks := append([]Sink(nil), b.sinks...)
	subs := append([]*Subscription(nil), b.subs...)
	b.mu.Unlock()

	metrics.EventBusPublished.WithLabelValues(string(event.Type)).Inc()
	metrics.EventBusQueueDepth.Set(float64(queueDepth))

	var sinkErrs []error
	for _, sink := range sinks {
		if err := sink.Handle(ctx, event); err != nil {
			metrics.EventBusSinkErrors.WithLabelValues(fmt.Sprintf("%T", sink), string(event.Type)).Inc()
			sinkErrs = append(sinkErrs, err)
		}
	}
	// Fan out to channel subscribers (non-blocking).
	clone := cloneEvent(event)
	for _, sub := range subs {
		if dropped := sub.trySend(clone); dropped {
			metrics.EventBusSubscriberDropped.WithLabelValues(string(event.Type)).Inc()
		}
	}
	return errors.Join(sinkErrs...)
}

// Subscribe registers a synchronous sink for future events. Nil sinks are
// ignored.
func (b *InMemoryBus) Subscribe(sink Sink) {
	if sink == nil {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.sinks = append(b.sinks, sink)
}

// SubscribeChannel creates a buffered channel subscription for live events.
// The caller must call Close on the returned Subscription when done.
func (b *InMemoryBus) SubscribeChannel(bufSize int) *Subscription {
	if bufSize <= 0 {
		bufSize = 64
	}
	sub := &Subscription{
		ch:  make(chan Event, bufSize),
		bus: b,
	}
	b.mu.Lock()
	b.subs = append(b.subs, sub)
	b.mu.Unlock()
	return sub
}

func (b *InMemoryBus) removeSub(sub *Subscription) {
	if sub == nil {
		return
	}
	b.mu.Lock()
	for i, s := range b.subs {
		if s == sub {
			b.subs = append(b.subs[:i], b.subs[i+1:]...)
			break
		}
	}
	b.mu.Unlock()
	sub.close()
}

// Snapshot returns a cloned copy of the retained event history.
func (b *InMemoryBus) Snapshot() []Event {
	b.mu.RLock()
	defer b.mu.RUnlock()
	out := make([]Event, len(b.events))
	for i, event := range b.events {
		out[i] = cloneEvent(event)
	}
	return out
}

// SnapshotSince returns events after the given cursor ID, up to limit.
// If sinceID is empty, returns the latest events (up to limit).
func (b *InMemoryBus) SnapshotSince(sinceID string, limit int) []Event {
	b.mu.RLock()
	defer b.mu.RUnlock()

	if sinceID == "" {
		remaining := b.events
		if limit > 0 && len(remaining) > limit {
			remaining = remaining[len(remaining)-limit:]
		}
		return cloneEvents(remaining)
	}

	start := 0
	for i, e := range b.events {
		if e.ID == sinceID {
			start = i + 1
			break
		}
	}
	remaining := b.events[start:]
	if limit > 0 && len(remaining) > limit {
		remaining = remaining[:limit]
	}
	out := make([]Event, len(remaining))
	for i, e := range remaining {
		out[i] = cloneEvent(e)
	}
	return out
}

// CursorStatus indicates the validity of a cursor position.
type CursorStatus string

const (
	CursorOK      CursorStatus = "ok"      // cursor found, returning events after it
	CursorExpired CursorStatus = "expired" // cursor was evicted, returning from earliest available
	CursorEmpty   CursorStatus = "empty"   // no cursor provided, returning latest
)

// CursorResult wraps a snapshot response with cursor metadata.
type CursorResult struct {
	Events []Event      `json:"events"`
	Status CursorStatus `json:"cursor_status"`
	// NextCursor is the ID of the last event in the result, to be used as the next `since` value.
	// Empty if no events returned.
	NextCursor string `json:"next_cursor,omitempty"`
}

// SnapshotSinceWithStatus returns events after the given cursor ID with cursor validity metadata.
func (b *InMemoryBus) SnapshotSinceWithStatus(sinceID string, limit int) CursorResult {
	b.mu.RLock()
	defer b.mu.RUnlock()

	if sinceID == "" {
		remaining := b.events
		if limit > 0 && len(remaining) > limit {
			remaining = remaining[len(remaining)-limit:]
		}
		out := cloneEvents(remaining)
		return CursorResult{
			Events:     out,
			Status:     CursorEmpty,
			NextCursor: lastEventID(out),
		}
	}

	// Search for the cursor.
	found := false
	start := 0
	for i, e := range b.events {
		if e.ID == sinceID {
			start = i + 1
			found = true
			break
		}
	}

	status := CursorOK
	if !found {
		// Cursor was evicted from the ring buffer.
		start = 0
		status = CursorExpired
	}

	remaining := b.events[start:]
	if limit > 0 && len(remaining) > limit {
		remaining = remaining[:limit]
	}
	out := cloneEvents(remaining)
	return CursorResult{
		Events:     out,
		Status:     status,
		NextCursor: lastEventID(out),
	}
}

func cloneEvents(events []Event) []Event {
	out := make([]Event, len(events))
	for i, e := range events {
		out[i] = cloneEvent(e)
	}
	return out
}

func lastEventID(events []Event) string {
	if len(events) == 0 {
		return ""
	}
	return events[len(events)-1].ID
}

func (s *Subscription) trySend(event Event) bool {
	if s == nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return false
	}
	select {
	case s.ch <- event:
		return false
	default:
		s.drops++
		s.lastDrop = time.Now().UTC()
		return true
	}
}

func (s *Subscription) close() {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	s.closed = true
	close(s.ch)
}

func cloneEvent(in Event) Event {
	out := in
	if in.Attrs != nil {
		out.Attrs = make(map[string]any, len(in.Attrs))
		for k, v := range in.Attrs {
			out.Attrs[k] = v
		}
	}
	return out
}

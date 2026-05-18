package cron

import (
	"sync"
	"time"
)

// ---------------------------------------------------------------------------
// Constants
// ---------------------------------------------------------------------------

const (
	// maxEventsPerSession caps the number of queued events per session.
	maxEventsPerSession = 20
)

// ---------------------------------------------------------------------------
// SystemEvent
// ---------------------------------------------------------------------------

// SystemEvent is a lightweight message queued by cron jobs for delivery
// to an agent session on the next heartbeat or prompt cycle.
type SystemEvent struct {
	Text       string    `json:"text"`
	SessionKey string    `json:"session_key"`
	AgentID    string    `json:"agent_id,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
}

// ---------------------------------------------------------------------------
// EventQueue
// ---------------------------------------------------------------------------

// EventQueue is a per-session in-memory event queue. Cron jobs enqueue
// events via Enqueue; the heartbeat or prompt cycle drains them via Drain.
type EventQueue struct {
	mu     sync.Mutex
	queues map[string][]SystemEvent // session_key → events
}

// NewEventQueue creates an empty event queue.
func NewEventQueue() *EventQueue {
	return &EventQueue{
		queues: make(map[string][]SystemEvent),
	}
}

// Enqueue adds a system event to the session's queue. The queue is capped at
// maxEventsPerSession per session (oldest dropped on overflow).
func (q *EventQueue) Enqueue(ev SystemEvent) {
	if ev.SessionKey == "" || ev.Text == "" {
		return
	}
	if ev.CreatedAt.IsZero() {
		ev.CreatedAt = time.Now().UTC()
	}

	q.mu.Lock()
	defer q.mu.Unlock()

	events := q.queues[ev.SessionKey]
	events = append(events, ev)

	// Cap at maxEventsPerSession — drop oldest on overflow.
	if len(events) > maxEventsPerSession {
		events = events[len(events)-maxEventsPerSession:]
	}

	q.queues[ev.SessionKey] = events
}

// Drain removes and returns all events for the given session key.
func (q *EventQueue) Drain(sessionKey string) []SystemEvent {
	q.mu.Lock()
	defer q.mu.Unlock()

	events := q.queues[sessionKey]
	if len(events) == 0 {
		return nil
	}
	delete(q.queues, sessionKey)

	// Return a copy to avoid data races.
	out := make([]SystemEvent, len(events))
	copy(out, events)
	return out
}

// Len returns the total number of queued events across all sessions.
func (q *EventQueue) Len() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	n := 0
	for _, events := range q.queues {
		n += len(events)
	}
	return n
}

// SessionKeys returns the session keys that have pending events.
func (q *EventQueue) SessionKeys() []string {
	q.mu.Lock()
	defer q.mu.Unlock()
	keys := make([]string, 0, len(q.queues))
	for k, events := range q.queues {
		if len(events) > 0 {
			keys = append(keys, k)
		}
	}
	return keys
}

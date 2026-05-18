package wakeup

import (
	"sync"
	"time"
)

// ---------------------------------------------------------------------------
// Constants
// ---------------------------------------------------------------------------

const (
	// coalesceWindow is the time window within which multiple wake requests
	// are merged into a single execution.
	coalesceWindow = 250 * time.Millisecond
)

// ---------------------------------------------------------------------------
// WakePriority
// ---------------------------------------------------------------------------

// WakePriority determines which wake request wins when multiple requests
// arrive within the coalesce window. Higher numeric value = higher priority.
type WakePriority int

const (
	PriorityRetry    WakePriority = 1
	PriorityInterval WakePriority = 2
	PriorityDefault  WakePriority = 3
	PriorityAction   WakePriority = 4
)

// ---------------------------------------------------------------------------
// WakeRequest
// ---------------------------------------------------------------------------

// WakeRequest represents a pending wake request with priority and metadata.
type WakeRequest struct {
	SessionKey  string
	Channel     string
	Message     string
	Priority    WakePriority
	RequestedAt time.Time
}

// ---------------------------------------------------------------------------
// Coalescer
// ---------------------------------------------------------------------------

// Coalescer batches multiple wake requests arriving within a short time
// window and emits only the highest-priority request per session key.
type Coalescer struct {
	mu       sync.Mutex
	pending  map[string]WakeRequest // session_key → highest-priority request
	timer    *time.Timer
	callback func(req WakeRequest)
}

// NewCoalescer creates a coalescer that calls callback with the winning
// request after the coalesce window expires.
func NewCoalescer(callback func(req WakeRequest)) *Coalescer {
	return &Coalescer{
		pending:  make(map[string]WakeRequest),
		callback: callback,
	}
}

// Submit adds a wake request. If a request for the same session key is
// already pending, the higher-priority one wins. The coalesce timer is
// (re)started on each submit.
func (c *Coalescer) Submit(req WakeRequest) {
	if req.RequestedAt.IsZero() {
		req.RequestedAt = time.Now().UTC()
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	existing, ok := c.pending[req.SessionKey]
	if !ok || req.Priority > existing.Priority {
		c.pending[req.SessionKey] = req
	}

	// Reset the coalesce timer.
	if c.timer != nil {
		c.timer.Stop()
	}
	c.timer = time.AfterFunc(coalesceWindow, c.flush)
}

// flush fires all pending requests and clears the map.
func (c *Coalescer) flush() {
	c.mu.Lock()
	batch := c.pending
	c.pending = make(map[string]WakeRequest)
	c.timer = nil
	c.mu.Unlock()

	for _, req := range batch {
		if c.callback != nil {
			c.callback(req)
		}
	}
}

// PendingCount returns the number of requests waiting to be flushed.
func (c *Coalescer) PendingCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.pending)
}

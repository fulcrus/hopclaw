package nodes

import (
	"crypto/rand"
	"encoding/hex"
	"sync"
	"time"
)

// ---------------------------------------------------------------------------
// Work item types and priorities
// ---------------------------------------------------------------------------

// WorkItemType identifies the kind of work a node should perform.
type WorkItemType string

const (
	WorkItemStatusRequest   WorkItemType = "status.request"
	WorkItemLocationRequest WorkItemType = "location.request"
)

// WorkItemPriority determines the ordering of work items in the queue.
type WorkItemPriority string

const (
	PriorityDefault WorkItemPriority = "default"
	PriorityNormal  WorkItemPriority = "normal"
	PriorityHigh    WorkItemPriority = "high"
)

// priorityRank returns a numeric rank for sorting (higher = dequeued first).
func priorityRank(p WorkItemPriority) int {
	switch p {
	case PriorityHigh:
		return 2
	case PriorityNormal:
		return 1
	default:
		return 0
	}
}

// ---------------------------------------------------------------------------
// Work item
// ---------------------------------------------------------------------------

// WorkItem represents a unit of work that a node can pull from the queue.
type WorkItem struct {
	ID        string           `json:"id"`
	Type      WorkItemType     `json:"type"`
	Priority  WorkItemPriority `json:"priority"`
	Params    map[string]any   `json:"params,omitempty"`
	CreatedAt time.Time        `json:"created_at"`
	ExpiresAt *time.Time       `json:"expires_at,omitempty"`
}

// isExpired reports whether the work item has passed its expiration time.
func (w *WorkItem) isExpired(now time.Time) bool {
	return w.ExpiresAt != nil && now.After(*w.ExpiresAt)
}

// ---------------------------------------------------------------------------
// Pending work queue
// ---------------------------------------------------------------------------

const (
	maxDrainDefault = 4
	maxDrainLimit   = 10
	workItemIDBytes = 16
)

// PendingWorkQueue is a thread-safe priority queue for work items.
type PendingWorkQueue struct {
	mu    sync.Mutex
	items []*WorkItem // sorted by priority (high first)
}

// NewPendingWorkQueue creates an empty work queue.
func NewPendingWorkQueue() *PendingWorkQueue {
	return &PendingWorkQueue{}
}

// Enqueue inserts a work item in priority order (highest first).
// If the item has no ID, one is generated automatically.
func (q *PendingWorkQueue) Enqueue(item WorkItem) {
	if item.ID == "" {
		item.ID = generateWorkItemID()
	}
	if item.CreatedAt.IsZero() {
		item.CreatedAt = time.Now().UTC()
	}
	if item.Priority == "" {
		item.Priority = PriorityDefault
	}

	q.mu.Lock()
	defer q.mu.Unlock()

	rank := priorityRank(item.Priority)
	insertIdx := len(q.items)
	for i, existing := range q.items {
		if priorityRank(existing.Priority) < rank {
			insertIdx = i
			break
		}
	}

	// Insert at position.
	q.items = append(q.items, nil)
	copy(q.items[insertIdx+1:], q.items[insertIdx:])
	q.items[insertIdx] = &item
}

// Drain pops up to maxItems work items from the front of the queue,
// skipping any that have expired. If maxItems <= 0, maxDrainDefault is used.
// maxItems is capped at maxDrainLimit.
func (q *PendingWorkQueue) Drain(maxItems int) []*WorkItem {
	if maxItems <= 0 {
		maxItems = maxDrainDefault
	}
	if maxItems > maxDrainLimit {
		maxItems = maxDrainLimit
	}

	q.mu.Lock()
	defer q.mu.Unlock()

	q.cleanupLocked()

	n := maxItems
	if n > len(q.items) {
		n = len(q.items)
	}

	result := make([]*WorkItem, n)
	copy(result, q.items[:n])
	q.items = q.items[n:]
	return result
}

// Len returns the number of items currently in the queue.
func (q *PendingWorkQueue) Len() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return len(q.items)
}

// cleanupLocked removes expired items. Must be called with mu held.
func (q *PendingWorkQueue) cleanupLocked() {
	now := time.Now()
	n := 0
	for _, item := range q.items {
		if !item.isExpired(now) {
			q.items[n] = item
			n++
		}
	}
	q.items = q.items[:n]
}

// generateWorkItemID returns a hex-encoded random ID.
func generateWorkItemID() string {
	var buf [workItemIDBytes]byte
	if _, err := rand.Read(buf[:]); err != nil {
		// Fallback: timestamp-based, acceptable for non-crypto use.
		return hex.EncodeToString([]byte(time.Now().String()))
	}
	return hex.EncodeToString(buf[:])
}

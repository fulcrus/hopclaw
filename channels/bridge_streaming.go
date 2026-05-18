package channels

import (
	"strings"
	"time"
)

const BridgeStreamingThrottle = 500 * time.Millisecond

const (
	BridgeDeliveredStateTTL = 24 * time.Hour
	BridgeStreamingStateTTL = 2 * time.Hour
)

type StreamingDeliveryState struct {
	Handle      string
	Content     string
	LastSent    string
	LastFlushAt time.Time
	UpdatedAt   time.Time
	Disabled    bool
}

func TouchStreamingDeliveryState(state *StreamingDeliveryState, now time.Time) {
	if state == nil {
		return
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	state.UpdatedAt = now
}

func StreamingDeliveryStateStale(state *StreamingDeliveryState, now time.Time) bool {
	if state == nil {
		return false
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	lastActivity := state.UpdatedAt
	if state.LastFlushAt.After(lastActivity) {
		lastActivity = state.LastFlushAt
	}
	if lastActivity.IsZero() {
		return false
	}
	return now.Sub(lastActivity) >= BridgeStreamingStateTTL
}

func MergeStreamingContent(previous, next string) string {
	if next == "" {
		return previous
	}
	if previous == "" || next == previous {
		return next
	}
	if strings.HasPrefix(next, previous) {
		return next
	}
	if strings.HasPrefix(previous, next) {
		return previous
	}
	if strings.Contains(next, previous) {
		return next
	}
	if strings.Contains(previous, next) {
		return previous
	}

	maxOverlap := len(previous)
	if len(next) < maxOverlap {
		maxOverlap = len(next)
	}
	for overlap := maxOverlap; overlap > 0; overlap-- {
		if previous[len(previous)-overlap:] == next[:overlap] {
			return previous + next[overlap:]
		}
	}
	return previous + next
}

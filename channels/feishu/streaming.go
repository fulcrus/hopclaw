package feishu

import (
	"context"
	"strings"
	"time"

	"github.com/fulcrus/hopclaw/channels"
)

type streamingCardController interface {
	StartStreamingCard(ctx context.Context, msg channels.OutboundMessage) (string, error)
	UpdateStreamingCard(ctx context.Context, messageID string, content string, final bool, metadata map[string]any) error
}

type typingReactionController interface {
	AddTypingIndicator(ctx context.Context, messageID string, metadata map[string]any) (string, error)
	RemoveTypingIndicator(ctx context.Context, messageID, reactionID string, metadata map[string]any) error
}

var typingKeepaliveInterval = 8 * time.Second
var typingCircuitBreakerCooldown = time.Minute

type streamingState struct {
	messageID   string
	content     string
	lastSent    string
	lastFlushAt time.Time
	updatedAt   time.Time
}

type typingState struct {
	messageID  string
	reactionID string
	metadata   map[string]any
	cancel     context.CancelFunc
}

type typingCircuitBreaker struct {
	until time.Time
}

func (c *typingCircuitBreaker) Open(now time.Time, cooldown time.Duration) {
	if cooldown <= 0 {
		cooldown = time.Minute
	}
	c.until = now.Add(cooldown)
}

func (c *typingCircuitBreaker) Allow(now time.Time) bool {
	return c.until.IsZero() || now.After(c.until)
}

func shouldFlushStreaming(state *streamingState, delta string, now time.Time, final bool) bool {
	if state == nil {
		return final
	}
	if final {
		return true
	}
	if len(state.content)-len(state.lastSent) >= 48 {
		return true
	}
	if strings.Contains(delta, "\n") {
		return true
	}
	if state.lastFlushAt.IsZero() {
		return true
	}
	return now.Sub(state.lastFlushAt) >= 250*time.Millisecond
}

func touchStreamingState(state *streamingState, now time.Time) {
	if state == nil {
		return
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	state.updatedAt = now
}

func streamingStateStale(state *streamingState, now time.Time) bool {
	if state == nil {
		return false
	}
	return channels.StreamingDeliveryStateStale(&channels.StreamingDeliveryState{
		LastFlushAt: state.lastFlushAt,
		UpdatedAt:   state.updatedAt,
	}, now)
}

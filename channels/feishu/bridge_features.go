package feishu

import (
	"context"
	"strings"
	"time"

	"github.com/fulcrus/hopclaw/channels"
	"github.com/fulcrus/hopclaw/eventbus"
	"github.com/fulcrus/hopclaw/internal/meta"
)

func (b *Bridge) handleStreamingDelta(ctx context.Context, runID string, target channels.RunNotificationTarget, delta string) {
	controller, ok := b.adapter.(streamingCardController)
	if !ok || strings.TrimSpace(runID) == "" || strings.TrimSpace(target.TargetID) == "" || strings.TrimSpace(delta) == "" {
		return
	}

	state := b.ensureStreamingState(ctx, controller, runID, target)
	if state == nil {
		return
	}
	state.content = mergeStreamingText(state.content, delta)
	now := time.Now().UTC()
	touchStreamingState(state, now)
	if !shouldFlushStreaming(state, delta, now, false) {
		return
	}
	if state.lastSent == state.content {
		return
	}
	if err := controller.UpdateStreamingCard(ctx, state.messageID, state.content, false, target.Metadata); err != nil {
		log.Warn("feishu bridge: update streaming card failed", "error", err, "run_id", runID)
		return
	}
	state.lastSent = state.content
	state.lastFlushAt = now
}

func (b *Bridge) finalizeStreaming(ctx context.Context, runID string, content string, metadata map[string]any) bool {
	controller, ok := b.adapter.(streamingCardController)
	if !ok || strings.TrimSpace(runID) == "" {
		return false
	}
	b.mu.Lock()
	b.pruneStreamingStatesLocked(time.Now().UTC())
	state := b.streams[runID]
	if state == nil {
		b.mu.Unlock()
		return false
	}
	delete(b.streams, runID)
	b.mu.Unlock()
	finalContent := strings.TrimSpace(content)
	if finalContent == "" {
		finalContent = state.content
	}
	if strings.TrimSpace(finalContent) == "" {
		return false
	}
	if err := controller.UpdateStreamingCard(ctx, state.messageID, finalContent, true, metadata); err != nil {
		log.Warn("feishu bridge: finalize streaming card failed", "error", err, "run_id", runID)
		return false
	}
	return true
}

func (b *Bridge) ensureStreamingState(ctx context.Context, controller streamingCardController, runID string, target channels.RunNotificationTarget) *streamingState {
	now := time.Now().UTC()
	b.mu.Lock()
	b.pruneStreamingStatesLocked(now)
	if state := b.streams[runID]; state != nil {
		b.mu.Unlock()
		return state
	}
	b.mu.Unlock()

	value, err, _ := b.streamOps.Do(runID, func() (any, error) {
		b.mu.Lock()
		b.pruneStreamingStatesLocked(time.Now().UTC())
		if state := b.streams[runID]; state != nil {
			b.mu.Unlock()
			return state, nil
		}
		b.mu.Unlock()

		messageID, err := controller.StartStreamingCard(ctx, channels.OutboundMessage{
			ChannelID: "feishu",
			TargetID:  target.TargetID,
			ReplyToID: target.ReplyToID,
			Content:   target.InputContent,
			Format:    defaultFeishuOutboundFormat(target.Format),
			Metadata:  target.Metadata,
		})
		if err != nil {
			return nil, err
		}
		state := &streamingState{messageID: messageID}
		touchStreamingState(state, time.Now().UTC())

		b.mu.Lock()
		defer b.mu.Unlock()
		b.pruneStreamingStatesLocked(time.Now().UTC())
		if existing := b.streams[runID]; existing != nil {
			return existing, nil
		}
		b.streams[runID] = state
		return state, nil
	})
	if err != nil {
		log.Warn("feishu bridge: start streaming card failed", "error", err, "run_id", runID)
		return nil
	}
	state, _ := value.(*streamingState)
	return state
}

func (b *Bridge) startTyping(ctx context.Context, runID string, target channels.RunNotificationTarget) {
	controller, ok := b.adapter.(typingReactionController)
	if !ok || strings.TrimSpace(runID) == "" || strings.TrimSpace(target.ReplyToID) == "" {
		return
	}
	b.mu.Lock()
	if _, exists := b.typing[runID]; exists || !b.typingCB.Allow(time.Now().UTC()) {
		b.mu.Unlock()
		return
	}
	b.mu.Unlock()

	reactionID, err := controller.AddTypingIndicator(ctx, target.ReplyToID, target.Metadata)
	if err != nil {
		if isTypingBackoff(err) {
			b.mu.Lock()
			b.typingCB.Open(time.Now().UTC(), typingCircuitBreakerCooldown)
			b.mu.Unlock()
		}
		log.Warn("feishu bridge: add typing indicator failed", "error", err, "run_id", runID)
		return
	}
	keepaliveCtx, cancel := context.WithCancel(ctx)
	b.mu.Lock()
	b.typing[runID] = &typingState{
		messageID:  target.ReplyToID,
		reactionID: reactionID,
		metadata:   cloneMetadata(target.Metadata),
		cancel:     cancel,
	}
	b.mu.Unlock()
	go b.typingKeepaliveLoop(keepaliveCtx, runID, controller)
}

func (b *Bridge) stopTyping(ctx context.Context, runID string) {
	controller, ok := b.adapter.(typingReactionController)
	if !ok || strings.TrimSpace(runID) == "" {
		return
	}
	b.mu.Lock()
	state := b.typing[runID]
	delete(b.typing, runID)
	b.mu.Unlock()
	if state == nil {
		return
	}
	if state.cancel != nil {
		state.cancel()
	}
	if err := controller.RemoveTypingIndicator(ctx, state.messageID, state.reactionID, state.metadata); err != nil && !isTypingBackoff(err) {
		log.Warn("feishu bridge: remove typing indicator failed", "error", err, "run_id", runID)
	}
}

func (b *Bridge) typingKeepaliveLoop(ctx context.Context, runID string, controller typingReactionController) {
	interval := typingKeepaliveInterval
	if interval <= 0 {
		interval = 8 * time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}

		b.mu.Lock()
		state := b.typing[runID]
		allowed := b.typingCB.Allow(time.Now().UTC())
		b.mu.Unlock()
		if state == nil || !allowed {
			return
		}

		refreshCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		_ = controller.RemoveTypingIndicator(refreshCtx, state.messageID, state.reactionID, state.metadata)
		reactionID, err := controller.AddTypingIndicator(refreshCtx, state.messageID, state.metadata)
		cancel()
		if err != nil {
			if isTypingBackoff(err) {
				b.mu.Lock()
				b.typingCB.Open(time.Now().UTC(), typingCircuitBreakerCooldown)
				delete(b.typing, runID)
				b.mu.Unlock()
				log.Warn("feishu bridge: typing keepalive backed off", "error", err, "run_id", runID)
				return
			}
			log.Warn("feishu bridge: typing keepalive refresh failed", "error", err, "run_id", runID)
			continue
		}

		b.mu.Lock()
		current := b.typing[runID]
		if current == nil {
			b.mu.Unlock()
			return
		}
		current.reactionID = reactionID
		b.mu.Unlock()
	}
}

func isTypingBackoff(err error) bool {
	if err == nil {
		return false
	}
	value := strings.ToLower(err.Error())
	return strings.Contains(value, "99991400") ||
		strings.Contains(value, "99991403") ||
		strings.Contains(value, "429")
}

func mergeStreamingText(previous, next string) string {
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

func (b *Bridge) handleLifecycleEvent(ctx context.Context, event eventbus.Event, snapshot channels.RunProgressSnapshot) {
	switch event.Type {
	case eventbus.EventRunStarted, eventbus.EventRunResumed:
		b.startTyping(ctx, event.RunID, snapshot.Target)
	case eventbus.EventModelStreamComplete:
		if strings.TrimSpace(event.RunID) == "" {
			return
		}
		b.mu.Lock()
		state := b.streams[event.RunID]
		b.mu.Unlock()
		if state != nil && strings.TrimSpace(state.content) != "" && state.lastSent != state.content {
			if controller, ok := b.adapter.(streamingCardController); ok {
				_ = controller.UpdateStreamingCard(ctx, state.messageID, state.content, false, snapshot.Target.Metadata)
				state.lastSent = state.content
				now := time.Now().UTC()
				state.lastFlushAt = now
				touchStreamingState(state, now)
			}
		}
	case eventbus.EventRunCancelled:
		b.stopTyping(ctx, event.RunID)
	}
}

func enrichStreamingMetadata(base map[string]any, target replyTarget) map[string]any {
	metadata := cloneMetadata(base)
	if metadata == nil {
		metadata = make(map[string]any, 4)
	}
	if strings.TrimSpace(target.accountID) != "" {
		metadata["account_id"] = target.accountID
	}
	if target.replyInThread {
		metadata["reply_in_thread"] = true
	}
	metadata[meta.KeyReceiveIDType] = target.receiveIDType
	return metadata
}

func cloneMetadata(base map[string]any) map[string]any {
	if len(base) == 0 {
		return nil
	}
	out := make(map[string]any, len(base))
	for key, value := range base {
		out[key] = value
	}
	return out
}

func defaultFeishuOutboundFormat(format string) string {
	if strings.TrimSpace(format) == "" {
		return "text"
	}
	return format
}

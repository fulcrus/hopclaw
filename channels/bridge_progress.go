package channels

import (
	"context"
	"strings"
	"time"
	"unicode"

	"github.com/fulcrus/hopclaw/agent"
	"github.com/fulcrus/hopclaw/contextengine"
	"github.com/fulcrus/hopclaw/eventbus"
	"github.com/fulcrus/hopclaw/internal/meta"
	"github.com/fulcrus/hopclaw/internal/usererror"
	runtimesvc "github.com/fulcrus/hopclaw/runtime"
)

func (b *Bridge) handleStreamingDelta(ctx context.Context, runID string, target RunNotificationTarget, delta string) {
	streamer, ok := b.adapter.(StreamingRenderer)
	if !ok || strings.TrimSpace(runID) == "" || strings.TrimSpace(target.TargetID) == "" || strings.TrimSpace(delta) == "" {
		return
	}

	state := b.ensureStreamingState(ctx, streamer, runID, target)
	if state == nil || strings.TrimSpace(state.Handle) == "" || state.Disabled {
		return
	}

	b.mu.Lock()
	state.Content = MergeStreamingContent(state.Content, delta)
	content := state.Content
	lastSent := state.LastSent
	lastFlushAt := state.LastFlushAt
	handle := state.Handle
	TouchStreamingDeliveryState(state, time.Now().UTC())
	b.mu.Unlock()

	now := time.Now().UTC()
	if content == lastSent || (!lastFlushAt.IsZero() && now.Sub(lastFlushAt) < BridgeStreamingThrottle) {
		return
	}
	if err := b.outbound.Do(func() error {
		return streamer.UpdateStreaming(ctx, handle, content)
	}); err != nil {
		log.Warn("bridge: update streaming failed", "channel", b.cfg.ChannelName, "error", err, "run_id", runID)
		return
	}

	b.mu.Lock()
	if current := b.streams[runID]; current != nil {
		current.LastSent = content
		current.LastFlushAt = now
		TouchStreamingDeliveryState(current, now)
	}
	b.mu.Unlock()
}

func (b *Bridge) ensureStreamingState(ctx context.Context, streamer StreamingRenderer, runID string, target RunNotificationTarget) *StreamingDeliveryState {
	now := time.Now().UTC()
	b.mu.Lock()
	b.pruneStreamingStatesLocked(now)
	if state := b.streams[runID]; state != nil {
		TouchStreamingDeliveryState(state, now)
		b.mu.Unlock()
		return state
	}
	state := &StreamingDeliveryState{}
	TouchStreamingDeliveryState(state, now)
	b.streams[runID] = state
	b.mu.Unlock()

	var handle string
	err := b.outbound.Do(func() error {
		var beginErr error
		handle, beginErr = streamer.BeginStreaming(ctx, OutboundMessage{
			ChannelID: target.ChannelID,
			TargetID:  target.TargetID,
			ReplyToID: target.ReplyToID,
			Content:   target.InputContent,
			Format:    target.Format,
			Metadata:  cloneAnyMap(target.Metadata),
		})
		return beginErr
	})
	if err != nil {
		log.Warn("bridge: begin streaming failed", "channel", b.cfg.ChannelName, "error", err, "run_id", runID)
		b.mu.Lock()
		if state := b.streams[runID]; state != nil {
			state.Disabled = true
			TouchStreamingDeliveryState(state, time.Now().UTC())
		}
		b.mu.Unlock()
		return nil
	}

	b.mu.Lock()
	defer b.mu.Unlock()
	state = b.streams[runID]
	if state == nil {
		state = &StreamingDeliveryState{}
		b.streams[runID] = state
	}
	state.Handle = handle
	TouchStreamingDeliveryState(state, time.Now().UTC())
	return state
}

func (b *Bridge) streamingTerminalSender(runID string) (func(context.Context, OutboundMessage) error, func() bool) {
	streamer, ok := b.adapter.(StreamingRenderer)
	if !ok {
		return b.send, func() bool { return false }
	}

	now := time.Now().UTC()
	b.mu.Lock()
	b.pruneStreamingStatesLocked(now)
	state := b.streams[runID]
	TouchStreamingDeliveryState(state, now)
	b.mu.Unlock()
	if state == nil || state.Disabled || strings.TrimSpace(state.Handle) == "" {
		return b.send, func() bool { return false }
	}

	finalized := false
	send := func(ctx context.Context, msg OutboundMessage) error {
		if err := b.outbound.Do(func() error {
			return streamer.EndStreaming(ctx, state.Handle, msg)
		}); err != nil {
			log.Warn("bridge: end streaming failed, falling back to send", "channel", b.cfg.ChannelName, "error", err, "run_id", runID)
			b.clearStreamingState(runID)
			return b.send(ctx, msg)
		}
		finalized = true
		b.clearStreamingState(runID)
		return nil
	}
	return send, func() bool { return finalized }
}

func (b *Bridge) clearStreamingState(runID string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.streams, runID)
}

func (b *Bridge) outboundLoop(ctx context.Context, sub *eventbus.Subscription) {
	if sub == nil {
		return
	}
	defer sub.Close()

	for {
		select {
		case <-ctx.Done():
			return
		case event, ok := <-sub.Events():
			if !ok {
				return
			}
			if IsSilentRunCancellation(event) {
				b.status.Clear(event.RunID)
				continue
			}
			snapshot, _ := b.status.SnapshotRun(event.RunID)
			projection, projected := b.projector.ProjectLive(event, snapshot)
			if projected {
				switch projection.Kind {
				case ProjectedRunEventPhase:
					b.status.NotifyPhaseChanged(event.RunID, projection.Phase, projection.ToolNames)
					continue
				case ProjectedRunEventToolProgress:
					b.status.NotifyToolProgress(event.RunID, projection.ToolRounds, projection.ToolNames)
					continue
				case ProjectedRunEventPlanProgress:
					b.status.NotifyPlanProgress(event.RunID, projection.ActiveTask, projection.Completed, projection.Total)
					continue
				case ProjectedRunEventApproval:
					if b.tryAutoApproveSkillInstall(ctx, event) {
						continue
					}
					b.status.NotifyApproval(ctx, event.RunID)
					continue
				case ProjectedRunEventResumed:
					b.status.NotifyResumed(ctx, event.RunID)
					continue
				case ProjectedRunEventCancelled:
					b.status.NotifyCancelled(ctx, event.RunID)
					continue
				case ProjectedRunEventStreaming:
					b.handleStreamingDelta(ctx, event.RunID, snapshot.Target, projection.Content)
					continue
				}
			}
			switch event.Type {
			case eventbus.EventRunCompleted, eventbus.EventRunFailed:
				b.status.Clear(event.RunID)
				b.handleTerminalRun(ctx, event)
			}
		}
	}
}

func (b *Bridge) handleTerminalRun(ctx context.Context, event eventbus.Event) {
	if event.RunID == "" || event.SessionID == "" || b.runtime == nil {
		return
	}
	if b.isDelivered(event.RunID) {
		return
	}

	session, err := agent.LoadSession(ctx, b.sessions, event.SessionID, agent.ScopeFilter{})
	if err != nil {
		log.Error("bridge: load session", "channel", b.cfg.ChannelName, "error", err, "session_id", event.SessionID)
		return
	}
	if agent.SessionChannelName(session) != b.cfg.ChannelName {
		return
	}
	switch event.Type {
	case eventbus.EventRunCompleted:
		b.authGate.Clear(session.Key)
	case eventbus.EventRunFailed:
		raw := ""
		if payload, ok := event.RunFailedPayload(); ok {
			raw = payload.Error
		}
		if bridgeLooksLikeAuthFailure(raw) {
			b.authGate.Arm(session.Key)
		} else {
			b.authGate.Clear(session.Key)
		}
	}

	run, err := b.runtime.GetRun(ctx, event.RunID)
	if err != nil {
		log.Error("bridge: load run", "channel", b.cfg.ChannelName, "error", err, "run_id", event.RunID)
		return
	}
	runResult, resultErr := GetRunResultIfSupported(ctx, b.runtime, event.RunID)
	if resultErr != nil {
		log.Warn("bridge: load run result", "channel", b.cfg.ChannelName, "error", resultErr, "run_id", event.RunID)
	}
	runVerification, verificationErr := GetRunVerificationIfSupported(ctx, b.runtime, event.RunID)
	if verificationErr != nil {
		log.Warn("bridge: load run verification", "channel", b.cfg.ChannelName, "error", verificationErr, "run_id", event.RunID)
	}

	targetID, messageID := b.extractReplyTarget(session, run.InputEventID)
	if targetID == "" {
		return
	}

	projection, ok := b.projector.ProjectTerminal(session, run, runResult, runVerification, event)
	if !ok || strings.TrimSpace(projection.Content) == "" {
		return
	}

	outbound := OutboundMessage{
		ChannelID: b.cfg.ChannelName,
		TargetID:  targetID,
		ReplyToID: messageID,
		Content:   projection.Content,
		Format:    "text",
		Metadata:  map[string]any{meta.KeyRunID: event.RunID, meta.KeyStatusKind: projection.StatusKind},
	}
	if runResult != nil && strings.TrimSpace(string(runResult.Outcome)) != "" {
		outbound.Metadata["outcome"] = string(runResult.Outcome)
	}
	applyDeliveryEnvelope(&outbound, runResult)
	failureTarget := RunNotificationTarget{
		RunID:        event.RunID,
		SessionKey:   session.Key,
		ChannelID:    b.cfg.ChannelName,
		TargetID:     targetID,
		ReplyToID:    messageID,
		InputContent: BridgeInputContent(session, run.InputEventID),
		Format:       defaultOutboundFormat(outbound.Format),
		Metadata:     cloneAnyMap(outbound.Metadata),
	}
	sendTerminal, finalized := b.streamingTerminalSender(event.RunID)
	if err := DeliverTerminalWithVerification(ctx, b.runtime, b.status, sendTerminal, session, run, outbound); err != nil {
		log.Error("bridge: send response", "channel", b.cfg.ChannelName, "error", err, "run_id", event.RunID)
		HandleOutboundDeliveryFailure(ctx, b.send, b.bus, DeliveryFailureContext{
			ChannelName: b.cfg.ChannelName,
			RunID:       event.RunID,
			SessionID:   event.SessionID,
			Target:      failureTarget,
		}, outbound, 1, err)
		return
	}
	if !finalized() {
		b.clearStreamingState(event.RunID)
	}
	b.markDelivered(event.RunID)
	log.Info("bridge: sent response", "channel", b.cfg.ChannelName, "run_id", event.RunID, "target", targetID)
}

func applyDeliveryEnvelope(outbound *OutboundMessage, result *runtimesvc.RunResult) {
	if outbound == nil || result == nil || result.Delivery == nil {
		return
	}
	if outbound.Metadata == nil {
		outbound.Metadata = make(map[string]any, 6)
	}
	outbound.Blocks = make([]OutboundBlock, 0, len(result.Delivery.Blocks))
	for _, block := range result.Delivery.Blocks {
		outbound.Blocks = append(outbound.Blocks, OutboundBlock{
			Kind:    block.Kind,
			Title:   block.Title,
			Content: block.Content,
		})
	}
	outbound.Attachments = make([]OutboundAttachment, 0, len(result.Delivery.Attachments))
	for _, item := range result.Delivery.Attachments {
		outbound.Attachments = append(outbound.Attachments, OutboundAttachment{
			Kind:        item.Kind,
			Label:       item.Label,
			URI:         item.URI,
			ContentType: item.ContentType,
		})
	}
	if result.Delivery.Verification != nil {
		outbound.Metadata["verification_status"] = result.Delivery.Verification.Status
		outbound.Metadata["verification_required_issues"] = result.Delivery.Verification.RequiredIssues
		outbound.Metadata["verification_advisory_issues"] = result.Delivery.Verification.AdvisoryIssues
	}
	if result.Delivery.Conversation != nil {
		ctx := result.Delivery.Conversation
		if ctx.ThreadID != "" {
			outbound.Metadata[meta.KeyThreadID] = ctx.ThreadID
		}
		if ctx.ParticipantID != "" {
			outbound.Metadata["participant_id"] = ctx.ParticipantID
		}
		if ctx.ParticipantName != "" {
			outbound.Metadata["participant_name"] = ctx.ParticipantName
		}
	}
}

func (b *Bridge) extractReplyTarget(session *agent.Session, inputEventID string) (targetID string, messageID string) {
	var fallbackTarget, fallbackMsg string

	for i := len(session.Messages) - 1; i >= 0; i-- {
		msg := session.Messages[i]
		if msg.Metadata == nil {
			continue
		}
		tid, _ := msg.Metadata[b.cfg.TargetIDKey].(string)
		mid, _ := msg.Metadata[b.cfg.MessageIDKey].(string)

		if strings.TrimSpace(tid) != "" && fallbackTarget == "" {
			fallbackTarget = strings.TrimSpace(tid)
			fallbackMsg = strings.TrimSpace(mid)
		}

		if inputEventID != "" && strings.TrimSpace(mid) == inputEventID {
			if strings.TrimSpace(tid) != "" {
				return strings.TrimSpace(tid), strings.TrimSpace(mid)
			}
			messageID = strings.TrimSpace(mid)
		}
	}

	if fallbackTarget != "" {
		if messageID == "" {
			messageID = fallbackMsg
		}
		return fallbackTarget, messageID
	}
	return "", ""
}

func (b *Bridge) isDelivered(runID string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.pruneDeliveredLocked(time.Now().UTC())
	_, ok := b.delivered[runID]
	return ok
}

func (b *Bridge) markDelivered(runID string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	now := time.Now().UTC()
	b.pruneDeliveredLocked(now)
	b.delivered[runID] = now
}

func (b *Bridge) pruneDeliveredLocked(now time.Time) {
	if b == nil {
		return
	}
	for runID, deliveredAt := range b.delivered {
		if deliveredAt.IsZero() || now.Sub(deliveredAt) >= BridgeDeliveredStateTTL {
			delete(b.delivered, runID)
		}
	}
}

func (b *Bridge) pruneStreamingStatesLocked(now time.Time) {
	if b == nil {
		return
	}
	for runID, state := range b.streams {
		if StreamingDeliveryStateStale(state, now) {
			delete(b.streams, runID)
		}
	}
}

func BridgeCompletedContent(session *agent.Session, runID string) string {
	var fallback string
	fallbackCount := 0
	for i := len(session.Messages) - 1; i >= 0; i-- {
		msg := session.Messages[i]
		if msg.Role != contextengine.RoleAssistant {
			continue
		}
		content := strings.TrimSpace(msg.Content)
		if content == "" {
			continue
		}
		if bridgeMessageRunID(msg.Metadata) == runID {
			return content
		}
		if fallbackCount == 0 {
			fallback = content
		}
		fallbackCount++
	}
	if fallbackCount == 1 {
		return fallback
	}
	return ""
}

func BridgeInputContent(session *agent.Session, inputEventID string) string {
	for i := len(session.Messages) - 1; i >= 0; i-- {
		msg := session.Messages[i]
		if msg.Role != contextengine.RoleUser {
			continue
		}
		if inputEventID != "" {
			if mid, _ := msg.Metadata[meta.KeyMessageID].(string); strings.TrimSpace(mid) == inputEventID {
				return msg.Content
			}
			continue
		}
		if strings.TrimSpace(msg.Content) != "" {
			return msg.Content
		}
	}
	return ""
}

func BridgeFailureMessage(inputContent string, raw string) string {
	if bridgeLooksLikeAuthFailure(raw) {
		if bridgeLooksChinese(inputContent) {
			return "后端模型鉴权失败，请联系管理员刷新凭据后重试。"
		}
		return "Backend model authentication failed. Please refresh credentials and try again."
	}
	return usererror.HumanizeText(raw, string(usererror.InferLocale(inputContent)))
}

func IsAuthFailure(raw string) bool {
	return bridgeLooksLikeAuthFailure(raw)
}

func bridgeMessageRunID(metadata map[string]any) string {
	if metadata == nil {
		return ""
	}
	runID, _ := metadata[meta.KeyRunID].(string)
	return strings.TrimSpace(runID)
}

func bridgeLooksLikeAuthFailure(raw string) bool {
	v := strings.ToLower(strings.TrimSpace(raw))
	return strings.Contains(v, "authentication_error") ||
		strings.Contains(v, "token has expired") ||
		strings.Contains(v, "invalid api key") ||
		strings.Contains(v, "unauthorized") ||
		strings.Contains(v, "status 401") ||
		strings.Contains(v, "api error (401)")
}

func bridgeLooksChinese(text string) bool {
	for _, r := range text {
		if unicode.Is(unicode.Han, r) {
			return true
		}
	}
	return false
}

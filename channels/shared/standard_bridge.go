package shared

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/fulcrus/hopclaw/agent"
	"github.com/fulcrus/hopclaw/channels"
	"github.com/fulcrus/hopclaw/eventbus"
	"github.com/fulcrus/hopclaw/internal/meta"
	"github.com/fulcrus/hopclaw/logging"
	runtimesvc "github.com/fulcrus/hopclaw/runtime"
)

var bridgeLog = logging.WithSubsystem("channels.sharedbridge")

type StandardBridgeConfig struct {
	ChannelName      string
	TargetIDKey      string
	MessageIDKey     string
	ThreadIDKey      string
	DirectPrefix     string
	DirectUsesChatID bool
}

type StandardBridge struct {
	adapter        channels.Adapter
	runtime        BridgeRuntime
	sessions       agent.SessionStore
	bus            *eventbus.InMemoryBus
	status         *channels.RunStatusNotifier
	authGate       *channels.AuthFailureGate
	projector      *channels.RunEventProjector
	threadBindings *channels.ThreadBinding
	policy         channels.PolicyConfig
	deduper        *channels.MessageDeduper
	outbound       *channels.OutboundSerializer

	cfg StandardBridgeConfig

	cancel context.CancelFunc

	mu        sync.Mutex
	delivered map[string]time.Time
	streams   map[string]*channels.StreamingDeliveryState
}

func NewStandardBridge(cfg StandardBridgeConfig, adapter channels.Adapter, runtime BridgeRuntime, sessions agent.SessionStore, bus *eventbus.InMemoryBus, statusDelay time.Duration) *StandardBridge {
	if strings.TrimSpace(cfg.ChannelName) == "" {
		cfg.ChannelName = "channel"
	}
	if strings.TrimSpace(cfg.TargetIDKey) == "" {
		cfg.TargetIDKey = "chat_id"
	}
	if strings.TrimSpace(cfg.MessageIDKey) == "" {
		cfg.MessageIDKey = "message_id"
	}
	serializer := channels.NewOutboundSerializer()

	var send func(context.Context, channels.OutboundMessage) error
	if adapter != nil {
		send = func(ctx context.Context, msg channels.OutboundMessage) error {
			return serializer.Do(func() error {
				return adapter.Send(ctx, msg)
			})
		}
	}

	return &StandardBridge{
		adapter:        adapter,
		runtime:        runtime,
		sessions:       sessions,
		bus:            bus,
		status:         channels.NewRunStatusNotifier(statusDelay, send),
		authGate:       channels.NewAuthFailureGate(channels.DefaultAuthFailureCooldown, channels.DefaultAuthFailureReminderInterval),
		projector:      channels.NewRunEventProjector(),
		threadBindings: channels.NewThreadBinding(),
		outbound:       serializer,
		cfg:            cfg,
		delivered:      make(map[string]time.Time),
		streams:        make(map[string]*channels.StreamingDeliveryState),
	}
}

func (b *StandardBridge) WithPolicy(policy channels.PolicyConfig) *StandardBridge {
	if b == nil {
		return nil
	}
	b.policy = channels.NormalizePolicyConfig(policy)
	return b
}

func (b *StandardBridge) WithMessageDeduper(deduper *channels.MessageDeduper) *StandardBridge {
	if b == nil {
		return nil
	}
	b.deduper = deduper
	return b
}

func (b *StandardBridge) WithThreadBindings(tb *channels.ThreadBinding) *StandardBridge {
	if b == nil {
		return nil
	}
	b.threadBindings = tb
	return b
}

func (b *StandardBridge) Start(ctx context.Context) {
	if b == nil || b.adapter == nil {
		return
	}
	cancel := StartBridgeLoops(ctx, b.adapter, b.bus, b.HandleInboundMessage, b.HandleOutboundEvent)
	b.mu.Lock()
	b.cancel = cancel
	b.mu.Unlock()
}

func (b *StandardBridge) Stop() {
	if b == nil {
		return
	}
	b.mu.Lock()
	cancel := b.cancel
	b.cancel = nil
	b.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

func (b *StandardBridge) RestoreRun(ctx context.Context, target channels.RunNotificationTarget, run *agent.Run) bool {
	if b == nil || b.status == nil {
		return false
	}
	return b.status.Restore(ctx, target, run)
}

func (b *StandardBridge) HandleInboundMessage(ctx context.Context, msg channels.InboundMessage) {
	if b == nil || b.runtime == nil {
		return
	}

	content := strings.TrimSpace(msg.Content)
	if content == "" && !channels.HasStructuredApprovalReply(msg.RawEvent) {
		return
	}

	targetID := firstAnyString(msg.RawEvent, b.cfg.TargetIDKey)
	if targetID == "" {
		targetID = strings.TrimSpace(msg.SenderID)
	}
	if targetID == "" {
		return
	}

	messageID := firstAnyString(msg.RawEvent, b.cfg.MessageIDKey)
	threadID := b.threadIDForMessage(msg)
	if b.deduper != nil && messageID != "" && b.deduper.Seen(strings.Join([]string{b.cfg.ChannelName, targetID, messageID}, ":")) {
		bridgeLog.Info("shared bridge: dropped duplicate inbound message", "channel", b.cfg.ChannelName, "target_id", targetID, "message_id", messageID)
		return
	}

	env := channels.PolicyEnvelope{
		SenderID:  strings.TrimSpace(msg.SenderID),
		ChatID:    targetID,
		ChatType:  standardBridgeChatType(b.cfg.ChannelName, msg.RawEvent),
		ThreadID:  threadID,
		MessageID: messageID,
		Mentioned: StandardBridgeMentioned(content, msg.RawEvent),
	}
	sessionKey := channels.SessionKeyForEnvelope(
		b.cfg.ChannelName,
		b.policy,
		env,
		channels.SessionKeyOptions{DirectPrefix: b.cfg.DirectPrefix, UseChatIDForDirect: b.cfg.DirectUsesChatID},
	)
	if threadID != "" && b.threadBindings != nil {
		if boundKey, ok := b.threadBindings.Resolve(b.cfg.ChannelName, threadID); ok {
			sessionKey = boundKey
		}
	}

	metadata := map[string]any{
		meta.KeyChannel:    b.cfg.ChannelName,
		meta.KeyMessageID:  messageID,
		b.cfg.TargetIDKey:  targetID,
		b.cfg.MessageIDKey: messageID,
		meta.KeySenderID:   msg.SenderID,
		meta.KeySenderName: msg.SenderName,
		"reply_in_thread":  b.policy.ReplyInThread,
	}
	metadata[meta.KeyChannelName] = b.cfg.ChannelName
	if threadID != "" {
		metadata["thread_id"] = threadID
	}
	channels.ApplyAdapterCapabilityMetadata(metadata, b.adapter)
	for key, value := range msg.RawEvent {
		if _, exists := metadata[key]; !exists {
			metadata[key] = value
		}
	}
	channels.NormalizeConversationMetadata(metadata, msg.RawEvent)
	channels.ApplyInboundScopeMetadata(b.runtime, metadata, msg.RawEvent)

	replyToID := b.replyTargetForMessage(messageID, threadID)
	target := channels.RunNotificationTarget{
		SessionKey:   sessionKey,
		ChannelID:    b.cfg.ChannelName,
		TargetID:     targetID,
		ReplyToID:    replyToID,
		InputContent: content,
		Format:       "text",
		Metadata:     metadata,
	}

	if cmd, ok := channels.ParseControlCommand(content); ok && (cmd == channels.ControlCommandBind || cmd == channels.ControlCommandUnbind) {
		if channels.HandleBindCommand(ctx, cmd, b.threadBindings, b.send, b.cfg.ChannelName, threadID, sessionKey, target) {
			return
		}
	}

	if decision := channels.EvaluatePolicy(b.policy, env); !decision.Allow {
		if strings.TrimSpace(decision.Notify) != "" && b.adapter != nil {
			_ = b.send(ctx, channels.OutboundMessage{
				ChannelID: b.cfg.ChannelName,
				TargetID:  target.TargetID,
				ReplyToID: target.ReplyToID,
				Content:   decision.Notify,
				Format:    "text",
				Metadata:  metadata,
			})
		}
		return
	}

	if blocked, notify := b.authGate.Blocked(sessionKey); blocked {
		if notify {
			b.status.NotifyBackendAuthUnavailable(ctx, target)
		}
		return
	}

	channels.HandleInboundInteract(ctx, b.runtime, b.bus, b.sessions, b.send, b.status, b.cfg.ChannelName, sessionKey, content, msg.ContentBlocks, msg.Images, messageID, metadata, target)
}

func (b *StandardBridge) HandleOutboundEvent(ctx context.Context, sub *eventbus.Subscription) {
	if b == nil || sub == nil {
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
			if channels.IsSilentRunCancellation(event) {
				b.status.Clear(event.RunID)
				continue
			}
			snapshot, _ := b.status.SnapshotRun(event.RunID)
			projection, projected := b.projector.ProjectLive(event, snapshot)
			if projected {
				switch projection.Kind {
				case channels.ProjectedRunEventPhase:
					b.status.NotifyPhaseChanged(event.RunID, projection.Phase, projection.ToolNames)
					continue
				case channels.ProjectedRunEventToolProgress:
					b.status.NotifyToolProgress(event.RunID, projection.ToolRounds, projection.ToolNames)
					continue
				case channels.ProjectedRunEventPlanProgress:
					b.status.NotifyPlanProgress(event.RunID, projection.ActiveTask, projection.Completed, projection.Total)
					continue
				case channels.ProjectedRunEventApproval:
					if approved, err := TryAutoApproveSkillInstall(ctx, b.runtime, b.sessions, b.status, event); approved && err == nil {
						continue
					}
					b.status.NotifyApproval(ctx, event.RunID)
					continue
				case channels.ProjectedRunEventResumed:
					b.status.NotifyResumed(ctx, event.RunID)
					continue
				case channels.ProjectedRunEventCancelled:
					b.status.NotifyCancelled(ctx, event.RunID)
					continue
				case channels.ProjectedRunEventStreaming:
					b.handleStreamingDelta(ctx, event.RunID, snapshot.Target, projection.Content)
					continue
				}
			}
			switch event.Type {
			case eventbus.EventRunCompleted, eventbus.EventRunFailed:
				b.status.Clear(event.RunID)
				b.HandleTerminalRun(ctx, event)
			}
		}
	}
}

func (b *StandardBridge) handleStreamingDelta(ctx context.Context, runID string, target channels.RunNotificationTarget, delta string) {
	streamer, ok := b.adapter.(channels.StreamingRenderer)
	if !ok || strings.TrimSpace(runID) == "" || strings.TrimSpace(target.TargetID) == "" || strings.TrimSpace(delta) == "" {
		return
	}

	state := b.ensureStreamingState(ctx, streamer, runID, target)
	if state == nil || strings.TrimSpace(state.Handle) == "" || state.Disabled {
		return
	}

	b.mu.Lock()
	state.Content = channels.MergeStreamingContent(state.Content, delta)
	content := state.Content
	lastSent := state.LastSent
	lastFlushAt := state.LastFlushAt
	handle := state.Handle
	channels.TouchStreamingDeliveryState(state, time.Now().UTC())
	b.mu.Unlock()

	now := time.Now().UTC()
	if content == lastSent || (!lastFlushAt.IsZero() && now.Sub(lastFlushAt) < channels.BridgeStreamingThrottle) {
		return
	}
	if err := b.outbound.Do(func() error {
		return streamer.UpdateStreaming(ctx, handle, content)
	}); err != nil {
		bridgeLog.Warn("shared bridge: update streaming failed", "channel", b.cfg.ChannelName, "error", err, "run_id", runID)
		return
	}

	b.mu.Lock()
	if current := b.streams[runID]; current != nil {
		current.LastSent = content
		current.LastFlushAt = now
		channels.TouchStreamingDeliveryState(current, now)
	}
	b.mu.Unlock()
}

func (b *StandardBridge) ensureStreamingState(ctx context.Context, streamer channels.StreamingRenderer, runID string, target channels.RunNotificationTarget) *channels.StreamingDeliveryState {
	now := time.Now().UTC()
	b.mu.Lock()
	b.pruneStreamingStatesLocked(now)
	if state := b.streams[runID]; state != nil {
		channels.TouchStreamingDeliveryState(state, now)
		b.mu.Unlock()
		return state
	}
	state := &channels.StreamingDeliveryState{}
	channels.TouchStreamingDeliveryState(state, now)
	b.streams[runID] = state
	b.mu.Unlock()

	var handle string
	err := b.outbound.Do(func() error {
		var beginErr error
		handle, beginErr = streamer.BeginStreaming(ctx, channels.OutboundMessage{
			ChannelID: target.ChannelID,
			TargetID:  target.TargetID,
			ReplyToID: target.ReplyToID,
			Content:   target.InputContent,
			Format:    target.Format,
			Metadata:  target.Metadata,
		})
		return beginErr
	})
	if err != nil {
		bridgeLog.Warn("shared bridge: begin streaming failed", "channel", b.cfg.ChannelName, "error", err, "run_id", runID)
		b.mu.Lock()
		if state := b.streams[runID]; state != nil {
			state.Disabled = true
			channels.TouchStreamingDeliveryState(state, time.Now().UTC())
		}
		b.mu.Unlock()
		return nil
	}

	b.mu.Lock()
	defer b.mu.Unlock()
	state = b.streams[runID]
	if state == nil {
		state = &channels.StreamingDeliveryState{}
		b.streams[runID] = state
	}
	state.Handle = handle
	channels.TouchStreamingDeliveryState(state, time.Now().UTC())
	return state
}

func (b *StandardBridge) streamingTerminalSender(runID string) (func(context.Context, channels.OutboundMessage) error, func() bool) {
	streamer, ok := b.adapter.(channels.StreamingRenderer)
	if !ok {
		return b.send, func() bool { return false }
	}

	now := time.Now().UTC()
	b.mu.Lock()
	b.pruneStreamingStatesLocked(now)
	state := b.streams[runID]
	channels.TouchStreamingDeliveryState(state, now)
	b.mu.Unlock()
	if state == nil || state.Disabled || strings.TrimSpace(state.Handle) == "" {
		return b.send, func() bool { return false }
	}

	finalized := false
	send := func(ctx context.Context, msg channels.OutboundMessage) error {
		if err := b.outbound.Do(func() error {
			return streamer.EndStreaming(ctx, state.Handle, msg)
		}); err != nil {
			bridgeLog.Warn("shared bridge: end streaming failed, falling back to send", "channel", b.cfg.ChannelName, "error", err, "run_id", runID)
			b.clearStreamingState(runID)
			return b.send(ctx, msg)
		}
		finalized = true
		b.clearStreamingState(runID)
		return nil
	}
	return send, func() bool { return finalized }
}

func (b *StandardBridge) clearStreamingState(runID string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.streams, runID)
}

func (b *StandardBridge) send(ctx context.Context, msg channels.OutboundMessage) error {
	if b == nil || b.adapter == nil {
		return fmt.Errorf("shared bridge: adapter is required")
	}
	return b.outbound.Do(func() error {
		return b.adapter.Send(ctx, msg)
	})
}

func (b *StandardBridge) threadIDForMessage(msg channels.InboundMessage) string {
	if b == nil {
		return ""
	}
	if b.cfg.ThreadIDKey != "" {
		if threadID := firstAnyString(msg.RawEvent, b.cfg.ThreadIDKey); threadID != "" {
			return threadID
		}
	}
	return firstAnyString(msg.RawEvent, "thread_id", "thread_ts", "thread_name", "topic_id", "message_thread_id", "root_id", "reply_to_id")
}

func (b *StandardBridge) replyTargetForMessage(messageID, threadID string) string {
	if b != nil && b.policy.ReplyInThread && threadID != "" {
		return threadID
	}
	return messageID
}

// HandleTerminalRun processes a completed or failed run event, delivering the
// result to the channel. It is exported so per-channel test suites can exercise
// terminal-run delivery without the full outbound loop.
func (b *StandardBridge) HandleTerminalRun(ctx context.Context, event eventbus.Event) {
	if b == nil || event.RunID == "" || event.SessionID == "" || b.runtime == nil {
		return
	}
	if b.isDelivered(event.RunID) {
		return
	}

	session, err := agent.LoadSession(ctx, b.sessions, event.SessionID, agent.ScopeFilter{})
	if err != nil {
		bridgeLog.Error("shared bridge: load session", "channel", b.cfg.ChannelName, "error", err, "session_id", event.SessionID)
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
		if channels.IsAuthFailure(raw) {
			b.authGate.Arm(session.Key)
		} else {
			b.authGate.Clear(session.Key)
		}
	}

	run, err := b.runtime.GetRun(ctx, event.RunID)
	if err != nil {
		bridgeLog.Error("shared bridge: load run", "channel", b.cfg.ChannelName, "error", err, "run_id", event.RunID)
		return
	}
	runResult, resultErr := channels.GetRunResultIfSupported(ctx, b.runtime, event.RunID)
	if resultErr != nil {
		bridgeLog.Warn("shared bridge: load run result", "channel", b.cfg.ChannelName, "error", resultErr, "run_id", event.RunID)
	}
	runVerification, verificationErr := channels.GetRunVerificationIfSupported(ctx, b.runtime, event.RunID)
	if verificationErr != nil {
		bridgeLog.Warn("shared bridge: load run verification", "channel", b.cfg.ChannelName, "error", verificationErr, "run_id", event.RunID)
	}

	targetID, messageID := b.ExtractReplyTarget(session, run.InputEventID)
	if targetID == "" {
		bridgeLog.Warn("shared bridge: no delivery target found", "channel", b.cfg.ChannelName, "run_id", event.RunID, "session_id", event.SessionID)
		return
	}

	projection, ok := b.projector.ProjectTerminal(session, run, runResult, runVerification, event)
	if !ok || strings.TrimSpace(projection.Content) == "" {
		return
	}

	outbound := channels.OutboundMessage{
		ChannelID: b.cfg.ChannelName,
		TargetID:  targetID,
		ReplyToID: messageID,
		Content:   projection.Content,
		Format:    "text",
		Metadata: map[string]any{
			meta.KeyRunID:      event.RunID,
			meta.KeyStatusKind: projection.StatusKind,
		},
	}
	if runResult != nil && strings.TrimSpace(string(runResult.Outcome)) != "" {
		outbound.Metadata["outcome"] = string(runResult.Outcome)
	}
	applyStandardDeliveryEnvelope(&outbound, runResult)
	failureTarget := channels.RunNotificationTarget{
		RunID:        event.RunID,
		SessionKey:   session.Key,
		ChannelID:    b.cfg.ChannelName,
		TargetID:     targetID,
		ReplyToID:    messageID,
		InputContent: channels.BridgeInputContent(session, run.InputEventID),
		Format:       "text",
		Metadata:     outbound.Metadata,
	}
	sendTerminal, finalized := b.streamingTerminalSender(event.RunID)
	if err := channels.DeliverTerminalWithVerification(ctx, b.runtime, b.status, sendTerminal, session, run, outbound); err != nil {
		bridgeLog.Error("shared bridge: send response", "channel", b.cfg.ChannelName, "error", err, "run_id", event.RunID, "target_id", targetID)
		channels.HandleOutboundDeliveryFailure(ctx, b.send, b.bus, channels.DeliveryFailureContext{
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
	bridgeLog.Info("shared bridge: sent response", "channel", b.cfg.ChannelName, "run_id", event.RunID, "target_id", targetID, "reply_to", messageID)
}

// ExtractReplyTarget finds the target and message IDs for a reply by scanning
// session metadata. Exported so per-channel test suites can verify reply
// routing without the full outbound loop.
func (b *StandardBridge) ExtractReplyTarget(session *agent.Session, inputEventID string) (targetID string, messageID string) {
	if b == nil || session == nil {
		return "", ""
	}

	var fallbackTarget string
	var fallbackMessageID string

	for index := len(session.Messages) - 1; index >= 0; index-- {
		msg := session.Messages[index]
		if msg.Metadata == nil {
			continue
		}

		candidateTargetID := firstAnyString(msg.Metadata, b.cfg.TargetIDKey)
		candidateMessageID := firstAnyString(msg.Metadata, b.cfg.MessageIDKey)
		if candidateTargetID != "" && fallbackTarget == "" {
			fallbackTarget = candidateTargetID
			fallbackMessageID = candidateMessageID
		}

		if inputEventID != "" && candidateMessageID == inputEventID {
			if candidateTargetID != "" {
				return candidateTargetID, candidateMessageID
			}
			messageID = candidateMessageID
		}
	}

	if fallbackTarget != "" {
		if messageID == "" {
			messageID = fallbackMessageID
		}
		return fallbackTarget, messageID
	}
	return "", ""
}

func (b *StandardBridge) isDelivered(runID string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.pruneDeliveredLocked(time.Now().UTC())
	_, ok := b.delivered[runID]
	return ok
}

func (b *StandardBridge) markDelivered(runID string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	now := time.Now().UTC()
	b.pruneDeliveredLocked(now)
	b.delivered[runID] = now
}

func (b *StandardBridge) pruneDeliveredLocked(now time.Time) {
	if b == nil {
		return
	}
	for runID, deliveredAt := range b.delivered {
		if deliveredAt.IsZero() || now.Sub(deliveredAt) >= channels.BridgeDeliveredStateTTL {
			delete(b.delivered, runID)
		}
	}
}

func (b *StandardBridge) pruneStreamingStatesLocked(now time.Time) {
	if b == nil {
		return
	}
	for runID, state := range b.streams {
		if channels.StreamingDeliveryStateStale(state, now) {
			delete(b.streams, runID)
		}
	}
}

func applyStandardDeliveryEnvelope(outbound *channels.OutboundMessage, result *runtimesvc.RunResult) {
	if outbound == nil || result == nil || result.Delivery == nil {
		return
	}
	if outbound.Metadata == nil {
		outbound.Metadata = make(map[string]any, 6)
	}

	outbound.Blocks = make([]channels.OutboundBlock, 0, len(result.Delivery.Blocks))
	for _, block := range result.Delivery.Blocks {
		outbound.Blocks = append(outbound.Blocks, channels.OutboundBlock{
			Kind:    block.Kind,
			Title:   block.Title,
			Content: block.Content,
		})
	}

	outbound.Attachments = make([]channels.OutboundAttachment, 0, len(result.Delivery.Attachments))
	for _, item := range result.Delivery.Attachments {
		outbound.Attachments = append(outbound.Attachments, channels.OutboundAttachment{
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
		conversation := result.Delivery.Conversation
		if conversation.ThreadID != "" {
			outbound.Metadata[meta.KeyThreadID] = conversation.ThreadID
		}
		if conversation.ParticipantID != "" {
			outbound.Metadata["participant_id"] = conversation.ParticipantID
		}
		if conversation.ParticipantName != "" {
			outbound.Metadata["participant_name"] = conversation.ParticipantName
		}
	}
}

func standardBridgeChatType(_ string, raw map[string]any) string {
	return channels.InferPolicyChatType(raw)
}

func StandardBridgeMentioned(content string, raw map[string]any) bool {
	for _, key := range []string{"mentioned", "bot_mentioned", "is_mentioned"} {
		switch value := raw[key].(type) {
		case bool:
			if value {
				return true
			}
		case string:
			normalized := strings.ToLower(strings.TrimSpace(value))
			if normalized == "true" || normalized == "1" || normalized == "yes" {
				return true
			}
		}
	}
	// Content-based mention detection for platforms that use inline mentions
	// (e.g. Discord/Slack <@USER> or Telegram @bot).
	if strings.Contains(content, "<@") || strings.Contains(content, "@") {
		return true
	}
	return false
}

func firstAnyString(values map[string]any, keys ...string) string {
	if values == nil {
		return ""
	}
	for _, key := range keys {
		if value, ok := values[key]; ok {
			if trimmed := strings.TrimSpace(fmt.Sprint(value)); trimmed != "" {
				return trimmed
			}
		}
	}
	return ""
}

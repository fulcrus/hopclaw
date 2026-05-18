package feishu

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/fulcrus/hopclaw/agent"
	"github.com/fulcrus/hopclaw/channels"
	channelpairing "github.com/fulcrus/hopclaw/channels/pairing"
	sharedbridge "github.com/fulcrus/hopclaw/channels/shared"
	"github.com/fulcrus/hopclaw/eventbus"
	"github.com/fulcrus/hopclaw/internal/meta"
	runtimesvc "github.com/fulcrus/hopclaw/runtime"
	"golang.org/x/sync/singleflight"
)

type replyTarget struct {
	accountID     string
	targetID      string
	replyToID     string
	receiveIDType string
	replyInThread bool
}

// Bridge connects a channel adapter to the HopClaw runtime.
// It forwards inbound messages to the runtime and sends terminal
// run results back through the adapter.
type Bridge struct {
	adapter   channels.Adapter
	runtime   sharedbridge.BridgeRuntime
	sessions  agent.SessionStore
	bus       *eventbus.InMemoryBus
	status    *channels.RunStatusNotifier
	authGate  *channels.AuthFailureGate
	projector *channels.RunEventProjector

	cancel context.CancelFunc

	mu        sync.Mutex
	delivered map[string]time.Time
	deduper   *MessageDeduper
	pairing   *channelpairing.Manager
	streams   map[string]*streamingState
	typing    map[string]*typingState
	typingCB  typingCircuitBreaker
	streamOps singleflight.Group
}

func NewBridge(adapter channels.Adapter, runtime sharedbridge.BridgeRuntime, sessions agent.SessionStore, bus *eventbus.InMemoryBus, statusDelay time.Duration) *Bridge {
	var send func(context.Context, channels.OutboundMessage) error
	if adapter != nil {
		send = adapter.Send
	}
	return &Bridge{
		adapter:   adapter,
		runtime:   runtime,
		sessions:  sessions,
		bus:       bus,
		status:    channels.NewRunStatusNotifier(statusDelay, send),
		authGate:  channels.NewAuthFailureGate(channels.DefaultAuthFailureCooldown, channels.DefaultAuthFailureReminderInterval),
		projector: channels.NewRunEventProjector(),
		delivered: make(map[string]time.Time),
		streams:   make(map[string]*streamingState),
		typing:    make(map[string]*typingState),
	}
}

func (b *Bridge) WithMessageDeduper(deduper *MessageDeduper) *Bridge {
	if b == nil {
		return nil
	}
	b.deduper = deduper
	return b
}

func (b *Bridge) WithPairing(manager *channelpairing.Manager) *Bridge {
	if b == nil {
		return nil
	}
	b.pairing = manager
	return b
}

// Start begins listening for inbound messages and terminal run events.
func (b *Bridge) Start(ctx context.Context) {
	if b == nil || b.adapter == nil {
		return
	}
	cancel := sharedbridge.StartBridgeLoops(ctx, b.adapter, b.bus, b.handleInbound, b.outboundLoop)
	b.mu.Lock()
	b.cancel = cancel
	b.mu.Unlock()
}

func (b *Bridge) Stop() {
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
	b.clearTransientState()
}

func (b *Bridge) RestoreRun(ctx context.Context, target channels.RunNotificationTarget, run *agent.Run) bool {
	if b == nil || b.status == nil {
		return false
	}
	return b.status.Restore(ctx, target, run)
}

func (b *Bridge) handleInbound(ctx context.Context, msg channels.InboundMessage) {
	if b.runtime == nil {
		return
	}
	account := b.resolveAccount(msg)
	envelope := inboundEnvelope{
		AccountID: account.ID,
		MessageID: rawString(msg.RawEvent, "message_id"),
		SenderID:  msg.SenderID,
		ChatID:    rawString(msg.RawEvent, "chat_id"),
		ChatType:  rawString(msg.RawEvent, "chat_type"),
		ThreadID:  rawString(msg.RawEvent, "thread_id"),
		RootID:    rawString(msg.RawEvent, "root_id"),
		ParentID:  rawString(msg.RawEvent, "parent_id"),
		Mentioned: rawBool(msg.RawEvent, "mentioned"),
	}
	if b.deduper != nil {
		key := strings.Join([]string{"feishu", account.ID, envelope.MessageID}, ":")
		if envelope.MessageID != "" && b.deduper.Seen(key) {
			log.Info("feishu bridge: dropped duplicate inbound message", "account_id", account.ID, "message_id", envelope.MessageID)
			return
		}
	}
	content := strings.TrimSpace(msg.Content)
	if content == "" {
		return
	}

	chatID := envelope.ChatID
	messageID := envelope.MessageID

	sessionKey := sessionKeyForInbound(envelope, account)
	notifyTarget := buildNotificationTarget(content, replyTarget{
		accountID:     account.ID,
		targetID:      strings.TrimSpace(chatID),
		replyToID:     strings.TrimSpace(messageID),
		receiveIDType: "chat_id",
		replyInThread: account.ReplyInThread || envelope.ThreadID != "",
	})
	if notifyTarget.TargetID == "" {
		notifyTarget = buildNotificationTarget(content, replyTarget{
			accountID:     account.ID,
			targetID:      strings.TrimSpace(msg.SenderID),
			replyToID:     strings.TrimSpace(messageID),
			receiveIDType: "open_id",
			replyInThread: false,
		})
	}
	if envelope.ThreadID != "" && (account.GroupSessionScope == "group_thread" || account.GroupSessionScope == "group_thread_sender") {
		notifyTarget.TargetID = strings.TrimSpace(envelope.ThreadID)
		notifyTarget.Metadata[meta.KeyReceiveIDType] = "thread_id"
		notifyTarget.Metadata["reply_in_thread"] = true
	}
	notifyTarget.SessionKey = sessionKey

	metadata := map[string]any{
		meta.KeyChannel:       "feishu",
		meta.KeyChannelName:   "feishu",
		meta.KeyChatID:        chatID,
		meta.KeyMessageID:     messageID,
		meta.KeySenderID:      msg.SenderID,
		meta.KeySenderName:    msg.SenderName,
		meta.KeyReceiveIDType: notifyTarget.Metadata[meta.KeyReceiveIDType],
		"account_id":          account.ID,
		"thread_id":           envelope.ThreadID,
		"root_id":             envelope.RootID,
		"parent_id":           envelope.ParentID,
		"reply_in_thread":     rawBool(notifyTarget.Metadata, "reply_in_thread"),
	}
	channels.ApplyAdapterCapabilityMetadata(metadata, b.adapter)
	channels.NormalizeConversationMetadata(metadata, msg.RawEvent)
	channels.ApplyInboundScopeMetadata(b.runtime, metadata, msg.RawEvent)
	if b.handlePairingMessage(ctx, account, envelope, content, notifyTarget) {
		return
	}
	if account.DMPolicy == "pairing" && b.pairing != nil && envelope.ChatType == "p2p" && b.pairing.IsVerified(pairingChannelName(account.ID), envelope.SenderID) {
		account.DMPolicy = "open"
	}
	if decision := evaluateInboundPolicy(account, envelope); !decision.allow {
		if strings.TrimSpace(decision.notify) != "" {
			_ = b.adapter.Send(ctx, channels.OutboundMessage{
				ChannelID: "feishu",
				TargetID:  notifyTarget.TargetID,
				ReplyToID: notifyTarget.ReplyToID,
				Content:   decision.notify,
				Metadata:  notifyTarget.Metadata,
			})
		}
		return
	}

	if blocked, notify := b.authGate.Blocked(sessionKey); blocked {
		if notify {
			b.status.NotifyBackendAuthUnavailable(ctx, notifyTarget)
		}
		return
	}
	result := channels.HandleInboundInteract(ctx, b.runtime, b.bus, b.sessions, b.adapter.Send, b.status, "feishu", sessionKey, content, msg.ContentBlocks, msg.Images, messageID, metadata, notifyTarget)
	if result != nil && result.Decision.ReplyAct == runtimesvc.ReplyActTaskAccept && result.Run != nil && strings.TrimSpace(result.Run.SessionID) != "" {
		if err := channels.EnableSessionAutoApproveSession(ctx, b.sessions, result.Run.SessionID); err != nil {
			log.Error("feishu bridge: auto-approve session", "error", err, "session_id", result.Run.SessionID)
		}
	}
}

func (b *Bridge) tryAutoApproveSkillInstall(ctx context.Context, event eventbus.Event) bool {
	if b == nil {
		return false
	}
	approved, err := sharedbridge.TryAutoApproveSkillInstall(ctx, b.runtime, b.sessions, b.status, event)
	if err != nil {
		log.Error("feishu bridge: auto-approve skill install", "error", err, "run_id", event.RunID)
		return false
	}
	return approved
}

func (b *Bridge) outboundLoop(ctx context.Context, sub *eventbus.Subscription) {
	if sub == nil {
		return
	}
	defer sub.Close()
	defer b.clearTransientState()

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
			b.handleLifecycleEvent(ctx, event, snapshot)
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
					if b.tryAutoApproveSkillInstall(ctx, event) {
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
				b.stopTyping(ctx, event.RunID)
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
		log.Error("feishu bridge: load session", "error", err, "session_id", event.SessionID)
		return
	}
	if agent.SessionChannelName(session) != "feishu" {
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
		if looksLikeAuthFailure(raw) {
			b.authGate.Arm(session.Key)
		} else {
			b.authGate.Clear(session.Key)
		}
	}

	run, err := b.runtime.GetRun(ctx, event.RunID)
	if err != nil {
		log.Error("feishu bridge: load run", "error", err, "run_id", event.RunID)
		return
	}
	runResult, resultErr := channels.GetRunResultIfSupported(ctx, b.runtime, event.RunID)
	if resultErr != nil {
		log.Warn("feishu bridge: load run result", "error", resultErr, "run_id", event.RunID)
	}
	runVerification, verificationErr := channels.GetRunVerificationIfSupported(ctx, b.runtime, event.RunID)
	if verificationErr != nil {
		log.Warn("feishu bridge: load run verification", "error", verificationErr, "run_id", event.RunID)
	}

	target := b.extractReplyTarget(session, run.InputEventID)
	if target.targetID == "" {
		log.Warn("feishu bridge: no delivery target found", "run_id", event.RunID, "session_id", event.SessionID)
		return
	}

	projection, ok := b.projector.ProjectTerminal(session, run, runResult, runVerification, event)
	if !ok || strings.TrimSpace(projection.Content) == "" {
		return
	}
	streamingMetadata := enrichStreamingMetadata(map[string]any{
		"account_id":      target.accountID,
		"reply_in_thread": target.replyInThread,
	}, target)
	if b.finalizeStreaming(ctx, event.RunID, projection.Content, streamingMetadata) {
		b.markDelivered(event.RunID)
		return
	}

	outbound := channels.OutboundMessage{
		ChannelID: "feishu",
		TargetID:  target.targetID,
		ReplyToID: target.replyToID,
		Content:   projection.Content,
		Format:    "text",
		Metadata: map[string]any{
			meta.KeyReceiveIDType: target.receiveIDType,
			meta.KeyRunID:         event.RunID,
			meta.KeyStatusKind:    projection.StatusKind,
			"account_id":          target.accountID,
			"reply_in_thread":     target.replyInThread,
		},
	}
	if err := channels.DeliverTerminalWithVerification(ctx, b.runtime, b.status, b.adapter.Send, session, run, outbound); err != nil {
		log.Error("feishu bridge: send response", "error", err, "run_id", event.RunID, "target_id", target.targetID)
		channels.HandleOutboundDeliveryFailure(ctx, b.adapter.Send, b.bus, channels.DeliveryFailureContext{
			ChannelName: "feishu",
			RunID:       event.RunID,
			SessionID:   event.SessionID,
			Target: channels.RunNotificationTarget{
				RunID:        event.RunID,
				SessionKey:   session.Key,
				ChannelID:    "feishu",
				TargetID:     target.targetID,
				ReplyToID:    target.replyToID,
				InputContent: channels.BridgeInputContent(session, run.InputEventID),
				Format:       "text",
				Metadata:     outbound.Metadata,
			},
		}, outbound, 1, err)
		return
	}
	b.markDelivered(event.RunID)
	log.Info("feishu bridge: sent response", "run_id", event.RunID, "target_id", target.targetID, "reply_to", target.replyToID)
}

func (b *Bridge) extractReplyTarget(session *agent.Session, inputEventID string) replyTarget {
	target := replyTarget{receiveIDType: "chat_id"}
	var fallback replyTarget

	for i := len(session.Messages) - 1; i >= 0; i-- {
		msg := session.Messages[i]
		candidate := replyTargetFromMetadata(msg.Metadata)
		if candidate.targetID != "" && fallback.targetID == "" {
			fallback = candidate
		}
		if inputEventID == "" {
			continue
		}
		messageID, _ := msg.Metadata[meta.KeyMessageID].(string)
		if strings.TrimSpace(messageID) == inputEventID {
			if candidate.targetID != "" {
				if candidate.replyToID == "" {
					candidate.replyToID = messageID
				}
				return candidate
			}
			target.replyToID = messageID
		}
	}

	if fallback.targetID != "" {
		if target.replyToID != "" && fallback.replyToID == "" {
			fallback.replyToID = target.replyToID
		}
		return fallback
	}
	return target
}

func replyTargetFromMetadata(metadata map[string]any) replyTarget {
	if metadata == nil {
		return replyTarget{}
	}
	if chatID, ok := metadata[meta.KeyChatID].(string); ok && strings.TrimSpace(chatID) != "" {
		messageID, _ := metadata[meta.KeyMessageID].(string)
		accountID, _ := metadata["account_id"].(string)
		replyInThread, _ := metadata["reply_in_thread"].(bool)
		receiveIDType := "chat_id"
		if threadID, _ := metadata["thread_id"].(string); strings.TrimSpace(threadID) != "" {
			if value, _ := metadata[meta.KeyReceiveIDType].(string); strings.TrimSpace(value) == "thread_id" {
				chatID = threadID
				receiveIDType = "thread_id"
				replyInThread = true
			}
		}
		return replyTarget{
			accountID:     strings.TrimSpace(accountID),
			targetID:      strings.TrimSpace(chatID),
			replyToID:     strings.TrimSpace(messageID),
			receiveIDType: receiveIDType,
			replyInThread: replyInThread,
		}
	}
	if senderID, ok := metadata["sender_id"].(string); ok && strings.TrimSpace(senderID) != "" {
		messageID, _ := metadata[meta.KeyMessageID].(string)
		accountID, _ := metadata["account_id"].(string)
		return replyTarget{
			accountID:     strings.TrimSpace(accountID),
			targetID:      strings.TrimSpace(senderID),
			replyToID:     strings.TrimSpace(messageID),
			receiveIDType: "open_id",
		}
	}
	return replyTarget{}
}

func buildNotificationTarget(inputContent string, target replyTarget) channels.RunNotificationTarget {
	if target.receiveIDType == "" {
		target.receiveIDType = "chat_id"
	}
	return channels.RunNotificationTarget{
		ChannelID:    "feishu",
		TargetID:     target.targetID,
		ReplyToID:    target.replyToID,
		InputContent: inputContent,
		Format:       "text",
		Metadata: map[string]any{
			meta.KeyReceiveIDType: target.receiveIDType,
			"account_id":          target.accountID,
			"reply_in_thread":     target.replyInThread,
		},
	}
}

func (b *Bridge) resolveAccount(msg channels.InboundMessage) ResolvedAccount {
	provider, ok := b.adapter.(accountConfigProvider)
	if !ok {
		return ResolvedAccount{ID: rawString(msg.RawEvent, "account_id")}
	}
	accountID := rawString(msg.RawEvent, "account_id")
	if strings.TrimSpace(accountID) == "" {
		accountID = provider.DefaultAccountID()
	}
	account, ok := provider.Account(accountID)
	if !ok {
		return ResolvedAccount{ID: accountID, GroupSessionScope: "group"}
	}
	return account
}

func rawString(values map[string]any, key string) string {
	if values == nil {
		return ""
	}
	value, _ := values[key].(string)
	return strings.TrimSpace(value)
}

func rawBool(values map[string]any, key string) bool {
	if values == nil {
		return false
	}
	value, _ := values[key].(bool)
	return value
}

func looksLikeAuthFailure(raw string) bool {
	value := strings.ToLower(strings.TrimSpace(raw))
	return strings.Contains(value, "authentication_error") ||
		strings.Contains(value, "token has expired") ||
		strings.Contains(value, "invalid api key") ||
		strings.Contains(value, "unauthorized") ||
		strings.Contains(value, "status 401") ||
		strings.Contains(value, "api error (401)")
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
		if deliveredAt.IsZero() || now.Sub(deliveredAt) >= channels.BridgeDeliveredStateTTL {
			delete(b.delivered, runID)
		}
	}
}

func (b *Bridge) pruneStreamingStatesLocked(now time.Time) {
	if b == nil {
		return
	}
	for runID, state := range b.streams {
		if streamingStateStale(state, now) {
			delete(b.streams, runID)
		}
	}
}

func (b *Bridge) clearTransientState() {
	if b == nil {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	for runID, state := range b.typing {
		if state != nil && state.cancel != nil {
			state.cancel()
		}
		delete(b.typing, runID)
	}
	b.delivered = make(map[string]time.Time)
	b.streams = make(map[string]*streamingState)
}

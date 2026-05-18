package channels

import (
	"context"
	"strings"

	"github.com/fulcrus/hopclaw/agent"
	"github.com/fulcrus/hopclaw/contextengine"
	"github.com/fulcrus/hopclaw/eventbus"
	"github.com/fulcrus/hopclaw/internal/meta"
	runtimesvc "github.com/fulcrus/hopclaw/runtime"
)

func (b *Bridge) inboundLoop(ctx context.Context, inbound <-chan InboundMessage) {
	for {
		select {
		case <-ctx.Done():
			return
		case msg, ok := <-inbound:
			if !ok {
				return
			}
			b.handleInbound(ctx, msg)
		}
	}
}

func (b *Bridge) handleInbound(ctx context.Context, msg InboundMessage) {
	if b.runtime == nil {
		return
	}
	content := strings.TrimSpace(msg.Content)
	if content == "" && !HasStructuredApprovalReply(msg.RawEvent) {
		return
	}

	targetID, _ := msg.RawEvent[b.cfg.TargetIDKey].(string)
	messageID, _ := msg.RawEvent[b.cfg.MessageIDKey].(string)
	if strings.TrimSpace(targetID) == "" {
		targetID = strings.TrimSpace(msg.SenderID)
	}
	if targetID == "" {
		return
	}
	threadID := b.threadIDForMessage(msg)
	if b.deduper != nil && strings.TrimSpace(messageID) != "" {
		if b.deduper.Seen(strings.Join([]string{b.cfg.ChannelName, targetID, messageID}, ":")) {
			log.Info("bridge: dropped duplicate inbound message", "channel", b.cfg.ChannelName, "target_id", targetID, "message_id", messageID)
			return
		}
	}
	env := PolicyEnvelope{
		SenderID:  strings.TrimSpace(msg.SenderID),
		ChatID:    strings.TrimSpace(targetID),
		ChatType:  inferChatType(b.cfg.ChannelName, msg.RawEvent),
		ThreadID:  threadID,
		MessageID: strings.TrimSpace(messageID),
		Mentioned: inferMentioned(msg.Content, msg.RawEvent),
	}
	sessionKey := SessionKeyForEnvelope(b.cfg.ChannelName, b.policy, env, SessionKeyOptions{UseChatIDForDirect: b.directUsesChatID})
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
	}
	metadata[meta.KeyChannelName] = b.cfg.ChannelName
	if threadID != "" {
		metadata["thread_id"] = threadID
	}
	metadata["reply_in_thread"] = b.policy.ReplyInThread
	ApplyAdapterCapabilityMetadata(metadata, b.adapter)
	for k, v := range msg.RawEvent {
		if _, exists := metadata[k]; !exists {
			metadata[k] = v
		}
	}
	normalizeConversationMetadata(metadata, msg.RawEvent)
	ApplyInboundScopeMetadata(b.runtime, metadata, msg.RawEvent)
	replyToID := b.replyTargetForMessage(messageID, threadID)
	if cmd, ok := ParseControlCommand(content); ok && (cmd == ControlCommandBind || cmd == ControlCommandUnbind) {
		if HandleBindCommand(ctx, cmd, b.threadBindings, b.send, b.cfg.ChannelName, threadID, sessionKey, RunNotificationTarget{
			SessionKey:   sessionKey,
			ChannelID:    b.cfg.ChannelName,
			TargetID:     targetID,
			ReplyToID:    replyToID,
			InputContent: content,
			Format:       "text",
			Metadata:     metadata,
		}) {
			return
		}
	}
	if decision := EvaluatePolicy(b.policy, env); !decision.Allow {
		if strings.TrimSpace(decision.Notify) != "" {
			_ = b.send(ctx, OutboundMessage{
				ChannelID: b.cfg.ChannelName,
				TargetID:  targetID,
				ReplyToID: replyToID,
				Content:   decision.Notify,
				Format:    "text",
				Metadata:  metadata,
			})
		}
		return
	}
	notifyTarget := RunNotificationTarget{
		SessionKey:   sessionKey,
		ChannelID:    b.cfg.ChannelName,
		TargetID:     targetID,
		ReplyToID:    replyToID,
		InputContent: content,
		Format:       "text",
		Metadata:     metadata,
	}
	if blocked, notify := b.authGate.Blocked(sessionKey); blocked {
		if notify {
			b.status.NotifyBackendAuthUnavailable(ctx, notifyTarget)
		}
		return
	}

	b.handleInboundViaInteract(ctx, b.runtime, sessionKey, content, msg.ContentBlocks, msg.Images, messageID, metadata, notifyTarget)
}

// HandleInboundInteract is the shared Interact entry point. It pre-parses
// structured signals, calls Interact, and delivers the result. Returns the
// result so callers can apply platform-specific post-hooks (e.g. auto-approve).
// Returns nil on error (error is already communicated via the status notifier).
func HandleInboundInteract(
	ctx context.Context,
	interactable InteractableRuntime,
	events eventbus.Bus,
	sessions agent.SessionStore,
	send func(context.Context, OutboundMessage) error,
	status *RunStatusNotifier,
	channelName string,
	sessionKey, content string,
	contentBlocks []contextengine.ContentBlock,
	images []string,
	externalEventID string,
	metadata map[string]any,
	target RunNotificationTarget,
) *runtimesvc.InteractionResult {
	automationID := inboundScopeValues(metadata)
	interactReq := runtimesvc.InteractionRequest{
		SessionKey:      sessionKey,
		Content:         content,
		ContentBlocks:   append([]contextengine.ContentBlock(nil), contentBlocks...),
		Images:          append([]string(nil), images...),
		ExternalEventID: externalEventID,
		AutomationID:    automationID,
		Metadata:        metadata,
	}
	if action, source, ok := ParseApprovalReplySignal(metadata, content); ok {
		if metadata == nil {
			metadata = make(map[string]any, 2)
			interactReq.Metadata = metadata
		}
		metadata["approval_reply_source"] = string(source)
		if source == ApprovalReplySourceDeprecatedText {
			metadata["approval_reply_deprecated_fallback"] = true
		}
		interactReq.StructuredApproval = &runtimesvc.StructuredApproval{Action: string(action)}
	}
	if cmd, ok := ParseControlCommand(content); ok {
		interactReq.StructuredCommand = &runtimesvc.StructuredCommand{Kind: string(cmd)}
	}
	result, err := interactable.Interact(ctx, interactReq)
	if err != nil {
		log.Error("bridge: interact failed", "channel", channelName, "error", err, "session_key", sessionKey)
		if status != nil {
			status.NotifySubmitFailure(ctx, target, err.Error())
		}
		return nil
	}
	persistSessionAutoApproveOnAlwaysReply(ctx, sessions, channelName, metadata, content, result)
	DeliverInteractionResult(ctx, send, events, status, channelName, result, target)
	return result
}

func (b *Bridge) handleInboundViaInteract(ctx context.Context, interactable InteractableRuntime, sessionKey, content string, contentBlocks []contextengine.ContentBlock, images []string, messageID string, metadata map[string]any, target RunNotificationTarget) {
	HandleInboundInteract(ctx, interactable, b.bus, b.sessions, b.send, b.status, b.cfg.ChannelName, sessionKey, content, contentBlocks, images, messageID, metadata, target)
}

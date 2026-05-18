package channels

import (
	"context"
	"fmt"
	"strings"

	"github.com/fulcrus/hopclaw/agent"
	"github.com/fulcrus/hopclaw/internal/meta"
	runtimesvc "github.com/fulcrus/hopclaw/runtime"
)

func persistSessionAutoApproveOnAlwaysReply(ctx context.Context, sessions agent.SessionStore, channelName string, metadata map[string]any, content string, result *runtimesvc.InteractionResult) {
	if sessions == nil || result == nil || !result.ApprovalResolved {
		return
	}
	action, _, ok := ParseApprovalReplySignal(metadata, content)
	if !ok || action != ApprovalReplyAlways {
		return
	}
	sessionID := strings.TrimSpace(result.Context.SessionID)
	if sessionID == "" && result.Run != nil {
		sessionID = strings.TrimSpace(result.Run.SessionID)
	}
	if sessionID == "" {
		return
	}
	if err := EnableSessionAutoApproveSession(ctx, sessions, sessionID); err != nil {
		log.Error("bridge: persist session auto-approve", "channel", channelName, "error", err, "session_id", sessionID)
	}
}

func inboundScopeValues(metadata map[string]any) string {
	return strings.TrimSpace(firstAnyString(metadata, "automation_id"))
}

// ApplyInboundScopeMetadata enriches inbound metadata with automation context
// so specialized bridges can match the shared bridge contract before calling
// HandleInboundInteract.
func ApplyInboundScopeMetadata(_ any, metadata map[string]any, raw map[string]any) {
	if metadata == nil {
		return
	}
	if raw != nil {
		if automationID := strings.TrimSpace(firstAnyString(raw, "automation_id")); automationID != "" && strings.TrimSpace(firstAnyString(metadata, "automation_id")) == "" {
			metadata["automation_id"] = automationID
		}
	}
	automationID := inboundScopeValues(metadata)
	if automationID != "" && strings.TrimSpace(firstAnyString(metadata, "automation_id")) == "" {
		metadata["automation_id"] = automationID
	}
}

func (b *Bridge) threadIDForMessage(msg InboundMessage) string {
	if b == nil {
		return ""
	}
	if b.cfg.ThreadIDKey != "" {
		if threadID, _ := msg.RawEvent[b.cfg.ThreadIDKey].(string); strings.TrimSpace(threadID) != "" {
			return strings.TrimSpace(threadID)
		}
	}
	return firstAnyString(msg.RawEvent, "thread_id", "thread_ts", "root_id", "topic_id", "message_thread_id")
}

func (b *Bridge) replyTargetForMessage(messageID, threadID string) string {
	if b != nil && b.policy.ReplyInThread && strings.TrimSpace(threadID) != "" {
		return strings.TrimSpace(threadID)
	}
	return strings.TrimSpace(messageID)
}

func normalizeConversationMetadata(metadata map[string]any, raw map[string]any) {
	if metadata == nil {
		return
	}
	if replyTo := firstAnyString(metadata, meta.KeyReplyToID, "reply_to_message_id", "reply_to_id"); replyTo != "" {
		metadata[meta.KeyReplyToID] = replyTo
	}
	threadID := firstAnyString(metadata, meta.KeyThreadID, "thread_id", "thread_ts", "thread_name", "topic_id", "message_thread_id", "root_id")
	if threadID == "" && raw != nil {
		threadID = firstAnyString(raw, "thread_id", "thread_ts", "thread_name", "topic_id", "message_thread_id", "root_id")
	}
	if threadID != "" {
		metadata[meta.KeyThreadID] = threadID
	}
	if senderName := firstAnyString(metadata, meta.KeySenderName, "sender_name", "display_name"); senderName != "" {
		metadata[meta.KeySenderName] = senderName
	}
	if chatType := InferPolicyChatType(metadata); chatType != "" {
		metadata[meta.KeyChatType] = chatType
		return
	}
	if chatType := InferPolicyChatType(raw); chatType != "" {
		metadata[meta.KeyChatType] = chatType
	}
}

func InferPolicyChatType(raw map[string]any) string {
	if len(raw) == 0 {
		return ""
	}
	if chatType := meta.NormalizeChatType(firstAnyString(raw, meta.KeyChatType)); chatType != meta.ChatTypeUnknown {
		return chatType.String()
	}
	switch strings.ToLower(strings.TrimSpace(firstAnyString(raw, "source_type"))) {
	case "user":
		return meta.ChatTypeDirect.String()
	case "group", "room":
		return meta.ChatTypeGroup.String()
	}
	switch strings.ToLower(strings.TrimSpace(firstAnyString(raw, "conversation_type"))) {
	case "personal":
		return meta.ChatTypeDirect.String()
	case "channel", "groupchat":
		return meta.ChatTypeGroup.String()
	}
	switch strings.ToUpper(strings.TrimSpace(firstAnyString(raw, "channel_type"))) {
	case "D":
		return meta.ChatTypeDirect.String()
	case "O", "P", "G":
		return meta.ChatTypeGroup.String()
	}
	switch strings.ToLower(strings.TrimSpace(firstAnyString(raw, "space_type"))) {
	case "dm", "direct", "direct_message":
		return meta.ChatTypeDirect.String()
	case "space", "room":
		return meta.ChatTypeGroup.String()
	}
	if _, ok := raw["guild_id"]; ok {
		if strings.TrimSpace(firstAnyString(raw, "guild_id")) == "" {
			return meta.ChatTypeDirect.String()
		}
		return meta.ChatTypeGroup.String()
	}
	if firstAnyString(raw, "group_id", "room_id") != "" {
		return meta.ChatTypeGroup.String()
	}
	channel := strings.TrimSpace(firstAnyString(raw, "channel"))
	switch {
	case strings.HasPrefix(channel, "D"):
		return meta.ChatTypeDirect.String()
	case channel != "":
		return meta.ChatTypeGroup.String()
	}
	target := strings.TrimSpace(firstAnyString(raw, "target"))
	switch {
	case strings.HasPrefix(target, "#"):
		return meta.ChatTypeGroup.String()
	case target != "":
		return meta.ChatTypeDirect.String()
	}
	return ""
}

func inferChatType(_ string, raw map[string]any) string {
	return InferPolicyChatType(raw)
}

func inferMentioned(content string, raw map[string]any) bool {
	for _, key := range []string{"mentioned", "bot_mentioned", "is_mentioned"} {
		if value, ok := raw[key]; ok {
			switch current := value.(type) {
			case bool:
				return current
			case string:
				trimmed := strings.ToLower(strings.TrimSpace(current))
				return trimmed == "true" || trimmed == "1" || trimmed == "yes"
			}
		}
	}
	return false
}

// NormalizeConversationMetadata applies shared reply/thread/sender normalization
// so bridge implementations feed comparable context into Interact.
func NormalizeConversationMetadata(metadata map[string]any, raw map[string]any) {
	normalizeConversationMetadata(metadata, raw)
}

func interactionReplyActStatusKind(replyAct runtimesvc.ReplyAct) meta.StatusKind {
	switch replyAct {
	case runtimesvc.ReplyActChatReply:
		return meta.StatusKindChatReply
	case runtimesvc.ReplyActClarificationPrompt:
		return meta.StatusKindClarificationPrompt
	case runtimesvc.ReplyActResumeAck:
		return meta.StatusKindResumeAck
	case runtimesvc.ReplyActTaskFailure:
		return meta.StatusKindTaskFailure
	default:
		return meta.StatusKindUnknown
	}
}

func defaultString(v, fallback string) string {
	if v != "" {
		return v
	}
	return fallback
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

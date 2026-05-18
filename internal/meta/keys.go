// Package meta defines common metadata key constants used across the codebase.
package meta

// ---------------------------------------------------------------------------
// Core identifiers
// ---------------------------------------------------------------------------

const (
	KeyRunID       = "run_id"
	KeySessionID   = "session_id"
	KeyChannel     = "channel"
	KeyChannelName = "_channel_name"
)

// ---------------------------------------------------------------------------
// Message identifiers
// ---------------------------------------------------------------------------

const (
	KeyMessageID  = "message_id"
	KeyReplyToID  = "reply_to_id"
	KeySenderID   = "sender_id"
	KeySenderName = "sender_name"
	KeyThreadID   = "thread_id"
)

// ---------------------------------------------------------------------------
// Status and state
// ---------------------------------------------------------------------------

const (
	KeyStatusKind = "status_kind"
	KeyApprovalID = "approval_id"
)

// ---------------------------------------------------------------------------
// Interaction metadata
// ---------------------------------------------------------------------------

const (
	KeyInteractionTurnID   = "interaction_turn_id"
	KeyInteractionEnvelope = "interaction_envelope"
	KeyChatType            = "chat_type"
)

// ---------------------------------------------------------------------------
// Channel capability metadata
// ---------------------------------------------------------------------------

const (
	KeyChannelCapabilities   = "channel_capabilities"
	KeyChannelInteractive    = "channel_interactive"
	KeyChannelThreading      = "channel_threading"
	KeyChannelMobile         = "channel_mobile"
	KeyChannelInlineDelivery = "channel_inline_delivery"
)

// ---------------------------------------------------------------------------
// Tool execution
// ---------------------------------------------------------------------------

const (
	KeyToolName   = "tool_name"
	KeyToolCallID = "tool_call_id"
)

// ---------------------------------------------------------------------------
// Channel-specific (for external API compatibility)
// ---------------------------------------------------------------------------

const (
	// Slack
	KeySlackChannel = "slack_channel"
	KeySlackTS      = "ts"

	// Telegram
	KeyChatID = "chat_id"

	// Discord
	KeyChannelID = "channel_id"

	// Feishu
	KeyReceiveIDType = "receive_id_type"

	// Webhook
	KeyWebhookID = "webhook_id"
)

// ---------------------------------------------------------------------------
// Ingress metadata
// ---------------------------------------------------------------------------

const (
	KeyIngressKind = "ingress_kind"
)

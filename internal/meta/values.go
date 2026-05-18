package meta

import "strings"

// ChatType is the normalized cross-runtime conversation shape carried in
// metadata. Adapter-specific raw event values stay at the adapter boundary.
type ChatType string

const (
	ChatTypeUnknown ChatType = ""
	ChatTypeDirect  ChatType = "direct"
	ChatTypeGroup   ChatType = "group"
)

func (t ChatType) String() string {
	return string(t)
}

func NormalizeChatType(value string) ChatType {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case string(ChatTypeDirect), "dm", "direct_message", "private", "private_chat", "p2p", "user", "personal":
		return ChatTypeDirect
	case string(ChatTypeGroup), "groupchat", "room", "space", "channel":
		return ChatTypeGroup
	default:
		return ChatTypeUnknown
	}
}

// StatusKind is the normalized metadata protocol for user-visible interaction
// and delivery states emitted across bridge/runtime/CLI boundaries.
type StatusKind string

const (
	StatusKindUnknown                   StatusKind = ""
	StatusKindChatReply                 StatusKind = "chat_reply"
	StatusKindClarificationPrompt       StatusKind = "clarification_prompt"
	StatusKindResumeAck                 StatusKind = "resume_ack"
	StatusKindTaskFailure               StatusKind = "task_failure"
	StatusKindSubmitFailed              StatusKind = "submit_failed"
	StatusKindApprovalReplyApproved     StatusKind = "approval_reply_approved"
	StatusKindApprovalReplyDenied       StatusKind = "approval_reply_denied"
	StatusKindApprovalReplyMissing      StatusKind = "approval_reply_missing"
	StatusKindApprovalWaiting           StatusKind = "approval_waiting"
	StatusKindSteerAccepted             StatusKind = "steer_accepted"
	StatusKindControlCommand            StatusKind = "control_command"
	StatusKindControlStatus             StatusKind = "control_status"
	StatusKindControlCancel             StatusKind = "control_cancel"
	StatusKindControlBind               StatusKind = "control_bind"
	StatusKindSmalltalkDuringTask       StatusKind = "smalltalk_during_task"
	StatusKindBackendAuthUnavailable    StatusKind = "backend_auth_unavailable"
	StatusKindCancelled                 StatusKind = "cancelled"
	StatusKindProcessing                StatusKind = "processing"
	StatusKindCompleted                 StatusKind = "completed"
	StatusKindPartial                   StatusKind = "partial"
	StatusKindFailed                    StatusKind = "failed"
	StatusKindVerificationFailed        StatusKind = "verification_failed"
	StatusKindVerificationRepairStarted StatusKind = "verification_repair_started"
	StatusKindPairingRequired           StatusKind = "pairing_required"
	StatusKindPairingStatus             StatusKind = "pairing_status"
	StatusKindPairingVerifyFailed       StatusKind = "pairing_verify_failed"
	StatusKindPairingVerified           StatusKind = "pairing_verified"
	StatusKindPairingRevoke             StatusKind = "pairing_revoke"
)

const StatusKindPreflightPrefix = "preflight_"

func (k StatusKind) String() string {
	return string(k)
}

func NormalizeStatusKind(value string) StatusKind {
	normalized := StatusKind(strings.ToLower(strings.TrimSpace(value)))
	switch normalized {
	case StatusKindChatReply,
		StatusKindClarificationPrompt,
		StatusKindResumeAck,
		StatusKindTaskFailure,
		StatusKindSubmitFailed,
		StatusKindApprovalReplyApproved,
		StatusKindApprovalReplyDenied,
		StatusKindApprovalReplyMissing,
		StatusKindApprovalWaiting,
		StatusKindSteerAccepted,
		StatusKindControlCommand,
		StatusKindControlStatus,
		StatusKindControlCancel,
		StatusKindControlBind,
		StatusKindSmalltalkDuringTask,
		StatusKindBackendAuthUnavailable,
		StatusKindCancelled,
		StatusKindProcessing,
		StatusKindCompleted,
		StatusKindPartial,
		StatusKindFailed,
		StatusKindVerificationFailed,
		StatusKindVerificationRepairStarted,
		StatusKindPairingRequired,
		StatusKindPairingStatus,
		StatusKindPairingVerifyFailed,
		StatusKindPairingVerified,
		StatusKindPairingRevoke:
		return normalized
	default:
		if strings.HasPrefix(string(normalized), StatusKindPreflightPrefix) {
			return normalized
		}
		return StatusKindUnknown
	}
}

func PreflightStatusKind(state string) StatusKind {
	state = strings.ToLower(strings.TrimSpace(state))
	if state == "" {
		return StatusKindUnknown
	}
	return StatusKind(StatusKindPreflightPrefix + state)
}

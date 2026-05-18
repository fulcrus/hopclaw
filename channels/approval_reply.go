package channels

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/fulcrus/hopclaw/agent"
	"github.com/fulcrus/hopclaw/approval"
)

const sessionMetaAutoApproveSession = "channel.auto_approve_session"

var ErrPendingApprovalNotFound = errors.New("pending approval not found")

type ApprovalRuntime interface {
	GetRun(ctx context.Context, id string) (*agent.Run, error)
	GetApproval(ctx context.Context, id string) (*approval.Ticket, error)
	FindPendingApproval(ctx context.Context, sessionID string) (*approval.Ticket, error)
	ResolveApproval(ctx context.Context, id string, resolution approval.Resolution) (*approval.Ticket, error)
}

type ApprovalReplyAction string

const (
	ApprovalReplyApprove ApprovalReplyAction = "approve"
	ApprovalReplyDeny    ApprovalReplyAction = "deny"
	ApprovalReplyAlways  ApprovalReplyAction = "always"
)

type ApprovalReplySource string

const (
	ApprovalReplySourceStructured     ApprovalReplySource = "structured"
	ApprovalReplySourceNumbered       ApprovalReplySource = "numbered"
	ApprovalReplySourceDeprecatedText ApprovalReplySource = "deprecated_text"
)

// ParseApprovalReplySelection matches the numbered approval contract used on
// text-only channels: 1=approve, 2=deny, 3=always.
func ParseApprovalReplySelection(input string) (ApprovalReplyAction, bool) {
	switch strings.TrimSpace(input) {
	case "1":
		return ApprovalReplyApprove, true
	case "2":
		return ApprovalReplyDeny, true
	case "3":
		return ApprovalReplyAlways, true
	default:
		return "", false
	}
}

// ParseApprovalReply is a deprecated fallback for legacy natural-language
// approval replies. The primary path is structured callback payloads or
// numbered replies.
func ParseApprovalReply(input string) (ApprovalReplyAction, bool) {
	switch strings.ToLower(strings.TrimSpace(input)) {
	case "y", "yes":
		return ApprovalReplyApprove, true
	case "n", "no":
		return ApprovalReplyDeny, true
	case "a", "always":
		return ApprovalReplyAlways, true
	default:
		return "", false
	}
}

// ParseApprovalReplySignal resolves approval replies from machine-format
// callback data first, then numbered replies, and only then the deprecated
// natural-language fallback.
func ParseApprovalReplySignal(rawEvent map[string]any, input string) (ApprovalReplyAction, ApprovalReplySource, bool) {
	if action, ok := parseStructuredApprovalReply(rawEvent); ok {
		return action, ApprovalReplySourceStructured, true
	}
	if action, ok := ParseApprovalReplySelection(input); ok {
		return action, ApprovalReplySourceNumbered, true
	}
	if action, ok := ParseApprovalReply(input); ok {
		return action, ApprovalReplySourceDeprecatedText, true
	}
	return "", "", false
}

func HasStructuredApprovalReply(rawEvent map[string]any) bool {
	_, ok := parseStructuredApprovalReply(rawEvent)
	return ok
}

func parseStructuredApprovalReply(rawEvent map[string]any) (ApprovalReplyAction, bool) {
	if len(rawEvent) == 0 {
		return "", false
	}
	for _, key := range []string{
		"approval_action",
		"interaction_action",
		"callback_action",
		"action_id",
		"button_id",
		"approval_reply",
		"approval_choice",
	} {
		if action, ok := approvalReplyActionFromValue(rawEvent[key]); ok {
			return action, true
		}
	}
	for _, key := range []string{"action", "callback", "interaction"} {
		nested, ok := rawEvent[key].(map[string]any)
		if !ok {
			continue
		}
		if action, ok := parseStructuredApprovalReply(nested); ok {
			return action, true
		}
	}
	return "", false
}

func approvalReplyActionFromValue(value any) (ApprovalReplyAction, bool) {
	token := strings.ToLower(strings.TrimSpace(fmt.Sprint(value)))
	switch token {
	case "", "<nil>":
		return "", false
	case "1", "approve", "approval:approve", "approval.approve", "approval_approve", "approval-approve":
		return ApprovalReplyApprove, true
	case "2", "deny", "approval:deny", "approval.deny", "approval_deny", "approval-deny":
		return ApprovalReplyDeny, true
	case "3", "always", "approval:always", "approval.always", "approval_always", "approval-always":
		return ApprovalReplyAlways, true
	default:
		return "", false
	}
}

func SessionAutoApproveSession(session *agent.Session) bool {
	if session == nil || session.Metadata == nil {
		return false
	}
	switch value := session.Metadata[sessionMetaAutoApproveSession].(type) {
	case bool:
		return value
	case string:
		return strings.EqualFold(strings.TrimSpace(value), "true")
	default:
		return false
	}
}

func EnableSessionAutoApproveSession(ctx context.Context, sessions agent.SessionStore, sessionID string) error {
	if sessions == nil {
		return fmt.Errorf("session store is required")
	}
	session, unlock, err := sessions.LoadForExecution(ctx, sessionID)
	if err != nil {
		return err
	}
	defer unlock()
	if session.Metadata == nil {
		session.Metadata = make(map[string]any, 1)
	}
	session.Metadata[sessionMetaAutoApproveSession] = true
	return sessions.Save(ctx, session)
}

func FindPendingApprovalBySessionKey(ctx context.Context, runtime ApprovalRuntime, sessions agent.SessionStore, sessionKey string) (*agent.Session, *approval.Ticket, error) {
	if runtime == nil {
		return nil, nil, fmt.Errorf("approval runtime is required")
	}
	session, err := agent.LoadSessionByKey(ctx, sessions, sessionKey, agent.ScopeFilter{})
	if errors.Is(err, agent.ErrSessionKeyLookupUnsupported) {
		return nil, nil, fmt.Errorf("session store does not support key lookup")
	}
	if err != nil {
		return nil, nil, ErrPendingApprovalNotFound
	}
	ticket, err := runtime.FindPendingApproval(ctx, session.ID)
	if err != nil {
		return session, nil, ErrPendingApprovalNotFound
	}
	return session, ticket, nil
}

func ApprovalLanguageHint(session *agent.Session) string {
	if session == nil {
		return ""
	}
	for i := len(session.Messages) - 1; i >= 0; i-- {
		msg := session.Messages[i]
		if msg.Role != "user" {
			continue
		}
		content := strings.TrimSpace(msg.Content)
		if content == "" {
			continue
		}
		if _, ok := ParseApprovalReplySelection(content); ok {
			continue
		}
		if _, ok := ParseApprovalReply(content); ok {
			continue
		}
		return content
	}
	return ""
}

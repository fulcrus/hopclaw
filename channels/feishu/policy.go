package feishu

import "strings"

type inboundPolicyDecision struct {
	allow         bool
	notify        string
	groupScoped   bool
	sessionScope  string
	replyInThread bool
}

func evaluateInboundPolicy(account ResolvedAccount, msg inboundEnvelope) inboundPolicyDecision {
	msg = normalizeInboundEnvelope(msg)
	decision := inboundPolicyDecision{
		allow:         true,
		groupScoped:   msg.ChatType != "p2p",
		sessionScope:  account.GroupSessionScope,
		replyInThread: account.ReplyInThread,
	}
	if msg.ChatType == "p2p" {
		switch account.DMPolicy {
		case "allowlist":
			decision.allow = senderAllowed(account.AllowFrom, msg.SenderID)
		case "pairing":
			decision.allow = senderAllowed(account.AllowFrom, msg.SenderID)
			if !decision.allow {
				decision.notify = "This Feishu direct message is gated by pairing policy."
			}
		}
		return decision
	}

	switch account.GroupPolicy {
	case "disabled":
		decision.allow = false
		return decision
	case "allowlist":
		decision.allow = senderAllowed(account.GroupAllowFrom, msg.SenderID)
		if !decision.allow {
			return decision
		}
	}
	if account.RequireMention && !msg.Mentioned {
		decision.allow = false
	}
	return decision
}

func senderAllowed(allowlist []string, senderID string) bool {
	if len(allowlist) == 0 {
		return false
	}
	normalizedSender := normalizeAllowEntry(senderID)
	for _, entry := range allowlist {
		switch normalizeAllowEntry(entry) {
		case "*":
			return true
		case normalizedSender:
			return true
		}
	}
	return false
}

func normalizeAllowEntry(value string) string {
	trimmed := strings.TrimSpace(strings.ToLower(value))
	trimmed = strings.TrimPrefix(trimmed, "feishu:")
	trimmed = strings.TrimPrefix(trimmed, "user:")
	trimmed = strings.TrimPrefix(trimmed, "open_id:")
	return trimmed
}

type inboundEnvelope struct {
	AccountID string
	MessageID string
	SenderID  string
	ChatID    string
	ChatType  string
	ThreadID  string
	RootID    string
	ParentID  string
	Mentioned bool
}

func sessionKeyForInbound(msg inboundEnvelope, account ResolvedAccount) string {
	msg = normalizeInboundEnvelope(msg)
	prefix := "feishu"
	if account.ID != "" && account.ID != "default" {
		prefix = prefix + ":" + account.ID
	}
	if msg.ChatType == "p2p" {
		return prefix + ":user:" + msg.SenderID
	}
	switch account.GroupSessionScope {
	case "group_sender":
		return prefix + ":" + msg.ChatID + ":sender:" + msg.SenderID
	case "group_thread":
		if msg.ThreadID != "" {
			return prefix + ":thread:" + msg.ThreadID
		}
	case "group_thread_sender":
		if msg.ThreadID != "" {
			return prefix + ":thread:" + msg.ThreadID + ":sender:" + msg.SenderID
		}
		return prefix + ":" + msg.ChatID + ":sender:" + msg.SenderID
	}
	if account.ID == "" || account.ID == "default" {
		return "feishu:" + msg.ChatID
	}
	return prefix + ":" + msg.ChatID
}

func normalizeInboundEnvelope(msg inboundEnvelope) inboundEnvelope {
	msg.AccountID = strings.TrimSpace(msg.AccountID)
	msg.MessageID = strings.TrimSpace(msg.MessageID)
	msg.SenderID = strings.TrimSpace(msg.SenderID)
	msg.ChatID = strings.TrimSpace(msg.ChatID)
	msg.ChatType = strings.TrimSpace(msg.ChatType)
	msg.ThreadID = strings.TrimSpace(msg.ThreadID)
	msg.RootID = strings.TrimSpace(msg.RootID)
	msg.ParentID = strings.TrimSpace(msg.ParentID)
	return msg
}

type sessionTarget struct {
	accountID     string
	targetID      string
	replyToID     string
	receiveIDType string
	replyInThread bool
}

func parseSessionTarget(sessionKey string) sessionTarget {
	parts := strings.Split(sessionKey, ":")
	if len(parts) < 2 || parts[0] != "feishu" {
		return sessionTarget{}
	}
	target := sessionTarget{receiveIDType: "chat_id"}
	idx := 1
	if len(parts) >= 4 && parts[2] == "user" {
		target.accountID = parts[1]
		target.targetID = parts[3]
		target.receiveIDType = "open_id"
		return target
	}
	if len(parts) >= 3 && parts[1] == "user" {
		target.targetID = parts[2]
		target.receiveIDType = "open_id"
		return target
	}
	if len(parts) >= 4 && parts[2] == "thread" {
		target.accountID = parts[1]
		target.targetID = parts[3]
		target.receiveIDType = "thread_id"
		target.replyInThread = true
		return target
	}
	if len(parts) >= 3 && parts[1] == "thread" {
		target.targetID = parts[2]
		target.receiveIDType = "thread_id"
		target.replyInThread = true
		return target
	}
	if len(parts) >= 3 && parts[2] != "sender" {
		target.accountID = parts[1]
		target.targetID = parts[2]
		return target
	}
	if len(parts) >= 2 {
		target.targetID = parts[idx]
	}
	return target
}

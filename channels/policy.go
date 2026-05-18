package channels

import (
	"fmt"
	"strings"
	"time"
)

type PolicyConfig struct {
	DMPolicy          string
	AllowFrom         []string
	GroupPolicy       string
	GroupAllowFrom    []string
	RequireMention    bool
	GroupSessionScope string
	ReplyInThread     bool
	DedupeTTL         time.Duration
	DedupeDir         string
}

type PolicyEnvelope struct {
	SenderID  string
	ChatID    string
	ChatType  string
	ThreadID  string
	MessageID string
	Mentioned bool
}

type PolicyDecision struct {
	Allow  bool
	Notify string
}

type SessionKeyOptions struct {
	AccountID          string
	DirectPrefix       string
	UseChatIDForDirect bool
}

func NormalizePolicyConfig(cfg PolicyConfig) PolicyConfig {
	cfg.DMPolicy = normalizePolicyValue(cfg.DMPolicy, "open", "allowlist", "pairing")
	cfg.GroupPolicy = normalizePolicyValue(cfg.GroupPolicy, "open", "allowlist", "disabled")
	cfg.GroupSessionScope = normalizePolicyValue(cfg.GroupSessionScope, "group", "group_sender", "group_thread", "group_thread_sender")
	if cfg.DedupeTTL <= 0 {
		cfg.DedupeTTL = 24 * time.Hour
	}
	return cfg
}

func ValidatePolicyConfig(path string, cfg PolicyConfig) error {
	cfg = NormalizePolicyConfig(cfg)
	if err := validatePolicyValue(path+".dm_policy", cfg.DMPolicy, "open", "allowlist", "pairing"); err != nil {
		return err
	}
	if err := validatePolicyValue(path+".group_policy", cfg.GroupPolicy, "open", "allowlist", "disabled"); err != nil {
		return err
	}
	if err := validatePolicyValue(path+".group_session_scope", cfg.GroupSessionScope, "group", "group_sender", "group_thread", "group_thread_sender"); err != nil {
		return err
	}
	return nil
}

func EvaluatePolicy(cfg PolicyConfig, env PolicyEnvelope) PolicyDecision {
	cfg = NormalizePolicyConfig(cfg)
	decision := PolicyDecision{Allow: true}
	if isDirectChat(env.ChatType) {
		switch cfg.DMPolicy {
		case "allowlist":
			decision.Allow = SenderAllowed(cfg.AllowFrom, env.SenderID)
		case "pairing":
			decision.Allow = SenderAllowed(cfg.AllowFrom, env.SenderID)
			if !decision.Allow {
				decision.Notify = "This direct message is gated by pairing policy."
			}
		}
		return decision
	}

	switch cfg.GroupPolicy {
	case "disabled":
		decision.Allow = false
		return decision
	case "allowlist":
		decision.Allow = SenderAllowed(cfg.GroupAllowFrom, env.SenderID)
		if !decision.Allow {
			return decision
		}
	}
	if cfg.RequireMention && !env.Mentioned {
		decision.Allow = false
	}
	return decision
}

func SessionKeyForEnvelope(channel string, cfg PolicyConfig, env PolicyEnvelope, opts SessionKeyOptions) string {
	cfg = NormalizePolicyConfig(cfg)
	channel = strings.TrimSpace(channel)
	if channel == "" {
		channel = "channel"
	}
	directPrefix := strings.TrimSpace(opts.DirectPrefix)
	if directPrefix == "" {
		directPrefix = "user"
	}
	prefix := channel
	if account := strings.TrimSpace(opts.AccountID); account != "" && account != "default" {
		prefix += ":" + account
	}

	if isDirectChat(env.ChatType) {
		if opts.UseChatIDForDirect {
			return prefix + ":" + strings.TrimSpace(env.ChatID)
		}
		return prefix + ":" + directPrefix + ":" + strings.TrimSpace(env.SenderID)
	}

	switch cfg.GroupSessionScope {
	case "group_sender":
		return fmt.Sprintf("%s:%s:sender:%s", prefix, strings.TrimSpace(env.ChatID), strings.TrimSpace(env.SenderID))
	case "group_thread":
		if strings.TrimSpace(env.ThreadID) != "" {
			return fmt.Sprintf("%s:thread:%s", prefix, strings.TrimSpace(env.ThreadID))
		}
	case "group_thread_sender":
		if strings.TrimSpace(env.ThreadID) != "" {
			return fmt.Sprintf("%s:thread:%s:sender:%s", prefix, strings.TrimSpace(env.ThreadID), strings.TrimSpace(env.SenderID))
		}
		return fmt.Sprintf("%s:%s:sender:%s", prefix, strings.TrimSpace(env.ChatID), strings.TrimSpace(env.SenderID))
	}
	return prefix + ":" + strings.TrimSpace(env.ChatID)
}

func SenderAllowed(allowlist []string, senderID string) bool {
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

func isDirectChat(chatType string) bool {
	value := strings.ToLower(strings.TrimSpace(chatType))
	return value == "" || value == "direct" || value == "dm" || value == "private" || value == "p2p" || value == "private_chat"
}

func normalizeAllowEntry(value string) string {
	trimmed := strings.TrimSpace(strings.ToLower(value))
	trimmed = strings.TrimPrefix(trimmed, "feishu:")
	trimmed = strings.TrimPrefix(trimmed, "slack:")
	trimmed = strings.TrimPrefix(trimmed, "discord:")
	trimmed = strings.TrimPrefix(trimmed, "telegram:")
	trimmed = strings.TrimPrefix(trimmed, "user:")
	trimmed = strings.TrimPrefix(trimmed, "open_id:")
	return trimmed
}

func normalizePolicyValue(value string, defaultValue string, allowed ...string) string {
	current := strings.ToLower(strings.TrimSpace(value))
	for _, item := range allowed {
		if current == item {
			return current
		}
	}
	return defaultValue
}

func validatePolicyValue(path, value string, allowed ...string) error {
	current := strings.TrimSpace(value)
	for _, item := range allowed {
		if current == item {
			return nil
		}
	}
	return fmt.Errorf("%s must be one of %s", path, strings.Join(allowed, ", "))
}

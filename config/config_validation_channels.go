package config

import (
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/fulcrus/hopclaw/internal/support/normalize"
	"github.com/fulcrus/hopclaw/logging"
)

var validationLog = logging.WithSubsystem("config")

func applyCommonChannelDefaults(cfg *CommonChannelConfig) {
	if cfg == nil {
		return
	}
	cfg.DMPolicy = normalizeCommonChannelPolicy(cfg.DMPolicy, "open")
	cfg.GroupPolicy = normalizeCommonChannelPolicy(cfg.GroupPolicy, "open")
	cfg.GroupSessionScope = normalizeCommonGroupSessionScope(cfg.GroupSessionScope)
	cfg.ReplyInThread = normalizeEnabledDisabled(cfg.ReplyInThread, "disabled")
	if cfg.DedupeTTL <= 0 {
		cfg.DedupeTTL = 24 * time.Hour
	}
}

func validateCommonChannelConfig(path string, cfg CommonChannelConfig) error {
	if err := validateOneOf(path+".dm_policy", cfg.DMPolicy, "open", "allowlist", "pairing"); err != nil {
		return err
	}
	if err := validateOneOf(path+".group_policy", cfg.GroupPolicy, "open", "allowlist", "disabled"); err != nil {
		return err
	}
	if err := validateOneOf(path+".group_session_scope", cfg.GroupSessionScope, "group", "group_sender", "group_thread", "group_thread_sender"); err != nil {
		return err
	}
	if err := validateOneOf(path+".reply_in_thread", cfg.ReplyInThread, "enabled", "disabled"); err != nil {
		return err
	}
	return nil
}

func normalizeCommonChannelPolicy(value string, defaultValue string) string {
	trimmed := strings.ToLower(strings.TrimSpace(value))
	switch trimmed {
	case "allowlist", "pairing", "disabled":
		return trimmed
	case "open", "":
		return "open"
	default:
		validationLog.Warn("unknown channel policy, using default",
			"value", value, "default", defaultValue,
			"allowed", "open, allowlist, pairing, disabled")
		return defaultValue
	}
}

func normalizeCommonGroupSessionScope(value string) string {
	trimmed := strings.ToLower(strings.TrimSpace(value))
	switch trimmed {
	case "group_sender", "group_thread", "group_thread_sender":
		return trimmed
	case "group", "":
		return "group"
	default:
		validationLog.Warn("unknown group_session_scope, using default",
			"value", value, "default", "group",
			"allowed", "group, group_sender, group_thread, group_thread_sender")
		return "group"
	}
}

func applyFeishuChannelDefaults(cfg *FeishuChannelConfig) {
	if cfg == nil {
		return
	}
	cfg.Domain = normalizeFeishuDomain(cfg.Domain)
	cfg.ConnectionMode = normalizeFeishuConnectionMode(cfg.ConnectionMode)
	cfg.DMPolicy = normalizeFeishuDMPolicy(cfg.DMPolicy)
	cfg.GroupPolicy = normalizeFeishuGroupPolicy(cfg.GroupPolicy)
	cfg.GroupSessionScope = normalizeFeishuGroupSessionScope(cfg.GroupSessionScope)
	cfg.ReplyInThread = normalizeEnabledDisabled(cfg.ReplyInThread, "disabled")
	if cfg.DedupeTTL <= 0 {
		cfg.DedupeTTL = 24 * time.Hour
	}
	for name, account := range cfg.Accounts {
		account.Domain = normalizeFeishuDomain(normalize.FirstNonEmpty(account.Domain, cfg.Domain))
		account.ConnectionMode = normalizeFeishuConnectionMode(normalize.FirstNonEmpty(account.ConnectionMode, cfg.ConnectionMode))
		account.DMPolicy = normalizeFeishuDMPolicy(normalize.FirstNonEmpty(account.DMPolicy, cfg.DMPolicy))
		account.GroupPolicy = normalizeFeishuGroupPolicy(normalize.FirstNonEmpty(account.GroupPolicy, cfg.GroupPolicy))
		account.GroupSessionScope = normalizeFeishuGroupSessionScope(normalize.FirstNonEmpty(account.GroupSessionScope, cfg.GroupSessionScope))
		account.ReplyInThread = normalizeEnabledDisabled(normalize.FirstNonEmpty(account.ReplyInThread, cfg.ReplyInThread), "disabled")
		cfg.Accounts[name] = account
	}
}

func validateFeishuChannelConfig(cfg FeishuChannelConfig) error {
	if err := validateOneOf("channels.feishu.connection_mode", cfg.ConnectionMode, "websocket", "webhook"); err != nil {
		return err
	}
	if err := validateOneOf("channels.feishu.dm_policy", cfg.DMPolicy, "open", "allowlist", "pairing"); err != nil {
		return err
	}
	if err := validateOneOf("channels.feishu.group_policy", cfg.GroupPolicy, "open", "allowlist", "disabled"); err != nil {
		return err
	}
	if err := validateOneOf("channels.feishu.group_session_scope", cfg.GroupSessionScope, "group", "group_sender", "group_thread", "group_thread_sender"); err != nil {
		return err
	}
	if err := validateOneOf("channels.feishu.reply_in_thread", cfg.ReplyInThread, "enabled", "disabled"); err != nil {
		return err
	}
	if domain := strings.TrimSpace(cfg.Domain); domain != "" && !validFeishuDomain(domain) {
		return fmt.Errorf("channels.feishu.domain must be feishu, lark, or an https URL")
	}
	for name, account := range cfg.Accounts {
		pathPrefix := fmt.Sprintf("channels.feishu.accounts[%s]", name)
		if err := validateOneOf(pathPrefix+".connection_mode", normalizeFeishuConnectionMode(account.ConnectionMode), "websocket", "webhook"); err != nil {
			return err
		}
		if err := validateOneOf(pathPrefix+".dm_policy", normalizeFeishuDMPolicy(normalize.FirstNonEmpty(account.DMPolicy, cfg.DMPolicy)), "open", "allowlist", "pairing"); err != nil {
			return err
		}
		if err := validateOneOf(pathPrefix+".group_policy", normalizeFeishuGroupPolicy(normalize.FirstNonEmpty(account.GroupPolicy, cfg.GroupPolicy)), "open", "allowlist", "disabled"); err != nil {
			return err
		}
		if err := validateOneOf(pathPrefix+".group_session_scope", normalizeFeishuGroupSessionScope(normalize.FirstNonEmpty(account.GroupSessionScope, cfg.GroupSessionScope)), "group", "group_sender", "group_thread", "group_thread_sender"); err != nil {
			return err
		}
		if err := validateOneOf(pathPrefix+".reply_in_thread", normalizeEnabledDisabled(normalize.FirstNonEmpty(account.ReplyInThread, cfg.ReplyInThread), "disabled"), "enabled", "disabled"); err != nil {
			return err
		}
		if domain := strings.TrimSpace(normalize.FirstNonEmpty(account.Domain, cfg.Domain)); domain != "" && !validFeishuDomain(domain) {
			return fmt.Errorf("%s.domain must be feishu, lark, or an https URL", pathPrefix)
		}
	}
	return nil
}

func validateOneOf(path, value string, allowed ...string) error {
	current := strings.TrimSpace(value)
	for _, item := range allowed {
		if current == item {
			return nil
		}
	}
	return fmt.Errorf("%s: got %q, must be one of: %s", path, current, strings.Join(allowed, ", "))
}

func normalizeFeishuDomain(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return "feishu"
	}
	lower := strings.ToLower(trimmed)
	switch lower {
	case "feishu", "lark":
		return lower
	default:
		return normalizeFeishuDomainURL(trimmed)
	}
}

func normalizeFeishuDomainURL(value string) string {
	trimmed := strings.TrimRight(strings.TrimSpace(value), "/")
	if trimmed == "" {
		return trimmed
	}
	parsed, err := url.Parse(trimmed)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return trimmed
	}
	if strings.TrimRight(parsed.Path, "/") == "/open-apis" {
		parsed.Path = ""
		parsed.RawPath = ""
	}
	return strings.TrimRight(parsed.String(), "/")
}

func normalizeFeishuConnectionMode(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "websocket":
		return "websocket"
	case "webhook":
		return "webhook"
	default:
		return strings.ToLower(strings.TrimSpace(value))
	}
}

func normalizeFeishuDMPolicy(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "open":
		return "open"
	case "allowlist", "pairing":
		return strings.ToLower(strings.TrimSpace(value))
	default:
		return strings.ToLower(strings.TrimSpace(value))
	}
}

func normalizeFeishuGroupPolicy(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "open":
		return "open"
	case "allowlist", "disabled":
		return strings.ToLower(strings.TrimSpace(value))
	default:
		return strings.ToLower(strings.TrimSpace(value))
	}
}

func normalizeFeishuGroupSessionScope(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "group":
		return "group"
	case "group_sender", "group_thread", "group_thread_sender":
		return strings.ToLower(strings.TrimSpace(value))
	default:
		return strings.ToLower(strings.TrimSpace(value))
	}
}

func normalizeEnabledDisabled(value string, fallback string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "":
		return fallback
	case "enabled", "disabled":
		return strings.ToLower(strings.TrimSpace(value))
	default:
		return strings.ToLower(strings.TrimSpace(value))
	}
}

func validFeishuDomain(value string) bool {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return true
	}
	if trimmed == "feishu" || trimmed == "lark" {
		return true
	}
	return strings.HasPrefix(trimmed, "https://")
}

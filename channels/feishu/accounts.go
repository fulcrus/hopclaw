package feishu

import (
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/fulcrus/hopclaw/internal/support/normalize"
)

type Config struct {
	Enabled           *bool                    `json:"enabled" yaml:"enabled"`
	DefaultAccount    string                   `json:"default_account,omitempty" yaml:"default_account"`
	AppID             string                   `json:"app_id" yaml:"app_id"`
	AppSecret         string                   `json:"app_secret" yaml:"app_secret"`
	EncryptKey        string                   `json:"encrypt_key,omitempty" yaml:"encrypt_key"`
	VerificationToken string                   `json:"verification_token,omitempty" yaml:"verification_token"`
	Domain            string                   `json:"domain,omitempty" yaml:"domain"`
	ConnectionMode    string                   `json:"connection_mode,omitempty" yaml:"connection_mode"`
	DMPolicy          string                   `json:"dm_policy,omitempty" yaml:"dm_policy"`
	AllowFrom         []string                 `json:"allow_from,omitempty" yaml:"allow_from"`
	GroupPolicy       string                   `json:"group_policy,omitempty" yaml:"group_policy"`
	GroupAllowFrom    []string                 `json:"group_allow_from,omitempty" yaml:"group_allow_from"`
	RequireMention    *bool                    `json:"require_mention,omitempty" yaml:"require_mention"`
	GroupSessionScope string                   `json:"group_session_scope,omitempty" yaml:"group_session_scope"`
	ReplyInThread     string                   `json:"reply_in_thread,omitempty" yaml:"reply_in_thread"`
	DedupeTTL         time.Duration            `json:"dedupe_ttl,omitempty" yaml:"dedupe_ttl"`
	DedupeDir         string                   `json:"dedupe_dir,omitempty" yaml:"dedupe_dir"`
	Accounts          map[string]AccountConfig `json:"accounts,omitempty" yaml:"accounts"`
}

type AccountConfig struct {
	Enabled           *bool    `json:"enabled,omitempty" yaml:"enabled"`
	Name              string   `json:"name,omitempty" yaml:"name"`
	AppID             string   `json:"app_id,omitempty" yaml:"app_id"`
	AppSecret         string   `json:"app_secret,omitempty" yaml:"app_secret"`
	EncryptKey        string   `json:"encrypt_key,omitempty" yaml:"encrypt_key"`
	VerificationToken string   `json:"verification_token,omitempty" yaml:"verification_token"`
	Domain            string   `json:"domain,omitempty" yaml:"domain"`
	ConnectionMode    string   `json:"connection_mode,omitempty" yaml:"connection_mode"`
	DMPolicy          string   `json:"dm_policy,omitempty" yaml:"dm_policy"`
	AllowFrom         []string `json:"allow_from,omitempty" yaml:"allow_from"`
	GroupPolicy       string   `json:"group_policy,omitempty" yaml:"group_policy"`
	GroupAllowFrom    []string `json:"group_allow_from,omitempty" yaml:"group_allow_from"`
	RequireMention    *bool    `json:"require_mention,omitempty" yaml:"require_mention"`
	GroupSessionScope string   `json:"group_session_scope,omitempty" yaml:"group_session_scope"`
	ReplyInThread     string   `json:"reply_in_thread,omitempty" yaml:"reply_in_thread"`
}

type accountState struct {
	id         string
	name       string
	config     AccountConfig
	domain     string
	client     *clientWrapper
	httpHandle httpHandler
}

type httpHandler func(req inboundHTTPRequest) (*inboundHTTPResponse, error)

type inboundHTTPRequest struct {
	method string
	header map[string][]string
	body   []byte
}

type inboundHTTPResponse struct {
	statusCode int
	headers    map[string]string
	body       []byte
}

type ResolvedAccount struct {
	ID                string
	Name              string
	Enabled           bool
	AppID             string
	AppSecret         string
	EncryptKey        string
	VerificationToken string
	Domain            string
	ConnectionMode    string
	DMPolicy          string
	AllowFrom         []string
	GroupPolicy       string
	GroupAllowFrom    []string
	RequireMention    bool
	GroupSessionScope string
	ReplyInThread     bool
}

func resolveAccounts(cfg Config) (string, []ResolvedAccount) {
	topLevel := ResolvedAccount{
		ID:                defaultAccountID(cfg.DefaultAccount),
		Enabled:           cfg.Enabled == nil || *cfg.Enabled,
		AppID:             strings.TrimSpace(cfg.AppID),
		AppSecret:         strings.TrimSpace(cfg.AppSecret),
		EncryptKey:        strings.TrimSpace(cfg.EncryptKey),
		VerificationToken: strings.TrimSpace(cfg.VerificationToken),
		Domain:            normalizeDomain(cfg.Domain),
		ConnectionMode:    normalizeConnectionMode(cfg.ConnectionMode),
		DMPolicy:          normalizeDMPolicy(cfg.DMPolicy),
		AllowFrom:         cloneStrings(cfg.AllowFrom),
		GroupPolicy:       normalizeGroupPolicy(cfg.GroupPolicy),
		GroupAllowFrom:    cloneStrings(cfg.GroupAllowFrom),
		RequireMention:    cfg.RequireMention != nil && *cfg.RequireMention,
		GroupSessionScope: normalizeGroupSessionScope(cfg.GroupSessionScope),
		ReplyInThread:     strings.EqualFold(strings.TrimSpace(cfg.ReplyInThread), "enabled"),
	}
	if topLevel.GroupPolicy == "open" && cfg.RequireMention == nil {
		topLevel.RequireMention = true
	}

	if len(cfg.Accounts) == 0 {
		return topLevel.ID, []ResolvedAccount{topLevel}
	}

	ids := make([]string, 0, len(cfg.Accounts))
	for id := range cfg.Accounts {
		if trimmed := strings.TrimSpace(id); trimmed != "" {
			ids = append(ids, trimmed)
		}
	}
	sort.Strings(ids)

	accounts := make([]ResolvedAccount, 0, len(ids))
	for _, id := range ids {
		raw := cfg.Accounts[id]
		account := topLevel
		account.ID = id
		account.Name = strings.TrimSpace(raw.Name)
		account.Enabled = account.Enabled && (raw.Enabled == nil || *raw.Enabled)
		account.AppID = normalize.FirstNonEmpty(raw.AppID, account.AppID)
		account.AppSecret = normalize.FirstNonEmpty(raw.AppSecret, account.AppSecret)
		account.EncryptKey = normalize.FirstNonEmpty(raw.EncryptKey, account.EncryptKey)
		account.VerificationToken = normalize.FirstNonEmpty(raw.VerificationToken, account.VerificationToken)
		account.Domain = normalizeDomain(normalize.FirstNonEmpty(raw.Domain, account.Domain))
		account.ConnectionMode = normalizeConnectionMode(normalize.FirstNonEmpty(raw.ConnectionMode, account.ConnectionMode))
		account.DMPolicy = normalizeDMPolicy(normalize.FirstNonEmpty(raw.DMPolicy, account.DMPolicy))
		if len(raw.AllowFrom) > 0 {
			account.AllowFrom = cloneStrings(raw.AllowFrom)
		}
		account.GroupPolicy = normalizeGroupPolicy(normalize.FirstNonEmpty(raw.GroupPolicy, account.GroupPolicy))
		if len(raw.GroupAllowFrom) > 0 {
			account.GroupAllowFrom = cloneStrings(raw.GroupAllowFrom)
		}
		if raw.RequireMention != nil {
			account.RequireMention = *raw.RequireMention
		}
		account.GroupSessionScope = normalizeGroupSessionScope(normalize.FirstNonEmpty(raw.GroupSessionScope, account.GroupSessionScope))
		account.ReplyInThread = strings.EqualFold(normalize.FirstNonEmpty(raw.ReplyInThread, boolToEnabledDisabled(account.ReplyInThread)), "enabled")
		accounts = append(accounts, account)
	}

	defaultID := strings.TrimSpace(cfg.DefaultAccount)
	if defaultID == "" {
		defaultID = accounts[0].ID
	}
	return defaultID, accounts
}

func defaultAccountID(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return "default"
	}
	return trimmed
}

func normalizeDomain(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return "feishu"
	}
	lower := strings.ToLower(trimmed)
	if lower == "feishu" || lower == "lark" {
		return lower
	}
	trimmed = strings.TrimRight(trimmed, "/")
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

func normalizeConnectionMode(value string) string {
	if strings.EqualFold(strings.TrimSpace(value), "webhook") {
		return "webhook"
	}
	return "websocket"
}

func normalizeDMPolicy(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "allowlist", "pairing":
		return strings.ToLower(strings.TrimSpace(value))
	default:
		return "open"
	}
}

func normalizeGroupPolicy(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "allowlist", "disabled":
		return strings.ToLower(strings.TrimSpace(value))
	default:
		return "open"
	}
}

func normalizeGroupSessionScope(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "group_sender", "group_thread", "group_thread_sender":
		return strings.ToLower(strings.TrimSpace(value))
	default:
		return "group"
	}
}

func boolToEnabledDisabled(value bool) string {
	if value {
		return "enabled"
	}
	return "disabled"
}

func cloneStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	out := make([]string, 0, len(values))
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

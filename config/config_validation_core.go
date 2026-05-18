package config

import (
	"fmt"
	neturl "net/url"
	"strings"
	"time"

	"github.com/fulcrus/hopclaw/model"
)

func (c Config) Validate() error {
	if err := validateRuntimeProfile(c.Runtime.Profile); err != nil {
		return err
	}
	if err := validateAuthConfig(c); err != nil {
		return err
	}
	switch strings.TrimSpace(strings.ToLower(c.Store.Backend)) {
	case "memory":
	case "jsonl":
		if strings.TrimSpace(c.Store.Path) == "" {
			return fmt.Errorf("store.path is required for jsonl backend")
		}
	case "sqlite":
		if strings.TrimSpace(c.Store.Path) == "" {
			return fmt.Errorf("store.path is required for sqlite backend")
		}
	default:
		return fmt.Errorf("unsupported store backend %q", c.Store.Backend)
	}
	if err := validateQueueMode(c.Agent.QueueMode); err != nil {
		return err
	}
	if c.Agent.MinActionConfidence < 0 || c.Agent.MinActionConfidence > 1 {
		return fmt.Errorf("agent.min_action_confidence must be between 0 and 1")
	}
	if err := validateNonNegativeInt("agent.max_tool_rounds", c.Agent.MaxToolRounds); err != nil {
		return err
	}
	if c.Agent.MaxRunDuration < 0 {
		return fmt.Errorf("agent.max_run_duration must be >= 0")
	}
	if err := validateNonNegativeDuration("agent.dedupe_window", c.Agent.DedupeWindow); err != nil {
		return err
	}
	if err := validateNonNegativeDuration("runtime.status_reminder_delay", c.Runtime.StatusReminderDelay); err != nil {
		return err
	}
	if err := validateNonNegativeInt("runtime.artifacts.inline_threshold", c.Runtime.Artifacts.InlineThreshold); err != nil {
		return err
	}
	if err := validateNonNegativeInt("runtime.artifacts.preview_chars", c.Runtime.Artifacts.PreviewChars); err != nil {
		return err
	}
	if err := validateServiceConfig(c.Tools.Services); err != nil {
		return err
	}
	if strings.TrimSpace(c.Models.OpenAICompat.BaseURL) != "" && strings.TrimSpace(c.Models.OpenAICompat.Model) == "" {
		return fmt.Errorf("models.openai_compat.model is required when base_url is set")
	}
	if err := validateModelsConfig(c.Agent, c.Models); err != nil {
		return err
	}
	if err := validateSkillInstallPolicy(c.Skills.InstallPolicy); err != nil {
		return err
	}
	if err := validateNonNegativeInt("skills.ensure_limit", c.Skills.EnsureLimit); err != nil {
		return err
	}
	if browserHostNeedsBaseURL(c.Hosts.Browser) && strings.TrimSpace(c.Hosts.Browser.BaseURL) == "" {
		return fmt.Errorf("hosts.browser.base_url is required when the browser host is enabled")
	}
	if desktopHostNeedsBaseURL(c.Hosts.Desktop) && strings.TrimSpace(c.Hosts.Desktop.BaseURL) == "" {
		return fmt.Errorf("hosts.desktop.base_url is required when the desktop host is enabled")
	}
	if c.Tunnel.Enabled != nil && *c.Tunnel.Enabled {
		if strings.TrimSpace(c.Tunnel.Host) == "" && strings.TrimSpace(c.Tunnel.Provider) == "" {
			return fmt.Errorf("tunnel is enabled but neither host (SSH) nor provider (tailscale) is configured")
		}
	}
	if err := validateRuntimeProfileConfig(c); err != nil {
		return err
	}
	if c.Runtime.State.SessionsRetention < 0 {
		return fmt.Errorf("runtime.state.sessions_retention must be >= 0")
	}
	for name, severity := range c.Runtime.Verification.VerifierSeverities {
		if strings.TrimSpace(name) == "" {
			return fmt.Errorf("runtime.verification.verifier_severities must not contain empty verifier names")
		}
		switch strings.ToLower(strings.TrimSpace(severity)) {
		case "info", "warning", "error", "blocking":
		default:
			return fmt.Errorf("runtime.verification.verifier_severities[%s] must be one of info, warning, error, blocking", name)
		}
	}
	if c.Runtime.State.RunsRetention < 0 {
		return fmt.Errorf("runtime.state.runs_retention must be >= 0")
	}
	if c.Runtime.State.EventsRetention < 0 {
		return fmt.Errorf("runtime.state.events_retention must be >= 0")
	}
	if c.Runtime.State.PruneInterval < 0 {
		return fmt.Errorf("runtime.state.prune_interval must be >= 0")
	}
	if c.Runtime.State.JSONLStartupLimit < 0 {
		return fmt.Errorf("runtime.state.jsonl_startup_limit must be >= 0")
	}
	if err := validateNonNegativeDuration("tools.builtins.default_exec_timeout", c.Tools.Builtins.DefaultExecTimeout); err != nil {
		return err
	}
	if err := validateNonNegativeInt("tools.builtins.max_read_bytes", c.Tools.Builtins.MaxReadBytes); err != nil {
		return err
	}
	if err := validateNonNegativeDuration("tools.local_exec.default_timeout", c.Tools.LocalExec.DefaultTimeout); err != nil {
		return err
	}
	if err := validateNonNegativeDuration("heartbeat.interval", c.Heartbeat.Interval); err != nil {
		return err
	}
	if err := validateNonNegativeDuration("heartbeat.timeout", c.Heartbeat.Timeout); err != nil {
		return err
	}
	if err := validateNonNegativeInt("wire.max_entries", c.Wire.MaxEntries); err != nil {
		return err
	}
	if err := validateNonNegativeInt("wire.max_body_bytes", c.Wire.MaxBodyBytes); err != nil {
		return err
	}
	if err := validateNonNegativeDuration("wire.retention_time", c.Wire.RetentionTime); err != nil {
		return err
	}
	if err := validateNonNegativeInt("sandbox.timeout", c.Sandbox.Timeout); err != nil {
		return err
	}
	if err := validateNonNegativeDuration("exec_approval.approval_timeout", c.ExecApproval.ApprovalTimeout); err != nil {
		return err
	}
	if err := validateNonNegativeDuration("exec_approval.grace_period", c.ExecApproval.GracePeriod); err != nil {
		return err
	}
	if err := validateNonNegativeDuration("channel_health.check_interval", c.ChannelHealth.CheckInterval); err != nil {
		return err
	}
	if err := validateNonNegativeDuration("channel_health.stale_socket_timeout", c.ChannelHealth.StaleSocketTimeout); err != nil {
		return err
	}
	if err := validateNonNegativeDuration("channel_health.stuck_run_timeout", c.ChannelHealth.StuckRunTimeout); err != nil {
		return err
	}
	if err := validateNonNegativeDuration("channel_health.startup_grace", c.ChannelHealth.StartupGrace); err != nil {
		return err
	}
	if err := validateNonNegativeInt("channel_health.max_restarts_per_hour", c.ChannelHealth.MaxRestartsPerHour); err != nil {
		return err
	}
	if err := validateNonNegativeInt64("security.max_content_size", c.Security.MaxContentSize); err != nil {
		return err
	}
	if err := validateApprovalProviderConfig(c.ExecApproval.Providers); err != nil {
		return err
	}
	if err := validateGovernanceAdapterConfig(c.Runtime.Governance.Adapters); err != nil {
		return err
	}
	if err := validateAuditSinkConfig(c.Runtime.Audit.Sinks); err != nil {
		return err
	}
	if err := validateAuditDeliveryConfig(c.Store, c.Runtime.Audit); err != nil {
		return err
	}
	if err := validateGovernanceDeliveryConfig(c.Store, c.Runtime.Governance.Delivery); err != nil {
		return err
	}
	if err := validateFeishuChannelConfig(c.Channels.Feishu); err != nil {
		return err
	}
	if err := validateCommonChannelConfig("channels.slack", c.Channels.Slack.CommonChannelConfig); err != nil {
		return err
	}
	if err := validateCommonChannelConfig("channels.discord", c.Channels.Discord.CommonChannelConfig); err != nil {
		return err
	}
	if err := validateCommonChannelConfig("channels.telegram", c.Channels.Telegram.CommonChannelConfig); err != nil {
		return err
	}
	if err := validateCommonChannelConfig("channels.whatsapp", c.Channels.WhatsApp.CommonChannelConfig); err != nil {
		return err
	}
	if err := validateCommonChannelConfig("channels.signal", c.Channels.Signal.CommonChannelConfig); err != nil {
		return err
	}
	if err := validateCommonChannelConfig("channels.line", c.Channels.LINE.CommonChannelConfig); err != nil {
		return err
	}
	if err := validateCommonChannelConfig("channels.msteams", c.Channels.MSTeams.CommonChannelConfig); err != nil {
		return err
	}
	if err := validateCommonChannelConfig("channels.matrix", c.Channels.Matrix.CommonChannelConfig); err != nil {
		return err
	}
	if err := validateCommonChannelConfig("channels.mattermost", c.Channels.Mattermost.CommonChannelConfig); err != nil {
		return err
	}
	return nil
}

func validateApprovalProviderConfig(items []ApprovalProviderConfig) error {
	if len(items) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(items))
	for i, item := range items {
		name := strings.TrimSpace(item.Name)
		if name == "" {
			return fmt.Errorf("exec_approval.providers[%d].name is required", i)
		}
		key := strings.ToLower(name)
		if _, exists := seen[key]; exists {
			return fmt.Errorf("duplicate exec_approval.providers name %q", name)
		}
		seen[key] = struct{}{}
		if item.Type != "" && item.Type != "webhook" {
			return fmt.Errorf("exec_approval.providers[%d].type must be one of webhook", i)
		}
		if err := validateNonNegativeDuration(fmt.Sprintf("exec_approval.providers[%d].callback_auth.max_age", i), item.CallbackAuth.MaxAge); err != nil {
			return err
		}
		if err := validateNonNegativeDuration(fmt.Sprintf("exec_approval.providers[%d].webhook.timeout", i), item.Webhook.Timeout); err != nil {
			return err
		}
		mode := strings.ToLower(strings.TrimSpace(item.CallbackAuth.Mode))
		if mode != "" && mode != "token" && mode != "hmac" {
			return fmt.Errorf("exec_approval.providers[%d].callback_auth.mode must be one of token, hmac", i)
		}
		if webhookConfigured(item.Webhook) || item.Type == "webhook" {
			if strings.TrimSpace(item.Webhook.SubmitURL) == "" &&
				strings.TrimSpace(item.Webhook.UpdateURL) == "" &&
				strings.TrimSpace(item.Webhook.SyncURL) == "" {
				return fmt.Errorf("exec_approval.providers[%d].webhook must configure at least one of submit_url, update_url, sync_url", i)
			}
			if err := validateApprovalWebhookURL(fmt.Sprintf("exec_approval.providers[%d].webhook.submit_url", i), item.Webhook.SubmitURL); err != nil {
				return err
			}
			if err := validateApprovalWebhookURL(fmt.Sprintf("exec_approval.providers[%d].webhook.update_url", i), item.Webhook.UpdateURL); err != nil {
				return err
			}
			if err := validateApprovalWebhookURL(fmt.Sprintf("exec_approval.providers[%d].webhook.sync_url", i), item.Webhook.SyncURL); err != nil {
				return err
			}
		}
		switch normalizeApprovalCallbackAuthMode(item.CallbackAuth) {
		case "token":
			if strings.TrimSpace(item.CallbackAuth.Token) == "" && strings.TrimSpace(item.CallbackAuth.Secret) != "" {
				return fmt.Errorf("exec_approval.providers[%d].callback_auth.secret requires mode hmac", i)
			}
		case "hmac":
			if strings.TrimSpace(item.CallbackAuth.Secret) == "" {
				return fmt.Errorf("exec_approval.providers[%d].callback_auth.secret is required for hmac mode", i)
			}
		}
	}
	return nil
}

func validateGovernanceAdapterConfig(items []GovernanceAdapterConfig) error {
	if len(items) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(items))
	for i, item := range items {
		name := strings.TrimSpace(item.Name)
		if name == "" {
			return fmt.Errorf("runtime.governance.adapters[%d].name is required", i)
		}
		key := strings.ToLower(name)
		if _, exists := seen[key]; exists {
			return fmt.Errorf("duplicate runtime.governance.adapters name %q", name)
		}
		seen[key] = struct{}{}
		if item.Type != "" && item.Type != "webhook" {
			return fmt.Errorf("runtime.governance.adapters[%d].type must be one of webhook", i)
		}
		if err := validateNonNegativeDuration(fmt.Sprintf("runtime.governance.adapters[%d].webhook.timeout", i), item.Webhook.Timeout); err != nil {
			return err
		}
		if governanceWebhookConfigured(item.Webhook) || item.Type == "webhook" {
			if strings.TrimSpace(item.Webhook.URL) == "" {
				return fmt.Errorf("runtime.governance.adapters[%d].webhook.url is required", i)
			}
			if err := validateWebhookURL(fmt.Sprintf("runtime.governance.adapters[%d].webhook.url", i), item.Webhook.URL); err != nil {
				return err
			}
			for j, kind := range item.Webhook.Kinds {
				if !isValidGovernanceAdapterKind(kind) {
					return fmt.Errorf("runtime.governance.adapters[%d].webhook.kinds[%d] must be one of approval_requested, approval_resolved, approval_timed_out, approval_grace_warning, security_event", i, j)
				}
			}
		}
	}
	return nil
}

func validateGovernanceDeliveryConfig(storeCfg StoreConfig, cfg GovernanceDeliveryConfig) error {
	switch normalizeGovernanceDeliveryBackend(cfg.Backend) {
	case "memory":
		if err := validateNonNegativeGovernanceDelivery("runtime.governance.delivery", cfg); err != nil {
			return err
		}
		return nil
	case "jsonl":
		if strings.TrimSpace(cfg.Path) == "" {
			return fmt.Errorf("runtime.governance.delivery.path is required for jsonl backend")
		}
		if err := validateNonNegativeGovernanceDelivery("runtime.governance.delivery", cfg); err != nil {
			return err
		}
		return nil
	case "sqlite":
		if strings.TrimSpace(cfg.Path) == "" && !strings.EqualFold(strings.TrimSpace(storeCfg.Backend), "sqlite") {
			return fmt.Errorf("runtime.governance.delivery.path is required for sqlite backend when store.backend is not sqlite")
		}
		if err := validateNonNegativeGovernanceDelivery("runtime.governance.delivery", cfg); err != nil {
			return err
		}
		return nil
	default:
		return fmt.Errorf("runtime.governance.delivery.backend must be one of memory, jsonl, sqlite")
	}
}

func validateWebhookURL(path, raw string) error {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	parsed, err := neturl.Parse(raw)
	if err != nil || parsed == nil || parsed.Scheme == "" || parsed.Host == "" {
		return fmt.Errorf("%s must be an absolute URL", path)
	}
	switch strings.ToLower(parsed.Scheme) {
	case "http", "https":
		return nil
	default:
		return fmt.Errorf("%s must use http or https", path)
	}
}

func validateApprovalWebhookURL(path, raw string) error {
	return validateWebhookURL(path, raw)
}

func webhookConfigured(cfg ApprovalWebhookProviderConfig) bool {
	return strings.TrimSpace(cfg.SubmitURL) != "" ||
		strings.TrimSpace(cfg.UpdateURL) != "" ||
		strings.TrimSpace(cfg.SyncURL) != ""
}

func governanceWebhookConfigured(cfg GovernanceWebhookAdapterConfig) bool {
	return strings.TrimSpace(cfg.URL) != ""
}

func validateAuditSinkConfig(items []AuditSinkConfig) error {
	if len(items) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(items))
	for i, item := range items {
		name := strings.TrimSpace(item.Name)
		if name == "" {
			return fmt.Errorf("runtime.audit.sinks[%d].name is required", i)
		}
		key := strings.ToLower(name)
		if _, exists := seen[key]; exists {
			return fmt.Errorf("duplicate runtime.audit.sinks name %q", name)
		}
		seen[key] = struct{}{}
		sinkType := normalizeAuditSinkType(item)
		switch sinkType {
		case "", "webhook", "elasticsearch", "splunk_hec":
		default:
			return fmt.Errorf("runtime.audit.sinks[%d].type must be one of webhook, elasticsearch, splunk_hec", i)
		}
		if configuredCount := configuredAuditSinkKinds(item); configuredCount > 1 {
			return fmt.Errorf("runtime.audit.sinks[%d] must configure exactly one sink kind", i)
		}
		if sinkType == "" {
			return fmt.Errorf("runtime.audit.sinks[%d] must configure one of webhook, elasticsearch, splunk_hec", i)
		}
		switch sinkType {
		case "webhook":
			if err := validateNonNegativeDuration(fmt.Sprintf("runtime.audit.sinks[%d].webhook.timeout", i), item.Webhook.Timeout); err != nil {
				return err
			}
			if strings.TrimSpace(item.Webhook.URL) == "" {
				return fmt.Errorf("runtime.audit.sinks[%d].webhook.url is required", i)
			}
			if err := validateWebhookURL(fmt.Sprintf("runtime.audit.sinks[%d].webhook.url", i), item.Webhook.URL); err != nil {
				return err
			}
		case "elasticsearch":
			if err := validateNonNegativeDuration(fmt.Sprintf("runtime.audit.sinks[%d].elasticsearch.timeout", i), item.Elasticsearch.Timeout); err != nil {
				return err
			}
			if strings.TrimSpace(item.Elasticsearch.URL) == "" {
				return fmt.Errorf("runtime.audit.sinks[%d].elasticsearch.url is required", i)
			}
			if err := validateWebhookURL(fmt.Sprintf("runtime.audit.sinks[%d].elasticsearch.url", i), item.Elasticsearch.URL); err != nil {
				return err
			}
			if strings.TrimSpace(item.Elasticsearch.Index) == "" {
				return fmt.Errorf("runtime.audit.sinks[%d].elasticsearch.index is required", i)
			}
		case "splunk_hec":
			if err := validateNonNegativeDuration(fmt.Sprintf("runtime.audit.sinks[%d].splunk_hec.timeout", i), item.SplunkHEC.Timeout); err != nil {
				return err
			}
			if strings.TrimSpace(item.SplunkHEC.URL) == "" {
				return fmt.Errorf("runtime.audit.sinks[%d].splunk_hec.url is required", i)
			}
			if err := validateWebhookURL(fmt.Sprintf("runtime.audit.sinks[%d].splunk_hec.url", i), item.SplunkHEC.URL); err != nil {
				return err
			}
			if strings.TrimSpace(item.SplunkHEC.Token) == "" {
				return fmt.Errorf("runtime.audit.sinks[%d].splunk_hec.token is required", i)
			}
		}
	}
	return nil
}

func auditWebhookConfigured(cfg AuditWebhookSinkConfig) bool {
	return strings.TrimSpace(cfg.URL) != ""
}

func auditElasticsearchConfigured(cfg AuditElasticsearchSinkConfig) bool {
	return strings.TrimSpace(cfg.URL) != "" ||
		strings.TrimSpace(cfg.Index) != "" ||
		strings.TrimSpace(cfg.APIKey) != "" ||
		len(cfg.Headers) > 0
}

func auditSplunkHECConfigured(cfg AuditSplunkHECSinkConfig) bool {
	return strings.TrimSpace(cfg.URL) != "" ||
		strings.TrimSpace(cfg.Token) != "" ||
		strings.TrimSpace(cfg.Source) != "" ||
		strings.TrimSpace(cfg.SourceType) != "" ||
		strings.TrimSpace(cfg.Index) != "" ||
		strings.TrimSpace(cfg.Host) != "" ||
		len(cfg.Headers) > 0
}

func configuredAuditSinkKinds(item AuditSinkConfig) int {
	count := 0
	if auditWebhookConfigured(item.Webhook) {
		count++
	}
	if auditElasticsearchConfigured(item.Elasticsearch) {
		count++
	}
	if auditSplunkHECConfigured(item.SplunkHEC) {
		count++
	}
	return count
}

func validateAuditDeliveryConfig(storeCfg StoreConfig, auditCfg AuditConfig) error {
	if len(auditCfg.Sinks) == 0 {
		return nil
	}
	if err := validateNonNegativeAuditDelivery("runtime.audit.delivery", auditCfg.Delivery); err != nil {
		return err
	}
	switch normalizeAuditDeliveryBackend(auditCfg.Delivery.Backend) {
	case "", "memory", "sqlite":
	default:
		return fmt.Errorf("runtime.audit.delivery.backend must be one of memory or sqlite")
	}
	if normalizeAuditDeliveryBackend(auditCfg.Delivery.Backend) == "sqlite" &&
		!strings.EqualFold(strings.TrimSpace(storeCfg.Backend), "sqlite") {
		return fmt.Errorf("runtime.audit.delivery.backend=sqlite requires store.backend=sqlite")
	}
	return nil
}

func normalizeGovernanceDeliveryBackend(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "memory":
		return strings.ToLower(strings.TrimSpace(value))
	case "jsonl":
		return "jsonl"
	case "sqlite":
		return "sqlite"
	default:
		return strings.ToLower(strings.TrimSpace(value))
	}
}

func normalizeApprovalCallbackAuthMode(cfg ApprovalCallbackAuthConfig) string {
	mode := strings.ToLower(strings.TrimSpace(cfg.Mode))
	if mode != "" {
		return mode
	}
	if strings.TrimSpace(cfg.Secret) != "" {
		return "hmac"
	}
	return "token"
}

func isValidGovernanceAdapterKind(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "approval_requested", "approval_resolved", "approval_timed_out", "approval_grace_warning", "security_event":
		return true
	default:
		return false
	}
}

func normalizeSkillInstallPolicy(raw string) string {
	switch strings.TrimSpace(strings.ToLower(raw)) {
	case "", SkillInstallPolicyAsk:
		return SkillInstallPolicyAsk
	case SkillInstallPolicyAuto:
		return SkillInstallPolicyAuto
	case SkillInstallPolicyDeny:
		return SkillInstallPolicyDeny
	default:
		return strings.TrimSpace(strings.ToLower(raw))
	}
}

func validateSkillInstallPolicy(raw string) error {
	switch normalizeSkillInstallPolicy(raw) {
	case SkillInstallPolicyAsk, SkillInstallPolicyAuto, SkillInstallPolicyDeny:
		return nil
	default:
		return fmt.Errorf("unsupported skills.install_policy %q", raw)
	}
}

func browserHostNeedsBaseURL(cfg BrowserHostConfig) bool {
	if cfg.Enabled != nil && *cfg.Enabled && strings.TrimSpace(cfg.BaseURL) == "" {
		return false
	}
	return enabledOrConfigSet(cfg.Enabled, cfg.BaseURL, cfg.AuthToken)
}

func desktopHostNeedsBaseURL(cfg DesktopHostConfig) bool {
	if cfg.Enabled != nil && *cfg.Enabled && strings.TrimSpace(cfg.BaseURL) == "" {
		return false
	}
	return enabledOrConfigSet(cfg.Enabled, cfg.BaseURL, cfg.AuthToken)
}

func normalizeRuntimeProfile(raw string) string {
	switch strings.TrimSpace(strings.ToLower(raw)) {
	case "":
		return RuntimeProfileTrustedDesktop
	case RuntimeProfileDesktop:
		return RuntimeProfileDesktop
	case RuntimeProfileTrustedDesktop:
		return RuntimeProfileTrustedDesktop
	case RuntimeProfileProduction:
		return RuntimeProfileProduction
	default:
		return strings.TrimSpace(strings.ToLower(raw))
	}
}

func validateRuntimeProfile(raw string) error {
	switch normalizeRuntimeProfile(raw) {
	case RuntimeProfileDesktop, RuntimeProfileTrustedDesktop, RuntimeProfileProduction:
		return nil
	default:
		return fmt.Errorf("unsupported runtime.profile %q", raw)
	}
}

func validateRuntimeProfileConfig(c Config) error {
	if normalizeRuntimeProfile(c.Runtime.Profile) != RuntimeProfileProduction {
		return nil
	}
	if !c.HasAuth() {
		return fmt.Errorf("authentication is required when runtime.profile is production (set server.auth_token or auth section)")
	}
	if !c.Runtime.Audit.Enabled {
		return fmt.Errorf("runtime.audit.enabled must be true when runtime.profile is production")
	}
	if strings.EqualFold(strings.TrimSpace(c.Store.Backend), "memory") {
		return fmt.Errorf("store.backend=memory is not supported when runtime.profile is production")
	}
	return nil
}

func validateNonNegativeInt(path string, value int) error {
	if value < 0 {
		return fmt.Errorf("%s must be >= 0", path)
	}
	return nil
}

func validateNonNegativeInt64(path string, value int64) error {
	if value < 0 {
		return fmt.Errorf("%s must be >= 0", path)
	}
	return nil
}

func validateNonNegativeDuration(path string, value time.Duration) error {
	if value < 0 {
		return fmt.Errorf("%s must be >= 0", path)
	}
	return nil
}

func validateNonNegativeAuditDelivery(path string, cfg AuditDeliveryConfig) error {
	if err := validateNonNegativeInt(path+".max_attempts", cfg.MaxAttempts); err != nil {
		return err
	}
	if err := validateNonNegativeDuration(path+".base_backoff", cfg.BaseBackoff); err != nil {
		return err
	}
	if err := validateNonNegativeDuration(path+".max_backoff", cfg.MaxBackoff); err != nil {
		return err
	}
	if err := validateNonNegativeDuration(path+".poll_interval", cfg.PollInterval); err != nil {
		return err
	}
	if err := validateNonNegativeInt(path+".batch_size", cfg.BatchSize); err != nil {
		return err
	}
	return nil
}

func validateNonNegativeGovernanceDelivery(path string, cfg GovernanceDeliveryConfig) error {
	if err := validateNonNegativeInt(path+".max_attempts", cfg.MaxAttempts); err != nil {
		return err
	}
	if err := validateNonNegativeDuration(path+".base_backoff", cfg.BaseBackoff); err != nil {
		return err
	}
	if err := validateNonNegativeDuration(path+".max_backoff", cfg.MaxBackoff); err != nil {
		return err
	}
	if err := validateNonNegativeDuration(path+".poll_interval", cfg.PollInterval); err != nil {
		return err
	}
	if err := validateNonNegativeInt(path+".batch_size", cfg.BatchSize); err != nil {
		return err
	}
	return nil
}

func validateModelsConfig(agentCfg AgentConfig, cfg ModelsConfig) error {
	providerNames := make(map[string]struct{}, len(cfg.Providers)+1)
	if strings.TrimSpace(cfg.OpenAICompat.BaseURL) != "" {
		providerNames["default"] = struct{}{}
	}
	for name, entry := range cfg.Providers {
		name = strings.TrimSpace(name)
		if name == "" {
			return fmt.Errorf("models.providers contains an empty provider name")
		}
		providerNames[name] = struct{}{}
		if catalog, ok := model.CatalogLookup(name); ok && catalog.RequireBaseURL && strings.TrimSpace(entry.BaseURL) == "" {
			return fmt.Errorf("models.providers.%s.base_url is required for this catalog provider", name)
		}
	}

	defaultProvider := strings.TrimSpace(cfg.DefaultProvider)
	if defaultProvider != "" {
		if _, ok := providerNames[defaultProvider]; !ok {
			return fmt.Errorf("models.default_provider %q is not configured", defaultProvider)
		}
	}

	_, hasCompatDefault := providerNames["default"]
	if len(providerNames) > 1 && defaultProvider == "" && !hasCompatDefault {
		knownProviders := make([]string, 0, len(providerNames))
		for name := range providerNames {
			knownProviders = append(knownProviders, name)
		}
		if _, modelID, ok := model.MatchProviderPrefix(strings.TrimSpace(agentCfg.DefaultModel), knownProviders); !ok || strings.TrimSpace(modelID) == "" {
			return fmt.Errorf("agent.default_model must use a configured provider/model reference or models.default_provider must be set when multiple model providers are configured")
		}
	}
	return nil
}

func validateQueueMode(raw string) error {
	mode := strings.TrimSpace(strings.ToLower(raw))
	switch mode {
	case "", "enqueue", "reject", "coalesce":
		return nil
	case "interrupt":
		return fmt.Errorf("agent.queue_mode %q is not supported in this release", raw)
	default:
		return fmt.Errorf("unsupported agent.queue_mode %q", raw)
	}
}

func validateServiceConfig(cfg ServicesConfig) error {
	if err := validateSearchService(cfg.Search); err != nil {
		return err
	}
	if err := validateSpeechService(cfg.Speech); err != nil {
		return err
	}
	if err := validateEmailService(cfg.Email); err != nil {
		return err
	}
	return nil
}

func enabledOrConfigSet(enabled *bool, values ...string) bool {
	if enabled != nil {
		return *enabled
	}
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return true
		}
	}
	return false
}

func validateSearchService(cfg SearchServiceConfig) error {
	provider := strings.TrimSpace(strings.ToLower(cfg.Provider))
	switch provider {
	case "", "serpapi", "tavily", "bing", "google", "generic":
	default:
		return fmt.Errorf("unsupported tools.services.search.provider %q", cfg.Provider)
	}
	if provider == "generic" && strings.TrimSpace(cfg.BaseURL) == "" {
		return fmt.Errorf("tools.services.search.base_url is required when provider is generic")
	}
	return nil
}

func validateSpeechService(cfg SpeechServiceConfig) error {
	hasBaseURL := strings.TrimSpace(cfg.BaseURL) != ""
	hasAPIKey := strings.TrimSpace(cfg.APIKey) != ""
	if hasBaseURL != hasAPIKey {
		return fmt.Errorf("tools.services.speech requires both base_url and api_key when enabled")
	}
	return nil
}

func validateEmailService(cfg EmailServiceConfig) error {
	hasIMAPHost := strings.TrimSpace(cfg.IMAPHost) != ""
	if !hasIMAPHost && cfg.IMAPPort != 0 {
		return fmt.Errorf("tools.services.email.imap_host is required when imap_port is set")
	}
	if hasIMAPHost {
		if strings.TrimSpace(cfg.Username) == "" || strings.TrimSpace(cfg.Password) == "" {
			return fmt.Errorf("tools.services.email requires username and password when imap_host is set")
		}
		if cfg.IMAPPort < 0 {
			return fmt.Errorf("tools.services.email.imap_port must be >= 0")
		}
	}
	return nil
}

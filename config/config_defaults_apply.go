package config

import (
	"strings"
	"time"
)

func (c *Config) applyDefaults() {
	if strings.TrimSpace(c.Server.Address) == "" {
		c.Server.Address = DefaultGatewayAddress
	}
	c.AuthZ.Mode = normalizeAuthZMode(c.AuthZ.Mode)
	c.AuthZ.Fallback = normalizeAuthZFallback(c.AuthZ.Fallback)
	c.AuthZ.Webhook.URL = strings.TrimSpace(c.AuthZ.Webhook.URL)
	c.AuthZ.Webhook.Secret = strings.TrimSpace(c.AuthZ.Webhook.Secret)
	if (c.AuthZ.Mode == "webhook" || strings.TrimSpace(c.AuthZ.Webhook.URL) != "") && c.AuthZ.Webhook.Timeout == 0 {
		c.AuthZ.Webhook.Timeout = 5 * time.Second
	}
	if strings.TrimSpace(c.Store.Backend) == "" {
		c.Store.Backend = "sqlite"
	}
	if strings.EqualFold(strings.TrimSpace(c.Store.Backend), "sqlite") && strings.TrimSpace(c.Store.Path) == "" {
		c.Store.Path = ".hopclaw/state"
	}
	if strings.TrimSpace(c.Agent.SystemPrompt) == "" {
		c.Agent.SystemPrompt = defaultAgentSystemPrompt
	}
	if strings.TrimSpace(c.Agent.DefaultModel) == "" {
		c.Agent.DefaultModel = "unconfigured-model"
	}
	if c.Agent.MaxToolRounds == 0 {
		c.Agent.MaxToolRounds = 16
	}
	if c.Agent.MinActionConfidence == 0 {
		c.Agent.MinActionConfidence = 0.4
	}
	if c.Agent.MaxRunDuration == 0 {
		c.Agent.MaxRunDuration = 10 * time.Minute
	}
	if strings.TrimSpace(c.Agent.QueueMode) == "" {
		c.Agent.QueueMode = "enqueue"
	}
	if c.Agent.DedupeWindow == 0 {
		c.Agent.DedupeWindow = time.Minute
	}
	applyFeishuChannelDefaults(&c.Channels.Feishu)
	applyCommonChannelDefaults(&c.Channels.Slack.CommonChannelConfig)
	applyCommonChannelDefaults(&c.Channels.Discord.CommonChannelConfig)
	applyCommonChannelDefaults(&c.Channels.Telegram.CommonChannelConfig)
	applyCommonChannelDefaults(&c.Channels.WhatsApp.CommonChannelConfig)
	applyCommonChannelDefaults(&c.Channels.Signal.CommonChannelConfig)
	applyCommonChannelDefaults(&c.Channels.LINE.CommonChannelConfig)
	applyCommonChannelDefaults(&c.Channels.MSTeams.CommonChannelConfig)
	applyCommonChannelDefaults(&c.Channels.Matrix.CommonChannelConfig)
	applyCommonChannelDefaults(&c.Channels.Mattermost.CommonChannelConfig)
	c.Runtime.Profile = normalizeRuntimeProfile(c.Runtime.Profile)
	if c.Runtime.StatusReminderDelay == 0 {
		c.Runtime.StatusReminderDelay = 6 * time.Second
	}
	if len(c.Runtime.Verification.VerifierSeverities) > 0 {
		normalized := make(map[string]string, len(c.Runtime.Verification.VerifierSeverities))
		for name, severity := range c.Runtime.Verification.VerifierSeverities {
			name = strings.TrimSpace(name)
			severity = strings.ToLower(strings.TrimSpace(severity))
			if name == "" || severity == "" {
				continue
			}
			normalized[name] = severity
		}
		if len(normalized) == 0 {
			c.Runtime.Verification.VerifierSeverities = nil
		} else {
			c.Runtime.Verification.VerifierSeverities = normalized
		}
	}
	if c.Runtime.Artifacts.Enabled == nil {
		enabled := true
		c.Runtime.Artifacts.Enabled = &enabled
	}
	if c.Runtime.Artifacts.InlineThreshold == 0 {
		c.Runtime.Artifacts.InlineThreshold = 8 * 1024
	}
	if c.Runtime.Artifacts.PreviewChars == 0 {
		c.Runtime.Artifacts.PreviewChars = 512
	}
	if (c.Runtime.State.SessionsRetention > 0 || c.Runtime.State.RunsRetention > 0 || c.Runtime.State.EventsRetention > 0) &&
		c.Runtime.State.PruneInterval == 0 {
		c.Runtime.State.PruneInterval = time.Hour
	}
	if c.Tools.Builtins.Enabled == nil {
		enabled := true
		c.Tools.Builtins.Enabled = &enabled
	}
	if strings.TrimSpace(c.Tools.Builtins.Root) == "" {
		c.Tools.Builtins.Root = "."
	}
	if c.Tools.Builtins.DefaultExecTimeout == 0 {
		c.Tools.Builtins.DefaultExecTimeout = 30 * time.Second
	}
	if c.Tools.Builtins.MaxReadBytes == 0 {
		c.Tools.Builtins.MaxReadBytes = 256 * 1024
	}
	if c.Tools.LocalExec.Enabled == nil {
		enabled := true
		c.Tools.LocalExec.Enabled = &enabled
	}
	if c.Tools.LocalExec.DefaultTimeout == 0 {
		c.Tools.LocalExec.DefaultTimeout = 30 * time.Second
	}
	if strings.TrimSpace(c.Skills.InstallPolicy) == "" {
		switch c.Runtime.Profile {
		case RuntimeProfileProduction:
			c.Skills.InstallPolicy = SkillInstallPolicyAsk
		default:
			c.Skills.InstallPolicy = SkillInstallPolicyAuto
		}
	} else {
		c.Skills.InstallPolicy = normalizeSkillInstallPolicy(c.Skills.InstallPolicy)
	}
	if c.Skills.EnsureLimit == 0 {
		c.Skills.EnsureLimit = 5
	}
	if c.Skills.AutoRefresh == nil {
		enabled := true
		c.Skills.AutoRefresh = &enabled
	}
	if strings.TrimSpace(c.Tools.Capabilities.Exec.Mode) == "" {
		switch c.Runtime.Profile {
		case RuntimeProfileProduction:
			c.Tools.Capabilities.Exec.Mode = "allowlist"
		default:
			c.Tools.Capabilities.Exec.Mode = "approve"
		}
	}
	if c.Tools.Capabilities.Net.AllowLocal == nil {
		b := c.Runtime.Profile != RuntimeProfileProduction
		c.Tools.Capabilities.Net.AllowLocal = &b
	}
	if len(c.Tools.Capabilities.FS.SkipDirs) == 0 {
		c.Tools.Capabilities.FS.SkipDirs = []string{
			".git", "node_modules", "vendor", "__pycache__", ".venv",
		}
	}
	if c.Heartbeat.Interval == 0 {
		c.Heartbeat.Interval = 30 * time.Second
	}
	if c.Heartbeat.Timeout == 0 {
		c.Heartbeat.Timeout = 2 * time.Minute
	}
	if c.Wire.MaxEntries == 0 {
		c.Wire.MaxEntries = 1000
	}
	if c.Wire.MaxBodyBytes == 0 {
		c.Wire.MaxBodyBytes = 64 * 1024
	}
	if c.Wire.RetentionTime == 0 {
		c.Wire.RetentionTime = time.Hour
	}
	if c.Sandbox.Timeout == 0 {
		c.Sandbox.Timeout = 30
	}
	if c.ExecApproval.ApprovalTimeout == 0 {
		c.ExecApproval.ApprovalTimeout = 5 * time.Minute
	}
	if c.ExecApproval.GracePeriod == 0 {
		c.ExecApproval.GracePeriod = 30 * time.Second
	}
	for i := range c.ExecApproval.Providers {
		c.ExecApproval.Providers[i].Name = strings.TrimSpace(c.ExecApproval.Providers[i].Name)
		c.ExecApproval.Providers[i].Type = strings.ToLower(strings.TrimSpace(c.ExecApproval.Providers[i].Type))
		callback := &c.ExecApproval.Providers[i].CallbackAuth
		if strings.TrimSpace(callback.Mode) == "" {
			if strings.TrimSpace(callback.Secret) != "" {
				callback.Mode = "hmac"
			} else {
				callback.Mode = "token"
			}
		}
		callback.Mode = strings.ToLower(strings.TrimSpace(callback.Mode))
		if strings.TrimSpace(callback.HeaderName) == "" {
			callback.HeaderName = "X-HopClaw-Approval-Token"
		}
		if strings.TrimSpace(callback.SignatureHeader) == "" {
			callback.SignatureHeader = "X-HopClaw-Signature"
		}
		if strings.TrimSpace(callback.TimestampHeader) == "" {
			callback.TimestampHeader = "X-HopClaw-Timestamp"
		}
		if callback.MaxAge == 0 {
			callback.MaxAge = 5 * time.Minute
		}
		callback.Token = strings.TrimSpace(callback.Token)
		callback.Secret = strings.TrimSpace(callback.Secret)
		webhook := &c.ExecApproval.Providers[i].Webhook
		webhook.SubmitURL = strings.TrimSpace(webhook.SubmitURL)
		webhook.UpdateURL = strings.TrimSpace(webhook.UpdateURL)
		webhook.SyncURL = strings.TrimSpace(webhook.SyncURL)
		webhook.Secret = strings.TrimSpace(webhook.Secret)
		if webhook.Timeout == 0 {
			webhook.Timeout = 15 * time.Second
		}
	}
	for i := range c.Runtime.Governance.Adapters {
		c.Runtime.Governance.Adapters[i].Name = strings.TrimSpace(c.Runtime.Governance.Adapters[i].Name)
		c.Runtime.Governance.Adapters[i].Type = strings.ToLower(strings.TrimSpace(c.Runtime.Governance.Adapters[i].Type))
		webhook := &c.Runtime.Governance.Adapters[i].Webhook
		webhook.URL = strings.TrimSpace(webhook.URL)
		webhook.Secret = strings.TrimSpace(webhook.Secret)
		if webhook.Timeout == 0 {
			webhook.Timeout = 15 * time.Second
		}
		if webhook.IncludeSnapshot == nil {
			includeSnapshot := true
			webhook.IncludeSnapshot = &includeSnapshot
		}
		if len(webhook.Kinds) > 0 {
			kinds := make([]string, 0, len(webhook.Kinds))
			for _, item := range webhook.Kinds {
				if trimmed := strings.ToLower(strings.TrimSpace(item)); trimmed != "" {
					kinds = append(kinds, trimmed)
				}
			}
			webhook.Kinds = kinds
		}
	}
	for i := range c.Runtime.Audit.Sinks {
		c.Runtime.Audit.Sinks[i].Name = strings.TrimSpace(c.Runtime.Audit.Sinks[i].Name)
		c.Runtime.Audit.Sinks[i].Type = normalizeAuditSinkType(c.Runtime.Audit.Sinks[i])
		webhook := &c.Runtime.Audit.Sinks[i].Webhook
		webhook.URL = strings.TrimSpace(webhook.URL)
		webhook.Secret = strings.TrimSpace(webhook.Secret)
		if webhook.Timeout == 0 {
			webhook.Timeout = 15 * time.Second
		}
		elasticsearch := &c.Runtime.Audit.Sinks[i].Elasticsearch
		elasticsearch.URL = strings.TrimSpace(elasticsearch.URL)
		elasticsearch.Index = strings.TrimSpace(elasticsearch.Index)
		elasticsearch.APIKey = strings.TrimSpace(elasticsearch.APIKey)
		if elasticsearch.Timeout == 0 && (auditElasticsearchConfigured(*elasticsearch) || c.Runtime.Audit.Sinks[i].Type == "elasticsearch") {
			elasticsearch.Timeout = 15 * time.Second
		}
		splunk := &c.Runtime.Audit.Sinks[i].SplunkHEC
		splunk.URL = strings.TrimSpace(splunk.URL)
		splunk.Token = strings.TrimSpace(splunk.Token)
		splunk.Source = strings.TrimSpace(splunk.Source)
		splunk.SourceType = strings.TrimSpace(splunk.SourceType)
		splunk.Index = strings.TrimSpace(splunk.Index)
		splunk.Host = strings.TrimSpace(splunk.Host)
		if splunk.Timeout == 0 && (auditSplunkHECConfigured(*splunk) || c.Runtime.Audit.Sinks[i].Type == "splunk_hec") {
			splunk.Timeout = 15 * time.Second
		}
	}
	c.Runtime.Audit.Delivery.Backend = normalizeAuditDeliveryBackend(c.Runtime.Audit.Delivery.Backend)
	if c.Runtime.Audit.Delivery.Backend == "" {
		switch normalizeGovernanceDeliveryBackend(c.Store.Backend) {
		case "sqlite":
			c.Runtime.Audit.Delivery.Backend = "sqlite"
		default:
			c.Runtime.Audit.Delivery.Backend = "memory"
		}
	}
	if len(c.Runtime.Audit.Sinks) > 0 {
		if c.Runtime.Audit.Delivery.MaxAttempts == 0 {
			c.Runtime.Audit.Delivery.MaxAttempts = 8
		}
		if c.Runtime.Audit.Delivery.BaseBackoff == 0 {
			c.Runtime.Audit.Delivery.BaseBackoff = 5 * time.Second
		}
		if c.Runtime.Audit.Delivery.MaxBackoff == 0 {
			c.Runtime.Audit.Delivery.MaxBackoff = 5 * time.Minute
		}
		if c.Runtime.Audit.Delivery.MaxBackoff > 0 && c.Runtime.Audit.Delivery.MaxBackoff < c.Runtime.Audit.Delivery.BaseBackoff {
			c.Runtime.Audit.Delivery.MaxBackoff = c.Runtime.Audit.Delivery.BaseBackoff
		}
		if c.Runtime.Audit.Delivery.PollInterval == 0 {
			c.Runtime.Audit.Delivery.PollInterval = 2 * time.Second
		}
		if c.Runtime.Audit.Delivery.BatchSize == 0 {
			c.Runtime.Audit.Delivery.BatchSize = 32
		}
	}
	c.Runtime.Governance.Delivery.Backend = normalizeGovernanceDeliveryBackend(c.Runtime.Governance.Delivery.Backend)
	if c.Runtime.Governance.Delivery.Backend == "" {
		c.Runtime.Governance.Delivery.Backend = normalizeGovernanceDeliveryBackend(c.Store.Backend)
		if c.Runtime.Governance.Delivery.Backend == "" {
			c.Runtime.Governance.Delivery.Backend = "memory"
		}
	}
	c.Runtime.Governance.Delivery.Path = strings.TrimSpace(c.Runtime.Governance.Delivery.Path)
	if c.Runtime.Governance.Delivery.Backend != "memory" && c.Runtime.Governance.Delivery.Path == "" {
		if strings.TrimSpace(c.Store.Path) != "" {
			c.Runtime.Governance.Delivery.Path = strings.TrimSpace(c.Store.Path)
		} else {
			c.Runtime.Governance.Delivery.Path = ".hopclaw/state"
		}
	}
	if c.Runtime.Governance.Delivery.MaxAttempts == 0 {
		c.Runtime.Governance.Delivery.MaxAttempts = 8
	}
	if c.Runtime.Governance.Delivery.BaseBackoff == 0 {
		c.Runtime.Governance.Delivery.BaseBackoff = 5 * time.Second
	}
	if c.Runtime.Governance.Delivery.MaxBackoff == 0 {
		c.Runtime.Governance.Delivery.MaxBackoff = 5 * time.Minute
	}
	if c.Runtime.Governance.Delivery.MaxBackoff > 0 && c.Runtime.Governance.Delivery.MaxBackoff < c.Runtime.Governance.Delivery.BaseBackoff {
		c.Runtime.Governance.Delivery.MaxBackoff = c.Runtime.Governance.Delivery.BaseBackoff
	}
	if c.Runtime.Governance.Delivery.PollInterval == 0 {
		c.Runtime.Governance.Delivery.PollInterval = 2 * time.Second
	}
	if c.Runtime.Governance.Delivery.BatchSize == 0 {
		c.Runtime.Governance.Delivery.BatchSize = 32
	}
	if c.ChannelHealth.CheckInterval == 0 {
		c.ChannelHealth.CheckInterval = 30 * time.Second
	}
	if c.ChannelHealth.StaleSocketTimeout == 0 {
		c.ChannelHealth.StaleSocketTimeout = 5 * time.Minute
	}
	if c.ChannelHealth.StuckRunTimeout == 0 {
		c.ChannelHealth.StuckRunTimeout = 10 * time.Minute
	}
	if c.ChannelHealth.StartupGrace == 0 {
		c.ChannelHealth.StartupGrace = 30 * time.Second
	}
	if c.ChannelHealth.MaxRestartsPerHour == 0 {
		c.ChannelHealth.MaxRestartsPerHour = 5
	}
	if c.Security.MaxContentSize == 0 {
		c.Security.MaxContentSize = 10 * 1024 * 1024
	}
}

func normalizeAuditDeliveryBackend(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "auto":
		return ""
	case "memory":
		return "memory"
	case "sqlite":
		return "sqlite"
	default:
		return strings.ToLower(strings.TrimSpace(value))
	}
}

func normalizeAuditSinkType(item AuditSinkConfig) string {
	switch strings.ToLower(strings.TrimSpace(item.Type)) {
	case "":
	case "webhook", "elasticsearch", "splunk_hec":
		return strings.ToLower(strings.TrimSpace(item.Type))
	default:
		return strings.ToLower(strings.TrimSpace(item.Type))
	}
	switch {
	case auditWebhookConfigured(item.Webhook):
		return "webhook"
	case auditElasticsearchConfigured(item.Elasticsearch):
		return "elasticsearch"
	case auditSplunkHECConfigured(item.SplunkHEC):
		return "splunk_hec"
	default:
		return ""
	}
}

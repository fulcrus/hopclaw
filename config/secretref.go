package config

import (
	"fmt"
	"sort"
	"strings"
)

type SecretRefKind string

const (
	SecretRefKindLiteral  SecretRefKind = "literal"
	SecretRefKindEnv      SecretRefKind = "env"
	SecretRefKindKeychain SecretRefKind = "keychain"
)

type SecretRefSummary struct {
	Path    string        `json:"path"`
	Kind    SecretRefKind `json:"kind"`
	Locator string        `json:"locator,omitempty"`
}

type SecretRefInventory struct {
	Items  []SecretRefSummary `json:"items"`
	Count  int                `json:"count"`
	ByKind map[string]int     `json:"by_kind,omitempty"`
}

// SecretInventory returns a sanitized inventory of configured secret-bearing
// fields. Literal values are never echoed back; env:/keychain: locators are
// preserved because they identify the source rather than the secret material.
func (c Config) SecretInventory() SecretRefInventory {
	items := make([]SecretRefSummary, 0, 32)
	counts := map[string]int{}
	c.walkSecretFields(func(path string, value *string) {
		kind, locator, ok := classifySecretRef(*value)
		if !ok {
			return
		}
		items = append(items, SecretRefSummary{
			Path:    path,
			Kind:    kind,
			Locator: locator,
		})
		counts[string(kind)]++
	})
	return SecretRefInventory{
		Items:  items,
		Count:  len(items),
		ByKind: emptyIfZeroSecretCounts(counts),
	}
}

func classifySecretRef(value string) (SecretRefKind, string, bool) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return "", "", false
	}
	switch {
	case strings.HasPrefix(trimmed, "env:"):
		target := strings.TrimSpace(strings.TrimPrefix(trimmed, "env:"))
		if target == "" {
			return SecretRefKindLiteral, "", true
		}
		return SecretRefKindEnv, "env:" + target, true
	case strings.HasPrefix(trimmed, "keychain:"):
		target := strings.TrimSpace(strings.TrimPrefix(trimmed, "keychain:"))
		if target == "" {
			return SecretRefKindLiteral, "", true
		}
		return SecretRefKindKeychain, "keychain:" + target, true
	default:
		return SecretRefKindLiteral, "", true
	}
}

func (c *Config) walkSecretFields(fn func(path string, value *string)) {
	if c == nil || fn == nil {
		return
	}
	visit := func(path string, value *string) {
		if value == nil {
			return
		}
		fn(path, value)
	}
	visitSensitiveHeaders := func(base string, headers map[string]string) {
		for _, key := range sensitiveHeaderKeys(headers) {
			value := headers[key]
			visit(base+".headers["+key+"]", &value)
			headers[key] = value
		}
	}

	visit("server.auth_token", &c.Server.AuthToken)

	visit("auth.bearer_token", &c.Auth.BearerToken)
	if c.Auth.JWT != nil {
		visit("auth.jwt.secret", &c.Auth.JWT.Secret)
	}
	visit("authz.webhook.secret", &c.AuthZ.Webhook.Secret)
	visitSensitiveHeaders("authz.webhook", c.AuthZ.Webhook.Headers)
	for i := range c.Auth.APIKeys {
		visit(indexedPath("auth.api_keys", i)+".key", &c.Auth.APIKeys[i].Key)
	}
	if c.Auth.OAuth2 != nil {
		visit("auth.oauth2.client_secret", &c.Auth.OAuth2.ClientSecret)
	}

	visit("models.openai_compat.api_key", &c.Models.OpenAICompat.APIKey)
	visitSensitiveHeaders("models.openai_compat", c.Models.OpenAICompat.Headers)
	for _, name := range sortedProviderConfigKeys(c.Models.Providers) {
		provider := c.Models.Providers[name]
		prefix := keyedPath("models.providers", name)
		visit(prefix+".api_key", &provider.APIKey)
		for i := range provider.APIKeys {
			visit(indexedPath(prefix+".api_keys", i), &provider.APIKeys[i])
		}
		visit(prefix+".secret_key", &provider.SecretKey)
		visit(prefix+".session_token", &provider.SessionToken)
		visitSensitiveHeaders(prefix, provider.Headers)
		c.Models.Providers[name] = provider
	}

	visit("channels.telegram.bot_token", &c.Channels.Telegram.BotToken)
	visit("channels.slack.bot_token", &c.Channels.Slack.BotToken)
	visit("channels.slack.app_token", &c.Channels.Slack.AppToken)
	visit("channels.discord.bot_token", &c.Channels.Discord.BotToken)
	visit("channels.feishu.app_secret", &c.Channels.Feishu.AppSecret)
	visit("channels.feishu.encrypt_key", &c.Channels.Feishu.EncryptKey)
	visit("channels.feishu.verification_token", &c.Channels.Feishu.VerificationToken)
	for _, name := range sortedFeishuAccountConfigKeys(c.Channels.Feishu.Accounts) {
		account := c.Channels.Feishu.Accounts[name]
		prefix := keyedPath("channels.feishu.accounts", name)
		visit(prefix+".app_secret", &account.AppSecret)
		visit(prefix+".encrypt_key", &account.EncryptKey)
		visit(prefix+".verification_token", &account.VerificationToken)
		c.Channels.Feishu.Accounts[name] = account
	}
	visit("channels.whatsapp.api_token", &c.Channels.WhatsApp.APIToken)
	visit("channels.signal.auth_token", &c.Channels.Signal.AuthToken)
	visit("channels.imessage.api_key", &c.Channels.IMessage.APIKey)
	visit("channels.line.channel_secret", &c.Channels.LINE.ChannelSecret)
	visit("channels.line.channel_token", &c.Channels.LINE.ChannelToken)
	visit("channels.msteams.password", &c.Channels.MSTeams.Password)
	visit("channels.googlechat.verification_key", &c.Channels.GoogleChat.VerificationKey)
	visit("channels.googlechat.webhook_url", &c.Channels.GoogleChat.WebhookURL)
	visit("channels.irc.password", &c.Channels.IRC.Password)
	visit("channels.matrix.access_token", &c.Channels.Matrix.AccessToken)
	visit("channels.mattermost.bot_token", &c.Channels.Mattermost.BotToken)
	visit("channels.nextcloud_talk.password", &c.Channels.NextcloudTalk.Password)
	visit("channels.nostr.private_key", &c.Channels.Nostr.PrivateKey)
	visit("channels.bluebubbles.password", &c.Channels.BlueBubbles.Password)
	visit("channels.synology_chat.webhook_url", &c.Channels.SynologyChat.WebhookURL)
	visit("channels.synology_chat.bot_token", &c.Channels.SynologyChat.BotToken)
	visit("channels.tlon.ship_code", &c.Channels.Tlon.ShipCode)
	visit("channels.twitch.oauth_token", &c.Channels.Twitch.OAuthToken)
	visit("channels.zalo.secret_key", &c.Channels.Zalo.SecretKey)
	visit("channels.zalo.access_token", &c.Channels.Zalo.AccessToken)
	visit("channels.zalo.refresh_token", &c.Channels.Zalo.RefreshToken)
	visit("channels.zalouser.cookie", &c.Channels.ZaloUser.Cookie)
	for _, name := range sortedWebhookInstanceConfigKeys(c.Channels.Webhook.Instances) {
		instance := c.Channels.Webhook.Instances[name]
		visit(keyedPath("channels.webhook.instances", name)+".secret", &instance.Secret)
		c.Channels.Webhook.Instances[name] = instance
	}

	visit("tools.services.search.api_key", &c.Tools.Services.Search.APIKey)
	visit("tools.services.email.password", &c.Tools.Services.Email.Password)
	visit("tools.services.speech.api_key", &c.Tools.Services.Speech.APIKey)
	visit("tools.services.calendar.password", &c.Tools.Services.Calendar.Password)
	visit("diagnostics.telemetry_token", &c.Diagnostics.TelemetryToken)
	visit("diagnostics.telemetry_collector_auth_token", &c.Diagnostics.TelemetryCollectorAuthToken)
	visit("diagnostics.upload_token", &c.Diagnostics.UploadToken)
	visit("diagnostics.collector_auth_token", &c.Diagnostics.CollectorAuthToken)

	for i := range c.ExecApproval.Providers {
		prefix := indexedPath("exec_approval.providers", i)
		visit(prefix+".callback_auth.token", &c.ExecApproval.Providers[i].CallbackAuth.Token)
		visit(prefix+".callback_auth.secret", &c.ExecApproval.Providers[i].CallbackAuth.Secret)
		visit(prefix+".webhook.secret", &c.ExecApproval.Providers[i].Webhook.Secret)
		visitSensitiveHeaders(prefix+".webhook", c.ExecApproval.Providers[i].Webhook.Headers)
	}
	for i := range c.Runtime.Governance.Adapters {
		prefix := indexedPath("runtime.governance.adapters", i)
		visit(prefix+".webhook.secret", &c.Runtime.Governance.Adapters[i].Webhook.Secret)
		visitSensitiveHeaders(prefix+".webhook", c.Runtime.Governance.Adapters[i].Webhook.Headers)
	}
	for i := range c.Runtime.Audit.Sinks {
		prefix := indexedPath("runtime.audit.sinks", i)
		visit(prefix+".webhook.secret", &c.Runtime.Audit.Sinks[i].Webhook.Secret)
		visitSensitiveHeaders(prefix+".webhook", c.Runtime.Audit.Sinks[i].Webhook.Headers)
		visit(prefix+".elasticsearch.api_key", &c.Runtime.Audit.Sinks[i].Elasticsearch.APIKey)
		visitSensitiveHeaders(prefix+".elasticsearch", c.Runtime.Audit.Sinks[i].Elasticsearch.Headers)
		visit(prefix+".splunk_hec.token", &c.Runtime.Audit.Sinks[i].SplunkHEC.Token)
		visitSensitiveHeaders(prefix+".splunk_hec", c.Runtime.Audit.Sinks[i].SplunkHEC.Headers)
	}

	visit("hosts.browser.auth_token", &c.Hosts.Browser.AuthToken)
	visit("hosts.desktop.auth_token", &c.Hosts.Desktop.AuthToken)

	visit("skills.hub.token", &c.Skills.Hub.Token)

	visit("embedding.api_key", &c.Embedding.APIKey)
	for _, name := range sortedEmbeddingProviderConfigKeys(c.Embedding.Providers) {
		provider := c.Embedding.Providers[name]
		visit(keyedPath("embedding.providers", name)+".api_key", &provider.APIKey)
		c.Embedding.Providers[name] = provider
	}
}

func isSensitiveHeaderName(name string) bool {
	key := strings.ToLower(strings.TrimSpace(name))
	if key == "" {
		return false
	}
	return strings.Contains(key, "auth") ||
		strings.Contains(key, "token") ||
		strings.Contains(key, "secret") ||
		strings.Contains(key, "signature") ||
		strings.Contains(key, "api-key") ||
		strings.Contains(key, "apikey") ||
		strings.HasSuffix(key, "key")
}

func sensitiveHeaderKeys(headers map[string]string) []string {
	if len(headers) == 0 {
		return nil
	}
	keys := make([]string, 0, len(headers))
	for key := range headers {
		if !isSensitiveHeaderName(key) {
			continue
		}
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func indexedPath(base string, index int) string {
	return fmt.Sprintf("%s[%d]", base, index)
}

func keyedPath(base, key string) string {
	return fmt.Sprintf("%s[%s]", base, strings.TrimSpace(key))
}

func emptyIfZeroSecretCounts(items map[string]int) map[string]int {
	if len(items) == 0 {
		return nil
	}
	return items
}

func sortedProviderConfigKeys(items map[string]ProviderConfig) []string {
	return sortedStringKeys(len(items), func(out []string) []string {
		for key := range items {
			out = append(out, key)
		}
		return out
	})
}

func sortedEmbeddingProviderConfigKeys(items map[string]EmbeddingProviderConfig) []string {
	return sortedStringKeys(len(items), func(out []string) []string {
		for key := range items {
			out = append(out, key)
		}
		return out
	})
}

func sortedFeishuAccountConfigKeys(items map[string]FeishuAccountConfig) []string {
	return sortedStringKeys(len(items), func(out []string) []string {
		for key := range items {
			out = append(out, key)
		}
		return out
	})
}

func sortedWebhookInstanceConfigKeys(items map[string]WebhookInstanceConfig) []string {
	return sortedStringKeys(len(items), func(out []string) []string {
		for key := range items {
			out = append(out, key)
		}
		return out
	})
}

func sortedStringKeys(capacity int, collect func([]string) []string) []string {
	if capacity == 0 {
		return nil
	}
	keys := collect(make([]string, 0, capacity))
	sort.Strings(keys)
	return keys
}

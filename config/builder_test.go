package config

import (
	"strings"
	"testing"
)

func TestBuildConfig_OpenAI(t *testing.T) {
	opts := SetupOptions{
		Provider: "openai",
		APIKey:   "sk-test123",
		BaseURL:  "https://api.openai.com/v1",
		Model:    "gpt-4o",
	}

	cfg, err := BuildConfig(opts)
	if err != nil {
		t.Fatalf("BuildConfig() error: %v", err)
	}

	checks := []string{
		`address: "127.0.0.1:16280"`,
		`default_model: "gpt-4o"`,
		`api_key: "sk-test123"`,
		`base_url: "https://api.openai.com/v1"`,
		`model: "gpt-4o"`,
		"openai_compat:",
	}
	for _, check := range checks {
		if !strings.Contains(cfg, check) {
			t.Errorf("config missing %q", check)
		}
	}

	// Should NOT contain provider prefix in default_model for openai.
	if strings.Contains(cfg, `default_model: "openai/`) {
		t.Error("openai should not have provider prefix in default_model")
	}
}

func TestBuildConfig_Anthropic(t *testing.T) {
	opts := SetupOptions{
		Provider: "anthropic",
		APIKey:   "sk-ant-test",
		Model:    "claude-sonnet-4-20250514",
	}

	cfg, err := BuildConfig(opts)
	if err != nil {
		t.Fatalf("BuildConfig() error: %v", err)
	}

	checks := []string{
		`default_model: "anthropic/claude-sonnet-4-20250514"`,
		`default_provider: anthropic`,
		`api: anthropic-messages`,
		`api_key: "sk-ant-test"`,
	}
	for _, check := range checks {
		if !strings.Contains(cfg, check) {
			t.Errorf("config missing %q", check)
		}
	}
}

func TestBuildConfig_Google(t *testing.T) {
	opts := SetupOptions{
		Provider: "google",
		APIKey:   "AItest",
		Model:    "gemini-2.0-flash",
	}

	cfg, err := BuildConfig(opts)
	if err != nil {
		t.Fatalf("BuildConfig() error: %v", err)
	}

	checks := []string{
		`default_model: "google/gemini-2.0-flash"`,
		`api: google-generative-ai`,
		`api_key: "AItest"`,
	}
	for _, check := range checks {
		if !strings.Contains(cfg, check) {
			t.Errorf("config missing %q", check)
		}
	}
}

func TestBuildConfig_DeepSeek(t *testing.T) {
	opts := SetupOptions{
		Provider: "deepseek",
		APIKey:   "sk-deepseek-test",
		Model:    "deepseek-chat",
	}

	cfg, err := BuildConfig(opts)
	if err != nil {
		t.Fatalf("BuildConfig() error: %v", err)
	}

	checks := []string{
		`default_model: "deepseek/deepseek-chat"`,
		`default_provider: deepseek`,
		`api: openai-completions`,
		`base_url: "https://api.deepseek.com/v1"`,
		`api_key: "sk-deepseek-test"`,
	}
	for _, check := range checks {
		if !strings.Contains(cfg, check) {
			t.Errorf("config missing %q", check)
		}
	}
}

func TestBuildConfig_Ollama(t *testing.T) {
	opts := SetupOptions{
		Provider: "ollama",
		BaseURL:  "http://localhost:11434/v1",
		Model:    "llama3.3",
	}

	cfg, err := BuildConfig(opts)
	if err != nil {
		t.Fatalf("BuildConfig() error: %v", err)
	}

	checks := []string{
		`default_model: "llama3.3"`,
		`base_url: "http://localhost:11434/v1"`,
		`api_key: "ollama"`,
	}
	for _, check := range checks {
		if !strings.Contains(cfg, check) {
			t.Errorf("config missing %q", check)
		}
	}
}

func TestBuildConfig_WithoutProviderUsesMinimalConfig(t *testing.T) {
	opts := SetupOptions{}

	cfg, err := BuildConfig(opts)
	if err != nil {
		t.Fatalf("BuildConfig() error: %v", err)
	}

	if !strings.Contains(cfg, `default_model: "unconfigured-model"`) {
		t.Fatalf("config missing unconfigured default model:\n%s", cfg)
	}
	if strings.Contains(cfg, "\nmodels:\n") {
		t.Fatalf("config should not render a models section when no provider is selected:\n%s", cfg)
	}
	if _, err := Parse([]byte(cfg)); err != nil {
		t.Fatalf("generated minimal config is not parseable: %v", err)
	}
}

func TestBuildConfig_QuotesServerAuthToken(t *testing.T) {
	opts := SetupOptions{
		AuthMode:  "bearer",
		AuthToken: `token"with\chars`,
	}

	cfg, err := BuildConfig(opts)
	if err != nil {
		t.Fatalf("BuildConfig() error: %v", err)
	}
	if !strings.Contains(cfg, `auth_token: "token\"with\\chars"`) {
		t.Fatalf("config missing quoted auth token:\n%s", cfg)
	}
	if _, err := Parse([]byte(cfg)); err != nil {
		t.Fatalf("generated config is not parseable: %v", err)
	}
}

func TestBuildConfig_Bedrock(t *testing.T) {
	opts := SetupOptions{
		Provider: "amazon-bedrock",
		ProviderValues: map[string]string{
			"region":        "us-east-1",
			"access_key_id": "AKIA_TEST",
			"secret_key":    "secret",
			"session_token": "session",
			"default_model": "anthropic.claude-3-5-sonnet-20241022-v2:0",
		},
	}

	cfg, err := BuildConfig(opts)
	if err != nil {
		t.Fatalf("BuildConfig() error: %v", err)
	}

	checks := []string{
		`default_model: "amazon-bedrock/anthropic.claude-3-5-sonnet-20241022-v2:0"`,
		`default_provider: amazon-bedrock`,
		`api: bedrock-converse`,
		`region: "us-east-1"`,
		`access_key_id: "AKIA_TEST"`,
		`secret_key: "secret"`,
		`session_token: "session"`,
		`default_model: "anthropic.claude-3-5-sonnet-20241022-v2:0"`,
	}
	for _, check := range checks {
		if !strings.Contains(cfg, check) {
			t.Errorf("config missing %q", check)
		}
	}

	if _, err := Parse([]byte(cfg)); err != nil {
		t.Fatalf("generated bedrock config is not parseable: %v", err)
	}
}

func TestBuildConfig_ProviderValuesOverrideLegacyFields(t *testing.T) {
	opts := SetupOptions{
		Provider: "openai",
		APIKey:   "legacy-key",
		BaseURL:  "https://legacy.example/v1",
		Model:    "legacy-model",
		ProviderValues: map[string]string{
			"api_key":       "provider-key",
			"base_url":      "https://provider.example/v1",
			"default_model": "gpt-4.1",
		},
	}

	cfg, err := BuildConfig(opts)
	if err != nil {
		t.Fatalf("BuildConfig() error: %v", err)
	}

	checks := []string{
		`api_key: "provider-key"`,
		`base_url: "https://provider.example/v1"`,
		`default_model: "gpt-4.1"`,
		`model: "gpt-4.1"`,
	}
	for _, check := range checks {
		if !strings.Contains(cfg, check) {
			t.Errorf("config missing %q", check)
		}
	}
	if strings.Contains(cfg, "legacy-key") || strings.Contains(cfg, "legacy-model") || strings.Contains(cfg, "https://legacy.example/v1") {
		t.Fatalf("legacy provider fields should not win:\n%s", cfg)
	}
}

func TestBuildConfig_OpenAICompatAdvancedFields(t *testing.T) {
	opts := SetupOptions{
		Provider: "openai",
		ProviderValues: map[string]string{
			"api_key":       "provider-key",
			"base_url":      "https://provider.example/v1",
			"default_model": "gpt-4.1",
			"timeout":       "45s",
			"headers": "Authorization: Bearer provider-key\n" +
				"X-Route-Key: prod",
		},
	}

	cfg, err := BuildConfig(opts)
	if err != nil {
		t.Fatalf("BuildConfig() error: %v", err)
	}
	checks := []string{
		"openai_compat:",
		`timeout: "45s"`,
		`headers:`,
		`"Authorization": "Bearer provider-key"`,
		`"X-Route-Key": "prod"`,
	}
	for _, check := range checks {
		if !strings.Contains(cfg, check) {
			t.Errorf("config missing %q", check)
		}
	}
	parsed, err := Parse([]byte(cfg))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if parsed.Models.OpenAICompat.Timeout.String() != "45s" {
		t.Fatalf("timeout = %s, want 45s", parsed.Models.OpenAICompat.Timeout)
	}
	if parsed.Models.OpenAICompat.Headers["Authorization"] != "Bearer provider-key" {
		t.Fatalf("Authorization = %q", parsed.Models.OpenAICompat.Headers["Authorization"])
	}
	if parsed.Models.OpenAICompat.Headers["X-Route-Key"] != "prod" {
		t.Fatalf("X-Route-Key = %q", parsed.Models.OpenAICompat.Headers["X-Route-Key"])
	}
}

func TestBuildConfig_OpenAIFallsBackToNamedProviderForAPIKeyPool(t *testing.T) {
	opts := SetupOptions{
		Provider: "openai",
		ProviderValues: map[string]string{
			"api_key":       "primary-key",
			"api_keys":      "backup-key-1\nbackup-key-2",
			"base_url":      "https://api.openai.com/v1",
			"default_model": "gpt-4o",
			"timeout":       "30s",
			"headers":       "X-Trace-Id: trace-123",
		},
	}

	cfg, err := BuildConfig(opts)
	if err != nil {
		t.Fatalf("BuildConfig() error: %v", err)
	}
	checks := []string{
		`default_model: "gpt-4o"`,
		`default_provider: openai`,
		`providers:`,
		`openai:`,
		`api_keys:`,
		`- "backup-key-1"`,
		`- "backup-key-2"`,
		`"X-Trace-Id": "trace-123"`,
	}
	for _, check := range checks {
		if !strings.Contains(cfg, check) {
			t.Errorf("config missing %q", check)
		}
	}
	if strings.Contains(cfg, "openai_compat:") {
		t.Fatalf("openai with api_keys should fall back to named provider config:\n%s", cfg)
	}
	parsed, err := Parse([]byte(cfg))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if parsed.Models.DefaultProvider != "openai" {
		t.Fatalf("DefaultProvider = %q, want openai", parsed.Models.DefaultProvider)
	}
	provider := parsed.Models.Providers["openai"]
	if len(provider.APIKeys) != 2 || provider.APIKeys[0] != "backup-key-1" || provider.APIKeys[1] != "backup-key-2" {
		t.Fatalf("provider.APIKeys = %#v", provider.APIKeys)
	}
	if provider.Timeout.String() != "30s" {
		t.Fatalf("provider.Timeout = %s, want 30s", provider.Timeout)
	}
	if provider.Headers["X-Trace-Id"] != "trace-123" {
		t.Fatalf("provider.Headers = %#v", provider.Headers)
	}
}

func TestBuildConfig_CustomRequiresBaseURL(t *testing.T) {
	opts := SetupOptions{
		Provider: "custom",
		ProviderValues: map[string]string{
			"api_key":       "sk-test",
			"default_model": "custom-model",
		},
	}

	if _, err := BuildConfig(opts); err == nil {
		t.Fatal("expected custom provider without base_url to fail")
	}
}

func TestBuildConfig_WithTelegram(t *testing.T) {
	opts := SetupOptions{
		Provider:     "openai",
		APIKey:       "sk-test",
		BaseURL:      "https://api.openai.com/v1",
		Model:        "gpt-4o",
		Channel:      "telegram",
		ChannelToken: "123456:ABC-DEF",
	}

	cfg, err := BuildConfig(opts)
	if err != nil {
		t.Fatalf("BuildConfig() error: %v", err)
	}

	checks := []string{
		"telegram:",
		"enabled: true",
		`bot_token: "123456:ABC-DEF"`,
	}
	for _, check := range checks {
		if !strings.Contains(cfg, check) {
			t.Errorf("config missing %q", check)
		}
	}
}

func TestBuildConfig_WithSlack(t *testing.T) {
	opts := SetupOptions{
		Provider:        "openai",
		APIKey:          "sk-test",
		BaseURL:         "https://api.openai.com/v1",
		Model:           "gpt-4o",
		Channel:         "slack",
		ChannelToken:    "xoxb-test",
		ChannelAppToken: "xapp-test",
	}

	cfg, err := BuildConfig(opts)
	if err != nil {
		t.Fatalf("BuildConfig() error: %v", err)
	}

	checks := []string{
		"slack:",
		"enabled: true",
		`bot_token: "xoxb-test"`,
		`app_token: "xapp-test"`,
	}
	for _, check := range checks {
		if !strings.Contains(cfg, check) {
			t.Errorf("config missing %q", check)
		}
	}
}

func TestBuildConfig_WithMultipleChannelsAndJWT(t *testing.T) {
	opts := SetupOptions{
		Provider:      "openai",
		APIKey:        "sk-test",
		BaseURL:       "https://api.openai.com/v1",
		Model:         "gpt-4o",
		AuthMode:      "jwt",
		AuthJWTSecret: "jwt-secret",
		Channels: []SetupChannelSelection{
			{
				ID: "telegram",
				Values: map[string]string{
					"bot_token": "123456:ABC-DEF",
				},
			},
			{
				ID: "mattermost",
				Values: map[string]string{
					"base_url":  "https://mm.example.com",
					"bot_token": "mm-token",
				},
			},
		},
	}

	cfg, err := BuildConfig(opts)
	if err != nil {
		t.Fatalf("BuildConfig() error: %v", err)
	}

	checks := []string{
		"auth:",
		"jwt:",
		`secret: "jwt-secret"`,
		"telegram:",
		`bot_token: "123456:ABC-DEF"`,
		"mattermost:",
		`base_url: "https://mm.example.com"`,
		`bot_token: "mm-token"`,
	}
	for _, check := range checks {
		if !strings.Contains(cfg, check) {
			t.Errorf("config missing %q", check)
		}
	}

	if _, err := Parse([]byte(cfg)); err != nil {
		t.Fatalf("generated multi-channel config is not parseable: %v", err)
	}
}

func TestBuildConfig_UsesOperatorChannelFields(t *testing.T) {
	opts := SetupOptions{
		Provider: "openai",
		APIKey:   "sk-test",
		BaseURL:  "https://api.openai.com/v1",
		Model:    "gpt-4o",
		Channels: []SetupChannelSelection{
			{
				ID: "slack",
				Values: map[string]string{
					"bot_token":    "xoxb-test",
					"app_token":    "xapp-test",
					"dm_policy":    "allowlist",
					"group_policy": "open",
				},
			},
		},
	}

	cfg, err := BuildConfig(opts)
	if err != nil {
		t.Fatalf("BuildConfig() error: %v", err)
	}

	checks := []string{
		"slack:",
		`bot_token: "xoxb-test"`,
		`app_token: "xapp-test"`,
		`dm_policy: "allowlist"`,
		`group_policy: "open"`,
	}
	for _, check := range checks {
		if !strings.Contains(cfg, check) {
			t.Errorf("config missing %q", check)
		}
	}
}

func TestBuildConfig_RendersBoolOperatorChannelFields(t *testing.T) {
	opts := SetupOptions{
		Provider: "openai",
		APIKey:   "sk-test",
		BaseURL:  "https://api.openai.com/v1",
		Model:    "gpt-4o",
		Channels: []SetupChannelSelection{
			{
				ID: "matrix",
				Values: map[string]string{
					"homeserver":      "https://matrix.example.com",
					"user_id":         "@bot:example.com",
					"access_token":    "matrix-token",
					"require_mention": "true",
				},
			},
		},
	}

	cfg, err := BuildConfig(opts)
	if err != nil {
		t.Fatalf("BuildConfig() error: %v", err)
	}
	if !strings.Contains(cfg, "require_mention: true") {
		t.Fatalf("config missing bool operator field: %s", cfg)
	}
	if _, err := Parse([]byte(cfg)); err != nil {
		t.Fatalf("generated matrix config is not parseable: %v", err)
	}
}

func TestBuildConfig_UnsupportedScaffoldChannel(t *testing.T) {
	opts := SetupOptions{
		Provider: "openai",
		APIKey:   "sk-test",
		BaseURL:  "https://api.openai.com/v1",
		Model:    "gpt-4o",
		Channels: []SetupChannelSelection{
			{ID: "webhook"},
		},
	}

	if _, err := BuildConfig(opts); err == nil {
		t.Fatal("expected error for unsupported scaffold channel")
	}
}

func TestBuildConfig_NoChannel(t *testing.T) {
	opts := SetupOptions{
		Provider: "openai",
		APIKey:   "sk-test",
		BaseURL:  "https://api.openai.com/v1",
		Model:    "gpt-4o",
	}

	cfg, err := BuildConfig(opts)
	if err != nil {
		t.Fatalf("BuildConfig() error: %v", err)
	}

	if strings.Contains(cfg, "channels:") {
		t.Error("config should not contain channels section when no channel selected")
	}
}

func TestBuildConfig_CustomAddress(t *testing.T) {
	opts := SetupOptions{
		Provider: "openai",
		APIKey:   "sk-test",
		BaseURL:  "https://api.openai.com/v1",
		Model:    "gpt-4o",
		Address:  "0.0.0.0:8080",
	}

	cfg, err := BuildConfig(opts)
	if err != nil {
		t.Fatalf("BuildConfig() error: %v", err)
	}

	if !strings.Contains(cfg, `address: "0.0.0.0:8080"`) {
		t.Error("config should use custom address")
	}
}

func TestBuildConfig_Parseable(t *testing.T) {
	// Verify the generated YAML can be parsed by config.Parse.
	opts := SetupOptions{
		Provider: "openai",
		APIKey:   "sk-test",
		BaseURL:  "https://api.openai.com/v1",
		Model:    "gpt-4o",
	}

	cfgStr, err := BuildConfig(opts)
	if err != nil {
		t.Fatalf("BuildConfig() error: %v", err)
	}

	if _, err := Parse([]byte(cfgStr)); err != nil {
		t.Fatalf("generated config is not parseable: %v", err)
	}
}

func TestBuildConfig_HonorsExplicitProviderAPIForUnknownProvider(t *testing.T) {
	opts := SetupOptions{
		Provider:    "remote-openai",
		ProviderAPI: "openai-completions",
		ProviderValues: map[string]string{
			"api_key":       "sk-remote-test",
			"base_url":      "https://remote.example.com/v1",
			"default_model": "gpt-4.1",
		},
	}

	cfgStr, err := BuildConfig(opts)
	if err != nil {
		t.Fatalf("BuildConfig() error: %v", err)
	}

	if !strings.Contains(cfgStr, "default_provider: remote-openai") {
		t.Fatalf("expected explicit provider name in config:\n%s", cfgStr)
	}
	if !strings.Contains(cfgStr, "api: openai-completions") {
		t.Fatalf("expected explicit provider api in config:\n%s", cfgStr)
	}
	if !strings.Contains(cfgStr, `base_url: "https://remote.example.com/v1"`) {
		t.Fatalf("expected explicit base_url in config:\n%s", cfgStr)
	}
	if !strings.Contains(cfgStr, `default_model: "gpt-4.1"`) {
		t.Fatalf("expected explicit default_model in config:\n%s", cfgStr)
	}
}

func TestBuildConfig_SupportsProviderNamesWithSlash(t *testing.T) {
	opts := SetupOptions{
		Provider:    "demo/copilot",
		ProviderAPI: "github-copilot",
		ProviderValues: map[string]string{
			"api_key":       "ghp_test",
			"default_model": "gpt-4o",
		},
	}

	cfgStr, err := BuildConfig(opts)
	if err != nil {
		t.Fatalf("BuildConfig() error: %v", err)
	}

	checks := []string{
		`default_model: "demo/copilot/gpt-4o"`,
		`default_provider: demo/copilot`,
		`demo/copilot:`,
		`api: github-copilot`,
	}
	for _, check := range checks {
		if !strings.Contains(cfgStr, check) {
			t.Fatalf("expected generated config to contain %q:\n%s", check, cfgStr)
		}
	}

	if _, err := Parse([]byte(cfgStr)); err != nil {
		t.Fatalf("generated plugin-style provider config is not parseable: %v\n%s", err, cfgStr)
	}
}

func TestDefaultModelsForProvider(t *testing.T) {
	tests := []struct {
		provider string
		wantLen  int
	}{
		{"openai", 5},
		{"anthropic", 3},
		{"google", 3},
		{"deepseek", 2},
		{"moonshot", 3},
		{"minimax", 2},
		{"xiaomi", 1},
		{"dashscope", 3},
		{"qianfan", 2},
		{"zai", 3},
		{"volcengine", 2},
		{"hunyuan", 2},
		{"ollama", 4},
		{"unknown", 0},
	}

	for _, tt := range tests {
		models := DefaultModelsForProvider(tt.provider)
		if len(models) != tt.wantLen {
			t.Errorf("DefaultModelsForProvider(%q) len = %d, want %d", tt.provider, len(models), tt.wantLen)
		}
	}
}

func TestDefaultBaseURL(t *testing.T) {
	tests := []struct {
		provider string
		want     string
	}{
		{"openai", "https://api.openai.com/v1"},
		{"anthropic", "https://api.anthropic.com"},
		{"deepseek", "https://api.deepseek.com/v1"},
		{"xiaomi", "https://api.xiaomimimo.com/anthropic"},
		{"dashscope", "https://dashscope.aliyuncs.com/compatible-mode/v1"},
		{"hunyuan", "https://api.hunyuan.cloud.tencent.com/anthropic"},
		{"ollama", "http://127.0.0.1:11434/v1"},
		{"unknown", ""},
	}

	for _, tt := range tests {
		got := DefaultBaseURL(tt.provider)
		if got != tt.want {
			t.Errorf("DefaultBaseURL(%q) = %q, want %q", tt.provider, got, tt.want)
		}
	}
}

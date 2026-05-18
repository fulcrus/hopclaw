package gateway

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/fulcrus/hopclaw/config"
	controloverlay "github.com/fulcrus/hopclaw/internal/controlplane/overlay"
	"gopkg.in/yaml.v3"
)

func TestHandleConfigPutSectionWritesFileWhenConfigBacked(t *testing.T) {
	t.Parallel()

	cfg := config.Config{
		Server: config.ServerConfig{AuthToken: "test-token"},
		Store:  config.StoreConfig{Backend: "memory"},
		Agent:  config.AgentConfig{DefaultModel: "gpt-4o", QueueMode: "enqueue"},
		Locale: "en",
	}
	cfg.ApplyDefaults()

	gw, configPath := newFileBackedTestGateway(t, cfg)
	rec := doRequest(t, gw.Handler(), "PUT", "/operator/config/locale", `"zh-CN"`)
	if rec.Code != 200 {
		t.Fatalf("PUT /operator/config/locale status = %d body=%s", rec.Code, rec.Body.String())
	}

	updated := loadConfigFileForTest(t, configPath)
	if updated.Locale != "zh-CN" {
		t.Fatalf("Locale = %q, want zh-CN", updated.Locale)
	}
}

func TestHandleConfigPutSectionRejectsTrailingJSONWhenConfigBacked(t *testing.T) {
	t.Parallel()

	cfg := config.Config{
		Server: config.ServerConfig{AuthToken: "test-token"},
		Store:  config.StoreConfig{Backend: "memory"},
		Agent:  config.AgentConfig{DefaultModel: "gpt-4o", QueueMode: "enqueue"},
		Locale: "en",
	}
	cfg.ApplyDefaults()

	gw, configPath := newFileBackedTestGateway(t, cfg)
	rec := doRequest(t, gw.Handler(), "PUT", "/operator/config/locale", `"zh-CN" {"extra":true}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("PUT /operator/config/locale status = %d body=%s", rec.Code, rec.Body.String())
	}

	updated := loadConfigFileForTest(t, configPath)
	if updated.Locale != "en" {
		t.Fatalf("Locale = %q, want en", updated.Locale)
	}
}

func TestHandleModelsCreateWritesProviderIntoConfigFile(t *testing.T) {
	t.Parallel()

	cfg := config.Config{
		Server: config.ServerConfig{AuthToken: "test-token"},
		Store:  config.StoreConfig{Backend: "memory"},
		Agent:  config.AgentConfig{DefaultModel: "gpt-4o", QueueMode: "enqueue"},
		Models: config.ModelsConfig{
			Providers: map[string]config.ProviderConfig{
				"openai": {API: "openai", APIKey: "secret"},
			},
		},
	}
	cfg.ApplyDefaults()

	gw, configPath := newFileBackedTestGateway(t, cfg)
	rec := doRequest(t, gw.Handler(), "POST", "/operator/models", `{
		"name": "anthropic",
		"api": "anthropic",
		"api_key": "k",
		"default_model": "claude-3-7-sonnet"
	}`)
	if rec.Code != 201 {
		t.Fatalf("POST /operator/models status = %d body=%s", rec.Code, rec.Body.String())
	}

	updated := loadConfigFileForTest(t, configPath)
	provider, ok := updated.Models.Providers["anthropic"]
	if !ok {
		t.Fatalf("provider anthropic missing from %#v", updated.Models.Providers)
	}
	if provider.API != "anthropic-messages" || provider.DefaultModel != "claude-3-7-sonnet" {
		t.Fatalf("provider = %#v", provider)
	}
}

func TestHandleModelsCreateRejectsTrailingJSONWhenConfigBacked(t *testing.T) {
	t.Parallel()

	cfg := config.Config{
		Server: config.ServerConfig{AuthToken: "test-token"},
		Store:  config.StoreConfig{Backend: "memory"},
		Agent:  config.AgentConfig{DefaultModel: "gpt-4o", QueueMode: "enqueue"},
	}
	cfg.ApplyDefaults()

	gw, configPath := newFileBackedTestGateway(t, cfg)
	rec := doRequest(t, gw.Handler(), "POST", "/operator/models", `{
		"name": "anthropic",
		"api_key": "k",
		"default_model": "claude-3-7-sonnet"
	} {"extra":true}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("POST /operator/models status = %d body=%s", rec.Code, rec.Body.String())
	}

	updated := loadConfigFileForTest(t, configPath)
	if _, ok := updated.Models.Providers["anthropic"]; ok {
		t.Fatalf("expected anthropic provider to be rejected from %#v", updated.Models.Providers)
	}
}

func TestHandleModelsCreateFillsKnownProviderAPIWhenOmitted(t *testing.T) {
	t.Parallel()

	cfg := config.Config{
		Server: config.ServerConfig{AuthToken: "test-token"},
		Store:  config.StoreConfig{Backend: "memory"},
		Agent:  config.AgentConfig{DefaultModel: "gpt-4o", QueueMode: "enqueue"},
	}
	cfg.ApplyDefaults()

	gw, configPath := newFileBackedTestGateway(t, cfg)
	rec := doRequest(t, gw.Handler(), "POST", "/operator/models", `{
		"name": "anthropic",
		"api_key": "k",
		"default_model": "claude-3-7-sonnet"
	}`)
	if rec.Code != 201 {
		t.Fatalf("POST /operator/models status = %d body=%s", rec.Code, rec.Body.String())
	}

	updated := loadConfigFileForTest(t, configPath)
	provider, ok := updated.Models.Providers["anthropic"]
	if !ok {
		t.Fatalf("provider anthropic missing from %#v", updated.Models.Providers)
	}
	if provider.API != "anthropic-messages" {
		t.Fatalf("provider.API = %q, want anthropic-messages", provider.API)
	}
}

func TestHandleModelsCreateWritesBedrockProviderIntoConfigFile(t *testing.T) {
	t.Parallel()

	cfg := config.Config{
		Server: config.ServerConfig{AuthToken: "test-token"},
		Store:  config.StoreConfig{Backend: "memory"},
		Agent:  config.AgentConfig{DefaultModel: "gpt-4o", QueueMode: "enqueue"},
	}
	cfg.ApplyDefaults()

	gw, configPath := newFileBackedTestGateway(t, cfg)
	rec := doRequest(t, gw.Handler(), "POST", "/operator/models", `{
		"name": "amazon-bedrock",
		"api": "bedrock",
		"region": "us-east-1",
		"access_key_id": "AKIA_TEST",
		"secret_key": "secret",
		"session_token": "session",
		"default_model": "anthropic.claude-3-5-sonnet-20241022-v2:0"
	}`)
	if rec.Code != 201 {
		t.Fatalf("POST /operator/models status = %d body=%s", rec.Code, rec.Body.String())
	}

	updated := loadConfigFileForTest(t, configPath)
	provider, ok := updated.Models.Providers["amazon-bedrock"]
	if !ok {
		t.Fatalf("provider amazon-bedrock missing from %#v", updated.Models.Providers)
	}
	if provider.API != "bedrock-converse" ||
		provider.Region != "us-east-1" ||
		provider.AccessKeyID != "AKIA_TEST" ||
		provider.SecretKey != "secret" ||
		provider.SessionToken != "session" ||
		provider.DefaultModel != "anthropic.claude-3-5-sonnet-20241022-v2:0" {
		t.Fatalf("provider = %#v", provider)
	}
}

func TestHandleModelsCreateWritesProviderAdvancedFieldsIntoConfigFile(t *testing.T) {
	t.Parallel()

	cfg := config.Config{
		Server: config.ServerConfig{AuthToken: "test-token"},
		Store:  config.StoreConfig{Backend: "memory"},
		Agent:  config.AgentConfig{DefaultModel: "gpt-4o", QueueMode: "enqueue"},
	}
	cfg.ApplyDefaults()

	gw, configPath := newFileBackedTestGateway(t, cfg)
	rec := doRequest(t, gw.Handler(), "POST", "/operator/models", `{
		"name": "openai",
		"api_keys": [" primary-key ", "", "backup-key"],
		"timeout": "45s",
		"headers": {
			" authorization ": " Bearer demo ",
			"x-trace-id": " trace-123 "
		},
		"default_model": "gpt-4o"
	}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("POST /operator/models status = %d body=%s", rec.Code, rec.Body.String())
	}

	updated := loadConfigFileForTest(t, configPath)
	provider, ok := updated.Models.Providers["openai"]
	if !ok {
		t.Fatalf("provider openai missing from %#v", updated.Models.Providers)
	}
	if provider.Timeout != 45*time.Second {
		t.Fatalf("provider.Timeout = %s, want 45s", provider.Timeout)
	}
	if len(provider.APIKeys) != 2 || provider.APIKeys[0] != "primary-key" || provider.APIKeys[1] != "backup-key" {
		t.Fatalf("provider.APIKeys = %#v", provider.APIKeys)
	}
	if got := provider.Headers["Authorization"]; got != "Bearer demo" {
		t.Fatalf("Authorization = %q, want %q", got, "Bearer demo")
	}
	if got := provider.Headers["X-Trace-Id"]; got != "trace-123" {
		t.Fatalf("X-Trace-Id = %q, want %q", got, "trace-123")
	}

	providerNode := loadProviderConfigNodeForTest(t, configPath, "openai")
	if got := providerNode["timeout"]; got != "45s" {
		t.Fatalf("provider timeout payload = %#v, want 45s", got)
	}
	if _, exists := providerNode["api_key"]; exists {
		t.Fatalf("did not expect empty api_key field in provider payload, got %#v", providerNode)
	}
}

func TestHandleModelsUpdateMergesProviderIntoConfigFile(t *testing.T) {
	t.Parallel()

	cfg := config.Config{
		Server: config.ServerConfig{AuthToken: "test-token"},
		Store:  config.StoreConfig{Backend: "memory"},
		Agent:  config.AgentConfig{DefaultModel: "gpt-4o", QueueMode: "enqueue"},
		Models: config.ModelsConfig{
			Providers: map[string]config.ProviderConfig{
				"anthropic": {
					BaseURL:      "https://api.anthropic.com",
					APIKey:       "secret",
					DefaultModel: "claude-3-5-sonnet",
				},
			},
		},
	}
	cfg.ApplyDefaults()

	gw, configPath := newFileBackedTestGateway(t, cfg)
	rec := doRequest(t, gw.Handler(), "PUT", "/operator/models/anthropic", `{
		"default_model": "claude-3-7-sonnet"
	}`)
	if rec.Code != 200 {
		t.Fatalf("PUT /operator/models/anthropic status = %d body=%s", rec.Code, rec.Body.String())
	}

	updated := loadConfigFileForTest(t, configPath)
	provider, ok := updated.Models.Providers["anthropic"]
	if !ok {
		t.Fatalf("provider anthropic missing from %#v", updated.Models.Providers)
	}
	if provider.API != "anthropic-messages" {
		t.Fatalf("provider.API = %q, want anthropic-messages", provider.API)
	}
	if provider.BaseURL != "https://api.anthropic.com" {
		t.Fatalf("provider.BaseURL = %q, want https://api.anthropic.com", provider.BaseURL)
	}
	if provider.APIKey != "secret" {
		t.Fatalf("provider.APIKey = %q, want secret", provider.APIKey)
	}
	if provider.DefaultModel != "claude-3-7-sonnet" {
		t.Fatalf("provider.DefaultModel = %q, want claude-3-7-sonnet", provider.DefaultModel)
	}
}

func TestHandleModelsUpdateCanReplaceAndClearProviderAdvancedFields(t *testing.T) {
	t.Parallel()

	cfg := config.Config{
		Server: config.ServerConfig{AuthToken: "test-token"},
		Store:  config.StoreConfig{Backend: "memory"},
		Agent:  config.AgentConfig{DefaultModel: "gpt-4o", QueueMode: "enqueue"},
		Models: config.ModelsConfig{
			Providers: map[string]config.ProviderConfig{
				"openai": {
					API:          "openai-completions",
					BaseURL:      "https://api.openai.com/v1",
					APIKey:       "legacy-key",
					APIKeys:      []string{"old-a", "old-b"},
					DefaultModel: "gpt-4o",
					Timeout:      30 * time.Second,
					Headers: map[string]string{
						"Authorization": "Bearer old",
						"X-Old":         "legacy",
					},
				},
			},
		},
	}
	cfg.ApplyDefaults()

	gw, configPath := newFileBackedTestGateway(t, cfg)
	rec := doRequest(t, gw.Handler(), "PUT", "/operator/models/openai", `{
		"api_key": "",
		"api_keys": [" next-key "],
		"base_url": "",
		"timeout": "",
		"headers": {
			"x-trace-id": " trace-456 "
		}
	}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT /operator/models/openai status = %d body=%s", rec.Code, rec.Body.String())
	}

	updated := loadConfigFileForTest(t, configPath)
	provider, ok := updated.Models.Providers["openai"]
	if !ok {
		t.Fatalf("provider openai missing from %#v", updated.Models.Providers)
	}
	if provider.APIKey != "" {
		t.Fatalf("provider.APIKey = %q, want empty", provider.APIKey)
	}
	if len(provider.APIKeys) != 1 || provider.APIKeys[0] != "next-key" {
		t.Fatalf("provider.APIKeys = %#v", provider.APIKeys)
	}
	if provider.BaseURL != "" {
		t.Fatalf("provider.BaseURL = %q, want empty", provider.BaseURL)
	}
	if provider.Timeout != 0 {
		t.Fatalf("provider.Timeout = %s, want 0", provider.Timeout)
	}
	if len(provider.Headers) != 1 {
		t.Fatalf("provider.Headers = %#v", provider.Headers)
	}
	if got := provider.Headers["X-Trace-Id"]; got != "trace-456" {
		t.Fatalf("X-Trace-Id = %q, want %q", got, "trace-456")
	}
	if _, exists := provider.Headers["Authorization"]; exists {
		t.Fatalf("expected old Authorization header to be replaced, got %#v", provider.Headers)
	}

	providerNode := loadProviderConfigNodeForTest(t, configPath, "openai")
	if _, exists := providerNode["base_url"]; exists {
		t.Fatalf("expected cleared base_url to be omitted from provider payload, got %#v", providerNode)
	}
	if _, exists := providerNode["api_key"]; exists {
		t.Fatalf("expected cleared api_key to be omitted from provider payload, got %#v", providerNode)
	}
	if _, exists := providerNode["timeout"]; exists {
		t.Fatalf("expected cleared timeout to be omitted from provider payload, got %#v", providerNode)
	}
	headers, _ := providerNode["headers"].(map[string]any)
	if _, exists := headers["Authorization"]; exists {
		t.Fatalf("expected replaced headers in provider payload, got %#v", headers)
	}
}

func TestHandleModelsUpdateRejectsBodyNameMismatch(t *testing.T) {
	t.Parallel()

	cfg := config.Config{
		Server: config.ServerConfig{AuthToken: "test-token"},
		Store:  config.StoreConfig{Backend: "memory"},
		Agent:  config.AgentConfig{DefaultModel: "gpt-4o", QueueMode: "enqueue"},
		Models: config.ModelsConfig{
			Providers: map[string]config.ProviderConfig{
				"anthropic": {
					APIKey:       "secret",
					DefaultModel: "claude-3-5-sonnet",
				},
			},
		},
	}
	cfg.ApplyDefaults()

	gw, configPath := newFileBackedTestGateway(t, cfg)
	rec := doRequest(t, gw.Handler(), "PUT", "/operator/models/anthropic", `{
		"name": "openai",
		"default_model": "claude-3-7-sonnet"
	}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("PUT /operator/models/anthropic status = %d body=%s", rec.Code, rec.Body.String())
	}

	updated := loadConfigFileForTest(t, configPath)
	provider, ok := updated.Models.Providers["anthropic"]
	if !ok {
		t.Fatalf("provider anthropic missing from %#v", updated.Models.Providers)
	}
	if provider.DefaultModel != "claude-3-5-sonnet" {
		t.Fatalf("provider.DefaultModel = %q, want claude-3-5-sonnet", provider.DefaultModel)
	}
	if _, ok := updated.Models.Providers["openai"]; ok {
		t.Fatalf("unexpected openai provider in %#v", updated.Models.Providers)
	}
}

func TestHandleModelsCreateRejectsInvalidTimeout(t *testing.T) {
	t.Parallel()

	cfg := config.Config{
		Server: config.ServerConfig{AuthToken: "test-token"},
		Store:  config.StoreConfig{Backend: "memory"},
		Agent:  config.AgentConfig{DefaultModel: "gpt-4o", QueueMode: "enqueue"},
	}
	cfg.ApplyDefaults()

	gw, configPath := newFileBackedTestGateway(t, cfg)
	rec := doRequest(t, gw.Handler(), "POST", "/operator/models", `{
		"name": "openai",
		"timeout": "definitely-not-a-duration"
	}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("POST /operator/models status = %d body=%s", rec.Code, rec.Body.String())
	}

	updated := loadConfigFileForTest(t, configPath)
	if _, ok := updated.Models.Providers["openai"]; ok {
		t.Fatalf("expected openai provider to be rejected from %#v", updated.Models.Providers)
	}
}

func TestHandleModelsDeleteRemovesProviderFromConfigFile(t *testing.T) {
	t.Parallel()

	cfg := config.Config{
		Server: config.ServerConfig{AuthToken: "test-token"},
		Store:  config.StoreConfig{Backend: "memory"},
		Agent:  config.AgentConfig{DefaultModel: "gpt-4o", QueueMode: "enqueue"},
		Models: config.ModelsConfig{
			Providers: map[string]config.ProviderConfig{
				"openai": {
					API:          "openai-completions",
					APIKey:       "sk-openai",
					DefaultModel: "gpt-4o",
				},
				"anthropic": {
					API:          "anthropic-messages",
					APIKey:       "sk-anthropic",
					DefaultModel: "claude-3-7-sonnet",
				},
			},
		},
	}
	cfg.ApplyDefaults()

	gw, configPath := newFileBackedTestGateway(t, cfg)
	rec := doRequest(t, gw.Handler(), "DELETE", "/operator/models/anthropic", "")
	if rec.Code != 200 {
		t.Fatalf("DELETE /operator/models/anthropic status = %d body=%s", rec.Code, rec.Body.String())
	}

	updated := loadConfigFileForTest(t, configPath)
	if _, ok := updated.Models.Providers["anthropic"]; ok {
		t.Fatalf("expected anthropic provider to be removed from %#v", updated.Models.Providers)
	}
	if _, ok := updated.Models.Providers["openai"]; !ok {
		t.Fatalf("expected openai provider to remain in %#v", updated.Models.Providers)
	}
}

func TestHandleChannelsCreateWritesChannelIntoConfigFile(t *testing.T) {
	t.Parallel()

	cfg := config.Config{
		Server: config.ServerConfig{AuthToken: "test-token"},
		Store:  config.StoreConfig{Backend: "memory"},
		Agent:  config.AgentConfig{DefaultModel: "gpt-4o", QueueMode: "enqueue"},
	}
	cfg.ApplyDefaults()

	gw, configPath := newFileBackedTestGateway(t, cfg)
	rec := doRequest(t, gw.Handler(), "POST", "/operator/channels", `{
		"name": "slack",
		"config": {
			"enabled": true,
			"bot_token": "x",
			"app_token": "y"
		}
	}`)
	if rec.Code != 201 {
		t.Fatalf("POST /operator/channels status = %d body=%s", rec.Code, rec.Body.String())
	}

	updated := loadConfigFileForTest(t, configPath)
	if updated.Channels.Slack.BotToken != "x" || updated.Channels.Slack.AppToken != "y" {
		t.Fatalf("slack config = %#v", updated.Channels.Slack)
	}
}

func TestHandleChannelsCreateCanonicalizesNameAndStripsTypeFromConfigFile(t *testing.T) {
	t.Parallel()

	cfg := config.Config{
		Server: config.ServerConfig{AuthToken: "test-token"},
		Store:  config.StoreConfig{Backend: "memory"},
		Agent:  config.AgentConfig{DefaultModel: "gpt-4o", QueueMode: "enqueue"},
	}
	cfg.ApplyDefaults()

	gw, configPath := newFileBackedTestGateway(t, cfg)
	rec := doRequest(t, gw.Handler(), "POST", "/operator/channels", `{
		"name": "Slack",
		"config": {
			"type": "slack",
			"bot_token": "x",
			"app_token": "y"
		}
	}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("POST /operator/channels status = %d body=%s", rec.Code, rec.Body.String())
	}

	updated := loadConfigFileForTest(t, configPath)
	if updated.Channels.Slack.BotToken != "x" || updated.Channels.Slack.AppToken != "y" {
		t.Fatalf("slack config = %#v", updated.Channels.Slack)
	}

	fileText := readConfigFileTextForTest(t, configPath)
	if strings.Contains(fileText, "\n  Slack:\n") {
		t.Fatalf("config file should use canonical slack key, got:\n%s", fileText)
	}
	if strings.Contains(fileText, "\n    type:") {
		t.Fatalf("config file should not persist redundant channel type, got:\n%s", fileText)
	}
}

func TestHandleChannelsCreateRejectsMismatchedFileBackedType(t *testing.T) {
	t.Parallel()

	cfg := config.Config{
		Server: config.ServerConfig{AuthToken: "test-token"},
		Store:  config.StoreConfig{Backend: "memory"},
		Agent:  config.AgentConfig{DefaultModel: "gpt-4o", QueueMode: "enqueue"},
	}
	cfg.ApplyDefaults()

	gw, configPath := newFileBackedTestGateway(t, cfg)
	rec := doRequest(t, gw.Handler(), "POST", "/operator/channels", `{
		"name": "slack",
		"config": {
			"type": "discord",
			"bot_token": "x"
		}
	}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("POST /operator/channels mismatch status = %d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `must match config.type \"discord\"`) {
		t.Fatalf("unexpected mismatch response: %s", rec.Body.String())
	}

	updated := loadConfigFileForTest(t, configPath)
	if updated.Channels.Slack.BotToken != "" {
		t.Fatalf("slack config should remain empty after mismatch: %#v", updated.Channels.Slack)
	}
}

func TestHandleChannelsCreateRejectsTrailingJSONWhenConfigBacked(t *testing.T) {
	t.Parallel()

	cfg := config.Config{
		Server: config.ServerConfig{AuthToken: "test-token"},
		Store:  config.StoreConfig{Backend: "memory"},
		Agent:  config.AgentConfig{DefaultModel: "gpt-4o", QueueMode: "enqueue"},
	}
	cfg.ApplyDefaults()

	gw, configPath := newFileBackedTestGateway(t, cfg)
	rec := doRequest(t, gw.Handler(), "POST", "/operator/channels", `{"name":"slack","config":{"bot_token":"x"}} {"extra":true}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("POST /operator/channels trailing json status = %d body=%s", rec.Code, rec.Body.String())
	}

	updated := loadConfigFileForTest(t, configPath)
	if updated.Channels.Slack.BotToken != "" {
		t.Fatalf("slack config should remain empty after trailing json: %#v", updated.Channels.Slack)
	}
}

func TestHandleChannelsUpdateRejectsTrailingJSONWhenConfigBacked(t *testing.T) {
	t.Parallel()

	cfg := config.Config{
		Server: config.ServerConfig{AuthToken: "test-token"},
		Store:  config.StoreConfig{Backend: "memory"},
		Agent:  config.AgentConfig{DefaultModel: "gpt-4o", QueueMode: "enqueue"},
		Channels: config.ChannelsConfig{
			Slack: config.SlackChannelConfig{
				BotToken: "old-token",
			},
		},
	}
	cfg.ApplyDefaults()

	gw, configPath := newFileBackedTestGateway(t, cfg)
	rec := doRequest(t, gw.Handler(), "PUT", "/operator/channels/slack", `{"config":{"bot_token":"new-token"}} {"extra":true}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("PUT /operator/channels/slack trailing json status = %d body=%s", rec.Code, rec.Body.String())
	}

	updated := loadConfigFileForTest(t, configPath)
	if updated.Channels.Slack.BotToken != "old-token" {
		t.Fatalf("slack config should remain unchanged after trailing json: %#v", updated.Channels.Slack)
	}
}

func newFileBackedTestGateway(t *testing.T, cfg config.Config) (*Gateway, string) {
	t.Helper()

	configPath := filepath.Join(t.TempDir(), "config.yaml")
	data, err := yaml.Marshal(cfg)
	if err != nil {
		t.Fatalf("yaml.Marshal() error = %v", err)
	}
	if err := os.WriteFile(configPath, data, 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	resolver, err := controloverlay.NewResolver(context.Background(), cfg, nil, controloverlay.Options{})
	if err != nil {
		t.Fatalf("NewResolver() error = %v", err)
	}

	gw := newTestGatewayFull(t)
	gw.SetConfigWatcher(nil, configPath)
	gw.SetEffectiveConfigResolver(resolver)
	return gw, configPath
}

func loadConfigFileForTest(t *testing.T, path string) config.Config {
	t.Helper()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	var cfg config.Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("yaml.Unmarshal() error = %v", err)
	}
	return cfg
}

func readConfigFileTextForTest(t *testing.T, path string) string {
	t.Helper()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	return string(data)
}

func loadProviderConfigNodeForTest(t *testing.T, path, name string) map[string]any {
	t.Helper()

	var root map[string]any
	if err := yaml.Unmarshal([]byte(readConfigFileTextForTest(t, path)), &root); err != nil {
		t.Fatalf("yaml.Unmarshal(root) error = %v", err)
	}
	modelsNode, _ := root["models"].(map[string]any)
	providersNode, _ := modelsNode["providers"].(map[string]any)
	providerNode, _ := providersNode[name].(map[string]any)
	if providerNode == nil {
		t.Fatalf("provider %q payload missing from %#v", name, providersNode)
	}
	return providerNode
}

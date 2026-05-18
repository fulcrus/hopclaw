package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/fulcrus/hopclaw/config"
	controloverlay "github.com/fulcrus/hopclaw/internal/controlplane/overlay"
	"github.com/fulcrus/hopclaw/internal/modules"
	"github.com/fulcrus/hopclaw/model"
	"github.com/fulcrus/hopclaw/plugin"
)

func TestHandleSetupCatalogReturnsRegistryBackedMetadata(t *testing.T) {
	gw := newTestGatewayFull(t)

	rec := doRequest(t, gw.Handler(), "GET", "/operator/setup/catalog", "")
	if rec.Code != 200 {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}

	var resp config.OperatorSetupCatalog
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if resp.DefaultAddress != config.DefaultGatewayAddress {
		t.Fatalf("DefaultAddress = %q, want %q", resp.DefaultAddress, config.DefaultGatewayAddress)
	}
	if len(resp.AuthModes) == 0 {
		t.Fatal("expected auth mode catalog")
	}
	if len(resp.Providers) == 0 {
		t.Fatal("expected provider catalog")
	}
	if len(resp.ProviderAPIs) == 0 {
		t.Fatal("expected provider api catalog")
	}
	if len(resp.Channels) == 0 {
		t.Fatal("expected channel catalog")
	}

	foundBearer := false
	for _, item := range resp.AuthModes {
		if item.ID == "bearer" {
			foundBearer = true
			if !item.Recommended {
				t.Fatal("expected bearer auth mode to be marked recommended")
			}
		}
	}
	if !foundBearer {
		t.Fatal("expected bearer auth mode in setup catalog")
	}

	foundOpenAI := false
	foundBedrockProvider := false
	for _, item := range resp.Providers {
		if item.ID == "openai" {
			foundOpenAI = true
			if item.DisplayName == "" {
				t.Fatal("expected provider display name")
			}
			if item.API != "openai-completions" {
				t.Fatalf("openai api = %q, want openai-completions", item.API)
			}
			if item.BaseURL != "https://api.openai.com/v1" {
				t.Fatalf("openai base_url = %q, want https://api.openai.com/v1", item.BaseURL)
			}
			if len(item.DefaultModels) == 0 || item.DefaultModels[0] != "gpt-4o" {
				t.Fatalf("openai default_models = %#v", item.DefaultModels)
			}
			if item.APIKeyHint == "" {
				t.Fatal("expected provider api key hint")
			}
			if item.CapabilityMatrix.ProviderAPI != model.APIOpenAICompletions || !item.CapabilityMatrix.SupportsTools {
				t.Fatalf("unexpected openai capability matrix: %+v", item.CapabilityMatrix)
			}
		}
		if item.ID == "amazon-bedrock" {
			foundBedrockProvider = true
			if item.API != "bedrock-converse" {
				t.Fatalf("bedrock provider api = %q, want bedrock-converse", item.API)
			}
			if len(item.DefaultModels) == 0 || item.DefaultModels[0] != "anthropic.claude-3-5-sonnet-20241022-v2:0" {
				t.Fatalf("bedrock provider default_models = %#v", item.DefaultModels)
			}
		}
	}
	if !foundOpenAI {
		t.Fatal("expected openai provider in setup catalog")
	}
	if !foundBedrockProvider {
		t.Fatal("expected amazon-bedrock provider in setup catalog")
	}

	foundBedrockAPI := false
	foundOpenAIAPI := false
	for _, item := range resp.ProviderAPIs {
		if item.ID == "openai-completions" {
			foundOpenAIAPI = true
			timeoutFound := false
			for _, field := range item.Fields {
				if field.ID != "timeout" {
					continue
				}
				timeoutFound = true
				if field.Type != "duration" || !field.Advanced {
					t.Fatalf("unexpected openai timeout field: %+v", field)
				}
			}
			if !timeoutFound {
				t.Fatal("expected openai api profile to expose timeout field")
			}
		}
		if item.ID == "bedrock-converse" {
			foundBedrockAPI = true
			if len(item.Fields) == 0 {
				t.Fatal("expected bedrock api profile fields")
			}
			if item.CapabilityMatrix.ProviderAPI != model.APIBedrockConverse || !item.CapabilityMatrix.SupportsStreaming {
				t.Fatalf("unexpected bedrock api capability matrix: %+v", item.CapabilityMatrix)
			}
		}
	}
	if !foundBedrockAPI {
		t.Fatal("expected bedrock provider api profile in setup catalog")
	}
	if !foundOpenAIAPI {
		t.Fatal("expected openai-completions provider api profile in setup catalog")
	}

	foundSlack := false
	for _, item := range resp.Channels {
		if item.ID == "slack" {
			foundSlack = true
			if !item.SetupSupported {
				t.Fatal("expected slack channel to be setup supported")
			}
			if len(item.OperatorFields) == 0 {
				t.Fatal("expected slack operator fields in setup catalog")
			}
			foundDMPolicy := false
			for _, field := range item.OperatorFields {
				if field.ID != "dm_policy" {
					continue
				}
				foundDMPolicy = true
				if field.Placeholder == "" {
					t.Fatal("expected slack dm_policy placeholder")
				}
			}
			if !foundDMPolicy {
				t.Fatal("expected slack dm_policy operator field")
			}
		}
	}
	if !foundSlack {
		t.Fatal("expected slack channel in setup catalog")
	}
}

func TestHandleSetupCatalogIncludesPluginProvidersWithKnownAPIProfiles(t *testing.T) {
	t.Parallel()

	gw := newTestGatewayFull(t)
	manager := plugin.NewManager()
	if err := manager.Register(plugin.LoadedPlugin{
		Manifest: plugin.Manifest{
			Name: "demo",
			Providers: map[string]plugin.ProviderDecl{
				"copilot": {
					API:          "github-copilot",
					BaseURL:      "https://copilot-proxy.example.test",
					DefaultModel: "gpt-4o",
					EnvVars:      []string{"GITHUB_TOKEN"},
					APIKeyHint:   "Use a GitHub token or GITHUB_TOKEN.",
				},
				"unknown": {
					API: "plugin-private-api",
				},
			},
		},
		Dir: t.TempDir(),
	}); err != nil {
		t.Fatalf("manager.Register() error = %v", err)
	}
	gw.SetModuleCatalog(modules.NewStore(modules.BuildCatalog(manager.Modules())))

	rec := doRequest(t, gw.Handler(), "GET", "/operator/setup/catalog", "")
	if rec.Code != 200 {
		t.Fatalf("GET /operator/setup/catalog status = %d body=%s", rec.Code, rec.Body.String())
	}

	var resp config.OperatorSetupCatalog
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}

	foundPluginProvider := false
	for _, item := range resp.Providers {
		if item.ID != "demo/copilot" {
			continue
		}
		foundPluginProvider = true
		if item.API != "github-copilot" {
			t.Fatalf("plugin provider api = %q, want github-copilot", item.API)
		}
		if item.BaseURL != "https://copilot-proxy.example.test" {
			t.Fatalf("plugin provider base_url = %q", item.BaseURL)
		}
		if len(item.DefaultModels) != 1 || item.DefaultModels[0] != "gpt-4o" {
			t.Fatalf("plugin provider default_models = %#v", item.DefaultModels)
		}
		if len(item.EnvVars) != 1 || item.EnvVars[0] != "GITHUB_TOKEN" {
			t.Fatalf("plugin provider env_vars = %#v", item.EnvVars)
		}
		if item.APIKeyHint != "Use a GitHub token or GITHUB_TOKEN." {
			t.Fatalf("plugin provider api_key_hint = %q", item.APIKeyHint)
		}
		if item.CapabilityMatrix.ProviderAPI != model.APIGitHubCopilot {
			t.Fatalf("plugin capability matrix = %+v", item.CapabilityMatrix)
		}
	}
	if !foundPluginProvider {
		t.Fatalf("expected plugin provider in setup catalog: %#v", resp.Providers)
	}
	for _, item := range resp.Providers {
		if item.ID == "demo/unknown" {
			t.Fatalf("unexpected plugin provider with unsupported api in setup catalog: %#v", item)
		}
	}
}

func TestHandleSetupStatusDetectsPluginProviderEnvVars(t *testing.T) {
	t.Setenv("HOPCLAW_PLUGIN_API_KEY", "plugin-secret")

	gw := newTestGatewayFull(t)
	manager := plugin.NewManager()
	if err := manager.Register(plugin.LoadedPlugin{
		Manifest: plugin.Manifest{
			Name: "demo",
			Providers: map[string]plugin.ProviderDecl{
				"plugin-openai": {
					API:        "openai-completions",
					BaseURL:    "https://api.plugin.example/v1",
					EnvVars:    []string{"HOPCLAW_PLUGIN_API_KEY"},
					APIKeyHint: "Enter the plugin openai API key. Common env var: HOPCLAW_PLUGIN_API_KEY",
				},
			},
		},
		Dir: t.TempDir(),
	}); err != nil {
		t.Fatalf("manager.Register() error = %v", err)
	}
	gw.SetModuleCatalog(modules.NewStore(modules.BuildCatalog(manager.Modules())))

	rec := doRequest(t, gw.Handler(), "GET", "/operator/setup/status", "")
	if rec.Code != 200 {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}

	var resp setupStatusResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if len(resp.DetectedProviders) != 1 {
		t.Fatalf("detected providers = %#v", resp.DetectedProviders)
	}
	if resp.DetectedProviders[0].ID != "demo/plugin-openai" {
		t.Fatalf("detected provider id = %q", resp.DetectedProviders[0].ID)
	}
	if resp.DetectedProviders[0].Name != "demo/plugin-openai" {
		t.Fatalf("detected provider name = %q", resp.DetectedProviders[0].Name)
	}
	if len(resp.DetectedEnvKeys) != 1 || resp.DetectedEnvKeys[0].Provider != "demo/plugin-openai" {
		t.Fatalf("detected env keys = %#v", resp.DetectedEnvKeys)
	}
}

func TestHandleSetupCatalogMergesPluginProvidersWithBuiltInCatalogEntries(t *testing.T) {
	t.Parallel()

	gw := newTestGatewayFull(t)
	manager := plugin.NewManager()
	if err := manager.Register(plugin.LoadedPlugin{
		Manifest: plugin.Manifest{
			Name:   "openai",
			Format: plugin.ManifestFormatOpenClawJSON,
			Providers: map[string]plugin.ProviderDecl{
				"openai": {
					API:              "openai-completions",
					BaseURL:          "https://api.openai.com/v1",
					DefaultModel:     "gpt-4o",
					EnvVars:          []string{"OPENAI_API_KEY", "OPENAI_FALLBACK_KEY"},
					APIKeyHint:       "Plugin-specific OpenAI key hint",
					PreferUnscopedID: true,
				},
			},
		},
		Dir: t.TempDir(),
	}); err != nil {
		t.Fatalf("manager.Register() error = %v", err)
	}
	gw.SetModuleCatalog(modules.NewStore(modules.BuildCatalog(manager.Modules())))

	rec := doRequest(t, gw.Handler(), "GET", "/operator/setup/catalog", "")
	if rec.Code != 200 {
		t.Fatalf("GET /operator/setup/catalog status = %d body=%s", rec.Code, rec.Body.String())
	}

	var resp config.OperatorSetupCatalog
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}

	count := 0
	var openAI config.SetupProviderProfile
	for _, item := range resp.Providers {
		if item.ID != "openai" {
			continue
		}
		count++
		openAI = item
	}
	if count != 1 {
		t.Fatalf("expected exactly one openai provider entry, got %d in %#v", count, resp.Providers)
	}
	if len(openAI.EnvVars) != 2 || openAI.EnvVars[0] != "OPENAI_API_KEY" || openAI.EnvVars[1] != "OPENAI_FALLBACK_KEY" {
		t.Fatalf("openai env vars = %#v", openAI.EnvVars)
	}
	if openAI.APIKeyHint != "Enter your OpenAI API key (sk-...)" {
		t.Fatalf("openai api key hint = %q", openAI.APIKeyHint)
	}
}

func TestHandleSetupStatusIncludesDetectedProvidersAndOpenAICompat(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "sk-test")
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("GOOGLE_API_KEY", "")

	gw := newTestGatewayFull(t)
	cfg := config.Config{
		Models: config.ModelsConfig{
			OpenAICompat: config.OpenAICompatConfig{
				BaseURL: "https://api.openai.com/v1",
				APIKey:  "sk-configured",
				Model:   "gpt-4o",
			},
		},
	}
	cfg.ApplyDefaults()
	resolver, err := controloverlay.NewResolver(context.Background(), cfg, nil, controloverlay.Options{})
	if err != nil {
		t.Fatalf("NewResolver() error = %v", err)
	}
	gw.SetEffectiveConfigResolver(resolver)

	rec := doRequest(t, gw.Handler(), "GET", "/operator/setup/status", "")
	if rec.Code != 200 {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}

	var resp setupStatusResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if !resp.Configured {
		t.Fatal("expected configured=true")
	}
	if len(resp.Providers) != 1 || resp.Providers[0] != "openai" {
		t.Fatalf("providers = %#v", resp.Providers)
	}
	if len(resp.DetectedProviders) != 1 {
		t.Fatalf("detected providers = %#v", resp.DetectedProviders)
	}
	if resp.DetectedProviders[0].ID != "openai" {
		t.Fatalf("detected provider = %#v", resp.DetectedProviders[0])
	}
	if resp.DetectedProviders[0].MaskedKey == "" {
		t.Fatal("expected masked key in detected provider payload")
	}
	if len(resp.DetectedEnvKeys) != 1 || resp.DetectedEnvKeys[0].Provider != "openai" {
		t.Fatalf("detected env keys = %#v", resp.DetectedEnvKeys)
	}
}

func TestBuildTempRegistryUsesCatalogDefaults(t *testing.T) {
	t.Parallel()

	registry, err := buildTempRegistry(providerConnectionInput{
		Provider: "deepseek",
		APIKey:   "sk-test",
	})
	if err != nil {
		t.Fatalf("buildTempRegistry() error = %v", err)
	}
	if names := registry.ProviderNames(); len(names) != 1 || names[0] != "deepseek" {
		t.Fatalf("ProviderNames() = %#v", names)
	}
}

func TestBuildTempRegistrySupportsBedrockCredentials(t *testing.T) {
	t.Parallel()

	registry, err := buildTempRegistry(providerConnectionInput{
		Provider:     "amazon-bedrock",
		Region:       "us-east-1",
		AccessKeyID:  "AKIA_TEST",
		SecretKey:    "secret",
		DefaultModel: "anthropic.claude-3-5-sonnet-20241022-v2:0",
	})
	if err != nil {
		t.Fatalf("buildTempRegistry() error = %v", err)
	}
	if names := registry.ProviderNames(); len(names) != 1 || names[0] != "amazon-bedrock" {
		t.Fatalf("ProviderNames() = %#v", names)
	}
}

func TestProviderEntryFromConnectionInputSupportsAdvancedFields(t *testing.T) {
	t.Parallel()

	entry, err := providerEntryFromConnectionInput(providerConnectionInput{
		Provider: "openai",
		APIKeys:  []string{" primary-key ", "", "backup-key"},
		Timeout:  "45s",
		Headers: map[string]string{
			" authorization ": " Bearer demo ",
			"x-trace-id":      " trace-123 ",
		},
	})
	if err != nil {
		t.Fatalf("providerEntryFromConnectionInput() error = %v", err)
	}
	if len(entry.APIKeys) != 2 || entry.APIKeys[0] != "primary-key" || entry.APIKeys[1] != "backup-key" {
		t.Fatalf("entry.APIKeys = %#v", entry.APIKeys)
	}
	if entry.Timeout != 45*time.Second {
		t.Fatalf("entry.Timeout = %s, want 45s", entry.Timeout)
	}
	if got := entry.Headers["Authorization"]; got != "Bearer demo" {
		t.Fatalf("Authorization = %q, want %q", got, "Bearer demo")
	}
	if got := entry.Headers["X-Trace-Id"]; got != "trace-123" {
		t.Fatalf("X-Trace-Id = %q, want %q", got, "trace-123")
	}
}

func TestProviderEntryFromConnectionInputUsesCatalogProviderDefaults(t *testing.T) {
	t.Parallel()

	entry, err := providerEntryFromConnectionInput(providerConnectionInput{
		Provider:        "team-openai",
		CatalogProvider: "deepseek",
	})
	if err != nil {
		t.Fatalf("providerEntryFromConnectionInput() error = %v", err)
	}
	if entry.API != model.APIOpenAICompletions {
		t.Fatalf("entry.API = %q, want %q", entry.API, model.APIOpenAICompletions)
	}
	if entry.BaseURL != "https://api.deepseek.com/v1" {
		t.Fatalf("entry.BaseURL = %q, want %q", entry.BaseURL, "https://api.deepseek.com/v1")
	}
	if entry.DefaultModel != "deepseek-chat" {
		t.Fatalf("entry.DefaultModel = %q, want %q", entry.DefaultModel, "deepseek-chat")
	}
}

func TestProviderEntryFromConnectionInputUsesAPIProfileDefaultsForCustomNames(t *testing.T) {
	t.Parallel()

	entry, err := providerEntryFromConnectionInput(providerConnectionInput{
		Provider: "team-openai",
		API:      "openai-completions",
	})
	if err != nil {
		t.Fatalf("providerEntryFromConnectionInput() error = %v", err)
	}
	if entry.API != model.APIOpenAICompletions {
		t.Fatalf("entry.API = %q, want %q", entry.API, model.APIOpenAICompletions)
	}
	if entry.BaseURL != "https://api.openai.com/v1" {
		t.Fatalf("entry.BaseURL = %q, want %q", entry.BaseURL, "https://api.openai.com/v1")
	}
	if entry.DefaultModel != "gpt-4o" {
		t.Fatalf("entry.DefaultModel = %q, want %q", entry.DefaultModel, "gpt-4o")
	}
}

func TestBuildTempRegistryRejectsInvalidTimeout(t *testing.T) {
	t.Parallel()

	_, err := buildTempRegistry(providerConnectionInput{
		Provider: "openai",
		Timeout:  "definitely-not-a-duration",
	})
	if err == nil {
		t.Fatal("expected invalid timeout to fail")
	}
}

func TestHandleModelsValidateMergesExistingProviderConfigWhenSecretsOmitted(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer sk-configured" {
			t.Fatalf("Authorization = %q, want Bearer sk-configured", got)
		}
		var req map[string]any
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("Decode(request) error = %v", err)
		}
		if req["model"] != "gpt-live" {
			t.Fatalf("req.model = %#v, want gpt-live", req["model"])
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintln(w, `data: {"choices":[{"delta":{"role":"assistant","content":"ok"}}]}`)
		fmt.Fprintln(w, `data: [DONE]`)
	}))
	defer server.Close()

	gw := newTestGatewayFull(t)
	cfg := config.Config{
		Models: config.ModelsConfig{
			Providers: map[string]config.ProviderConfig{
				"team-openai": {
					API:          "openai-completions",
					BaseURL:      server.URL,
					APIKey:       "sk-configured",
					DefaultModel: "gpt-live",
				},
			},
		},
	}
	cfg.ApplyDefaults()
	resolver, err := controloverlay.NewResolver(context.Background(), cfg, nil, controloverlay.Options{})
	if err != nil {
		t.Fatalf("NewResolver() error = %v", err)
	}
	gw.SetEffectiveConfigResolver(resolver)

	rec := doRequest(t, gw.Handler(), "POST", "/operator/models/validate", `{
		"provider": "team-openai",
		"api": "openai-completions"
	}`)
	if rec.Code != 200 {
		t.Fatalf("POST /operator/models/validate status = %d body=%s", rec.Code, rec.Body.String())
	}

	var resp validateModelsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if !resp.Valid {
		t.Fatalf("resp.Valid = false, message = %q", resp.Message)
	}
}

func TestHandleModelsValidateUsesTemporaryProviderConnection(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer sk-test" {
			t.Fatalf("Authorization = %q, want Bearer sk-test", got)
		}
		var req map[string]any
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("Decode(request) error = %v", err)
		}
		if req["model"] != "gpt-test" {
			t.Fatalf("req.model = %#v, want gpt-test", req["model"])
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintln(w, `data: {"choices":[{"delta":{"role":"assistant","content":"ok"}}]}`)
		fmt.Fprintln(w, `data: [DONE]`)
	}))
	defer server.Close()

	gw := newTestGatewayFull(t)
	rec := doRequest(t, gw.Handler(), "POST", "/operator/models/validate", fmt.Sprintf(`{
		"provider": "openai",
		"api": "openai",
		"base_url": %q,
		"api_key": "sk-test",
		"default_model": "gpt-test"
	}`, server.URL))
	if rec.Code != 200 {
		t.Fatalf("POST /operator/models/validate status = %d body=%s", rec.Code, rec.Body.String())
	}

	var resp validateModelsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if !resp.Valid {
		t.Fatalf("Valid = false, message = %q", resp.Message)
	}
	if resp.Message != "connection successful" {
		t.Fatalf("Message = %q, want connection successful", resp.Message)
	}
	if len(resp.Models) == 0 {
		t.Fatal("expected provider models in validate response")
	}
}

func TestHandleModelsTestChatUsesTemporaryProviderConnection(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer sk-test" {
			t.Fatalf("Authorization = %q, want Bearer sk-test", got)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintln(w, `data: {"choices":[{"delta":{"role":"assistant","content":"pong"}}]}`)
		fmt.Fprintln(w, `data: [DONE]`)
	}))
	defer server.Close()

	gw := newTestGatewayFull(t)
	rec := doRequest(t, gw.Handler(), "POST", "/operator/models/test-chat", fmt.Sprintf(`{
		"provider": "openai",
		"api": "openai",
		"base_url": %q,
		"api_key": "sk-test",
		"default_model": "gpt-test",
		"message": "ping"
	}`, server.URL))
	if rec.Code != 200 {
		t.Fatalf("POST /operator/models/test-chat status = %d body=%s", rec.Code, rec.Body.String())
	}

	var resp testChatResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if !resp.OK {
		t.Fatalf("OK = false, reply = %q", resp.Reply)
	}
	if resp.Reply != "pong" {
		t.Fatalf("Reply = %q, want pong", resp.Reply)
	}
	if resp.LatencyMS < 0 {
		t.Fatalf("LatencyMS = %d, want non-negative", resp.LatencyMS)
	}
}

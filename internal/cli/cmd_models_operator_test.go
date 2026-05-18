package cli

import (
	"testing"

	"github.com/fulcrus/hopclaw/model"
)

func TestResolveModelsProviderCommandContext_ExplicitAPIOverridesPromptPreset(t *testing.T) {

	commandCtx, err := resolveModelsProviderCommandContext(localCLISetupCatalog(), "openai", "", "bedrock-converse", modelProviderState{}, false)
	if err != nil {
		t.Fatalf("resolveModelsProviderCommandContext() error = %v", err)
	}
	if commandCtx.EffectiveAPI != "bedrock-converse" {
		t.Fatalf("EffectiveAPI = %q, want bedrock-converse", commandCtx.EffectiveAPI)
	}
	if commandCtx.RequestAPI != "bedrock-converse" {
		t.Fatalf("RequestAPI = %q, want bedrock-converse", commandCtx.RequestAPI)
	}
	if commandCtx.RequestCatalogProvider != "" {
		t.Fatalf("RequestCatalogProvider = %q, want empty", commandCtx.RequestCatalogProvider)
	}
	if commandCtx.PromptProvider != "amazon-bedrock" {
		t.Fatalf("PromptProvider = %q, want amazon-bedrock", commandCtx.PromptProvider)
	}
}

func TestBuildModelsProviderMutationRequest_AddUsesCatalogDefaultsAndTypedFields(t *testing.T) {

	req, commandCtx, err := buildModelsProviderMutationRequest(localCLISetupCatalog(), "openai-prod", modelsProviderCommandOptions{
		CatalogProvider: "openai",
		Set: []string{
			"api_key=sk-test",
			"api_keys=key-a,key-b",
			"headers=Authorization=Bearer demo",
			"headers=X-Trace-Id=trace-123",
		},
	}, modelProviderState{}, true, true)
	if err != nil {
		t.Fatalf("buildModelsProviderMutationRequest() error = %v", err)
	}
	if commandCtx.RequestAPI != "openai-completions" {
		t.Fatalf("RequestAPI = %q, want openai-completions", commandCtx.RequestAPI)
	}
	if req.API == nil || *req.API != "openai-completions" {
		t.Fatalf("API = %#v, want openai-completions", req.API)
	}
	if req.BaseURL == nil || *req.BaseURL != "https://api.openai.com/v1" {
		t.Fatalf("BaseURL = %#v, want OpenAI default base URL", req.BaseURL)
	}
	if req.DefaultModel == nil || *req.DefaultModel != "gpt-4o" {
		t.Fatalf("DefaultModel = %#v, want gpt-4o", req.DefaultModel)
	}
	if req.APIKey == nil || *req.APIKey != "sk-test" {
		t.Fatalf("APIKey = %#v, want sk-test", req.APIKey)
	}
	if req.APIKeys == nil || len(*req.APIKeys) != 2 {
		t.Fatalf("APIKeys = %#v, want 2 entries", req.APIKeys)
	}
	if req.Headers == nil || len(*req.Headers) != 2 {
		t.Fatalf("Headers = %#v, want 2 entries", req.Headers)
	}
	if (*req.Headers)["Authorization"] != "Bearer demo" {
		t.Fatalf("Authorization header = %q", (*req.Headers)["Authorization"])
	}
	if (*req.Headers)["X-Trace-Id"] != "trace-123" {
		t.Fatalf("X-Trace-Id header = %q", (*req.Headers)["X-Trace-Id"])
	}
}

func TestBuildModelsProviderMutationRequest_UpdateSupportsClearSemantics(t *testing.T) {

	state := modelProviderState{
		Providers: map[string]model.ProviderEntry{
			"anthropic": {
				API:          model.APIAnthropicMessages,
				DefaultModel: "claude-sonnet-4-20250514",
			},
		},
		Details: map[string]modelProviderDetail{
			"anthropic": {Mutable: true},
		},
	}

	req, commandCtx, err := buildModelsProviderMutationRequest(localCLISetupCatalog(), "anthropic", modelsProviderCommandOptions{
		Set:   []string{"timeout=45s"},
		Clear: []string{"api_key", "headers"},
	}, state, true, false)
	if err != nil {
		t.Fatalf("buildModelsProviderMutationRequest() error = %v", err)
	}
	if commandCtx.RequestAPI != "" {
		t.Fatalf("RequestAPI = %q, want empty for in-place patch", commandCtx.RequestAPI)
	}
	if req.Timeout == nil || *req.Timeout != "45s" {
		t.Fatalf("Timeout = %#v, want 45s", req.Timeout)
	}
	if req.APIKey == nil || *req.APIKey != "" {
		t.Fatalf("APIKey clear = %#v, want empty string pointer", req.APIKey)
	}
	if req.Headers == nil {
		t.Fatal("Headers clear pointer is nil")
	}
	if *req.Headers != nil {
		t.Fatalf("Headers clear payload = %#v, want nil map", *req.Headers)
	}
}

func TestBuildModelsProviderConnectionInput_ExistingProviderDoesNotForceOverrides(t *testing.T) {

	state := modelProviderState{
		Providers: map[string]model.ProviderEntry{
			"openai": {
				API:          model.APIOpenAICompletions,
				BaseURL:      "https://proxy.example.com/v1",
				DefaultModel: "gpt-4.1",
			},
		},
		Details: map[string]modelProviderDetail{
			"openai": {Mutable: true},
		},
	}

	input, commandCtx, err := buildModelsProviderConnectionInput(localCLISetupCatalog(), "openai", modelsProviderCommandOptions{}, state, true)
	if err != nil {
		t.Fatalf("buildModelsProviderConnectionInput() error = %v", err)
	}
	if !commandCtx.Existing {
		t.Fatal("expected existing provider context")
	}
	if input.Provider != "openai" {
		t.Fatalf("Provider = %q, want openai", input.Provider)
	}
	if input.API != "" {
		t.Fatalf("API = %q, want empty for existing provider merge", input.API)
	}
	if input.CatalogProvider != "" {
		t.Fatalf("CatalogProvider = %q, want empty for existing provider merge", input.CatalogProvider)
	}
	if input.BaseURL != "" || input.DefaultModel != "" {
		t.Fatalf("unexpected inline overrides: %+v", input)
	}
}

func TestEnsureModelsProviderMutableRejectsReadOnlyEntries(t *testing.T) {

	state := modelProviderState{
		Providers: map[string]model.ProviderEntry{
			"default": {
				API:          model.APIOpenAICompletions,
				DefaultModel: "gpt-4o",
			},
		},
		Details: map[string]modelProviderDetail{
			"default": {
				Mutable:     false,
				ConfigScope: "openai_compat",
			},
		},
	}

	if err := ensureModelsProviderMutable(state, true, "default"); err == nil {
		t.Fatal("expected read-only provider to be rejected")
	}
}

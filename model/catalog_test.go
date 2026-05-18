package model

import (
	"testing"
)

// ---------------------------------------------------------------------------
// KnownProviders tests
// ---------------------------------------------------------------------------

func TestKnownProvidersNotEmpty(t *testing.T) {
	t.Parallel()

	providers := KnownProviders()
	if len(providers) == 0 {
		t.Fatal("KnownProviders() returned empty map")
	}
}

func TestKnownProvidersContainsExpectedEntries(t *testing.T) {
	t.Parallel()

	expected := []struct {
		name string
		api  ProviderAPI
	}{
		{"anthropic", APIAnthropicMessages},
		{"openai", APIOpenAICompletions},
		{"google", APIGoogleGenerativeAI},
		{"amazon-bedrock", APIBedrockConverse},
		{"github-copilot", APIGitHubCopilot},
		{"deepseek", APIOpenAICompletions},
		{"xiaomi", APIAnthropicMessages},
		{"dashscope", APIOpenAICompletions},
		{"hunyuan", APIAnthropicMessages},
		{"siliconflow", APIOpenAICompletions},
		{"groq", APIOpenAICompletions},
		{"ollama", APIOllama},
	}

	providers := KnownProviders()
	for _, tt := range expected {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			entry, ok := providers[tt.name]
			if !ok {
				t.Fatalf("provider %q not found in catalog", tt.name)
			}
			if entry.Provider.API != tt.api {
				t.Fatalf("provider %q API = %q, want %q", tt.name, entry.Provider.API, tt.api)
			}
		})
	}
}

func TestKnownProvidersReturnsFreshMap(t *testing.T) {
	t.Parallel()

	a := KnownProviders()
	b := KnownProviders()

	// Mutating one should not affect the other.
	delete(a, "anthropic")
	if _, ok := b["anthropic"]; !ok {
		t.Fatal("KnownProviders() should return independent copies")
	}
}

// ---------------------------------------------------------------------------
// CatalogLookup tests
// ---------------------------------------------------------------------------

func TestCatalogLookupFound(t *testing.T) {
	t.Parallel()

	entry, ok := CatalogLookup("anthropic")
	if !ok {
		t.Fatal("CatalogLookup(anthropic) returned false")
	}
	if entry.Provider.API != APIAnthropicMessages {
		t.Fatalf("entry.Provider.API = %q", entry.Provider.API)
	}
	if entry.Provider.BaseURL != "https://api.anthropic.com" {
		t.Fatalf("entry.Provider.BaseURL = %q", entry.Provider.BaseURL)
	}
}

func TestCatalogLookupNotFound(t *testing.T) {
	t.Parallel()

	_, ok := CatalogLookup("nonexistent-provider")
	if ok {
		t.Fatal("CatalogLookup should return false for unknown provider")
	}
}

func TestCatalogLookupTrimsWhitespace(t *testing.T) {
	t.Parallel()

	entry, ok := CatalogLookup("  openai  ")
	if !ok {
		t.Fatal("CatalogLookup should trim whitespace")
	}
	if entry.Provider.API != APIOpenAICompletions {
		t.Fatalf("entry.Provider.API = %q", entry.Provider.API)
	}
}

func TestCatalogLookupRequireBaseURL(t *testing.T) {
	t.Parallel()

	entry, ok := CatalogLookup("litellm")
	if !ok {
		t.Fatal("CatalogLookup(litellm) returned false")
	}
	if !entry.RequireBaseURL {
		t.Fatal("litellm should require base URL")
	}
}

// ---------------------------------------------------------------------------
// MergeWithCatalog tests
// ---------------------------------------------------------------------------

func TestMergeWithCatalogFillsMissingFields(t *testing.T) {
	t.Parallel()

	providers := map[string]ProviderEntry{
		"anthropic": {
			APIKey: "sk-test",
		},
	}

	merged := MergeWithCatalog(providers)
	entry, ok := merged["anthropic"]
	if !ok {
		t.Fatal("merged map missing anthropic")
	}
	if entry.API != APIAnthropicMessages {
		t.Fatalf("entry.API = %q, want %q", entry.API, APIAnthropicMessages)
	}
	if entry.BaseURL != "https://api.anthropic.com" {
		t.Fatalf("entry.BaseURL = %q", entry.BaseURL)
	}
	if entry.APIKey != "sk-test" {
		t.Fatalf("entry.APIKey = %q, want sk-test", entry.APIKey)
	}
}

func TestMergeWithCatalogExplicitOverridesCatalog(t *testing.T) {
	t.Parallel()

	providers := map[string]ProviderEntry{
		"anthropic": {
			API:          APIOpenAICompletions, // Override API type.
			BaseURL:      "https://my-proxy.com",
			DefaultModel: "my-custom-model",
		},
	}

	merged := MergeWithCatalog(providers)
	entry := merged["anthropic"]
	if entry.API != APIOpenAICompletions {
		t.Fatalf("entry.API = %q, should keep explicit value", entry.API)
	}
	if entry.BaseURL != "https://my-proxy.com" {
		t.Fatalf("entry.BaseURL = %q, should keep explicit value", entry.BaseURL)
	}
	if entry.DefaultModel != "my-custom-model" {
		t.Fatalf("entry.DefaultModel = %q, should keep explicit value", entry.DefaultModel)
	}
}

func TestMergeWithCatalogUnknownProviderPassedThrough(t *testing.T) {
	t.Parallel()

	providers := map[string]ProviderEntry{
		"custom-provider": {
			API:     APIOpenAICompletions,
			BaseURL: "https://custom.api.com",
			APIKey:  "sk-custom",
		},
	}

	merged := MergeWithCatalog(providers)
	entry, ok := merged["custom-provider"]
	if !ok {
		t.Fatal("unknown provider should be passed through")
	}
	if entry.API != APIOpenAICompletions {
		t.Fatalf("entry.API = %q", entry.API)
	}
	if entry.BaseURL != "https://custom.api.com" {
		t.Fatalf("entry.BaseURL = %q", entry.BaseURL)
	}
}

func TestMergeWithCatalogEmptyInput(t *testing.T) {
	t.Parallel()

	merged := MergeWithCatalog(map[string]ProviderEntry{})
	if len(merged) != 0 {
		t.Fatalf("merged should be empty, got %d entries", len(merged))
	}
}

// ---------------------------------------------------------------------------
// CatalogEntry.RequireBaseURL tests
// ---------------------------------------------------------------------------

func TestCatalogRequireBaseURLProviders(t *testing.T) {
	t.Parallel()

	requireBaseURL := []string{"litellm", "opencode", "cloudflare-ai-gateway"}
	for _, name := range requireBaseURL {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			entry, ok := CatalogLookup(name)
			if !ok {
				t.Fatalf("CatalogLookup(%q) returned false", name)
			}
			if !entry.RequireBaseURL {
				t.Fatalf("provider %q should have RequireBaseURL=true", name)
			}
		})
	}
}

func TestCatalogNonRequireBaseURLProviders(t *testing.T) {
	t.Parallel()

	noBaseURLRequired := []string{"anthropic", "openai", "google", "deepseek"}
	for _, name := range noBaseURLRequired {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			entry, ok := CatalogLookup(name)
			if !ok {
				t.Fatalf("CatalogLookup(%q) returned false", name)
			}
			if entry.RequireBaseURL {
				t.Fatalf("provider %q should not have RequireBaseURL=true", name)
			}
		})
	}
}

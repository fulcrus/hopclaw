package config

import (
	"testing"

	"github.com/fulcrus/hopclaw/model"
)

func TestNormalizeProviderConfigUsesCatalogAPIWhenMissing(t *testing.T) {
	t.Parallel()

	cfg := NormalizeProviderConfig("anthropic", ProviderConfig{})
	if cfg.API != string(model.APIAnthropicMessages) {
		t.Fatalf("API = %q, want %q", cfg.API, model.APIAnthropicMessages)
	}
}

func TestNormalizeProviderConfigNormalizesAPIKeysAndHeaders(t *testing.T) {
	t.Parallel()

	cfg := NormalizeProviderConfig("custom", ProviderConfig{
		APIKeys: []string{" key-a ", "", "key-b"},
		Headers: map[string]string{
			" authorization ": " Bearer demo ",
			"  ":              "ignored",
			"x-trace-id":      " trace-123 ",
		},
	})

	if len(cfg.APIKeys) != 2 || cfg.APIKeys[0] != "key-a" || cfg.APIKeys[1] != "key-b" {
		t.Fatalf("APIKeys = %#v", cfg.APIKeys)
	}
	if len(cfg.Headers) != 2 {
		t.Fatalf("Headers = %#v", cfg.Headers)
	}
	if got := cfg.Headers["Authorization"]; got != "Bearer demo" {
		t.Fatalf("Authorization = %q, want %q", got, "Bearer demo")
	}
	if got := cfg.Headers["X-Trace-Id"]; got != "trace-123" {
		t.Fatalf("X-Trace-Id = %q, want %q", got, "trace-123")
	}
}

func TestProviderEntryFromConfigClonesAndNormalizesFields(t *testing.T) {
	t.Parallel()

	entry := ProviderEntryFromConfig("custom", ProviderConfig{
		API:          "openai",
		BaseURL:      " https://api.example.com/v1 ",
		APIKey:       " sk-test ",
		DefaultModel: " model-a ",
		Headers:      map[string]string{"X-Test": "value"},
		APIKeys:      []string{"k1", "k2"},
	})
	if entry.API != model.APIOpenAICompletions {
		t.Fatalf("API = %q, want %q", entry.API, model.APIOpenAICompletions)
	}
	if entry.BaseURL != "https://api.example.com/v1" {
		t.Fatalf("BaseURL = %q", entry.BaseURL)
	}
	if entry.APIKey != "sk-test" {
		t.Fatalf("APIKey = %q", entry.APIKey)
	}
	if entry.DefaultModel != "model-a" {
		t.Fatalf("DefaultModel = %q", entry.DefaultModel)
	}
	entry.Headers["X-Test"] = "changed"
	entry.APIKeys[0] = "changed"
	fresh := ProviderEntryFromConfig("custom", ProviderConfig{
		API:     "openai",
		Headers: map[string]string{"X-Test": "value"},
		APIKeys: []string{"k1", "k2"},
	})
	if fresh.Headers["X-Test"] != "value" || fresh.APIKeys[0] != "k1" {
		t.Fatalf("expected cloned slices/maps, got %+v", fresh)
	}
}

func TestOpenAICompatProviderEntryRequiresBaseURL(t *testing.T) {
	t.Parallel()

	if _, ok := OpenAICompatProviderEntry(OpenAICompatConfig{}); ok {
		t.Fatal("expected missing base url to disable openai compat entry")
	}

	entry, ok := OpenAICompatProviderEntry(OpenAICompatConfig{
		BaseURL: "https://api.openai.com/v1",
		APIKey:  "sk-test",
		Model:   "gpt-4o",
	})
	if !ok {
		t.Fatal("expected configured openai compat provider entry")
	}
	if entry.API != model.APIOpenAICompletions || entry.DefaultModel != "gpt-4o" {
		t.Fatalf("entry = %+v", entry)
	}
}

func TestProviderConfigHasCredentials(t *testing.T) {
	t.Parallel()

	if ProviderConfigHasCredentials(ProviderConfig{}) {
		t.Fatal("expected empty provider config to report no credentials")
	}
	if !ProviderConfigHasCredentials(ProviderConfig{APIKey: "sk-test"}) {
		t.Fatal("expected api key to count as credentials")
	}
	if !ProviderConfigHasCredentials(ProviderConfig{AccessKeyID: "AKIA", SecretKey: "secret"}) {
		t.Fatal("expected access/secret pair to count as credentials")
	}
}

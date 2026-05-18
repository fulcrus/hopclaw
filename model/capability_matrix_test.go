package model

import (
	"testing"

	"github.com/fulcrus/hopclaw/modelrouter"
)

func TestCapabilityMatrixForProviderUsesRuntimeStreamingSemantics(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		providerName  string
		entry         ProviderEntry
		wantStreaming bool
		wantTools     bool
	}{
		{
			name:         "openai compat streams",
			providerName: "openai",
			entry: ProviderEntry{
				API:          APIOpenAICompletions,
				DefaultModel: "gpt-4o",
			},
			wantStreaming: true,
			wantTools:     true,
		},
		{
			name:         "openai responses streams",
			providerName: "aixj",
			entry: ProviderEntry{
				API:          APIOpenAIResponses,
				DefaultModel: "gpt-5.4",
			},
			wantStreaming: true,
			wantTools:     true,
		},
		{
			name:         "google runtime stream enabled",
			providerName: "google",
			entry: ProviderEntry{
				API:          APIGoogleGenerativeAI,
				DefaultModel: "gemini-2.5-pro",
			},
			wantStreaming: true,
			wantTools:     true,
		},
		{
			name:         "bedrock runtime stream enabled",
			providerName: "amazon-bedrock",
			entry: ProviderEntry{
				API:          APIBedrockConverse,
				DefaultModel: "claude-sonnet-4-5-20241022",
			},
			wantStreaming: true,
			wantTools:     true,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			matrix := CapabilityMatrixForProvider(tt.providerName, tt.entry)
			if matrix.SupportsStreaming != tt.wantStreaming {
				t.Fatalf("SupportsStreaming = %v, want %v", matrix.SupportsStreaming, tt.wantStreaming)
			}
			if matrix.SupportsTools != tt.wantTools {
				t.Fatalf("SupportsTools = %v, want %v", matrix.SupportsTools, tt.wantTools)
			}
		})
	}
}

func TestCapabilityMatrixForUnknownModelFallsBackToAPIDefaults(t *testing.T) {
	t.Parallel()

	matrix := CapabilityMatrixForProvider("custom-openai", ProviderEntry{
		API:          APIOpenAICompletions,
		DefaultModel: "my-unknown-model",
	})
	if matrix.Source != "api_defaults" {
		t.Fatalf("Source = %q, want api_defaults", matrix.Source)
	}
	if matrix.ContextWindow != 128_000 {
		t.Fatalf("ContextWindow = %d, want 128000", matrix.ContextWindow)
	}
	if matrix.MaxOutputTokens != 8_192 {
		t.Fatalf("MaxOutputTokens = %d, want 8192", matrix.MaxOutputTokens)
	}
	if matrix.DisplayName != "my-unknown-model" {
		t.Fatalf("DisplayName = %q, want my-unknown-model", matrix.DisplayName)
	}
}

func TestCapabilityMatrixForCatalogEntryNormalizesProviderAPI(t *testing.T) {
	t.Parallel()

	matrix := CapabilityMatrixForCatalogEntry("preset-openai", "openai", "gpt-4o")
	if matrix.ProviderAPI != APIOpenAICompletions {
		t.Fatalf("ProviderAPI = %q, want %q", matrix.ProviderAPI, APIOpenAICompletions)
	}
	if !matrix.SupportsTools {
		t.Fatal("expected catalog matrix to reuse runtime tool capability contract")
	}
	if matrix.Model != "gpt-4o" {
		t.Fatalf("Model = %q, want gpt-4o", matrix.Model)
	}
}

func TestBuildRouterProfilesUsesDefaultProviderRawModelIDs(t *testing.T) {
	t.Parallel()

	profiles := BuildRouterProfiles(map[string]ProviderEntry{
		"default": {
			API:          APIOpenAICompletions,
			DefaultModel: "gpt-4o",
		},
		"anthropic": {
			API:          APIAnthropicMessages,
			DefaultModel: "claude-sonnet-4-5-20241022",
		},
	}, "default")

	var foundDefault bool
	var foundAnthropic bool
	for _, profile := range profiles {
		switch profile.ID {
		case "gpt-4o":
			foundDefault = true
			if profile.Provider != "default" {
				t.Fatalf("default profile provider = %q", profile.Provider)
			}
		case "anthropic/claude-sonnet-4-5-20241022":
			foundAnthropic = true
			if !profile.Supports[modelrouter.CapabilityStreaming] {
				t.Fatal("anthropic profile should keep runtime streaming=true")
			}
		}
	}
	if !foundDefault {
		t.Fatal("expected raw model id for default provider profile")
	}
	if !foundAnthropic {
		t.Fatal("expected provider-prefixed profile for non-default provider")
	}
}

func TestBuildRouterProfilesWithProviderCapabilitiesHonorsRuntimeContract(t *testing.T) {
	t.Parallel()

	profiles := BuildRouterProfilesWithProviderCapabilities(
		map[string]ProviderEntry{
			"openai": {
				API:          APIOpenAICompletions,
				DefaultModel: "gpt-4o",
			},
		},
		map[string]CapabilityMatrix{
			"openai": {
				ProviderName:         "openai",
				ProviderAPI:          APIOpenAICompletions,
				Model:                "gpt-4o",
				SupportsSystemPrompt: true,
				SupportsTemperature:  true,
				SupportsMaxTokens:    true,
				SupportsTools:        false,
				SupportsToolReplay:   true,
				SupportsStreaming:    false,
				Source:               "operator_contract",
			},
		},
		"openai",
	)

	assertProfileCapability := func(profileID string) {
		for _, profile := range profiles {
			if profile.ID != profileID {
				continue
			}
			if profile.Supports[modelrouter.CapabilityTools] {
				t.Fatalf("%s tools = true, want false from runtime contract", profileID)
			}
			if profile.Supports[modelrouter.CapabilityStreaming] {
				t.Fatalf("%s streaming = true, want false from runtime contract", profileID)
			}
			return
		}
		t.Fatalf("profile %q not found", profileID)
	}

	assertProfileCapability("gpt-4o")
	assertProfileCapability("gpt-4o-mini")
}

func TestResolveDefaultProviderFallsBackToSyntheticDefaultEntry(t *testing.T) {
	t.Parallel()

	providers := map[string]ProviderEntry{
		"default": {
			API:          APIOpenAICompletions,
			DefaultModel: "gpt-4o",
		},
		"anthropic": {
			API:          APIAnthropicMessages,
			DefaultModel: "claude-sonnet-4-5-20241022",
		},
	}

	if got := ResolveDefaultProvider(providers, ""); got != "default" {
		t.Fatalf("ResolveDefaultProvider() = %q, want default", got)
	}
}

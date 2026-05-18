package cli

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/fulcrus/hopclaw/config"
)

func TestLoadCLISetupCatalogPrefersOperatorSurface(t *testing.T) {

	remote := config.OperatorSetupCatalog{
		DefaultAddress: "127.0.0.1:19090",
		AuthModes: []config.AuthModeProfile{
			{ID: "bearer", DisplayName: "Remote Bearer", Recommended: true},
		},
		Providers: []config.SetupProviderProfile{
			{
				ID:            "remote-openai",
				DisplayName:   "Remote OpenAI",
				Description:   "Remote provider profile",
				API:           "openai-completions",
				BaseURL:       "https://remote.example.com/v1",
				DefaultModels: []string{"gpt-4.1"},
				EnvVars:       []string{"REMOTE_OPENAI_API_KEY"},
				APIKeyHint:    "Remote API key",
			},
		},
		ProviderAPIs: []config.ProviderAPIProfile{
			{
				ID:          "openai-completions",
				DisplayName: "Remote OpenAI-compatible",
				Fields: []config.SetupProviderField{
					{ID: "api_key", Label: "Remote API Key", Required: true},
					{ID: "default_model", Label: "Remote Default Model"},
				},
			},
		},
		Channels: []config.ChannelProfile{
			{
				ID:                  "remote_chat",
				DisplayName:         "Remote Chat",
				Implemented:         true,
				SetupSupported:      true,
				OnboardingSupported: true,
				Fields: []config.SetupChannelField{
					{ID: "legacy_token", ConfigKey: "legacy_token", Label: "Legacy Token"},
				},
				OperatorFields: []config.SetupChannelField{
					{ID: "bot_token", ConfigKey: "bot_token", Label: "Remote Bot Token", Required: true, Secret: true},
					{ID: "require_mention", ConfigKey: "require_mention", Label: "Require Mention", Type: config.SetupChannelFieldBool},
				},
			},
		},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != setupCatalogPath {
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(remote)
	}))
	defer srv.Close()

	catalog := loadCLISetupCatalog(context.Background(), &GatewayClient{
		BaseURL: srv.URL,
		HTTP:    srv.Client(),
	})

	if catalog.DefaultProviderAPI("remote-openai") != "openai-completions" {
		t.Fatalf("DefaultProviderAPI = %q, want openai-completions", catalog.DefaultProviderAPI("remote-openai"))
	}
	if catalog.DefaultBaseURL("remote-openai") != "https://remote.example.com/v1" {
		t.Fatalf("DefaultBaseURL = %q", catalog.DefaultBaseURL("remote-openai"))
	}
	if catalog.DefaultModelForProvider("remote-openai") != "gpt-4.1" {
		t.Fatalf("DefaultModelForProvider = %q", catalog.DefaultModelForProvider("remote-openai"))
	}
	if catalog.ProviderDisplayName("remote-openai") != "Remote OpenAI" {
		t.Fatalf("ProviderDisplayName = %q", catalog.ProviderDisplayName("remote-openai"))
	}
	apiProfile, ok := catalog.LookupProviderAPIProfile("openai-completions")
	if !ok {
		t.Fatal("expected remote provider api profile")
	}
	if apiProfile.DisplayName != "Remote OpenAI-compatible" {
		t.Fatalf("api profile display name = %q", apiProfile.DisplayName)
	}
	if got := catalog.ProviderAPIFieldDefault("openai-completions", "api_key"); got != "" {
		t.Fatalf("ProviderAPIFieldDefault(api_key) = %q, want empty", got)
	}
	auth, ok := catalog.LookupAuthModeProfile("Bearer")
	if !ok || auth.DisplayName != "Remote Bearer" {
		t.Fatalf("auth mode = %+v, ok=%v", auth, ok)
	}
	channels := catalog.SetupChannelProfiles()
	if len(channels) != 1 || channels[0].ID != "remote_chat" {
		t.Fatalf("SetupChannelProfiles = %#v", channels)
	}
	if len(channels[0].OperatorFields) != 2 || channels[0].OperatorFields[0].ID != "bot_token" {
		t.Fatalf("SetupChannelProfiles operator fields = %#v", channels[0].OperatorFields)
	}
	channels[0].OperatorFields[0].Label = "mutated"
	fresh := catalog.SetupChannelProfiles()
	if fresh[0].OperatorFields[0].Label == "mutated" {
		t.Fatal("setup channel profiles should return independent operator field copies")
	}

	providers := catalog.ProviderProfiles()
	if len(providers[0].EnvVars) != 1 || providers[0].EnvVars[0] != "REMOTE_OPENAI_API_KEY" {
		t.Fatalf("ProviderProfiles env vars = %#v", providers[0].EnvVars)
	}
	providers[0].EnvVars[0] = "MUTATED"
	freshProviders := catalog.ProviderProfiles()
	if freshProviders[0].EnvVars[0] == "MUTATED" {
		t.Fatal("provider env vars should be returned as independent copies")
	}
}

func TestLoadCLISetupCatalogFallsBackToLocalCatalog(t *testing.T) {

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":"boom"}`, http.StatusInternalServerError)
	}))
	defer srv.Close()

	catalog := loadCLISetupCatalog(context.Background(), &GatewayClient{
		BaseURL: srv.URL,
		HTTP:    srv.Client(),
	})

	if _, ok := catalog.LookupProviderProfile("openai"); !ok {
		t.Fatal("expected local catalog fallback to include openai")
	}
	if _, ok := catalog.LookupProviderAPIProfile("openai-completions"); !ok {
		t.Fatal("expected local catalog fallback to include openai-completions")
	}
	if _, ok := catalog.LookupAuthModeProfile("bearer"); !ok {
		t.Fatal("expected local catalog fallback to include bearer auth")
	}
	if len(catalog.SetupChannelProfiles()) == 0 {
		t.Fatal("expected local catalog fallback to include setup channels")
	}
}

func TestProviderFieldInitialValueUsesCatalogEnvVars(t *testing.T) {
	t.Setenv("PLUGIN_API_KEY", "plugin-secret")

	catalog := cliSetupCatalog{
		catalog: config.OperatorSetupCatalog{
			Providers: []config.SetupProviderProfile{
				{
					ID:         "plugin-openai",
					API:        "openai-completions",
					EnvVars:    []string{"PLUGIN_API_KEY"},
					APIKeyHint: "Plugin API key",
				},
			},
		},
	}

	value := providerFieldInitialValue(catalog, "plugin-openai", config.SetupProviderField{ID: "api_key"})
	if value != "plugin-secret" {
		t.Fatalf("providerFieldInitialValue(api_key) = %q, want plugin-secret", value)
	}
}

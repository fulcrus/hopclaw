package bootstrap

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/fulcrus/hopclaw/config"
	"github.com/fulcrus/hopclaw/internal/modules"
)

func TestRuntimeConfigModulesExposeConfiguredProvidersAndExternalTools(t *testing.T) {
	t.Parallel()

	cfg := config.Config{
		Models: config.ModelsConfig{
			OpenAICompat: config.OpenAICompatConfig{
				BaseURL:   "https://compat.example/v1",
				APIKey:    "compat-key",
				Fallbacks: []string{"backup"},
				Model:     "gpt-4o-mini",
				Timeout:   12 * time.Second,
				Headers: map[string]string{
					"X-Compat": "1",
				},
			},
			Providers: map[string]config.ProviderConfig{
				"copilot": {
					API:          "github-copilot",
					BaseURL:      "https://copilot.example/v1",
					APIKeys:      []string{"k1", "k2"},
					Fallbacks:    []string{"secondary"},
					DefaultModel: "gpt-4o",
				},
			},
		},
		Tools: config.ToolsConfig{
			External: []config.ExternalToolConfig{{
				Name:        "web.lookup",
				Description: "Lookup URLs",
				Endpoint:    "https://tools.example/lookup",
				Timeout:     "15s",
				InputSchema: map[string]any{"type": "object"},
			}},
		},
		Channels: config.ChannelsConfig{
			Slack: config.SlackChannelConfig{
				Enabled:  boolPtr(true),
				BotToken: "xoxb-test",
				AppToken: "xapp-test",
			},
		},
	}

	catalog := modules.BuildCatalog(runtimeConfigModules(cfg))
	if _, ok := catalog.Find(configProviderModulePrefix + "default-openai-compat"); !ok {
		t.Fatal("expected openai compat provider module in catalog")
	}
	if _, ok := catalog.Find(configProviderModulePrefix + "copilot"); !ok {
		t.Fatal("expected named provider module in catalog")
	}
	if _, ok := catalog.Find(configExternalToolModulePrefix + "web.lookup"); !ok {
		t.Fatal("expected external tool module in catalog")
	}
	if _, ok := catalog.Find(configChannelModulePrefix + "slack"); !ok {
		t.Fatal("expected channel module in catalog")
	}

	providers := catalog.ProviderProjections()
	if len(providers) != 2 {
		t.Fatalf("len(provider projections) = %d, want 2", len(providers))
	}
	if providers[0].Name != "copilot" {
		t.Fatalf("first provider projection = %#v", providers[0])
	}
	if !reflect.DeepEqual(providers[0].APIKeys, []string{"k1", "k2"}) {
		t.Fatalf("copilot api keys = %#v", providers[0].APIKeys)
	}
	if !reflect.DeepEqual(providers[0].Fallbacks, []string{"secondary"}) {
		t.Fatalf("copilot fallbacks = %#v", providers[0].Fallbacks)
	}
	raw, err := json.Marshal(providers)
	if err != nil {
		t.Fatalf("Marshal(provider projections) error = %v", err)
	}
	if strings.Contains(string(raw), "k1") || strings.Contains(string(raw), "compat-key") {
		t.Fatalf("provider projections leaked credentials: %s", string(raw))
	}
	providerModule, ok := catalog.Find(configProviderModulePrefix + "copilot")
	if !ok {
		t.Fatal("expected copilot provider module in catalog")
	}
	providerMeta := providerModule.Contributions().Providers[0].Metadata
	for _, forbidden := range []string{"api_key", "api_keys", "access_key_id", "secret_key", "session_token", "headers"} {
		if _, exists := providerMeta[forbidden]; exists {
			t.Fatalf("provider metadata leaked %q: %#v", forbidden, providerMeta)
		}
	}

	tools := catalog.ToolProjections()
	if len(tools) != 1 {
		t.Fatalf("len(tool projections) = %d, want 1", len(tools))
	}
	if tools[0].Name != "web.lookup" || tools[0].Endpoint != "https://tools.example/lookup" || tools[0].Timeout != 15*time.Second {
		t.Fatalf("tool projection = %#v", tools[0])
	}

	channels := catalog.ChannelProjections()
	if len(channels) != 1 {
		t.Fatalf("len(channel projections) = %d, want 1", len(channels))
	}
	if channels[0].Name != "slack" || channels[0].Type != "slack" {
		t.Fatalf("channel projection = %#v", channels[0])
	}
	raw, err = json.Marshal(channels)
	if err != nil {
		t.Fatalf("Marshal(channel projections) error = %v", err)
	}
	if strings.Contains(string(raw), "xoxb-test") || strings.Contains(string(raw), "xapp-test") {
		t.Fatalf("channel projections leaked secrets: %s", string(raw))
	}
	channelModule, ok := catalog.Find(configChannelModulePrefix + "slack")
	if !ok {
		t.Fatal("expected slack channel module in catalog")
	}
	channelMeta := channelModule.Contributions().Channels[0].Metadata
	for _, forbidden := range []string{"secret", "config"} {
		if _, exists := channelMeta[forbidden]; exists {
			t.Fatalf("channel metadata leaked %q: %#v", forbidden, channelMeta)
		}
	}
}

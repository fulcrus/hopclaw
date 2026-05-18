package bootstrap

import (
	"reflect"
	"testing"
	"time"

	"github.com/fulcrus/hopclaw/config"
	"github.com/fulcrus/hopclaw/internal/modules"
	"github.com/fulcrus/hopclaw/plugin"
)

func TestBuildModelProvidersUsesModuleCatalogProviderProjections(t *testing.T) {
	t.Parallel()

	manager := plugin.NewManager()
	if err := manager.Register(plugin.LoadedPlugin{
		Dir: t.TempDir(),
		Manifest: plugin.Manifest{
			Name: "demo",
			Providers: map[string]plugin.ProviderDecl{
				"copilot": {
					API:          "github-copilot",
					BaseURL:      "https://copilot-proxy.example.test",
					APIKey:       "test-key",
					DefaultModel: "gpt-4o",
					Timeout:      45 * time.Second,
					Headers: map[string]string{
						"X-Plugin": "demo",
					},
				},
			},
		},
	}); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	providers, defaultProvider, err := buildModelProviders(config.ModelsConfig{}, modules.NewStore(modules.BuildCatalog(manager.Modules())))
	if err != nil {
		t.Fatalf("buildModelProviders() error = %v", err)
	}

	entry, ok := providers["demo/copilot"]
	if !ok {
		t.Fatalf("providers = %#v, want demo/copilot", providers)
	}
	if defaultProvider != "demo/copilot" {
		t.Fatalf("defaultProvider = %q, want demo/copilot", defaultProvider)
	}
	if entry.APIKey != "test-key" {
		t.Fatalf("entry.APIKey = %q", entry.APIKey)
	}
	if entry.Timeout != 45*time.Second {
		t.Fatalf("entry.Timeout = %s, want 45s", entry.Timeout)
	}
	if !reflect.DeepEqual(entry.Headers, map[string]string{"X-Plugin": "demo"}) {
		t.Fatalf("entry.Headers = %#v", entry.Headers)
	}
}

func TestBuildModelProvidersPrefersExplicitConfigOverModuleProjection(t *testing.T) {
	t.Parallel()

	manager := plugin.NewManager()
	if err := manager.Register(plugin.LoadedPlugin{
		Dir: t.TempDir(),
		Manifest: plugin.Manifest{
			Name:   "demo",
			Format: plugin.ManifestFormatOpenClawJSON,
			Providers: map[string]plugin.ProviderDecl{
				"openai": {
					API:              "openai-completions",
					BaseURL:          "https://plugin.example/v1",
					DefaultModel:     "plugin-model",
					PreferUnscopedID: true,
				},
			},
		},
	}); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	providers, _, err := buildModelProviders(config.ModelsConfig{
		Providers: map[string]config.ProviderConfig{
			"openai": {
				API:          "openai-completions",
				BaseURL:      "https://config.example/v1",
				DefaultModel: "config-model",
			},
		},
	}, modules.NewStore(modules.BuildCatalog(manager.Modules())))
	if err != nil {
		t.Fatalf("buildModelProviders() error = %v", err)
	}

	entry, ok := providers["openai"]
	if !ok {
		t.Fatalf("providers = %#v, want openai", providers)
	}
	if entry.BaseURL != "https://config.example/v1" {
		t.Fatalf("entry.BaseURL = %q, want config value", entry.BaseURL)
	}
	if entry.DefaultModel != "config-model" {
		t.Fatalf("entry.DefaultModel = %q, want config-model", entry.DefaultModel)
	}
}

func TestBuildModelProvidersUsesConfigProviderModulesWhenPresent(t *testing.T) {
	t.Parallel()

	cfg := config.ModelsConfig{
		Providers: map[string]config.ProviderConfig{
			"openai": {
				API:          "openai-completions",
				BaseURL:      "https://config.example/v1",
				APIKeys:      []string{"k1", "k2"},
				Fallbacks:    []string{"backup"},
				DefaultModel: "gpt-4o",
			},
		},
	}

	moduleCatalog := modules.NewStore(modules.BuildCatalog(configProviderModules(cfg)))
	providers, defaultProvider, err := buildModelProviders(cfg, moduleCatalog)
	if err != nil {
		t.Fatalf("buildModelProviders() error = %v", err)
	}

	entry, ok := providers["openai"]
	if !ok {
		t.Fatalf("providers = %#v, want openai", providers)
	}
	if defaultProvider != "openai" {
		t.Fatalf("defaultProvider = %q, want openai", defaultProvider)
	}
	if !reflect.DeepEqual(entry.APIKeys, []string{"k1", "k2"}) {
		t.Fatalf("entry.APIKeys = %#v", entry.APIKeys)
	}
	if !reflect.DeepEqual(entry.Fallbacks, []string{"backup"}) {
		t.Fatalf("entry.Fallbacks = %#v", entry.Fallbacks)
	}
	if entry.BaseURL != "https://config.example/v1" {
		t.Fatalf("entry.BaseURL = %q", entry.BaseURL)
	}
}

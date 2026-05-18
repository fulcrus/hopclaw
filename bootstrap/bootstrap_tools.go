package bootstrap

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/fulcrus/hopclaw/agent"
	"github.com/fulcrus/hopclaw/artifact"
	capregistry "github.com/fulcrus/hopclaw/capability/registry"
	"github.com/fulcrus/hopclaw/config"
	"github.com/fulcrus/hopclaw/internal/modules"
	"github.com/fulcrus/hopclaw/model"
	"github.com/fulcrus/hopclaw/modelrouter"
	"github.com/fulcrus/hopclaw/toolruntime"
	toolregistry "github.com/fulcrus/hopclaw/toolruntime/registry"
)

const defaultModelRouterCooldown = 30 * time.Second

func initModelRuntime(cfg config.ModelsConfig, moduleCatalog *modules.Store) (agent.ModelClient, agent.ModelRouter, error) {
	providers, defaultProvider, err := buildModelProviders(cfg, moduleCatalog)
	if err != nil {
		return nil, nil, err
	}
	if len(providers) == 0 {
		return unavailableModelClient{}, nil, nil
	}
	registry, err := model.NewRegistry(providers)
	if err != nil {
		return nil, nil, err
	}
	if defaultProvider != "" {
		if err := registry.SetDefault(defaultProvider); err != nil {
			return nil, nil, err
		}
	}
	profiles := model.BuildRouterProfiles(providers, defaultProvider)
	if len(profiles) == 0 {
		return registry, nil, nil
	}
	return registry, modelrouter.NewInMemoryRouter(profiles, defaultModelRouterCooldown), nil
}

func buildModelProviders(cfg config.ModelsConfig, moduleCatalog *modules.Store) (map[string]model.ProviderEntry, string, error) {
	providers := make(map[string]model.ProviderEntry)
	useConfigModules := moduleCatalogHasConfigProviderModules(moduleCatalog)

	if !useConfigModules {
		// Backward compat: treat models.openai_compat as the "default" provider.
		if entry, ok := config.OpenAICompatProviderEntry(cfg.OpenAICompat); ok {
			providers["default"] = entry
		}

		// Multi-provider config.
		for name, pcfg := range cfg.Providers {
			providers[name] = config.ProviderEntryFromConfig(name, pcfg)
		}
	}

	// Module-contributed providers.
	if moduleCatalog != nil {
		for _, projection := range moduleCatalog.ProviderProjections() {
			key := strings.TrimSpace(projection.Name)
			if key == "" {
				continue
			}
			if _, exists := providers[key]; exists {
				continue // explicit config takes precedence
			}
			providers[key] = model.ProviderEntry{
				API:          model.ProviderAPI(strings.TrimSpace(projection.API)),
				BaseURL:      strings.TrimSpace(projection.BaseURL),
				Region:       strings.TrimSpace(projection.Region),
				APIKey:       strings.TrimSpace(projection.APIKey),
				APIKeys:      append([]string(nil), projection.APIKeys...),
				Fallbacks:    append([]string(nil), projection.Fallbacks...),
				AccessKeyID:  strings.TrimSpace(projection.AccessKeyID),
				SecretKey:    strings.TrimSpace(projection.SecretKey),
				SessionToken: strings.TrimSpace(projection.SessionToken),
				DefaultModel: strings.TrimSpace(projection.DefaultModel),
				Timeout:      projection.Timeout,
				Headers:      cloneStringMap(projection.Headers),
			}
		}
	}

	// Merge with built-in catalog so users only need an API key for known providers.
	providers = model.MergeWithCatalog(providers)
	for name, entry := range providers {
		if catalog, ok := model.CatalogLookup(name); ok && catalog.RequireBaseURL && strings.TrimSpace(entry.BaseURL) == "" {
			return nil, "", fmt.Errorf("models.providers.%s.base_url is required for this catalog provider", name)
		}
	}
	defaultProvider := strings.TrimSpace(cfg.DefaultProvider)
	if defaultProvider == "" {
		if _, ok := providers["default"]; ok {
			defaultProvider = "default"
		} else if len(providers) == 1 {
			for name := range providers {
				defaultProvider = name
			}
		}
	}
	return providers, defaultProvider, nil
}

func moduleCatalogHasConfigProviderModules(moduleCatalog *modules.Store) bool {
	if moduleCatalog == nil {
		return false
	}
	for _, projection := range moduleCatalog.ProviderProjections() {
		if strings.HasPrefix(strings.TrimSpace(projection.ModuleID), configProviderModulePrefix) {
			return true
		}
	}
	return false
}

type unavailableModelClient struct{}

func (unavailableModelClient) Chat(context.Context, agent.ChatRequest) (*agent.ModelResponse, error) {
	return nil, fmt.Errorf("model client is not configured")
}

// ---------------------------------------------------------------------------
// Tool initialization
// ---------------------------------------------------------------------------

type toolsResult struct {
	Executor agent.ToolExecutor
	Builtins *toolruntime.Builtins
	Layer2   *toolruntime.Layer2Registry
}

func initTools(cfg config.Config, capabilities *capregistry.Registry, artifactStore artifact.Store) (toolsResult, error) {
	result, err := toolregistry.BuildBase(context.Background(), cfg, capabilities, artifactStore)
	if err != nil {
		return toolsResult{}, err
	}
	return toolsResult{
		Executor: result.Executor,
		Builtins: result.Builtins,
		Layer2:   result.Layer2,
	}, nil
}

func initArtifacts(cfg config.Config) (artifact.Store, error) {
	if !enabledOrDefault(cfg.Runtime.Artifacts.Enabled, true) {
		return nil, nil
	}
	path := strings.TrimSpace(cfg.Runtime.Artifacts.Path)
	switch strings.TrimSpace(strings.ToLower(cfg.Store.Backend)) {
	case "jsonl":
		if path == "" {
			path = filepath.Join(cfg.Store.Path, "artifacts")
		}
	default:
		if path == "" {
			return artifact.NewInMemoryStore(), nil
		}
	}
	return artifact.NewFileStore(path)
}

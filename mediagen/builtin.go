package mediagen

import (
	"os"
	"slices"
	"strings"
	"sync"

	"github.com/fulcrus/hopclaw/config"
	"github.com/fulcrus/hopclaw/model"
)

type BuiltinProviderBuilder func(model.ProviderEntry) (Provider, error)

var (
	builtinProviderBuildersMu sync.RWMutex
	builtinProviderBuilders   = map[string]BuiltinProviderBuilder{}
)

func RegisterBuiltinProviderBuilder(name string, build BuiltinProviderBuilder) {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" || build == nil {
		return
	}
	builtinProviderBuildersMu.Lock()
	builtinProviderBuilders[trimmed] = build
	builtinProviderBuildersMu.Unlock()
}

func builtinProviderBuilder(name string) (BuiltinProviderBuilder, bool) {
	builtinProviderBuildersMu.RLock()
	build, ok := builtinProviderBuilders[strings.TrimSpace(name)]
	builtinProviderBuildersMu.RUnlock()
	return build, ok
}

func BuildBuiltinRegistry(models config.ModelsConfig) *Registry {
	entries := configuredProviderEntries(models)
	if len(entries) == 0 {
		return nil
	}
	registry := NewRegistry()
	for _, name := range orderedProviderNames(models.DefaultProvider, entries) {
		entry := entries[name]
		build, ok := builtinProviderBuilder(name)
		if !ok {
			continue
		}
		if provider, err := build(entry); err == nil {
			registry.Register(provider)
		}
	}
	if registry.Empty() {
		return nil
	}
	return registry
}

func configuredProviderEntries(models config.ModelsConfig) map[string]model.ProviderEntry {
	providers := make(map[string]model.ProviderEntry)
	for name, cfg := range models.Providers {
		entry := config.ProviderEntryFromConfig(name, cfg)
		if len(entry.ResolveKeys()) == 0 {
			continue
		}
		providers[strings.TrimSpace(name)] = entry
	}
	providers = model.MergeWithCatalog(providers)
	for _, detected := range config.DetectAPIKeys() {
		name := strings.TrimSpace(detected.Provider)
		if name == "" {
			continue
		}
		if _, exists := providers[name]; exists {
			continue
		}
		catalog, ok := model.CatalogLookup(name)
		if !ok {
			continue
		}
		entry := catalog.Provider
		entry.APIKey = detected.Key
		providers[name] = entry
	}
	if _, exists := providers["fal"]; !exists {
		if key := strings.TrimSpace(firstNonEmpty(os.Getenv("FAL_KEY"), os.Getenv("FAL_API_KEY"))); key != "" {
			providers["fal"] = model.ProviderEntry{
				BaseURL: falDefaultBaseURL,
				APIKey:  key,
			}
		}
	}
	if _, exists := providers["runway"]; !exists {
		if key := strings.TrimSpace(firstNonEmpty(os.Getenv("RUNWAYML_API_SECRET"), os.Getenv("RUNWAY_API_KEY"))); key != "" {
			providers["runway"] = model.ProviderEntry{
				BaseURL: runwayDefaultBaseURL,
				APIKey:  key,
			}
		}
	}
	return providers
}

func orderedProviderNames(defaultProvider string, entries map[string]model.ProviderEntry) []string {
	if len(entries) == 0 {
		return nil
	}
	names := make([]string, 0, len(entries))
	for name := range entries {
		if _, ok := builtinProviderBuilder(name); ok {
			names = append(names, name)
		}
	}
	if len(names) == 0 {
		return nil
	}
	slices.Sort(names)
	defaultProvider = strings.TrimSpace(defaultProvider)
	if defaultProvider != "" {
		if index := slices.Index(names, defaultProvider); index > 0 {
			names[0], names[index] = names[index], names[0]
		}
	}
	return names
}

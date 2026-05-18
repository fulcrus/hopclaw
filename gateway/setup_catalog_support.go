package gateway

import (
	"fmt"
	"strings"

	"github.com/fulcrus/hopclaw/config"
	"github.com/fulcrus/hopclaw/internal/modules"
	"github.com/fulcrus/hopclaw/model"
)

func (g *Gateway) currentSetupCatalog() config.OperatorSetupCatalog {
	catalog := config.CurrentOperatorSetupCatalog()
	if g == nil {
		return catalog
	}
	if extras := moduleSetupProviderProfiles(g.moduleCatalog); len(extras) > 0 {
		catalog.Providers = mergeSetupProviderProfiles(catalog.Providers, extras)
	}
	return catalog
}

func moduleSetupProviderProfiles(store *modules.Store) []config.SetupProviderProfile {
	if store == nil {
		return nil
	}
	projections := store.ProviderProjections()
	if len(projections) == 0 {
		return nil
	}
	out := make([]config.SetupProviderProfile, 0, len(projections))
	for _, projection := range projections {
		if projection.Source != modules.SourcePlugin {
			continue
		}
		api := canonicalProviderAPI(projection.API)
		if api == "" {
			api = string(model.APIOpenAICompletions)
		}
		if _, ok := config.LookupSetupProviderAPIProfile(api); !ok {
			continue
		}
		profile := config.SetupProviderProfile{
			ID:          projection.Name,
			DisplayName: projection.Name,
			Description: pluginProviderCatalogDescription(projection.ModuleName, projection.LocalName, api),
			API:         api,
			BaseURL:     strings.TrimSpace(projection.BaseURL),
			EnvVars:     append([]string(nil), projection.EnvVars...),
			APIKeyHint:  strings.TrimSpace(projection.APIKeyHint),
		}
		if defaultModel := strings.TrimSpace(projection.DefaultModel); defaultModel != "" {
			profile.DefaultModels = []string{defaultModel}
		}
		profile.CapabilityMatrix = model.CapabilityMatrixForCatalogEntry(
			profile.ID,
			model.ProviderAPI(api),
			strings.TrimSpace(projection.DefaultModel),
		)
		out = append(out, profile)
	}
	return out
}

func pluginProviderCatalogDescription(pluginName, providerName, api string) string {
	pluginName = strings.TrimSpace(pluginName)
	providerName = strings.TrimSpace(providerName)
	api = strings.TrimSpace(api)
	if api == "" {
		api = "openai-completions"
	}
	switch {
	case pluginName == "" && providerName == "":
		return fmt.Sprintf("Provider contributed by a plugin using %s", api)
	case pluginName == "":
		return fmt.Sprintf("Plugin provider %s using %s", providerName, api)
	case providerName == "":
		return fmt.Sprintf("Provider contributed by plugin %s using %s", pluginName, api)
	default:
		return fmt.Sprintf("Plugin provider %s from %s using %s", providerName, pluginName, api)
	}
}

func mergeSetupProviderProfiles(base, extras []config.SetupProviderProfile) []config.SetupProviderProfile {
	if len(extras) == 0 {
		return base
	}

	out := make([]config.SetupProviderProfile, len(base))
	copy(out, base)
	indexByID := make(map[string]int, len(out))
	for i, profile := range out {
		indexByID[strings.TrimSpace(strings.ToLower(profile.ID))] = i
	}

	for _, extra := range extras {
		key := strings.TrimSpace(strings.ToLower(extra.ID))
		if key == "" {
			continue
		}
		index, ok := indexByID[key]
		if !ok {
			indexByID[key] = len(out)
			out = append(out, extra)
			continue
		}
		out[index] = mergeSetupProviderProfile(out[index], extra)
	}
	return out
}

func mergeSetupProviderProfile(base, extra config.SetupProviderProfile) config.SetupProviderProfile {
	merged := base
	if strings.TrimSpace(merged.DisplayName) == "" {
		merged.DisplayName = strings.TrimSpace(extra.DisplayName)
	}
	if strings.TrimSpace(merged.Description) == "" {
		merged.Description = strings.TrimSpace(extra.Description)
	}
	if strings.TrimSpace(merged.API) == "" {
		merged.API = strings.TrimSpace(extra.API)
	}
	if strings.TrimSpace(merged.BaseURL) == "" {
		merged.BaseURL = strings.TrimSpace(extra.BaseURL)
	}
	if len(merged.DefaultModels) == 0 && len(extra.DefaultModels) > 0 {
		merged.DefaultModels = append([]string(nil), extra.DefaultModels...)
	}
	merged.EnvVars = mergeStringLists(merged.EnvVars, extra.EnvVars)
	if strings.TrimSpace(merged.APIKeyHint) == "" {
		merged.APIKeyHint = strings.TrimSpace(extra.APIKeyHint)
	}
	if merged.CapabilityMatrix.ProviderAPI == "" {
		merged.CapabilityMatrix = extra.CapabilityMatrix
	}
	return merged
}

func mergeStringLists(left, right []string) []string {
	if len(left) == 0 && len(right) == 0 {
		return nil
	}
	out := make([]string, 0, len(left)+len(right))
	seen := make(map[string]struct{}, len(left)+len(right))
	for _, value := range append(append([]string(nil), left...), right...) {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

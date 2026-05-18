package cli

import (
	"context"
	"strings"

	"github.com/fulcrus/hopclaw/config"
)

const setupCatalogPath = "/operator/setup/catalog"

type cliSetupCatalog struct {
	catalog config.OperatorSetupCatalog
}

func localCLISetupCatalog() cliSetupCatalog {
	return cliSetupCatalog{
		catalog: config.CurrentOperatorSetupCatalog(),
	}
}

func loadCLISetupCatalogBestEffort(ctx context.Context) cliSetupCatalog {
	client, _ := NewGatewayClient()
	return loadCLISetupCatalog(ctx, client)
}

func loadCLISetupCatalog(ctx context.Context, client *GatewayClient) cliSetupCatalog {
	fallback := localCLISetupCatalog()
	if client == nil {
		return fallback
	}

	var remote config.OperatorSetupCatalog
	if err := client.Get(ctx, setupCatalogPath, &remote); err != nil {
		return fallback
	}
	if len(remote.AuthModes) == 0 && len(remote.Providers) == 0 && len(remote.ProviderAPIs) == 0 && len(remote.Channels) == 0 {
		return fallback
	}
	return cliSetupCatalog{catalog: cloneOperatorSetupCatalog(remote)}
}

func (c cliSetupCatalog) AuthModeProfiles() []config.AuthModeProfile {
	out := make([]config.AuthModeProfile, len(c.catalog.AuthModes))
	copy(out, c.catalog.AuthModes)
	return out
}

func (c cliSetupCatalog) LookupAuthModeProfile(mode string) (config.AuthModeProfile, bool) {
	mode = strings.TrimSpace(strings.ToLower(mode))
	for _, profile := range c.catalog.AuthModes {
		if profile.ID == mode {
			return profile, true
		}
	}
	return config.AuthModeProfile{}, false
}

func (c cliSetupCatalog) ProviderProfiles() []config.SetupProviderProfile {
	out := make([]config.SetupProviderProfile, len(c.catalog.Providers))
	for i, profile := range c.catalog.Providers {
		out[i] = cloneSetupProviderProfile(profile)
	}
	return out
}

func (c cliSetupCatalog) LookupProviderProfile(provider string) (config.SetupProviderProfile, bool) {
	provider = strings.TrimSpace(strings.ToLower(provider))
	for _, profile := range c.catalog.Providers {
		if profile.ID == provider {
			return cloneSetupProviderProfile(profile), true
		}
	}
	return config.SetupProviderProfile{}, false
}

func (c cliSetupCatalog) DefaultProviderAPI(provider string) string {
	if profile, ok := c.LookupProviderProfile(provider); ok {
		return strings.TrimSpace(profile.API)
	}
	return ""
}

func (c cliSetupCatalog) DefaultModelsForProvider(provider string) []string {
	if profile, ok := c.LookupProviderProfile(provider); ok {
		return append([]string(nil), profile.DefaultModels...)
	}
	return nil
}

func (c cliSetupCatalog) DefaultModelForProvider(provider string) string {
	models := c.DefaultModelsForProvider(provider)
	if len(models) == 0 {
		return ""
	}
	return strings.TrimSpace(models[0])
}

func (c cliSetupCatalog) DefaultBaseURL(provider string) string {
	if profile, ok := c.LookupProviderProfile(provider); ok {
		return strings.TrimSpace(profile.BaseURL)
	}
	return ""
}

func (c cliSetupCatalog) ProviderDisplayName(provider string) string {
	if profile, ok := c.LookupProviderProfile(provider); ok && strings.TrimSpace(profile.DisplayName) != "" {
		return strings.TrimSpace(profile.DisplayName)
	}
	return strings.TrimSpace(provider)
}

func (c cliSetupCatalog) ProviderAPIKeyHint(provider string) string {
	if profile, ok := c.LookupProviderProfile(provider); ok && strings.TrimSpace(profile.APIKeyHint) != "" {
		return strings.TrimSpace(profile.APIKeyHint)
	}
	return "Enter your API key"
}

func (c cliSetupCatalog) ProviderAPIProfiles() []config.ProviderAPIProfile {
	out := make([]config.ProviderAPIProfile, len(c.catalog.ProviderAPIs))
	for i, profile := range c.catalog.ProviderAPIs {
		out[i] = cloneProviderAPIProfile(profile)
	}
	return out
}

func (c cliSetupCatalog) LookupProviderAPIProfile(api string) (config.ProviderAPIProfile, bool) {
	api = strings.TrimSpace(strings.ToLower(api))
	for _, profile := range c.catalog.ProviderAPIs {
		if profile.ID == api {
			return cloneProviderAPIProfile(profile), true
		}
	}
	return config.ProviderAPIProfile{}, false
}

func (c cliSetupCatalog) ProviderAPIFieldDefault(api, fieldID string) string {
	profile, ok := c.LookupProviderAPIProfile(api)
	if !ok {
		return ""
	}
	fieldID = strings.TrimSpace(fieldID)
	for _, field := range profile.Fields {
		if strings.TrimSpace(field.ID) != fieldID {
			continue
		}
		if value := strings.TrimSpace(field.DefaultValue); value != "" {
			return value
		}
		if value := strings.TrimSpace(field.Placeholder); value != "" {
			return value
		}
		return ""
	}
	return ""
}

func (c cliSetupCatalog) ChannelProfiles() []config.ChannelProfile {
	out := make([]config.ChannelProfile, len(c.catalog.Channels))
	for i, profile := range c.catalog.Channels {
		out[i] = cloneChannelProfile(profile)
	}
	return out
}

func (c cliSetupCatalog) SetupChannelProfiles() []config.ChannelProfile {
	return c.filterChannelProfiles(func(profile config.ChannelProfile) bool { return profile.SetupSupported })
}

func (c cliSetupCatalog) OnboardingChannelProfiles() []config.ChannelProfile {
	return c.filterChannelProfiles(func(profile config.ChannelProfile) bool { return profile.OnboardingSupported })
}

func (c cliSetupCatalog) LookupChannelProfile(channel string) (config.ChannelProfile, bool) {
	normalized := normalizeCatalogChannelID(channel)
	for _, profile := range c.catalog.Channels {
		if profile.ID == normalized {
			return cloneChannelProfile(profile), true
		}
	}
	return config.ChannelProfile{}, false
}

func (c cliSetupCatalog) filterChannelProfiles(keep func(config.ChannelProfile) bool) []config.ChannelProfile {
	out := make([]config.ChannelProfile, 0, len(c.catalog.Channels))
	for _, profile := range c.catalog.Channels {
		if keep(profile) {
			out = append(out, cloneChannelProfile(profile))
		}
	}
	return out
}

func cloneOperatorSetupCatalog(in config.OperatorSetupCatalog) config.OperatorSetupCatalog {
	out := in
	out.AuthModes = append([]config.AuthModeProfile(nil), in.AuthModes...)
	out.Providers = make([]config.SetupProviderProfile, len(in.Providers))
	for i, profile := range in.Providers {
		out.Providers[i] = cloneSetupProviderProfile(profile)
	}
	out.ProviderAPIs = make([]config.ProviderAPIProfile, len(in.ProviderAPIs))
	for i, profile := range in.ProviderAPIs {
		out.ProviderAPIs[i] = cloneProviderAPIProfile(profile)
	}
	out.Channels = make([]config.ChannelProfile, len(in.Channels))
	for i, profile := range in.Channels {
		out.Channels[i] = cloneChannelProfile(profile)
	}
	return out
}

func cloneSetupProviderProfile(profile config.SetupProviderProfile) config.SetupProviderProfile {
	out := profile
	out.DefaultModels = append([]string(nil), profile.DefaultModels...)
	out.EnvVars = append([]string(nil), profile.EnvVars...)
	return out
}

func cloneProviderAPIProfile(profile config.ProviderAPIProfile) config.ProviderAPIProfile {
	out := profile
	out.Fields = append([]config.SetupProviderField(nil), profile.Fields...)
	return out
}

func cloneChannelProfile(profile config.ChannelProfile) config.ChannelProfile {
	out := profile
	out.Fields = append([]config.SetupChannelField(nil), profile.Fields...)
	out.OperatorFields = append([]config.SetupChannelField(nil), profile.OperatorFields...)
	return out
}

func normalizeCatalogChannelID(channel string) string {
	normalized := strings.TrimSpace(strings.ToLower(channel))
	return strings.ReplaceAll(normalized, "-", "_")
}

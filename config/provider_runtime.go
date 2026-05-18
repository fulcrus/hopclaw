package config

import (
	"net/http"
	"strings"

	"github.com/fulcrus/hopclaw/model"
)

// NormalizeProviderConfig canonicalizes provider API aliases and hydrates the
// API type from the built-in provider catalog when the provider name is known.
func NormalizeProviderConfig(name string, cfg ProviderConfig) ProviderConfig {
	cfg.API = string(model.NormalizeProviderAPI(model.ProviderAPI(cfg.API)))
	cfg.APIKeys = normalizeProviderStringSlice(cfg.APIKeys)
	cfg.Fallbacks = normalizeProviderStringSlice(cfg.Fallbacks)
	cfg.Headers = normalizeProviderHeaders(cfg.Headers)
	if cfg.Timeout < 0 {
		cfg.Timeout = 0
	}
	if strings.TrimSpace(cfg.API) != "" {
		return cfg
	}
	catalog, ok := model.CatalogLookup(strings.TrimSpace(name))
	if !ok {
		return cfg
	}
	cfg.API = string(model.NormalizeProviderAPI(catalog.Provider.API))
	return cfg
}

func ProviderEntryFromConfig(name string, cfg ProviderConfig) model.ProviderEntry {
	cfg = NormalizeProviderConfig(name, cfg)
	return model.ProviderEntry{
		API:          model.NormalizeProviderAPI(model.ProviderAPI(cfg.API)),
		BaseURL:      strings.TrimSpace(cfg.BaseURL),
		Region:       strings.TrimSpace(cfg.Region),
		APIKey:       strings.TrimSpace(cfg.APIKey),
		APIKeys:      append([]string(nil), cfg.APIKeys...),
		Fallbacks:    append([]string(nil), cfg.Fallbacks...),
		AccessKeyID:  strings.TrimSpace(cfg.AccessKeyID),
		SecretKey:    strings.TrimSpace(cfg.SecretKey),
		SessionToken: strings.TrimSpace(cfg.SessionToken),
		DefaultModel: strings.TrimSpace(cfg.DefaultModel),
		Timeout:      cfg.Timeout,
		Headers:      cloneProviderStringMap(cfg.Headers),
	}
}

func OpenAICompatProviderEntry(cfg OpenAICompatConfig) (model.ProviderEntry, bool) {
	if strings.TrimSpace(cfg.BaseURL) == "" {
		return model.ProviderEntry{}, false
	}
	return model.ProviderEntry{
		API:          model.APIOpenAICompletions,
		BaseURL:      strings.TrimSpace(cfg.BaseURL),
		APIKey:       strings.TrimSpace(cfg.APIKey),
		Fallbacks:    normalizeProviderStringSlice(cfg.Fallbacks),
		DefaultModel: strings.TrimSpace(cfg.Model),
		Timeout:      cfg.Timeout,
		Headers:      cloneProviderStringMap(cfg.Headers),
	}, true
}

func ProviderConfigHasCredentials(cfg ProviderConfig) bool {
	return strings.TrimSpace(cfg.APIKey) != "" ||
		len(cfg.APIKeys) > 0 ||
		(strings.TrimSpace(cfg.AccessKeyID) != "" && strings.TrimSpace(cfg.SecretKey) != "")
}

func cloneProviderStringMap(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func normalizeProviderStringSlice(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	out := make([]string, 0, len(in))
	for _, item := range in {
		trimmed := strings.TrimSpace(item)
		if trimmed == "" {
			continue
		}
		out = append(out, trimmed)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func normalizeProviderHeaders(headers map[string]string) map[string]string {
	if len(headers) == 0 {
		return nil
	}
	out := make(map[string]string, len(headers))
	for key, value := range headers {
		name := http.CanonicalHeaderKey(strings.TrimSpace(key))
		if name == "" {
			continue
		}
		out[name] = strings.TrimSpace(value)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

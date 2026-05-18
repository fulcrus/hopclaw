package config

import (
	"encoding/json"
	"strings"
)

// SectionOverlayName returns the top-level config section encoded in a dynamic
// setting overlay key.
func SectionOverlayName(key string) (string, bool) {
	return overlaySectionName(key)
}

// SanitizeProviderConfigForOperator returns an operator-safe provider config.
// Literal secret values are replaced with preserve markers, while env/keychain
// references remain visible.
func SanitizeProviderConfigForOperator(name string, provider ProviderConfig) ProviderConfig {
	name = strings.TrimSpace(name)
	cfg := Config{
		Models: ModelsConfig{
			Providers: map[string]ProviderConfig{
				name: NormalizeProviderConfig(name, provider),
			},
		},
	}
	sanitized := cfg.SanitizeForOperator()
	return sanitized.Models.Providers[name]
}

// SanitizeStoredChannelConfigForOperator returns an operator-safe channel
// payload using the stored config blob itself rather than the current effective
// root config.
func SanitizeStoredChannelConfigForOperator(name string, raw json.RawMessage) (json.RawMessage, error) {
	name = strings.TrimSpace(name)
	if canonical, ok := canonicalChannelName(name); ok {
		cfg, err := decodeChannelConfig(canonical, raw)
		if err != nil {
			return nil, err
		}
		return marshalKnownChannelConfig(cfg.SanitizeForOperator(), canonical)
	}
	return sanitizeUnknownChannelConfigForOperator(raw)
}

// SanitizeSectionValueForOperator returns an operator-safe section payload using
// the stored overlay value rather than the current effective root config.
func SanitizeSectionValueForOperator(section string, value any) (any, error) {
	section = strings.TrimSpace(section)
	if section == "" {
		return value, nil
	}
	cfg, err := decodeSectionValueIntoConfig(section, value)
	if err != nil {
		return nil, err
	}
	sanitized := cfg.SanitizeForOperator()
	extracted, err := ExtractSection(sanitized, section)
	if err != nil {
		return nil, err
	}
	return canonicalSectionPayload(extracted)
}

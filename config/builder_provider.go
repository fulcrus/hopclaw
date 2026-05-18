package config

import (
	"fmt"
	"sort"
	"strings"
)

type setupProviderConfig struct {
	AgentDefaultModel string
	ModelsSection     string
}

func resolveSetupProviderConfig(opts SetupOptions) (setupProviderConfig, error) {
	provider := strings.TrimSpace(opts.Provider)
	if provider == "" {
		return setupProviderConfig{
			AgentDefaultModel: "unconfigured-model",
		}, nil
	}

	api := strings.TrimSpace(opts.ProviderAPI)
	if api == "" {
		api = strings.TrimSpace(DefaultProviderAPI(provider))
	}
	if api == "" {
		api = "openai-completions"
	}

	values := normalizeSetupProviderValues(opts)
	if values["base_url"] == "" {
		values["base_url"] = strings.TrimSpace(DefaultBaseURL(provider))
	}
	if values["default_model"] == "" {
		values["default_model"] = strings.TrimSpace(DefaultModelForProvider(provider))
	}
	if provider == "ollama" && values["api_key"] == "" {
		values["api_key"] = "ollama"
	}

	useCompatRoot := providerUsesCompatRoot(provider)
	if provider == "custom" && strings.TrimSpace(values["base_url"]) == "" {
		return setupProviderConfig{}, fmt.Errorf("custom provider requires a base URL")
	}

	apiProfile, ok := LookupSetupProviderAPIProfile(api)
	if ok {
		for _, field := range apiProfile.Fields {
			value := values[field.ID]
			switch SetupProviderFieldType(field) {
			case "string_list":
				value = strings.Join(SplitSetupProviderFieldList(value), "\n")
			case "string_map":
				items, err := ParseSetupProviderFieldMap(value)
				if err != nil {
					return setupProviderConfig{}, fmt.Errorf("%s is invalid: %w", field.Label, err)
				}
				value = renderSetupProviderFieldMapValue(items)
			default:
				value = strings.TrimSpace(value)
			}
			values[field.ID] = value
			if field.Required && !SetupProviderFieldHasValue(field, value) {
				return setupProviderConfig{}, fmt.Errorf("%s requires %s", ProviderDisplayName(provider), field.Label)
			}
		}
		if useCompatRoot {
			useCompatRoot = canRenderSetupOpenAICompat(apiProfile.Fields, values)
		}
	}

	defaultModel := strings.TrimSpace(values["default_model"])
	if defaultModel == "" {
		return setupProviderConfig{}, fmt.Errorf("%s requires a default model", ProviderDisplayName(provider))
	}

	agentDefaultModel := defaultModel
	if !providerUsesCompatDefaultModel(provider) {
		agentDefaultModel = provider + "/" + defaultModel
	}
	modelsSection, err := renderSetupModelsSection(provider, api, values, useCompatRoot)
	if err != nil {
		return setupProviderConfig{}, err
	}
	return setupProviderConfig{
		AgentDefaultModel: agentDefaultModel,
		ModelsSection:     modelsSection,
	}, nil
}

func normalizeSetupProviderValues(opts SetupOptions) map[string]string {
	values := copyProviderValues(opts.ProviderValues)
	if value := strings.TrimSpace(opts.APIKey); value != "" && values["api_key"] == "" {
		values["api_key"] = value
	}
	if value := strings.TrimSpace(opts.BaseURL); value != "" && values["base_url"] == "" {
		values["base_url"] = value
	}
	if value := strings.TrimSpace(opts.Model); value != "" && values["default_model"] == "" {
		values["default_model"] = value
	}
	return values
}

func renderSetupModelsSection(provider, api string, values map[string]string, useCompatRoot bool) (string, error) {
	provider = strings.TrimSpace(provider)
	api = strings.TrimSpace(api)
	if provider == "" {
		return "", fmt.Errorf("provider is required")
	}

	apiProfile, _ := LookupSetupProviderAPIProfile(api)
	var buf strings.Builder
	if useCompatRoot {
		buf.WriteString("  openai_compat:\n")
		if err := renderSetupProviderField(&buf, "    ", "base_url", SetupProviderField{ID: "base_url", Type: "url"}, values["base_url"]); err != nil {
			return "", err
		}
		if err := renderSetupProviderField(&buf, "    ", "api_key", SetupProviderField{ID: "api_key", Secret: true}, values["api_key"]); err != nil {
			return "", err
		}
		if err := renderSetupProviderField(&buf, "    ", "headers", SetupProviderField{ID: "headers", Type: "string_map"}, values["headers"]); err != nil {
			return "", err
		}
		if err := renderSetupProviderField(&buf, "    ", "timeout", SetupProviderField{ID: "timeout", Type: "duration"}, values["timeout"]); err != nil {
			return "", err
		}
		if err := renderSetupProviderField(&buf, "    ", "model", SetupProviderField{ID: "default_model"}, values["default_model"]); err != nil {
			return "", err
		}
		return buf.String(), nil
	}

	buf.WriteString("  default_provider: " + provider + "\n")
	buf.WriteString("  providers:\n")
	buf.WriteString("    " + provider + ":\n")
	buf.WriteString("      api: " + api + "\n")
	for _, field := range apiProfile.Fields {
		if field.ID == "" || field.ID == "api" {
			continue
		}
		if err := renderSetupProviderField(&buf, "      ", field.ID, field, values[field.ID]); err != nil {
			return "", err
		}
	}
	return buf.String(), nil
}

func providerUsesCompatRoot(provider string) bool {
	switch strings.TrimSpace(provider) {
	case "openai", "ollama", "custom":
		return true
	default:
		return false
	}
}

func providerUsesCompatDefaultModel(provider string) bool {
	switch strings.TrimSpace(provider) {
	case "openai", "ollama", "custom":
		return true
	default:
		return false
	}
}

func canRenderSetupOpenAICompat(fields []SetupProviderField, values map[string]string) bool {
	for _, field := range fields {
		if !SetupProviderFieldHasValue(field, values[field.ID]) {
			continue
		}
		switch field.ID {
		case "base_url", "api_key", "default_model", "timeout", "headers":
			continue
		default:
			return false
		}
	}
	return true
}

func renderSetupProviderField(buf *strings.Builder, indent, configKey string, field SetupProviderField, raw string) error {
	if !SetupProviderFieldHasValue(field, raw) {
		return nil
	}
	switch SetupProviderFieldType(field) {
	case "string_list":
		items := SplitSetupProviderFieldList(raw)
		if len(items) == 0 {
			return nil
		}
		buf.WriteString(indent + configKey + ":\n")
		for _, item := range items {
			buf.WriteString(indent + "  - " + yamlQuoteString(item) + "\n")
		}
	case "string_map":
		items, err := ParseSetupProviderFieldMap(raw)
		if err != nil {
			return fmt.Errorf("%s is invalid: %w", field.Label, err)
		}
		if len(items) == 0 {
			return nil
		}
		keys := make([]string, 0, len(items))
		for key := range items {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		buf.WriteString(indent + configKey + ":\n")
		for _, key := range keys {
			buf.WriteString(indent + "  " + yamlQuoteString(key) + ": " + yamlQuoteString(items[key]) + "\n")
		}
	default:
		value := strings.TrimSpace(raw)
		if value == "" {
			return nil
		}
		buf.WriteString(indent + configKey + ": " + yamlQuoteString(value) + "\n")
	}
	return nil
}

func renderSetupProviderFieldMapValue(items map[string]string) string {
	if len(items) == 0 {
		return ""
	}
	keys := make([]string, 0, len(items))
	for key := range items {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	lines := make([]string, 0, len(keys))
	for _, key := range keys {
		line := key + ":"
		if value := strings.TrimSpace(items[key]); value != "" {
			line += " " + value
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}

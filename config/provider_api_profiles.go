package config

import (
	"strings"

	"github.com/fulcrus/hopclaw/model"
)

type SetupProviderField struct {
	ID           string `json:"id"`
	Label        string `json:"label"`
	Description  string `json:"description,omitempty"`
	Type         string `json:"type,omitempty"`
	Required     bool   `json:"required"`
	Secret       bool   `json:"secret,omitempty"`
	Advanced     bool   `json:"advanced,omitempty"`
	DefaultValue string `json:"default_value,omitempty"`
	Placeholder  string `json:"placeholder,omitempty"`
}

type ProviderAPIProfile struct {
	ID               string                 `json:"id"`
	DisplayName      string                 `json:"display_name"`
	Description      string                 `json:"description,omitempty"`
	Fields           []SetupProviderField   `json:"fields,omitempty"`
	CapabilityMatrix model.CapabilityMatrix `json:"capability_matrix,omitempty"`
}

var providerAPIProfiles = []ProviderAPIProfile{
	{
		ID:          "openai-completions",
		DisplayName: "OpenAI-compatible Completions",
		Description: "Chat/completions style endpoints used by OpenAI-compatible providers and gateways",
		Fields: []SetupProviderField{
			{ID: "base_url", Label: "Base URL", Type: "url", Placeholder: "https://api.openai.com/v1"},
			{ID: "api_key", Label: "API Key", Required: true, Secret: true, Placeholder: "Enter your API key"},
			{ID: "api_keys", Label: "API Key Pool", Type: "string_list", Advanced: true, Description: "Optional fallback keys. Enter one key per line to replace the active key pool."},
			{ID: "headers", Label: "Extra Headers", Type: "string_map", Advanced: true, Description: "Optional HTTP headers. Enter one header per line using Header-Name: value."},
			{ID: "timeout", Label: "Request Timeout", Type: "duration", Advanced: true, Description: "Optional Go duration such as 30s, 90s, or 2m."},
			{ID: "default_model", Label: "Default Model", Placeholder: "gpt-4o"},
		},
	},
	{
		ID:          "openai-responses",
		DisplayName: "OpenAI Responses API",
		Description: "Responses API surface for newer GPT-style models",
		Fields: []SetupProviderField{
			{ID: "base_url", Label: "Base URL", Type: "url", Placeholder: "https://api.openai.com/v1"},
			{ID: "api_key", Label: "API Key", Required: true, Secret: true, Placeholder: "Enter your API key"},
			{ID: "api_keys", Label: "API Key Pool", Type: "string_list", Advanced: true, Description: "Optional fallback keys. Enter one key per line to replace the active key pool."},
			{ID: "headers", Label: "Extra Headers", Type: "string_map", Advanced: true, Description: "Optional HTTP headers. Enter one header per line using Header-Name: value."},
			{ID: "timeout", Label: "Request Timeout", Type: "duration", Advanced: true, Description: "Optional Go duration such as 30s, 90s, or 2m."},
			{ID: "default_model", Label: "Default Model", Placeholder: "gpt-4.1"},
		},
	},
	{
		ID:          "anthropic-messages",
		DisplayName: "Anthropic Messages",
		Description: "Anthropic-compatible messages endpoint",
		Fields: []SetupProviderField{
			{ID: "base_url", Label: "Base URL", Type: "url", Placeholder: "https://api.anthropic.com"},
			{ID: "api_key", Label: "API Key", Required: true, Secret: true, Placeholder: "Enter your API key"},
			{ID: "api_keys", Label: "API Key Pool", Type: "string_list", Advanced: true, Description: "Optional fallback keys. Enter one key per line to replace the active key pool."},
			{ID: "headers", Label: "Extra Headers", Type: "string_map", Advanced: true, Description: "Optional HTTP headers. Enter one header per line using Header-Name: value."},
			{ID: "timeout", Label: "Request Timeout", Type: "duration", Advanced: true, Description: "Optional Go duration such as 30s, 90s, or 2m."},
			{ID: "default_model", Label: "Default Model", Placeholder: "claude-sonnet-4-20250514"},
		},
	},
	{
		ID:          "google-generative-ai",
		DisplayName: "Google Generative AI",
		Description: "Gemini models over the Google Generative AI API",
		Fields: []SetupProviderField{
			{ID: "base_url", Label: "Base URL", Type: "url", Placeholder: "https://generativelanguage.googleapis.com"},
			{ID: "api_key", Label: "API Key", Required: true, Secret: true, Placeholder: "Enter your Google AI API key"},
			{ID: "api_keys", Label: "API Key Pool", Type: "string_list", Advanced: true, Description: "Optional fallback keys. Enter one key per line to replace the active key pool."},
			{ID: "headers", Label: "Extra Headers", Type: "string_map", Advanced: true, Description: "Optional HTTP headers. Enter one header per line using Header-Name: value."},
			{ID: "timeout", Label: "Request Timeout", Type: "duration", Advanced: true, Description: "Optional Go duration such as 30s, 90s, or 2m."},
			{ID: "default_model", Label: "Default Model", Placeholder: "gemini-2.0-flash"},
		},
	},
	{
		ID:          "bedrock-converse",
		DisplayName: "AWS Bedrock Converse",
		Description: "AWS Bedrock Converse API with SigV4 credentials",
		Fields: []SetupProviderField{
			{ID: "region", Label: "AWS Region", Required: true, Placeholder: "us-east-1"},
			{ID: "access_key_id", Label: "Access Key ID", Required: true, Placeholder: "AKIA..."},
			{ID: "secret_key", Label: "Secret Access Key", Required: true, Secret: true, Placeholder: "Enter your AWS secret access key"},
			{ID: "session_token", Label: "Session Token", Secret: true, Placeholder: "Optional temporary session token"},
			{ID: "timeout", Label: "Request Timeout", Type: "duration", Advanced: true, Description: "Optional Go duration such as 30s, 90s, or 2m."},
			{ID: "default_model", Label: "Default Model", Placeholder: "anthropic.claude-3-5-sonnet-20241022-v2:0"},
		},
	},
	{
		ID:          "ollama",
		DisplayName: "Ollama",
		Description: "Local Ollama runtime over the OpenAI-compatible endpoint",
		Fields: []SetupProviderField{
			{ID: "base_url", Label: "Base URL", Type: "url", Required: true, DefaultValue: "http://127.0.0.1:11434/v1", Placeholder: "http://127.0.0.1:11434/v1"},
			{ID: "headers", Label: "Extra Headers", Type: "string_map", Advanced: true, Description: "Optional HTTP headers. Enter one header per line using Header-Name: value."},
			{ID: "timeout", Label: "Request Timeout", Type: "duration", Advanced: true, Description: "Optional Go duration such as 30s, 90s, or 2m."},
			{ID: "default_model", Label: "Default Model", Placeholder: "llama3.3"},
		},
	},
	{
		ID:          "github-copilot",
		DisplayName: "GitHub Copilot",
		Description: "GitHub Copilot chat API using a GitHub token when needed",
		Fields: []SetupProviderField{
			{ID: "api_key", Label: "GitHub Token", Secret: true, Placeholder: "ghp_... (optional when env vars are already configured)"},
			{ID: "api_keys", Label: "Token Pool", Type: "string_list", Advanced: true, Description: "Optional fallback tokens. Enter one token per line to replace the active token pool."},
			{ID: "headers", Label: "Extra Headers", Type: "string_map", Advanced: true, Description: "Optional HTTP headers. Enter one header per line using Header-Name: value."},
			{ID: "timeout", Label: "Request Timeout", Type: "duration", Advanced: true, Description: "Optional Go duration such as 30s, 90s, or 2m."},
			{ID: "default_model", Label: "Default Model", Placeholder: "gpt-4o"},
		},
	},
}

func SetupProviderAPIProfiles() []ProviderAPIProfile {
	out := make([]ProviderAPIProfile, len(providerAPIProfiles))
	for i, profile := range providerAPIProfiles {
		out[i] = hydrateProviderAPIProfile(profile)
	}
	return out
}

func LookupSetupProviderAPIProfile(api string) (ProviderAPIProfile, bool) {
	api = strings.TrimSpace(strings.ToLower(api))
	for _, profile := range providerAPIProfiles {
		if profile.ID == api {
			return hydrateProviderAPIProfile(profile), true
		}
	}
	return ProviderAPIProfile{}, false
}

func ProviderAPIFieldDefault(api, fieldID string) string {
	profile, ok := LookupSetupProviderAPIProfile(api)
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

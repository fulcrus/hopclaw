package config

import "strings"

const DefaultGatewayAddress = "127.0.0.1:16280"

type AuthModeProfile struct {
	ID             string `json:"id"`
	DisplayName    string `json:"display_name"`
	Description    string `json:"description,omitempty"`
	Recommended    bool   `json:"recommended,omitempty"`
	CredentialKind string `json:"credential_kind,omitempty"`
}

type OperatorSetupCatalog struct {
	DefaultAddress string                 `json:"default_address"`
	AuthModes      []AuthModeProfile      `json:"auth_modes"`
	Providers      []SetupProviderProfile `json:"providers"`
	ProviderAPIs   []ProviderAPIProfile   `json:"provider_apis"`
	Channels       []ChannelProfile       `json:"channels"`
}

var authModeProfiles = []AuthModeProfile{
	{
		ID:             "bearer",
		DisplayName:    "Bearer token",
		Description:    "Shared token for local operator access",
		Recommended:    true,
		CredentialKind: "token",
	},
	{
		ID:             "apikey",
		DisplayName:    "API key",
		Description:    "Named key for local CLI or automation clients",
		CredentialKind: "api_key",
	},
	{
		ID:             "jwt",
		DisplayName:    "JWT",
		Description:    "HS256 signed tokens for structured operator auth",
		CredentialKind: "secret",
	},
	{
		ID:             "none",
		DisplayName:    "None",
		Description:    "No operator authentication. Only suitable for local development.",
		CredentialKind: "none",
	},
}

func AuthModeProfiles() []AuthModeProfile {
	out := make([]AuthModeProfile, len(authModeProfiles))
	copy(out, authModeProfiles)
	return out
}

func LookupAuthModeProfile(mode string) (AuthModeProfile, bool) {
	normalized := strings.TrimSpace(strings.ToLower(mode))
	for _, profile := range authModeProfiles {
		if profile.ID == normalized {
			return profile, true
		}
	}
	return AuthModeProfile{}, false
}

func CurrentOperatorSetupCatalog() OperatorSetupCatalog {
	return OperatorSetupCatalog{
		DefaultAddress: DefaultGatewayAddress,
		AuthModes:      AuthModeProfiles(),
		Providers:      SetupProviderProfiles(),
		ProviderAPIs:   SetupProviderAPIProfiles(),
		Channels:       ChannelProfiles(),
	}
}

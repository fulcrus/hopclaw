package config

import (
	"fmt"
	"strings"
)

func renderSetupAuthConfig(opts SetupOptions) (serverAuthToken, authSection string, err error) {
	switch strings.TrimSpace(strings.ToLower(opts.AuthMode)) {
	case "", "none":
		return "", "", nil
	case "bearer":
		token := strings.TrimSpace(opts.AuthToken)
		if token == "" {
			return "", "", fmt.Errorf("bearer auth requires a token")
		}
		return token, "", nil
	case "apikey":
		token := strings.TrimSpace(opts.AuthAPIKey)
		if token == "" {
			return "", "", fmt.Errorf("api key auth requires a key")
		}
		var buf strings.Builder
		buf.WriteString("auth:\n")
		buf.WriteString("  api_keys:\n")
		buf.WriteString("    - name: \"operator\"\n")
		buf.WriteString("      key: " + yamlQuoteString(token) + "\n")
		buf.WriteString("      enabled: true\n")
		buf.WriteString("      scopes:\n")
		buf.WriteString("        - \"*\"\n")
		return "", buf.String(), nil
	case "jwt":
		secret := strings.TrimSpace(opts.AuthJWTSecret)
		if secret == "" {
			return "", "", fmt.Errorf("jwt auth requires a secret")
		}
		var buf strings.Builder
		buf.WriteString("auth:\n")
		buf.WriteString("  jwt:\n")
		buf.WriteString("    secret: " + yamlQuoteString(secret) + "\n")
		buf.WriteString("    algorithm: \"HS256\"\n")
		return "", buf.String(), nil
	default:
		return "", "", fmt.Errorf("unsupported auth mode %q", opts.AuthMode)
	}
}

package cli

import (
	"strings"

	"github.com/fulcrus/hopclaw/config"
	"github.com/fulcrus/hopclaw/keychain"
)

func resolveConfigOperatorToken(cfg config.Config) string {
	cfg.ResolveSecrets(keychain.ResolveField)

	if token := strings.TrimSpace(cfg.Server.AuthToken); token != "" {
		return token
	}
	if token := strings.TrimSpace(cfg.Auth.BearerToken); token != "" {
		return token
	}
	for _, entry := range cfg.Auth.APIKeys {
		if entry.Enabled && strings.TrimSpace(entry.Key) != "" {
			return strings.TrimSpace(entry.Key)
		}
	}
	for _, entry := range cfg.Auth.APIKeys {
		if strings.TrimSpace(entry.Key) != "" {
			return strings.TrimSpace(entry.Key)
		}
	}
	return ""
}

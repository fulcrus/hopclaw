package cli

import (
	"fmt"
	"os"
	"strings"

	"github.com/fulcrus/hopclaw/config"
)

func checkAuthConfiguration() checkResult {
	p := resolveConfigPath()
	if p == "" {
		return checkResult{
			Category: "auth",
			Name:     "Authentication",
			Status:   "warn",
			Detail:   "no config file; authentication is not configured",
			Fix:      "run 'hopclaw setup' or configure auth.bearer_token/auth.jwt in config",
		}
	}

	cfg, err := config.Load(p)
	if err != nil {
		return checkResult{
			Category: "auth",
			Name:     "Authentication",
			Status:   "warn",
			Detail:   fmt.Sprintf("cannot load config: %v", err),
		}
	}

	var modes []string
	switch {
	case strings.TrimSpace(cfg.Auth.BearerToken) != "":
		modes = append(modes, "bearer token")
	case strings.TrimSpace(cfg.Server.AuthToken) != "":
		modes = append(modes, "server auth token")
	}
	if cfg.Auth.JWT != nil && (strings.TrimSpace(cfg.Auth.JWT.Secret) != "" || strings.TrimSpace(cfg.Auth.JWT.PublicKey) != "") {
		modes = append(modes, "jwt")
	}
	if len(cfg.Auth.APIKeys) > 0 {
		modes = append(modes, fmt.Sprintf("%d api key(s)", len(cfg.Auth.APIKeys)))
	}
	if cfg.Auth.OAuth2 != nil && strings.TrimSpace(cfg.Auth.OAuth2.ClientID) != "" {
		modes = append(modes, "oauth2")
	}
	if cfg.Auth.Session != nil && strings.TrimSpace(cfg.Auth.Session.CookieName) != "" {
		modes = append(modes, "session cookie")
	}

	if len(modes) == 0 {
		status := "warn"
		if cfg.Runtime.Profile == config.RuntimeProfileProduction {
			status = "fail"
		}
		return checkResult{
			Category: "auth",
			Name:     "Authentication",
			Status:   status,
			Detail:   fmt.Sprintf("profile=%s with no operator auth configured", cfg.Runtime.Profile),
			Fix:      "configure auth.bearer_token, auth.jwt, auth.api_keys, or auth.oauth2 in config",
		}
	}
	return checkResult{
		Category: "auth",
		Name:     "Authentication",
		Status:   "ok",
		Detail:   strings.Join(modes, ", ") + " configured",
	}
}

func checkAPIKeys() checkResult {
	provider, _ := config.DetectAPIKey()
	if provider != "" {
		return checkResult{
			Category: "auth",
			Name:     "Provider credentials",
			Status:   "ok",
			Detail:   fmt.Sprintf("%s key found in environment", provider),
		}
	}

	p := resolveConfigPath()
	if p != "" {
		cfg, err := config.Load(p)
		if err == nil {
			configured := make([]string, 0, len(cfg.Models.Providers)+1)
			if strings.TrimSpace(cfg.Models.OpenAICompat.BaseURL) != "" && strings.TrimSpace(cfg.Models.OpenAICompat.APIKey) != "" {
				configured = append(configured, "default")
			}
			for name, providerCfg := range cfg.Models.Providers {
				if config.ProviderConfigHasCredentials(providerCfg) {
					configured = append(configured, name)
				}
			}
			if len(configured) > 0 {
				return checkResult{
					Category: "auth",
					Name:     "Provider credentials",
					Status:   "ok",
					Detail:   fmt.Sprintf("%d provider credential set(s) configured", len(configured)),
				}
			}
		}
	}

	return checkResult{
		Category: "auth",
		Name:     "Provider credentials",
		Status:   "warn",
		Detail:   config.MissingAPIKeyDoctorDetail(),
		Fix:      config.MissingAPIKeyDoctorFix(),
	}
}

func checkAuthProfile() checkResult {
	p := resolveConfigPath()
	if p == "" {
		return checkResult{
			Category: "auth",
			Name:     "Auth profile",
			Status:   "ok",
			Detail:   "no config file; skipped",
		}
	}

	cfg, err := config.Load(p)
	if err != nil {
		return checkResult{
			Category: "auth",
			Name:     "Auth profile",
			Status:   "warn",
			Detail:   fmt.Sprintf("cannot load config: %v", err),
		}
	}

	if cfg.Runtime.Profile == config.RuntimeProfileProduction && !cfg.HasAuth() {
		return checkResult{
			Category: "auth",
			Name:     "Auth profile",
			Status:   "fail",
			Detail:   "production profile requires authentication",
			Fix:      "configure auth.bearer_token or auth.api_keys in config",
		}
	}

	return checkResult{
		Category: "auth",
		Name:     "Auth profile",
		Status:   "ok",
		Detail:   fmt.Sprintf("profile=%s, auth configured=%v", cfg.Runtime.Profile, cfg.HasAuth()),
	}
}

func checkSecrets() checkResult {
	p := resolveConfigPath()
	if p == "" {
		return checkResult{
			Category: "security",
			Name:     "Plaintext secrets",
			Status:   "ok",
			Detail:   "no config file; skipped",
		}
	}

	data, err := os.ReadFile(p)
	if err != nil {
		return checkResult{
			Category: "security",
			Name:     "Plaintext secrets",
			Status:   "warn",
			Detail:   fmt.Sprintf("cannot read config: %v", err),
		}
	}

	content := string(data)
	var found []string
	for _, prefix := range secretPrefixes {
		if strings.Contains(content, prefix) {
			found = append(found, prefix+"...")
		}
	}

	if len(found) > 0 {
		return checkResult{
			Category: "security",
			Name:     "Plaintext secrets",
			Status:   "warn",
			Detail:   fmt.Sprintf("possible plaintext secrets detected: %s", strings.Join(found, ", ")),
			Fix:      "use 'hopclaw secrets set' to store secrets in keychain",
		}
	}

	return checkResult{
		Category: "security",
		Name:     "Plaintext secrets",
		Status:   "ok",
		Detail:   "no plaintext secret patterns found",
	}
}

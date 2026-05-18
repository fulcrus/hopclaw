package cli

import (
	"fmt"
	"os"
	"strings"

	"github.com/fulcrus/hopclaw/config"
	"github.com/fulcrus/hopclaw/internal/update"
	"github.com/fulcrus/hopclaw/internal/version"

	"gopkg.in/yaml.v3"
)

func checkVersion() checkResult {
	return checkResult{
		Category: "config",
		Name:     "Version",
		Status:   "ok",
		Detail:   version.Full(),
	}
}

func checkUpdatePolicy() checkResult {
	policy := loadUpdatePolicy()
	if !policy.Enabled {
		return checkResult{
			Category: "config",
			Name:     "Update policy",
			Status:   "warn",
			Detail:   "update checks are disabled",
			Fix:      "enable update.enabled and update.check_on_start for release visibility",
		}
	}
	detail := fmt.Sprintf("channel=%s", policy.Channel)
	if policy.ManifestURL != "" {
		detail += ", manifest=" + policy.ManifestURL
	}
	return checkResult{
		Category: "config",
		Name:     "Update policy",
		Status:   "ok",
		Detail:   detail,
	}
}

func checkAvailableUpdate() checkResult {
	result := update.LastCheckResult()
	if result == nil {
		return checkResult{
			Category: "config",
			Name:     "Available update",
			Status:   "warn",
			Detail:   "no update check result yet",
			Fix:      "run 'hopclaw update --check' or start the gateway once to record update state",
		}
	}
	if result.Error != "" {
		return checkResult{
			Category: "config",
			Name:     "Available update",
			Status:   "warn",
			Detail:   result.Error,
			Fix:      "verify network access to the release manifest or GitHub releases API",
		}
	}
	if result.UpToDate {
		return checkResult{
			Category: "config",
			Name:     "Available update",
			Status:   "ok",
			Detail:   "already on the latest visible release",
		}
	}
	detail := fmt.Sprintf("%s available on %s", result.LatestVersion, defaultDoctorString(result.UpdateURL, result.Source))
	return checkResult{
		Category: "config",
		Name:     "Available update",
		Status:   "warn",
		Detail:   detail,
		Fix:      "run 'hopclaw update' after reviewing the release notes",
	}
}

func checkConfigFile() checkResult {
	p := resolveConfigPath()
	if p == "" {
		return checkResult{
			Category: "config",
			Name:     "Config file",
			Status:   "warn",
			Detail:   "not found; run 'hopclaw setup'",
			Fix:      "run 'hopclaw setup' to create a config file",
		}
	}
	if _, err := config.Load(p); err != nil {
		return checkResult{
			Category: "config",
			Name:     "Config file",
			Status:   "fail",
			Detail:   fmt.Sprintf("%s: %v", p, err),
		}
	}
	return checkResult{
		Category: "config",
		Name:     "Config file",
		Status:   "ok",
		Detail:   p,
	}
}

func checkConfigSyntax() checkResult {
	p := resolveConfigPath()
	if p == "" {
		return checkResult{
			Category: "config",
			Name:     "Config syntax",
			Status:   "warn",
			Detail:   "no config file found",
		}
	}

	data, err := os.ReadFile(p)
	if err != nil {
		return checkResult{
			Category: "config",
			Name:     "Config syntax",
			Status:   "fail",
			Detail:   fmt.Sprintf("cannot read %s: %v", p, err),
		}
	}

	var raw any
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return checkResult{
			Category: "config",
			Name:     "Config syntax",
			Status:   "fail",
			Detail:   fmt.Sprintf("YAML parse error: %v", err),
		}
	}

	return checkResult{
		Category: "config",
		Name:     "Config syntax",
		Status:   "ok",
		Detail:   "valid YAML",
	}
}

func checkConfigMigration() checkResult {
	p := resolveConfigPath()
	if p == "" {
		return checkResult{
			Category: "config",
			Name:     "Config migration",
			Status:   "ok",
			Detail:   "no config file to check",
		}
	}

	data, err := os.ReadFile(p)
	if err != nil {
		return checkResult{
			Category: "config",
			Name:     "Config migration",
			Status:   "warn",
			Detail:   fmt.Sprintf("cannot read config: %v", err),
		}
	}

	content := string(data)
	var found []string
	for _, key := range deprecatedConfigKeys {
		// Convert dotted key to YAML-style check. For "server.auth_token"
		// we look for "auth_token" under server context; a simple substring
		// match on the leaf key is sufficient for a diagnostic hint.
		parts := strings.Split(key, ".")
		leaf := parts[len(parts)-1]
		if strings.Contains(content, leaf+":") {
			found = append(found, key)
		}
	}

	if len(found) > 0 {
		return checkResult{
			Category: "config",
			Name:     "Config migration",
			Status:   "warn",
			Detail:   fmt.Sprintf("deprecated keys found: %s", strings.Join(found, ", ")),
			Fix:      "migrate to the new auth section (see docs)",
		}
	}

	return checkResult{
		Category: "config",
		Name:     "Config migration",
		Status:   "ok",
		Detail:   "no deprecated keys found",
	}
}

func checkModelConfig() checkResult {
	p := resolveConfigPath()
	if p == "" {
		return checkResult{
			Category: "config",
			Name:     "Model config",
			Status:   "warn",
			Detail:   "no config file found",
		}
	}

	cfg, err := config.Load(p)
	if err != nil {
		return checkResult{
			Category: "config",
			Name:     "Model config",
			Status:   "warn",
			Detail:   fmt.Sprintf("cannot load config: %v", err),
		}
	}

	hasProvider := false
	if strings.TrimSpace(cfg.Models.OpenAICompat.BaseURL) != "" {
		hasProvider = true
	}
	if len(cfg.Models.Providers) > 0 {
		hasProvider = true
	}

	if !hasProvider {
		return checkResult{
			Category: "config",
			Name:     "Model config",
			Status:   "warn",
			Detail:   "no model provider configured",
			Fix:      "run 'hopclaw setup' to configure a model provider",
		}
	}

	count := len(cfg.Models.Providers)
	if strings.TrimSpace(cfg.Models.OpenAICompat.BaseURL) != "" {
		count++
	}

	return checkResult{
		Category: "config",
		Name:     "Model config",
		Status:   "ok",
		Detail:   fmt.Sprintf("%d provider(s) configured", count),
	}
}

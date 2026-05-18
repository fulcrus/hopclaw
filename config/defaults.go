package config

import (
	"os"
	"path/filepath"
	"strings"
)

// ---------------------------------------------------------------------------
// Config file auto-discovery
// ---------------------------------------------------------------------------

// DefaultConfigPaths returns the search order for config files:
//  1. $HOPCLAW_CONFIG environment variable
//  2. ./.hopclaw/config.yaml (current directory)
//  3. ~/.hopclaw/config.yaml (user home)
//  4. /etc/hopclaw/config.yaml (system-wide)
func DefaultConfigPaths() []string {
	var paths []string

	if env := os.Getenv("HOPCLAW_CONFIG"); strings.TrimSpace(env) != "" {
		paths = append(paths, env)
	}

	paths = append(paths, filepath.Join(".", ".hopclaw", "config.yaml"))

	if home, err := os.UserHomeDir(); err == nil {
		paths = append(paths, filepath.Join(home, ".hopclaw", "config.yaml"))
	}

	paths = append(paths, filepath.Join("/etc", "hopclaw", "config.yaml"))

	return paths
}

// DiscoverConfigPath returns the first existing config file from
// DefaultConfigPaths. Returns empty string if none found.
func DiscoverConfigPath() string {
	for _, p := range DefaultConfigPaths() {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}

// ---------------------------------------------------------------------------
// Default config generation
// ---------------------------------------------------------------------------

// GenerateDefaultConfig produces a minimal YAML configuration for the
// consumer edition. It auto-detects supported provider environment variables
// (for example OPENAI_API_KEY, DEEPSEEK_API_KEY, DASHSCOPE_API_KEY, and
// XIAOMI_API_KEY) and picks the first one found.
func GenerateDefaultConfig() string {
	provider, apiKey, model := detectProvider()
	if provider == "" {
		return minimalConfig()
	}
	cfg, err := BuildConfig(SetupOptions{
		Provider:    provider,
		ProviderAPI: DefaultProviderAPI(provider),
		APIKey:      apiKey,
		BaseURL:     DefaultBaseURL(provider),
		Model:       model,
	})
	if err != nil {
		return minimalConfig()
	}
	return cfg
}

// DetectAPIKey returns the first API key found from well-known environment
// variables. Returns the provider name and key, or empty strings if none
// found.
func DetectAPIKey() (provider, key string) {
	for _, entry := range SetupProviderProfiles() {
		if !providerSupportsAPIKeyEnv(entry.ID) {
			continue
		}
		for _, envVar := range entry.EnvVars {
			if v := strings.TrimSpace(os.Getenv(envVar)); v != "" {
				return entry.ID, v
			}
		}
	}
	return "", ""
}

type DetectedAPIKey struct {
	Provider string
	Key      string
}

func DetectAPIKeys() []DetectedAPIKey {
	profiles := SetupProviderProfiles()
	out := make([]DetectedAPIKey, 0, len(profiles))
	for _, entry := range profiles {
		if !providerSupportsAPIKeyEnv(entry.ID) {
			continue
		}
		for _, envVar := range entry.EnvVars {
			if v := strings.TrimSpace(os.Getenv(envVar)); v != "" {
				out = append(out, DetectedAPIKey{
					Provider: entry.ID,
					Key:      v,
				})
				break
			}
		}
	}
	return out
}

// HasAPIKey returns true if any well-known API key environment variable is set.
func HasAPIKey() bool {
	p, _ := DetectAPIKey()
	return p != ""
}

func detectProvider() (name, apiKey, model string) {
	for _, entry := range SetupProviderProfiles() {
		if !providerSupportsAPIKeyEnv(entry.ID) {
			continue
		}
		for _, envVar := range entry.EnvVars {
			if v := strings.TrimSpace(os.Getenv(envVar)); v != "" {
				return entry.ID, v, DefaultModelForProvider(entry.ID)
			}
		}
	}
	return "", "", ""
}

func providerSupportsAPIKeyEnv(provider string) bool {
	api := strings.TrimSpace(DefaultProviderAPI(provider))
	if api == "" {
		return false
	}
	profile, ok := LookupSetupProviderAPIProfile(api)
	if !ok {
		return false
	}
	for _, field := range profile.Fields {
		if strings.TrimSpace(field.ID) == "api_key" {
			return true
		}
	}
	return false
}

func minimalConfig() string {
	return `# HopClaw configuration
# Run 'hopclaw setup' for interactive configuration.
server:
  address: "` + DefaultGatewayAddress + `"
store:
  backend: memory
agent:
  default_model: "unconfigured-model"
tools:
  builtins:
    enabled: true
`
}

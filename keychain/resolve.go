package keychain

import (
	"fmt"
	"os"
	"strings"
)

// ResolveSecret resolves a secret value based on its prefix:
//   - "keychain:<key>" looks up <key> in the platform keychain
//   - "env:<VAR>" looks up the environment variable VAR
//   - anything else is returned as-is (literal value)
func ResolveSecret(value string) (string, error) {
	switch {
	case strings.HasPrefix(value, keychainPrefix):
		key := strings.TrimPrefix(value, keychainPrefix)
		if strings.TrimSpace(key) == "" {
			return "", fmt.Errorf("keychain: empty key in reference %q", value)
		}
		return activeStore().Get(defaultService, key)

	case strings.HasPrefix(value, envPrefix):
		envVar := strings.TrimPrefix(value, envPrefix)
		if strings.TrimSpace(envVar) == "" {
			return "", fmt.Errorf("keychain: empty variable name in reference %q", value)
		}
		val := os.Getenv(envVar)
		if val == "" {
			return "", fmt.Errorf("keychain: environment variable %q is not set", envVar)
		}
		return val, nil

	default:
		return value, nil
	}
}

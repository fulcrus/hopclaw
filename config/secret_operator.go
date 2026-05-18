package config

import "strings"

const SecretPlaceholder = "***"

func IsSecretPlaceholder(value string) bool {
	return strings.TrimSpace(value) == SecretPlaceholder
}

func sanitizeSecretValueForOperator(value string) string {
	kind, locator, ok := classifySecretRef(value)
	if !ok {
		return ""
	}
	if kind == SecretRefKindLiteral {
		return SecretPlaceholder
	}
	return locator
}

// SanitizeForOperator returns a secret-aware operator view of the config.
// Literal secret values are replaced with a preserve marker, while env/keychain
// references stay visible because they identify the secret source rather than
// the secret material itself.
func (c Config) SanitizeForOperator() Config {
	sanitized := c
	sanitized.walkSecretFields(func(_ string, value *string) {
		*value = sanitizeSecretValueForOperator(*value)
	})
	return sanitized
}

func (c Config) secretFieldValues() map[string]string {
	values := make(map[string]string, 32)
	c.walkSecretFields(func(path string, value *string) {
		values[path] = strings.TrimSpace(*value)
	})
	return values
}

func preserveSecretPlaceholders(next *Config, current Config) {
	if next == nil {
		return
	}
	currentValues := current.secretFieldValues()
	next.walkSecretFields(func(path string, value *string) {
		if !IsSecretPlaceholder(*value) {
			return
		}
		*value = currentValues[path]
	})
}

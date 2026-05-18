package i18n

import (
	"os"
	"strings"
)

// envPriority is the environment variable lookup order for locale detection.
var envPriority = []string{
	"HOPCLAW_LOCALE",
	"LC_ALL",
	"LANG",
}

// DetectLocale returns the user's locale from environment variables.
// Lookup order: HOPCLAW_LOCALE -> LC_ALL -> LANG -> "en".
func DetectLocale() Locale {
	for _, env := range envPriority {
		if val := os.Getenv(env); val != "" {
			return normalizeLocale(val)
		}
	}
	return EN
}

// normalizeLocale maps common locale strings to supported Locale values.
// Examples: "zh_CN.UTF-8" -> ZhCN, "en_US.UTF-8" -> EN
func normalizeLocale(raw string) Locale {
	// Strip encoding suffix (e.g., ".UTF-8").
	if idx := strings.IndexByte(raw, '.'); idx >= 0 {
		raw = raw[:idx]
	}
	// Normalize separators.
	raw = strings.ReplaceAll(raw, "_", "-")
	lower := strings.ToLower(raw)

	switch {
	case strings.HasPrefix(lower, "zh-cn"), lower == "zh-hans":
		return ZhCN
	case strings.HasPrefix(lower, "zh-tw"), strings.HasPrefix(lower, "zh-hk"), lower == "zh-hant":
		return ZhTW
	case strings.HasPrefix(lower, "zh"):
		return ZhCN // default Chinese to Simplified
	case strings.HasPrefix(lower, "ja"):
		return JaJP
	case strings.HasPrefix(lower, "en"):
		return EN
	default:
		// Check if the raw value matches a known locale.
		switch Locale(raw) {
		case EN, ZhCN, ZhTW, JaJP:
			return Locale(raw)
		}
		return EN
	}
}

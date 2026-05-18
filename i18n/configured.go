package i18n

import "strings"

func ApplyConfiguredLocale(raw string) Locale {
	locale := ResolveConfiguredLocale(raw)
	global.SetFallback(locale)
	global.EnsureLoaded(locale)
	return locale
}

func ResolveConfiguredLocale(raw string) Locale {
	if trimmed := strings.TrimSpace(raw); trimmed != "" {
		return normalizeLocale(trimmed)
	}
	return DetectLocale()
}

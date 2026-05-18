package i18n

// SupportedLocales returns the release-stable locale catalog identifiers.
func SupportedLocales() []Locale {
	return []Locale{EN, ZhCN, ZhTW, JaJP}
}

// SupportedLocaleStrings returns the release-stable locale catalog identifiers
// as JSON-friendly strings.
func SupportedLocaleStrings() []string {
	locales := SupportedLocales()
	out := make([]string, 0, len(locales))
	for _, locale := range locales {
		out = append(out, string(locale))
	}
	return out
}

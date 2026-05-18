package i18n

import "context"

type localeKey struct{}

// WithLocale returns a new context with the given locale attached.
func WithLocale(ctx context.Context, locale Locale) context.Context {
	return context.WithValue(ctx, localeKey{}, locale)
}

// LocaleFrom extracts the locale from the context.
// Returns empty string if no locale is set.
func LocaleFrom(ctx context.Context) Locale {
	if ctx == nil {
		return ""
	}
	if l, ok := ctx.Value(localeKey{}).(Locale); ok {
		return l
	}
	return ""
}

package i18n

import (
	"context"
	"strconv"
	"strings"
)

// T translates a key using the global registry's fallback locale.
// Params are applied as {0}, {1}, ... or as key=value pairs.
func T(key string, params ...string) string {
	return resolve(global, global.Fallback(), key, params...)
}

// TCtx translates a key using the locale from the context.
// Falls back to the global registry's fallback locale if no locale is in ctx.
func TCtx(ctx context.Context, key string, params ...string) string {
	locale := LocaleFrom(ctx)
	if locale == "" {
		locale = global.Fallback()
	}
	global.EnsureLoaded(locale)
	return resolve(global, locale, key, params...)
}

// resolve looks up a key in the given locale, falls back to the
// registry's fallback locale, and applies parameter substitution.
// Returns the key itself if no translation is found.
func resolve(r *Registry, locale Locale, key string, params ...string) string {
	cat := r.Get(locale)
	msg, ok := cat.Messages[key]
	if !ok {
		// Try fallback catalog.
		fb := r.Get(r.Fallback())
		if fb != nil && fb != cat {
			if m, ok2 := fb.Messages[key]; ok2 {
				msg = m
			} else {
				return key
			}
		} else {
			return key
		}
	}
	return applyParams(msg, params)
}

// applyParams replaces {0}, {1}, ... or {name} placeholders with provided values.
// Positional: T("hello {0}", "world") -> "hello world"
// Named: T("step {step}/{total}", "step", "1", "total", "4") -> "step 1/4"
func applyParams(msg string, params []string) string {
	if len(params) == 0 {
		return msg
	}

	// Check if params are named (even count, every other is a key).
	if len(params) >= 2 && len(params)%2 == 0 {
		// Try named params first.
		result := msg
		for i := 0; i < len(params); i += 2 {
			result = strings.ReplaceAll(result, "{"+params[i]+"}", params[i+1])
		}
		if result != msg {
			return result
		}
	}

	// Positional params.
	result := msg
	for i, p := range params {
		placeholder := "{" + strconv.Itoa(i) + "}"
		result = strings.ReplaceAll(result, placeholder, p)
	}
	return result
}

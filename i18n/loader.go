package i18n

import (
	"embed"
	"fmt"
	"log/slog"
	"sync"

	"github.com/fulcrus/hopclaw/logging"

	"gopkg.in/yaml.v3"
)

//go:embed locales/*.yaml
var localeFS embed.FS

// LoadEmbedded loads a locale from the embedded filesystem.
// Non-default locales are loaded lazily via sync.Once.
func (r *Registry) LoadEmbedded(locale Locale) error {
	data, err := localeFS.ReadFile("locales/" + string(locale) + ".yaml")
	if err != nil {
		return fmt.Errorf("i18n: load embedded %s: %w", locale, err)
	}
	return r.loadYAML(locale, data)
}

// EnsureLoaded loads a non-default locale lazily (once).
func (r *Registry) EnsureLoaded(locale Locale) {
	r.mu.Lock()
	once, ok := r.loaders[locale]
	if !ok {
		once = &sync.Once{}
		r.loaders[locale] = once
	}
	r.mu.Unlock()

	once.Do(func() {
		// Best-effort: if load fails, fallback will be used.
		logging.DebugIfErr(r.LoadEmbedded(locale), "load embedded locale failed", slog.String("locale", string(locale)))
	})
}

func (r *Registry) loadYAML(locale Locale, data []byte) error {
	var raw map[string]any
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return fmt.Errorf("i18n: parse %s yaml: %w", locale, err)
	}

	messages := make(map[string]string)
	flatten("", raw, messages)

	r.Register(&Catalog{
		Locale:   locale,
		Messages: messages,
	})
	return nil
}

// flatten recursively flattens a nested map into dot-notation keys.
func flatten(prefix string, m map[string]any, out map[string]string) {
	for k, v := range m {
		key := k
		if prefix != "" {
			key = prefix + "." + k
		}
		switch val := v.(type) {
		case string:
			out[key] = val
		case map[string]any:
			flatten(key, val, out)
		}
	}
}

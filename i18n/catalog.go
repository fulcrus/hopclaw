package i18n

import (
	"sync"
)

// Locale represents a BCP 47 locale tag.
type Locale string

// Supported locale constants.
const (
	EN   Locale = "en"
	ZhCN Locale = "zh-CN"
	ZhTW Locale = "zh-TW"
	JaJP Locale = "ja-JP"
)

// Catalog holds all translated messages for a single locale.
type Catalog struct {
	Locale   Locale
	Messages map[string]string // flat dot-notation keys
}

// Registry manages locale catalogs and provides thread-safe access.
type Registry struct {
	mu       sync.RWMutex // guards catalogs and fallback
	catalogs map[Locale]*Catalog
	fallback Locale
	loaders  map[Locale]*sync.Once // lazy load non-default locales
}

// global is the package-level registry used by T() and TCtx().
var global *Registry

func init() {
	global = NewRegistry(EN)
	// Load embedded English catalog eagerly.
	if err := global.LoadEmbedded(EN); err != nil {
		// English must always load; panic if it doesn't.
		panic("i18n: failed to load embedded en catalog: " + err.Error())
	}
}

// NewRegistry creates a new Registry with the given fallback locale.
func NewRegistry(fallback Locale) *Registry {
	return &Registry{
		catalogs: make(map[Locale]*Catalog),
		fallback: fallback,
		loaders:  make(map[Locale]*sync.Once),
	}
}

// Register adds or replaces a catalog in the registry.
func (r *Registry) Register(c *Catalog) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.catalogs[c.Locale] = c
}

// Get returns the catalog for the given locale, falling back to the
// registry's fallback locale if not found.
func (r *Registry) Get(locale Locale) *Catalog {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if c, ok := r.catalogs[locale]; ok {
		return c
	}
	return r.catalogs[r.fallback]
}

// SetFallback changes the fallback locale.
func (r *Registry) SetFallback(locale Locale) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.fallback = locale
}

// Fallback returns the current fallback locale.
func (r *Registry) Fallback() Locale {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.fallback
}

// Messages returns a copy of the flat message catalog for the given locale.
func (r *Registry) Messages(locale Locale) map[string]string {
	cat := r.Get(locale)
	if cat == nil || len(cat.Messages) == 0 {
		return nil
	}
	out := make(map[string]string, len(cat.Messages))
	for key, value := range cat.Messages {
		out[key] = value
	}
	return out
}

// Global returns the package-level registry.
func Global() *Registry { return global }

package i18n

import (
	"sync"
	"testing"
)

func TestNewRegistry(t *testing.T) {
	t.Parallel()

	r := NewRegistry(EN)
	if r.Fallback() != EN {
		t.Fatalf("expected fallback %q, got %q", EN, r.Fallback())
	}
}

func TestRegistry_RegisterAndGet(t *testing.T) {
	t.Parallel()

	r := NewRegistry(EN)
	cat := &Catalog{
		Locale:   ZhCN,
		Messages: map[string]string{"greeting": "你好"},
	}
	r.Register(cat)

	got := r.Get(ZhCN)
	if got == nil {
		t.Fatalf("expected catalog for %q, got nil", ZhCN)
	}
	if got.Messages["greeting"] != "你好" {
		t.Fatalf("expected message %q, got %q", "你好", got.Messages["greeting"])
	}
}

func TestRegistry_GetFallback(t *testing.T) {
	t.Parallel()

	r := NewRegistry(EN)
	enCat := &Catalog{
		Locale:   EN,
		Messages: map[string]string{"greeting": "Hello"},
	}
	r.Register(enCat)

	// Request an unregistered locale; should fall back to EN.
	got := r.Get(ZhTW)
	if got == nil {
		t.Fatalf("expected fallback catalog, got nil")
	}
	if got.Locale != EN {
		t.Fatalf("expected fallback locale %q, got %q", EN, got.Locale)
	}
	if got.Messages["greeting"] != "Hello" {
		t.Fatalf("expected message %q, got %q", "Hello", got.Messages["greeting"])
	}
}

func TestRegistry_GetNoCatalogs(t *testing.T) {
	t.Parallel()

	r := NewRegistry(EN)
	got := r.Get(EN)
	if got != nil {
		t.Fatalf("expected nil catalog from empty registry, got %+v", got)
	}
}

func TestRegistry_SetFallback(t *testing.T) {
	t.Parallel()

	r := NewRegistry(EN)
	r.SetFallback(ZhCN)
	if r.Fallback() != ZhCN {
		t.Fatalf("expected fallback %q, got %q", ZhCN, r.Fallback())
	}
}

func TestRegistry_RegisterOverwrite(t *testing.T) {
	t.Parallel()

	r := NewRegistry(EN)
	r.Register(&Catalog{
		Locale:   EN,
		Messages: map[string]string{"key": "first"},
	})
	r.Register(&Catalog{
		Locale:   EN,
		Messages: map[string]string{"key": "second"},
	})

	got := r.Get(EN)
	if got.Messages["key"] != "second" {
		t.Fatalf("expected overwritten message %q, got %q", "second", got.Messages["key"])
	}
}

func TestRegistry_ConcurrentAccess(t *testing.T) {
	t.Parallel()

	const goroutines = 50

	r := NewRegistry(EN)
	r.Register(&Catalog{
		Locale:   EN,
		Messages: map[string]string{"key": "value"},
	})

	var wg sync.WaitGroup
	wg.Add(goroutines * 3)

	// Concurrent readers.
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			cat := r.Get(EN)
			if cat == nil {
				t.Errorf("expected non-nil catalog")
			}
		}()
	}

	// Concurrent writers.
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			r.Register(&Catalog{
				Locale:   ZhCN,
				Messages: map[string]string{"key": "值"},
			})
		}()
	}

	// Concurrent fallback readers.
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			_ = r.Fallback()
		}()
	}

	wg.Wait()
}

func TestGlobal(t *testing.T) {
	t.Parallel()

	g := Global()
	if g == nil {
		t.Fatalf("expected non-nil global registry")
	}
	if g.Fallback() != EN {
		t.Fatalf("expected global fallback %q, got %q", EN, g.Fallback())
	}
}

func TestGlobal_EnglishCatalogLoaded(t *testing.T) {
	t.Parallel()

	g := Global()
	cat := g.Get(EN)
	if cat == nil {
		t.Fatalf("expected english catalog in global registry")
	}
	if len(cat.Messages) == 0 {
		t.Fatalf("expected non-empty english message catalog")
	}
	// Spot-check a known key.
	if cat.Messages["common.yes"] != "Yes" {
		t.Fatalf("expected common.yes=%q, got %q", "Yes", cat.Messages["common.yes"])
	}
}

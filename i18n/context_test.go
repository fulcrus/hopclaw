package i18n

import (
	"context"
	"testing"
)

func TestWithLocale_Roundtrip(t *testing.T) {
	t.Parallel()

	ctx := WithLocale(context.Background(), ZhCN)
	got := LocaleFrom(ctx)
	if got != ZhCN {
		t.Fatalf("expected %q, got %q", ZhCN, got)
	}
}

func TestLocaleFrom_NilContext(t *testing.T) {
	t.Parallel()

	//lint:ignore SA1012 intentionally testing nil context handling
	got := LocaleFrom(nil)
	if got != "" {
		t.Fatalf("expected empty locale for nil context, got %q", got)
	}
}

func TestLocaleFrom_MissingLocale(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	got := LocaleFrom(ctx)
	if got != "" {
		t.Fatalf("expected empty locale for context without locale, got %q", got)
	}
}

func TestWithLocale_Override(t *testing.T) {
	t.Parallel()

	ctx := WithLocale(context.Background(), EN)
	ctx = WithLocale(ctx, ZhTW)
	got := LocaleFrom(ctx)
	if got != ZhTW {
		t.Fatalf("expected %q after override, got %q", ZhTW, got)
	}
}

func TestWithLocale_AllLocales(t *testing.T) {
	t.Parallel()

	locales := []Locale{EN, ZhCN, ZhTW}
	for _, locale := range locales {
		t.Run(string(locale), func(t *testing.T) {
			t.Parallel()

			ctx := WithLocale(context.Background(), locale)
			got := LocaleFrom(ctx)
			if got != locale {
				t.Fatalf("expected %q, got %q", locale, got)
			}
		})
	}
}

func TestLocaleFrom_CancelledContext(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	ctx = WithLocale(ctx, ZhCN)
	cancel()

	// Locale should still be readable from a cancelled context.
	got := LocaleFrom(ctx)
	if got != ZhCN {
		t.Fatalf("expected %q from cancelled context, got %q", ZhCN, got)
	}
}

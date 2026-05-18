package i18n

import (
	"context"
	"testing"
)

func TestT_SimpleKey(t *testing.T) {
	t.Parallel()

	got := T("common.yes")
	if got != "Yes" {
		t.Fatalf("expected %q, got %q", "Yes", got)
	}
}

func TestT_MissingKeyReturnsKey(t *testing.T) {
	t.Parallel()

	key := "nonexistent.key.that.does.not.exist"
	got := T(key)
	if got != key {
		t.Fatalf("expected missing key to return %q, got %q", key, got)
	}
}

func TestT_NestedKey(t *testing.T) {
	t.Parallel()

	got := T("cli.doctor.title")
	if got != "HopClaw Doctor" {
		t.Fatalf("expected %q, got %q", "HopClaw Doctor", got)
	}
}

func TestT_NamedParams(t *testing.T) {
	t.Parallel()

	got := T("channel.status.plan_progress", "step", "2", "total", "5", "description", "Running tests")
	expected := "Step 2/5: Running tests"
	if got != expected {
		t.Fatalf("expected %q, got %q", expected, got)
	}
}

func TestT_PositionalParams(t *testing.T) {
	t.Parallel()

	// Use a direct resolve call to test positional params since
	// the catalog uses named params. We test the applyParams logic.
	msg := applyParams("hello {0}, welcome to {1}", []string{"world", "HopClaw"})
	expected := "hello world, welcome to HopClaw"
	if msg != expected {
		t.Fatalf("expected %q, got %q", expected, msg)
	}
}

func TestT_NoParams(t *testing.T) {
	t.Parallel()

	msg := applyParams("no params here", nil)
	expected := "no params here"
	if msg != expected {
		t.Fatalf("expected %q, got %q", expected, msg)
	}
}

func TestT_EmptyParams(t *testing.T) {
	t.Parallel()

	msg := applyParams("no params here", []string{})
	expected := "no params here"
	if msg != expected {
		t.Fatalf("expected %q, got %q", expected, msg)
	}
}

func TestT_NamedParamNoMatch(t *testing.T) {
	t.Parallel()

	// When named params don't match any placeholder, fall back to positional.
	msg := applyParams("value is {0}", []string{"nokey", "novalue"})
	// Named won't match, so positional applies: {0} -> "nokey"
	expected := "value is nokey"
	if msg != expected {
		t.Fatalf("expected %q, got %q", expected, msg)
	}
}

func TestT_SingleParam(t *testing.T) {
	t.Parallel()

	// Odd number of params forces positional.
	msg := applyParams("{0} is great", []string{"HopClaw"})
	expected := "HopClaw is great"
	if msg != expected {
		t.Fatalf("expected %q, got %q", expected, msg)
	}
}

func TestTCtx_WithLocale(t *testing.T) {
	t.Parallel()

	ctx := WithLocale(context.Background(), ZhCN)
	got := TCtx(ctx, "common.yes")
	if got != "是" {
		t.Fatalf("expected %q, got %q", "是", got)
	}
}

func TestTCtx_FallbackOnMissingLocale(t *testing.T) {
	t.Parallel()

	// No locale in context; should use fallback (EN).
	ctx := context.Background()
	got := TCtx(ctx, "common.yes")
	if got != "Yes" {
		t.Fatalf("expected %q, got %q", "Yes", got)
	}
}

func TestTCtx_MissingKeyInZhCN_FallsBackToEN(t *testing.T) {
	t.Parallel()

	// Register a zh-CN catalog with a missing key.
	r := NewRegistry(EN)
	r.Register(&Catalog{
		Locale:   EN,
		Messages: map[string]string{"only.in.en": "English only"},
	})
	r.Register(&Catalog{
		Locale:   ZhCN,
		Messages: map[string]string{"other.key": "其他"},
	})

	got := resolve(r, ZhCN, "only.in.en")
	if got != "English only" {
		t.Fatalf("expected fallback to EN %q, got %q", "English only", got)
	}
}

func TestTCtx_MissingKeyBothLocales(t *testing.T) {
	t.Parallel()

	r := NewRegistry(EN)
	r.Register(&Catalog{
		Locale:   EN,
		Messages: map[string]string{"exists": "yes"},
	})

	key := "totally.missing"
	got := resolve(r, EN, key)
	if got != key {
		t.Fatalf("expected missing key %q, got %q", key, got)
	}
}

func TestResolve_SameLocaleAsFallback(t *testing.T) {
	t.Parallel()

	r := NewRegistry(EN)
	r.Register(&Catalog{
		Locale:   EN,
		Messages: map[string]string{"key": "value"},
	})

	// When locale == fallback and key is missing, should return key.
	got := resolve(r, EN, "missing.key")
	if got != "missing.key" {
		t.Fatalf("expected %q, got %q", "missing.key", got)
	}
}

func TestResolve_WithParams(t *testing.T) {
	t.Parallel()

	r := NewRegistry(EN)
	r.Register(&Catalog{
		Locale:   EN,
		Messages: map[string]string{"greeting": "Hello {name}!"},
	})

	got := resolve(r, EN, "greeting", "name", "World")
	if got != "Hello World!" {
		t.Fatalf("expected %q, got %q", "Hello World!", got)
	}
}

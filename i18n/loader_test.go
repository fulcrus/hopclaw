package i18n

import (
	"testing"
)

func TestLoadEmbedded_English(t *testing.T) {
	t.Parallel()

	r := NewRegistry(EN)
	if err := r.LoadEmbedded(EN); err != nil {
		t.Fatalf("failed to load embedded EN: %v", err)
	}

	cat := r.Get(EN)
	if cat == nil {
		t.Fatalf("expected EN catalog after loading")
	}
	if cat.Locale != EN {
		t.Fatalf("expected locale %q, got %q", EN, cat.Locale)
	}
	if len(cat.Messages) == 0 {
		t.Fatalf("expected non-empty messages")
	}
}

func TestLoadEmbedded_ZhCN(t *testing.T) {
	t.Parallel()

	r := NewRegistry(EN)
	if err := r.LoadEmbedded(ZhCN); err != nil {
		t.Fatalf("failed to load embedded zh-CN: %v", err)
	}

	cat := r.Get(ZhCN)
	if cat == nil {
		t.Fatalf("expected zh-CN catalog after loading")
	}
	if cat.Locale != ZhCN {
		t.Fatalf("expected locale %q, got %q", ZhCN, cat.Locale)
	}
	if len(cat.Messages) == 0 {
		t.Fatalf("expected non-empty messages")
	}
}

func TestLoadEmbedded_ZhTW(t *testing.T) {
	t.Parallel()

	r := NewRegistry(EN)
	if err := r.LoadEmbedded(ZhTW); err != nil {
		t.Fatalf("failed to load embedded zh-TW: %v", err)
	}

	cat := r.Get(ZhTW)
	if cat == nil {
		t.Fatalf("expected zh-TW catalog after loading")
	}
	if cat.Locale != ZhTW {
		t.Fatalf("expected locale %q, got %q", ZhTW, cat.Locale)
	}
	if len(cat.Messages) == 0 {
		t.Fatalf("expected non-empty messages")
	}
}

func TestLoadEmbedded_JaJP(t *testing.T) {
	t.Parallel()

	r := NewRegistry(EN)
	if err := r.LoadEmbedded(JaJP); err != nil {
		t.Fatalf("failed to load embedded ja-JP: %v", err)
	}

	cat := r.Get(JaJP)
	if cat == nil {
		t.Fatalf("expected ja-JP catalog after loading")
	}
	if cat.Locale != JaJP {
		t.Fatalf("expected locale %q, got %q", JaJP, cat.Locale)
	}
	if len(cat.Messages) == 0 {
		t.Fatalf("expected non-empty messages")
	}
}

func TestLoadEmbedded_NonExistent(t *testing.T) {
	t.Parallel()

	r := NewRegistry(EN)
	err := r.LoadEmbedded(Locale("fr"))
	if err == nil {
		t.Fatalf("expected error loading non-existent locale")
	}
}

func TestEnsureLoaded_LazyInit(t *testing.T) {
	t.Parallel()

	r := NewRegistry(EN)
	if err := r.LoadEmbedded(EN); err != nil {
		t.Fatalf("failed to load EN: %v", err)
	}

	// zh-CN not loaded yet.
	r.mu.RLock()
	_, hasBefore := r.catalogs[ZhCN]
	r.mu.RUnlock()
	if hasBefore {
		t.Fatalf("expected zh-CN not loaded before EnsureLoaded")
	}

	// EnsureLoaded should lazily load zh-CN.
	r.EnsureLoaded(ZhCN)

	cat := r.Get(ZhCN)
	if cat == nil {
		t.Fatalf("expected zh-CN catalog after EnsureLoaded")
	}
	if cat.Locale != ZhCN {
		t.Fatalf("expected locale %q, got %q", ZhCN, cat.Locale)
	}
}

func TestEnsureLoaded_OnlyOnce(t *testing.T) {
	t.Parallel()

	r := NewRegistry(EN)
	if err := r.LoadEmbedded(EN); err != nil {
		t.Fatalf("failed to load EN: %v", err)
	}

	// Call EnsureLoaded multiple times; should not panic or error.
	for i := 0; i < 10; i++ {
		r.EnsureLoaded(ZhCN)
	}

	cat := r.Get(ZhCN)
	if cat == nil {
		t.Fatalf("expected zh-CN catalog")
	}
}

func TestEnsureLoaded_NonExistentFallsBack(t *testing.T) {
	t.Parallel()

	r := NewRegistry(EN)
	if err := r.LoadEmbedded(EN); err != nil {
		t.Fatalf("failed to load EN: %v", err)
	}

	// Loading a non-existent locale is best-effort; should fall back.
	r.EnsureLoaded(Locale("fr"))

	cat := r.Get(Locale("fr"))
	if cat == nil {
		t.Fatalf("expected fallback catalog for non-existent locale")
	}
	if cat.Locale != EN {
		t.Fatalf("expected fallback to %q, got %q", EN, cat.Locale)
	}
}

func TestFlatten_Simple(t *testing.T) {
	t.Parallel()

	input := map[string]any{
		"greeting": "Hello",
		"farewell": "Goodbye",
	}
	out := make(map[string]string)
	flatten("", input, out)

	if out["greeting"] != "Hello" {
		t.Fatalf("expected %q, got %q", "Hello", out["greeting"])
	}
	if out["farewell"] != "Goodbye" {
		t.Fatalf("expected %q, got %q", "Goodbye", out["farewell"])
	}
}

func TestFlatten_Nested(t *testing.T) {
	t.Parallel()

	input := map[string]any{
		"level1": map[string]any{
			"level2": map[string]any{
				"key": "deep value",
			},
			"key": "shallow value",
		},
	}
	out := make(map[string]string)
	flatten("", input, out)

	if out["level1.level2.key"] != "deep value" {
		t.Fatalf("expected %q, got %q", "deep value", out["level1.level2.key"])
	}
	if out["level1.key"] != "shallow value" {
		t.Fatalf("expected %q, got %q", "shallow value", out["level1.key"])
	}
}

func TestFlatten_WithPrefix(t *testing.T) {
	t.Parallel()

	input := map[string]any{
		"key": "value",
	}
	out := make(map[string]string)
	flatten("prefix", input, out)

	if out["prefix.key"] != "value" {
		t.Fatalf("expected %q, got %q", "value", out["prefix.key"])
	}
}

func TestFlatten_SkipsNonStringNonMap(t *testing.T) {
	t.Parallel()

	input := map[string]any{
		"string_key": "value",
		"int_key":    42,
		"bool_key":   true,
		"nested": map[string]any{
			"inner": "ok",
		},
	}
	out := make(map[string]string)
	flatten("", input, out)

	if len(out) != 2 {
		t.Fatalf("expected 2 entries, got %d: %v", len(out), out)
	}
	if out["string_key"] != "value" {
		t.Fatalf("expected %q, got %q", "value", out["string_key"])
	}
	if out["nested.inner"] != "ok" {
		t.Fatalf("expected %q, got %q", "ok", out["nested.inner"])
	}
}

func TestLoadYAML_InvalidYAML(t *testing.T) {
	t.Parallel()

	r := NewRegistry(EN)
	err := r.loadYAML(EN, []byte(":::invalid yaml[[["))
	if err == nil {
		t.Fatalf("expected error for invalid YAML")
	}
}

func TestLoadYAML_ValidYAML(t *testing.T) {
	t.Parallel()

	r := NewRegistry(EN)
	yamlData := []byte("greeting: Hello\nnested:\n  key: value\n")
	if err := r.loadYAML(EN, yamlData); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	cat := r.Get(EN)
	if cat == nil {
		t.Fatalf("expected catalog after loadYAML")
	}
	if cat.Messages["greeting"] != "Hello" {
		t.Fatalf("expected %q, got %q", "Hello", cat.Messages["greeting"])
	}
	if cat.Messages["nested.key"] != "value" {
		t.Fatalf("expected %q, got %q", "value", cat.Messages["nested.key"])
	}
}

func TestEnZhCN_KeyParity(t *testing.T) {
	t.Parallel()

	r := NewRegistry(EN)
	if err := r.LoadEmbedded(EN); err != nil {
		t.Fatalf("failed to load EN: %v", err)
	}
	if err := r.LoadEmbedded(ZhCN); err != nil {
		t.Fatalf("failed to load zh-CN: %v", err)
	}

	enCat := r.Get(EN)
	zhCat := r.Get(ZhCN)

	// Every EN key should exist in zh-CN.
	for key := range enCat.Messages {
		if _, ok := zhCat.Messages[key]; !ok {
			t.Errorf("key %q exists in EN but not in zh-CN", key)
		}
	}

	// Every zh-CN key should exist in EN.
	for key := range zhCat.Messages {
		if _, ok := enCat.Messages[key]; !ok {
			t.Errorf("key %q exists in zh-CN but not in EN", key)
		}
	}
}

func TestEnZhTW_KeyParity(t *testing.T) {
	t.Parallel()

	r := NewRegistry(EN)
	if err := r.LoadEmbedded(EN); err != nil {
		t.Fatalf("failed to load EN: %v", err)
	}
	if err := r.LoadEmbedded(ZhTW); err != nil {
		t.Fatalf("failed to load zh-TW: %v", err)
	}

	enCat := r.Get(EN)
	zhTWCat := r.Get(ZhTW)

	for key := range enCat.Messages {
		if _, ok := zhTWCat.Messages[key]; !ok {
			t.Errorf("key %q exists in EN but not in zh-TW", key)
		}
	}

	for key := range zhTWCat.Messages {
		if _, ok := enCat.Messages[key]; !ok {
			t.Errorf("key %q exists in zh-TW but not in EN", key)
		}
	}
}

func TestEnJaJP_KeyParity(t *testing.T) {
	t.Parallel()

	r := NewRegistry(EN)
	if err := r.LoadEmbedded(EN); err != nil {
		t.Fatalf("failed to load EN: %v", err)
	}
	if err := r.LoadEmbedded(JaJP); err != nil {
		t.Fatalf("failed to load ja-JP: %v", err)
	}

	enCat := r.Get(EN)
	jaCat := r.Get(JaJP)

	for key := range enCat.Messages {
		if _, ok := jaCat.Messages[key]; !ok {
			t.Errorf("key %q exists in EN but not in ja-JP", key)
		}
	}

	for key := range jaCat.Messages {
		if _, ok := enCat.Messages[key]; !ok {
			t.Errorf("key %q exists in ja-JP but not in EN", key)
		}
	}
}

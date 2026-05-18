package model

import "testing"

func TestKnownModelsNotEmpty(t *testing.T) {
	t.Parallel()

	models := KnownModels()
	if len(models) == 0 {
		t.Fatal("KnownModels() returned empty slice")
	}

	// Every entry must have Provider, Model, DisplayName, and at least one capability.
	for _, m := range models {
		if m.Provider == "" {
			t.Errorf("model %q has empty Provider", m.Model)
		}
		if m.Model == "" {
			t.Errorf("model with Provider %q has empty Model", m.Provider)
		}
		if m.DisplayName == "" {
			t.Errorf("model %q has empty DisplayName", m.Model)
		}
		if m.ContextWindow <= 0 {
			t.Errorf("model %q has non-positive ContextWindow: %d", m.Model, m.ContextWindow)
		}
		if m.MaxOutput <= 0 {
			t.Errorf("model %q has non-positive MaxOutput: %d", m.Model, m.MaxOutput)
		}
		if len(m.Capabilities) == 0 {
			t.Errorf("model %q has no capabilities", m.Model)
		}
	}
}

func TestKnownModelsReturnsDefensiveCopy(t *testing.T) {
	t.Parallel()

	a := KnownModels()
	b := KnownModels()
	if len(a) != len(b) {
		t.Fatalf("successive calls returned different lengths: %d vs %d", len(a), len(b))
	}

	// Mutating the returned slice must not affect future calls.
	a[0].Provider = "mutated"
	c := KnownModels()
	if c[0].Provider == "mutated" {
		t.Fatal("KnownModels() does not return a defensive copy")
	}
}

func TestLookupModel(t *testing.T) {
	t.Parallel()

	t.Run("found", func(t *testing.T) {
		t.Parallel()

		meta, ok := LookupModel("anthropic", "claude-opus-4-6")
		if !ok {
			t.Fatal("expected to find anthropic/claude-opus-4-6")
		}
		if meta.DisplayName != "Claude Opus 4.6" {
			t.Errorf("unexpected DisplayName: %q", meta.DisplayName)
		}
		if meta.ContextWindow != ctx200k {
			t.Errorf("unexpected ContextWindow: %d", meta.ContextWindow)
		}
		if meta.MaxOutput != out32k {
			t.Errorf("unexpected MaxOutput: %d", meta.MaxOutput)
		}
	})

	t.Run("not found", func(t *testing.T) {
		t.Parallel()

		_, ok := LookupModel("nonexistent", "no-such-model")
		if ok {
			t.Fatal("expected LookupModel to return false for unknown model")
		}
	})

	t.Run("wrong provider", func(t *testing.T) {
		t.Parallel()

		// Model exists under "anthropic" but not "openai".
		_, ok := LookupModel("openai", "claude-opus-4-6")
		if ok {
			t.Fatal("expected LookupModel to return false for mismatched provider")
		}
	})
}

func TestModelsForProvider(t *testing.T) {
	t.Parallel()

	t.Run("known provider", func(t *testing.T) {
		t.Parallel()

		models := ModelsForProvider("openai")
		if len(models) == 0 {
			t.Fatal("expected non-empty result for openai provider")
		}
		for _, m := range models {
			if m.Provider != "openai" {
				t.Errorf("ModelsForProvider(\"openai\") returned model with Provider=%q", m.Provider)
			}
		}
	})

	t.Run("unknown provider", func(t *testing.T) {
		t.Parallel()

		models := ModelsForProvider("nonexistent")
		if models != nil {
			t.Fatalf("expected nil for unknown provider, got %d models", len(models))
		}
	})

	t.Run("multi model provider", func(t *testing.T) {
		t.Parallel()

		models := ModelsForProvider("google")
		expectedCount := 3 // gemini-2.0-flash, gemini-2.5-pro, gemini-2.5-flash
		if len(models) != expectedCount {
			t.Fatalf("expected %d Google models, got %d", expectedCount, len(models))
		}
	})

	t.Run("domestic provider", func(t *testing.T) {
		t.Parallel()

		models := ModelsForProvider("hunyuan")
		if len(models) != 2 {
			t.Fatalf("expected 2 Hunyuan models, got %d", len(models))
		}
	})

	t.Run("xiaomi provider", func(t *testing.T) {
		t.Parallel()

		models := ModelsForProvider("xiaomi")
		if len(models) != 1 {
			t.Fatalf("expected 1 Xiaomi model, got %d", len(models))
		}
		if models[0].Model != "mimo-v2-flash" {
			t.Fatalf("unexpected Xiaomi model %q", models[0].Model)
		}
	})
}

func TestHasCapability(t *testing.T) {
	t.Parallel()

	t.Run("present", func(t *testing.T) {
		t.Parallel()

		meta, ok := LookupModel("anthropic", "claude-opus-4-6")
		if !ok {
			t.Fatal("expected to find anthropic/claude-opus-4-6")
		}
		for _, cap := range []ModelCapability{CapVision, CapReasoning, CapToolUse, CapStreaming} {
			if !HasCapability(meta, cap) {
				t.Errorf("expected claude-opus-4-6 to have capability %q", cap)
			}
		}
	})

	t.Run("absent", func(t *testing.T) {
		t.Parallel()

		meta, ok := LookupModel("deepseek", "deepseek-chat")
		if !ok {
			t.Fatal("expected to find deepseek/deepseek-chat")
		}
		if HasCapability(meta, CapVision) {
			t.Error("deepseek-chat should not have vision capability")
		}
		if HasCapability(meta, CapReasoning) {
			t.Error("deepseek-chat should not have reasoning capability")
		}
	})

	t.Run("empty capabilities", func(t *testing.T) {
		t.Parallel()

		meta := ModelMeta{Capabilities: nil}
		if HasCapability(meta, CapVision) {
			t.Error("empty capabilities should not match any capability")
		}
	})
}

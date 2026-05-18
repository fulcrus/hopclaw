package voice

import (
	"context"
	"testing"
)

// ---------------------------------------------------------------------------
// ParseDirectives tests
// ---------------------------------------------------------------------------

func TestParseDirectivesNoDirectives(t *testing.T) {
	t.Parallel()

	text := "Hello, this is plain text with no directives."
	clean, directives := ParseDirectives(text)
	if clean != text {
		t.Fatalf("expected clean text %q, got %q", text, clean)
	}
	if len(directives) != 0 {
		t.Fatalf("expected no directives, got %d", len(directives))
	}
}

func TestParseDirectivesSingleDirective(t *testing.T) {
	t.Parallel()

	text := "[[tts:alloy]]Hello, welcome to the demo."
	clean, directives := ParseDirectives(text)

	wantClean := "Hello, welcome to the demo."
	if clean != wantClean {
		t.Fatalf("expected clean text %q, got %q", wantClean, clean)
	}
	if len(directives) != 1 {
		t.Fatalf("expected 1 directive, got %d", len(directives))
	}
	if directives[0].Voice != "alloy" {
		t.Fatalf("expected voice %q, got %q", "alloy", directives[0].Voice)
	}
}

func TestParseDirectivesMultipleDirectives(t *testing.T) {
	t.Parallel()

	text := "Part one [[tts:nova]] and part two [[tts:shimmer]] end."
	clean, directives := ParseDirectives(text)

	wantClean := "Part one  and part two  end."
	if clean != wantClean {
		t.Fatalf("expected clean text %q, got %q", wantClean, clean)
	}
	if len(directives) != 2 {
		t.Fatalf("expected 2 directives, got %d", len(directives))
	}
	if directives[0].Voice != "nova" {
		t.Fatalf("expected first voice %q, got %q", "nova", directives[0].Voice)
	}
	if directives[1].Voice != "shimmer" {
		t.Fatalf("expected second voice %q, got %q", "shimmer", directives[1].Voice)
	}
}

func TestParseDirectivesTrimsWhitespace(t *testing.T) {
	t.Parallel()

	text := "[[tts: echo ]]Hello there."
	clean, directives := ParseDirectives(text)

	wantClean := "Hello there."
	if clean != wantClean {
		t.Fatalf("expected clean text %q, got %q", wantClean, clean)
	}
	if len(directives) != 1 {
		t.Fatalf("expected 1 directive, got %d", len(directives))
	}
	if directives[0].Voice != "echo" {
		t.Fatalf("expected voice %q, got %q", "echo", directives[0].Voice)
	}
}

func TestParseDirectivesEmptyString(t *testing.T) {
	t.Parallel()

	clean, directives := ParseDirectives("")
	if clean != "" {
		t.Fatalf("expected empty clean text, got %q", clean)
	}
	if len(directives) != 0 {
		t.Fatalf("expected no directives, got %d", len(directives))
	}
}

// ---------------------------------------------------------------------------
// NewProvider tests
// ---------------------------------------------------------------------------

func TestNewProviderOpenAI(t *testing.T) {
	t.Parallel()

	cfg := Config{
		Provider: "openai",
		OpenAI:   OpenAIConfig{APIKey: "test-key"},
	}
	p, err := NewProvider(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.Name() != "openai" {
		t.Fatalf("expected provider name %q, got %q", "openai", p.Name())
	}
}

func TestNewProviderElevenLabs(t *testing.T) {
	t.Parallel()

	cfg := Config{
		Provider:   "elevenlabs",
		ElevenLabs: ElevenLabsConfig{APIKey: "test-key"},
	}
	p, err := NewProvider(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.Name() != "elevenlabs" {
		t.Fatalf("expected provider name %q, got %q", "elevenlabs", p.Name())
	}
}

func TestNewProviderEdge(t *testing.T) {
	t.Parallel()

	cfg := Config{
		Provider: "edge",
	}
	p, err := NewProvider(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.Name() != "edge" {
		t.Fatalf("expected provider name %q, got %q", "edge", p.Name())
	}
}

func TestNewProviderUnknown(t *testing.T) {
	t.Parallel()

	cfg := Config{Provider: "unknown-provider"}
	_, err := NewProvider(cfg)
	if err == nil {
		t.Fatal("expected error for unknown provider")
	}
}

func TestNewProviderOpenAIMissingAPIKey(t *testing.T) {
	t.Parallel()

	cfg := Config{
		Provider: "openai",
		OpenAI:   OpenAIConfig{},
	}
	_, err := NewProvider(cfg)
	if err == nil {
		t.Fatal("expected error for missing openai api key")
	}
}

func TestNewProviderElevenLabsMissingAPIKey(t *testing.T) {
	t.Parallel()

	cfg := Config{
		Provider:   "elevenlabs",
		ElevenLabs: ElevenLabsConfig{},
	}
	_, err := NewProvider(cfg)
	if err == nil {
		t.Fatal("expected error for missing elevenlabs api key")
	}
}

// ---------------------------------------------------------------------------
// Synthesize convenience function tests
// ---------------------------------------------------------------------------

func TestSynthesizeInvalidConfig(t *testing.T) {
	t.Parallel()

	cfg := Config{Provider: "nonexistent"}
	_, err := Synthesize(context.Background(), cfg, "hello")
	if err == nil {
		t.Fatal("expected error for invalid config")
	}
}

func TestSynthesizeEdgeReturnsError(t *testing.T) {
	t.Parallel()

	cfg := Config{Provider: "edge"}
	_, err := Synthesize(context.Background(), cfg, "hello")
	if err == nil {
		t.Fatal("expected error from edge tts stub")
	}
}

func TestSynthesizeFallbackAlsoFails(t *testing.T) {
	t.Parallel()

	cfg := Config{
		Provider: "edge",
		Fallback: "edge", // fallback also uses stub edge
	}
	_, err := Synthesize(context.Background(), cfg, "hello")
	if err == nil {
		t.Fatal("expected error when both primary and fallback fail")
	}
}

func TestSynthesizeFallbackInvalidProvider(t *testing.T) {
	t.Parallel()

	cfg := Config{
		Provider: "edge",
		Fallback: "nonexistent",
	}
	_, err := Synthesize(context.Background(), cfg, "hello")
	if err == nil {
		t.Fatal("expected error when fallback provider is invalid")
	}
}

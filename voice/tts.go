package voice

import (
	"context"
	"fmt"
	"time"
)

// ---------------------------------------------------------------------------
// Provider interface and AudioResult
// ---------------------------------------------------------------------------

// Provider is implemented by each TTS backend.
type Provider interface {
	// Name returns the provider identifier (e.g. "openai", "elevenlabs").
	Name() string

	// Synthesize converts text to speech audio.
	Synthesize(ctx context.Context, text string) (*AudioResult, error)
}

// AudioResult holds the synthesised audio bytes and metadata.
type AudioResult struct {
	Data        []byte        `json:"data"`
	ContentType string        `json:"content_type"`       // "audio/mpeg", "audio/wav", etc.
	Duration    time.Duration `json:"duration,omitempty"` // estimated duration; may be zero
}

// ---------------------------------------------------------------------------
// Provider names
// ---------------------------------------------------------------------------

const (
	providerOpenAI     = "openai"
	providerElevenLabs = "elevenlabs"
	providerEdge       = "edge"
)

// ---------------------------------------------------------------------------
// Factory
// ---------------------------------------------------------------------------

// NewProvider creates a Provider for the configured backend.
func NewProvider(cfg Config) (Provider, error) {
	switch cfg.Provider {
	case providerOpenAI:
		return newOpenAIProvider(cfg.OpenAI)
	case providerElevenLabs:
		return newElevenLabsProvider(cfg.ElevenLabs)
	case providerEdge:
		return newEdgeProvider(cfg.Edge)
	default:
		return nil, fmt.Errorf("unknown tts provider: %q", cfg.Provider)
	}
}

// ---------------------------------------------------------------------------
// Convenience function
// ---------------------------------------------------------------------------

// Synthesize is a convenience helper that creates a provider, synthesises the
// text, and falls back to cfg.Fallback on failure.
func Synthesize(ctx context.Context, cfg Config, text string) (*AudioResult, error) {
	primary, err := NewProvider(cfg)
	if err != nil {
		return nil, fmt.Errorf("creating primary tts provider: %w", err)
	}

	result, primaryErr := primary.Synthesize(ctx, text)
	if primaryErr == nil {
		return result, nil
	}

	// Try fallback if configured.
	if cfg.Fallback == "" {
		return nil, fmt.Errorf("tts synthesis failed: %w", primaryErr)
	}

	fallbackCfg := cfg
	fallbackCfg.Provider = cfg.Fallback
	fallbackCfg.Fallback = "" // prevent infinite recursion

	fallback, err := NewProvider(fallbackCfg)
	if err != nil {
		return nil, fmt.Errorf("creating fallback tts provider: %w (primary error: %w)", err, primaryErr)
	}

	result, fallbackErr := fallback.Synthesize(ctx, text)
	if fallbackErr != nil {
		return nil, fmt.Errorf("tts fallback failed: %w (primary error: %w)", fallbackErr, primaryErr)
	}
	return result, nil
}

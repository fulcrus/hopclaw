package stt

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"time"
)

// ---------------------------------------------------------------------------
// Provider interface and result types
// ---------------------------------------------------------------------------

// Provider is implemented by each STT backend.
type Provider interface {
	// Name returns the provider identifier (e.g. "openai", "local").
	Name() string

	// Transcribe converts audio to text.
	Transcribe(ctx context.Context, req TranscribeRequest) (*TranscribeResult, error)
}

// TranscribeRequest holds the input parameters for a transcription.
type TranscribeRequest struct {
	Audio       io.Reader `json:"-"`                     // audio data stream
	Filename    string    `json:"filename"`              // e.g. "audio.wav"
	Language    string    `json:"language,omitempty"`    // ISO 639-1 code (optional)
	Prompt      string    `json:"prompt,omitempty"`      // context hint (optional)
	Format      string    `json:"format,omitempty"`      // response format: json, text, srt, vtt
	Temperature float64   `json:"temperature,omitempty"` // 0.0-1.0
}

// TranscribeResult holds the transcription output.
type TranscribeResult struct {
	Text     string        `json:"text"`
	Language string        `json:"language,omitempty"`
	Duration time.Duration `json:"duration,omitempty"`
	Segments []Segment     `json:"segments,omitempty"`
}

// Segment represents a timed portion of the transcription.
type Segment struct {
	Start time.Duration `json:"start"`
	End   time.Duration `json:"end"`
	Text  string        `json:"text"`
}

// ---------------------------------------------------------------------------
// Configuration
// ---------------------------------------------------------------------------

// ProviderConfig holds the top-level STT configuration.
type ProviderConfig struct {
	APIKey  string        `json:"api_key,omitempty" yaml:"api_key,omitempty"`
	BaseURL string        `json:"base_url,omitempty" yaml:"base_url,omitempty"` // override API endpoint
	Model   string        `json:"model,omitempty" yaml:"model,omitempty"`       // model name override
	Timeout time.Duration `json:"timeout,omitempty" yaml:"timeout,omitempty"`   // HTTP timeout override
}

// ---------------------------------------------------------------------------
// Provider names
// ---------------------------------------------------------------------------

const (
	providerOpenAI = "openai"
	providerLocal  = "local"
)

// ---------------------------------------------------------------------------
// Constants
// ---------------------------------------------------------------------------

const (
	// maxAudioSize is the maximum audio upload size (25 MiB, OpenAI limit).
	maxAudioSize = 25 * 1024 * 1024

	// defaultResponseFormat is the default response format for transcription.
	defaultResponseFormat = "verbose_json"

	// minTemperature is the minimum allowed temperature value.
	minTemperature = 0.0

	// maxTemperature is the maximum allowed temperature value.
	maxTemperature = 1.0
)

// allowedExtensions lists the audio file extensions accepted by the OpenAI API.
var allowedExtensions = map[string]bool{
	".flac": true,
	".mp3":  true,
	".mp4":  true,
	".mpeg": true,
	".mpga": true,
	".m4a":  true,
	".ogg":  true,
	".wav":  true,
	".webm": true,
}

// ---------------------------------------------------------------------------
// Sentinel errors
// ---------------------------------------------------------------------------

// ErrLocalWhisperNotAvailable is returned when the local whisper CLI binary
// cannot be found on the system PATH.
var ErrLocalWhisperNotAvailable = errors.New("local whisper binary not available")

// ---------------------------------------------------------------------------
// Factory
// ---------------------------------------------------------------------------

// NewProvider creates a Provider for the given backend name.
func NewProvider(name string, cfg ProviderConfig) (Provider, error) {
	switch strings.ToLower(name) {
	case providerOpenAI:
		return newOpenAIProvider(cfg)
	case providerLocal:
		return newLocalProvider(cfg)
	default:
		return nil, fmt.Errorf("unknown stt provider: %q", name)
	}
}

// ---------------------------------------------------------------------------
// Request validation
// ---------------------------------------------------------------------------

// Validate checks that the TranscribeRequest has the required fields and
// that the values are within acceptable ranges.
func (r *TranscribeRequest) Validate() error {
	if r.Audio == nil {
		return fmt.Errorf("stt: audio reader is required")
	}
	if r.Filename == "" {
		return fmt.Errorf("stt: filename is required")
	}

	ext := strings.ToLower(filepath.Ext(r.Filename))
	if !allowedExtensions[ext] {
		return fmt.Errorf("stt: unsupported file extension %q", ext)
	}

	if r.Temperature < minTemperature || r.Temperature > maxTemperature {
		return fmt.Errorf("stt: temperature must be between %.1f and %.1f", minTemperature, maxTemperature)
	}

	if r.Format != "" {
		switch r.Format {
		case "json", "text", "srt", "vtt", "verbose_json":
			// valid
		default:
			return fmt.Errorf("stt: unsupported response format %q", r.Format)
		}
	}

	return nil
}

package voice

// ---------------------------------------------------------------------------
// Configuration types
// ---------------------------------------------------------------------------

// Config holds the top-level TTS configuration.
type Config struct {
	Enabled    bool             `json:"enabled" yaml:"enabled"`
	Provider   string           `json:"provider" yaml:"provider"`                     // "openai", "elevenlabs", "edge"
	Fallback   string           `json:"fallback,omitempty" yaml:"fallback,omitempty"` // fallback provider name
	OpenAI     OpenAIConfig     `json:"openai,omitempty" yaml:"openai,omitempty"`
	ElevenLabs ElevenLabsConfig `json:"elevenlabs,omitempty" yaml:"elevenlabs,omitempty"`
	Edge       EdgeConfig       `json:"edge,omitempty" yaml:"edge,omitempty"`
}

// OpenAIConfig holds configuration for the OpenAI TTS provider.
type OpenAIConfig struct {
	APIKey string `json:"api_key,omitempty" yaml:"api_key,omitempty"`
	Model  string `json:"model,omitempty" yaml:"model,omitempty"` // default "tts-1"
	Voice  string `json:"voice,omitempty" yaml:"voice,omitempty"` // default "alloy"
}

// ElevenLabsConfig holds configuration for the ElevenLabs TTS provider.
type ElevenLabsConfig struct {
	APIKey  string `json:"api_key,omitempty" yaml:"api_key,omitempty"`
	VoiceID string `json:"voice_id,omitempty" yaml:"voice_id,omitempty"`
	ModelID string `json:"model_id,omitempty" yaml:"model_id,omitempty"` // default "eleven_monolingual_v1"
}

// EdgeConfig holds configuration for the Edge TTS provider.
type EdgeConfig struct {
	Voice string `json:"voice,omitempty" yaml:"voice,omitempty"` // default "en-US-AriaNeural"
}

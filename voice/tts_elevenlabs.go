package voice

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// ---------------------------------------------------------------------------
// ElevenLabs TTS constants
// ---------------------------------------------------------------------------

const (
	elevenLabsBaseURL          = "https://api.elevenlabs.io/v1/text-to-speech"
	defaultElevenLabsModelID   = "eleven_monolingual_v1"
	defaultElevenLabsVoiceID   = "21m00Tcm4TlvDq8ikWAM" // "Rachel" — ElevenLabs default
	elevenLabsAPIKeyHeader     = "xi-api-key"
	elevenLabsAudioContentType = "audio/mpeg"
)

// ---------------------------------------------------------------------------
// ElevenLabs TTS provider
// ---------------------------------------------------------------------------

type elevenLabsProvider struct {
	apiKey     string
	voiceID    string
	modelID    string
	httpClient *http.Client
}

func newElevenLabsProvider(cfg ElevenLabsConfig) (*elevenLabsProvider, error) {
	if cfg.APIKey == "" {
		return nil, fmt.Errorf("elevenlabs tts: api key is required")
	}
	voiceID := cfg.VoiceID
	if voiceID == "" {
		voiceID = defaultElevenLabsVoiceID
	}
	modelID := cfg.ModelID
	if modelID == "" {
		modelID = defaultElevenLabsModelID
	}
	return &elevenLabsProvider{
		apiKey:  cfg.APIKey,
		voiceID: voiceID,
		modelID: modelID,
		httpClient: &http.Client{
			Timeout: ttsHTTPTimeout,
		},
	}, nil
}

func (p *elevenLabsProvider) Name() string { return providerElevenLabs }

// elevenLabsTTSRequest is the request body for the ElevenLabs TTS endpoint.
type elevenLabsTTSRequest struct {
	Text    string `json:"text"`
	ModelID string `json:"model_id"`
}

func (p *elevenLabsProvider) Synthesize(ctx context.Context, text string) (*AudioResult, error) {
	endpoint := elevenLabsBaseURL + "/" + p.voiceID

	reqBody := elevenLabsTTSRequest{
		Text:    text,
		ModelID: p.modelID,
	}

	payload, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("elevenlabs tts: marshalling request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("elevenlabs tts: creating request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(elevenLabsAPIKeyHeader, p.apiKey)

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("elevenlabs tts: sending request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("elevenlabs tts: unexpected status %d: %s", resp.StatusCode, body)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("elevenlabs tts: reading response body: %w", err)
	}

	return &AudioResult{
		Data:        data,
		ContentType: elevenLabsAudioContentType,
	}, nil
}

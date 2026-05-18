package voice

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// ---------------------------------------------------------------------------
// OpenAI TTS constants
// ---------------------------------------------------------------------------

const (
	openAITTSEndpoint      = "https://api.openai.com/v1/audio/speech"
	defaultOpenAIModel     = "tts-1"
	defaultOpenAIVoice     = "alloy"
	defaultOpenAIFormat    = "mp3"
	openAIAudioContentType = "audio/mpeg"
	ttsHTTPTimeout         = 30 * time.Second
)

// ---------------------------------------------------------------------------
// OpenAI TTS provider
// ---------------------------------------------------------------------------

type openAIProvider struct {
	apiKey     string
	model      string
	voice      string
	httpClient *http.Client
}

func newOpenAIProvider(cfg OpenAIConfig) (*openAIProvider, error) {
	if cfg.APIKey == "" {
		return nil, fmt.Errorf("openai tts: api key is required")
	}
	model := cfg.Model
	if model == "" {
		model = defaultOpenAIModel
	}
	voice := cfg.Voice
	if voice == "" {
		voice = defaultOpenAIVoice
	}
	return &openAIProvider{
		apiKey: cfg.APIKey,
		model:  model,
		voice:  voice,
		httpClient: &http.Client{
			Timeout: ttsHTTPTimeout,
		},
	}, nil
}

func (p *openAIProvider) Name() string { return providerOpenAI }

// openAITTSRequest is the request body for the OpenAI TTS endpoint.
type openAITTSRequest struct {
	Model          string `json:"model"`
	Input          string `json:"input"`
	Voice          string `json:"voice"`
	ResponseFormat string `json:"response_format"`
}

func (p *openAIProvider) Synthesize(ctx context.Context, text string) (*AudioResult, error) {
	reqBody := openAITTSRequest{
		Model:          p.model,
		Input:          text,
		Voice:          p.voice,
		ResponseFormat: defaultOpenAIFormat,
	}

	payload, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("openai tts: marshalling request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, openAITTSEndpoint, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("openai tts: creating request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+p.apiKey)

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("openai tts: sending request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("openai tts: unexpected status %d: %s", resp.StatusCode, body)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("openai tts: reading response body: %w", err)
	}

	return &AudioResult{
		Data:        data,
		ContentType: openAIAudioContentType,
	}, nil
}

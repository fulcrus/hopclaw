package media

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// ---------------------------------------------------------------------------
// Google/Gemini provider constants
// ---------------------------------------------------------------------------

const (
	googleProviderID = "google"

	googleDefaultBaseURL = "https://generativelanguage.googleapis.com/v1beta"
	googleDefaultModel   = "gemini-2.0-flash"

	googleDefaultImagePrompt = "Describe this image in detail."
	googleDefaultAudioPrompt = "Transcribe the audio."
	googleDefaultVideoPrompt = "Describe what happens in this video."

	googleDefaultTimeout = 120 * time.Second
	googleMaxInlineSize  = 20 * 1024 * 1024 // 20 MiB

	googleContentType = "Content-Type"
)

// ---------------------------------------------------------------------------
// Configuration
// ---------------------------------------------------------------------------

// GoogleConfig holds the configuration for the Google/Gemini media provider.
type GoogleConfig struct {
	BaseURL string
	APIKey  string
	Model   string
	Timeout time.Duration
}

// ---------------------------------------------------------------------------
// Provider
// ---------------------------------------------------------------------------

// GoogleProvider implements ImageProvider, AudioProvider, and VideoProvider
// using the Gemini generateContent API.
type GoogleProvider struct {
	config GoogleConfig
	client *http.Client
}

// NewGoogleProvider creates a Google/Gemini media provider.
func NewGoogleProvider(cfg GoogleConfig) (*GoogleProvider, error) {
	if cfg.APIKey == "" {
		return nil, fmt.Errorf("media/google: api key is required")
	}

	if cfg.BaseURL == "" {
		cfg.BaseURL = googleDefaultBaseURL
	}

	if cfg.Model == "" {
		cfg.Model = googleDefaultModel
	}

	timeout := cfg.Timeout
	if timeout == 0 {
		timeout = googleDefaultTimeout
	}

	return &GoogleProvider{
		config: cfg,
		client: &http.Client{Timeout: timeout},
	}, nil
}

// ID returns the provider identifier.
func (p *GoogleProvider) ID() string { return googleProviderID }

// Capabilities returns the media capabilities supported by this provider.
func (p *GoogleProvider) Capabilities() []Capability {
	return []Capability{CapabilityImage, CapabilityAudio, CapabilityVideo}
}

// ---------------------------------------------------------------------------
// Gemini API request/response types
// ---------------------------------------------------------------------------

// geminiRequest represents the generateContent request body.
type geminiRequest struct {
	Contents []geminiContent `json:"contents"`
}

// geminiContent represents a single content entry with parts.
type geminiContent struct {
	Parts []geminiPart `json:"parts"`
}

// geminiPart can be text or inline data.
type geminiPart struct {
	Text       string        `json:"text,omitempty"`
	InlineData *geminiInline `json:"inline_data,omitempty"`
}

// geminiInline holds base64-encoded media data.
type geminiInline struct {
	MIMEType string `json:"mime_type"`
	Data     string `json:"data"` // base64-encoded
}

// geminiResponse represents the generateContent response.
type geminiResponse struct {
	Candidates   []geminiCandidate `json:"candidates"`
	ModelVersion string            `json:"modelVersion,omitempty"` // Gemini API uses camelCase
}

// geminiCandidate represents one candidate response.
type geminiCandidate struct {
	Content struct {
		Parts []struct {
			Text string `json:"text"`
		} `json:"parts"`
	} `json:"content"`
}

// geminiErrorResponse represents an error response from the Gemini API.
type geminiErrorResponse struct {
	Error struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
		Status  string `json:"status"`
	} `json:"error"`
}

// ---------------------------------------------------------------------------
// Image description
// ---------------------------------------------------------------------------

// DescribeImage sends an image to Gemini for description.
func (p *GoogleProvider) DescribeImage(ctx context.Context, req ImageRequest) (*ImageResult, error) {
	if len(req.Data) == 0 {
		return nil, fmt.Errorf("media/google: image data is required")
	}
	if int64(len(req.Data)) > googleMaxInlineSize {
		return nil, fmt.Errorf("media/google: image exceeds maximum inline size of %d bytes", googleMaxInlineSize)
	}

	prompt := req.Prompt
	if prompt == "" {
		prompt = googleDefaultImagePrompt
	}

	mimeType := req.MIMEType
	if mimeType == "" {
		mimeType = DetectMIMETypeFromBytes(req.Data)
	}

	text, err := p.generateContent(ctx, prompt, mimeType, req.Data)
	if err != nil {
		return nil, fmt.Errorf("media/google: image description: %w", err)
	}

	return &ImageResult{
		Text:  text,
		Model: p.config.Model,
	}, nil
}

// ---------------------------------------------------------------------------
// Audio transcription
// ---------------------------------------------------------------------------

// TranscribeAudio sends audio to Gemini for transcription.
func (p *GoogleProvider) TranscribeAudio(ctx context.Context, req AudioRequest) (*AudioResult, error) {
	if len(req.Data) == 0 {
		return nil, fmt.Errorf("media/google: audio data is required")
	}
	if int64(len(req.Data)) > googleMaxInlineSize {
		return nil, fmt.Errorf("media/google: audio exceeds maximum inline size of %d bytes", googleMaxInlineSize)
	}

	prompt := req.Prompt
	if prompt == "" {
		prompt = googleDefaultAudioPrompt
	}
	if req.Language != "" {
		prompt = fmt.Sprintf("%s Language: %s.", prompt, req.Language)
	}

	mimeType := req.MIMEType
	if mimeType == "" {
		mimeType = DetectMIMETypeFromBytes(req.Data)
	}

	text, err := p.generateContent(ctx, prompt, mimeType, req.Data)
	if err != nil {
		return nil, fmt.Errorf("media/google: audio transcription: %w", err)
	}

	return &AudioResult{
		Text:     text,
		Language: req.Language,
		Model:    p.config.Model,
	}, nil
}

// ---------------------------------------------------------------------------
// Video description
// ---------------------------------------------------------------------------

// DescribeVideo sends video data to Gemini for description.
func (p *GoogleProvider) DescribeVideo(ctx context.Context, req VideoRequest) (*VideoResult, error) {
	if len(req.Data) == 0 {
		return nil, fmt.Errorf("media/google: video data is required")
	}
	if int64(len(req.Data)) > googleMaxInlineSize {
		return nil, fmt.Errorf("media/google: video exceeds maximum inline size of %d bytes", googleMaxInlineSize)
	}

	prompt := req.Prompt
	if prompt == "" {
		prompt = googleDefaultVideoPrompt
	}

	mimeType := req.MIMEType
	if mimeType == "" {
		mimeType = DetectMIMETypeFromBytes(req.Data)
	}

	text, err := p.generateContent(ctx, prompt, mimeType, req.Data)
	if err != nil {
		return nil, fmt.Errorf("media/google: video description: %w", err)
	}

	return &VideoResult{
		Text:  text,
		Model: p.config.Model,
	}, nil
}

// ---------------------------------------------------------------------------
// Shared generateContent call
// ---------------------------------------------------------------------------

// generateContent sends a single generateContent request to the Gemini API
// with text and inline media data.
func (p *GoogleProvider) generateContent(ctx context.Context, prompt, mimeType string, data []byte) (string, error) {
	gemReq := geminiRequest{
		Contents: []geminiContent{
			{
				Parts: []geminiPart{
					{Text: prompt},
					{InlineData: &geminiInline{
						MIMEType: mimeType,
						Data:     base64.StdEncoding.EncodeToString(data),
					}},
				},
			},
		},
	}

	body, err := json.Marshal(gemReq)
	if err != nil {
		return "", fmt.Errorf("marshaling request: %w", err)
	}

	endpoint := fmt.Sprintf("%s/models/%s:generateContent?key=%s",
		p.config.BaseURL, p.config.Model, p.config.APIKey)

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("creating request: %w", err)
	}
	httpReq.Header.Set(googleContentType, "application/json")

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return "", fmt.Errorf("sending request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("reading response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", parseGoogleError(resp.StatusCode, respBody)
	}

	var gemResp geminiResponse
	if err := json.Unmarshal(respBody, &gemResp); err != nil {
		return "", fmt.Errorf("parsing response: %w", err)
	}

	if len(gemResp.Candidates) == 0 || len(gemResp.Candidates[0].Content.Parts) == 0 {
		return "", fmt.Errorf("no content in response")
	}

	return gemResp.Candidates[0].Content.Parts[0].Text, nil
}

// ---------------------------------------------------------------------------
// Error parsing
// ---------------------------------------------------------------------------

// parseGoogleError extracts a meaningful error from a Gemini API error response.
func parseGoogleError(statusCode int, body []byte) error {
	var apiErr geminiErrorResponse
	if err := json.Unmarshal(body, &apiErr); err == nil && apiErr.Error.Message != "" {
		return fmt.Errorf("api error (status %d): %s", statusCode, apiErr.Error.Message)
	}
	return fmt.Errorf("unexpected status %d: %s", statusCode, body)
}

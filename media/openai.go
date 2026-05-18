package media

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"time"
)

// ---------------------------------------------------------------------------
// OpenAI provider constants
// ---------------------------------------------------------------------------

const (
	openaiProviderID = "openai"

	openaiDefaultBaseURL = "https://api.openai.com/v1"

	openaiDefaultVisionModel  = "gpt-4o"
	openaiDefaultWhisperModel = "whisper-1"

	openaiDefaultImagePrompt = "Describe this image in detail."

	openaiChatEndpoint          = "/chat/completions"
	openaiTranscriptionEndpoint = "/audio/transcriptions"

	openaiDefaultTimeout = 60 * time.Second
	openaiMaxAudioSize   = 25 * 1024 * 1024 // 25 MiB

	openaiAuthHeader    = "Authorization"
	openaiContentType   = "Content-Type"
	openaiAuthPrefix    = "Bearer "
	openaiFormFieldFile = "file"
)

// ---------------------------------------------------------------------------
// Configuration
// ---------------------------------------------------------------------------

// OpenAIConfig holds the configuration for the OpenAI media provider.
type OpenAIConfig struct {
	BaseURL string
	APIKey  string
	Timeout time.Duration
}

// ---------------------------------------------------------------------------
// Provider
// ---------------------------------------------------------------------------

// OpenAIProvider implements ImageProvider and AudioProvider using OpenAI APIs.
type OpenAIProvider struct {
	config OpenAIConfig
	client *http.Client
}

// NewOpenAIProvider creates an OpenAI media provider.
func NewOpenAIProvider(cfg OpenAIConfig) (*OpenAIProvider, error) {
	if cfg.APIKey == "" {
		return nil, fmt.Errorf("media/openai: api key is required")
	}

	if cfg.BaseURL == "" {
		cfg.BaseURL = openaiDefaultBaseURL
	}

	timeout := cfg.Timeout
	if timeout == 0 {
		timeout = openaiDefaultTimeout
	}

	return &OpenAIProvider{
		config: cfg,
		client: &http.Client{Timeout: timeout},
	}, nil
}

// ID returns the provider identifier.
func (p *OpenAIProvider) ID() string { return openaiProviderID }

// Capabilities returns the media capabilities supported by this provider.
func (p *OpenAIProvider) Capabilities() []Capability {
	return []Capability{CapabilityImage, CapabilityAudio}
}

// ---------------------------------------------------------------------------
// Image description (GPT-4o with vision)
// ---------------------------------------------------------------------------

// openaiChatRequest represents the chat completions request body.
type openaiChatRequest struct {
	Model     string              `json:"model"`
	Messages  []openaiChatMessage `json:"messages"`
	MaxTokens int                 `json:"max_tokens"`
}

// openaiChatMessage represents a single message in the chat.
type openaiChatMessage struct {
	Role    string       `json:"role"`
	Content []openaiPart `json:"content"`
}

// openaiPart represents a content part (text or image_url).
type openaiPart struct {
	Type     string          `json:"type"`
	Text     string          `json:"text,omitempty"`
	ImageURL *openaiImageURL `json:"image_url,omitempty"`
}

// openaiImageURL holds the data URL for inline image content.
type openaiImageURL struct {
	URL string `json:"url"`
}

// openaiChatResponse is the response from chat completions.
type openaiChatResponse struct {
	Choices []openaiChoice `json:"choices"`
	Model   string         `json:"model"`
}

// openaiChoice represents one completion choice.
type openaiChoice struct {
	Message struct {
		Content string `json:"content"`
	} `json:"message"`
}

// openaiErrorResponse represents an error response from the OpenAI API.
type openaiErrorResponse struct {
	Error struct {
		Message string `json:"message"`
		Type    string `json:"type"`
	} `json:"error"`
}

const (
	openaiVisionMaxTokens  = 1024
	openaiPartTypeText     = "text"
	openaiPartTypeImageURL = "image_url"
	openaiRoleUser         = "user"
)

// DescribeImage sends an image to GPT-4o for description.
func (p *OpenAIProvider) DescribeImage(ctx context.Context, req ImageRequest) (*ImageResult, error) {
	if len(req.Data) == 0 {
		return nil, fmt.Errorf("media/openai: image data is required")
	}

	prompt := req.Prompt
	if prompt == "" {
		prompt = openaiDefaultImagePrompt
	}

	mimeType := req.MIMEType
	if mimeType == "" {
		mimeType = DetectMIMETypeFromBytes(req.Data)
	}

	dataURL := fmt.Sprintf("data:%s;base64,%s", mimeType, base64.StdEncoding.EncodeToString(req.Data))

	chatReq := openaiChatRequest{
		Model:     openaiDefaultVisionModel,
		MaxTokens: openaiVisionMaxTokens,
		Messages: []openaiChatMessage{
			{
				Role: openaiRoleUser,
				Content: []openaiPart{
					{Type: openaiPartTypeText, Text: prompt},
					{Type: openaiPartTypeImageURL, ImageURL: &openaiImageURL{URL: dataURL}},
				},
			},
		},
	}

	body, err := json.Marshal(chatReq)
	if err != nil {
		return nil, fmt.Errorf("media/openai: marshaling request: %w", err)
	}

	endpoint := p.config.BaseURL + openaiChatEndpoint
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("media/openai: creating request: %w", err)
	}
	httpReq.Header.Set(openaiContentType, "application/json")
	httpReq.Header.Set(openaiAuthHeader, openaiAuthPrefix+p.config.APIKey)

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("media/openai: sending image request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("media/openai: reading image response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, parseOpenAIMediaError(resp.StatusCode, respBody)
	}

	var chatResp openaiChatResponse
	if err := json.Unmarshal(respBody, &chatResp); err != nil {
		return nil, fmt.Errorf("media/openai: parsing image response: %w", err)
	}

	if len(chatResp.Choices) == 0 {
		return nil, fmt.Errorf("media/openai: no choices in image response")
	}

	return &ImageResult{
		Text:  chatResp.Choices[0].Message.Content,
		Model: chatResp.Model,
	}, nil
}

// ---------------------------------------------------------------------------
// Audio transcription (Whisper)
// ---------------------------------------------------------------------------

// openaiTranscriptionResp is the JSON response from the Whisper API.
type openaiTranscriptionResp struct {
	Text     string `json:"text"`
	Language string `json:"language"`
}

const (
	openaiFormModel    = "model"
	openaiFormLanguage = "language"
	openaiFormPrompt   = "prompt"
)

// TranscribeAudio sends audio to the Whisper API for transcription.
func (p *OpenAIProvider) TranscribeAudio(ctx context.Context, req AudioRequest) (*AudioResult, error) {
	if len(req.Data) == 0 {
		return nil, fmt.Errorf("media/openai: audio data is required")
	}
	if len(req.Data) > openaiMaxAudioSize {
		return nil, fmt.Errorf("media/openai: audio exceeds maximum size of %d bytes", openaiMaxAudioSize)
	}

	body, contentType, err := p.buildWhisperForm(req)
	if err != nil {
		return nil, err
	}

	endpoint := p.config.BaseURL + openaiTranscriptionEndpoint
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, body)
	if err != nil {
		return nil, fmt.Errorf("media/openai: creating audio request: %w", err)
	}
	httpReq.Header.Set(openaiContentType, contentType)
	httpReq.Header.Set(openaiAuthHeader, openaiAuthPrefix+p.config.APIKey)

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("media/openai: sending audio request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("media/openai: reading audio response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, parseOpenAIMediaError(resp.StatusCode, respBody)
	}

	var transcription openaiTranscriptionResp
	if err := json.Unmarshal(respBody, &transcription); err != nil {
		return nil, fmt.Errorf("media/openai: parsing audio response: %w", err)
	}

	return &AudioResult{
		Text:     transcription.Text,
		Language: transcription.Language,
		Model:    openaiDefaultWhisperModel,
	}, nil
}

// buildWhisperForm constructs the multipart/form-data body for the Whisper API.
func (p *OpenAIProvider) buildWhisperForm(req AudioRequest) (*bytes.Buffer, string, error) {
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)

	// Determine filename from MIME type.
	filename := mimeToFilename(req.MIMEType)

	part, err := writer.CreateFormFile(openaiFormFieldFile, filename)
	if err != nil {
		return nil, "", fmt.Errorf("media/openai: creating form file: %w", err)
	}
	if _, err := part.Write(req.Data); err != nil {
		return nil, "", fmt.Errorf("media/openai: writing audio data: %w", err)
	}

	if err := writer.WriteField(openaiFormModel, openaiDefaultWhisperModel); err != nil {
		return nil, "", fmt.Errorf("media/openai: writing model field: %w", err)
	}

	if req.Language != "" {
		if err := writer.WriteField(openaiFormLanguage, req.Language); err != nil {
			return nil, "", fmt.Errorf("media/openai: writing language field: %w", err)
		}
	}

	if req.Prompt != "" {
		if err := writer.WriteField(openaiFormPrompt, req.Prompt); err != nil {
			return nil, "", fmt.Errorf("media/openai: writing prompt field: %w", err)
		}
	}

	if err := writer.Close(); err != nil {
		return nil, "", fmt.Errorf("media/openai: closing multipart writer: %w", err)
	}

	return &buf, writer.FormDataContentType(), nil
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// mimeToFilename returns a default filename for the given MIME type.
func mimeToFilename(mimeType string) string {
	switch mimeType {
	case "audio/wav", "audio/x-wav":
		return "audio.wav"
	case "audio/mpeg", "audio/mp3":
		return "audio.mp3"
	case "audio/ogg":
		return "audio.ogg"
	case "audio/flac", "audio/x-flac":
		return "audio.flac"
	case "audio/mp4", "audio/x-m4a":
		return "audio.m4a"
	case "audio/webm":
		return "audio.webm"
	case "audio/aac":
		return "audio.aac"
	default:
		return "audio.wav"
	}
}

// parseOpenAIMediaError extracts a meaningful error from an OpenAI API error response.
func parseOpenAIMediaError(statusCode int, body []byte) error {
	var apiErr openaiErrorResponse
	if err := json.Unmarshal(body, &apiErr); err == nil && apiErr.Error.Message != "" {
		return fmt.Errorf("media/openai: api error (status %d): %s", statusCode, apiErr.Error.Message)
	}
	return fmt.Errorf("media/openai: unexpected status %d: %s", statusCode, body)
}

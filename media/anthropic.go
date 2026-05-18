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
// Anthropic provider constants
// ---------------------------------------------------------------------------

const (
	anthropicProviderID = "anthropic"

	anthropicDefaultBaseURL = "https://api.anthropic.com/v1"
	anthropicDefaultModel   = "claude-sonnet-4-5-20250514"

	anthropicDefaultImagePrompt = "Describe this image in detail."

	anthropicAPIVersion     = "2023-06-01"
	anthropicDefaultTimeout = 60 * time.Second

	anthropicMessagesEndpoint = "/messages"

	anthropicHeaderAPIKey  = "x-api-key"
	anthropicHeaderVersion = "anthropic-version"
	anthropicContentType   = "Content-Type"
	anthropicMaxTokens     = 1024
)

// ---------------------------------------------------------------------------
// Configuration
// ---------------------------------------------------------------------------

// AnthropicConfig holds the configuration for the Anthropic media provider.
type AnthropicConfig struct {
	BaseURL string
	APIKey  string
	Model   string
	Timeout time.Duration
}

// ---------------------------------------------------------------------------
// Provider
// ---------------------------------------------------------------------------

// AnthropicProvider implements ImageProvider using the Claude Messages API.
type AnthropicProvider struct {
	config AnthropicConfig
	client *http.Client
}

// NewAnthropicProvider creates an Anthropic media provider.
func NewAnthropicProvider(cfg AnthropicConfig) (*AnthropicProvider, error) {
	if cfg.APIKey == "" {
		return nil, fmt.Errorf("media/anthropic: api key is required")
	}

	if cfg.BaseURL == "" {
		cfg.BaseURL = anthropicDefaultBaseURL
	}

	if cfg.Model == "" {
		cfg.Model = anthropicDefaultModel
	}

	timeout := cfg.Timeout
	if timeout == 0 {
		timeout = anthropicDefaultTimeout
	}

	return &AnthropicProvider{
		config: cfg,
		client: &http.Client{Timeout: timeout},
	}, nil
}

// ID returns the provider identifier.
func (p *AnthropicProvider) ID() string { return anthropicProviderID }

// Capabilities returns the media capabilities supported by this provider.
func (p *AnthropicProvider) Capabilities() []Capability {
	return []Capability{CapabilityImage}
}

// ---------------------------------------------------------------------------
// Claude Messages API request/response types
// ---------------------------------------------------------------------------

// anthropicRequest represents the Messages API request body.
type anthropicRequest struct {
	Model     string             `json:"model"`
	MaxTokens int                `json:"max_tokens"`
	Messages  []anthropicMessage `json:"messages"`
}

// anthropicMessage represents a single message in the conversation.
type anthropicMessage struct {
	Role    string          `json:"role"`
	Content []anthropicPart `json:"content"`
}

// anthropicPart can be a text block or an image block.
type anthropicPart struct {
	Type   string           `json:"type"`
	Text   string           `json:"text,omitempty"`
	Source *anthropicSource `json:"source,omitempty"`
}

// anthropicSource holds base64-encoded image data.
type anthropicSource struct {
	Type      string `json:"type"`       // always "base64"
	MediaType string `json:"media_type"` // e.g., "image/jpeg"
	Data      string `json:"data"`       // base64-encoded
}

// anthropicResponse represents the Messages API response.
type anthropicResponse struct {
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
	Model string `json:"model"`
}

// anthropicErrorResponse represents an error response from the Claude API.
type anthropicErrorResponse struct {
	Error struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"error"`
}

const (
	anthropicRoleUser     = "user"
	anthropicTypeText     = "text"
	anthropicTypeImage    = "image"
	anthropicSourceBase64 = "base64"
)

// ---------------------------------------------------------------------------
// Image description
// ---------------------------------------------------------------------------

// DescribeImage sends an image to Claude for description.
func (p *AnthropicProvider) DescribeImage(ctx context.Context, req ImageRequest) (*ImageResult, error) {
	if len(req.Data) == 0 {
		return nil, fmt.Errorf("media/anthropic: image data is required")
	}

	prompt := req.Prompt
	if prompt == "" {
		prompt = anthropicDefaultImagePrompt
	}

	mimeType := req.MIMEType
	if mimeType == "" {
		mimeType = DetectMIMETypeFromBytes(req.Data)
	}

	anthropicReq := anthropicRequest{
		Model:     p.config.Model,
		MaxTokens: anthropicMaxTokens,
		Messages: []anthropicMessage{
			{
				Role: anthropicRoleUser,
				Content: []anthropicPart{
					{
						Type: anthropicTypeImage,
						Source: &anthropicSource{
							Type:      anthropicSourceBase64,
							MediaType: mimeType,
							Data:      base64.StdEncoding.EncodeToString(req.Data),
						},
					},
					{
						Type: anthropicTypeText,
						Text: prompt,
					},
				},
			},
		},
	}

	body, err := json.Marshal(anthropicReq)
	if err != nil {
		return nil, fmt.Errorf("media/anthropic: marshaling request: %w", err)
	}

	endpoint := p.config.BaseURL + anthropicMessagesEndpoint
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("media/anthropic: creating request: %w", err)
	}
	httpReq.Header.Set(anthropicContentType, "application/json")
	httpReq.Header.Set(anthropicHeaderAPIKey, p.config.APIKey)
	httpReq.Header.Set(anthropicHeaderVersion, anthropicAPIVersion)

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("media/anthropic: sending request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("media/anthropic: reading response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, parseAnthropicError(resp.StatusCode, respBody)
	}

	var anthropicResp anthropicResponse
	if err := json.Unmarshal(respBody, &anthropicResp); err != nil {
		return nil, fmt.Errorf("media/anthropic: parsing response: %w", err)
	}

	if len(anthropicResp.Content) == 0 {
		return nil, fmt.Errorf("media/anthropic: no content in response")
	}

	return &ImageResult{
		Text:  anthropicResp.Content[0].Text,
		Model: anthropicResp.Model,
	}, nil
}

// ---------------------------------------------------------------------------
// Error parsing
// ---------------------------------------------------------------------------

// parseAnthropicError extracts a meaningful error from a Claude API error response.
func parseAnthropicError(statusCode int, body []byte) error {
	var apiErr anthropicErrorResponse
	if err := json.Unmarshal(body, &apiErr); err == nil && apiErr.Error.Message != "" {
		return fmt.Errorf("media/anthropic: api error (status %d): %s", statusCode, apiErr.Error.Message)
	}
	return fmt.Errorf("media/anthropic: unexpected status %d: %s", statusCode, body)
}

package model

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/fulcrus/hopclaw/agent"
)

const (
	defaultGeminiEmbeddingBaseURL = "https://generativelanguage.googleapis.com"
	defaultGeminiEmbeddingModel   = "text-embedding-004"
	defaultGeminiEmbeddingTimeout = 30 * time.Second
	geminiEmbedPathFmt            = "/v1beta/models/%s:batchEmbedContents"
)

// GeminiEmbeddingConfig configures a Gemini embedding client.
type GeminiEmbeddingConfig struct {
	BaseURL string
	APIKey  string
	Model   string        // default: "text-embedding-004"
	Timeout time.Duration // default: 30s
}

// GeminiEmbeddingClient calls the Google Gemini batchEmbedContents API.
type GeminiEmbeddingClient struct {
	config GeminiEmbeddingConfig
	client *http.Client
}

func init() {
	RegisterEmbeddingClientBuilder(EmbedGemini, func(input EmbeddingClientBuildInput) (agent.EmbeddingClient, error) {
		return NewGeminiEmbeddingClient(GeminiEmbeddingConfig(input)), nil
	})
}

// NewGeminiEmbeddingClient creates a Gemini embedding client with the given config.
func NewGeminiEmbeddingClient(cfg GeminiEmbeddingConfig) *GeminiEmbeddingClient {
	if cfg.BaseURL == "" {
		cfg.BaseURL = defaultGeminiEmbeddingBaseURL
	}
	if cfg.Model == "" {
		cfg.Model = defaultGeminiEmbeddingModel
	}
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = defaultGeminiEmbeddingTimeout
	}
	return &GeminiEmbeddingClient{
		config: cfg,
		client: &http.Client{Timeout: timeout},
	}
}

// ---------------------------------------------------------------------------
// Wire types
// ---------------------------------------------------------------------------

type geminiBatchEmbedRequest struct {
	Requests []geminiEmbedContentRequest `json:"requests"`
}

type geminiEmbedContentRequest struct {
	Model   string            `json:"model"`
	Content geminiTextContent `json:"content"`
}

type geminiTextContent struct {
	Parts []geminiTextPart `json:"parts"`
}

type geminiTextPart struct {
	Text string `json:"text"`
}

type geminiBatchEmbedResponse struct {
	Embeddings []geminiEmbeddingValues `json:"embeddings"`
}

type geminiEmbeddingValues struct {
	Values []float32 `json:"values"`
}

// Embed generates vector embeddings for the given texts using the Gemini
// batchEmbedContents endpoint.
func (c *GeminiEmbeddingClient) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, nil
	}

	modelRef := "models/" + c.config.Model
	requests := make([]geminiEmbedContentRequest, len(texts))
	for i, text := range texts {
		requests[i] = geminiEmbedContentRequest{
			Model: modelRef,
			Content: geminiTextContent{
				Parts: []geminiTextPart{{Text: text}},
			},
		}
	}

	reqBody := geminiBatchEmbedRequest{Requests: requests}
	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("gemini embedding: failed to marshal request: %w", err)
	}

	url := fmt.Sprintf("%s"+geminiEmbedPathFmt+"?key=%s", c.config.BaseURL, c.config.Model, c.config.APIKey)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("gemini embedding: failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("gemini embedding: request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("gemini embedding: failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("gemini embedding: API returned status %d: %s", resp.StatusCode, string(respBody))
	}

	var batchResp geminiBatchEmbedResponse
	if err := json.Unmarshal(respBody, &batchResp); err != nil {
		return nil, fmt.Errorf("gemini embedding: failed to unmarshal response: %w", err)
	}

	if len(batchResp.Embeddings) != len(texts) {
		return nil, fmt.Errorf("gemini embedding: expected %d embeddings, got %d", len(texts), len(batchResp.Embeddings))
	}

	results := make([][]float32, len(texts))
	for i, emb := range batchResp.Embeddings {
		results[i] = emb.Values
	}
	return results, nil
}

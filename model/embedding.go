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
	defaultEmbeddingModel   = "text-embedding-3-small"
	defaultEmbeddingTimeout = 30 * time.Second
	embeddingsPath          = "/v1/embeddings"
)

// EmbeddingConfig configures an OpenAI-compatible embedding client.
type EmbeddingConfig struct {
	BaseURL string
	APIKey  string
	Model   string        // default: "text-embedding-3-small"
	Timeout time.Duration // default: 30s
}

// EmbeddingClient calls an OpenAI-compatible /v1/embeddings endpoint.
type EmbeddingClient struct {
	config EmbeddingConfig
	client *http.Client
}

func init() {
	RegisterEmbeddingClientBuilder(EmbedOpenAI, func(input EmbeddingClientBuildInput) (agent.EmbeddingClient, error) {
		return NewEmbeddingClient(EmbeddingConfig(input)), nil
	})
}

// NewEmbeddingClient creates an embedding client with the given config.
// Applies default values for Model and Timeout when not set.
func NewEmbeddingClient(cfg EmbeddingConfig) *EmbeddingClient {
	if cfg.Model == "" {
		cfg.Model = defaultEmbeddingModel
	}
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = defaultEmbeddingTimeout
	}
	return &EmbeddingClient{
		config: cfg,
		client: &http.Client{Timeout: timeout},
	}
}

// embeddingRequest is the JSON body for the /v1/embeddings API.
type embeddingRequest struct {
	Input []string `json:"input"`
	Model string   `json:"model"`
}

// embeddingResponse is the JSON response from the /v1/embeddings API.
type embeddingResponse struct {
	Data []embeddingData `json:"data"`
}

// embeddingData is a single embedding entry in the API response.
type embeddingData struct {
	Embedding []float32 `json:"embedding"`
	Index     int       `json:"index"`
}

// Embed generates vector embeddings for the given texts using the
// OpenAI-compatible /v1/embeddings endpoint.
func (c *EmbeddingClient) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, nil
	}

	reqBody := embeddingRequest{
		Input: texts,
		Model: c.config.Model,
	}
	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("embedding: failed to marshal request: %w", err)
	}

	url := c.config.BaseURL + embeddingsPath
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("embedding: failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if c.config.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.config.APIKey)
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("embedding: request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("embedding: failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("embedding: API returned status %d: %s", resp.StatusCode, string(respBody))
	}

	var embResp embeddingResponse
	if err := json.Unmarshal(respBody, &embResp); err != nil {
		return nil, fmt.Errorf("embedding: failed to unmarshal response: %w", err)
	}

	if len(embResp.Data) != len(texts) {
		return nil, fmt.Errorf("embedding: expected %d embeddings, got %d", len(texts), len(embResp.Data))
	}

	// Sort results by index to match input ordering.
	results := make([][]float32, len(texts))
	for _, d := range embResp.Data {
		if d.Index < 0 || d.Index >= len(texts) {
			return nil, fmt.Errorf("embedding: response index %d out of range [0, %d)", d.Index, len(texts))
		}
		results[d.Index] = d.Embedding
	}

	return results, nil
}

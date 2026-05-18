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
	defaultOllamaEmbeddingBaseURL = "http://localhost:11434"
	defaultOllamaEmbeddingModel   = "nomic-embed-text"
	defaultOllamaEmbeddingTimeout = 30 * time.Second
	ollamaEmbedPath               = "/api/embed"
)

// OllamaEmbeddingConfig configures an Ollama embedding client.
type OllamaEmbeddingConfig struct {
	BaseURL string
	APIKey  string        // optional; Ollama does not require auth by default
	Model   string        // default: "nomic-embed-text"
	Timeout time.Duration // default: 30s
}

// OllamaEmbeddingClient calls the Ollama native /api/embed endpoint.
type OllamaEmbeddingClient struct {
	config OllamaEmbeddingConfig
	client *http.Client
}

func init() {
	RegisterEmbeddingClientBuilder(EmbedOllama, func(input EmbeddingClientBuildInput) (agent.EmbeddingClient, error) {
		return NewOllamaEmbeddingClient(OllamaEmbeddingConfig(input)), nil
	})
}

// NewOllamaEmbeddingClient creates an Ollama embedding client with the given config.
func NewOllamaEmbeddingClient(cfg OllamaEmbeddingConfig) *OllamaEmbeddingClient {
	if cfg.BaseURL == "" {
		cfg.BaseURL = defaultOllamaEmbeddingBaseURL
	}
	if cfg.Model == "" {
		cfg.Model = defaultOllamaEmbeddingModel
	}
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = defaultOllamaEmbeddingTimeout
	}
	return &OllamaEmbeddingClient{
		config: cfg,
		client: &http.Client{Timeout: timeout},
	}
}

// ---------------------------------------------------------------------------
// Wire types
// ---------------------------------------------------------------------------

type ollamaEmbedRequest struct {
	Model string   `json:"model"`
	Input []string `json:"input"`
}

type ollamaEmbedResponse struct {
	Embeddings [][]float32 `json:"embeddings"`
}

// Embed generates vector embeddings for the given texts using the Ollama
// /api/embed endpoint.
func (c *OllamaEmbeddingClient) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, nil
	}

	reqBody := ollamaEmbedRequest{
		Model: c.config.Model,
		Input: texts,
	}
	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("ollama embedding: failed to marshal request: %w", err)
	}

	url := c.config.BaseURL + ollamaEmbedPath
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("ollama embedding: failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if c.config.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.config.APIKey)
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("ollama embedding: request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("ollama embedding: failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("ollama embedding: API returned status %d: %s", resp.StatusCode, string(respBody))
	}

	var embedResp ollamaEmbedResponse
	if err := json.Unmarshal(respBody, &embedResp); err != nil {
		return nil, fmt.Errorf("ollama embedding: failed to unmarshal response: %w", err)
	}

	if len(embedResp.Embeddings) != len(texts) {
		return nil, fmt.Errorf("ollama embedding: expected %d embeddings, got %d", len(texts), len(embedResp.Embeddings))
	}

	return embedResp.Embeddings, nil
}

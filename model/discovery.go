package model

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	discoveryTimeout   = 10 * time.Second
	discoveryMaxModels = 200
	discoveryMaxBody   = 2 * 1024 * 1024 // 2 MiB max response body
)

// ---------------------------------------------------------------------------
// DiscoveredModel
// ---------------------------------------------------------------------------

// DiscoveredModel represents a model found via provider API discovery.
type DiscoveredModel struct {
	ID      string `json:"id"`
	Object  string `json:"object,omitempty"`
	OwnedBy string `json:"owned_by,omitempty"`
	Created int64  `json:"created,omitempty"`
}

// ---------------------------------------------------------------------------
// OpenAI-compatible discovery
// ---------------------------------------------------------------------------

// openAIModelsResponse is the standard OpenAI /models response envelope.
type openAIModelsResponse struct {
	Data []DiscoveredModel `json:"data"`
}

// DiscoverModels queries a provider's /models endpoint to get available models.
// This works for OpenAI-compatible providers that expose GET /models.
// It handles both the standard {"data": [...]} envelope and a plain array response.
func DiscoverModels(ctx context.Context, baseURL, apiKey string, headers map[string]string) ([]DiscoveredModel, error) {
	endpoint := strings.TrimRight(baseURL, "/") + "/models"

	ctx, cancel := context.WithTimeout(ctx, discoveryTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("discovery: building request: %w", err)
	}
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("discovery: request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, discoveryMaxBody))
		return nil, fmt.Errorf("discovery: unexpected status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, discoveryMaxBody))
	if err != nil {
		return nil, fmt.Errorf("discovery: reading response: %w", err)
	}

	models, err := parseModelsResponse(body)
	if err != nil {
		return nil, fmt.Errorf("discovery: parsing response: %w", err)
	}

	if len(models) > discoveryMaxModels {
		models = models[:discoveryMaxModels]
	}
	return models, nil
}

// parseModelsResponse handles both {"data": [...]} and plain [...] formats.
func parseModelsResponse(body []byte) ([]DiscoveredModel, error) {
	// Try the standard {"data": [...]} envelope first.
	var envelope openAIModelsResponse
	if err := json.Unmarshal(body, &envelope); err == nil && envelope.Data != nil {
		return envelope.Data, nil
	}

	// Fall back to a plain array.
	var models []DiscoveredModel
	if err := json.Unmarshal(body, &models); err != nil {
		return nil, fmt.Errorf("unrecognized models response format: %w", err)
	}
	return models, nil
}

// ---------------------------------------------------------------------------
// Ollama discovery
// ---------------------------------------------------------------------------

// ollamaTagsResponse is the response from Ollama's /api/tags endpoint.
type ollamaTagsResponse struct {
	Models []ollamaModelEntry `json:"models"`
}

// ollamaModelEntry is a single model entry from Ollama's /api/tags.
type ollamaModelEntry struct {
	Name  string `json:"name"`
	Model string `json:"model"`
}

// ollamaShowRequest is the request body for Ollama's /api/show endpoint.
type ollamaShowRequest struct {
	Name string `json:"name"`
}

// ollamaShowResponse is the response from Ollama's /api/show endpoint.
type ollamaShowResponse struct {
	ModelInfo  map[string]any `json:"model_info"`
	Parameters string         `json:"parameters"`
}

// stripV1Suffix removes a trailing /v1 from the base URL so that Ollama's
// native endpoints (which live outside /v1) can be reached.
func stripV1Suffix(baseURL string) string {
	trimmed := strings.TrimRight(baseURL, "/")
	trimmed = strings.TrimSuffix(trimmed, "/v1")
	return trimmed
}

// OllamaDiscoverModels queries Ollama's /api/tags endpoint.
func OllamaDiscoverModels(ctx context.Context, baseURL string) ([]DiscoveredModel, error) {
	endpoint := stripV1Suffix(baseURL) + "/api/tags"

	ctx, cancel := context.WithTimeout(ctx, discoveryTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("ollama discovery: building request: %w", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("ollama discovery: request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, discoveryMaxBody))
		return nil, fmt.Errorf("ollama discovery: unexpected status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var tagsResp ollamaTagsResponse
	if err := json.NewDecoder(resp.Body).Decode(&tagsResp); err != nil {
		return nil, fmt.Errorf("ollama discovery: parsing response: %w", err)
	}

	models := make([]DiscoveredModel, 0, len(tagsResp.Models))
	for _, m := range tagsResp.Models {
		id := m.Name
		if id == "" {
			id = m.Model
		}
		if id == "" {
			continue
		}
		models = append(models, DiscoveredModel{
			ID:     id,
			Object: "model",
		})
	}

	if len(models) > discoveryMaxModels {
		models = models[:discoveryMaxModels]
	}
	return models, nil
}

// OllamaModelInfo queries the context window size for a specific Ollama model
// via the /api/show endpoint. It returns the context_length (or num_ctx) value
// found in the model info or parameters.
func OllamaModelInfo(ctx context.Context, baseURL, model string) (int, error) {
	endpoint := stripV1Suffix(baseURL) + "/api/show"

	ctx, cancel := context.WithTimeout(ctx, discoveryTimeout)
	defer cancel()

	payload, err := json.Marshal(ollamaShowRequest{Name: model})
	if err != nil {
		return 0, fmt.Errorf("ollama model info: marshalling request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(string(payload)))
	if err != nil {
		return 0, fmt.Errorf("ollama model info: building request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, fmt.Errorf("ollama model info: request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, discoveryMaxBody))
		return 0, fmt.Errorf("ollama model info: unexpected status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var showResp ollamaShowResponse
	if err := json.NewDecoder(resp.Body).Decode(&showResp); err != nil {
		return 0, fmt.Errorf("ollama model info: parsing response: %w", err)
	}

	// Try model_info keys that commonly contain the context window size.
	for _, key := range []string{"context_length", "num_ctx"} {
		if v, ok := showResp.ModelInfo[key]; ok {
			if n, ok := toInt(v); ok && n > 0 {
				return n, nil
			}
		}
	}

	// Fallback: some Ollama versions nest context length under a
	// architecture-prefixed key like "llama.context_length".
	for key, v := range showResp.ModelInfo {
		if strings.HasSuffix(key, ".context_length") {
			if n, ok := toInt(v); ok && n > 0 {
				return n, nil
			}
		}
	}

	return 0, fmt.Errorf("ollama model info: context window not found for model %q", model)
}

// toInt attempts to convert a JSON-decoded numeric value to int.
// JSON numbers are decoded as float64 by encoding/json.
func toInt(v any) (int, bool) {
	switch n := v.(type) {
	case float64:
		return int(n), true
	case int:
		return n, true
	case int64:
		return int(n), true
	default:
		return 0, false
	}
}

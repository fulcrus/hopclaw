package toolruntime

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
)

// ---------------------------------------------------------------------------
// Search provider interface
// ---------------------------------------------------------------------------

// SearchProvider defines a web search backend that can be used independently
// of the built-in search tool configuration (ServicesConfig). Each provider
// checks for its own API key via environment variable.
type SearchProvider interface {
	// Search executes a web search and returns up to limit results.
	Search(ctx context.Context, query string, limit int) ([]SearchResult, error)

	// Name returns the provider identifier (e.g. "brave", "perplexity", "kimi").
	Name() string

	// RequiredEnvVar returns the environment variable name that must be set
	// for this provider to function (e.g. "BRAVE_SEARCH_API_KEY").
	RequiredEnvVar() string
}

// SearchResult represents a single web search result.
type SearchResult struct {
	Title   string `json:"title"`
	URL     string `json:"url"`
	Snippet string `json:"snippet"`
}

// ---------------------------------------------------------------------------
// Constants
// ---------------------------------------------------------------------------

const (
	braveSearchEndpoint     = "https://api.search.brave.com/res/v1/web/search"
	braveEnvVar             = "BRAVE_SEARCH_API_KEY"
	braveSubscriptionHeader = "X-Subscription-Token"
	braveAcceptHeader       = "Accept"

	perplexityEndpoint = "https://api.perplexity.ai/chat/completions"
	perplexityEnvVar   = "PERPLEXITY_API_KEY"

	kimiSearchEndpoint = "https://api.moonshot.cn/v1/search"
	kimiEnvVar         = "KIMI_API_KEY"

	defaultSearchLimit     = 5
	searchResponseMaxBytes = 1 << 20 // 1 MiB
)

// ---------------------------------------------------------------------------
// Brave Search provider
// ---------------------------------------------------------------------------

// BraveSearchProvider uses the Brave Search API for web search.
type BraveSearchProvider struct{}

// Name returns "brave".
func (p *BraveSearchProvider) Name() string { return "brave" }

// RequiredEnvVar returns "BRAVE_SEARCH_API_KEY".
func (p *BraveSearchProvider) RequiredEnvVar() string { return braveEnvVar }

// Search executes a web search via the Brave Search API.
func (p *BraveSearchProvider) Search(ctx context.Context, query string, limit int) ([]SearchResult, error) {
	apiKey := os.Getenv(braveEnvVar)
	if apiKey == "" {
		return nil, fmt.Errorf("brave: %s environment variable is not set", braveEnvVar)
	}
	if limit <= 0 {
		limit = defaultSearchLimit
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, braveSearchEndpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("brave: creating request: %w", err)
	}

	q := req.URL.Query()
	q.Set("q", query)
	q.Set("count", strconv.Itoa(limit))
	req.URL.RawQuery = q.Encode()

	req.Header.Set(braveSubscriptionHeader, apiKey)
	req.Header.Set(braveAcceptHeader, "application/json")

	resp, err := serviceHTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("brave: sending request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, searchResponseMaxBytes))
	if err != nil {
		return nil, fmt.Errorf("brave: reading response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("brave: api returned status %d: %s", resp.StatusCode, body)
	}

	var parsed braveSearchResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("brave: parsing response: %w", err)
	}

	results := make([]SearchResult, 0, len(parsed.Web.Results))
	for _, r := range parsed.Web.Results {
		results = append(results, SearchResult{
			Title:   r.Title,
			URL:     r.URL,
			Snippet: r.Description,
		})
	}
	return results, nil
}

// braveSearchResponse is the relevant portion of the Brave Search API response.
type braveSearchResponse struct {
	Web struct {
		Results []struct {
			Title       string `json:"title"`
			URL         string `json:"url"`
			Description string `json:"description"`
		} `json:"results"`
	} `json:"web"`
}

// ---------------------------------------------------------------------------
// Perplexity Search provider
// ---------------------------------------------------------------------------

// PerplexitySearchProvider uses the Perplexity API as a search proxy by
// sending a search query as a chat completion request.
type PerplexitySearchProvider struct{}

// Name returns "perplexity".
func (p *PerplexitySearchProvider) Name() string { return "perplexity" }

// RequiredEnvVar returns "PERPLEXITY_API_KEY".
func (p *PerplexitySearchProvider) RequiredEnvVar() string { return perplexityEnvVar }

// Search executes a search via the Perplexity chat completions API.
func (p *PerplexitySearchProvider) Search(ctx context.Context, query string, limit int) ([]SearchResult, error) {
	apiKey := os.Getenv(perplexityEnvVar)
	if apiKey == "" {
		return nil, fmt.Errorf("perplexity: %s environment variable is not set", perplexityEnvVar)
	}
	if limit <= 0 {
		limit = defaultSearchLimit
	}

	reqBody := perplexityRequest{
		Model: "sonar",
		Messages: []perplexityMessage{
			{Role: "user", Content: query},
		},
	}
	encoded, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("perplexity: encoding request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, perplexityEndpoint, bytes.NewReader(encoded))
	if err != nil {
		return nil, fmt.Errorf("perplexity: creating request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)

	resp, err := serviceHTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("perplexity: sending request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, searchResponseMaxBytes))
	if err != nil {
		return nil, fmt.Errorf("perplexity: reading response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("perplexity: api returned status %d: %s", resp.StatusCode, body)
	}

	var parsed perplexityResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("perplexity: parsing response: %w", err)
	}

	// Perplexity returns a single text response with optional citations.
	// We return the content as a single result with citations as additional results.
	results := make([]SearchResult, 0, limit)
	if len(parsed.Choices) > 0 {
		results = append(results, SearchResult{
			Title:   "Perplexity Answer",
			Snippet: parsed.Choices[0].Message.Content,
		})
	}
	for i, citation := range parsed.Citations {
		if i+1 >= limit {
			break
		}
		results = append(results, SearchResult{
			Title: fmt.Sprintf("Citation %d", i+1),
			URL:   citation,
		})
	}

	return results, nil
}

// perplexityRequest is the request body for the Perplexity chat completions API.
type perplexityRequest struct {
	Model    string              `json:"model"`
	Messages []perplexityMessage `json:"messages"`
}

// perplexityMessage represents a single message in the Perplexity API request.
type perplexityMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// perplexityResponse is the relevant portion of the Perplexity API response.
type perplexityResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
	Citations []string `json:"citations"`
}

// ---------------------------------------------------------------------------
// Kimi (Moonshot) Search provider
// ---------------------------------------------------------------------------

// KimiSearchProvider uses the Kimi/Moonshot API for web search.
type KimiSearchProvider struct{}

// Name returns "kimi".
func (p *KimiSearchProvider) Name() string { return "kimi" }

// RequiredEnvVar returns "KIMI_API_KEY".
func (p *KimiSearchProvider) RequiredEnvVar() string { return kimiEnvVar }

// Search executes a web search via the Kimi/Moonshot API.
func (p *KimiSearchProvider) Search(ctx context.Context, query string, limit int) ([]SearchResult, error) {
	apiKey := os.Getenv(kimiEnvVar)
	if apiKey == "" {
		return nil, fmt.Errorf("kimi: %s environment variable is not set", kimiEnvVar)
	}
	if limit <= 0 {
		limit = defaultSearchLimit
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, kimiSearchEndpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("kimi: creating request: %w", err)
	}

	q := req.URL.Query()
	q.Set("q", query)
	req.URL.RawQuery = q.Encode()

	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Accept", "application/json")

	resp, err := serviceHTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("kimi: sending request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, searchResponseMaxBytes))
	if err != nil {
		return nil, fmt.Errorf("kimi: reading response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("kimi: api returned status %d: %s", resp.StatusCode, body)
	}

	var parsed kimiSearchResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("kimi: parsing response: %w", err)
	}

	results := make([]SearchResult, 0, len(parsed.Results))
	for i, r := range parsed.Results {
		if i >= limit {
			break
		}
		results = append(results, SearchResult{
			Title:   r.Title,
			URL:     r.URL,
			Snippet: r.Snippet,
		})
	}
	return results, nil
}

// kimiSearchResponse is the relevant portion of the Kimi/Moonshot search API response.
type kimiSearchResponse struct {
	Results []struct {
		Title   string `json:"title"`
		URL     string `json:"url"`
		Snippet string `json:"snippet"`
	} `json:"results"`
}

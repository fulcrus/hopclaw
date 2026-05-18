package model

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/fulcrus/hopclaw/agent"
)

const (
	copilotTokenURL        = "https://api.github.com/copilot_internal/v2/token"
	copilotDefaultAPIBase  = "https://api.githubcopilot.com"
	copilotTokenSafeMargin = 60 * time.Second // refresh a minute before expiry
)

// CopilotConfig holds the configuration for a GitHub Copilot API client.
type CopilotConfig struct {
	GitHubToken  string // GITHUB_TOKEN, GH_TOKEN, or COPILOT_GITHUB_TOKEN
	DefaultModel string
	Timeout      time.Duration
	Headers      map[string]string
}

// CopilotClient implements agent.ModelClient by exchanging a GitHub token for
// a short-lived Copilot token, then delegating to an OpenAI-compatible client.
type CopilotClient struct {
	githubToken  string
	defaultModel string
	timeout      time.Duration

	mu          sync.Mutex
	cachedToken string
	tokenExpiry time.Time
	apiBaseURL  string
	inner       *OpenAICompatClient
	headers     map[string]string
}

// copilotTokenResponse is the JSON shape returned by the Copilot token endpoint.
type copilotTokenResponse struct {
	Token     string `json:"token"`
	ExpiresAt int64  `json:"expires_at"`
	Endpoints struct {
		API string `json:"api"`
	} `json:"endpoints"`
}

// NewCopilotClient creates a new CopilotClient from the given config.
func NewCopilotClient(cfg CopilotConfig) (*CopilotClient, error) {
	token := strings.TrimSpace(cfg.GitHubToken)
	if token == "" {
		return nil, fmt.Errorf("copilot: github token is required")
	}
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = 60 * time.Second
	}
	return &CopilotClient{
		githubToken:  token,
		defaultModel: strings.TrimSpace(cfg.DefaultModel),
		timeout:      timeout,
		headers:      cloneHeaders(cfg.Headers),
	}, nil
}

// Chat implements agent.ModelClient. It ensures the Copilot token is current,
// creates or updates the inner OpenAI-compatible client, and delegates the call.
func (c *CopilotClient) Chat(ctx context.Context, req agent.ChatRequest) (*agent.ModelResponse, error) {
	inner, err := c.ensureClient(ctx)
	if err != nil {
		return nil, err
	}
	return inner.Chat(ctx, req)
}

func (c *CopilotClient) ChatStream(ctx context.Context, req agent.ChatRequest, cb agent.StreamCallback) (*agent.ModelResponse, error) {
	inner, err := c.ensureClient(ctx)
	if err != nil {
		return nil, err
	}
	if sc, ok := any(inner).(agent.StreamingModelClient); ok {
		return sc.ChatStream(ctx, req, cb)
	}
	return streamModelResponseFallback(ctx, inner, req, cb)
}

// ensureClient returns an OpenAICompatClient with a valid Copilot token,
// refreshing the token and rebuilding the client when necessary.
func (c *CopilotClient) ensureClient(ctx context.Context) (*OpenAICompatClient, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.inner != nil && time.Now().Before(c.tokenExpiry) {
		return c.inner, nil
	}

	tok, err := c.exchangeToken(ctx)
	if err != nil {
		return nil, fmt.Errorf("copilot: token exchange: %w", err)
	}

	c.cachedToken = tok.Token
	c.tokenExpiry = time.Unix(tok.ExpiresAt, 0).Add(-copilotTokenSafeMargin)

	apiBase := strings.TrimSpace(tok.Endpoints.API)
	if apiBase == "" {
		apiBase = copilotDefaultAPIBase
	}
	c.apiBaseURL = strings.TrimRight(apiBase, "/")

	inner, err := NewOpenAICompatClient(OpenAICompatConfig{
		BaseURL:      c.apiBaseURL,
		APIKey:       c.cachedToken,
		DefaultModel: c.defaultModel,
		Timeout:      c.timeout,
		Headers:      c.headers,
	})
	if err != nil {
		return nil, fmt.Errorf("copilot: create inner client: %w", err)
	}
	c.inner = inner
	return c.inner, nil
}

// exchangeToken calls the GitHub Copilot token endpoint to exchange the
// long-lived GitHub token for a short-lived Copilot session token.
func (c *CopilotClient) exchangeToken(ctx context.Context) (*copilotTokenResponse, error) {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, copilotTokenURL, nil)
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Authorization", "token "+c.githubToken)
	httpReq.Header.Set("Accept", "application/json")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("token endpoint returned %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var tok copilotTokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tok); err != nil {
		return nil, fmt.Errorf("decode token response: %w", err)
	}
	if tok.Token == "" {
		return nil, fmt.Errorf("token endpoint returned empty token")
	}
	return &tok, nil
}

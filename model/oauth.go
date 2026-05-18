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

// ---------------------------------------------------------------------------
// OAuth constants
// ---------------------------------------------------------------------------

const (
	oauthTokenRefreshMargin = 60 * time.Second // refresh before actual expiry
	oauthTokenTimeout       = 30 * time.Second // HTTP timeout for token exchange
	oauthDefaultExpiry      = 3600             // seconds; used when server omits expires_in
)

// ---------------------------------------------------------------------------
// OAuthConfig
// ---------------------------------------------------------------------------

// OAuthConfig configures an OAuth-based provider.
type OAuthConfig struct {
	// TokenURL is the OAuth token endpoint.
	TokenURL string
	// ClientID and ClientSecret for OAuth client credentials flow.
	ClientID     string
	ClientSecret string
	// RefreshToken for OAuth refresh token flow (alternative to client credentials).
	RefreshToken string
	// Scopes requested during token exchange.
	Scopes []string
	// ProviderConfig is the underlying provider configuration.
	// BaseURL and API type determine how the inner client is built.
	ProviderConfig ProviderEntry
}

// ---------------------------------------------------------------------------
// OAuthClient
// ---------------------------------------------------------------------------

// OAuthClient wraps a model client with automatic OAuth token management.
// It exchanges credentials for short-lived access tokens and rebuilds the
// inner client when tokens expire.
type OAuthClient struct {
	config     OAuthConfig
	httpClient *http.Client

	mu          sync.Mutex // guards accessToken, tokenExpiry, and inner
	accessToken string
	tokenExpiry time.Time
	inner       agent.ModelClient
}

// oauthTokenResponse is the standard OAuth 2.0 token response.
type oauthTokenResponse struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"`
	RefreshToken string `json:"refresh_token,omitempty"`
	Scope        string `json:"scope,omitempty"`
}

// oauthErrorResponse is the standard OAuth 2.0 error response.
type oauthErrorResponse struct {
	Error            string `json:"error"`
	ErrorDescription string `json:"error_description,omitempty"`
}

// NewOAuthClient creates an OAuthClient that manages token lifecycle for
// providers using OAuth token exchange.
func NewOAuthClient(cfg OAuthConfig) (*OAuthClient, error) {
	if strings.TrimSpace(cfg.TokenURL) == "" {
		return nil, fmt.Errorf("oauth: token url is required")
	}
	hasClientCreds := strings.TrimSpace(cfg.ClientID) != "" && strings.TrimSpace(cfg.ClientSecret) != ""
	hasRefreshToken := strings.TrimSpace(cfg.RefreshToken) != ""
	if !hasClientCreds && !hasRefreshToken {
		return nil, fmt.Errorf("oauth: either client_id+client_secret or refresh_token is required")
	}
	return &OAuthClient{
		config: cfg,
		httpClient: &http.Client{
			Timeout: oauthTokenTimeout,
		},
	}, nil
}

// Chat implements agent.ModelClient by ensuring a valid token and delegating
// to the inner client.
func (c *OAuthClient) Chat(ctx context.Context, req agent.ChatRequest) (*agent.ModelResponse, error) {
	if err := c.ensureToken(ctx); err != nil {
		return nil, fmt.Errorf("oauth: token exchange failed: %w", err)
	}
	c.mu.Lock()
	inner := c.inner
	c.mu.Unlock()
	return inner.Chat(ctx, req)
}

// ChatStream implements agent.StreamingModelClient by ensuring a valid token
// and delegating to the inner client if it supports streaming; otherwise it
// falls back to Chat.
func (c *OAuthClient) ChatStream(ctx context.Context, req agent.ChatRequest, cb agent.StreamCallback) (*agent.ModelResponse, error) {
	if err := c.ensureToken(ctx); err != nil {
		return nil, fmt.Errorf("oauth: token exchange failed: %w", err)
	}
	c.mu.Lock()
	inner := c.inner
	c.mu.Unlock()
	if sc, ok := inner.(agent.StreamingModelClient); ok {
		return sc.ChatStream(ctx, req, cb)
	}
	return streamModelResponseFallback(ctx, inner, req, cb)
}

// ensureToken checks whether the current access token is still valid (with a
// safety margin) and, if not, exchanges credentials for a new one and rebuilds
// the inner client.
func (c *OAuthClient) ensureToken(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.accessToken != "" && time.Now().Add(oauthTokenRefreshMargin).Before(c.tokenExpiry) {
		return nil
	}

	tok, err := c.exchangeToken(ctx)
	if err != nil {
		return err
	}

	c.accessToken = tok.AccessToken
	expiry := tok.ExpiresIn
	if expiry <= 0 {
		expiry = oauthDefaultExpiry
	}
	c.tokenExpiry = time.Now().Add(time.Duration(expiry) * time.Second)

	// Update refresh token if the server rotated it.
	if tok.RefreshToken != "" {
		c.config.RefreshToken = tok.RefreshToken
	}

	inner, err := c.buildInnerClient(tok.AccessToken)
	if err != nil {
		return fmt.Errorf("oauth: build inner client: %w", err)
	}
	c.inner = inner
	return nil
}

// exchangeToken performs the OAuth 2.0 token exchange. It uses the refresh_token
// grant when a refresh token is configured, otherwise client_credentials.
func (c *OAuthClient) exchangeToken(ctx context.Context) (*oauthTokenResponse, error) {
	params := make([]string, 0, 8)

	if strings.TrimSpace(c.config.RefreshToken) != "" {
		params = append(params,
			"grant_type=refresh_token",
			"refresh_token="+c.config.RefreshToken,
		)
		if strings.TrimSpace(c.config.ClientID) != "" {
			params = append(params, "client_id="+c.config.ClientID)
		}
		if strings.TrimSpace(c.config.ClientSecret) != "" {
			params = append(params, "client_secret="+c.config.ClientSecret)
		}
	} else {
		params = append(params,
			"grant_type=client_credentials",
			"client_id="+c.config.ClientID,
			"client_secret="+c.config.ClientSecret,
		)
	}

	if len(c.config.Scopes) > 0 {
		params = append(params, "scope="+strings.Join(c.config.Scopes, " "))
	}

	body := strings.Join(params, "&")
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.config.TokenURL, strings.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("oauth: create token request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	httpReq.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("oauth: token request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("oauth: read token response: %w", err)
	}

	if resp.StatusCode >= 400 {
		var oauthErr oauthErrorResponse
		if jsonErr := json.Unmarshal(respBody, &oauthErr); jsonErr == nil && oauthErr.Error != "" {
			desc := oauthErr.Error
			if oauthErr.ErrorDescription != "" {
				desc += ": " + oauthErr.ErrorDescription
			}
			return nil, fmt.Errorf("oauth: token exchange failed (%d): %s", resp.StatusCode, desc)
		}
		return nil, fmt.Errorf("oauth: token exchange failed (%d): %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
	}

	var tok oauthTokenResponse
	if err := json.Unmarshal(respBody, &tok); err != nil {
		return nil, fmt.Errorf("oauth: decode token response: %w", err)
	}
	if strings.TrimSpace(tok.AccessToken) == "" {
		return nil, fmt.Errorf("oauth: token response missing access_token")
	}
	return &tok, nil
}

// buildInnerClient creates a new model client using the provider config with
// the given OAuth token as the API key.
func (c *OAuthClient) buildInnerClient(token string) (agent.ModelClient, error) {
	entry := c.config.ProviderConfig
	entry.APIKey = token
	entry.APIKeys = nil
	return newClientForAPI(entry)
}

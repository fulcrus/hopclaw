// Package client provides an HTTP client for communicating with the
// browserd process using the browser.v1 protocol.
package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/fulcrus/hopclaw/browserapi/types"
)

// Client talks to a running browserd instance over HTTP.
type Client struct {
	baseURL    string
	authToken  string
	resolver   EndpointResolver
	httpClient *http.Client
}

type Endpoint struct {
	BaseURL   string
	AuthToken string
}

type EndpointResolver func(ctx context.Context) (Endpoint, error)

type Config struct {
	BaseURL          string
	AuthToken        string
	Timeout          time.Duration
	HTTPClient       *http.Client
	EndpointResolver EndpointResolver
}

type Profile struct {
	Name      string `json:"name"`
	Color     string `json:"color,omitempty"`
	Driver    string `json:"driver"`
	CDPURL    string `json:"cdp_url,omitempty"`
	CreatedAt string `json:"created_at,omitempty"`
}

type CreateProfileRequest struct {
	Name   string `json:"name"`
	Color  string `json:"color,omitempty"`
	CDPURL string `json:"cdp_url,omitempty"`
}

// New creates a browser client pointing at the given browserd base URL.
func New(baseURL string) *Client {
	return NewWithConfig(Config{BaseURL: baseURL})
}

// NewWithConfig creates a browser client with explicit options.
func NewWithConfig(cfg Config) *Client {
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	httpClient := cfg.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: timeout}
	}
	return &Client{
		baseURL:    strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/"),
		authToken:  strings.TrimSpace(cfg.AuthToken),
		resolver:   cfg.EndpointResolver,
		httpClient: httpClient,
	}
}

// Do sends a browser.v1 request and returns the response.
func (c *Client) Do(ctx context.Context, req types.Request) (*types.Response, error) {
	return c.do(ctx, req, 0)
}

// DoWithTimeout sends a browser.v1 request with a one-off HTTP timeout
// override. This is useful for heavier actions such as full-page captures.
func (c *Client) DoWithTimeout(ctx context.Context, req types.Request, timeout time.Duration) (*types.Response, error) {
	return c.do(ctx, req, timeout)
}

func (c *Client) do(ctx context.Context, req types.Request, timeout time.Duration) (*types.Response, error) {
	endpoint, err := c.resolveEndpoint(ctx)
	if err != nil {
		return nil, err
	}
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint.BaseURL+"/browser/v1", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create http request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	applyAuth(httpReq, endpoint.AuthToken)

	httpClient := c.httpClient
	if timeout > 0 && httpClient != nil && httpClient.Timeout != timeout {
		cloned := *httpClient
		cloned.Timeout = timeout
		httpClient = &cloned
	}

	httpResp, err := httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("send request: %w", err)
	}
	defer httpResp.Body.Close()

	var resp types.Response
	if err := json.NewDecoder(httpResp.Body).Decode(&resp); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	if httpResp.StatusCode >= http.StatusBadRequest && resp.Error == "" {
		resp.Error = httpResp.Status
	}
	return &resp, nil
}

// Health checks that browserd is reachable.
func (c *Client) Health(ctx context.Context) error {
	endpoint, err := c.resolveEndpoint(ctx)
	if err != nil {
		return err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.BaseURL+"/healthz", nil)
	if err != nil {
		return fmt.Errorf("create health request: %w", err)
	}
	applyAuth(httpReq, endpoint.AuthToken)

	httpResp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return fmt.Errorf("ping browser host: %w", err)
	}
	defer httpResp.Body.Close()
	if httpResp.StatusCode != http.StatusOK {
		return fmt.Errorf("browser host health status %s", httpResp.Status)
	}
	return nil
}

// ListProfiles returns the registered persistent browser profiles.
func (c *Client) ListProfiles(ctx context.Context) ([]Profile, error) {
	endpoint, err := c.resolveEndpoint(ctx)
	if err != nil {
		return nil, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.BaseURL+"/browser/v1/profiles", nil)
	if err != nil {
		return nil, fmt.Errorf("create profile list request: %w", err)
	}
	applyAuth(httpReq, endpoint.AuthToken)

	httpResp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("list profiles: %w", err)
	}
	defer httpResp.Body.Close()
	if httpResp.StatusCode >= http.StatusBadRequest {
		return nil, decodeHTTPError(httpResp)
	}

	var profiles []Profile
	if err := json.NewDecoder(httpResp.Body).Decode(&profiles); err != nil {
		return nil, fmt.Errorf("decode profiles response: %w", err)
	}
	return profiles, nil
}

// CreateProfile creates a persistent browser profile.
func (c *Client) CreateProfile(ctx context.Context, req CreateProfileRequest) (*Profile, error) {
	endpoint, err := c.resolveEndpoint(ctx)
	if err != nil {
		return nil, err
	}
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal profile request: %w", err)
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint.BaseURL+"/browser/v1/profiles", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create profile request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	applyAuth(httpReq, endpoint.AuthToken)

	httpResp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("create profile: %w", err)
	}
	defer httpResp.Body.Close()
	if httpResp.StatusCode >= http.StatusBadRequest {
		return nil, decodeHTTPError(httpResp)
	}

	var profile Profile
	if err := json.NewDecoder(httpResp.Body).Decode(&profile); err != nil {
		return nil, fmt.Errorf("decode create profile response: %w", err)
	}
	return &profile, nil
}

// DeleteProfile deletes a persistent browser profile.
func (c *Client) DeleteProfile(ctx context.Context, name string) error {
	endpoint, err := c.resolveEndpoint(ctx)
	if err != nil {
		return err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodDelete, endpoint.BaseURL+"/browser/v1/profiles/"+url.PathEscape(strings.TrimSpace(name)), nil)
	if err != nil {
		return fmt.Errorf("create delete profile request: %w", err)
	}
	applyAuth(httpReq, endpoint.AuthToken)

	httpResp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return fmt.Errorf("delete profile: %w", err)
	}
	defer httpResp.Body.Close()
	if httpResp.StatusCode >= http.StatusBadRequest {
		return decodeHTTPError(httpResp)
	}
	return nil
}

func (c *Client) resolveEndpoint(ctx context.Context) (Endpoint, error) {
	if c.resolver != nil {
		endpoint, err := c.resolver(ctx)
		if err != nil {
			return Endpoint{}, err
		}
		endpoint.BaseURL = strings.TrimRight(strings.TrimSpace(endpoint.BaseURL), "/")
		endpoint.AuthToken = strings.TrimSpace(endpoint.AuthToken)
		if endpoint.BaseURL == "" {
			return Endpoint{}, fmt.Errorf("browser host endpoint is not configured")
		}
		return endpoint, nil
	}
	if c.baseURL == "" {
		return Endpoint{}, fmt.Errorf("browser host endpoint is not configured")
	}
	return Endpoint{
		BaseURL:   c.baseURL,
		AuthToken: c.authToken,
	}, nil
}

func applyAuth(req *http.Request, token string) {
	if token == "" {
		return
	}
	req.Header.Set("Authorization", "Bearer "+token)
}

func decodeHTTPError(resp *http.Response) error {
	var payload struct {
		Error string `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err == nil && strings.TrimSpace(payload.Error) != "" {
		return fmt.Errorf("%s", payload.Error)
	}
	return fmt.Errorf("browser host returned status %s", resp.Status)
}

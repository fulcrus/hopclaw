// Package client provides an HTTP client for communicating with the
// desktop daemon using the desktop.v1 protocol.
package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	desktoptypes "github.com/fulcrus/hopclaw/desktopapi/types"
)

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

func New(baseURL string) *Client {
	return NewWithConfig(Config{BaseURL: baseURL})
}

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

func (c *Client) Do(ctx context.Context, req desktoptypes.Request) (*desktoptypes.Response, error) {
	endpoint, err := c.resolveEndpoint(ctx)
	if err != nil {
		return nil, err
	}
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint.BaseURL+"/desktop/v1", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create http request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	applyAuth(httpReq, endpoint.AuthToken)

	httpResp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("send request: %w", err)
	}
	defer httpResp.Body.Close()

	var resp desktoptypes.Response
	if err := json.NewDecoder(httpResp.Body).Decode(&resp); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	if httpResp.StatusCode >= http.StatusBadRequest && resp.Error == "" {
		resp.Error = httpResp.Status
	}
	return &resp, nil
}

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
		return fmt.Errorf("ping desktop host: %w", err)
	}
	defer httpResp.Body.Close()
	if httpResp.StatusCode != http.StatusOK {
		return fmt.Errorf("desktop host health status %s", httpResp.Status)
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
			return Endpoint{}, fmt.Errorf("desktop host endpoint is not configured")
		}
		return endpoint, nil
	}
	if c.baseURL == "" {
		return Endpoint{}, fmt.Errorf("desktop host endpoint is not configured")
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

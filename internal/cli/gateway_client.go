package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/fulcrus/hopclaw/config"
)

// ---------------------------------------------------------------------------
// Gateway client constants
// ---------------------------------------------------------------------------

const (
	gatewayClientTimeout = 15 * time.Second
	authHeaderName       = "X-HopClaw-Token"
	operatorStatusPath   = "/operator/status"
	publicHealthPath     = "/healthz"
)

// ---------------------------------------------------------------------------
// GatewayClient
// ---------------------------------------------------------------------------

// GatewayClient is a reusable HTTP client for CLI commands that talk to a
// running HopClaw gateway.
type GatewayClient struct {
	BaseURL   string
	AuthToken string
	HTTP      *http.Client
}

type gatewayStatusError struct {
	StatusCode int
	Message    string
}

func (e *gatewayStatusError) Error() string {
	if e == nil {
		return "gateway error"
	}
	if strings.TrimSpace(e.Message) == "" {
		return fmt.Sprintf("gateway error (HTTP %d)", e.StatusCode)
	}
	return fmt.Sprintf("gateway error (HTTP %d): %s", e.StatusCode, e.Message)
}

// NewGatewayClient loads the gateway address and auth token from config, then
// returns a configured client.
func NewGatewayClient() (*GatewayClient, error) {
	if flagLocal && strings.TrimSpace(flagRemote) != "" {
		return nil, fmt.Errorf("--local and --remote cannot be used together")
	}

	client, err := newConfiguredGatewayClient()
	if err != nil {
		return nil, err
	}
	if flagLocal {
		return client, nil
	}

	targetName := strings.TrimSpace(flagRemote)
	if targetName == "" {
		return client, nil
	}

	target, err := resolveNamedInteractiveTarget(context.Background(), targetName, interactiveTarget{})
	if err != nil {
		return nil, err
	}
	if isPrivateLocalInteractiveTarget(target) {
		return client, nil
	}

	authToken, err := resolveInteractiveTargetAuthToken(target)
	if err != nil {
		return nil, err
	}
	client, _, err = newGatewayClientWithOptions(target.BaseURL, authToken, target.Insecure)
	if err != nil {
		return nil, err
	}
	return client, nil
}

func newConfiguredGatewayClient() (*GatewayClient, error) {
	addr := resolveGatewayAddr()
	baseURL := "http://" + addr

	var authToken string
	configPath := resolveConfigPath()
	if configPath != "" {
		cfg, err := config.Load(configPath)
		if err == nil {
			authToken = resolveConfigOperatorToken(cfg)
		}
	}

	return &GatewayClient{
		BaseURL:   baseURL,
		AuthToken: authToken,
		HTTP:      &http.Client{Timeout: gatewayClientTimeout},
	}, nil
}

// Get performs an HTTP GET and decodes the response JSON into target.
func (c *GatewayClient) Get(ctx context.Context, path string, target any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.url(path), nil)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	return c.doJSON(req, target)
}

// Post performs an HTTP POST with a JSON body and decodes the response into target.
func (c *GatewayClient) Post(ctx context.Context, path string, body any, target any) error {
	var bodyReader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("marshal request body: %w", err)
		}
		bodyReader = bytes.NewReader(data)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url(path), bodyReader)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return c.doJSON(req, target)
}

// Delete performs an HTTP DELETE and decodes the response JSON into target.
func (c *GatewayClient) Delete(ctx context.Context, path string, target any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, c.url(path), nil)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	return c.doJSON(req, target)
}

// Put performs an HTTP PUT with a JSON body and decodes the response into target.
func (c *GatewayClient) Put(ctx context.Context, path string, body any, target any) error {
	var bodyReader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("marshal request body: %w", err)
		}
		bodyReader = bytes.NewReader(data)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPut, c.url(path), bodyReader)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return c.doJSON(req, target)
}

// Patch performs an HTTP PATCH with a JSON body and decodes the response into target.
func (c *GatewayClient) Patch(ctx context.Context, path string, body any, target any) error {
	var bodyReader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("marshal request body: %w", err)
		}
		bodyReader = bytes.NewReader(data)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPatch, c.url(path), bodyReader)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return c.doJSON(req, target)
}

// GetRaw performs an HTTP GET and returns the raw response body bytes. It is
// intended for endpoints that return non-JSON content (e.g. binary images).
func (c *GatewayClient) GetRaw(ctx context.Context, path string) ([]byte, error) {
	body, statusCode, err := c.GetRawWithStatus(ctx, path)
	if err != nil {
		return nil, err
	}
	if statusCode >= 400 {
		return nil, gatewayHTTPError(statusCode, body)
	}
	return body, nil
}

// GetRawWithStatus performs an HTTP GET and returns the raw response body and
// HTTP status code. It is intended for endpoints where callers need to inspect
// non-success statuses directly while still reusing gateway auth handling.
func (c *GatewayClient) GetRawWithStatus(ctx context.Context, path string) ([]byte, int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.url(path), nil)
	if err != nil {
		return nil, 0, fmt.Errorf("build request: %w", err)
	}
	return c.doRaw(req)
}

// ---------------------------------------------------------------------------
// Internal helpers
// ---------------------------------------------------------------------------

func (c *GatewayClient) url(path string) string {
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return c.BaseURL + path
}

func (c *GatewayClient) doJSON(req *http.Request, target any) error {
	body, statusCode, err := c.doRaw(req)
	if err != nil {
		return err
	}

	if statusCode >= 400 {
		return gatewayHTTPError(statusCode, body)
	}

	if target != nil {
		if err := decodeGatewayJSON(body, target); err != nil {
			return err
		}
	}
	return nil
}

func (c *GatewayClient) doRaw(req *http.Request) ([]byte, int, error) {
	if c.AuthToken != "" {
		req.Header.Set(authHeaderName, c.AuthToken)
	}

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("gateway request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, 0, fmt.Errorf("read response: %w", err)
	}

	return body, resp.StatusCode, nil
}

func gatewayHTTPError(statusCode int, body []byte) error {
	var errResp struct {
		Error string `json:"error"`
	}
	if json.Unmarshal(body, &errResp) == nil && errResp.Error != "" {
		return &gatewayStatusError{StatusCode: statusCode, Message: strings.TrimSpace(errResp.Error)}
	}

	message := strings.TrimSpace(string(body))
	if message == "" {
		return &gatewayStatusError{StatusCode: statusCode}
	}
	return &gatewayStatusError{StatusCode: statusCode, Message: message}
}

func gatewayErrorStatus(err error) int {
	var statusErr *gatewayStatusError
	if errors.As(err, &statusErr) {
		return statusErr.StatusCode
	}
	return 0
}

func decodeGatewayJSON(body []byte, target any) error {
	if err := json.Unmarshal(body, target); err != nil {
		preview := strings.TrimSpace(string(body))
		if looksLikeHTML(preview) {
			title := extractHTMLTitle(preview)
			if title != "" {
				return fmt.Errorf("decode response: gateway returned HTML instead of JSON (%s)", title)
			}
			return fmt.Errorf("decode response: gateway returned HTML instead of JSON")
		}
		if preview != "" {
			if len(preview) > 160 {
				preview = preview[:160] + "..."
			}
			return fmt.Errorf("decode response: %w (body: %s)", err, preview)
		}
		return fmt.Errorf("decode response: %w", err)
	}
	return nil
}

func looksLikeHTML(body string) bool {
	lower := strings.ToLower(strings.TrimSpace(body))
	return strings.HasPrefix(lower, "<!doctype html") || strings.HasPrefix(lower, "<html")
}

func extractHTMLTitle(body string) string {
	lower := strings.ToLower(body)
	start := strings.Index(lower, "<title>")
	end := strings.Index(lower, "</title>")
	if start < 0 || end <= start {
		return ""
	}
	title := strings.TrimSpace(body[start+len("<title>") : end])
	return html.UnescapeString(title)
}

package webhookclient

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const (
	DefaultTimeout   = 15 * time.Second
	SignatureHeader  = "X-HopClaw-Signature"
	TimestampHeader  = "X-HopClaw-Timestamp"
	maxResponseBytes = 1 << 20
)

type Config struct {
	Headers map[string]string
	Secret  string
	Timeout time.Duration
}

type Client struct {
	headers map[string]string
	secret  string
	timeout time.Duration
	client  *http.Client
	now     func() time.Time
}

func New(cfg Config) *Client {
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	headers := make(map[string]string, len(cfg.Headers))
	for key, value := range cfg.Headers {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		headers[key] = strings.TrimSpace(value)
	}
	return &Client{
		headers: headers,
		secret:  strings.TrimSpace(cfg.Secret),
		timeout: timeout,
		client:  &http.Client{},
		now:     time.Now,
	}
}

func (c *Client) PostJSON(ctx context.Context, endpoint string, payload any, target any, headers map[string]string) (bool, error) {
	respBody, err := c.PostJSONRaw(ctx, endpoint, payload, headers)
	if err != nil {
		return false, err
	}
	if target == nil || len(bytes.TrimSpace(respBody)) == 0 {
		return false, nil
	}
	if err := json.Unmarshal(respBody, target); err != nil {
		return false, err
	}
	return true, nil
}

func (c *Client) PostJSONRaw(ctx context.Context, endpoint string, payload any, headers map[string]string) ([]byte, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal webhook payload: %w", err)
	}
	reqCtx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	httpReq, err := http.NewRequestWithContext(reqCtx, http.MethodPost, strings.TrimSpace(endpoint), bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create webhook request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	for key, value := range c.headers {
		httpReq.Header.Set(key, value)
	}
	for key, value := range headers {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		httpReq.Header.Set(key, strings.TrimSpace(value))
	}
	if c.secret != "" {
		timestamp := strconv.FormatInt(c.now().UTC().Unix(), 10)
		httpReq.Header.Set(TimestampHeader, timestamp)
		httpReq.Header.Set(SignatureHeader, "sha256="+ComputeHMAC(c.secret, timestamp, body))
	}

	resp, err := c.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("webhook request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return nil, fmt.Errorf("read webhook response: %w", err)
	}
	if resp.StatusCode >= http.StatusBadRequest {
		return nil, fmt.Errorf("webhook status %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
	}
	return respBody, nil
}

func ComputeHMAC(secret, timestamp string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(timestamp))
	mac.Write([]byte("."))
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}

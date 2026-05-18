package authz

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
	neturl "net/url"
	"strconv"
	"strings"
	"time"
)

type WebhookConfig struct {
	URL     string
	Timeout time.Duration
	Headers map[string]string
	Secret  string
	Client  *http.Client
}

type WebhookDecider struct {
	url     string
	timeout time.Duration
	headers map[string]string
	secret  string
	client  *http.Client
}

func NewWebhookDecider(cfg WebhookConfig) (*WebhookDecider, error) {
	target := strings.TrimSpace(cfg.URL)
	if target == "" {
		return nil, fmt.Errorf("webhook url is required")
	}
	parsed, err := neturl.Parse(target)
	if err != nil || parsed == nil || parsed.Scheme == "" || parsed.Host == "" {
		return nil, fmt.Errorf("webhook url must be an absolute URL")
	}
	switch strings.ToLower(parsed.Scheme) {
	case "http", "https":
	default:
		return nil, fmt.Errorf("webhook url must use http or https")
	}
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	client := cfg.Client
	if client == nil {
		client = &http.Client{Timeout: timeout}
	}
	return &WebhookDecider{
		url:     target,
		timeout: timeout,
		headers: cloneStringMap(cfg.Headers),
		secret:  strings.TrimSpace(cfg.Secret),
		client:  client,
	}, nil
}

func (d *WebhookDecider) Decide(ctx context.Context, req AuthorizationRequest) (AuthorizationDecision, error) {
	if d == nil {
		return AuthorizationDecision{}, fmt.Errorf("webhook decider is nil")
	}
	payload, err := json.Marshal(req)
	if err != nil {
		return AuthorizationDecision{}, fmt.Errorf("marshal authorization request: %w", err)
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, d.url, bytes.NewReader(payload))
	if err != nil {
		return AuthorizationDecision{}, fmt.Errorf("build webhook request: %w", err)
	}
	httpReq.Header.Set("Accept", "application/json")
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("User-Agent", "HopClaw-AuthZ-Webhook/1.0")
	for key, value := range d.headers {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		httpReq.Header.Set(key, value)
	}
	if d.secret != "" {
		timestamp := strconv.FormatInt(time.Now().UTC().Unix(), 10)
		httpReq.Header.Set("X-HopClaw-Timestamp", timestamp)
		httpReq.Header.Set("X-HopClaw-Signature", "sha256="+computeWebhookHMAC(d.secret, timestamp, payload))
	}
	resp, err := d.client.Do(httpReq)
	if err != nil {
		return AuthorizationDecision{}, fmt.Errorf("call authz webhook: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return AuthorizationDecision{}, fmt.Errorf("read authz webhook response: %w", err)
	}

	decision, decodeErr := decodeWebhookDecision(body)
	switch {
	case resp.StatusCode >= 200 && resp.StatusCode < 300:
		if decodeErr != nil {
			return AuthorizationDecision{}, fmt.Errorf("decode authz webhook response: %w", decodeErr)
		}
		if decision.Source == "" {
			decision.Source = "webhook"
		}
		return decision, nil
	case resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusUnauthorized:
		if decodeErr == nil {
			if decision.Source == "" {
				decision.Source = "webhook"
			}
			return decision, nil
		}
		return AuthorizationDecision{
			Allowed: false,
			Reason:  strings.TrimSpace(http.StatusText(resp.StatusCode)),
			Source:  "webhook",
		}, nil
	default:
		if decodeErr == nil && !decision.Allowed {
			if decision.Source == "" {
				decision.Source = "webhook"
			}
			return decision, nil
		}
		return AuthorizationDecision{}, fmt.Errorf("authz webhook returned %s", resp.Status)
	}
}

func (d *WebhookDecider) DescribeAuthorization() Summary {
	metadata := map[string]string{
		"target":     webhookTargetHost(d.url),
		"timeout":    d.timeout.String(),
		"has_secret": boolString(d.secret != ""),
	}
	if len(d.headers) > 0 {
		metadata["header_count"] = strconv.Itoa(len(d.headers))
	}
	return Summary{
		Kind:          "webhook",
		Name:          "WebhookDecider",
		Mode:          "tob",
		DefaultEffect: "delegate",
		Resources:     ResourceNames(AllResources()),
		Actions:       ActionNames(AllActions()),
		Notes: []string{
			"Authorization requests are delegated to an external HTTP webhook.",
		},
		Metadata: metadata,
	}
}

func decodeWebhookDecision(body []byte) (AuthorizationDecision, error) {
	var envelope struct {
		Decision *AuthorizationDecision `json:"decision"`
	}
	if err := json.Unmarshal(body, &envelope); err == nil && envelope.Decision != nil {
		return *envelope.Decision, nil
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil {
		return AuthorizationDecision{}, fmt.Errorf("invalid json response")
	}
	if _, ok := raw["allowed"]; !ok {
		return AuthorizationDecision{}, fmt.Errorf("response must include allowed or decision.allowed")
	}
	var decision AuthorizationDecision
	if err := json.Unmarshal(body, &decision); err != nil {
		return AuthorizationDecision{}, err
	}
	return decision, nil
}

func computeWebhookHMAC(secret, timestamp string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(timestamp))
	mac.Write([]byte("."))
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}

func webhookTargetHost(raw string) string {
	parsed, err := neturl.Parse(strings.TrimSpace(raw))
	if err != nil || parsed == nil {
		return ""
	}
	return parsed.Host
}

func cloneStringMap(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

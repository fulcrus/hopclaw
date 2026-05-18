package audit

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

	"github.com/fulcrus/hopclaw/eventbus"
	"github.com/fulcrus/hopclaw/logging"
)

var webhookLog = logging.WithSubsystem("audit.webhook")

type MultiSink struct {
	Sinks []eventbus.Sink
}

func (s MultiSink) Handle(ctx context.Context, event eventbus.Event) error {
	for _, sink := range s.Sinks {
		if sink == nil {
			continue
		}
		if err := sink.Handle(ctx, event); err != nil {
			return err
		}
	}
	return nil
}

type WebhookRecorderConfig struct {
	Name    string
	URL     string
	Timeout time.Duration
	Headers map[string]string
	Secret  string
	Client  *http.Client
}

type WebhookRecorder struct {
	name    string
	url     string
	timeout time.Duration
	headers map[string]string
	secret  string
	client  *http.Client
}

func NewWebhookRecorder(cfg WebhookRecorderConfig) (*WebhookRecorder, error) {
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
		timeout = 15 * time.Second
	}
	client := cfg.Client
	if client == nil {
		client = &http.Client{Timeout: timeout}
	}
	return &WebhookRecorder{
		name:    strings.TrimSpace(cfg.Name),
		url:     target,
		timeout: timeout,
		headers: cloneStringMap(cfg.Headers),
		secret:  strings.TrimSpace(cfg.Secret),
		client:  client,
	}, nil
}

func (r *WebhookRecorder) Handle(ctx context.Context, event eventbus.Event) error {
	if err := r.Deliver(ctx, event); err != nil {
		webhookLog.WarnContext(ctx, "audit webhook delivery failed", "name", r.name, "target", webhookTargetHost(r.url), "error", err)
	}
	return nil
}

func (r *WebhookRecorder) Name() string {
	if r == nil {
		return ""
	}
	return strings.TrimSpace(r.name)
}

func (r *WebhookRecorder) Deliver(ctx context.Context, event eventbus.Event) error {
	if r == nil {
		return nil
	}
	payload, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("marshal audit event: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, r.url, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("build audit webhook request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "HopClaw-Audit-Webhook/1.0")
	for key, value := range r.headers {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		req.Header.Set(key, value)
	}
	if r.secret != "" {
		timestamp := strconv.FormatInt(time.Now().UTC().Unix(), 10)
		req.Header.Set("X-HopClaw-Timestamp", timestamp)
		req.Header.Set("X-HopClaw-Signature", "sha256="+computeWebhookRecorderHMAC(r.secret, timestamp, payload))
	}
	resp, err := r.client.Do(req)
	if err != nil {
		return fmt.Errorf("call audit webhook: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
	return fmt.Errorf("audit webhook returned %s: %s", resp.Status, strings.TrimSpace(string(body)))
}

func computeWebhookRecorderHMAC(secret, timestamp string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(timestamp))
	mac.Write([]byte("."))
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
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

func webhookTargetHost(raw string) string {
	parsed, err := neturl.Parse(strings.TrimSpace(raw))
	if err != nil || parsed == nil {
		return ""
	}
	return parsed.Host
}

package audit

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	neturl "net/url"
	"strings"
	"time"

	"github.com/fulcrus/hopclaw/eventbus"
)

type SplunkHECRecorderConfig struct {
	Name       string
	URL        string
	Token      string
	Timeout    time.Duration
	Headers    map[string]string
	Source     string
	SourceType string
	Index      string
	Host       string
	Client     *http.Client
}

type SplunkHECRecorder struct {
	name       string
	url        string
	token      string
	timeout    time.Duration
	headers    map[string]string
	source     string
	sourceType string
	index      string
	host       string
	client     *http.Client
}

type splunkHECPayload struct {
	Time       float64        `json:"time,omitempty"`
	Host       string         `json:"host,omitempty"`
	Source     string         `json:"source,omitempty"`
	SourceType string         `json:"sourcetype,omitempty"`
	Index      string         `json:"index,omitempty"`
	Event      eventbus.Event `json:"event"`
	Fields     map[string]any `json:"fields,omitempty"`
}

func NewSplunkHECRecorder(cfg SplunkHECRecorderConfig) (*SplunkHECRecorder, error) {
	target := strings.TrimSpace(cfg.URL)
	if target == "" {
		return nil, fmt.Errorf("splunk hec url is required")
	}
	parsed, err := neturl.Parse(target)
	if err != nil || parsed == nil || parsed.Scheme == "" || parsed.Host == "" {
		return nil, fmt.Errorf("splunk hec url must be an absolute URL")
	}
	switch strings.ToLower(parsed.Scheme) {
	case "http", "https":
	default:
		return nil, fmt.Errorf("splunk hec url must use http or https")
	}
	token := strings.TrimSpace(cfg.Token)
	if token == "" {
		return nil, fmt.Errorf("splunk hec token is required")
	}
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = 15 * time.Second
	}
	client := cfg.Client
	if client == nil {
		client = &http.Client{Timeout: timeout}
	}
	return &SplunkHECRecorder{
		name:       strings.TrimSpace(cfg.Name),
		url:        target,
		token:      token,
		timeout:    timeout,
		headers:    cloneStringMap(cfg.Headers),
		source:     strings.TrimSpace(cfg.Source),
		sourceType: strings.TrimSpace(cfg.SourceType),
		index:      strings.TrimSpace(cfg.Index),
		host:       strings.TrimSpace(cfg.Host),
		client:     client,
	}, nil
}

func (r *SplunkHECRecorder) Name() string {
	if r == nil {
		return ""
	}
	return strings.TrimSpace(r.name)
}

func (r *SplunkHECRecorder) Deliver(ctx context.Context, event eventbus.Event) error {
	if r == nil {
		return nil
	}
	if event.Time.IsZero() {
		event.Time = time.Now().UTC()
	}
	payload, err := json.Marshal(splunkHECPayload{
		Time:       float64(event.Time.UnixNano()) / float64(time.Second),
		Host:       r.host,
		Source:     r.source,
		SourceType: r.sourceType,
		Index:      r.index,
		Event:      cloneEvent(event),
		Fields: map[string]any{
			"hopclaw_event_id":   strings.TrimSpace(event.ID),
			"hopclaw_event_type": strings.TrimSpace(string(event.Type)),
			"run_id":             strings.TrimSpace(event.RunID),
			"session_id":         strings.TrimSpace(event.SessionID),
		},
	})
	if err != nil {
		return fmt.Errorf("marshal splunk hec audit event: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, r.url, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("build splunk hec audit request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "HopClaw-Audit-SplunkHEC/1.0")
	for key, value := range r.headers {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		req.Header.Set(key, value)
	}
	if strings.TrimSpace(req.Header.Get("Authorization")) == "" {
		req.Header.Set("Authorization", "Splunk "+r.token)
	}
	req.Header.Set("X-HopClaw-Event-ID", strings.TrimSpace(event.ID))
	req.Header.Set("X-HopClaw-Event-Type", strings.TrimSpace(string(event.Type)))
	resp, err := r.client.Do(req)
	if err != nil {
		return fmt.Errorf("call splunk hec audit sink: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
	return fmt.Errorf("splunk hec audit sink returned %s: %s", resp.Status, strings.TrimSpace(string(body)))
}

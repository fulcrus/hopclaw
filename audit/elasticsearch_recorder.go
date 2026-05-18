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

type ElasticsearchRecorderConfig struct {
	Name    string
	URL     string
	Index   string
	Timeout time.Duration
	Headers map[string]string
	APIKey  string
	Client  *http.Client
}

type ElasticsearchRecorder struct {
	name    string
	baseURL string
	index   string
	timeout time.Duration
	headers map[string]string
	apiKey  string
	client  *http.Client
}

func NewElasticsearchRecorder(cfg ElasticsearchRecorderConfig) (*ElasticsearchRecorder, error) {
	target := strings.TrimSpace(cfg.URL)
	if target == "" {
		return nil, fmt.Errorf("elasticsearch url is required")
	}
	parsed, err := neturl.Parse(target)
	if err != nil || parsed == nil || parsed.Scheme == "" || parsed.Host == "" {
		return nil, fmt.Errorf("elasticsearch url must be an absolute URL")
	}
	switch strings.ToLower(parsed.Scheme) {
	case "http", "https":
	default:
		return nil, fmt.Errorf("elasticsearch url must use http or https")
	}
	index := strings.TrimSpace(cfg.Index)
	if index == "" {
		return nil, fmt.Errorf("elasticsearch index is required")
	}
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = 15 * time.Second
	}
	client := cfg.Client
	if client == nil {
		client = &http.Client{Timeout: timeout}
	}
	return &ElasticsearchRecorder{
		name:    strings.TrimSpace(cfg.Name),
		baseURL: strings.TrimRight(target, "/"),
		index:   index,
		timeout: timeout,
		headers: cloneStringMap(cfg.Headers),
		apiKey:  strings.TrimSpace(cfg.APIKey),
		client:  client,
	}, nil
}

func (r *ElasticsearchRecorder) Name() string {
	if r == nil {
		return ""
	}
	return strings.TrimSpace(r.name)
}

func (r *ElasticsearchRecorder) Deliver(ctx context.Context, event eventbus.Event) error {
	if r == nil {
		return nil
	}
	if event.Time.IsZero() {
		event.Time = time.Now().UTC()
	}
	payload, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("marshal elasticsearch audit event: %w", err)
	}
	method := http.MethodPost
	target := elasticsearchDocumentURL(r.baseURL, r.index)
	if eventID := strings.TrimSpace(event.ID); eventID != "" {
		method = http.MethodPut
		target = elasticsearchDocumentURL(r.baseURL, r.index, eventID)
	}
	req, err := http.NewRequestWithContext(ctx, method, target, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("build elasticsearch audit request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "HopClaw-Audit-Elasticsearch/1.0")
	for key, value := range r.headers {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		req.Header.Set(key, value)
	}
	if r.apiKey != "" && strings.TrimSpace(req.Header.Get("Authorization")) == "" {
		req.Header.Set("Authorization", "ApiKey "+r.apiKey)
	}
	req.Header.Set("X-HopClaw-Event-ID", strings.TrimSpace(event.ID))
	req.Header.Set("X-HopClaw-Event-Type", strings.TrimSpace(string(event.Type)))
	resp, err := r.client.Do(req)
	if err != nil {
		return fmt.Errorf("call elasticsearch audit sink: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
	return fmt.Errorf("elasticsearch audit sink returned %s: %s", resp.Status, strings.TrimSpace(string(body)))
}

func elasticsearchDocumentURL(baseURL, index string, docID ...string) string {
	trimmedBase := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	target := trimmedBase + "/" + neturl.PathEscape(strings.TrimSpace(index)) + "/_doc"
	if len(docID) > 0 && strings.TrimSpace(docID[0]) != "" {
		target += "/" + neturl.PathEscape(strings.TrimSpace(docID[0]))
	}
	return target
}

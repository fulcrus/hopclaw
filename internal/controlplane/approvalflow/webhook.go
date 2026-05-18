package approvalflow

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/fulcrus/hopclaw/internal/controlplane/webhookclient"
)

const (
	webhookSignatureHeader = "X-HopClaw-Signature"
	webhookTimestampHeader = "X-HopClaw-Timestamp"
	webhookProviderHeader  = "X-HopClaw-Approval-Provider"
	webhookOperationHeader = "X-HopClaw-Approval-Op"
)

type WebhookProviderConfig struct {
	Name      string
	SubmitURL string
	UpdateURL string
	SyncURL   string
	Headers   map[string]string
	Secret    string
	Timeout   time.Duration
}

type WebhookProvider struct {
	name      string
	submitURL string
	updateURL string
	syncURL   string
	headers   map[string]string
	secret    string
	timeout   time.Duration
	sender    *webhookclient.Client
}

func NewWebhookProvider(cfg WebhookProviderConfig) (*WebhookProvider, error) {
	name := strings.TrimSpace(cfg.Name)
	if name == "" {
		return nil, fmt.Errorf("webhook provider name is required")
	}
	submitURL := strings.TrimSpace(cfg.SubmitURL)
	updateURL := strings.TrimSpace(cfg.UpdateURL)
	syncURL := strings.TrimSpace(cfg.SyncURL)
	if submitURL == "" && updateURL == "" && syncURL == "" {
		return nil, fmt.Errorf("webhook provider %q must configure at least one endpoint", name)
	}
	timeout := cfg.Timeout
	headers := make(map[string]string, len(cfg.Headers))
	for key, value := range cfg.Headers {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		headers[key] = strings.TrimSpace(value)
	}
	return &WebhookProvider{
		name:      name,
		submitURL: submitURL,
		updateURL: updateURL,
		syncURL:   syncURL,
		headers:   headers,
		secret:    strings.TrimSpace(cfg.Secret),
		timeout:   timeout,
		sender: webhookclient.New(webhookclient.Config{
			Headers: headers,
			Secret:  strings.TrimSpace(cfg.Secret),
			Timeout: timeout,
		}),
	}, nil
}

func (p *WebhookProvider) Name() string {
	if p == nil {
		return ""
	}
	return p.name
}

func (p *WebhookProvider) SubmitApproval(ctx context.Context, req SubmitRequest) (*Submission, error) {
	if p == nil || strings.TrimSpace(p.submitURL) == "" {
		return nil, nil
	}
	var submission Submission
	ok, err := p.postJSON(ctx, p.submitURL, "submit", req, &submission)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, nil
	}
	return &submission, nil
}

func (p *WebhookProvider) UpdateApproval(ctx context.Context, req UpdateRequest) error {
	if p == nil || strings.TrimSpace(p.updateURL) == "" {
		return nil
	}
	_, err := p.postJSON(ctx, p.updateURL, "update", req, nil)
	return err
}

func (p *WebhookProvider) SyncPendingApprovals(ctx context.Context, req SyncRequest) ([]SyncResult, error) {
	if p == nil || strings.TrimSpace(p.syncURL) == "" {
		return nil, nil
	}
	respBody, err := p.postJSONRaw(ctx, p.syncURL, "sync", req)
	if err != nil {
		return nil, err
	}
	if len(bytes.TrimSpace(respBody)) == 0 {
		return nil, nil
	}
	var envelope struct {
		Results []SyncResult `json:"results"`
	}
	if err := json.Unmarshal(respBody, &envelope); err == nil && len(envelope.Results) > 0 {
		return append([]SyncResult(nil), envelope.Results...), nil
	}
	var items []SyncResult
	if err := json.Unmarshal(respBody, &items); err != nil {
		return nil, fmt.Errorf("decode webhook sync response: %w", err)
	}
	return append([]SyncResult(nil), items...), nil
}

func (p *WebhookProvider) postJSON(ctx context.Context, endpoint string, operation string, payload any, target any) (bool, error) {
	respBody, err := p.postJSONRaw(ctx, endpoint, operation, payload)
	if err != nil {
		return false, err
	}
	if target == nil || len(bytes.TrimSpace(respBody)) == 0 {
		return false, nil
	}
	if err := json.Unmarshal(respBody, target); err != nil {
		return false, fmt.Errorf("decode webhook %s response: %w", operation, err)
	}
	return true, nil
}

func (p *WebhookProvider) postJSONRaw(ctx context.Context, endpoint string, operation string, payload any) ([]byte, error) {
	if p == nil || p.sender == nil {
		return nil, fmt.Errorf("webhook provider is not configured")
	}
	respBody, err := p.sender.PostJSONRaw(ctx, endpoint, payload, map[string]string{
		webhookProviderHeader:  p.name,
		webhookOperationHeader: operation,
	})
	if err != nil {
		return nil, fmt.Errorf("webhook %s: %w", operation, err)
	}
	return respBody, nil
}

var _ Provider = (*WebhookProvider)(nil)
var _ SyncProvider = (*WebhookProvider)(nil)

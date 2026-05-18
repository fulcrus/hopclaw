package governanceadapter

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/fulcrus/hopclaw/internal/controlplane/webhookclient"
	supportmaps "github.com/fulcrus/hopclaw/internal/support/maps"
)

const (
	webhookAdapterHeader = "X-HopClaw-Governance-Adapter"
	webhookKindHeader    = "X-HopClaw-Governance-Kind"
	webhookEventHeader   = "X-HopClaw-Governance-Event"
)

type WebhookAdapterConfig struct {
	Name            string
	URL             string
	Headers         map[string]string
	Secret          string
	Timeout         time.Duration
	IncludeSnapshot bool
	Kinds           []Kind
	Metadata        map[string]any
}

type WebhookPayload struct {
	Adapter  string         `json:"adapter"`
	Record   Record         `json:"record"`
	Metadata map[string]any `json:"metadata,omitempty"`
}

type WebhookAdapter struct {
	name            string
	url             string
	includeSnapshot bool
	kinds           map[Kind]struct{}
	metadata        map[string]any
	sender          *webhookclient.Client
}

func NewWebhookAdapter(cfg WebhookAdapterConfig) (*WebhookAdapter, error) {
	name := strings.TrimSpace(cfg.Name)
	if name == "" {
		return nil, fmt.Errorf("governance webhook adapter name is required")
	}
	url := strings.TrimSpace(cfg.URL)
	if url == "" {
		return nil, fmt.Errorf("governance webhook adapter %q url is required", name)
	}
	kinds := make(map[Kind]struct{}, len(cfg.Kinds))
	for _, item := range cfg.Kinds {
		normalized, ok := normalizeKind(item)
		if !ok {
			return nil, fmt.Errorf("governance webhook adapter %q kind %q is not supported", name, item)
		}
		kinds[normalized] = struct{}{}
	}
	return &WebhookAdapter{
		name:            name,
		url:             url,
		includeSnapshot: cfg.IncludeSnapshot,
		kinds:           kinds,
		metadata:        supportmaps.Clone(cfg.Metadata),
		sender: webhookclient.New(webhookclient.Config{
			Headers: cfg.Headers,
			Secret:  strings.TrimSpace(cfg.Secret),
			Timeout: cfg.Timeout,
		}),
	}, nil
}

func (a *WebhookAdapter) Name() string {
	if a == nil {
		return ""
	}
	return a.name
}

func (a *WebhookAdapter) HandleGovernanceRecord(ctx context.Context, record Record) error {
	if a == nil || a.sender == nil || strings.TrimSpace(a.url) == "" {
		return nil
	}
	record = record.Normalized()
	if len(a.kinds) > 0 {
		if _, ok := a.kinds[record.Kind]; !ok {
			return nil
		}
	}
	if !a.includeSnapshot {
		record.Snapshot = nil
	}
	_, err := a.sender.PostJSON(ctx, a.url, WebhookPayload{
		Adapter:  a.name,
		Record:   record,
		Metadata: supportmaps.Clone(a.metadata),
	}, nil, map[string]string{
		webhookAdapterHeader: a.name,
		webhookKindHeader:    string(record.Kind),
		webhookEventHeader:   string(record.EventType),
	})
	if err != nil {
		return fmt.Errorf("governance webhook adapter %s: %w", a.name, err)
	}
	return nil
}

func normalizeKind(value Kind) (Kind, bool) {
	switch Kind(strings.ToLower(strings.TrimSpace(string(value)))) {
	case KindApprovalRequested:
		return KindApprovalRequested, true
	case KindApprovalResolved:
		return KindApprovalResolved, true
	case KindApprovalTimedOut:
		return KindApprovalTimedOut, true
	case KindApprovalGraceWarning:
		return KindApprovalGraceWarning, true
	case KindSecurityEvent:
		return KindSecurityEvent, true
	default:
		return "", false
	}
}

var _ Adapter = (*WebhookAdapter)(nil)
var _ NamedAdapter = (*WebhookAdapter)(nil)

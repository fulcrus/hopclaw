package governanceadapter

import (
	"context"
	"strings"

	"github.com/fulcrus/hopclaw/controlplane"
)

type governanceDeliveryControl interface {
	GetDelivery(ctx context.Context, id string) (*DeliveryEntry, error)
	ListDeliveries(ctx context.Context, filter DeliveryListFilter) ([]*DeliveryEntry, error)
	Redrive(ctx context.Context, ids []string, opts DeliveryRedriveOptions) ([]*DeliveryEntry, error)
}

type publicGovernanceDeliveryController struct {
	inner governanceDeliveryControl
}

func AdaptDeliveryController(inner governanceDeliveryControl) controlplane.GovernanceDeliveryController {
	if inner == nil {
		return nil
	}
	return publicGovernanceDeliveryController{inner: inner}
}

func (c publicGovernanceDeliveryController) GetDelivery(ctx context.Context, id string) (*controlplane.GovernanceDeliveryEntry, error) {
	item, err := c.inner.GetDelivery(ctx, id)
	if err != nil {
		return nil, err
	}
	return publicGovernanceDeliveryEntry(item), nil
}

func (c publicGovernanceDeliveryController) ListDeliveries(ctx context.Context, filter controlplane.GovernanceDeliveryListFilter) ([]*controlplane.GovernanceDeliveryEntry, error) {
	items, err := c.inner.ListDeliveries(ctx, filter.Normalized())
	if err != nil {
		return nil, err
	}
	return publicGovernanceDeliveryEntries(items), nil
}

func (c publicGovernanceDeliveryController) Redrive(ctx context.Context, ids []string, opts controlplane.GovernanceDeliveryRedriveOptions) ([]*controlplane.GovernanceDeliveryEntry, error) {
	items, err := c.inner.Redrive(ctx, ids, opts)
	if err != nil {
		return nil, err
	}
	return publicGovernanceDeliveryEntries(items), nil
}

func publicGovernanceDeliveryEntries(items []*DeliveryEntry) []*controlplane.GovernanceDeliveryEntry {
	if len(items) == 0 {
		return nil
	}
	out := make([]*controlplane.GovernanceDeliveryEntry, 0, len(items))
	for _, item := range items {
		if entry := publicGovernanceDeliveryEntry(item); entry != nil {
			out = append(out, entry)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func publicGovernanceDeliveryEntry(item *DeliveryEntry) *controlplane.GovernanceDeliveryEntry {
	if item == nil {
		return nil
	}
	normalized := item.Normalized()
	return &controlplane.GovernanceDeliveryEntry{
		ID:             strings.TrimSpace(normalized.ID),
		AdapterName:    strings.TrimSpace(normalized.AdapterName),
		IdempotencyKey: strings.TrimSpace(normalized.IdempotencyKey),
		Status:         normalized.Status,
		Record: controlplane.GovernanceDeliveryRecord{
			Kind:                      normalized.Record.Kind,
			EventID:                   strings.TrimSpace(normalized.Record.EventID),
			EventType:                 normalized.Record.EventType,
			RunID:                     strings.TrimSpace(normalized.Record.RunID),
			SessionID:                 strings.TrimSpace(normalized.Record.SessionID),
			Severity:                  strings.TrimSpace(normalized.Record.Severity),
			Summary:                   strings.TrimSpace(normalized.Record.Summary),
			SecurityCategory:          strings.TrimSpace(normalized.Record.SecurityCategory),
			Scope:                     controlplane.ScopeRef{AutomationID: strings.TrimSpace(normalized.Record.Governance.Scope.AutomationID)},
			EffectiveConfigSnapshotID: strings.TrimSpace(normalized.Record.Governance.EffectiveConfigSnapshotID),
			ToolNames:                 append([]string(nil), normalized.Record.Governance.ToolNames...),
		},
		Attempts:      normalized.Attempts,
		MaxAttempts:   normalized.MaxAttempts,
		LastError:     strings.TrimSpace(normalized.LastError),
		NextAttemptAt: normalized.NextAttemptAt,
		LastAttemptAt: normalized.LastAttemptAt,
		CreatedAt:     normalized.CreatedAt,
		UpdatedAt:     normalized.UpdatedAt,
		DeliveredAt:   normalized.DeliveredAt,
	}
}

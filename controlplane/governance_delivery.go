package controlplane

import (
	"context"
	"strings"
	"time"

	"github.com/fulcrus/hopclaw/eventbus"
)

type GovernanceDeliveryStatus string

const (
	GovernanceDeliveryStatusPending    GovernanceDeliveryStatus = "pending"
	GovernanceDeliveryStatusDelivered  GovernanceDeliveryStatus = "delivered"
	GovernanceDeliveryStatusDeadLetter GovernanceDeliveryStatus = "dead_letter"
)

type GovernanceDeliveryRecord struct {
	Kind                      GovernanceKind     `json:"kind,omitempty"`
	EventID                   string             `json:"event_id,omitempty"`
	EventType                 eventbus.EventType `json:"event_type,omitempty"`
	RunID                     string             `json:"run_id,omitempty"`
	SessionID                 string             `json:"session_id,omitempty"`
	Severity                  string             `json:"severity,omitempty"`
	Summary                   string             `json:"summary,omitempty"`
	SecurityCategory          string             `json:"security_category,omitempty"`
	Scope                     ScopeRef           `json:"scope,omitempty"`
	EffectiveConfigSnapshotID string             `json:"effective_config_snapshot_id,omitempty"`
	ToolNames                 []string           `json:"tool_names,omitempty"`
}

func (r GovernanceDeliveryRecord) Normalized() GovernanceDeliveryRecord {
	out := r
	out.Kind = GovernanceKind(strings.TrimSpace(string(out.Kind)))
	out.EventID = strings.TrimSpace(out.EventID)
	out.EventType = eventbus.EventType(strings.TrimSpace(string(out.EventType)))
	out.RunID = strings.TrimSpace(out.RunID)
	out.SessionID = strings.TrimSpace(out.SessionID)
	out.Severity = strings.TrimSpace(out.Severity)
	out.Summary = strings.TrimSpace(out.Summary)
	out.SecurityCategory = strings.TrimSpace(out.SecurityCategory)
	out.Scope = out.Scope.Normalize()
	out.EffectiveConfigSnapshotID = strings.TrimSpace(out.EffectiveConfigSnapshotID)
	out.ToolNames = dedupeNonEmptyStrings(out.ToolNames)
	return out
}

type GovernanceDeliveryEntry struct {
	ID             string                   `json:"id"`
	AdapterName    string                   `json:"adapter_name"`
	IdempotencyKey string                   `json:"idempotency_key,omitempty"`
	Status         GovernanceDeliveryStatus `json:"status"`
	Record         GovernanceDeliveryRecord `json:"record"`
	Attempts       int                      `json:"attempts,omitempty"`
	MaxAttempts    int                      `json:"max_attempts,omitempty"`
	LastError      string                   `json:"last_error,omitempty"`
	NextAttemptAt  time.Time                `json:"next_attempt_at,omitempty"`
	LastAttemptAt  time.Time                `json:"last_attempt_at,omitempty"`
	CreatedAt      time.Time                `json:"created_at"`
	UpdatedAt      time.Time                `json:"updated_at"`
	DeliveredAt    time.Time                `json:"delivered_at,omitempty"`
}

func (e GovernanceDeliveryEntry) Normalized() GovernanceDeliveryEntry {
	out := e
	out.ID = strings.TrimSpace(out.ID)
	out.AdapterName = strings.TrimSpace(out.AdapterName)
	out.IdempotencyKey = strings.TrimSpace(out.IdempotencyKey)
	out.Status = GovernanceDeliveryStatus(strings.TrimSpace(string(out.Status)))
	if out.Status == "" {
		out.Status = GovernanceDeliveryStatusPending
	}
	out.Record = out.Record.Normalized()
	out.LastError = strings.TrimSpace(out.LastError)
	out.NextAttemptAt = out.NextAttemptAt.UTC()
	out.LastAttemptAt = out.LastAttemptAt.UTC()
	out.CreatedAt = out.CreatedAt.UTC()
	out.UpdatedAt = out.UpdatedAt.UTC()
	out.DeliveredAt = out.DeliveredAt.UTC()
	return out
}

type GovernanceDeliveryListFilter struct {
	Status      GovernanceDeliveryStatus
	AdapterName string
	Limit       int
}

func (f GovernanceDeliveryListFilter) Normalized() GovernanceDeliveryListFilter {
	return GovernanceDeliveryListFilter{
		Status:      GovernanceDeliveryStatus(strings.TrimSpace(string(f.Status))),
		AdapterName: strings.TrimSpace(f.AdapterName),
		Limit:       f.Limit,
	}
}

type GovernanceDeliveryRedriveOptions struct {
	ResetAttempts bool
	ClearError    bool
}

type GovernanceDeliveryController interface {
	GetDelivery(ctx context.Context, id string) (*GovernanceDeliveryEntry, error)
	ListDeliveries(ctx context.Context, filter GovernanceDeliveryListFilter) ([]*GovernanceDeliveryEntry, error)
	Redrive(ctx context.Context, ids []string, opts GovernanceDeliveryRedriveOptions) ([]*GovernanceDeliveryEntry, error)
}

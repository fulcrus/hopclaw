package runtime

import (
	"context"
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/fulcrus/hopclaw/controlplane"
	"github.com/fulcrus/hopclaw/eventbus"
	"github.com/fulcrus/hopclaw/internal/support/normalize"
)

var ErrGovernanceDeliveryControllerNil = errors.New("governance delivery controller is not configured")

const (
	governanceHealthStatusOK       = "ok"
	governanceHealthStatusWarn     = "warn"
	governanceHealthStatusCritical = "critical"

	governancePendingWarnAfter     = 5 * time.Minute
	governancePendingCriticalAfter = 30 * time.Minute
)

type GovernanceDeliveryController = controlplane.GovernanceDeliveryController

type GovernanceDeliveryFilter struct {
	Status      controlplane.GovernanceDeliveryStatus `json:"status,omitempty"`
	AdapterName string                                `json:"adapter_name,omitempty"`
	RunID       string                                `json:"run_id,omitempty"`
	SessionID   string                                `json:"session_id,omitempty"`
	EventType   eventbus.EventType                    `json:"event_type,omitempty"`
	Kind        controlplane.GovernanceKind           `json:"kind,omitempty"`
	Query       string                                `json:"query,omitempty"`
	Limit       int                                   `json:"limit,omitempty"`
}

type GovernanceDeliveryRecordView struct {
	Kind                      controlplane.GovernanceKind `json:"kind,omitempty"`
	EventID                   string                      `json:"event_id,omitempty"`
	EventType                 eventbus.EventType          `json:"event_type,omitempty"`
	RunID                     string                      `json:"run_id,omitempty"`
	SessionID                 string                      `json:"session_id,omitempty"`
	Severity                  string                      `json:"severity,omitempty"`
	Summary                   string                      `json:"summary,omitempty"`
	SecurityCategory          string                      `json:"security_category,omitempty"`
	Scope                     controlplane.ScopeRef       `json:"scope,omitempty"`
	EffectiveConfigSnapshotID string                      `json:"effective_config_snapshot_id,omitempty"`
	ToolNames                 []string                    `json:"tool_names,omitempty"`
}

type GovernanceDeliveryView struct {
	ID             string                                `json:"id"`
	AdapterName    string                                `json:"adapter_name"`
	IdempotencyKey string                                `json:"idempotency_key,omitempty"`
	Status         controlplane.GovernanceDeliveryStatus `json:"status"`
	Attempts       int                                   `json:"attempts,omitempty"`
	MaxAttempts    int                                   `json:"max_attempts,omitempty"`
	LastError      string                                `json:"last_error,omitempty"`
	NextAttemptAt  time.Time                             `json:"next_attempt_at,omitempty"`
	LastAttemptAt  time.Time                             `json:"last_attempt_at,omitempty"`
	CreatedAt      time.Time                             `json:"created_at"`
	UpdatedAt      time.Time                             `json:"updated_at"`
	DeliveredAt    time.Time                             `json:"delivered_at,omitempty"`
	CanRedrive     bool                                  `json:"can_redrive"`
	Record         GovernanceDeliveryRecordView          `json:"record"`
}

type GovernanceDeliveryStats struct {
	Total      int                                           `json:"total"`
	Redrivable int                                           `json:"redrivable"`
	ByStatus   map[controlplane.GovernanceDeliveryStatus]int `json:"by_status,omitempty"`
	ByAdapter  []GovernanceDeliveryAdapterStats              `json:"by_adapter,omitempty"`
}

type GovernanceDeliveryHealth struct {
	Status             string    `json:"status"`
	Summary            string    `json:"summary,omitempty"`
	EvaluatedAt        time.Time `json:"evaluated_at"`
	Total              int       `json:"total"`
	PendingCount       int       `json:"pending_count"`
	DeliveredCount     int       `json:"delivered_count"`
	DeadLetterCount    int       `json:"dead_letter_count"`
	RedrivableCount    int       `json:"redrivable_count"`
	StalePendingCount  int       `json:"stale_pending_count"`
	OldestPendingAt    time.Time `json:"oldest_pending_at,omitempty"`
	OldestDeadLetterAt time.Time `json:"oldest_dead_letter_at,omitempty"`
	AdaptersImpacted   []string  `json:"adapters_impacted,omitempty"`
}

type GovernanceDeliveryAdapterStats struct {
	AdapterName string `json:"adapter_name"`
	Total       int    `json:"total"`
	Pending     int    `json:"pending"`
	Delivered   int    `json:"delivered"`
	DeadLetter  int    `json:"dead_letter"`
	Redrivable  int    `json:"redrivable"`
}

type GovernanceRedriveRequest struct {
	IDs           []string                 `json:"ids,omitempty"`
	Filter        GovernanceDeliveryFilter `json:"filter,omitempty"`
	ResetAttempts *bool                    `json:"reset_attempts,omitempty"`
	ClearError    *bool                    `json:"clear_error,omitempty"`
}

type GovernanceRedriveResult struct {
	Items   []*GovernanceDeliveryView `json:"items"`
	Count   int                       `json:"count"`
	Updated int                       `json:"updated"`
	Skipped int                       `json:"skipped"`
}

type GovernanceEventFilter struct {
	Type           eventbus.EventType `json:"type,omitempty"`
	RunID          string             `json:"run_id,omitempty"`
	SessionID      string             `json:"session_id,omitempty"`
	AdapterName    string             `json:"adapter_name,omitempty"`
	DeliveryStatus string             `json:"delivery_status,omitempty"`
	Severity       string             `json:"severity,omitempty"`
	Limit          int                `json:"limit,omitempty"`
}

func (s *Service) WithGovernanceDelivery(controller GovernanceDeliveryController) *Service {
	s.governance = controller
	return s
}

func (s *Service) GetGovernanceDeliveryController() GovernanceDeliveryController {
	if s == nil {
		return nil
	}
	return s.governance
}

func (s *Service) ListGovernanceDeliveries(ctx context.Context, filter GovernanceDeliveryFilter) ([]*GovernanceDeliveryView, error) {
	if s == nil || s.governance == nil {
		return nil, ErrGovernanceDeliveryControllerNil
	}
	normalized := filter.Normalize()
	storeFilter := controlplane.GovernanceDeliveryListFilter{
		Status:      normalized.Status,
		AdapterName: normalized.AdapterName,
	}
	if !normalized.requiresPostFilter() && normalized.Limit > 0 {
		storeFilter.Limit = normalized.Limit
	}
	items, err := s.governance.ListDeliveries(ctx, storeFilter)
	if err != nil {
		return nil, err
	}
	views := projectGovernanceDeliveryViews(items)
	if normalized.requiresPostFilter() {
		views = filterGovernanceDeliveryViews(views, normalized)
	}
	sortGovernanceDeliveryViews(views)
	if normalized.Limit > 0 && len(views) > normalized.Limit {
		views = views[:normalized.Limit]
	}
	return views, nil
}

func (s *Service) GetGovernanceDelivery(ctx context.Context, id string) (*GovernanceDeliveryView, error) {
	if s == nil || s.governance == nil {
		return nil, ErrGovernanceDeliveryControllerNil
	}
	entry, err := s.governance.GetDelivery(ctx, strings.TrimSpace(id))
	if err != nil {
		return nil, err
	}
	return buildGovernanceDeliveryView(entry), nil
}

func (s *Service) GetGovernanceDeliveryStats(ctx context.Context, filter GovernanceDeliveryFilter) (*GovernanceDeliveryStats, error) {
	views, err := s.ListGovernanceDeliveries(ctx, filter.withoutLimit())
	if err != nil {
		return nil, err
	}
	stats := &GovernanceDeliveryStats{
		ByStatus: make(map[controlplane.GovernanceDeliveryStatus]int, 3),
	}
	byAdapter := make(map[string]*GovernanceDeliveryAdapterStats)
	for _, item := range views {
		if item == nil {
			continue
		}
		stats.Total++
		stats.ByStatus[item.Status]++
		if item.CanRedrive {
			stats.Redrivable++
		}
		adapterName := strings.TrimSpace(item.AdapterName)
		if adapterName == "" {
			adapterName = "<unknown>"
		}
		current := byAdapter[adapterName]
		if current == nil {
			current = &GovernanceDeliveryAdapterStats{AdapterName: adapterName}
			byAdapter[adapterName] = current
		}
		current.Total++
		if item.CanRedrive {
			current.Redrivable++
		}
		switch item.Status {
		case controlplane.GovernanceDeliveryStatusPending:
			current.Pending++
		case controlplane.GovernanceDeliveryStatusDelivered:
			current.Delivered++
		case controlplane.GovernanceDeliveryStatusDeadLetter:
			current.DeadLetter++
		}
	}
	stats.ByAdapter = make([]GovernanceDeliveryAdapterStats, 0, len(byAdapter))
	for _, item := range byAdapter {
		stats.ByAdapter = append(stats.ByAdapter, *item)
	}
	sort.Slice(stats.ByAdapter, func(i, j int) bool {
		if stats.ByAdapter[i].Total == stats.ByAdapter[j].Total {
			return stats.ByAdapter[i].AdapterName < stats.ByAdapter[j].AdapterName
		}
		return stats.ByAdapter[i].Total > stats.ByAdapter[j].Total
	})
	return stats, nil
}

func (s *Service) GetGovernanceDeliveryHealth(ctx context.Context, filter GovernanceDeliveryFilter) (*GovernanceDeliveryHealth, error) {
	views, err := s.ListGovernanceDeliveries(ctx, filter.withoutLimit())
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	health := &GovernanceDeliveryHealth{
		Status:      governanceHealthStatusOK,
		EvaluatedAt: now,
	}
	impacted := make(map[string]struct{})
	criticalOverdue := 0
	for _, item := range views {
		if item == nil {
			continue
		}
		health.Total++
		if item.CanRedrive {
			health.RedrivableCount++
		}
		switch item.Status {
		case controlplane.GovernanceDeliveryStatusPending:
			health.PendingCount++
			dueAt := governanceDeliveryPendingDueAt(item)
			if !dueAt.IsZero() && (health.OldestPendingAt.IsZero() || dueAt.Before(health.OldestPendingAt)) {
				health.OldestPendingAt = dueAt
			}
			if !dueAt.IsZero() && now.Sub(dueAt) >= governancePendingWarnAfter {
				health.StalePendingCount++
				impacted[strings.TrimSpace(item.AdapterName)] = struct{}{}
			}
			if !dueAt.IsZero() && now.Sub(dueAt) >= governancePendingCriticalAfter {
				criticalOverdue++
			}
		case controlplane.GovernanceDeliveryStatusDelivered:
			health.DeliveredCount++
		case controlplane.GovernanceDeliveryStatusDeadLetter:
			health.DeadLetterCount++
			deadAt := governanceDeliveryDeadLetterAt(item)
			if !deadAt.IsZero() && (health.OldestDeadLetterAt.IsZero() || deadAt.Before(health.OldestDeadLetterAt)) {
				health.OldestDeadLetterAt = deadAt
			}
			impacted[strings.TrimSpace(item.AdapterName)] = struct{}{}
		}
	}
	switch {
	case health.DeadLetterCount > 0 || criticalOverdue > 0:
		health.Status = governanceHealthStatusCritical
	case health.StalePendingCount > 0:
		health.Status = governanceHealthStatusWarn
	default:
		health.Status = governanceHealthStatusOK
	}
	health.AdaptersImpacted = sortedNonEmptyKeys(impacted)
	health.Summary = buildGovernanceDeliveryHealthSummary(health)
	return health, nil
}

func (s *Service) RedriveGovernanceDeliveries(ctx context.Context, req GovernanceRedriveRequest) (*GovernanceRedriveResult, error) {
	if s == nil || s.governance == nil {
		return nil, ErrGovernanceDeliveryControllerNil
	}
	ids := normalize.DedupeStrings(req.IDs)
	if len(ids) == 0 {
		filter := req.Filter.Normalize()
		if filter.IsZero() {
			return nil, errors.New("governance delivery ids or filter are required")
		}
		items, err := s.ListGovernanceDeliveries(ctx, filter.withoutLimit())
		if err != nil {
			return nil, err
		}
		ids = collectGovernanceDeliveryRedriveIDs(items)
		if filter.Limit > 0 && len(ids) > filter.Limit {
			ids = ids[:filter.Limit]
		}
	}
	redriveIDs, skipped := s.partitionGovernanceDeliveryIDsForRedrive(ctx, ids)
	if len(redriveIDs) == 0 {
		return &GovernanceRedriveResult{
			Items:   nil,
			Count:   skipped,
			Updated: 0,
			Skipped: skipped,
		}, nil
	}
	opts := controlplane.GovernanceDeliveryRedriveOptions{
		ResetAttempts: boolValue(req.ResetAttempts, true),
		ClearError:    boolValue(req.ClearError, true),
	}
	items, err := s.governance.Redrive(ctx, redriveIDs, opts)
	if err != nil {
		return nil, err
	}
	views := projectGovernanceDeliveryViews(items)
	sortGovernanceDeliveryViews(views)
	return &GovernanceRedriveResult{
		Items:   views,
		Count:   len(redriveIDs) + skipped,
		Updated: len(views),
		Skipped: skipped,
	}, nil
}

func (s *Service) ListGovernanceEventViews(filter GovernanceEventFilter) []EventView {
	normalized := filter.Normalize()
	events := s.EventSnapshot()
	if len(events) == 0 {
		return nil
	}
	items := make([]eventbus.Event, 0, len(events))
	for _, event := range events {
		if !normalized.matchesEvent(event) {
			continue
		}
		items = append(items, event)
	}
	items = tailLimitEvents(items, normalized.Limit)
	return ProjectEventViews(items)
}

func (f GovernanceDeliveryFilter) Normalize() GovernanceDeliveryFilter {
	return GovernanceDeliveryFilter{
		Status:      controlplane.GovernanceDeliveryStatus(strings.TrimSpace(string(f.Status))),
		AdapterName: strings.TrimSpace(f.AdapterName),
		RunID:       strings.TrimSpace(f.RunID),
		SessionID:   strings.TrimSpace(f.SessionID),
		EventType:   eventbus.EventType(strings.TrimSpace(string(f.EventType))),
		Kind:        controlplane.GovernanceKind(strings.TrimSpace(string(f.Kind))),
		Query:       strings.ToLower(strings.TrimSpace(f.Query)),
		Limit:       f.Limit,
	}
}

func (f GovernanceDeliveryFilter) IsZero() bool {
	normalized := f.Normalize()
	return normalized.Status == "" &&
		normalized.AdapterName == "" &&
		normalized.RunID == "" &&
		normalized.SessionID == "" &&
		normalized.EventType == "" &&
		normalized.Kind == "" &&
		normalized.Query == ""
}

func (f GovernanceDeliveryFilter) withoutLimit() GovernanceDeliveryFilter {
	f.Limit = 0
	return f
}

func (f GovernanceDeliveryFilter) requiresPostFilter() bool {
	return f.RunID != "" || f.SessionID != "" || f.EventType != "" || f.Kind != "" || f.Query != ""
}

func (f GovernanceEventFilter) Normalize() GovernanceEventFilter {
	return GovernanceEventFilter{
		Type:           eventbus.EventType(strings.TrimSpace(string(f.Type))),
		RunID:          strings.TrimSpace(f.RunID),
		SessionID:      strings.TrimSpace(f.SessionID),
		AdapterName:    strings.TrimSpace(f.AdapterName),
		DeliveryStatus: strings.TrimSpace(f.DeliveryStatus),
		Severity:       strings.TrimSpace(f.Severity),
		Limit:          f.Limit,
	}
}

func (f GovernanceEventFilter) matchesEvent(event eventbus.Event) bool {
	if !isGovernanceEvent(event) {
		return false
	}
	normalized := f.Normalize()
	if normalized.Type != "" && event.Type != normalized.Type {
		return false
	}
	if normalized.RunID != "" && strings.TrimSpace(event.RunID) != normalized.RunID {
		return false
	}
	if normalized.SessionID != "" && strings.TrimSpace(event.SessionID) != normalized.SessionID {
		return false
	}
	payload, _ := event.GovernanceDeliveryPayload()
	if normalized.AdapterName != "" && strings.TrimSpace(payload.AdapterName) != normalized.AdapterName {
		return false
	}
	if normalized.DeliveryStatus != "" && strings.TrimSpace(payload.DeliveryStatus) != normalized.DeliveryStatus {
		return false
	}
	if normalized.Severity != "" && !strings.EqualFold(governanceEventSeverity(event), normalized.Severity) {
		return false
	}
	return true
}

func governanceEventSeverity(event eventbus.Event) string {
	if payload, ok := event.SecurityFindingPayload(); ok {
		return strings.TrimSpace(payload.Severity)
	}
	if payload, ok := event.SecurityRiskDetectedPayload(); ok {
		return strings.TrimSpace(payload.Severity)
	}
	return ""
}

func buildGovernanceDeliveryView(entry *controlplane.GovernanceDeliveryEntry) *GovernanceDeliveryView {
	if entry == nil {
		return nil
	}
	item := entry.Normalized()
	return &GovernanceDeliveryView{
		ID:             strings.TrimSpace(item.ID),
		AdapterName:    strings.TrimSpace(item.AdapterName),
		IdempotencyKey: strings.TrimSpace(item.IdempotencyKey),
		Status:         item.Status,
		Attempts:       item.Attempts,
		MaxAttempts:    item.MaxAttempts,
		LastError:      strings.TrimSpace(item.LastError),
		NextAttemptAt:  item.NextAttemptAt,
		LastAttemptAt:  item.LastAttemptAt,
		CreatedAt:      item.CreatedAt,
		UpdatedAt:      item.UpdatedAt,
		DeliveredAt:    item.DeliveredAt,
		CanRedrive:     item.Status != controlplane.GovernanceDeliveryStatusDelivered,
		Record: GovernanceDeliveryRecordView{
			Kind:                      item.Record.Kind,
			EventID:                   strings.TrimSpace(item.Record.EventID),
			EventType:                 item.Record.EventType,
			RunID:                     strings.TrimSpace(item.Record.RunID),
			SessionID:                 strings.TrimSpace(item.Record.SessionID),
			Severity:                  strings.TrimSpace(item.Record.Severity),
			Summary:                   strings.TrimSpace(item.Record.Summary),
			SecurityCategory:          strings.TrimSpace(item.Record.SecurityCategory),
			Scope:                     item.Record.Scope.Normalize(),
			EffectiveConfigSnapshotID: strings.TrimSpace(item.Record.EffectiveConfigSnapshotID),
			ToolNames:                 normalize.DedupeStrings(append([]string(nil), item.Record.ToolNames...)),
		},
	}
}

func projectGovernanceDeliveryViews(items []*controlplane.GovernanceDeliveryEntry) []*GovernanceDeliveryView {
	if len(items) == 0 {
		return nil
	}
	out := make([]*GovernanceDeliveryView, 0, len(items))
	for _, item := range items {
		if view := buildGovernanceDeliveryView(item); view != nil {
			out = append(out, view)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func filterGovernanceDeliveryViews(items []*GovernanceDeliveryView, filter GovernanceDeliveryFilter) []*GovernanceDeliveryView {
	if len(items) == 0 {
		return nil
	}
	normalized := filter.Normalize()
	out := make([]*GovernanceDeliveryView, 0, len(items))
	for _, item := range items {
		if item == nil {
			continue
		}
		if normalized.RunID != "" && item.Record.RunID != normalized.RunID {
			continue
		}
		if normalized.SessionID != "" && item.Record.SessionID != normalized.SessionID {
			continue
		}
		if normalized.EventType != "" && item.Record.EventType != normalized.EventType {
			continue
		}
		if normalized.Kind != "" && item.Record.Kind != normalized.Kind {
			continue
		}
		if normalized.Query != "" && !governanceDeliveryMatchesQuery(item, normalized.Query) {
			continue
		}
		out = append(out, item)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func governanceDeliveryMatchesQuery(item *GovernanceDeliveryView, query string) bool {
	if item == nil || query == "" {
		return false
	}
	fields := []string{
		item.ID,
		item.AdapterName,
		item.IdempotencyKey,
		string(item.Status),
		item.LastError,
		item.Record.EventID,
		string(item.Record.EventType),
		string(item.Record.Kind),
		item.Record.RunID,
		item.Record.SessionID,
		item.Record.Severity,
		item.Record.Summary,
		item.Record.SecurityCategory,
		item.Record.Scope.AutomationID,
		item.Record.EffectiveConfigSnapshotID,
	}
	fields = append(fields, item.Record.ToolNames...)
	for _, field := range fields {
		if strings.Contains(strings.ToLower(strings.TrimSpace(field)), query) {
			return true
		}
	}
	return false
}

func sortGovernanceDeliveryViews(items []*GovernanceDeliveryView) {
	sort.Slice(items, func(i, j int) bool {
		left := governanceDeliverySortTime(items[i])
		right := governanceDeliverySortTime(items[j])
		if left.Equal(right) {
			return items[i].ID > items[j].ID
		}
		return left.After(right)
	})
}

func governanceDeliverySortTime(item *GovernanceDeliveryView) time.Time {
	if item == nil {
		return time.Time{}
	}
	if !item.UpdatedAt.IsZero() {
		return item.UpdatedAt
	}
	return item.CreatedAt
}

func collectGovernanceDeliveryRedriveIDs(items []*GovernanceDeliveryView) []string {
	if len(items) == 0 {
		return nil
	}
	ids := make([]string, 0, len(items))
	for _, item := range items {
		if item == nil || !item.CanRedrive {
			continue
		}
		ids = append(ids, item.ID)
	}
	return normalize.DedupeStrings(ids)
}

func governanceDeliveryPendingDueAt(item *GovernanceDeliveryView) time.Time {
	if item == nil {
		return time.Time{}
	}
	if !item.NextAttemptAt.IsZero() {
		return item.NextAttemptAt.UTC()
	}
	if !item.LastAttemptAt.IsZero() {
		return item.LastAttemptAt.UTC()
	}
	if !item.UpdatedAt.IsZero() {
		return item.UpdatedAt.UTC()
	}
	return item.CreatedAt.UTC()
}

func governanceDeliveryDeadLetterAt(item *GovernanceDeliveryView) time.Time {
	if item == nil {
		return time.Time{}
	}
	if !item.UpdatedAt.IsZero() {
		return item.UpdatedAt.UTC()
	}
	if !item.LastAttemptAt.IsZero() {
		return item.LastAttemptAt.UTC()
	}
	return item.CreatedAt.UTC()
}

func buildGovernanceDeliveryHealthSummary(health *GovernanceDeliveryHealth) string {
	if health == nil {
		return ""
	}
	switch {
	case health.DeadLetterCount > 0:
		return "Dead-letter governance deliveries require operator redrive."
	case health.StalePendingCount > 0:
		return "Governance deliveries are overdue and need operator attention."
	case health.PendingCount > 0:
		return "Governance deliveries are flowing with queued work in progress."
	case health.Total == 0:
		return "No governance deliveries have been recorded yet."
	default:
		return "Governance delivery pipeline is healthy."
	}
}

func sortedNonEmptyKeys(items map[string]struct{}) []string {
	if len(items) == 0 {
		return nil
	}
	out := make([]string, 0, len(items))
	for key := range items {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		out = append(out, key)
	}
	sort.Strings(out)
	if len(out) == 0 {
		return nil
	}
	return out
}

func (s *Service) partitionGovernanceDeliveryIDsForRedrive(ctx context.Context, ids []string) ([]string, int) {
	if len(ids) == 0 {
		return nil, 0
	}
	redriveIDs := make([]string, 0, len(ids))
	skipped := 0
	for _, id := range normalize.DedupeStrings(ids) {
		item, err := s.GetGovernanceDelivery(ctx, id)
		if err != nil || item == nil {
			skipped++
			continue
		}
		if !item.CanRedrive {
			skipped++
			continue
		}
		redriveIDs = append(redriveIDs, id)
	}
	return redriveIDs, skipped
}

func isGovernanceEvent(event eventbus.Event) bool {
	if strings.HasPrefix(string(event.Type), "governance.delivery.") {
		return true
	}
	return governanceReceiptFromEvent(event) != nil
}

func boolValue(value *bool, defaultValue bool) bool {
	if value == nil {
		return defaultValue
	}
	return *value
}

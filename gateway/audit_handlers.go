package gateway

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/fulcrus/hopclaw/eventbus"
	domainscope "github.com/fulcrus/hopclaw/internal/domain/scope"
	"github.com/fulcrus/hopclaw/internal/support/ints"
	runtimepkg "github.com/fulcrus/hopclaw/runtime"
)

const (
	auditFamilySecurity   = "security"
	auditFamilyApproval   = "approval"
	auditFamilyGovernance = "governance"
)

// ---------------------------------------------------------------------------
// Constants
// ---------------------------------------------------------------------------

const (
	defaultAuditEventLimit = 100
	maxAuditEventLimit     = 1000
)

// ---------------------------------------------------------------------------
// Response types
// ---------------------------------------------------------------------------

type auditEventsResponse struct {
	Items        []runtimepkg.EventView `json:"items"`
	Count        int                    `json:"count"`
	NextCursor   string                 `json:"next_cursor,omitempty"`
	CursorStatus string                 `json:"cursor_status,omitempty"`
}

type auditEventFilter struct {
	Families    map[string]bool
	EventType   string
	Severity    string
	RunID       string
	SessionID   string
	ApprovalID  string
	AdapterName string
	Since       time.Time
	Until       time.Time
}

// ---------------------------------------------------------------------------
// Handler
// ---------------------------------------------------------------------------

// handleAuditEvents returns security audit events with optional filters.
//
//	GET /operator/audit/events?type=security.risk_detected&severity=high&since=2024-01-01T00:00:00Z&until=2024-12-31T23:59:59Z&limit=50
func (g *Gateway) handleAuditEvents(w http.ResponseWriter, r *http.Request) {
	if g.runtime == nil {
		gwError(w, http.StatusServiceUnavailable, "runtime not available")
		return
	}

	query := r.URL.Query()
	authScope := authScopeFromIdentity(AuthIdentityFromContext(r.Context()))

	filter := auditEventFilter{
		Families:    parseAuditFamilies(query.Get("family")),
		EventType:   strings.TrimSpace(query.Get("type")),
		Severity:    strings.TrimSpace(query.Get("severity")),
		RunID:       strings.TrimSpace(query.Get("run_id")),
		SessionID:   strings.TrimSpace(query.Get("session_id")),
		ApprovalID:  strings.TrimSpace(query.Get("approval_id")),
		AdapterName: strings.TrimSpace(query.Get("adapter_name")),
	}

	if raw := strings.TrimSpace(query.Get("since")); raw != "" {
		parsed, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			gwErrorf(w, http.StatusBadRequest, "invalid since: %v", err)
			return
		}
		filter.Since = parsed
	}
	if raw := strings.TrimSpace(query.Get("until")); raw != "" {
		parsed, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			gwErrorf(w, http.StatusBadRequest, "invalid until: %v", err)
			return
		}
		filter.Until = parsed
	}

	limit := defaultAuditEventLimit
	if raw := strings.TrimSpace(query.Get("limit")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 0 {
			gwError(w, http.StatusBadRequest, "invalid limit")
			return
		}
		limit = parsed
		if limit > maxAuditEventLimit {
			limit = maxAuditEventLimit
		}
	}

	sinceID := strings.TrimSpace(query.Get("since_id"))
	storeLimit := limit
	if !authScope.isZero() {
		storeLimit = 0
	}
	var filtered []eventbus.Event
	var nextCursor string
	cursorStatus := string(eventbus.CursorEmpty)
	if sinceID != "" {
		result := g.runtime.EventsSince(sinceID, 0)
		cursorStatus = string(result.Status)
		filtered, nextCursor = filterAuditEventsSince(result.Events, filter, storeLimit)
		if nextCursor == "" {
			nextCursor = strings.TrimSpace(result.NextCursor)
		}
	} else {
		all := g.runtime.EventSnapshot()
		filtered = filterAuditEvents(all, filter, storeLimit)
		nextCursor = lastAuditCursor(all)
	}
	if !authScope.isZero() {
		filtered = g.filterAuditEventsByAuthScope(r.Context(), authScope, filtered)
		if sinceID != "" {
			filtered = limitAuditEventsSince(filtered, limit)
		} else {
			filtered = limitAuditEventsSnapshot(filtered, limit)
		}
	}

	gwJSON(w, http.StatusOK, auditEventsResponse{
		Items:        runtimepkg.ProjectEventViews(filtered),
		Count:        len(filtered),
		NextCursor:   nextCursor,
		CursorStatus: cursorStatus,
	})
}

// filterAuditEvents applies the given filters to a list of events and returns
// matching events up to the specified limit.
func filterAuditEvents(events []eventbus.Event, filter auditEventFilter, limit int) []eventbus.Event {
	if len(events) == 0 {
		return []eventbus.Event{}
	}
	out := make([]eventbus.Event, 0, len(events))
	for _, event := range events {
		if !matchesAuditEventFilter(event, filter) {
			continue
		}
		out = append(out, event)
	}
	if limit > 0 && len(out) > limit {
		out = out[len(out)-limit:]
	}
	return out
}

func filterAuditEventsSince(events []eventbus.Event, filter auditEventFilter, limit int) ([]eventbus.Event, string) {
	if len(events) == 0 {
		return []eventbus.Event{}, ""
	}
	out := make([]eventbus.Event, 0, ints.PositiveMin(limit, len(events)))
	lastScanned := ""
	for _, event := range events {
		lastScanned = strings.TrimSpace(event.ID)
		if !matchesAuditEventFilter(event, filter) {
			continue
		}
		out = append(out, event)
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out, lastScanned
}

func limitAuditEventsSnapshot(events []eventbus.Event, limit int) []eventbus.Event {
	if limit > 0 && len(events) > limit {
		return events[len(events)-limit:]
	}
	return events
}

func limitAuditEventsSince(events []eventbus.Event, limit int) []eventbus.Event {
	if limit > 0 && len(events) > limit {
		return events[:limit]
	}
	return events
}

func (g *Gateway) filterAuditEventsByAuthScope(ctx context.Context, scope authScope, events []eventbus.Event) []eventbus.Event {
	if len(events) == 0 || scope.isZero() {
		return events
	}
	out := make([]eventbus.Event, 0, len(events))
	for _, event := range events {
		if g.auditEventMatchesAuthScope(ctx, scope, event) {
			out = append(out, event)
		}
	}
	return out
}

func (g *Gateway) auditEventMatchesAuthScope(ctx context.Context, scope authScope, event eventbus.Event) bool {
	if scope.isZero() {
		return true
	}
	scopeFilter := scope.scopeFilter()
	if eventScope := domainscope.FromValue(event.Attrs["scope"]); !eventScope.IsZero() {
		return scopeFilter.Matches(eventScope)
	}
	if g.runtime != nil {
		if runID := strings.TrimSpace(event.RunID); runID != "" {
			if _, err := g.runtime.GetRunScoped(ctx, runID, scopeFilter); err == nil {
				return true
			}
		}
		if sessionID := strings.TrimSpace(event.SessionID); sessionID != "" {
			if _, err := g.runtime.GetSessionScoped(ctx, sessionID, scopeFilter); err == nil {
				return true
			}
		}
	}
	return false
}

func matchesAuditEventFilter(event eventbus.Event, filter auditEventFilter) bool {
	family := auditEventFamily(event)
	if family == "" || !filter.Families[family] {
		return false
	}
	if filter.EventType != "" && string(event.Type) != filter.EventType {
		return false
	}
	if filter.RunID != "" && strings.TrimSpace(event.RunID) != filter.RunID {
		return false
	}
	if filter.SessionID != "" && strings.TrimSpace(event.SessionID) != filter.SessionID {
		return false
	}
	if !filter.Since.IsZero() && event.Time.Before(filter.Since) {
		return false
	}
	if !filter.Until.IsZero() && event.Time.After(filter.Until) {
		return false
	}
	if filter.Severity != "" && !strings.EqualFold(auditEventSeverity(event), filter.Severity) {
		return false
	}
	if filter.ApprovalID != "" && auditEventApprovalID(event) != filter.ApprovalID {
		return false
	}
	if filter.AdapterName != "" && auditEventAdapterName(event) != filter.AdapterName {
		return false
	}
	return true
}

func auditEventSeverity(event eventbus.Event) string {
	if payload, ok := event.SecurityFindingPayload(); ok {
		return strings.TrimSpace(payload.Severity)
	}
	if payload, ok := event.SecurityRiskDetectedPayload(); ok {
		return strings.TrimSpace(payload.Severity)
	}
	return ""
}

func auditEventApprovalID(event eventbus.Event) string {
	if payload, ok := event.ApprovalPayload(); ok {
		return strings.TrimSpace(payload.ApprovalID)
	}
	return ""
}

func auditEventAdapterName(event eventbus.Event) string {
	if payload, ok := event.GovernanceDeliveryPayload(); ok {
		return strings.TrimSpace(payload.AdapterName)
	}
	return ""
}

func auditEventScope(event eventbus.Event) domainscope.Ref {
	if payload, ok := event.GovernancePayload(); ok {
		return payload.Scope.Normalize()
	}
	return domainscope.Ref{}
}

func parseAuditFamilies(raw string) map[string]bool {
	out := map[string]bool{
		auditFamilySecurity:   true,
		auditFamilyApproval:   true,
		auditFamilyGovernance: true,
	}
	text := strings.TrimSpace(raw)
	if text == "" {
		return out
	}
	out = make(map[string]bool)
	for _, item := range strings.Split(text, ",") {
		switch strings.ToLower(strings.TrimSpace(item)) {
		case auditFamilySecurity, auditFamilyApproval, auditFamilyGovernance:
			out[strings.ToLower(strings.TrimSpace(item))] = true
		}
	}
	if len(out) == 0 {
		return map[string]bool{
			auditFamilySecurity:   true,
			auditFamilyApproval:   true,
			auditFamilyGovernance: true,
		}
	}
	return out
}

func auditEventFamily(event eventbus.Event) string {
	value := string(event.Type)
	switch {
	case strings.HasPrefix(value, auditFamilySecurity+"."):
		return auditFamilySecurity
	case strings.HasPrefix(value, auditFamilyApproval+"."):
		return auditFamilyApproval
	case strings.HasPrefix(value, auditFamilyGovernance+"."):
		return auditFamilyGovernance
	default:
		return ""
	}
}

func lastAuditCursor(events []eventbus.Event) string {
	if len(events) == 0 {
		return ""
	}
	return strings.TrimSpace(events[len(events)-1].ID)
}

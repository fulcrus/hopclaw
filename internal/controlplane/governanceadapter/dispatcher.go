package governanceadapter

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/fulcrus/hopclaw/controlplane"
	"github.com/fulcrus/hopclaw/eventbus"
	controlsnapshot "github.com/fulcrus/hopclaw/internal/controlplane/snapshot"
	domaingov "github.com/fulcrus/hopclaw/internal/domain/governance"
	supportmaps "github.com/fulcrus/hopclaw/internal/support/maps"
	"github.com/fulcrus/hopclaw/internal/support/normalize"
	"github.com/fulcrus/hopclaw/logging"
)

var log = logging.WithSubsystem("governance_adapter")

type Kind = controlplane.GovernanceKind

const (
	KindApprovalRequested    Kind = controlplane.GovernanceKindApprovalRequested
	KindApprovalResolved     Kind = controlplane.GovernanceKindApprovalResolved
	KindApprovalTimedOut     Kind = controlplane.GovernanceKindApprovalTimedOut
	KindApprovalGraceWarning Kind = controlplane.GovernanceKindApprovalGraceWarning
	KindSecurityEvent        Kind = controlplane.GovernanceKindSecurityEvent
)

type GovernanceApproval = domaingov.EventApproval
type GovernanceContext = domaingov.EventContext

type Record struct {
	Kind             Kind                                     `json:"kind"`
	EventID          string                                   `json:"event_id,omitempty"`
	EventType        eventbus.EventType                       `json:"event_type"`
	RunID            string                                   `json:"run_id,omitempty"`
	SessionID        string                                   `json:"session_id,omitempty"`
	Time             time.Time                                `json:"time,omitempty"`
	Severity         string                                   `json:"severity,omitempty"`
	Summary          string                                   `json:"summary,omitempty"`
	SecurityCategory string                                   `json:"security_category,omitempty"`
	Governance       GovernanceContext                        `json:"governance,omitempty"`
	Snapshot         *controlsnapshot.EffectiveConfigSnapshot `json:"snapshot,omitempty"`
	Attrs            map[string]any                           `json:"attrs,omitempty"`
}

func (r Record) Normalized() Record {
	out := r
	out.EventID = strings.TrimSpace(out.EventID)
	out.RunID = strings.TrimSpace(out.RunID)
	out.SessionID = strings.TrimSpace(out.SessionID)
	out.Severity = strings.TrimSpace(out.Severity)
	out.Summary = strings.TrimSpace(out.Summary)
	out.SecurityCategory = strings.TrimSpace(out.SecurityCategory)
	out.Governance = out.Governance.Normalized()
	if out.Summary == "" {
		out.Summary = normalize.FirstNonEmpty(strings.TrimSpace(out.Governance.Summary), governancePolicySummary(out.Attrs))
	}
	if out.Snapshot != nil {
		out.Snapshot = out.Snapshot.Clone()
	}
	if len(out.Attrs) > 0 {
		out.Attrs = supportmaps.Clone(out.Attrs)
	} else {
		out.Attrs = nil
	}
	return out
}

func governancePolicySummary(attrs map[string]any) string {
	if len(attrs) == 0 {
		return ""
	}
	payload, _ := (eventbus.Event{Attrs: attrs}).GovernancePayload()
	return strings.TrimSpace(payload.PolicySummary)
}

type Adapter interface {
	HandleGovernanceRecord(ctx context.Context, record Record) error
}

type NamedAdapter interface {
	Adapter
	Name() string
}

type AdapterFunc func(ctx context.Context, record Record) error

func (f AdapterFunc) HandleGovernanceRecord(ctx context.Context, record Record) error {
	return f(ctx, record)
}

type SnapshotResolver interface {
	EffectiveConfigSnapshot() *controlsnapshot.EffectiveConfigSnapshot
}

type Dispatcher struct {
	adapters         []Adapter
	snapshotResolver SnapshotResolver
}

func NewDispatcher(adapters ...Adapter) *Dispatcher {
	filtered := make([]Adapter, 0, len(adapters))
	for _, adapter := range adapters {
		if isNilAdapter(adapter) {
			continue
		}
		filtered = append(filtered, adapter)
	}
	return &Dispatcher{adapters: filtered}
}

func (d *Dispatcher) WithSnapshotResolver(resolver SnapshotResolver) *Dispatcher {
	d.snapshotResolver = resolver
	return d
}

func (d *Dispatcher) Handle(ctx context.Context, event eventbus.Event) error {
	if d == nil || len(d.adapters) == 0 {
		return nil
	}
	record, ok := Project(event)
	if !ok {
		return nil
	}
	if d.snapshotResolver != nil && strings.TrimSpace(record.Governance.EffectiveConfigSnapshotID) != "" {
		if snapshot := d.snapshotResolver.EffectiveConfigSnapshot(); snapshot != nil && strings.TrimSpace(snapshot.ID) == record.Governance.EffectiveConfigSnapshotID {
			record.Snapshot = snapshot.Clone()
		}
	}
	record = record.Normalized()
	for _, adapter := range d.adapters {
		if err := d.handleOne(ctx, adapter, record); err != nil {
			log.Warn("governance adapter handle failed",
				"event_type", string(record.EventType),
				"run_id", record.RunID,
				"session_id", record.SessionID,
				"error", err)
		}
	}
	return nil
}

func (d *Dispatcher) handleOne(ctx context.Context, adapter Adapter, record Record) (err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("adapter panic: %v", recovered)
		}
	}()
	return adapter.HandleGovernanceRecord(ctx, record)
}

func Project(event eventbus.Event) (Record, bool) {
	kind, ok := classifyEvent(event.Type)
	if !ok {
		return Record{}, false
	}
	governance := governanceFromEvent(event)
	record := Record{
		Kind:             kind,
		EventID:          strings.TrimSpace(event.ID),
		EventType:        event.Type,
		RunID:            strings.TrimSpace(event.RunID),
		SessionID:        strings.TrimSpace(event.SessionID),
		Time:             event.Time,
		Severity:         eventSeverity(event),
		Summary:          eventSummary(event, governance),
		SecurityCategory: securityCategory(event.Type),
		Governance:       governance,
		Attrs:            supportmaps.Clone(event.Attrs),
	}
	return record.Normalized(), true
}

func eventSeverity(event eventbus.Event) string {
	if payload, ok := event.SecurityFindingPayload(); ok {
		return strings.TrimSpace(payload.Severity)
	}
	if payload, ok := event.SecurityRiskDetectedPayload(); ok {
		return strings.TrimSpace(payload.Severity)
	}
	return ""
}

func classifyEvent(eventType eventbus.EventType) (Kind, bool) {
	switch eventType {
	case eventbus.EventApprovalRequested:
		return KindApprovalRequested, true
	case eventbus.EventApprovalResolved:
		return KindApprovalResolved, true
	case eventbus.EventApprovalTimedOut:
		return KindApprovalTimedOut, true
	case eventbus.EventApprovalGraceWarning:
		return KindApprovalGraceWarning, true
	default:
		if strings.HasPrefix(string(eventType), "security.") {
			return KindSecurityEvent, true
		}
		return "", false
	}
}

func securityCategory(eventType eventbus.EventType) string {
	if !strings.HasPrefix(string(eventType), "security.") {
		return ""
	}
	return strings.TrimSpace(strings.TrimPrefix(string(eventType), "security."))
}

func governanceFromEvent(event eventbus.Event) GovernanceContext {
	return domaingov.EventContextFromEvent(event.Type, event.Attrs)
}

func eventSummary(event eventbus.Event, governance GovernanceContext) string {
	return domaingov.EventSummary(event, governance)
}

func isNilAdapter(adapter Adapter) bool {
	if adapter == nil {
		return true
	}
	value := reflect.ValueOf(adapter)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return value.IsNil()
	default:
		return false
	}
}

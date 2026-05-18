package runtime

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/fulcrus/hopclaw/agent"
	"github.com/fulcrus/hopclaw/eventbus"
	"github.com/fulcrus/hopclaw/internal/support/normalize"
	"github.com/fulcrus/hopclaw/resultmodel"
)

type EventClass string

const (
	EventClassEvidence EventClass = "evidence"
	EventClassAudit    EventClass = "audit"
	EventClassDelivery EventClass = "delivery"
)

// LedgerEvent is the normalized event truth row used by runtime projections.
type LedgerEvent struct {
	ID         string             `json:"id"`
	EventClass EventClass         `json:"event_class"`
	Type       eventbus.EventType `json:"type"`
	RunID      string             `json:"run_id,omitempty"`
	SessionID  string             `json:"session_id,omitempty"`
	Time       time.Time          `json:"time,omitempty"`
	Summary    string             `json:"summary,omitempty"`
	Attrs      map[string]any     `json:"attrs,omitempty"`
}

// EventLedger is the unified evidence/audit/delivery ledger for a run.
type EventLedger struct {
	RunID     string        `json:"run_id"`
	SessionID string        `json:"session_id,omitempty"`
	Events    []LedgerEvent `json:"events,omitempty"`
}

func (s *Service) GetRunEventLedger(ctx context.Context, id string) (*EventLedger, error) {
	run, err := s.GetRun(ctx, id)
	if err != nil {
		return nil, err
	}
	return buildRunEventLedger(run, s.EventSnapshotContext(ctx)), nil
}

func buildRunEventLedger(run *agent.Run, events []eventbus.Event) *EventLedger {
	if run == nil {
		return nil
	}
	entries := make([]LedgerEvent, 0, len(events))
	for _, event := range events {
		if strings.TrimSpace(run.ID) != "" && strings.TrimSpace(event.RunID) != strings.TrimSpace(run.ID) {
			continue
		}
		entries = append(entries, projectLedgerEvent(event))
	}
	if len(entries) == 0 {
		return nil
	}
	return &EventLedger{
		RunID:     strings.TrimSpace(run.ID),
		SessionID: strings.TrimSpace(run.SessionID),
		Events:    entries,
	}
}

func projectLedgerEvent(event eventbus.Event) LedgerEvent {
	return LedgerEvent{
		ID:         strings.TrimSpace(event.ID),
		EventClass: classifyEventClass(event),
		Type:       event.Type,
		RunID:      strings.TrimSpace(event.RunID),
		SessionID:  strings.TrimSpace(event.SessionID),
		Time:       event.Time.UTC(),
		Summary:    ledgerEventSummary(event),
		Attrs:      cloneMetadata(event.Attrs),
	}
}

func classifyEventClass(event eventbus.Event) EventClass {
	switch {
	case strings.HasPrefix(string(event.Type), "governance.delivery."):
		return EventClassDelivery
	case isEvidenceEventType(event.Type):
		return EventClassEvidence
	default:
		return EventClassAudit
	}
}

func isEvidenceEventType(eventType eventbus.EventType) bool {
	switch eventType {
	case eventbus.EventToolExecuted,
		eventbus.EventPlanTaskStarted,
		eventbus.EventPlanTaskCompleted,
		eventbus.EventPlanTaskFailed,
		eventbus.EventPlanTaskCancelled,
		eventbus.EventPlanTaskSkipped,
		eventbus.EventPlanSnapshotUpdated,
		eventbus.EventRunPlanned,
		eventbus.EventArtifactPruned:
		return true
	default:
		return false
	}
}

func ledgerEventSummary(event eventbus.Event) string {
	if payload, ok := event.RunStatusPayload(); ok && strings.TrimSpace(payload.Summary) != "" {
		return payload.Summary
	}
	if payload, ok := event.RunTimeoutPayload(); ok && strings.TrimSpace(payload.Error) != "" {
		return payload.Error
	}
	if event.Type == eventbus.EventToolExecuted {
		if summary := toolSummaryFromEventAttrs(event.Attrs); summary != "" {
			return summary
		}
		toolName := ""
		if payload, ok := event.ToolExecutedPayload(); ok {
			if len(payload.ToolNames) > 0 {
				toolName = strings.Join(payload.ToolNames, ", ")
			}
			if toolName == "" && len(payload.Results) > 0 {
				toolName = strings.TrimSpace(payload.Results[0].ToolName)
			}
		}
		if toolName != "" {
			return fmt.Sprintf("tool evidence: %s", toolName)
		}
	}
	if strings.HasPrefix(string(event.Type), "governance.delivery.") {
		parts := make([]string, 0, 3)
		if payload, ok := event.GovernanceDeliveryPayload(); ok {
			if adapter := strings.TrimSpace(payload.AdapterName); adapter != "" {
				parts = append(parts, adapter)
			}
			if status := strings.TrimSpace(payload.DeliveryStatus); status != "" {
				parts = append(parts, status)
			}
			if sourceType := strings.TrimSpace(payload.SourceEventType); sourceType != "" {
				parts = append(parts, sourceType)
			}
		}
		if len(parts) > 0 {
			return strings.Join(parts, " ")
		}
		return ""
	}
	if receipt := governanceReceiptFromEvent(event); receipt != nil {
		if summary := strings.TrimSpace(eventSummary(event, receipt)); summary != "" {
			return summary
		}
	}
	return strings.TrimSpace(string(event.Type))
}

func toolSummaryFromEventAttrs(attrs map[string]any) string {
	for _, raw := range eventResults(attrs["results"]) {
		fields, ok := raw.(map[string]any)
		if !ok || len(fields) == 0 {
			continue
		}
		if result, ok := resultmodel.DecodeToolResultMetadata(map[string]any{
			resultmodel.MetadataKeyToolResult: fields[resultmodel.MetadataKeyToolResult],
		}); ok {
			normalized := result.Normalized()
			for _, text := range []string{normalized.Summary, normalized.TranscriptText, normalized.Content} {
				if summary := compactSummary(text); summary != "" {
					return summary
				}
			}
			continue
		}
		for _, key := range []string{"summary", "transcript_text", "content"} {
			if summary := compactSummary(normalize.String(fields[key])); summary != "" {
				return summary
			}
		}
	}
	return ""
}

func collectToolResultsFromLedger(ledger *EventLedger) []resultmodel.ToolResult {
	if ledger == nil || len(ledger.Events) == 0 {
		return nil
	}
	out := make([]resultmodel.ToolResult, 0)
	for _, item := range ledger.Events {
		if item.EventClass != EventClassEvidence || item.Type != eventbus.EventToolExecuted {
			continue
		}
		for _, result := range collectToolResultsFromEvents([]eventbus.Event{{
			Type:  item.Type,
			RunID: item.RunID,
			Attrs: item.Attrs,
		}}, item.RunID) {
			out = append(out, result)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func collectDeliverablesFromLedger(ledger *EventLedger) []DeliverableRef {
	if ledger == nil || len(ledger.Events) == 0 {
		return nil
	}
	out := make([]DeliverableRef, 0)
	for _, item := range ledger.Events {
		if item.EventClass != EventClassEvidence || item.Type != eventbus.EventToolExecuted {
			continue
		}
		payload, ok := (eventbus.Event{
			Type:  item.Type,
			RunID: item.RunID,
			Attrs: item.Attrs,
		}).ToolExecutedPayload()
		if !ok {
			continue
		}
		toolName := toolNameFromToolExecutedPayload(payload)
		for _, uri := range collectArtifactURIs(payload.ToMap()) {
			out = append(out, DeliverableRef{
				Kind:     "artifact",
				Name:     deliverableNameFromURI(uri, ""),
				URI:      uri,
				ToolName: toolName,
			})
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func toolNameFromToolExecutedPayload(payload eventbus.ToolExecutedPayload) string {
	for _, result := range payload.Results {
		if toolName := strings.TrimSpace(result.ToolName); toolName != "" {
			return toolName
		}
	}
	for _, toolName := range payload.ToolNames {
		if toolName = strings.TrimSpace(toolName); toolName != "" {
			return toolName
		}
	}
	return ""
}

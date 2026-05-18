package runtime

import (
	"strings"

	"github.com/fulcrus/hopclaw/eventbus"
)

type EventFilter struct {
	Type      eventbus.EventType `json:"type,omitempty"`
	RunID     string             `json:"run_id,omitempty"`
	SessionID string             `json:"session_id,omitempty"`
}

func (f EventFilter) Normalize() EventFilter {
	return EventFilter{
		Type:      eventbus.EventType(strings.TrimSpace(string(f.Type))),
		RunID:     strings.TrimSpace(f.RunID),
		SessionID: strings.TrimSpace(f.SessionID),
	}
}

func (f EventFilter) IsZero() bool {
	normalized := f.Normalize()
	return normalized.Type == "" && normalized.RunID == "" && normalized.SessionID == ""
}

func (f EventFilter) Matches(event eventbus.Event) bool {
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
	return true
}

func FilterEvents(events []eventbus.Event, filter EventFilter) []eventbus.Event {
	if len(events) == 0 {
		return nil
	}
	normalized := filter.Normalize()
	if normalized.IsZero() {
		return append([]eventbus.Event(nil), events...)
	}
	filtered := make([]eventbus.Event, 0, len(events))
	for _, event := range events {
		if !normalized.Matches(event) {
			continue
		}
		filtered = append(filtered, event)
	}
	if len(filtered) == 0 {
		return nil
	}
	return filtered
}

func headLimitEvents(events []eventbus.Event, limit int) []eventbus.Event {
	if limit <= 0 || len(events) <= limit {
		return events
	}
	return events[:limit]
}

func tailLimitEvents(events []eventbus.Event, limit int) []eventbus.Event {
	if limit <= 0 || len(events) <= limit {
		return events
	}
	return events[len(events)-limit:]
}

func lastFilteredEventID(events []eventbus.Event) string {
	if len(events) == 0 {
		return ""
	}
	return strings.TrimSpace(events[len(events)-1].ID)
}

func (s *Service) EventSnapshotFiltered(filter EventFilter, limit int) []eventbus.Event {
	items := FilterEvents(s.EventSnapshot(), filter)
	return tailLimitEvents(items, limit)
}

func (s *Service) EventsSinceFiltered(sinceID string, filter EventFilter, limit int) eventbus.CursorResult {
	normalized := filter.Normalize()
	if normalized.IsZero() {
		return s.EventsSince(strings.TrimSpace(sinceID), limit)
	}
	result := s.EventsSince(strings.TrimSpace(sinceID), 0)
	result.Events = headLimitEvents(FilterEvents(result.Events, normalized), limit)
	result.NextCursor = lastFilteredEventID(result.Events)
	return result
}

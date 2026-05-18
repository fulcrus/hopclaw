package toolruntime

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/fulcrus/hopclaw/agent"
)

func TestCalendarCreateICS(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	builtins := NewBuiltins(BuiltinsConfig{
		Root:         root,
		MaxReadBytes: 1024 * 64,
	})

	results, err := builtins.ExecuteBatch(context.Background(), &agent.Run{ID: "run-1"}, &agent.Session{ID: "sess-1"}, []agent.ToolCall{{
		ID:   "call-create-ics",
		Name: "calendar.create_ics",
		Input: map[string]any{
			"path": "meetings.ics",
			"events": []any{
				map[string]any{
					"summary":     "Team Standup",
					"start":       "2026-03-15T10:00:00Z",
					"end":         "2026-03-15T10:30:00Z",
					"description": "Daily standup meeting",
					"location":    "Room 42",
					"status":      "CONFIRMED",
					"organizer":   "mailto:boss@example.com",
					"attendees":   []any{"mailto:dev@example.com", "mailto:qa@example.com"},
				},
			},
		},
	}})
	if err != nil {
		t.Fatalf("ExecuteBatch(calendar.create_ics) error = %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}

	var payload struct {
		Path       string `json:"path"`
		EventCount int    `json:"event_count"`
		Bytes      int    `json:"bytes"`
	}
	if err := json.Unmarshal([]byte(results[0].Content), &payload); err != nil {
		t.Fatalf("json.Unmarshal error = %v\nraw: %s", err, results[0].Content)
	}
	if payload.EventCount != 1 {
		t.Fatalf("expected event_count=1, got %d", payload.EventCount)
	}
	if payload.Bytes <= 0 {
		t.Fatalf("expected bytes > 0, got %d", payload.Bytes)
	}

	// Verify file exists and contains ICS markers.
	data, err := os.ReadFile(filepath.Join(root, "meetings.ics"))
	if err != nil {
		t.Fatalf("ReadFile error = %v", err)
	}
	content := string(data)
	for _, marker := range []string{"BEGIN:VCALENDAR", "BEGIN:VEVENT", "SUMMARY:Team Standup", "LOCATION:Room 42", "END:VEVENT", "END:VCALENDAR"} {
		if !contains(content, marker) {
			t.Errorf("ICS file missing %q", marker)
		}
	}
}

func TestCalendarParseICS(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	builtins := NewBuiltins(BuiltinsConfig{
		Root:         root,
		MaxReadBytes: 1024 * 64,
	})

	// First create an ICS file.
	_, err := builtins.ExecuteBatch(context.Background(), &agent.Run{ID: "run-1"}, &agent.Session{ID: "sess-1"}, []agent.ToolCall{{
		ID:   "call-create",
		Name: "calendar.create_ics",
		Input: map[string]any{
			"path": "test.ics",
			"events": []any{
				map[string]any{
					"summary":   "Design Review",
					"start":     "2026-04-01T14:00:00Z",
					"end":       "2026-04-01T15:00:00Z",
					"location":  "Conference Room A",
					"organizer": "mailto:lead@example.com",
				},
			},
		},
	}})
	if err != nil {
		t.Fatalf("create ICS error = %v", err)
	}

	// Now parse it.
	results, err := builtins.ExecuteBatch(context.Background(), &agent.Run{ID: "run-2"}, &agent.Session{ID: "sess-1"}, []agent.ToolCall{{
		ID:   "call-parse",
		Name: "calendar.parse_ics",
		Input: map[string]any{
			"path": "test.ics",
		},
	}})
	if err != nil {
		t.Fatalf("ExecuteBatch(calendar.parse_ics) error = %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}

	var payload struct {
		Path       string           `json:"path"`
		Events     []map[string]any `json:"events"`
		EventCount int              `json:"event_count"`
	}
	if err := json.Unmarshal([]byte(results[0].Content), &payload); err != nil {
		t.Fatalf("json.Unmarshal error = %v\nraw: %s", err, results[0].Content)
	}
	if payload.EventCount != 1 {
		t.Fatalf("expected event_count=1, got %d", payload.EventCount)
	}
	if len(payload.Events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(payload.Events))
	}

	ev := payload.Events[0]
	if ev["summary"] != "Design Review" {
		t.Errorf("expected summary 'Design Review', got %q", ev["summary"])
	}
	if ev["location"] != "Conference Room A" {
		t.Errorf("expected location 'Conference Room A', got %q", ev["location"])
	}
}

func TestCalendarParseICSMultipleEvents(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	builtins := NewBuiltins(BuiltinsConfig{
		Root:         root,
		MaxReadBytes: 1024 * 64,
	})

	// Create ICS with multiple events.
	_, err := builtins.ExecuteBatch(context.Background(), &agent.Run{ID: "run-1"}, &agent.Session{ID: "sess-1"}, []agent.ToolCall{{
		ID:   "call-create-multi",
		Name: "calendar.create_ics",
		Input: map[string]any{
			"path": "multi.ics",
			"events": []any{
				map[string]any{
					"summary": "Morning Standup",
					"start":   "2026-03-20T09:00:00Z",
					"end":     "2026-03-20T09:15:00Z",
					"status":  "CONFIRMED",
				},
				map[string]any{
					"summary":     "Sprint Planning",
					"start":       "2026-03-20T10:00:00Z",
					"end":         "2026-03-20T12:00:00Z",
					"description": "Plan next sprint",
				},
				map[string]any{
					"summary":  "Lunch",
					"start":    "2026-03-20T12:00:00Z",
					"end":      "2026-03-20T13:00:00Z",
					"location": "Cafeteria",
				},
			},
		},
	}})
	if err != nil {
		t.Fatalf("create multi ICS error = %v", err)
	}

	// Parse the multi-event file.
	results, err := builtins.ExecuteBatch(context.Background(), &agent.Run{ID: "run-2"}, &agent.Session{ID: "sess-1"}, []agent.ToolCall{{
		ID:   "call-parse-multi",
		Name: "calendar.parse_ics",
		Input: map[string]any{
			"path": "multi.ics",
		},
	}})
	if err != nil {
		t.Fatalf("parse multi ICS error = %v", err)
	}

	var payload struct {
		Events     []map[string]any `json:"events"`
		EventCount int              `json:"event_count"`
	}
	if err := json.Unmarshal([]byte(results[0].Content), &payload); err != nil {
		t.Fatalf("json.Unmarshal error = %v\nraw: %s", err, results[0].Content)
	}
	if payload.EventCount != 3 {
		t.Fatalf("expected event_count=3, got %d", payload.EventCount)
	}

	// Verify all three events are present.
	summaries := make(map[string]bool)
	for _, ev := range payload.Events {
		if s, ok := ev["summary"].(string); ok {
			summaries[s] = true
		}
	}
	for _, expected := range []string{"Morning Standup", "Sprint Planning", "Lunch"} {
		if !summaries[expected] {
			t.Errorf("missing event with summary %q", expected)
		}
	}
}

// contains checks if s contains substr (helper for test assertions).
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsStr(s, substr))
}

func containsStr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

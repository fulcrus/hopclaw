// Package calendar implements calendar ICS tool handlers (calendar.parse_ics,
// calendar.create_ics) for the toolruntime registry.
package calendar

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/fulcrus/hopclaw/agent"
	"github.com/fulcrus/hopclaw/contextengine"
	"github.com/fulcrus/hopclaw/skill"
)

// Runtime is the narrow interface that calendar handlers need from *Builtins.
type Runtime interface {
	JSONResult(call agent.ToolCall, payload map[string]any) (contextengine.ToolResult, error)
	ResolvePath(input string) (string, error)
	DisplayPath(absPath string) string
}

// Handler is the tool handler signature for calendar tools.
type Handler func(ctx context.Context, rt Runtime, call agent.ToolCall) (contextengine.ToolResult, error)

// ToolDef pairs a tool manifest with a calendar handler.
type ToolDef struct {
	Manifest skill.ToolManifest
	Handler  Handler
}

// ToolDefs returns all calendar domain tool definitions.
func ToolDefs() []ToolDef {
	return []ToolDef{
		{
			Manifest: skill.ToolManifest{
				Name:            "calendar.parse_ics",
				Description:     "Parse an ICS file and extract calendar events.",
				InputSchema:     calendarParseICSInputSchema(),
				OutputSchema:    calendarParseICSOutputSchema(),
				SideEffectClass: "read",
				Idempotent:      true,
			},
			Handler: handleCalendarParseICS,
		},
		{
			Manifest: skill.ToolManifest{
				Name:             "calendar.create_ics",
				Description:      "Create an ICS calendar file with one or more events.",
				InputSchema:      calendarCreateICSInputSchema(),
				OutputSchema:     calendarCreateICSOutputSchema(),
				SideEffectClass:  "local_write",
				RequiresApproval: true,
			},
			Handler: handleCalendarCreateICS,
		},
	}
}

// ---------------------------------------------------------------------------
// Param helpers — duplicated locally to avoid importing toolruntime.
// ---------------------------------------------------------------------------

func stringFrom(value any) (string, error) {
	switch typed := value.(type) {
	case nil:
		return "", nil
	case string:
		return typed, nil
	default:
		return "", fmt.Errorf("expected string, got %T", value)
	}
}

func requiredString(input map[string]any, key string) (string, error) {
	value, err := stringFrom(input[key])
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(value) == "" {
		return "", fmt.Errorf("%s is required", key)
	}
	return value, nil
}

// ---------------------------------------------------------------------------
// Schema helpers — duplicated locally to avoid importing toolruntime.
// ---------------------------------------------------------------------------

func stringSchema(description string) map[string]any {
	schema := map[string]any{"type": "string"}
	if description != "" {
		schema["description"] = description
	}
	return schema
}

func integerSchema(description string) map[string]any {
	schema := map[string]any{"type": "integer"}
	if description != "" {
		schema["description"] = description
	}
	return schema
}

func objectSchema(properties map[string]any, required ...string) map[string]any {
	schema := map[string]any{
		"type":                 "object",
		"properties":           properties,
		"additionalProperties": false,
	}
	if len(required) > 0 {
		schema["required"] = required
	}
	return schema
}

func stringArraySchema(description string) map[string]any {
	schema := map[string]any{
		"type":  "array",
		"items": map[string]any{"type": "string"},
	}
	if description != "" {
		schema["description"] = description
	}
	return schema
}

func arraySchema(items map[string]any, description string) map[string]any {
	schema := map[string]any{
		"type":  "array",
		"items": items,
	}
	if description != "" {
		schema["description"] = description
	}
	return schema
}

// ---------------------------------------------------------------------------
// Input / Output schemas
// ---------------------------------------------------------------------------

func calendarParseICSInputSchema() map[string]any {
	return objectSchema(map[string]any{
		"path": stringSchema("Path to the .ics file to parse (relative to workspace root)."),
	}, "path")
}

func calendarParseICSOutputSchema() map[string]any {
	eventObj := objectSchema(map[string]any{
		"uid":         stringSchema("Unique event identifier."),
		"summary":     stringSchema("Event summary/title."),
		"description": stringSchema("Event description."),
		"location":    stringSchema("Event location."),
		"start":       stringSchema("Event start date/time (ISO 8601)."),
		"end":         stringSchema("Event end date/time (ISO 8601)."),
		"status":      stringSchema("Event status (CONFIRMED, TENTATIVE, CANCELLED)."),
		"organizer":   stringSchema("Event organizer."),
		"attendees":   stringArraySchema("Event attendees."),
	}, "uid", "summary", "start")
	return objectSchema(map[string]any{
		"path":        stringSchema("Path to the parsed file."),
		"events":      arraySchema(eventObj, "Extracted events."),
		"event_count": integerSchema("Number of events extracted."),
	}, "path", "events", "event_count")
}

func calendarCreateICSInputSchema() map[string]any {
	eventObj := objectSchema(map[string]any{
		"summary":     stringSchema("Event summary/title."),
		"start":       stringSchema("Event start date/time (ISO 8601, e.g. 2026-03-15T10:00:00Z)."),
		"end":         stringSchema("Event end date/time (ISO 8601)."),
		"description": stringSchema("Optional event description."),
		"location":    stringSchema("Optional event location."),
		"status":      stringSchema("Optional event status (CONFIRMED, TENTATIVE, CANCELLED)."),
		"organizer":   stringSchema("Optional organizer (e.g. mailto:boss@example.com)."),
		"attendees": arraySchema(
			map[string]any{"type": "string"},
			"Optional list of attendees (e.g. mailto:dev@example.com).",
		),
	}, "summary", "start", "end")
	return objectSchema(map[string]any{
		"path":   stringSchema("Destination file path for the .ics file (relative to workspace root)."),
		"events": arraySchema(eventObj, "Events to include in the calendar file."),
	}, "path", "events")
}

func calendarCreateICSOutputSchema() map[string]any {
	return objectSchema(map[string]any{
		"path":        stringSchema("Path to the created file."),
		"event_count": integerSchema("Number of events written."),
		"bytes":       integerSchema("File size in bytes."),
	}, "path", "event_count", "bytes")
}

// ---------------------------------------------------------------------------
// Handlers
// ---------------------------------------------------------------------------

func handleCalendarParseICS(_ context.Context, rt Runtime, call agent.ToolCall) (contextengine.ToolResult, error) {
	relPath, err := requiredString(call.Input, "path")
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("calendar.parse_ics: %w", err)
	}
	absPath, err := rt.ResolvePath(relPath)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("calendar.parse_ics: %w", err)
	}

	f, err := os.Open(absPath)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("calendar.parse_ics: %w", err)
	}
	defer f.Close()

	events, err := parseICSEvents(f)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("calendar.parse_ics: %w", err)
	}

	return rt.JSONResult(call, map[string]any{
		"path":        rt.DisplayPath(absPath),
		"events":      events,
		"event_count": len(events),
	})
}

// HandleCalendarCreateICS is the exported entry point for calendar.create_ics,
// used by root-package code (e.g. semantic.deliver nested calls).
func HandleCalendarCreateICS(ctx context.Context, rt Runtime, call agent.ToolCall) (contextengine.ToolResult, error) {
	return handleCalendarCreateICS(ctx, rt, call)
}

func handleCalendarCreateICS(_ context.Context, rt Runtime, call agent.ToolCall) (contextengine.ToolResult, error) {
	relPath, err := requiredString(call.Input, "path")
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("calendar.create_ics: %w", err)
	}
	absPath, err := rt.ResolvePath(relPath)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("calendar.create_ics: %w", err)
	}

	rawEvents, ok := call.Input["events"].([]any)
	if !ok || len(rawEvents) == 0 {
		return contextengine.ToolResult{}, fmt.Errorf("calendar.create_ics: events is required and must be a non-empty array")
	}

	var sb strings.Builder
	sb.WriteString("BEGIN:VCALENDAR\r\n")
	sb.WriteString("VERSION:2.0\r\n")
	sb.WriteString("PRODID:-//HopClaw//EN\r\n")

	for i, raw := range rawEvents {
		ev, ok := raw.(map[string]any)
		if !ok {
			return contextengine.ToolResult{}, fmt.Errorf("calendar.create_ics: event[%d] is not an object", i)
		}
		icsEvent, err := buildICSEvent(ev, i)
		if err != nil {
			return contextengine.ToolResult{}, fmt.Errorf("calendar.create_ics: %w", err)
		}
		sb.WriteString(icsEvent)
	}

	sb.WriteString("END:VCALENDAR\r\n")

	// Ensure parent directory exists.
	if err := os.MkdirAll(filepath.Dir(absPath), 0o755); err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("calendar.create_ics: mkdir: %w", err)
	}

	content := sb.String()
	if err := os.WriteFile(absPath, []byte(content), 0o644); err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("calendar.create_ics: write: %w", err)
	}

	return rt.JSONResult(call, map[string]any{
		"path":        rt.DisplayPath(absPath),
		"event_count": len(rawEvents),
		"bytes":       len(content),
	})
}

// ---------------------------------------------------------------------------
// ICS parsing — line-based parser with folding support
// ---------------------------------------------------------------------------

type icsEvent struct {
	UID         string   `json:"uid"`
	Summary     string   `json:"summary"`
	Description string   `json:"description,omitempty"`
	Location    string   `json:"location,omitempty"`
	Start       string   `json:"start"`
	End         string   `json:"end,omitempty"`
	Status      string   `json:"status,omitempty"`
	Organizer   string   `json:"organizer,omitempty"`
	Attendees   []string `json:"attendees,omitempty"`
}

func parseICSEvents(r *os.File) ([]map[string]any, error) {
	scanner := bufio.NewScanner(r)
	var lines []string
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}

	// Unfold: lines starting with space or tab are continuations.
	unfolded := unfoldICSLines(lines)

	var events []map[string]any
	var current *icsEvent
	inEvent := false

	for _, line := range unfolded {
		upper := strings.ToUpper(line)
		if upper == "BEGIN:VEVENT" {
			inEvent = true
			current = &icsEvent{}
			continue
		}
		if upper == "END:VEVENT" {
			if inEvent && current != nil {
				events = append(events, icsEventToMap(current))
			}
			inEvent = false
			current = nil
			continue
		}
		if !inEvent || current == nil {
			continue
		}

		key, value := parseICSProperty(line)
		switch key {
		case "UID":
			current.UID = value
		case "SUMMARY":
			current.Summary = value
		case "DESCRIPTION":
			current.Description = value
		case "LOCATION":
			current.Location = value
		case "DTSTART":
			current.Start = normalizeICSDateTime(value)
		case "DTEND":
			current.End = normalizeICSDateTime(value)
		case "STATUS":
			current.Status = value
		case "ORGANIZER":
			current.Organizer = value
		case "ATTENDEE":
			current.Attendees = append(current.Attendees, value)
		}
	}

	return events, nil
}

// unfoldICSLines handles RFC 5545 line folding: continuation lines start with
// a single space or horizontal tab character.
func unfoldICSLines(lines []string) []string {
	var result []string
	for _, line := range lines {
		line = strings.TrimRight(line, "\r")
		if len(line) > 0 && (line[0] == ' ' || line[0] == '\t') {
			// Continuation of previous line.
			if len(result) > 0 {
				result[len(result)-1] += line[1:]
			}
		} else {
			result = append(result, line)
		}
	}
	return result
}

// parseICSProperty splits "KEY;PARAM=x:VALUE" into (KEY, VALUE).
// The key name is extracted before any parameters (delimited by ';').
func parseICSProperty(line string) (string, string) {
	colonIdx := strings.Index(line, ":")
	if colonIdx < 0 {
		return strings.ToUpper(line), ""
	}
	keyPart := line[:colonIdx]
	value := line[colonIdx+1:]

	// Strip parameters (e.g., DTSTART;TZID=America/New_York -> DTSTART)
	if semiIdx := strings.Index(keyPart, ";"); semiIdx >= 0 {
		keyPart = keyPart[:semiIdx]
	}
	return strings.ToUpper(strings.TrimSpace(keyPart)), value
}

// normalizeICSDateTime converts ICS datetime formats to ISO 8601.
// e.g., "20260315T100000Z" -> "2026-03-15T10:00:00Z"
func normalizeICSDateTime(value string) string {
	value = strings.TrimSpace(value)

	layouts := []string{
		"20060102T150405Z",
		"20060102T150405",
		"20060102",
	}
	for _, layout := range layouts {
		t, err := time.Parse(layout, value)
		if err == nil {
			if layout == "20060102" {
				return t.Format("2006-01-02")
			}
			return t.Format(time.RFC3339)
		}
	}
	return value
}

func icsEventToMap(ev *icsEvent) map[string]any {
	m := map[string]any{
		"uid":     ev.UID,
		"summary": ev.Summary,
		"start":   ev.Start,
	}
	if ev.Description != "" {
		m["description"] = ev.Description
	}
	if ev.Location != "" {
		m["location"] = ev.Location
	}
	if ev.End != "" {
		m["end"] = ev.End
	}
	if ev.Status != "" {
		m["status"] = ev.Status
	}
	if ev.Organizer != "" {
		m["organizer"] = ev.Organizer
	}
	if len(ev.Attendees) > 0 {
		m["attendees"] = ev.Attendees
	}
	return m
}

// ---------------------------------------------------------------------------
// ICS generation
// ---------------------------------------------------------------------------

func buildICSEvent(ev map[string]any, idx int) (string, error) {
	summary, _ := stringFrom(ev["summary"])
	if summary == "" {
		return "", fmt.Errorf("event[%d]: summary is required", idx)
	}
	start, _ := stringFrom(ev["start"])
	if start == "" {
		return "", fmt.Errorf("event[%d]: start is required", idx)
	}
	end, _ := stringFrom(ev["end"])
	if end == "" {
		return "", fmt.Errorf("event[%d]: end is required", idx)
	}

	uid := fmt.Sprintf("hopclaw-%d-%d@hopclaw", time.Now().UnixNano(), idx)

	var sb strings.Builder
	sb.WriteString("BEGIN:VEVENT\r\n")
	sb.WriteString(fmt.Sprintf("UID:%s\r\n", uid))
	sb.WriteString(fmt.Sprintf("DTSTAMP:%s\r\n", time.Now().UTC().Format("20060102T150405Z")))
	sb.WriteString(fmt.Sprintf("DTSTART:%s\r\n", toICSDateTime(start)))
	sb.WriteString(fmt.Sprintf("DTEND:%s\r\n", toICSDateTime(end)))
	sb.WriteString(fmt.Sprintf("SUMMARY:%s\r\n", summary))

	if desc, _ := stringFrom(ev["description"]); desc != "" {
		sb.WriteString(fmt.Sprintf("DESCRIPTION:%s\r\n", desc))
	}
	if loc, _ := stringFrom(ev["location"]); loc != "" {
		sb.WriteString(fmt.Sprintf("LOCATION:%s\r\n", loc))
	}
	if status, _ := stringFrom(ev["status"]); status != "" {
		sb.WriteString(fmt.Sprintf("STATUS:%s\r\n", strings.ToUpper(status)))
	}
	if org, _ := stringFrom(ev["organizer"]); org != "" {
		sb.WriteString(fmt.Sprintf("ORGANIZER:%s\r\n", org))
	}
	if attendees, ok := ev["attendees"].([]any); ok {
		for _, a := range attendees {
			if s, ok := a.(string); ok {
				sb.WriteString(fmt.Sprintf("ATTENDEE:%s\r\n", s))
			}
		}
	}

	sb.WriteString("END:VEVENT\r\n")
	return sb.String(), nil
}

// toICSDateTime converts ISO 8601 strings to ICS DTSTART format.
// e.g., "2026-03-15T10:00:00Z" -> "20260315T100000Z"
func toICSDateTime(value string) string {
	layouts := []string{
		time.RFC3339,
		"2006-01-02T15:04:05",
		"2006-01-02",
	}
	for _, layout := range layouts {
		t, err := time.Parse(layout, value)
		if err == nil {
			if layout == "2006-01-02" {
				return t.Format("20060102")
			}
			return t.Format("20060102T150405Z")
		}
	}
	return value
}

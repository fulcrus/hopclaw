package toolruntime

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/fulcrus/hopclaw/agent"
	"github.com/fulcrus/hopclaw/contextengine"
	"github.com/fulcrus/hopclaw/skill"

	ical "github.com/emersion/go-ical"
	"github.com/emersion/go-webdav"
	"github.com/emersion/go-webdav/caldav"
)

func init() {
	RegisterLayer2GroupToggle("calendar", "calendar")
}

// ---------------------------------------------------------------------------
// CalDAV Layer 2 tools — remote calendar access
// ---------------------------------------------------------------------------

func (r *Layer2Registry) registerCalendarGroup() {
	timeout := r.config.DefaultExecTimeout
	r.registerGroup("calendar", []string{}, []layer2ToolDef{
		{manifest: skill.ToolManifest{
			Name: "calendar.list_events", Description: "List events from a CalDAV calendar.",
			InputSchema: calendarListEventsSchema(), OutputSchema: calendarListEventsOutputSchema(),
			SideEffectClass: "read", Idempotent: true, Timeout: timeout,
		}, execFn: calendarExec},
		{manifest: skill.ToolManifest{
			Name: "calendar.create_event", Description: "Create a new event on a CalDAV calendar.",
			InputSchema: calendarCreateEventSchema(), OutputSchema: calendarCreateEventOutputSchema(),
			SideEffectClass: "external_write", RequiresApproval: true, Timeout: timeout,
		}, execFn: calendarExec},
		{manifest: skill.ToolManifest{
			Name: "calendar.update_event", Description: "Update an existing event on a CalDAV calendar.",
			InputSchema: calendarUpdateEventSchema(), OutputSchema: calendarUpdateEventOutputSchema(),
			SideEffectClass: "external_write", RequiresApproval: true, Timeout: timeout,
		}, execFn: calendarExec},
		{manifest: skill.ToolManifest{
			Name: "calendar.delete_event", Description: "Delete an event from a CalDAV calendar.",
			InputSchema: calendarDeleteEventSchema(), OutputSchema: calendarDeleteEventOutputSchema(),
			SideEffectClass: "external_write", RequiresApproval: true, Timeout: timeout,
		}, execFn: calendarExec},
	})
}

// ---------------------------------------------------------------------------
// Dispatcher
// ---------------------------------------------------------------------------

func calendarExec(ctx context.Context, _ *ws, config BuiltinsConfig, call agent.ToolCall) (contextengine.ToolResult, error) {
	svc := config.Services.Calendar
	if !svc.IsConfigured() {
		return notConfiguredResult(call, "tools.services.calendar")
	}
	switch call.Name {
	case "calendar.list_events":
		return calendarListEvents(ctx, svc, call)
	case "calendar.create_event":
		return calendarCreateEvent(ctx, svc, call)
	case "calendar.update_event":
		return calendarUpdateEvent(ctx, svc, call)
	case "calendar.delete_event":
		return calendarDeleteEvent(ctx, svc, call)
	default:
		return notConfiguredResult(call, "tools.services.calendar")
	}
}

// ---------------------------------------------------------------------------
// CalDAV client helpers
// ---------------------------------------------------------------------------

func newCalDAVClient(svc CalendarServiceConfig) (*caldav.Client, error) {
	httpClient := webdav.HTTPClientWithBasicAuth(nil, svc.Username, svc.Password)
	return caldav.NewClient(httpClient, svc.CalDAVURL)
}

// findCalendarPath discovers the first calendar path from the CalDAV server.
func findCalendarPath(ctx context.Context, client *caldav.Client) (string, error) {
	principal, err := client.FindCurrentUserPrincipal(ctx)
	if err != nil {
		return "", fmt.Errorf("find principal: %w", err)
	}
	homeSet, err := client.FindCalendarHomeSet(ctx, principal)
	if err != nil {
		return "", fmt.Errorf("find calendar home set: %w", err)
	}
	calendars, err := client.FindCalendars(ctx, homeSet)
	if err != nil {
		return "", fmt.Errorf("find calendars: %w", err)
	}
	if len(calendars) == 0 {
		return "", fmt.Errorf("no calendars found")
	}
	return calendars[0].Path, nil
}

func calendarJSONResult(call agent.ToolCall, payload map[string]any) (contextengine.ToolResult, error) {
	body, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return contextengine.ToolResult{}, err
	}
	return contextengine.ToolResult{ToolName: call.Name, ToolCallID: call.ID, Content: string(body)}, nil
}

// ---------------------------------------------------------------------------
// calendar.list_events
// ---------------------------------------------------------------------------

func calendarListEvents(ctx context.Context, svc CalendarServiceConfig, call agent.ToolCall) (contextengine.ToolResult, error) {
	client, err := newCalDAVClient(svc)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("calendar.list_events: %w", err)
	}
	calPath, err := findCalendarPath(ctx, client)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("calendar.list_events: %w", err)
	}

	// Parse optional time range.
	startStr, _ := stringFrom(call.Input["start"])
	endStr, _ := stringFrom(call.Input["end"])
	limit, _ := intFrom(call.Input["limit"], 100)

	start := time.Now().AddDate(0, -1, 0) // default: 1 month ago
	end := time.Now().AddDate(0, 3, 0)    // default: 3 months from now
	if startStr != "" {
		if t, err := time.Parse(time.RFC3339, startStr); err == nil {
			start = t
		} else if t, err := time.Parse("2006-01-02", startStr); err == nil {
			start = t
		}
	}
	if endStr != "" {
		if t, err := time.Parse(time.RFC3339, endStr); err == nil {
			end = t
		} else if t, err := time.Parse("2006-01-02", endStr); err == nil {
			end = t.Add(24*time.Hour - time.Second) // end of day
		}
	}

	query := &caldav.CalendarQuery{
		CompRequest: caldav.CalendarCompRequest{
			Name:  ical.CompCalendar,
			Props: []string{ical.PropVersion},
			Comps: []caldav.CalendarCompRequest{{
				Name:     ical.CompEvent,
				AllProps: true,
			}},
		},
		CompFilter: caldav.CompFilter{
			Name: ical.CompCalendar,
			Comps: []caldav.CompFilter{{
				Name:  ical.CompEvent,
				Start: start,
				End:   end,
			}},
		},
	}

	objects, err := client.QueryCalendar(ctx, calPath, query)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("calendar.list_events: query: %w", err)
	}

	var events []map[string]any
	for _, obj := range objects {
		if obj.Data == nil {
			continue
		}
		for _, ev := range obj.Data.Events() {
			events = append(events, extractEventProps(&ev))
		}
		if len(events) >= limit {
			events = events[:limit]
			break
		}
	}
	if events == nil {
		events = []map[string]any{}
	}

	return calendarJSONResult(call, map[string]any{
		"events": events,
		"count":  len(events),
	})
}

// extractEventProps extracts key properties from an ical.Event into a map.
func extractEventProps(ev *ical.Event) map[string]any {
	m := make(map[string]any)

	if uid, err := ev.Props.Text(ical.PropUID); err == nil {
		m["uid"] = uid
	}
	if summary, err := ev.Props.Text(ical.PropSummary); err == nil {
		m["summary"] = summary
	}
	if desc, err := ev.Props.Text(ical.PropDescription); err == nil && desc != "" {
		m["description"] = desc
	}
	if loc, err := ev.Props.Text(ical.PropLocation); err == nil && loc != "" {
		m["location"] = loc
	}
	if status, err := ev.Props.Text(ical.PropStatus); err == nil && status != "" {
		m["status"] = status
	}
	if start, err := ev.DateTimeStart(nil); err == nil {
		m["start"] = start.Format(time.RFC3339)
	}
	if end, err := ev.DateTimeEnd(nil); err == nil {
		m["end"] = end.Format(time.RFC3339)
	}
	if org, err := ev.Props.Text(ical.PropOrganizer); err == nil && org != "" {
		m["organizer"] = org
	}
	if attendeeProps := ev.Props[ical.PropAttendee]; len(attendeeProps) > 0 {
		attendees := make([]string, 0, len(attendeeProps))
		for _, p := range attendeeProps {
			attendees = append(attendees, p.Value)
		}
		m["attendees"] = attendees
	}

	return m
}

// ---------------------------------------------------------------------------
// calendar.create_event
// ---------------------------------------------------------------------------

func calendarCreateEvent(ctx context.Context, svc CalendarServiceConfig, call agent.ToolCall) (contextengine.ToolResult, error) {
	client, err := newCalDAVClient(svc)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("calendar.create_event: %w", err)
	}
	calPath, err := findCalendarPath(ctx, client)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("calendar.create_event: %w", err)
	}

	summary, err := requiredString(call.Input, "summary")
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("calendar.create_event: %w", err)
	}
	startStr, err := requiredString(call.Input, "start")
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("calendar.create_event: %w", err)
	}
	endStr, err := requiredString(call.Input, "end")
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("calendar.create_event: %w", err)
	}

	startTime, err := parseFlexibleTime(startStr)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("calendar.create_event: start: %w", err)
	}
	endTime, err := parseFlexibleTime(endStr)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("calendar.create_event: end: %w", err)
	}

	uid := fmt.Sprintf("hopclaw-%d@hopclaw", time.Now().UnixNano())

	event := ical.NewEvent()
	event.Props.SetText(ical.PropUID, uid)
	event.Props.SetDateTime(ical.PropDateTimeStamp, time.Now().UTC())
	event.Props.SetDateTime(ical.PropDateTimeStart, startTime)
	event.Props.SetDateTime(ical.PropDateTimeEnd, endTime)
	event.Props.SetText(ical.PropSummary, summary)

	if desc, _ := stringFrom(call.Input["description"]); desc != "" {
		event.Props.SetText(ical.PropDescription, desc)
	}
	if loc, _ := stringFrom(call.Input["location"]); loc != "" {
		event.Props.SetText(ical.PropLocation, loc)
	}
	if attendees, ok := call.Input["attendees"].([]any); ok {
		for _, a := range attendees {
			if s, ok := a.(string); ok {
				prop := ical.Prop{Name: ical.PropAttendee}
				prop.Value = s
				event.Props.Add(&prop)
			}
		}
	}

	cal := ical.NewCalendar()
	cal.Props.SetText(ical.PropVersion, "2.0")
	cal.Props.SetText(ical.PropProductID, "-//HopClaw//EN")
	cal.Children = append(cal.Children, event.Component)

	eventPath := strings.TrimSuffix(calPath, "/") + "/" + uid + ".ics"
	_, err = client.PutCalendarObject(ctx, eventPath, cal)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("calendar.create_event: put: %w", err)
	}

	return calendarJSONResult(call, map[string]any{
		"uid":     uid,
		"summary": summary,
		"created": true,
	})
}

// ---------------------------------------------------------------------------
// calendar.update_event
// ---------------------------------------------------------------------------

func calendarUpdateEvent(ctx context.Context, svc CalendarServiceConfig, call agent.ToolCall) (contextengine.ToolResult, error) {
	client, err := newCalDAVClient(svc)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("calendar.update_event: %w", err)
	}
	calPath, err := findCalendarPath(ctx, client)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("calendar.update_event: %w", err)
	}

	uid, err := requiredString(call.Input, "uid")
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("calendar.update_event: %w", err)
	}

	// Find the existing event by querying for its UID.
	existing, eventPath, err := findEventByUID(ctx, client, calPath, uid)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("calendar.update_event: %w", err)
	}

	// Apply updates.
	if summary, _ := stringFrom(call.Input["summary"]); summary != "" {
		existing.Props.SetText(ical.PropSummary, summary)
	}
	if desc, _ := stringFrom(call.Input["description"]); desc != "" {
		existing.Props.SetText(ical.PropDescription, desc)
	}
	if loc, _ := stringFrom(call.Input["location"]); loc != "" {
		existing.Props.SetText(ical.PropLocation, loc)
	}
	if startStr, _ := stringFrom(call.Input["start"]); startStr != "" {
		if t, err := parseFlexibleTime(startStr); err == nil {
			existing.Props.SetDateTime(ical.PropDateTimeStart, t)
		}
	}
	if endStr, _ := stringFrom(call.Input["end"]); endStr != "" {
		if t, err := parseFlexibleTime(endStr); err == nil {
			existing.Props.SetDateTime(ical.PropDateTimeEnd, t)
		}
	}

	// Update timestamp.
	existing.Props.SetDateTime(ical.PropDateTimeStamp, time.Now().UTC())

	// Re-wrap in calendar.
	cal := ical.NewCalendar()
	cal.Props.SetText(ical.PropVersion, "2.0")
	cal.Props.SetText(ical.PropProductID, "-//HopClaw//EN")
	cal.Children = append(cal.Children, existing.Component)

	_, err = client.PutCalendarObject(ctx, eventPath, cal)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("calendar.update_event: put: %w", err)
	}

	return calendarJSONResult(call, map[string]any{
		"uid":     uid,
		"updated": true,
	})
}

// ---------------------------------------------------------------------------
// calendar.delete_event
// ---------------------------------------------------------------------------

func calendarDeleteEvent(ctx context.Context, svc CalendarServiceConfig, call agent.ToolCall) (contextengine.ToolResult, error) {
	client, err := newCalDAVClient(svc)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("calendar.delete_event: %w", err)
	}
	calPath, err := findCalendarPath(ctx, client)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("calendar.delete_event: %w", err)
	}

	uid, err := requiredString(call.Input, "uid")
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("calendar.delete_event: %w", err)
	}

	_, eventPath, err := findEventByUID(ctx, client, calPath, uid)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("calendar.delete_event: %w", err)
	}

	if err := client.RemoveAll(ctx, eventPath); err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("calendar.delete_event: delete: %w", err)
	}

	return calendarJSONResult(call, map[string]any{
		"uid":     uid,
		"deleted": true,
	})
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// findEventByUID searches a CalDAV calendar for an event with the given UID.
// Returns the event, its server path, or an error.
func findEventByUID(ctx context.Context, client *caldav.Client, calPath, uid string) (*ical.Event, string, error) {
	query := &caldav.CalendarQuery{
		CompRequest: caldav.CalendarCompRequest{
			Name:  ical.CompCalendar,
			Props: []string{ical.PropVersion},
			Comps: []caldav.CalendarCompRequest{{
				Name:     ical.CompEvent,
				AllProps: true,
			}},
		},
		CompFilter: caldav.CompFilter{
			Name: ical.CompCalendar,
			Comps: []caldav.CompFilter{{
				Name: ical.CompEvent,
				Props: []caldav.PropFilter{{
					Name:      ical.PropUID,
					TextMatch: &caldav.TextMatch{Text: uid},
				}},
			}},
		},
	}

	objects, err := client.QueryCalendar(ctx, calPath, query)
	if err != nil {
		return nil, "", fmt.Errorf("query for uid %q: %w", uid, err)
	}
	for _, obj := range objects {
		if obj.Data == nil {
			continue
		}
		for _, ev := range obj.Data.Events() {
			evUID, _ := ev.Props.Text(ical.PropUID)
			if evUID == uid {
				return &ev, obj.Path, nil
			}
		}
	}
	return nil, "", fmt.Errorf("event with uid %q not found", uid)
}

// parseFlexibleTime parses ISO 8601 date or datetime strings.
func parseFlexibleTime(s string) (time.Time, error) {
	layouts := []string{
		time.RFC3339,
		"2006-01-02T15:04:05",
		"2006-01-02",
	}
	for _, layout := range layouts {
		if t, err := time.Parse(layout, s); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("could not parse time %q", s)
}

// ---------------------------------------------------------------------------
// Schemas
// ---------------------------------------------------------------------------

func calendarListEventsSchema() map[string]any {
	return objectSchema(map[string]any{
		"start": stringSchema("Start of time range (ISO date or datetime). Default: 1 month ago."),
		"end":   stringSchema("End of time range (ISO date or datetime). Default: 3 months from now."),
		"limit": integerSchema("Maximum number of events to return. Default: 100."),
	})
}

func calendarListEventsOutputSchema() map[string]any {
	eventObj := objectSchema(map[string]any{
		"uid":         stringSchema("Event unique identifier."),
		"summary":     stringSchema("Event summary."),
		"description": stringSchema("Event description."),
		"location":    stringSchema("Event location."),
		"start":       stringSchema("Event start (RFC3339)."),
		"end":         stringSchema("Event end (RFC3339)."),
		"status":      stringSchema("Event status."),
		"organizer":   stringSchema("Event organizer."),
		"attendees":   stringArraySchema("Event attendees."),
	}, "uid", "summary")
	return objectSchema(map[string]any{
		"events": arraySchema(eventObj, "Calendar events."),
		"count":  integerSchema("Number of events returned."),
	}, "events", "count")
}

func calendarCreateEventSchema() map[string]any {
	return objectSchema(map[string]any{
		"summary":     stringSchema("Event title/summary."),
		"start":       stringSchema("Start date/time (ISO 8601)."),
		"end":         stringSchema("End date/time (ISO 8601)."),
		"description": stringSchema("Optional event description."),
		"location":    stringSchema("Optional event location."),
		"attendees": arraySchema(
			map[string]any{"type": "string"},
			"Optional list of attendee URIs (e.g. mailto:dev@example.com).",
		),
	}, "summary", "start", "end")
}

func calendarCreateEventOutputSchema() map[string]any {
	return objectSchema(map[string]any{
		"uid":     stringSchema("Created event UID."),
		"summary": stringSchema("Event summary."),
		"created": booleanSchema("Whether the event was created."),
	}, "uid", "summary", "created")
}

func calendarUpdateEventSchema() map[string]any {
	return objectSchema(map[string]any{
		"uid":         stringSchema("UID of the event to update."),
		"summary":     stringSchema("New event title."),
		"start":       stringSchema("New start date/time (ISO 8601)."),
		"end":         stringSchema("New end date/time (ISO 8601)."),
		"description": stringSchema("New event description."),
		"location":    stringSchema("New event location."),
	}, "uid")
}

func calendarUpdateEventOutputSchema() map[string]any {
	return objectSchema(map[string]any{
		"uid":     stringSchema("Updated event UID."),
		"updated": booleanSchema("Whether the event was updated."),
	}, "uid", "updated")
}

func calendarDeleteEventSchema() map[string]any {
	return objectSchema(map[string]any{
		"uid": stringSchema("UID of the event to delete."),
	}, "uid")
}

func calendarDeleteEventOutputSchema() map[string]any {
	return objectSchema(map[string]any{
		"uid":     stringSchema("Deleted event UID."),
		"deleted": booleanSchema("Whether the event was deleted."),
	}, "uid", "deleted")
}

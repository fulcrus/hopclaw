---
name: calendar
description: Work with calendar events using built-in CalDAV and ICS capabilities.
user-invocable: true
metadata:
  openclaw:
    skillKey: productivity.calendar
    emoji: "\U0001F4C5"
    always: false
---
# Calendar

Use the built-in calendar capabilities instead of raw HTTP requests or shell scripts.

Primary tools:

- `calendar.list_events` for reading events in a date range
- `calendar.create_event` for creating new events
- `calendar.update_event` for changing an existing event
- `calendar.delete_event` for removing an event
- `calendar.parse_ics` for inspecting an ICS file
- `calendar.create_ics` for generating an ICS file when the user wants a file artifact

Working rules:

- For read-only requests, default to a reasonable near-term range when the user did not specify one, and say which range you used.
- For create, update, or delete requests, confirm missing details such as title, date, time, timezone, or target calendar before taking action.
- Treat attendee emails, meeting links, locations, and descriptions as sensitive data.
- If remote calendar access is not configured, explain that the calendar service must be configured instead of improvising with external API examples.

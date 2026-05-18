package autoreply

import "strings"

// ---------------------------------------------------------------------------
// Template interpolation
// ---------------------------------------------------------------------------

// Template syntax: {{.FieldName}}
//
// Supported fields:
//   - SenderID, SenderName, Channel, SessionKey, Content
//   - Timestamp (RFC3339)
//   - Date (YYYY-MM-DD)
//   - Time (HH:MM)
//   - DayOfWeek (Monday, Tuesday, ...)
//
// Unknown placeholders are left as-is. Uses strings.NewReplacer for efficiency
// rather than text/template to avoid injection risks.

const (
	templateDateFormat = "2006-01-02"
	templateTimeFormat = "15:04"
)

func applyTemplate(tmpl string, ctx MessageContext) string {
	if tmpl == "" {
		return ""
	}

	r := strings.NewReplacer(
		"{{.SenderID}}", ctx.SenderID,
		"{{.SenderName}}", ctx.SenderName,
		"{{.Channel}}", ctx.Channel,
		"{{.SessionKey}}", ctx.SessionKey,
		"{{.Content}}", ctx.Content,
		"{{.ReceivedAt}}", ctx.ReceivedAt.Format(timeRFC3339),
		"{{.Date}}", ctx.ReceivedAt.Format(templateDateFormat),
		"{{.Time}}", ctx.ReceivedAt.Format(templateTimeFormat),
		"{{.DayOfWeek}}", ctx.ReceivedAt.Weekday().String(),
	)

	return r.Replace(tmpl)
}

// timeRFC3339 is the Go reference layout for RFC 3339 timestamps.
const timeRFC3339 = "2006-01-02T15:04:05Z07:00"

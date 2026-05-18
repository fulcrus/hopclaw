package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// ---------------------------------------------------------------------------
// Constants
// ---------------------------------------------------------------------------

const (
	eventsPollInterval   = 5 * time.Second
	eventsMaxItems       = 200
	eventsTypeWidth      = 24
	eventsTimeWidth      = 22
	eventsRunIDWidth     = 14
	eventsSessionIDWidth = 14
	eventsDataWidth      = 40
	eventsScrollPageSize = 10
)

// ---------------------------------------------------------------------------
// Messages
// ---------------------------------------------------------------------------

// eventsTickMsg triggers periodic event refresh.
type eventsTickMsg struct{}

// eventsFetchResultMsg carries the result of an events fetch.
type eventsFetchResultMsg struct {
	events *EventsResponse
	err    error
}

// ---------------------------------------------------------------------------
// EventsModel
// ---------------------------------------------------------------------------

// EventsModel is the bubbletea model for the Events tab.
type EventsModel struct {
	client    *Client
	events    *EventsResponse
	lastErr   string
	width     int
	height    int
	scrollPos int
}

// NewEventsModel creates a new EventsModel.
func NewEventsModel(client *Client) EventsModel {
	return EventsModel{
		client: client,
	}
}

// Init returns the initial command for the events model.
func (m EventsModel) Init() tea.Cmd {
	return tea.Batch(m.fetchEvents(), m.tickCmd())
}

// Update handles messages for the events model.
func (m EventsModel) Update(msg tea.Msg) (EventsModel, tea.Cmd) {
	switch msg := msg.(type) {
	case eventsTickMsg:
		return m, tea.Batch(m.fetchEvents(), m.tickCmd())

	case eventsFetchResultMsg:
		if msg.err != nil {
			m.lastErr = msg.err.Error()
			m.events = nil
		} else {
			m.lastErr = ""
			m.events = msg.events
		}
		return m, nil

	case tea.KeyMsg:
		switch msg.String() {
		case "r":
			return m, m.fetchEvents()
		case "up":
			if m.scrollPos > 0 {
				m.scrollPos--
			}
		case "down":
			m.scrollPos++
		case "pgup":
			m.scrollPos -= eventsScrollPageSize
			if m.scrollPos < 0 {
				m.scrollPos = 0
			}
		case "pgdown":
			m.scrollPos += eventsScrollPageSize
		}
	}
	return m, nil
}

// View renders the events tab.
func (m EventsModel) View() string {
	var b strings.Builder

	b.WriteString(titleStyle.Render("Events"))
	b.WriteString("\n\n")

	if m.lastErr != "" {
		b.WriteString(errorStyle.Render("error: " + m.lastErr))
		b.WriteString("\n\n")
		b.WriteString(helpStyle.Render("Press 'r' to retry"))
		return b.String()
	}

	if m.events == nil {
		b.WriteString(helpStyle.Render("Loading..."))
		return b.String()
	}

	if len(m.events.Items) == 0 {
		b.WriteString(helpStyle.Render("No events found"))
		b.WriteString("\n\n")
		b.WriteString(helpStyle.Render("Press 'r' to refresh"))
		return b.String()
	}

	// Header.
	header := tableHeaderStyle.Render(padRight("TYPE", eventsTypeWidth)) +
		tableHeaderStyle.Render(padRight("TIME", eventsTimeWidth)) +
		tableHeaderStyle.Render(padRight("RUN", eventsRunIDWidth)) +
		tableHeaderStyle.Render(padRight("SESSION", eventsSessionIDWidth)) +
		tableHeaderStyle.Render(padRight("DATA", eventsDataWidth))
	b.WriteString(header)
	b.WriteString("\n")

	// Rows (show most recent first).
	items := m.events.Items
	if len(items) > eventsMaxItems {
		items = items[len(items)-eventsMaxItems:]
	}

	// Reverse so newest is first.
	reversed := make([]EventItem, len(items))
	for i, item := range items {
		reversed[len(items)-1-i] = item
	}

	visibleHeight := m.height - 8
	if visibleHeight < 1 {
		visibleHeight = 1
	}

	start := m.scrollPos
	if start > len(reversed)-visibleHeight {
		start = len(reversed) - visibleHeight
	}
	if start < 0 {
		start = 0
	}
	end := start + visibleHeight
	if end > len(reversed) {
		end = len(reversed)
	}

	for _, item := range reversed[start:end] {
		timeStr := item.Time.Format(time.RFC3339)
		dataSummary := summarizeAttrs(item.Attrs)
		row := tableRowStyle.Render(padRight(item.Type, eventsTypeWidth)) +
			tableRowDimStyle.Render(padRight(timeStr, eventsTimeWidth)) +
			tableRowStyle.Render(padRight(item.RunID, eventsRunIDWidth)) +
			tableRowStyle.Render(padRight(item.SessionID, eventsSessionIDWidth)) +
			tableRowDimStyle.Render(padRight(dataSummary, eventsDataWidth))
		b.WriteString(row)
		b.WriteString("\n")
	}

	if len(reversed) > visibleHeight {
		b.WriteString(fmt.Sprintf("\n%s", helpStyle.Render(
			fmt.Sprintf("showing %d-%d of %d (scroll with up/down, pgup/pgdown)", start+1, end, len(reversed)),
		)))
	}

	b.WriteString("\n\n")
	b.WriteString(helpStyle.Render("Press 'r' to refresh"))

	return b.String()
}

// SetSize updates the terminal dimensions.
func (m *EventsModel) SetSize(w, h int) {
	m.width = w
	m.height = h
}

// ---------------------------------------------------------------------------
// Commands
// ---------------------------------------------------------------------------

func (m EventsModel) fetchEvents() tea.Cmd {
	client := m.client
	return func() tea.Msg {
		ctx := context.Background()
		events, err := client.GetEvents(ctx)
		if err != nil {
			return eventsFetchResultMsg{err: err}
		}
		return eventsFetchResultMsg{events: &events}
	}
}

func (m EventsModel) tickCmd() tea.Cmd {
	return tea.Tick(eventsPollInterval, func(_ time.Time) tea.Msg {
		return eventsTickMsg{}
	})
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// summarizeAttrs produces a brief string from event attributes.
func summarizeAttrs(attrs map[string]any) string {
	if len(attrs) == 0 {
		return ""
	}
	var parts []string
	for k, v := range attrs {
		s := fmt.Sprintf("%s=%v", k, v)
		parts = append(parts, s)
		// Keep it brief.
		if len(parts) >= 3 {
			break
		}
	}
	return strings.Join(parts, " ")
}

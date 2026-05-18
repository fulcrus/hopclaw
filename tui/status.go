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
	statusPollInterval    = 5 * time.Second
	statusMaxCapabilities = 50
	statusCapNameWidth    = 24
	statusCapKindWidth    = 16
	statusCapStatusWidth  = 12
	statusCapModelWidth   = 24
)

// ---------------------------------------------------------------------------
// Messages
// ---------------------------------------------------------------------------

// statusTickMsg triggers a periodic status refresh.
type statusTickMsg struct{}

// statusFetchResultMsg carries the result of a status + capabilities fetch.
type statusFetchResultMsg struct {
	status       *StatusResponse
	capabilities *CapabilitiesResponse
	err          error
}

// ---------------------------------------------------------------------------
// StatusModel
// ---------------------------------------------------------------------------

// StatusModel is the bubbletea model for the Status tab.
type StatusModel struct {
	client       *Client
	status       *StatusResponse
	capabilities *CapabilitiesResponse
	lastErr      string
	width        int
	height       int
	scrollPos    int
}

// NewStatusModel creates a new StatusModel.
func NewStatusModel(client *Client) StatusModel {
	return StatusModel{
		client: client,
	}
}

// Init returns the initial command for the status model.
func (m StatusModel) Init() tea.Cmd {
	return tea.Batch(m.fetchStatus(), m.tickCmd())
}

// Update handles messages for the status model.
func (m StatusModel) Update(msg tea.Msg) (StatusModel, tea.Cmd) {
	switch msg := msg.(type) {
	case statusTickMsg:
		return m, tea.Batch(m.fetchStatus(), m.tickCmd())

	case statusFetchResultMsg:
		if msg.err != nil {
			m.lastErr = msg.err.Error()
			m.status = nil
			m.capabilities = nil
		} else {
			m.lastErr = ""
			m.status = msg.status
			m.capabilities = msg.capabilities
		}
		return m, nil

	case tea.KeyMsg:
		switch msg.String() {
		case "r":
			return m, m.fetchStatus()
		case "up":
			if m.scrollPos > 0 {
				m.scrollPos--
			}
		case "down":
			m.scrollPos++
		}
	}
	return m, nil
}

// View renders the status tab.
func (m StatusModel) View() string {
	var b strings.Builder

	b.WriteString(titleStyle.Render("Gateway Status"))
	b.WriteString("\n\n")

	if m.lastErr != "" {
		b.WriteString(errorStyle.Render("error: " + m.lastErr))
		b.WriteString("\n\n")
		b.WriteString(helpStyle.Render("Press 'r' to retry"))
		return b.String()
	}

	if m.status == nil {
		b.WriteString(helpStyle.Render("Loading..."))
		return b.String()
	}

	// Status info.
	b.WriteString(fmt.Sprintf("  Version:      %s\n", m.status.Version))
	b.WriteString(fmt.Sprintf("  Uptime:       %s\n", m.status.Uptime))
	b.WriteString(fmt.Sprintf("  Capabilities: %d\n", m.status.CapabilityCount))
	b.WriteString("\n")

	// Capabilities table.
	if m.capabilities != nil && len(m.capabilities.Items) > 0 {
		b.WriteString(titleStyle.Render("Capabilities"))
		b.WriteString("\n\n")

		// Header.
		header := tableHeaderStyle.Render(padRight("NAME", statusCapNameWidth)) +
			tableHeaderStyle.Render(padRight("KIND", statusCapKindWidth)) +
			tableHeaderStyle.Render(padRight("STATUS", statusCapStatusWidth)) +
			tableHeaderStyle.Render(padRight("MODEL", statusCapModelWidth))
		b.WriteString(header)
		b.WriteString("\n")

		// Rows.
		items := m.capabilities.Items
		if len(items) > statusMaxCapabilities {
			items = items[:statusMaxCapabilities]
		}

		visibleHeight := m.height - 12
		if visibleHeight < 1 {
			visibleHeight = 1
		}

		start := m.scrollPos
		if start > len(items)-visibleHeight {
			start = len(items) - visibleHeight
		}
		if start < 0 {
			start = 0
		}
		end := start + visibleHeight
		if end > len(items) {
			end = len(items)
		}

		for _, item := range items[start:end] {
			row := tableRowStyle.Render(padRight(item.Name, statusCapNameWidth)) +
				tableRowStyle.Render(padRight(item.Kind, statusCapKindWidth)) +
				tableRowStyle.Render(padRight(item.Status, statusCapStatusWidth)) +
				tableRowDimStyle.Render(padRight(item.Model, statusCapModelWidth))
			b.WriteString(row)
			b.WriteString("\n")
		}

		if len(items) > visibleHeight {
			b.WriteString(fmt.Sprintf("\n%s", helpStyle.Render(
				fmt.Sprintf("showing %d-%d of %d (scroll with up/down)", start+1, end, len(items)),
			)))
		}
	} else {
		b.WriteString(helpStyle.Render("No capabilities registered"))
	}

	b.WriteString("\n\n")
	b.WriteString(helpStyle.Render("Press 'r' to refresh"))

	return b.String()
}

// SetSize updates the terminal dimensions.
func (m *StatusModel) SetSize(w, h int) {
	m.width = w
	m.height = h
}

// IsConnected returns true if the last status fetch succeeded.
func (m StatusModel) IsConnected() bool {
	return m.status != nil && m.lastErr == ""
}

// ---------------------------------------------------------------------------
// Commands
// ---------------------------------------------------------------------------

func (m StatusModel) fetchStatus() tea.Cmd {
	client := m.client
	return func() tea.Msg {
		ctx := context.Background()

		status, err := client.GetStatus(ctx)
		if err != nil {
			return statusFetchResultMsg{err: err}
		}

		caps, err := client.GetCapabilities(ctx)
		if err != nil {
			// Status succeeded but capabilities failed; still show status.
			return statusFetchResultMsg{status: &status, err: nil}
		}

		return statusFetchResultMsg{status: &status, capabilities: &caps}
	}
}

func (m StatusModel) tickCmd() tea.Cmd {
	return tea.Tick(statusPollInterval, func(_ time.Time) tea.Msg {
		return statusTickMsg{}
	})
}

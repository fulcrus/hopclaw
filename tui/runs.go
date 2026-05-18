package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// ---------------------------------------------------------------------------
// Commands
// ---------------------------------------------------------------------------

func (m RunsModel) fetchRuns() tea.Cmd {
	client := m.client
	return func() tea.Msg {
		ctx := context.Background()
		runs, err := client.GetRuns(ctx)
		if err != nil {
			return runsFetchResultMsg{err: err}
		}
		return runsFetchResultMsg{runs: &runs}
	}
}

func (m RunsModel) tickCmd() tea.Cmd {
	return tea.Tick(runsPollInterval, func(_ time.Time) tea.Msg {
		return runsTickMsg{}
	})
}

// ---------------------------------------------------------------------------
// Constants
// ---------------------------------------------------------------------------

const (
	runsPollInterval = 5 * time.Second
	runsMaxItems     = 200
	runsIDWidth      = 14
	runsSessionWidth = 14
	runsStatusWidth  = 18
	runsPhaseWidth   = 16
	runsCreatedWidth = 22
	runsScrollPage   = 10
)

// ---------------------------------------------------------------------------
// Messages
// ---------------------------------------------------------------------------

// runsTickMsg triggers periodic runs refresh.
type runsTickMsg struct{}

// runsFetchResultMsg carries the result of a runs fetch.
type runsFetchResultMsg struct {
	runs *RunsResponse
	err  error
}

// ---------------------------------------------------------------------------
// RunsModel
// ---------------------------------------------------------------------------

// RunsModel is the bubbletea model for the Runs tab.
type RunsModel struct {
	client  *Client
	data    *RunsResponse
	lastErr string
	width   int
	height  int
	table   Table
}

// NewRunsModel creates a new RunsModel.
func NewRunsModel(client *Client) RunsModel {
	columns := []TableColumn{
		{Title: "ID", Width: runsIDWidth},
		{Title: "SESSION", Width: runsSessionWidth},
		{Title: "STATUS", Width: runsStatusWidth},
		{Title: "PHASE", Width: runsPhaseWidth},
		{Title: "CREATED", Width: runsCreatedWidth},
	}
	return RunsModel{
		client: client,
		table:  NewTable(columns),
	}
}

// Init returns the initial command for the runs model.
func (m RunsModel) Init() tea.Cmd {
	return tea.Batch(m.fetchRuns(), m.tickCmd())
}

// Update handles messages for the runs model.
func (m RunsModel) Update(msg tea.Msg) (RunsModel, tea.Cmd) {
	switch msg := msg.(type) {
	case runsTickMsg:
		return m, tea.Batch(m.fetchRuns(), m.tickCmd())

	case runsFetchResultMsg:
		if msg.err != nil {
			m.lastErr = msg.err.Error()
			m.data = nil
		} else {
			m.lastErr = ""
			m.data = msg.runs
			m.updateTableRows()
		}
		return m, nil

	case tea.KeyMsg:
		switch msg.String() {
		case "r":
			return m, m.fetchRuns()
		case "up", "k":
			m.table.SelectUp()
			return m, nil
		case "down", "j":
			m.table.SelectDown()
			return m, nil
		case "pgup":
			for i := 0; i < runsScrollPage; i++ {
				m.table.SelectUp()
			}
			return m, nil
		case "pgdown":
			for i := 0; i < runsScrollPage; i++ {
				m.table.SelectDown()
			}
			return m, nil
		}
	}
	return m, nil
}

// View renders the runs tab.
func (m RunsModel) View() string {
	var b strings.Builder

	b.WriteString(titleStyle.Render("Run History"))
	b.WriteString("\n\n")

	if m.lastErr != "" {
		b.WriteString(errorStyle.Render("error: " + m.lastErr))
		b.WriteString("\n\n")
		b.WriteString(helpStyle.Render("press 'r' to retry"))
		return b.String()
	}

	if m.data == nil {
		b.WriteString(helpStyle.Render("Loading..."))
		return b.String()
	}

	if len(m.data.Items) == 0 {
		b.WriteString(helpStyle.Render("No runs found"))
		b.WriteString("\n\n")
		b.WriteString(helpStyle.Render("press 'r' to refresh"))
		return b.String()
	}

	b.WriteString(m.table.View())
	b.WriteString("\n\n")
	b.WriteString(helpStyle.Render("r:refresh  up/down:navigate"))

	return b.String()
}

// SetSize updates the terminal dimensions.
func (m *RunsModel) SetSize(w, h int) {
	m.width = w
	m.height = h
	m.table.SetSize(w, h-8)
}

// ---------------------------------------------------------------------------
// Table data
// ---------------------------------------------------------------------------

func (m *RunsModel) updateTableRows() {
	if m.data == nil {
		m.table.SetRows(nil)
		return
	}

	items := m.data.Items
	if len(items) > runsMaxItems {
		items = items[:runsMaxItems]
	}

	rows := make([][]string, 0, len(items))
	for _, item := range items {
		rows = append(rows, []string{
			item.ID,
			truncate(item.SessionID, runsSessionWidth-tableDefaultColPadding),
			item.Status,
			item.Phase,
			item.CreatedAt.Format(time.RFC3339),
		})
	}
	m.table.SetRows(rows)
}

// renderRunStatus returns a color-coded status string for a run.
func renderRunStatus(status string) string {
	switch status {
	case "completed":
		return runCompletedStyle.Render(status)
	case "failed":
		return runFailedStyle.Render(status)
	case "running", "streaming":
		return runRunningStyle.Render(status)
	case "queued", "waiting_approval":
		return runPendingStyle.Render(status)
	case "cancelled":
		return cancelledStyle.Render(status)
	default:
		return status
	}
}

// StyledView renders the runs table with color-coded status values.
// This is used when the table widget does not handle per-cell styling.
func (m RunsModel) StyledView() string {
	var b strings.Builder

	b.WriteString(titleStyle.Render("Run History"))
	b.WriteString("\n\n")

	if m.lastErr != "" {
		b.WriteString(errorStyle.Render("error: " + m.lastErr))
		b.WriteString("\n\n")
		b.WriteString(helpStyle.Render("press 'r' to retry"))
		return b.String()
	}

	if m.data == nil {
		b.WriteString(helpStyle.Render("Loading..."))
		return b.String()
	}

	if len(m.data.Items) == 0 {
		b.WriteString(helpStyle.Render("No runs found"))
		b.WriteString("\n\n")
		b.WriteString(helpStyle.Render("press 'r' to refresh"))
		return b.String()
	}

	// Header.
	header := tableHeaderStyle.Render(padRight("ID", runsIDWidth)) +
		tableHeaderStyle.Render(padRight("SESSION", runsSessionWidth)) +
		tableHeaderStyle.Render(padRight("STATUS", runsStatusWidth)) +
		tableHeaderStyle.Render(padRight("PHASE", runsPhaseWidth)) +
		tableHeaderStyle.Render(padRight("CREATED", runsCreatedWidth))
	b.WriteString(header)
	b.WriteString("\n")

	items := m.data.Items
	if len(items) > runsMaxItems {
		items = items[:runsMaxItems]
	}

	visibleHeight := m.height - 8
	if visibleHeight < 1 {
		visibleHeight = 1
	}

	selectedIdx := m.table.SelectedIndex()
	scrollPos := 0
	if selectedIdx > visibleHeight-1 {
		scrollPos = selectedIdx - visibleHeight + 1
	}

	start := scrollPos
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

	for i := start; i < end; i++ {
		item := items[i]
		isSelected := i == selectedIdx

		idCell := padRight(item.ID, runsIDWidth)
		sessCell := padRight(truncate(item.SessionID, runsSessionWidth-tableDefaultColPadding), runsSessionWidth)
		statusCell := padRight(item.Status, runsStatusWidth)
		phaseCell := padRight(item.Phase, runsPhaseWidth)
		createdCell := padRight(item.CreatedAt.Format(time.RFC3339), runsCreatedWidth)

		if isSelected {
			row := selectedRowStyle.Render(idCell) +
				selectedRowStyle.Render(sessCell) +
				selectedRowStyle.Render(statusCell) +
				selectedRowStyle.Render(phaseCell) +
				selectedRowStyle.Render(createdCell)
			b.WriteString(row)
		} else {
			row := tableRowStyle.Render(idCell) +
				tableRowDimStyle.Render(sessCell) +
				renderRunStatus(statusCell) +
				tableRowDimStyle.Render(phaseCell) +
				tableRowDimStyle.Render(createdCell)
			b.WriteString(row)
		}
		b.WriteString("\n")
	}

	if len(items) > visibleHeight {
		b.WriteString(fmt.Sprintf("\n%s", helpStyle.Render(
			fmt.Sprintf("showing %d-%d of %d", start+1, end, len(items)),
		)))
	}

	b.WriteString("\n\n")
	b.WriteString(helpStyle.Render("r:refresh  up/down:navigate"))

	return b.String()
}

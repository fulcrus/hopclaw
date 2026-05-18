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
	sessionsMaxItems     = 100
	sessionsIDWidth      = 14
	sessionsKeyWidth     = 24
	sessionsModelWidth   = 24
	sessionsMsgsWidth    = 8
	sessionsUpdatedWidth = 22

	modalIDNewSession    = "new_session"
	modalIDDeleteSession = "delete_session"
)

// ---------------------------------------------------------------------------
// Messages
// ---------------------------------------------------------------------------

// sessionsFetchResultMsg carries the result of a sessions fetch.
type sessionsFetchResultMsg struct {
	sessions *SessionsResponse
	err      error
}

// sessionsCreateResultMsg carries the result of a session creation.
type sessionsCreateResultMsg struct {
	err error
}

// sessionsDeleteResultMsg carries the result of a session deletion.
type sessionsDeleteResultMsg struct {
	err error
}

// ---------------------------------------------------------------------------
// SessionsModel
// ---------------------------------------------------------------------------

// SessionsModel is the bubbletea model for the Sessions tab.
type SessionsModel struct {
	client      *Client
	sessions    *SessionsResponse
	lastErr     string
	width       int
	height      int
	scrollPos   int
	selectedIdx int
	loading     bool
	modals      ModalStack
}

// NewSessionsModel creates a new SessionsModel.
func NewSessionsModel(client *Client) SessionsModel {
	return SessionsModel{
		client:      client,
		selectedIdx: -1,
		modals:      NewModalStack(),
	}
}

// Init returns the initial command for the sessions model.
func (m SessionsModel) Init() tea.Cmd {
	return m.fetchSessions()
}

// Update handles messages for the sessions model.
func (m SessionsModel) Update(msg tea.Msg) (SessionsModel, tea.Cmd) {
	switch msg := msg.(type) {
	case sessionsFetchResultMsg:
		m.loading = false
		if msg.err != nil {
			m.lastErr = msg.err.Error()
			m.sessions = nil
		} else {
			m.lastErr = ""
			m.sessions = msg.sessions
		}
		return m, nil

	case sessionsCreateResultMsg:
		if msg.err != nil {
			m.lastErr = msg.err.Error()
		} else {
			m.lastErr = ""
		}
		return m, m.fetchSessions()

	case sessionsDeleteResultMsg:
		if msg.err != nil {
			m.lastErr = msg.err.Error()
		} else {
			m.lastErr = ""
		}
		return m, m.fetchSessions()

	case ModalResultMsg:
		return m.handleModalResult(msg)

	case tea.KeyMsg:
		// Route to modal stack first if active.
		if m.modals.IsActive() {
			cmd, _ := m.modals.Update(msg)
			return m, cmd
		}

		switch msg.String() {
		case "r":
			if !m.loading {
				m.loading = true
				return m, m.fetchSessions()
			}
		case "up", "k":
			if m.selectedIdx > 0 {
				m.selectedIdx--
				m.ensureVisible()
			} else if m.selectedIdx < 0 && m.sessionCount() > 0 {
				m.selectedIdx = 0
			}
		case "down", "j":
			if m.selectedIdx < m.sessionCount()-1 {
				m.selectedIdx++
				m.ensureVisible()
			} else if m.selectedIdx < 0 && m.sessionCount() > 0 {
				m.selectedIdx = 0
			}
		case "n":
			return m.startCreateSession()
		case "d":
			return m.startDeleteSession()
		}
	}
	return m, nil
}

// View renders the sessions tab.
func (m SessionsModel) View() string {
	// Render modal on top if active.
	if m.modals.IsActive() {
		return m.modals.View(m.width, m.height)
	}

	var b strings.Builder

	b.WriteString(titleStyle.Render("Sessions"))
	b.WriteString("\n\n")

	if m.lastErr != "" {
		b.WriteString(errorStyle.Render("error: " + m.lastErr))
		b.WriteString("\n\n")
		b.WriteString(helpStyle.Render("press 'r' to retry"))
		return b.String()
	}

	if m.sessions == nil {
		if m.loading {
			b.WriteString(helpStyle.Render("Loading..."))
		} else {
			b.WriteString(helpStyle.Render("press 'r' to load sessions"))
		}
		return b.String()
	}

	if len(m.sessions.Items) == 0 {
		b.WriteString(helpStyle.Render("No sessions found"))
		b.WriteString("\n\n")
		b.WriteString(helpStyle.Render("n:new  r:refresh"))
		return b.String()
	}

	// Header.
	header := tableHeaderStyle.Render(padRight("ID", sessionsIDWidth)) +
		tableHeaderStyle.Render(padRight("KEY", sessionsKeyWidth)) +
		tableHeaderStyle.Render(padRight("MODEL", sessionsModelWidth)) +
		tableHeaderStyle.Render(padRight("MSGS", sessionsMsgsWidth)) +
		tableHeaderStyle.Render(padRight("UPDATED", sessionsUpdatedWidth))
	b.WriteString(header)
	b.WriteString("\n")

	// Rows.
	items := m.sessions.Items
	if len(items) > sessionsMaxItems {
		items = items[:sessionsMaxItems]
	}

	visibleHeight := m.height - 8
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

	for i := start; i < end; i++ {
		item := items[i]
		updatedStr := "-"
		if !item.UpdatedAt.IsZero() {
			updatedStr = item.UpdatedAt.Format(time.RFC3339)
		}

		isSelected := i == m.selectedIdx

		idCell := padRight(item.ID, sessionsIDWidth)
		keyCell := padRight(item.Key, sessionsKeyWidth)
		modelCell := padRight(item.Model, sessionsModelWidth)
		msgsCell := padRight(fmt.Sprintf("%d", item.MessageCount), sessionsMsgsWidth)
		updatedCell := padRight(updatedStr, sessionsUpdatedWidth)

		if isSelected {
			row := selectedRowStyle.Render(idCell) +
				selectedRowStyle.Render(keyCell) +
				selectedRowStyle.Render(modelCell) +
				selectedRowStyle.Render(msgsCell) +
				selectedRowStyle.Render(updatedCell)
			b.WriteString(row)
		} else {
			row := tableRowStyle.Render(idCell) +
				tableRowStyle.Render(keyCell) +
				tableRowDimStyle.Render(modelCell) +
				tableRowStyle.Render(msgsCell) +
				tableRowDimStyle.Render(updatedCell)
			b.WriteString(row)
		}
		b.WriteString("\n")
	}

	if len(items) > visibleHeight {
		b.WriteString(fmt.Sprintf("\n%s", helpStyle.Render(
			fmt.Sprintf("showing %d-%d of %d (scroll with up/down)", start+1, end, len(items)),
		)))
	}

	b.WriteString("\n\n")
	b.WriteString(helpStyle.Render("n:new  d:delete  r:refresh  up/down:select"))

	return b.String()
}

// SetSize updates the terminal dimensions.
func (m *SessionsModel) SetSize(w, h int) {
	m.width = w
	m.height = h
}

// ---------------------------------------------------------------------------
// Session creation
// ---------------------------------------------------------------------------

func (m SessionsModel) startCreateSession() (SessionsModel, tea.Cmd) {
	dialog := NewInputDialog(
		modalIDNewSession,
		"New Session",
		"Enter session key (e.g. tui:my-session)",
		nil,
	)
	m.modals.Push(dialog)
	return m, nil
}

func (m SessionsModel) startDeleteSession() (SessionsModel, tea.Cmd) {
	if m.selectedIdx < 0 || m.sessions == nil || m.selectedIdx >= len(m.sessions.Items) {
		return m, nil
	}
	item := m.sessions.Items[m.selectedIdx]
	dialog := NewConfirmDialog(
		modalIDDeleteSession,
		"Delete Session",
		fmt.Sprintf("Delete session %s (%s)?", item.ID, item.Key),
		nil,
	)
	m.modals.Push(dialog)
	return m, nil
}

func (m SessionsModel) handleModalResult(msg ModalResultMsg) (SessionsModel, tea.Cmd) {
	switch msg.ID {
	case modalIDNewSession:
		if !msg.Confirmed || strings.TrimSpace(msg.Value) == "" {
			return m, nil
		}
		sessionKey := strings.TrimSpace(msg.Value)
		return m, m.createSession(sessionKey)

	case modalIDDeleteSession:
		if !msg.Confirmed {
			return m, nil
		}
		if m.selectedIdx < 0 || m.sessions == nil || m.selectedIdx >= len(m.sessions.Items) {
			return m, nil
		}
		item := m.sessions.Items[m.selectedIdx]
		return m, m.deleteSession(item.ID)
	}
	return m, nil
}

// ---------------------------------------------------------------------------
// Commands
// ---------------------------------------------------------------------------

func (m SessionsModel) fetchSessions() tea.Cmd {
	client := m.client
	return func() tea.Msg {
		ctx := context.Background()
		sessions, err := client.GetSessions(ctx)
		if err != nil {
			return sessionsFetchResultMsg{err: err}
		}
		return sessionsFetchResultMsg{sessions: &sessions}
	}
}

func (m SessionsModel) createSession(sessionKey string) tea.Cmd {
	client := m.client
	return func() tea.Msg {
		ctx := context.Background()
		_, err := client.CreateSession(ctx, sessionKey, "")
		return sessionsCreateResultMsg{err: err}
	}
}

func (m SessionsModel) deleteSession(sessionID string) tea.Cmd {
	client := m.client
	return func() tea.Msg {
		ctx := context.Background()
		err := client.DeleteSession(ctx, sessionID)
		return sessionsDeleteResultMsg{err: err}
	}
}

// ---------------------------------------------------------------------------
// Internal helpers
// ---------------------------------------------------------------------------

func (m SessionsModel) sessionCount() int {
	if m.sessions == nil {
		return 0
	}
	return len(m.sessions.Items)
}

func (m *SessionsModel) ensureVisible() {
	visibleHeight := m.height - 8
	if visibleHeight < 1 {
		visibleHeight = 1
	}
	if m.selectedIdx < m.scrollPos {
		m.scrollPos = m.selectedIdx
	}
	if m.selectedIdx >= m.scrollPos+visibleHeight {
		m.scrollPos = m.selectedIdx - visibleHeight + 1
	}
	if m.scrollPos < 0 {
		m.scrollPos = 0
	}
}

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
	approvalsPollInterval = 5 * time.Second
	approvalsMaxItems     = 200
	approvalsIDWidth      = 14
	approvalsToolWidth    = 20
	approvalsSessionWidth = 14
	approvalsStatusWidth  = 12
	approvalsCreatedWidth = 22
	approvalsScrollPage   = 10

	approvalScopeOnce    = "once"
	approvalScopeSession = "session"
	approvalScopeAlways  = "always"

	modalIDApproveScope  = "approve_scope"
	modalIDDenyConfirm   = "deny_confirm"
	modalIDCancelConfirm = "cancel_confirm"
)

// ---------------------------------------------------------------------------
// Messages
// ---------------------------------------------------------------------------

// approvalsTickMsg triggers periodic approval refresh.
type approvalsTickMsg struct{}

// approvalsFetchResultMsg carries the result of an approvals fetch.
type approvalsFetchResultMsg struct {
	approvals *ApprovalsResponse
	err       error
}

// approvalsResolveResultMsg carries the result of an approval resolution.
type approvalsResolveResultMsg struct {
	err error
}

// ---------------------------------------------------------------------------
// ApprovalsModel
// ---------------------------------------------------------------------------

// ApprovalsModel is the bubbletea model for the Approvals tab.
type ApprovalsModel struct {
	client  *Client
	data    *ApprovalsResponse
	lastErr string
	width   int
	height  int
	table   Table
	modals  ModalStack

	// Detail view state.
	showDetail    bool
	detailTicket  *ApprovalTicket
	pendingAction string // "approve", "deny", "cancel"
}

// NewApprovalsModel creates a new ApprovalsModel.
func NewApprovalsModel(client *Client) ApprovalsModel {
	columns := []TableColumn{
		{Title: "ID", Width: approvalsIDWidth},
		{Title: "TOOL", Width: approvalsToolWidth},
		{Title: "SESSION", Width: approvalsSessionWidth},
		{Title: "STATUS", Width: approvalsStatusWidth},
		{Title: "CREATED", Width: approvalsCreatedWidth},
	}
	return ApprovalsModel{
		client: client,
		table:  NewTable(columns),
		modals: NewModalStack(),
	}
}

// Init returns the initial command for the approvals model.
func (m ApprovalsModel) Init() tea.Cmd {
	return tea.Batch(m.fetchApprovals(), m.tickCmd())
}

// PendingCount returns the number of pending approval tickets.
func (m ApprovalsModel) PendingCount() int {
	if m.data == nil {
		return 0
	}
	count := 0
	for _, item := range m.data.Items {
		if item.Status == "pending" {
			count++
		}
	}
	return count
}

// Update handles messages for the approvals model.
func (m ApprovalsModel) Update(msg tea.Msg) (ApprovalsModel, tea.Cmd) {
	switch msg := msg.(type) {
	case approvalsTickMsg:
		return m, tea.Batch(m.fetchApprovals(), m.tickCmd())

	case approvalsFetchResultMsg:
		if msg.err != nil {
			m.lastErr = msg.err.Error()
			m.data = nil
		} else {
			m.lastErr = ""
			m.data = msg.approvals
			m.updateTableRows()
		}
		return m, nil

	case approvalsResolveResultMsg:
		if msg.err != nil {
			m.lastErr = msg.err.Error()
		} else {
			m.lastErr = ""
			m.showDetail = false
			m.detailTicket = nil
		}
		return m, m.fetchApprovals()

	case ModalResultMsg:
		return m.handleModalResult(msg)

	case tea.KeyMsg:
		// Route to modal stack first if active.
		if m.modals.IsActive() {
			cmd, _ := m.modals.Update(msg)
			return m, cmd
		}

		// Detail view key handling.
		if m.showDetail {
			return m.handleDetailKey(msg)
		}

		// List view key handling.
		switch msg.String() {
		case "r":
			return m, m.fetchApprovals()
		case "up", "k":
			m.table.SelectUp()
			return m, nil
		case "down", "j":
			m.table.SelectDown()
			return m, nil
		case "enter":
			return m.enterDetail()
		case "a":
			return m.startApprove()
		case "d":
			return m.startDeny()
		case "c":
			return m.startCancel()
		}
	}
	return m, nil
}

// View renders the approvals tab.
func (m ApprovalsModel) View() string {
	// Render modal on top if active.
	if m.modals.IsActive() {
		return m.modals.View(m.width, m.height)
	}

	if m.showDetail {
		return m.viewDetail()
	}

	return m.viewList()
}

// SetSize updates the terminal dimensions.
func (m *ApprovalsModel) SetSize(w, h int) {
	m.width = w
	m.height = h
	m.table.SetSize(w, h-8)
}

// ---------------------------------------------------------------------------
// List view
// ---------------------------------------------------------------------------

func (m ApprovalsModel) viewList() string {
	var b strings.Builder

	b.WriteString(titleStyle.Render("Approvals"))
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
		b.WriteString(helpStyle.Render("No pending approvals"))
		b.WriteString("\n\n")
		b.WriteString(helpStyle.Render("press 'r' to refresh"))
		return b.String()
	}

	b.WriteString(m.table.View())
	b.WriteString("\n\n")
	b.WriteString(helpStyle.Render("a:approve  d:deny  c:cancel  Enter:detail  r:refresh"))

	return b.String()
}

// ---------------------------------------------------------------------------
// Detail view
// ---------------------------------------------------------------------------

func (m ApprovalsModel) viewDetail() string {
	if m.detailTicket == nil {
		return helpStyle.Render("No ticket selected")
	}

	t := m.detailTicket
	var b strings.Builder

	b.WriteString(titleStyle.Render("Approval Detail"))
	b.WriteString("\n\n")

	b.WriteString(fmt.Sprintf("  ID:         %s\n", t.ID))
	b.WriteString(fmt.Sprintf("  Session:    %s\n", t.SessionID))
	b.WriteString(fmt.Sprintf("  Run:        %s\n", t.RunID))
	b.WriteString(fmt.Sprintf("  Status:     %s\n", m.renderStatus(t.Status)))
	b.WriteString(fmt.Sprintf("  Created:    %s\n", t.CreatedAt.Format(time.RFC3339)))
	if t.ResolvedAt != nil {
		b.WriteString(fmt.Sprintf("  Resolved:   %s\n", t.ResolvedAt.Format(time.RFC3339)))
	}
	if t.ResolvedBy != "" {
		b.WriteString(fmt.Sprintf("  ResolvedBy: %s\n", t.ResolvedBy))
	}
	if t.Note != "" {
		b.WriteString(fmt.Sprintf("  Note:       %s\n", t.Note))
	}

	b.WriteString("\n")
	b.WriteString(titleStyle.Render("  Tool Calls"))
	b.WriteString("\n")
	for _, tc := range t.ToolCalls {
		b.WriteString(fmt.Sprintf("    - %s", tc.Name))
		if tc.Input != "" {
			b.WriteString(fmt.Sprintf("  input: %s", truncate(tc.Input, 60)))
		}
		b.WriteString("\n")
	}

	b.WriteString("\n")
	if t.Status == "pending" {
		b.WriteString(helpStyle.Render("a:approve  d:deny  c:cancel  Esc:back"))
	} else {
		b.WriteString(helpStyle.Render("Esc:back"))
	}

	return b.String()
}

func (m ApprovalsModel) renderStatus(status string) string {
	switch status {
	case "pending":
		return pendingStyle.Render(status)
	case "approved":
		return approvedStyle.Render(status)
	case "denied":
		return deniedStyle.Render(status)
	case "cancelled":
		return cancelledStyle.Render(status)
	default:
		return status
	}
}

func (m ApprovalsModel) handleDetailKey(msg tea.KeyMsg) (ApprovalsModel, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.showDetail = false
		m.detailTicket = nil
		return m, nil
	case "a":
		return m.startApprove()
	case "d":
		return m.startDeny()
	case "c":
		return m.startCancel()
	}
	return m, nil
}

// ---------------------------------------------------------------------------
// Actions
// ---------------------------------------------------------------------------

func (m ApprovalsModel) enterDetail() (ApprovalsModel, tea.Cmd) {
	idx := m.table.SelectedIndex()
	if idx < 0 || m.data == nil || idx >= len(m.data.Items) {
		return m, nil
	}
	ticket := m.data.Items[idx]
	m.showDetail = true
	m.detailTicket = &ticket
	return m, nil
}

func (m ApprovalsModel) selectedTicket() *ApprovalTicket {
	if m.showDetail && m.detailTicket != nil {
		return m.detailTicket
	}
	idx := m.table.SelectedIndex()
	if idx < 0 || m.data == nil || idx >= len(m.data.Items) {
		return nil
	}
	t := m.data.Items[idx]
	return &t
}

func (m ApprovalsModel) startApprove() (ApprovalsModel, tea.Cmd) {
	ticket := m.selectedTicket()
	if ticket == nil || ticket.Status != "pending" {
		return m, nil
	}
	m.pendingAction = "approve"

	// Show scope selection dialog.
	dialog := NewConfirmDialog(
		modalIDApproveScope,
		"Approve - Select Scope",
		"Choose approval scope:\n  1) once    - this invocation only\n  2) session - entire session\n  3) always  - permanent",
		nil,
	)
	dialog.Buttons = []string{"once", "session", "always"}
	dialog.ActiveButton = 0
	m.modals.Push(dialog)
	return m, nil
}

func (m ApprovalsModel) startDeny() (ApprovalsModel, tea.Cmd) {
	ticket := m.selectedTicket()
	if ticket == nil || ticket.Status != "pending" {
		return m, nil
	}
	m.pendingAction = "deny"

	dialog := NewConfirmDialog(
		modalIDDenyConfirm,
		"Deny Approval",
		fmt.Sprintf("Deny approval %s?", ticket.ID),
		nil,
	)
	m.modals.Push(dialog)
	return m, nil
}

func (m ApprovalsModel) startCancel() (ApprovalsModel, tea.Cmd) {
	ticket := m.selectedTicket()
	if ticket == nil || ticket.Status != "pending" {
		return m, nil
	}
	m.pendingAction = "cancel"

	dialog := NewConfirmDialog(
		modalIDCancelConfirm,
		"Cancel Approval",
		fmt.Sprintf("Cancel approval %s?", ticket.ID),
		nil,
	)
	m.modals.Push(dialog)
	return m, nil
}

func (m ApprovalsModel) handleModalResult(msg ModalResultMsg) (ApprovalsModel, tea.Cmd) {
	ticket := m.selectedTicket()
	if ticket == nil {
		return m, nil
	}

	switch msg.ID {
	case modalIDApproveScope:
		if !msg.Confirmed && msg.Value == "" {
			// Cancelled or dismissed without selecting.
			// Check which button was active.
			m.pendingAction = ""
			return m, nil
		}
		// The button labels are the scope values.
		scope := msg.Value
		if scope == "" {
			// Confirmed defaults to the active button label.
			if msg.Confirmed {
				scope = approvalScopeOnce
			} else {
				m.pendingAction = ""
				return m, nil
			}
		}
		return m, m.resolveApproval(ticket.ID, "approved", scope, "")

	case modalIDDenyConfirm:
		if !msg.Confirmed {
			m.pendingAction = ""
			return m, nil
		}
		return m, m.resolveApproval(ticket.ID, "denied", "", "")

	case modalIDCancelConfirm:
		if !msg.Confirmed {
			m.pendingAction = ""
			return m, nil
		}
		return m, m.cancelApproval(ticket.ID)
	}

	return m, nil
}

// ---------------------------------------------------------------------------
// Table data
// ---------------------------------------------------------------------------

func (m *ApprovalsModel) updateTableRows() {
	if m.data == nil {
		m.table.SetRows(nil)
		return
	}

	rows := make([][]string, 0, len(m.data.Items))
	for _, item := range m.data.Items {
		toolName := ""
		if len(item.ToolCalls) > 0 {
			toolName = item.ToolCalls[0].Name
			if len(item.ToolCalls) > 1 {
				toolName += fmt.Sprintf(" +%d", len(item.ToolCalls)-1)
			}
		}
		rows = append(rows, []string{
			item.ID,
			toolName,
			truncate(item.SessionID, approvalsSessionWidth-tableDefaultColPadding),
			item.Status,
			item.CreatedAt.Format(time.RFC3339),
		})
	}
	m.table.SetRows(rows)
}

// ---------------------------------------------------------------------------
// Commands
// ---------------------------------------------------------------------------

func (m ApprovalsModel) fetchApprovals() tea.Cmd {
	client := m.client
	return func() tea.Msg {
		ctx := context.Background()
		approvals, err := client.GetApprovals(ctx, "pending")
		if err != nil {
			return approvalsFetchResultMsg{err: err}
		}
		return approvalsFetchResultMsg{approvals: &approvals}
	}
}

func (m ApprovalsModel) resolveApproval(id, status, scope, note string) tea.Cmd {
	client := m.client
	return func() tea.Msg {
		ctx := context.Background()
		err := client.ResolveApproval(ctx, id, status, scope, note)
		return approvalsResolveResultMsg{err: err}
	}
}

func (m ApprovalsModel) cancelApproval(id string) tea.Cmd {
	client := m.client
	return func() tea.Msg {
		ctx := context.Background()
		err := client.CancelApproval(ctx, id)
		return approvalsResolveResultMsg{err: err}
	}
}

func (m ApprovalsModel) tickCmd() tea.Cmd {
	return tea.Tick(approvalsPollInterval, func(_ time.Time) tea.Msg {
		return approvalsTickMsg{}
	})
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// truncate shortens a string to maxLen, appending "..." if truncated.
func truncate(s string, maxLen int) string {
	if maxLen <= 0 {
		return ""
	}
	if len(s) <= maxLen {
		return s
	}
	if maxLen <= 3 {
		return s[:maxLen]
	}
	return s[:maxLen-3] + "..."
}

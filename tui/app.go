package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// ---------------------------------------------------------------------------
// Constants
// ---------------------------------------------------------------------------

const (
	tabChat      = 0
	tabStatus    = 1
	tabSessions  = 2
	tabEvents    = 3
	tabApprovals = 4
	tabRuns      = 5
	tabCount     = 6

	minTermWidth  = 40
	minTermHeight = 10

	// contentMargin accounts for tab bar, status bar, and padding.
	contentMargin = 4
)

// tabNames are the display labels for each tab.
var tabNames = []string{"Chat", "Status", "Sessions", "Events", "Approvals", "Runs"}

// ---------------------------------------------------------------------------
// App
// ---------------------------------------------------------------------------

// App is the top-level bubbletea model for the HopClaw TUI.
type App struct {
	// Global state.
	activeTab   int
	gatewayAddr string
	client      *Client
	width       int
	height      int
	showHelp    bool

	// Tab models.
	chat      ChatModel
	status    StatusModel
	sessions  SessionsModel
	events    EventsModel
	approvals ApprovalsModel
	runs      RunsModel
}

// New creates a new TUI application.
func New(gatewayAddr, authToken string) *App {
	baseURL := "http://" + gatewayAddr
	client := NewClient(baseURL, authToken)

	return &App{
		activeTab:   tabChat,
		gatewayAddr: gatewayAddr,
		client:      client,
		chat:        NewChatModel(client),
		status:      NewStatusModel(client),
		sessions:    NewSessionsModel(client),
		events:      NewEventsModel(client),
		approvals:   NewApprovalsModel(client),
		runs:        NewRunsModel(client),
	}
}

// Run starts the bubbletea program.
func (a *App) Run() error {
	p := tea.NewProgram(a, tea.WithAltScreen())
	_, err := p.Run()
	return err
}

// Init implements tea.Model.
func (a *App) Init() tea.Cmd {
	return tea.Batch(
		a.chat.Init(),
		a.status.Init(),
		a.sessions.Init(),
		a.events.Init(),
		a.approvals.Init(),
		a.runs.Init(),
	)
}

// Update implements tea.Model.
func (a *App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		a.width = msg.Width
		a.height = msg.Height
		a.propagateSize()
		return a, nil

	case tea.KeyMsg:
		// Help overlay toggle takes priority.
		switch msg.String() {
		case "?":
			// In chat tab, only toggle help if not focused on input
			// (to allow typing '?'). We toggle for non-chat tabs always.
			if a.activeTab != tabChat || a.showHelp {
				a.showHelp = !a.showHelp
				return a, nil
			}
			// In chat tab without help showing, let '?' go to input.

		case "f1":
			a.showHelp = !a.showHelp
			return a, nil

		case "esc":
			if a.showHelp {
				a.showHelp = false
				return a, nil
			}
		}

		// If help overlay is showing, consume all other keys.
		if a.showHelp {
			return a, nil
		}

		switch msg.String() {
		case "ctrl+c", "q":
			// Allow 'q' to quit only when not in chat input focus.
			if msg.String() == "ctrl+c" {
				return a, tea.Quit
			}
			if a.activeTab != tabChat {
				return a, tea.Quit
			}
			// In chat tab, 'q' goes to the text input.

		case "tab":
			a.switchTab((a.activeTab + 1) % tabCount)
			return a, nil

		case "shift+tab":
			a.switchTab((a.activeTab - 1 + tabCount) % tabCount)
			return a, nil

		case "ctrl+a":
			// Jump to approvals tab if there are pending items.
			if a.approvals.PendingCount() > 0 {
				a.switchTab(tabApprovals)
				return a, nil
			}

		case "1":
			if a.activeTab != tabChat {
				a.switchTab(tabChat)
				return a, nil
			}
		case "2":
			if a.activeTab != tabStatus {
				a.switchTab(tabStatus)
				return a, nil
			}
		case "3":
			if a.activeTab != tabSessions {
				a.switchTab(tabSessions)
				return a, nil
			}
		case "4":
			if a.activeTab != tabEvents {
				a.switchTab(tabEvents)
				return a, nil
			}
		case "5":
			if a.activeTab != tabApprovals {
				a.switchTab(tabApprovals)
				return a, nil
			}
		case "6":
			if a.activeTab != tabRuns {
				a.switchTab(tabRuns)
				return a, nil
			}
		}
	}

	// Route message to the active tab model.
	var cmd tea.Cmd
	switch a.activeTab {
	case tabChat:
		a.chat, cmd = a.chat.Update(msg)
	case tabStatus:
		a.status, cmd = a.status.Update(msg)
	case tabSessions:
		a.sessions, cmd = a.sessions.Update(msg)
	case tabEvents:
		a.events, cmd = a.events.Update(msg)
	case tabApprovals:
		a.approvals, cmd = a.approvals.Update(msg)
	case tabRuns:
		a.runs, cmd = a.runs.Update(msg)
	}

	// Also route tick/fetch messages to background tabs so they keep updating.
	var bgCmds []tea.Cmd
	switch msg.(type) {
	case statusTickMsg, statusFetchResultMsg:
		if a.activeTab != tabStatus {
			var c tea.Cmd
			a.status, c = a.status.Update(msg)
			bgCmds = append(bgCmds, c)
		}
	case eventsTickMsg, eventsFetchResultMsg:
		if a.activeTab != tabEvents {
			var c tea.Cmd
			a.events, c = a.events.Update(msg)
			bgCmds = append(bgCmds, c)
		}
	case chatSubmitResultMsg:
		if a.activeTab != tabChat {
			var c tea.Cmd
			a.chat, c = a.chat.Update(msg)
			bgCmds = append(bgCmds, c)
		}
	case chatSessionsFetchResultMsg:
		if a.activeTab != tabChat {
			var c tea.Cmd
			a.chat, c = a.chat.Update(msg)
			bgCmds = append(bgCmds, c)
		}
	case sessionsFetchResultMsg:
		if a.activeTab != tabSessions {
			var c tea.Cmd
			a.sessions, c = a.sessions.Update(msg)
			bgCmds = append(bgCmds, c)
		}
	case approvalsTickMsg, approvalsFetchResultMsg, approvalsResolveResultMsg:
		if a.activeTab != tabApprovals {
			var c tea.Cmd
			a.approvals, c = a.approvals.Update(msg)
			bgCmds = append(bgCmds, c)
		}
	case runsTickMsg, runsFetchResultMsg:
		if a.activeTab != tabRuns {
			var c tea.Cmd
			a.runs, c = a.runs.Update(msg)
			bgCmds = append(bgCmds, c)
		}
	case sessionsCreateResultMsg, sessionsDeleteResultMsg:
		if a.activeTab != tabSessions {
			var c tea.Cmd
			a.sessions, c = a.sessions.Update(msg)
			bgCmds = append(bgCmds, c)
		}
	}

	if len(bgCmds) > 0 {
		bgCmds = append(bgCmds, cmd)
		return a, tea.Batch(bgCmds...)
	}
	return a, cmd
}

// View implements tea.Model.
func (a *App) View() string {
	if a.width < minTermWidth || a.height < minTermHeight {
		return "terminal too small, please resize"
	}

	// Tab bar.
	tabBar := renderTabBar(tabNames, a.activeTab, a.width)

	// Content area.
	contentHeight := a.height - contentMargin
	var content string
	switch a.activeTab {
	case tabChat:
		content = a.chat.View()
	case tabStatus:
		content = a.status.View()
	case tabSessions:
		content = a.sessions.View()
	case tabEvents:
		content = a.events.View()
	case tabApprovals:
		content = a.approvals.View()
	case tabRuns:
		content = a.runs.StyledView()
	}

	// Pad content to fill available height.
	contentLines := strings.Count(content, "\n") + 1
	for contentLines < contentHeight {
		content += "\n"
		contentLines++
	}

	styledContent := contentStyle.Width(a.width).Height(contentHeight).Render(content)

	// Status bar.
	statusBar := a.renderStatusBar()

	view := lipgloss.JoinVertical(lipgloss.Left, tabBar, styledContent, statusBar)

	// Render help overlay on top if active.
	if a.showHelp {
		overlay := helpOverlay(a.width, a.height)
		return overlay
	}

	return view
}

// ---------------------------------------------------------------------------
// Internal helpers
// ---------------------------------------------------------------------------

func (a *App) switchTab(idx int) {
	if idx < 0 || idx >= tabCount {
		return
	}

	// Blur chat input when leaving chat tab.
	if a.activeTab == tabChat {
		a.chat.Blur()
	}

	a.activeTab = idx

	// Focus chat input when entering chat tab.
	if a.activeTab == tabChat {
		a.chat.Focus()
	}
}

func (a *App) propagateSize() {
	contentHeight := a.height - contentMargin
	if contentHeight < 1 {
		contentHeight = 1
	}
	contentWidth := a.width - contentMargin
	if contentWidth < 1 {
		contentWidth = 1
	}

	a.chat.SetSize(contentWidth, contentHeight)
	a.status.SetSize(contentWidth, contentHeight)
	a.sessions.SetSize(contentWidth, contentHeight)
	a.events.SetSize(contentWidth, contentHeight)
	a.approvals.SetSize(contentWidth, contentHeight)
	a.runs.SetSize(contentWidth, contentHeight)
}

func (a *App) renderStatusBar() string {
	// Connection status.
	connStatus := statusDisconnectedStyle.Render("disconnected")
	if a.status.IsConnected() {
		connStatus = statusConnectedStyle.Render("connected")
	}

	// Mode indicator.
	modeStr := chatModeStyle.Render("CHAT")
	if a.chat.IsBashMode() {
		modeStr = bashModeStyle.Render("BASH")
	}

	// Session name.
	sessStr := sessionNameStyle.Render(a.chat.SessionName())

	// Pending approvals indicator.
	approvalStr := ""
	pending := a.approvals.PendingCount()
	if pending > 0 {
		approvalStr = fmt.Sprintf(" | %s", pendingStyle.Render(fmt.Sprintf("%d pending", pending)))
	}

	left := fmt.Sprintf(" %s | %s | %s | %s%s", modeStr, sessStr, a.gatewayAddr, connStatus, approvalStr)
	right := "?:help | Tab:switch | Ctrl+S:session | Ctrl+B:bash | Ctrl+A:approvals | q:quit "

	gap := a.width - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 0 {
		gap = 0
	}

	bar := left + strings.Repeat(" ", gap) + right
	return statusBarStyle.Width(a.width).Render(bar)
}

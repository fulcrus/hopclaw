package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// ---------------------------------------------------------------------------
// Constants
// ---------------------------------------------------------------------------

const (
	chatDefaultSessionKey = "tui"
	chatDefaultChannel    = "tui"
	chatPollInterval      = 1 * time.Second
	chatMaxMessages       = 500
	chatScrollPageSize    = 5
	chatBashPrompt        = "$ "
	chatNormalPrompt      = "Type a message..."
	chatThinkingInterval  = 200 * time.Millisecond
)

// ---------------------------------------------------------------------------
// Messages
// ---------------------------------------------------------------------------

// chatMessage represents a single chat message displayed in the TUI.
type chatMessage struct {
	Role    string
	Content string
	SentAt  time.Time
}

// chatSubmitResultMsg is sent when a message submission + polling completes.
type chatSubmitResultMsg struct {
	response string
	err      error
}

// chatSessionsFetchResultMsg carries session list results for the chat session selector.
type chatSessionsFetchResultMsg struct {
	sessions []SessionItem
	err      error
}

// chatThinkingTickMsg triggers spinner advancement while waiting for a response.
type chatThinkingTickMsg struct{}

// ---------------------------------------------------------------------------
// ChatModel
// ---------------------------------------------------------------------------

// ChatModel is the bubbletea model for the Chat tab.
type ChatModel struct {
	client     *Client
	input      textinput.Model
	messages   []chatMessage
	scrollPos  int
	width      int
	height     int
	sending    bool
	lastErr    string
	sessionKey string

	// Session switching.
	availableSessions []SessionItem
	activeSessionIdx  int
	sessionName       string

	// Bash mode.
	bashMode bool

	// Command history.
	history InputHistory

	// Thinking spinner.
	spinner Spinner
}

// NewChatModel creates a new ChatModel.
func NewChatModel(client *Client) ChatModel {
	ti := textinput.New()
	ti.Placeholder = chatNormalPrompt
	ti.CharLimit = 4096
	ti.Width = 80

	return ChatModel{
		client:      client,
		input:       ti,
		messages:    make([]chatMessage, 0),
		sessionKey:  chatDefaultChannel + ":" + chatDefaultSessionKey,
		sessionName: chatDefaultSessionKey,
		history:     NewInputHistory(),
		spinner:     NewSpinner(),
	}
}

// Init returns the initial command for the chat model.
func (m ChatModel) Init() tea.Cmd {
	return tea.Batch(textinput.Blink, m.fetchAvailableSessions())
}

// Update handles messages for the chat model.
func (m ChatModel) Update(msg tea.Msg) (ChatModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "enter":
			return m.handleSubmit()
		case "up":
			// If input is empty or we're already browsing history, cycle up.
			if val, ok := m.history.Up(m.input.Value()); ok {
				m.input.SetValue(val)
				m.input.CursorEnd()
				return m, nil
			}
			// Otherwise scroll messages.
			if m.scrollPos > 0 {
				m.scrollPos--
			}
			return m, nil
		case "down":
			// If browsing history, cycle down.
			if val, ok := m.history.Down(); ok {
				m.input.SetValue(val)
				m.input.CursorEnd()
				return m, nil
			}
			// Otherwise scroll messages.
			maxScroll := m.maxScroll()
			if m.scrollPos < maxScroll {
				m.scrollPos++
			}
			return m, nil
		case "pgup":
			m.scrollPos -= chatScrollPageSize
			if m.scrollPos < 0 {
				m.scrollPos = 0
			}
			return m, nil
		case "pgdown":
			m.scrollPos += chatScrollPageSize
			maxScroll := m.maxScroll()
			if m.scrollPos > maxScroll {
				m.scrollPos = maxScroll
			}
			return m, nil
		case "ctrl+s":
			return m.cycleSession()
		case "ctrl+b":
			return m.toggleBashMode()
		case "ctrl+l":
			return m.clearMessages()
		}

	case chatSubmitResultMsg:
		m.sending = false
		if msg.err != nil {
			m.lastErr = msg.err.Error()
		} else {
			m.lastErr = ""
			m.addMessage("assistant", msg.response)
		}
		return m, nil

	case chatSessionsFetchResultMsg:
		if msg.err == nil && len(msg.sessions) > 0 {
			m.availableSessions = msg.sessions
		}
		return m, nil

	case chatThinkingTickMsg:
		if m.sending {
			m.spinner.Tick()
			return m, m.thinkingTickCmd()
		}
		return m, nil
	}

	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

// View renders the chat tab.
func (m ChatModel) View() string {
	chatHeight := m.height - 5 // room for header + input + help line

	// Chat header with session name and mode indicator.
	header := m.renderChatHeader()

	// Render messages.
	var lines []string
	contentWidth := m.width - 4
	if contentWidth < 20 {
		contentWidth = 20
	}

	for _, msg := range m.messages {
		ts := timestampStyle.Render(msg.SentAt.Format("15:04:05"))
		var roleLine string
		switch msg.Role {
		case "user":
			roleLine = userMsgStyle.Render("you") + " " + ts
		case "assistant":
			roleLine = assistantMsgStyle.Render("assistant") + " " + ts
		default:
			roleLine = systemMsgStyle.Render(msg.Role) + " " + ts
		}
		lines = append(lines, roleLine)

		// Apply basic markdown rendering then wrap.
		rendered := renderMarkdown(msg.Content, contentWidth)
		lines = append(lines, rendered)
		lines = append(lines, "") // blank separator
	}

	if len(lines) == 0 {
		lines = []string{helpStyle.Render("No messages yet. Type a message and press Enter.")}
	}

	// Apply scroll.
	visibleLines := chatHeight
	if visibleLines < 1 {
		visibleLines = 1
	}
	totalLines := len(lines)
	start := m.scrollPos
	if start > totalLines-visibleLines {
		start = totalLines - visibleLines
	}
	if start < 0 {
		start = 0
	}
	end := start + visibleLines
	if end > totalLines {
		end = totalLines
	}

	visible := strings.Join(lines[start:end], "\n")

	// Pad to fill available height.
	renderedLines := strings.Count(visible, "\n") + 1
	for renderedLines < visibleLines {
		visible += "\n"
		renderedLines++
	}

	// Input area with mode-specific prompt.
	inputLine := m.input.View()
	if m.sending {
		inputLine = helpStyle.Render(fmt.Sprintf("%s thinking...", m.spinner.View()))
	}

	// Error display.
	errLine := ""
	if m.lastErr != "" {
		errLine = errorStyle.Render("error: "+m.lastErr) + "\n"
	}

	return header + "\n" + visible + "\n" + errLine + inputLine
}

// Focus gives focus to the text input.
func (m *ChatModel) Focus() {
	m.input.Focus()
}

// Blur removes focus from the text input.
func (m *ChatModel) Blur() {
	m.input.Blur()
}

// SetSize updates the terminal dimensions for layout.
func (m *ChatModel) SetSize(w, h int) {
	m.width = w
	m.height = h
	m.input.Width = w - 4
	if m.input.Width < 20 {
		m.input.Width = 20
	}
}

// IsBashMode returns whether bash mode is active.
func (m ChatModel) IsBashMode() bool {
	return m.bashMode
}

// SessionName returns the display name of the current session.
func (m ChatModel) SessionName() string {
	return m.sessionName
}

// ---------------------------------------------------------------------------
// Internal helpers
// ---------------------------------------------------------------------------

func (m ChatModel) renderChatHeader() string {
	sessionLabel := chatHeaderStyle.Render("session: ") + sessionNameStyle.Render(m.sessionName)
	modeLabel := ""
	if m.bashMode {
		modeLabel = " " + bashModeStyle.Render("BASH")
	}
	return sessionLabel + modeLabel
}

func (m ChatModel) handleSubmit() (ChatModel, tea.Cmd) {
	content := strings.TrimSpace(m.input.Value())
	if content == "" || m.sending {
		return m, nil
	}

	// Save to command history and reset browsing.
	m.history.Push(content)
	m.history.Reset()

	m.sending = true
	m.lastErr = ""

	// In bash mode, wrap the content to indicate shell execution.
	submitContent := content
	displayRole := "user"
	if m.bashMode {
		submitContent = fmt.Sprintf("[BASH] Execute the following shell command and return the output:\n```bash\n%s\n```", content)
		displayRole = "user"
	}

	m.addMessage(displayRole, content)
	m.input.SetValue("")

	// Scroll to bottom.
	m.scrollPos = m.maxScroll()

	client := m.client
	sessionKey := m.sessionKey

	submitCmd := func() tea.Msg {
		ctx := context.Background()
		run, err := client.SubmitMessage(ctx, sessionKey, submitContent)
		if err != nil {
			return chatSubmitResultMsg{err: err}
		}

		// Poll until terminal status.
		for {
			switch run.Status {
			case "completed", "failed", "cancelled":
				return m.fetchAssistantReply(ctx, client, run)
			}
			time.Sleep(chatPollInterval)
			run, err = client.GetRun(ctx, run.ID)
			if err != nil {
				return chatSubmitResultMsg{err: fmt.Errorf("poll run: %w", err)}
			}
		}
	}

	return m, tea.Batch(submitCmd, m.thinkingTickCmd())
}

func (m ChatModel) cycleSession() (ChatModel, tea.Cmd) {
	if len(m.availableSessions) == 0 {
		return m, m.fetchAvailableSessions()
	}

	m.activeSessionIdx = (m.activeSessionIdx + 1) % len(m.availableSessions)
	selected := m.availableSessions[m.activeSessionIdx]
	m.sessionKey = selected.Key
	m.sessionName = selected.Key
	if m.sessionName == "" {
		m.sessionName = selected.ID
	}

	// Clear messages and reload from the new session.
	m.messages = m.messages[:0]
	m.scrollPos = 0
	m.lastErr = ""

	client := m.client
	sessionID := selected.ID

	return m, func() tea.Msg {
		ctx := context.Background()
		session, err := client.GetSession(ctx, sessionID)
		if err != nil {
			return chatSubmitResultMsg{err: fmt.Errorf("load session: %w", err)}
		}

		// Replay all messages from the session (user + assistant).
		var response strings.Builder
		for _, msg := range session.Messages {
			content := strings.TrimSpace(msg.Content)
			if content == "" {
				continue
			}
			switch msg.Role {
			case "user":
				if response.Len() > 0 {
					response.WriteString("\n---\n")
				}
				response.WriteString("[user] " + content)
			case "assistant":
				if response.Len() > 0 {
					response.WriteString("\n---\n")
				}
				response.WriteString(content)
			}
		}
		if response.Len() == 0 {
			return chatSubmitResultMsg{response: "(session loaded, no previous messages)"}
		}
		return chatSubmitResultMsg{response: response.String()}
	}
}

func (m ChatModel) toggleBashMode() (ChatModel, tea.Cmd) {
	m.bashMode = !m.bashMode
	if m.bashMode {
		m.input.Placeholder = chatBashPrompt
	} else {
		m.input.Placeholder = chatNormalPrompt
	}
	return m, nil
}

func (m ChatModel) clearMessages() (ChatModel, tea.Cmd) {
	m.messages = m.messages[:0]
	m.scrollPos = 0
	m.lastErr = ""
	return m, nil
}

func (m ChatModel) fetchAssistantReply(ctx context.Context, client *Client, run RunResponse) chatSubmitResultMsg {
	if run.Status == runStatusFailed {
		errMsg := run.Error
		if errMsg == "" {
			errMsg = "run failed"
		}
		return chatSubmitResultMsg{err: fmt.Errorf("run %s failed: %s", run.ID, errMsg)}
	}
	if run.Status == runStatusCancelled {
		return chatSubmitResultMsg{err: fmt.Errorf("run %s was cancelled", run.ID)}
	}

	// Fetch session to get the assistant's last message.
	session, err := client.GetSession(ctx, run.SessionID)
	if err != nil {
		return chatSubmitResultMsg{err: fmt.Errorf("fetch session: %w", err)}
	}

	for i := len(session.Messages) - 1; i >= 0; i-- {
		msg := session.Messages[i]
		if msg.Role == "assistant" && strings.TrimSpace(msg.Content) != "" {
			return chatSubmitResultMsg{response: msg.Content}
		}
	}

	return chatSubmitResultMsg{response: "(no response)"}
}

func (m *ChatModel) addMessage(role, content string) {
	m.messages = append(m.messages, chatMessage{
		Role:    role,
		Content: content,
		SentAt:  time.Now(),
	})
	// Trim if too many messages.
	if len(m.messages) > chatMaxMessages {
		m.messages = m.messages[len(m.messages)-chatMaxMessages:]
	}
}

func (m ChatModel) maxScroll() int {
	chatHeight := m.height - 5
	if chatHeight < 1 {
		chatHeight = 1
	}
	totalLines := 0
	for range m.messages {
		totalLines += 3 // role line + content + blank
	}
	if totalLines <= chatHeight {
		return 0
	}
	return totalLines - chatHeight
}

func (m ChatModel) fetchAvailableSessions() tea.Cmd {
	client := m.client
	return func() tea.Msg {
		ctx := context.Background()
		sessions, err := client.GetSessions(ctx)
		if err != nil {
			return chatSessionsFetchResultMsg{err: err}
		}
		return chatSessionsFetchResultMsg{sessions: sessions.Items}
	}
}

func (m ChatModel) thinkingTickCmd() tea.Cmd {
	return tea.Tick(chatThinkingInterval, func(_ time.Time) tea.Msg {
		return chatThinkingTickMsg{}
	})
}

// ---------------------------------------------------------------------------
// Markdown rendering
// ---------------------------------------------------------------------------

// renderMarkdown applies basic markdown formatting to message content.
// Supports code blocks (```...```) and bold text (**...**).
func renderMarkdown(s string, width int) string {
	if width <= 0 {
		return s
	}

	var result strings.Builder
	lines := strings.Split(s, "\n")
	inCodeBlock := false

	for i, line := range lines {
		if i > 0 {
			result.WriteString("\n")
		}

		// Toggle code block state.
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") {
			inCodeBlock = !inCodeBlock
			if inCodeBlock {
				// Show the language hint if present.
				lang := strings.TrimPrefix(trimmed, "```")
				if lang != "" {
					result.WriteString(codeBlockStyle.Render(" " + lang + " "))
				}
			}
			continue
		}

		if inCodeBlock {
			// Render code block lines with a distinct background.
			padded := line
			if len(padded) < width {
				padded = padded + strings.Repeat(" ", width-len(padded))
			}
			result.WriteString(codeBlockStyle.Render(padded))
		} else {
			// Apply bold rendering for **text** markers.
			rendered := renderBold(line)
			wrapped := wrapText(rendered, width)
			result.WriteString(wrapped)
		}
	}

	return result.String()
}

// renderBold replaces **text** with bold styled text.
func renderBold(s string) string {
	var result strings.Builder
	for {
		start := strings.Index(s, "**")
		if start == -1 {
			result.WriteString(s)
			break
		}
		end := strings.Index(s[start+2:], "**")
		if end == -1 {
			result.WriteString(s)
			break
		}
		result.WriteString(s[:start])
		boldContent := s[start+2 : start+2+end]
		result.WriteString(boldTextStyle.Render(boldContent))
		s = s[start+2+end+2:]
	}
	return result.String()
}

// wrapText wraps a string to fit within the given width.
func wrapText(s string, width int) string {
	if width <= 0 {
		return s
	}
	var result strings.Builder
	for _, line := range strings.Split(s, "\n") {
		if len(line) <= width {
			if result.Len() > 0 {
				result.WriteString("\n")
			}
			result.WriteString(line)
			continue
		}
		for len(line) > width {
			// Find a good break point.
			breakAt := width
			spaceIdx := strings.LastIndex(line[:width], " ")
			if spaceIdx > width/2 {
				breakAt = spaceIdx + 1
			}
			if result.Len() > 0 {
				result.WriteString("\n")
			}
			result.WriteString(line[:breakAt])
			line = line[breakAt:]
		}
		if len(line) > 0 {
			if result.Len() > 0 {
				result.WriteString("\n")
			}
			result.WriteString(line)
		}
	}
	return result.String()
}

// Connected returns true if the client appears to be set (used by status bar).
func (m ChatModel) Connected() bool {
	return m.client != nil
}

// Render a padded column value for table-like output.
func padRight(s string, width int) string {
	if len(s) >= width {
		if width > 3 {
			return s[:width-3] + "..."
		}
		return s[:width]
	}
	return s + strings.Repeat(" ", width-len(s))
}

// renderTabBar renders the tab bar with the given tabs, highlighting activeTab.
func renderTabBar(tabs []string, activeTab int, width int) string {
	var rendered []string
	for i, tab := range tabs {
		label := fmt.Sprintf(" %d:%s ", i+1, tab)
		if i == activeTab {
			rendered = append(rendered, tabActiveStyle.Render(label))
		} else {
			rendered = append(rendered, tabInactiveStyle.Render(label))
		}
	}
	bar := lipgloss.JoinHorizontal(lipgloss.Top, rendered...)
	return tabBarStyle.Width(width).Render(bar)
}

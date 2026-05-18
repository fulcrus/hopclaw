package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// ---------------------------------------------------------------------------
// Constants
// ---------------------------------------------------------------------------

const (
	helpKeyColumnWidth = 22
	helpBoxMinWidth    = 30
	helpBoxPadding     = 4
	helpDescExtra      = 40
)

// ---------------------------------------------------------------------------
// Help Overlay
// ---------------------------------------------------------------------------

// helpOverlay renders a centered help box on top of content.
func helpOverlay(width, height int) string {
	title := "Keyboard Shortcuts"

	shortcuts := []struct {
		key  string
		desc string
	}{
		// Navigation.
		{"Tab / Shift+Tab", "Switch between tabs"},
		{"1-6", "Jump to tab by number"},
		{"Up / Down", "Scroll or navigate rows"},
		{"PgUp / PgDown", "Scroll by page"},
		{"j / k", "Navigate rows (non-Chat tabs)"},

		// Chat.
		{"Enter", "Send message (Chat tab)"},
		{"Up / Down", "Cycle command history (Chat input)"},
		{"Ctrl+S", "Cycle active session (Chat tab)"},
		{"Ctrl+B", "Toggle bash mode (Chat tab)"},
		{"Ctrl+L", "Clear chat messages (Chat tab)"},

		// Sessions.
		{"n", "Create new session (Sessions tab)"},
		{"d", "Delete selected session (Sessions tab)"},

		// Approvals.
		{"a", "Approve ticket (Approvals tab)"},
		{"d", "Deny ticket (Approvals tab)"},
		{"c", "Cancel ticket (Approvals tab)"},
		{"Enter", "View ticket details (Approvals tab)"},
		{"Ctrl+A", "Jump to Approvals (if pending)"},

		// General.
		{"r", "Refresh data (non-Chat tabs)"},
		{"? / F1", "Toggle this help overlay"},
		{"Esc", "Dismiss overlay / go back"},
		{"Ctrl+C", "Quit"},
		{"q", "Quit (when not in Chat tab)"},
	}

	// Build the help text.
	var b strings.Builder

	for _, sc := range shortcuts {
		keyText := helpKeyStyle.Render(padRight(sc.key, helpKeyColumnWidth))
		descText := helpDescStyle.Render(sc.desc)
		b.WriteString("  " + keyText + descText + "\n")
	}

	helpContent := b.String()

	// Build the box.
	boxWidth := helpKeyColumnWidth + helpDescExtra
	if boxWidth > width-helpBoxPadding {
		boxWidth = width - helpBoxPadding
	}
	if boxWidth < helpBoxMinWidth {
		boxWidth = helpBoxMinWidth
	}

	titleRendered := helpTitleStyle.Width(boxWidth).Render(title)
	contentRendered := helpContentStyle.Width(boxWidth).Render(helpContent)
	box := lipgloss.JoinVertical(lipgloss.Left, titleRendered, contentRendered)

	styledBox := helpBoxStyle.Render(box)

	// Center the box.
	boxHeight := lipgloss.Height(styledBox)
	boxWidthActual := lipgloss.Width(styledBox)

	topPad := (height - boxHeight) / 2
	if topPad < 0 {
		topPad = 0
	}
	leftPad := (width - boxWidthActual) / 2
	if leftPad < 0 {
		leftPad = 0
	}

	return lipgloss.NewStyle().
		MarginTop(topPad).
		MarginLeft(leftPad).
		Render(styledBox)
}

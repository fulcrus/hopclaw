package tui

import "github.com/charmbracelet/lipgloss"

// ---------------------------------------------------------------------------
// Color palette
// ---------------------------------------------------------------------------

const (
	colorAccent = lipgloss.Color("205")
	colorDimmed = lipgloss.Color("240")
	colorWhite  = lipgloss.Color("255")
	colorBlack  = lipgloss.Color("235")
	colorGreen  = lipgloss.Color("78")
	colorRed    = lipgloss.Color("196")
	colorYellow = lipgloss.Color("220")
	colorCyan   = lipgloss.Color("81")
	colorBlue   = lipgloss.Color("63")
)

// ---------------------------------------------------------------------------
// Tab bar styles
// ---------------------------------------------------------------------------

var (
	tabActiveStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(colorWhite).
			Background(colorAccent).
			Padding(0, 2)

	tabInactiveStyle = lipgloss.NewStyle().
				Foreground(colorDimmed).
				Padding(0, 2)

	tabBarStyle = lipgloss.NewStyle().
			PaddingBottom(1)
)

// ---------------------------------------------------------------------------
// Status bar styles
// ---------------------------------------------------------------------------

var (
	statusBarStyle = lipgloss.NewStyle().
			Foreground(colorWhite).
			Background(lipgloss.Color("236")).
			PaddingLeft(1).
			PaddingRight(1)

	statusConnectedStyle = lipgloss.NewStyle().
				Foreground(colorGreen).
				Bold(true)

	statusDisconnectedStyle = lipgloss.NewStyle().
				Foreground(colorRed).
				Bold(true)
)

// ---------------------------------------------------------------------------
// Chat message styles
// ---------------------------------------------------------------------------

var (
	userMsgStyle = lipgloss.NewStyle().
			Foreground(colorCyan).
			Bold(true)

	assistantMsgStyle = lipgloss.NewStyle().
				Foreground(colorGreen)

	systemMsgStyle = lipgloss.NewStyle().
			Foreground(colorYellow).
			Italic(true)

	timestampStyle = lipgloss.NewStyle().
			Foreground(colorDimmed)
)

// ---------------------------------------------------------------------------
// Table styles
// ---------------------------------------------------------------------------

var (
	tableHeaderStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(colorAccent).
				PaddingRight(2)

	tableRowStyle = lipgloss.NewStyle().
			PaddingRight(2)

	tableRowDimStyle = lipgloss.NewStyle().
				Foreground(colorDimmed).
				PaddingRight(2)
)

// ---------------------------------------------------------------------------
// General styles
// ---------------------------------------------------------------------------

var (
	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(colorAccent).
			MarginBottom(1)

	helpStyle = lipgloss.NewStyle().
			Foreground(colorDimmed)

	errorStyle = lipgloss.NewStyle().
			Foreground(colorRed)

	contentStyle = lipgloss.NewStyle().
			Padding(1, 2)
)

// ---------------------------------------------------------------------------
// Help overlay styles
// ---------------------------------------------------------------------------

var (
	helpBoxStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colorAccent).
			Padding(1, 2).
			Background(lipgloss.Color("234"))

	helpTitleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(colorAccent).
			Align(lipgloss.Center).
			MarginBottom(1)

	helpKeyStyle = lipgloss.NewStyle().
			Foreground(colorCyan).
			Bold(true)

	helpDescStyle = lipgloss.NewStyle().
			Foreground(colorWhite)

	helpContentStyle = lipgloss.NewStyle()
)

// ---------------------------------------------------------------------------
// Chat mode indicator styles
// ---------------------------------------------------------------------------

var (
	bashModeStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(colorBlack).
			Background(colorYellow).
			Padding(0, 1)

	chatModeStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(colorBlack).
			Background(colorCyan).
			Padding(0, 1)

	sessionNameStyle = lipgloss.NewStyle().
				Foreground(colorAccent).
				Bold(true)

	codeBlockStyle = lipgloss.NewStyle().
			Background(lipgloss.Color("236")).
			Foreground(colorWhite)

	boldTextStyle = lipgloss.NewStyle().
			Bold(true)
)

// ---------------------------------------------------------------------------
// Chat header styles
// ---------------------------------------------------------------------------

var (
	chatHeaderStyle = lipgloss.NewStyle().
		Foreground(colorDimmed).
		Italic(true)
)

// ---------------------------------------------------------------------------
// Modal styles
// ---------------------------------------------------------------------------

var (
	modalBoxStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colorAccent).
			Padding(1, 2).
			Background(lipgloss.Color("234"))

	modalTitleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(colorAccent).
			Align(lipgloss.Center).
			MarginBottom(1)

	modalButtonStyle = lipgloss.NewStyle().
				Foreground(colorDimmed).
				Border(lipgloss.NormalBorder()).
				BorderForeground(colorDimmed).
				Padding(0, 1)

	modalActiveButtonStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(colorWhite).
				Background(colorAccent).
				Padding(0, 1)
)

// ---------------------------------------------------------------------------
// Approval status styles
// ---------------------------------------------------------------------------

var (
	approvedStyle = lipgloss.NewStyle().
			Foreground(colorGreen).
			Bold(true)

	deniedStyle = lipgloss.NewStyle().
			Foreground(colorRed).
			Bold(true)

	pendingStyle = lipgloss.NewStyle().
			Foreground(colorYellow).
			Bold(true)

	cancelledStyle = lipgloss.NewStyle().
			Foreground(colorDimmed)
)

// ---------------------------------------------------------------------------
// Run status styles
// ---------------------------------------------------------------------------

var (
	runCompletedStyle = lipgloss.NewStyle().
				Foreground(colorGreen).
				PaddingRight(2)

	runFailedStyle = lipgloss.NewStyle().
			Foreground(colorRed).
			PaddingRight(2)

	runRunningStyle = lipgloss.NewStyle().
			Foreground(colorYellow).
			Bold(true).
			PaddingRight(2)

	runPendingStyle = lipgloss.NewStyle().
			Foreground(colorCyan).
			PaddingRight(2)
)

// ---------------------------------------------------------------------------
// Table selection styles
// ---------------------------------------------------------------------------

var (
	selectedRowStyle = lipgloss.NewStyle().
		Background(lipgloss.Color("237")).
		Foreground(colorWhite).
		Bold(true).
		PaddingRight(2)
)

// ---------------------------------------------------------------------------
// Viewport indicator styles
// ---------------------------------------------------------------------------

var (
	scrollUpStyle = lipgloss.NewStyle().
			Foreground(colorDimmed).
			Italic(true)

	scrollDownStyle = lipgloss.NewStyle().
			Foreground(colorDimmed).
			Italic(true)
)

package tui

import (
	"fmt"
	"strings"
)

// ---------------------------------------------------------------------------
// Constants
// ---------------------------------------------------------------------------

const (
	viewportScrollUpIndicator   = "  ▲ more above"
	viewportScrollDownIndicator = "  ▼ more below"

	tableDefaultColPadding = 2

	inputHistoryMaxItems = 50
)

// Default spinner frames.
var spinnerDefaultFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// ---------------------------------------------------------------------------
// Viewport — scrollable container with scroll indicators
// ---------------------------------------------------------------------------

// Viewport is a scrollable container that manages content lines and renders
// visible content with scroll indicators.
type Viewport struct {
	lines     []string
	scrollPos int
	width     int
	height    int
}

// NewViewport creates a new Viewport with the given dimensions.
func NewViewport(width, height int) Viewport {
	return Viewport{
		width:  width,
		height: height,
	}
}

// SetContent replaces the viewport content with the given lines.
func (v *Viewport) SetContent(lines []string) {
	v.lines = lines
	v.clampScroll()
}

// SetSize updates the viewport dimensions.
func (v *Viewport) SetSize(width, height int) {
	v.width = width
	v.height = height
	v.clampScroll()
}

// ScrollUp moves the viewport up by one line.
func (v *Viewport) ScrollUp() {
	if v.scrollPos > 0 {
		v.scrollPos--
	}
}

// ScrollDown moves the viewport down by one line.
func (v *Viewport) ScrollDown() {
	v.scrollPos++
	v.clampScroll()
}

// ScrollPageUp moves the viewport up by one page.
func (v *Viewport) ScrollPageUp(pageSize int) {
	v.scrollPos -= pageSize
	if v.scrollPos < 0 {
		v.scrollPos = 0
	}
}

// ScrollPageDown moves the viewport down by one page.
func (v *Viewport) ScrollPageDown(pageSize int) {
	v.scrollPos += pageSize
	v.clampScroll()
}

// ScrollToBottom scrolls the viewport to the bottom.
func (v *Viewport) ScrollToBottom() {
	v.scrollPos = v.maxScroll()
}

// View renders the visible portion of the viewport with scroll indicators.
func (v Viewport) View() string {
	if len(v.lines) == 0 {
		return ""
	}

	visibleHeight := v.height
	if visibleHeight < 1 {
		visibleHeight = 1
	}

	// Reserve lines for scroll indicators.
	contentHeight := visibleHeight
	hasUp := v.scrollPos > 0
	hasDown := v.scrollPos < v.maxScroll()

	if hasUp {
		contentHeight--
	}
	if hasDown {
		contentHeight--
	}
	if contentHeight < 1 {
		contentHeight = 1
	}

	start := v.scrollPos
	if start > len(v.lines)-contentHeight {
		start = len(v.lines) - contentHeight
	}
	if start < 0 {
		start = 0
	}
	end := start + contentHeight
	if end > len(v.lines) {
		end = len(v.lines)
	}

	var b strings.Builder
	if hasUp {
		b.WriteString(scrollUpStyle.Render(viewportScrollUpIndicator))
		b.WriteString("\n")
	}
	b.WriteString(strings.Join(v.lines[start:end], "\n"))
	if hasDown {
		b.WriteString("\n")
		b.WriteString(scrollDownStyle.Render(viewportScrollDownIndicator))
	}
	return b.String()
}

func (v Viewport) maxScroll() int {
	contentHeight := v.height
	if contentHeight < 1 {
		contentHeight = 1
	}
	if len(v.lines) <= contentHeight {
		return 0
	}
	return len(v.lines) - contentHeight
}

func (v *Viewport) clampScroll() {
	max := v.maxScroll()
	if v.scrollPos > max {
		v.scrollPos = max
	}
	if v.scrollPos < 0 {
		v.scrollPos = 0
	}
}

// ---------------------------------------------------------------------------
// TableColumn — column definition for the Table widget
// ---------------------------------------------------------------------------

// TableColumn defines a column in a Table.
type TableColumn struct {
	Title string
	Width int
}

// ---------------------------------------------------------------------------
// Table — reusable table widget with selection support
// ---------------------------------------------------------------------------

// Table is a reusable table widget with configurable columns, headers,
// row selection highlighting, and scroll support.
type Table struct {
	columns     []TableColumn
	rows        [][]string
	selectedIdx int
	scrollPos   int
	width       int
	height      int
}

// NewTable creates a new Table with the given columns.
func NewTable(columns []TableColumn) Table {
	return Table{
		columns:     columns,
		selectedIdx: -1,
	}
}

// SetRows replaces the table rows.
func (t *Table) SetRows(rows [][]string) {
	t.rows = rows
	t.clampSelection()
	t.clampScroll()
}

// SetSize updates the table dimensions.
func (t *Table) SetSize(width, height int) {
	t.width = width
	t.height = height
	t.clampScroll()
}

// SelectedIndex returns the current selected row index, or -1 if none.
func (t Table) SelectedIndex() int {
	return t.selectedIdx
}

// SelectUp moves the selection up by one row.
func (t *Table) SelectUp() {
	if t.selectedIdx > 0 {
		t.selectedIdx--
	} else if t.selectedIdx < 0 && len(t.rows) > 0 {
		t.selectedIdx = 0
	}
	t.ensureVisible()
}

// SelectDown moves the selection down by one row.
func (t *Table) SelectDown() {
	if t.selectedIdx < len(t.rows)-1 {
		t.selectedIdx++
	} else if t.selectedIdx < 0 && len(t.rows) > 0 {
		t.selectedIdx = 0
	}
	t.ensureVisible()
}

// ClearSelection removes the selection.
func (t *Table) ClearSelection() {
	t.selectedIdx = -1
}

// View renders the table with header and rows.
func (t Table) View() string {
	if len(t.columns) == 0 {
		return ""
	}

	var b strings.Builder

	// Render header.
	for _, col := range t.columns {
		b.WriteString(tableHeaderStyle.Render(padRight(col.Title, col.Width)))
	}
	b.WriteString("\n")

	if len(t.rows) == 0 {
		return b.String()
	}

	// Calculate visible area (subtract header line).
	visibleHeight := t.height - 1
	if visibleHeight < 1 {
		visibleHeight = 1
	}

	start := t.scrollPos
	if start > len(t.rows)-visibleHeight {
		start = len(t.rows) - visibleHeight
	}
	if start < 0 {
		start = 0
	}
	end := start + visibleHeight
	if end > len(t.rows) {
		end = len(t.rows)
	}

	for i := start; i < end; i++ {
		row := t.rows[i]
		isSelected := i == t.selectedIdx

		for j, col := range t.columns {
			cellValue := ""
			if j < len(row) {
				cellValue = row[j]
			}
			cell := padRight(cellValue, col.Width)
			if isSelected {
				b.WriteString(selectedRowStyle.Render(cell))
			} else {
				b.WriteString(tableRowStyle.Render(cell))
			}
		}
		b.WriteString("\n")
	}

	// Scroll info.
	if len(t.rows) > visibleHeight {
		b.WriteString(helpStyle.Render(
			fmt.Sprintf("showing %d-%d of %d", start+1, end, len(t.rows)),
		))
	}

	return b.String()
}

func (t *Table) clampSelection() {
	if len(t.rows) == 0 {
		t.selectedIdx = -1
		return
	}
	if t.selectedIdx >= len(t.rows) {
		t.selectedIdx = len(t.rows) - 1
	}
}

func (t *Table) clampScroll() {
	visibleHeight := t.height - 1
	if visibleHeight < 1 {
		visibleHeight = 1
	}
	max := len(t.rows) - visibleHeight
	if max < 0 {
		max = 0
	}
	if t.scrollPos > max {
		t.scrollPos = max
	}
	if t.scrollPos < 0 {
		t.scrollPos = 0
	}
}

func (t *Table) ensureVisible() {
	visibleHeight := t.height - 1
	if visibleHeight < 1 {
		visibleHeight = 1
	}
	if t.selectedIdx < t.scrollPos {
		t.scrollPos = t.selectedIdx
	}
	if t.selectedIdx >= t.scrollPos+visibleHeight {
		t.scrollPos = t.selectedIdx - visibleHeight + 1
	}
	t.clampScroll()
}

// ---------------------------------------------------------------------------
// InputHistory — command history with up/down arrow cycling
// ---------------------------------------------------------------------------

// InputHistory stores a history of input strings and supports cycling
// through them with up/down navigation.
type InputHistory struct {
	items []string
	pos   int // -1 means not browsing history; 0..len-1 is active position
	draft string
}

// NewInputHistory creates a new empty InputHistory.
func NewInputHistory() InputHistory {
	return InputHistory{
		pos: -1,
	}
}

// Push adds a new entry to the history. Duplicates of the most recent
// entry are not added.
func (h *InputHistory) Push(s string) {
	if s == "" {
		return
	}
	// Avoid duplicating the most recent entry.
	if len(h.items) > 0 && h.items[len(h.items)-1] == s {
		h.pos = -1
		return
	}
	h.items = append(h.items, s)
	if len(h.items) > inputHistoryMaxItems {
		h.items = h.items[len(h.items)-inputHistoryMaxItems:]
	}
	h.pos = -1
}

// Up moves to the previous (older) history entry. currentInput is saved
// as a draft so the user can return to it. Returns the history entry to
// display, or empty string if no history is available.
func (h *InputHistory) Up(currentInput string) (string, bool) {
	if len(h.items) == 0 {
		return "", false
	}
	if h.pos < 0 {
		// Start browsing; save current input as draft.
		h.draft = currentInput
		h.pos = len(h.items) - 1
	} else if h.pos > 0 {
		h.pos--
	} else {
		return h.items[h.pos], false
	}
	return h.items[h.pos], true
}

// Down moves to the next (newer) history entry. If we move past the end,
// the saved draft is returned.
func (h *InputHistory) Down() (string, bool) {
	if h.pos < 0 {
		return "", false
	}
	h.pos++
	if h.pos >= len(h.items) {
		// Past the end; return draft.
		h.pos = -1
		return h.draft, true
	}
	return h.items[h.pos], true
}

// Reset stops browsing history.
func (h *InputHistory) Reset() {
	h.pos = -1
	h.draft = ""
}

// ---------------------------------------------------------------------------
// Spinner — loading spinner with customizable frames
// ---------------------------------------------------------------------------

// Spinner is a loading spinner with customizable animation frames.
type Spinner struct {
	frames []string
	pos    int
}

// NewSpinner creates a new Spinner with the default frames.
func NewSpinner() Spinner {
	return Spinner{
		frames: spinnerDefaultFrames,
	}
}

// NewSpinnerWithFrames creates a new Spinner with custom frames.
func NewSpinnerWithFrames(frames []string) Spinner {
	if len(frames) == 0 {
		frames = spinnerDefaultFrames
	}
	return Spinner{frames: frames}
}

// Tick advances the spinner to the next frame.
func (s *Spinner) Tick() {
	s.pos = (s.pos + 1) % len(s.frames)
}

// View returns the current frame of the spinner.
func (s Spinner) View() string {
	return s.frames[s.pos]
}

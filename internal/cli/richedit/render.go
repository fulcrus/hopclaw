package richedit

import (
	"fmt"
	"io"
	"strings"
	"unicode/utf8"

	"github.com/mattn/go-runewidth"
)

func init() {
	// Modern terminals (iTerm2, Terminal.app, WezTerm, Alacritty) render East
	// Asian Ambiguous characters (box-drawing ─│┌┐, middle-dot ·, etc.) as
	// narrow (width 1), even under CJK locales. go-runewidth auto-detects
	// EastAsianWidth=true from the locale, causing all padding/alignment to
	// over-count these characters. Force narrow to match actual rendering.
	runewidth.DefaultCondition.EastAsianWidth = false
}

const (
	ansiCyan      = "\033[36m"
	ansiDim       = "\033[90m"
	ansiInvert    = "\033[7m"
	ansiReset     = "\033[0m"
	ansiHideCur   = "\033[?25l"
	ansiShowCur   = "\033[?25h"
	ansiClearLine = "\033[2K"
	ansiCRLF      = "\r\n"
)

// RenderState tracks the previous render for incremental updates.
type RenderState struct {
	lineCount int
	cursorRow int
}

type Chrome struct {
	Top    string
	Bottom string
}

type EditorView struct {
	Popup        *popupState
	Overlay      *OverlayPanel
	Expanded     *Token
	ExpandedMode expandedMode
	Status       string
	PastePrompt  string
	Chrome       Chrome
}

type renderPiece struct {
	prefix  string
	visible string
	suffix  string
}

type screenLine struct {
	builder strings.Builder
	width   int
	body    bool
}

// Render draws the editor content to the terminal and returns the new state.
func Render(out io.Writer, doc *Document, cursor Cursor, prompt string, prev RenderState, termWidth int, termHeight int, view EditorView) RenderState {
	fmt.Fprint(out, ansiHideCur)
	clearPreviousRender(out, prev)

	renderWidth := terminalRenderWidth(termWidth)
	lines := make([]string, 0, doc.LineCount()+8)

	bodyLines, bodyCursorRow, bodyCursorCol := renderDocumentRows(doc, cursor, prompt, renderWidth)
	attachmentLines := renderAttachmentRail(doc, cursor)
	popupLines := renderPopup(view.Popup, termWidth)
	overlayLines := renderOverlay(view.Overlay, termWidth)
	expandedLines := renderExpanded(view, termWidth)
	pastePromptLine := strings.TrimSpace(view.PastePrompt)
	statusLine := strings.TrimSpace(view.Status)

	nonBodyLines := len(attachmentLines) + len(popupLines) + len(overlayLines) + len(expandedLines)
	if strings.TrimSpace(view.Chrome.Top) != "" {
		nonBodyLines++
	}
	if pastePromptLine != "" {
		nonBodyLines++
	}
	if statusLine != "" {
		nonBodyLines++
	}
	if strings.TrimSpace(view.Chrome.Bottom) != "" {
		nonBodyLines++
	}

	bodyLines, bodyCursorRow = clipBodyRows(bodyLines, bodyCursorRow, bodyViewportRows(termHeight, nonBodyLines))

	cursorRow := 0
	cursorCol := bodyCursorCol
	if strings.TrimSpace(view.Chrome.Top) != "" {
		lines = append(lines, compactTerminalLine(view.Chrome.Top, renderWidth))
		cursorRow++
	}
	cursorRow += bodyCursorRow
	lines = append(lines, bodyLines...)

	for _, line := range attachmentLines {
		lines = append(lines, ansiDim+compactTerminalLine(line, renderWidth)+ansiReset)
	}

	for _, line := range popupLines {
		lines = append(lines, compactTerminalLine(line, renderWidth))
	}

	for _, line := range expandedLines {
		lines = append(lines, compactTerminalLine(line, renderWidth))
	}

	if pastePromptLine != "" {
		lines = append(lines, compactTerminalLine(pastePromptLine, renderWidth))
	}
	if statusLine != "" {
		lines = append(lines, ansiDim+compactTerminalLine(statusLine, renderWidth)+ansiReset)
	}
	if strings.TrimSpace(view.Chrome.Bottom) != "" {
		lines = append(lines, compactTerminalLine(view.Chrome.Bottom, renderWidth))
	}

	for _, line := range overlayLines {
		lines = append(lines, compactTerminalLine(line, renderWidth))
	}

	for i, line := range lines {
		if i > 0 {
			fmt.Fprint(out, ansiCRLF)
		}
		fmt.Fprint(out, line)
	}

	totalLines := len(lines)
	if totalLines == 0 {
		totalLines = 1
	}
	if cursorRow < totalLines-1 {
		fmt.Fprintf(out, "\033[%dA", totalLines-1-cursorRow)
	}
	fmt.Fprintf(out, "\r\033[%dC", cursorCol)
	fmt.Fprint(out, ansiShowCur)

	return RenderState{lineCount: totalLines, cursorRow: cursorRow}
}

func bodyViewportRows(termHeight int, nonBodyLines int) int {
	if termHeight <= 0 {
		return 0
	}
	available := termHeight - nonBodyLines
	if available < 1 {
		return 1
	}
	return available
}

func clipBodyRows(rows []string, cursorRow int, maxRows int) ([]string, int) {
	if maxRows <= 0 || len(rows) <= maxRows {
		return rows, cursorRow
	}

	start := 0
	switch {
	case cursorRow < maxRows:
		start = 0
	case cursorRow >= len(rows)-maxRows:
		start = len(rows) - maxRows
	default:
		start = cursorRow - maxRows/2
	}
	if start < 0 {
		start = 0
	}
	end := start + maxRows
	if end > len(rows) {
		end = len(rows)
		start = max(end-maxRows, 0)
	}
	return rows[start:end], cursorRow - start
}

func renderDocumentRows(doc *Document, cursor Cursor, prompt string, termWidth int) ([]string, int, int) {
	promptWidth := runewidth.StringWidth(prompt)
	wrapPrefix := strings.Repeat(" ", promptWidth)
	rows := make([]string, 0, doc.LineCount()+2)
	cursorRow := 0
	cursorCol := promptWidth

	for i, line := range doc.Lines {
		prefix := prompt
		if i > 0 {
			prefix = strings.Repeat(" ", promptWidth)
		}
		current := newScreenLine(prefix)

		if i == cursor.Line && cursor.Col == 0 {
			cursorRow = len(rows)
			cursorCol = current.width
		}

		for j, tok := range line {
			piece := renderTokenPiece(tok, i == cursor.Line && j == cursor.Col)
			current = wrapScreenLineForPiece(current, &rows, pieceWidth(piece), termWidth, wrapPrefix)
			if i == cursor.Line && j == cursor.Col {
				cursorRow = len(rows)
				cursorCol = current.width
			}
			current = appendWrappedPiece(current, &rows, piece, termWidth, wrapPrefix)
		}

		if i == cursor.Line && cursor.Col >= len(line) {
			cursorRow = len(rows)
			cursorCol = current.width
		}

		rows = append(rows, current.String())
	}

	if len(rows) == 0 {
		rows = append(rows, prompt)
	}

	return rows, cursorRow, cursorCol
}

func renderAttachmentRail(doc *Document, cursor Cursor) []string {
	items := doc.Attachments()
	if len(items) == 0 {
		return nil
	}
	selected := cursor.TokenAtCursor(doc)
	if selected == nil || !selected.IsAttachment() {
		return nil
	}
	return []string{"selected: " + selected.RailText()}
}

func renderTokenPiece(tok Token, selected bool) renderPiece {
	if !tok.IsAttachment() {
		return renderPiece{visible: string(tok.Rune)}
	}
	piece := renderPiece{
		prefix:  ansiCyan,
		visible: tok.DisplayText(),
		suffix:  ansiReset,
	}
	if selected {
		piece.prefix = ansiInvert
	}
	return piece
}

func renderPopup(popup *popupState, termWidth int) []string {
	if popup == nil {
		return nil
	}
	lines := []string{fmt.Sprintf("@ attachments: %s", popup.query)}
	if len(popup.items) == 0 {
		lines = append(lines, "  (no matches)")
		return lines
	}
	for i, item := range popup.items {
		prefix := "  "
		if i == popup.selected {
			prefix = "> "
		}
		line := prefix + item.Label
		if strings.TrimSpace(item.Detail) != "" {
			line += "  " + item.Detail
		}
		lines = append(lines, compactLine(line, termWidth))
		if i >= 5 {
			break
		}
	}
	return lines
}

func renderExpanded(view EditorView, termWidth int) []string {
	if view.Expanded == nil {
		return nil
	}
	title := "details:"
	if view.ExpandedMode == expandedPreview {
		title = "preview:"
	}
	lines := []string{title}
	for _, line := range view.Expanded.DetailLines() {
		lines = append(lines, "  "+compactLine(line, termWidth))
	}
	return lines
}

func renderOverlay(panel *OverlayPanel, termWidth int) []string {
	if panel == nil {
		return nil
	}
	title := strings.TrimSpace(panel.Title)
	if title == "" {
		title = "Panel"
	}
	summary := strings.TrimSpace(panel.Summary)
	actions := strings.TrimSpace(panel.Actions)
	tip := strings.TrimSpace(panel.Tip)
	if termWidth < 80 {
		lines := []string{"[panel] " + title}
		if summary != "" {
			lines = append(lines, summary)
		}
		for _, line := range panel.Lines {
			text := strings.TrimSpace(line.Text)
			if text == "" {
				lines = append(lines, "")
				continue
			}
			prefix := "  "
			if line.Selected {
				prefix = "> "
			}
			lines = append(lines, prefix+text)
		}
		if actions != "" {
			lines = append(lines, "Actions: "+actions)
		}
		if tip != "" {
			lines = append(lines, "Tip: "+tip)
		}
		return lines
	}

	innerWidth := overlayInnerWidth(panel, termWidth)
	lines := []string{
		"┌" + strings.Repeat("─", innerWidth) + "┐",
		renderOverlayBoxLine(innerWidth, compactVisibleLine(title, innerWidth-2)),
	}
	if summary != "" {
		lines = append(lines, renderOverlayBoxLine(innerWidth, compactVisibleLine(summary, innerWidth-2)))
	}
	lines = append(lines, "├"+strings.Repeat("─", innerWidth)+"┤")
	for _, line := range panel.Lines {
		text := line.Text
		if line.Selected {
			text = ansiInvert + compactVisibleLine(text, innerWidth-2) + ansiReset
		} else {
			text = compactVisibleLine(text, innerWidth-2)
		}
		if strings.TrimSpace(line.Text) == "" {
			text = ""
		}
		lines = append(lines, renderOverlayBoxLine(innerWidth, text))
	}
	if actions != "" || tip != "" {
		lines = append(lines, "├"+strings.Repeat("─", innerWidth)+"┤")
		if actions != "" {
			lines = append(lines, renderOverlayBoxLine(innerWidth, compactVisibleLine("Actions: "+actions, innerWidth-2)))
		}
		if tip != "" {
			lines = append(lines, renderOverlayBoxLine(innerWidth, compactVisibleLine("Tip: "+tip, innerWidth-2)))
		}
	}
	lines = append(lines, "└"+strings.Repeat("─", innerWidth)+"┘")
	return lines
}

func overlayInnerWidth(panel *OverlayPanel, termWidth int) int {
	maxContentWidth := 0
	push := func(text string) {
		maxContentWidth = max(maxContentWidth, visibleWidth(strings.TrimSpace(text)))
	}

	if panel == nil {
		return 44
	}
	push(panel.Title)
	push(panel.Summary)
	for _, line := range panel.Lines {
		push(line.Text)
	}
	if strings.TrimSpace(panel.Actions) != "" {
		push("Actions: " + panel.Actions)
	}
	if strings.TrimSpace(panel.Tip) != "" {
		push("Tip: " + panel.Tip)
	}

	available := max(terminalRenderWidth(termWidth)-2, 1)
	desired := maxContentWidth + 2
	return min(max(desired, 44), min(available, 96))
}

func compactLine(text string, width int) string {
	text = strings.TrimRight(text, "\r\n")
	if width <= 0 {
		return text
	}
	if runewidth.StringWidth(text) <= width {
		return text
	}
	for runewidth.StringWidth(text+"…") > width && len(text) > 0 {
		_, size := utf8.DecodeLastRuneInString(text)
		text = text[:len(text)-size]
	}
	return text + "…"
}

func compactTerminalLine(text string, width int) string {
	text = strings.TrimRight(text, "\r\n")
	if width <= 0 || text == "" {
		return text
	}
	if visibleWidth(text) <= width {
		return text
	}
	if !strings.Contains(text, "\033") {
		return compactLine(text, width)
	}
	return compactVisibleLine(stripANSI(text), width)
}

func stripANSI(text string) string {
	if text == "" {
		return ""
	}
	var builder strings.Builder
	builder.Grow(len(text))

	inEscape := false
	for _, r := range text {
		if inEscape {
			if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
				inEscape = false
			}
			continue
		}
		if r == '\033' {
			inEscape = true
			continue
		}
		builder.WriteRune(r)
	}
	return builder.String()
}

func visibleWidth(text string) int {
	return runewidth.StringWidth(stripANSI(text))
}

func compactVisibleLine(text string, width int) string {
	text = strings.TrimSpace(text)
	if width <= 0 || text == "" {
		return text
	}
	if runewidth.StringWidth(text) <= width {
		return text
	}
	for runewidth.StringWidth(text+"…") > width && len(text) > 0 {
		_, size := utf8.DecodeLastRuneInString(text)
		text = text[:len(text)-size]
	}
	return text + "…"
}

func renderOverlayBoxLine(innerWidth int, content string) string {
	if innerWidth <= 0 {
		return "││"
	}
	return "│ " + padVisibleRight(content, max(innerWidth-2, 0)) + " │"
}

func padVisibleRight(text string, width int) string {
	text = strings.TrimRight(text, "\r\n")
	current := visibleWidth(text)
	if current >= width {
		return text
	}
	return text + strings.Repeat(" ", width-current)
}

func terminalRenderWidth(termWidth int) int {
	switch {
	case termWidth <= 1:
		return 1
	default:
		// Keep one spare column so the editor never depends on terminal auto-wrap.
		return termWidth - 1
	}
}

func newScreenLine(prefix string) *screenLine {
	line := &screenLine{width: runewidth.StringWidth(prefix)}
	line.builder.WriteString(prefix)
	return line
}

func (l *screenLine) String() string {
	if l == nil {
		return ""
	}
	return l.builder.String()
}

func pieceWidth(piece renderPiece) int {
	return runewidth.StringWidth(piece.visible)
}

func wrapScreenLineForPiece(current *screenLine, rows *[]string, width, termWidth int, wrapPrefix string) *screenLine {
	if current == nil {
		current = newScreenLine("")
	}
	if termWidth <= 0 {
		return current
	}
	if current.body && width > termWidth-current.width {
		*rows = append(*rows, current.String())
		return newScreenLine(wrapPrefix)
	}
	return current
}

func appendWrappedPiece(current *screenLine, rows *[]string, piece renderPiece, termWidth int, wrapPrefix string) *screenLine {
	if current == nil {
		current = newScreenLine("")
	}
	if piece.visible == "" {
		return current
	}
	if termWidth <= 0 {
		current.builder.WriteString(piece.prefix)
		current.builder.WriteString(piece.visible)
		current.builder.WriteString(piece.suffix)
		current.width += pieceWidth(piece)
		return current
	}

	remainingText := piece.visible
	for remainingText != "" {
		if current.width > 0 && current.width >= termWidth {
			*rows = append(*rows, current.String())
			current = newScreenLine(wrapPrefix)
		}

		available := termWidth - current.width
		chunk := remainingText
		if runewidth.StringWidth(chunk) > available {
			chunk = takeVisiblePrefix(chunk, available)
			if chunk == "" {
				if current.width > 0 {
					*rows = append(*rows, current.String())
					current = newScreenLine(wrapPrefix)
					continue
				}
				chunk = firstRuneString(remainingText)
			}
		}

		current.builder.WriteString(piece.prefix)
		current.builder.WriteString(chunk)
		current.builder.WriteString(piece.suffix)
		current.width += runewidth.StringWidth(chunk)
		current.body = true
		remainingText = remainingText[len(chunk):]

		if remainingText != "" {
			*rows = append(*rows, current.String())
			current = newScreenLine(wrapPrefix)
		}
	}

	return current
}

func takeVisiblePrefix(text string, width int) string {
	if width <= 0 || text == "" {
		return ""
	}
	if runewidth.StringWidth(text) <= width {
		return text
	}

	end := 0
	used := 0
	for i, r := range text {
		w := runewidth.RuneWidth(r)
		if w < 0 {
			w = 0
		}
		if used+w > width {
			break
		}
		used += w
		end = i + utf8.RuneLen(r)
	}
	if end <= 0 {
		return ""
	}
	return text[:end]
}

func firstRuneString(text string) string {
	if text == "" {
		return ""
	}
	_, size := utf8.DecodeRuneInString(text)
	if size <= 0 {
		return ""
	}
	return text[:size]
}

// ClearRender clears all lines of the previous render.
func ClearRender(out io.Writer, prev RenderState) {
	clearPreviousRender(out, prev)
}

func clearPreviousRender(out io.Writer, prev RenderState) {
	if prev.cursorRow > 0 {
		fmt.Fprintf(out, "\033[%dA", prev.cursorRow)
	}
	fmt.Fprint(out, "\r")
	for i := 0; i < prev.lineCount; i++ {
		fmt.Fprint(out, ansiClearLine)
		if i+1 < prev.lineCount {
			fmt.Fprint(out, "\033[1B\r")
		}
	}
	if prev.lineCount > 1 {
		fmt.Fprintf(out, "\033[%dA", prev.lineCount-1)
	}
	fmt.Fprint(out, "\r")
}

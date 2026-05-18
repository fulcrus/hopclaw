package repl

import (
	"fmt"
	"os"
	"strings"
	"time"

	badgepkg "github.com/fulcrus/hopclaw/internal/cli/badge"
)

type CardRow struct {
	Label  string
	Value  string
	Status string
}

type CardSpec struct {
	Title    string
	Subtitle string
	Rows     []CardRow
	Actions  string
	Footer   string
	Width    int
}

type PanelSpec struct {
	Title   string
	Summary string
	Meta    string
	Body    []string
	Actions string
	Tip     string
	Width   int
}

type railChip struct {
	Key   string
	Label string
	Tone  string
}

func (r *Renderer) RenderCard(spec CardSpec) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.stopSpinnerLocked()
	r.flushToolEventTrackerLocked()
	r.flushCurrentLineLocked(true)
	r.renderCardLocked(spec)
}

func (r *Renderer) RenderPanelSpec(spec PanelSpec) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.stopSpinnerLocked()
	r.flushToolEventTrackerLocked()
	r.flushCurrentLineLocked(true)
	r.renderPanelSpecLocked(spec)
}

func (r *Renderer) renderCardLocked(spec CardSpec) {
	title := strings.TrimSpace(spec.Title)
	if title == "" {
		title = "Card"
	}
	header := title
	if subtitle := strings.TrimSpace(spec.Subtitle); subtitle != "" {
		header += " · " + subtitle
	}
	labelWidth := 0
	body := make([]string, 0, len(spec.Rows)+2)
	for _, row := range spec.Rows {
		labelWidth = max(labelWidth, len(strings.TrimSpace(row.Label)))
	}
	for _, row := range spec.Rows {
		label := strings.TrimSpace(row.Label)
		value := strings.TrimSpace(row.Value)
		if value == "" {
			value = "-"
		}
		if label == "" {
			body = append(body, value)
			continue
		}
		body = append(body, fmt.Sprintf("%-*s  %s", labelWidth, label, value))
	}
	if footer := strings.TrimSpace(spec.Footer); footer != "" {
		body = append(body, footer)
	}

	width := spec.Width
	if width <= 0 {
		width = 72
	}
	termWidth := terminalWidthFallback()
	width = min(width, termWidth-2)
	if !r.tty {
		fmt.Fprintf(r.statusWriter(), "[card] %s\n", header)
		for _, line := range body {
			fmt.Fprintln(r.statusWriter(), compact(line, max(32, width)))
		}
		if actions := strings.TrimSpace(spec.Actions); actions != "" {
			fmt.Fprintf(r.statusWriter(), "Actions: %s\n", compact(actions, max(24, width-9)))
		}
		return
	}

	// Plain text card with bold title — no box-drawing borders.
	fmt.Fprintf(r.statusWriter(), "\033[1m%s\033[0m\n", compact(header, width))
	for _, line := range body {
		if strings.TrimSpace(line) == "" {
			continue
		}
		fmt.Fprintf(r.statusWriter(), "  %s\n", compact(line, max(32, width-2)))
	}
	if actions := strings.TrimSpace(spec.Actions); actions != "" {
		fmt.Fprintf(r.statusWriter(), "\033[90m  Actions: %s\033[0m\n", compact(actions, max(24, width-11)))
	}
}

func (r *Renderer) renderPanelSpecLocked(spec PanelSpec) {
	title := defaultString(spec.Title, "Panel")
	width := spec.Width
	if width <= 0 {
		width = 84
	}
	width = min(max(width, 56), 116)

	if !r.tty || width < 72 {
		fmt.Fprintf(r.statusWriter(), "[panel] %s\n", title)
		if summary := strings.TrimSpace(spec.Summary); summary != "" {
			fmt.Fprintln(r.statusWriter(), compact(summary, max(32, width)))
		}
		if meta := strings.TrimSpace(spec.Meta); meta != "" {
			fmt.Fprintln(r.statusWriter(), compact(meta, max(32, width)))
		}
		for _, line := range spec.Body {
			if strings.TrimSpace(line) == "" {
				fmt.Fprintln(r.statusWriter())
				continue
			}
			fmt.Fprintln(r.statusWriter(), compact(line, max(32, width)))
		}
		if actions := strings.TrimSpace(spec.Actions); actions != "" {
			fmt.Fprintf(r.statusWriter(), "Actions: %s\n", compact(actions, max(32, width)))
		}
		if tip := strings.TrimSpace(spec.Tip); tip != "" {
			fmt.Fprintf(r.statusWriter(), "Tip: %s\n", compact(tip, max(32, width)))
		}
		return
	}

	termWidth := terminalWidthFallback()
	width = min(width, termWidth-2)

	// Plain text panel — no box-drawing borders.
	fmt.Fprintf(r.statusWriter(), "\033[1m%s\033[0m\n", compact(title, width))
	if summary := strings.TrimSpace(spec.Summary); summary != "" {
		fmt.Fprintf(r.statusWriter(), "  %s\n", compact(summary, max(32, width-2)))
	}
	if meta := strings.TrimSpace(spec.Meta); meta != "" {
		fmt.Fprintf(r.statusWriter(), "\033[90m  %s\033[0m\n", compact(meta, max(32, width-2)))
	}
	for _, line := range spec.Body {
		if strings.TrimSpace(line) == "" {
			continue
		}
		fmt.Fprintf(r.statusWriter(), "  %s\n", compact(line, max(32, width-2)))
	}
	if actions := strings.TrimSpace(spec.Actions); actions != "" {
		fmt.Fprintf(r.statusWriter(), "\033[90m  %s\033[0m\n", compact(actions, max(32, width-2)))
	}
	if tip := strings.TrimSpace(spec.Tip); tip != "" {
		fmt.Fprintf(r.statusWriter(), "\033[90m  %s\033[0m\n", compact(tip, max(32, width-2)))
	}
}

// WorkbenchLines returns the workbench rail lines for the given state.
func WorkbenchLines(state REPLViewState, mode LayoutMode, tty bool) []string {
	return workbenchLines(state, mode, tty)
}

func workbenchLines(state REPLViewState, mode LayoutMode, tty bool) []string {
	if mode == LayoutPlain || !tty {
		status := normalizedExecutionState(state.ExecutionState)
		if status == "" {
			status = "ready"
		}
		return []string{
			fmt.Sprintf("[state] runtime=%s model=%s status=%s ctx=%s",
				targetConnectionLabel(state.Target, state.TargetKind),
				defaultString(state.Model, "(default)"),
				status,
				percentString(state.ContextPercent),
			),
		}
	}

	width := state.TerminalWidth
	if width <= 0 {
		width = 80
	}

	if mode == LayoutMinimal {
		statusLeft, statusRight := workbenchStatusLine(state, mode, tty)
		// In minimal mode, append hint to the right side when actionable
		if hint := compactHintForState(state); hint != "" && hint != "/help" {
			if statusRight != "" {
				statusRight += " · " + hint
			} else {
				statusRight = hint
			}
		}
		return []string{statusTextLine(statusLeft, statusRight, width, tty)}
	}

	topRail, bottomRail := workbenchChrome(state, mode, tty, width)
	lines := make([]string, 0, 2)
	if strings.TrimSpace(topRail) != "" {
		lines = append(lines, topRail)
	}
	if strings.TrimSpace(bottomRail) != "" {
		lines = append(lines, bottomRail)
	}
	return lines
}

func workbenchChrome(state REPLViewState, mode LayoutMode, tty bool, width int) (string, string) {
	if width <= 0 {
		width = 80
	}
	topLeft, topRight := workbenchTopRail(state, mode, tty)
	bottomLeft, bottomRight := workbenchBottomRail(state, mode, width-railFrameOverhead)
	return framedRail(topLeft, topRight, width), framedRail(bottomLeft, bottomRight, width)
}

func workbenchTopRail(state REPLViewState, mode LayoutMode, tty bool) (string, string) {
	execution := normalizedExecutionState(state.ExecutionState)
	emphasis := topRailPrimaryEmphasis(state)
	chips := make([]railChip, 0, 4)
	remoteChip := railChip{Key: "remote", Label: remoteChipLabel(state.Target, state.TargetKind), Tone: "34"}
	modelChip := railChip{Key: "model", Label: "MODEL " + defaultString(state.Model, "(default)"), Tone: "36"}

	switch execution {
	case "waiting approval":
		chips = append(chips, railChip{Key: "state", Label: "WAITING APPROVAL", Tone: "33"}, remoteChip)
		if risk := strings.TrimSpace(state.ApprovalRisk); risk != "" {
			chips = append(chips, railChip{Key: "risk", Label: "RISK " + strings.ToUpper(risk), Tone: "31"})
		}
	case "paused":
		chips = append(chips, railChip{Key: "state", Label: "PAUSED", Tone: "33"})
		if state.Resumable {
			chips = append(chips, railChip{Key: "resumable", Label: "RESUMABLE", Tone: "36"})
		}
		chips = append(chips, remoteChip)
	case "completed":
		if strings.EqualFold(strings.TrimSpace(state.DeliveryState), "retrying") {
			chips = append(chips,
				railChip{Key: "state", Label: "COMPLETED", Tone: "32"},
				railChip{Key: "delivery", Label: "DELIVERY RETRY", Tone: "33"},
				remoteChip,
			)
		} else {
			chips = append(chips, remoteChip, railChip{Key: "state", Label: "COMPLETED", Tone: "32"}, modelChip)
		}
	case "error":
		chips = append(chips, remoteChip, railChip{Key: "error", Label: "ERROR", Tone: "31"}, modelChip)
	case "running":
		chips = append(chips, remoteChip, railChip{Key: "state", Label: "RUNNING", Tone: "32"}, modelChip)
	default:
		chips = append(chips, remoteChip, modelChip, railChip{Key: "state", Label: "READY", Tone: "32"})
	}
	if strings.EqualFold(strings.TrimSpace(state.Think), "on") {
		chips = append(chips, railChip{Key: "think", Label: "THINK ON", Tone: "36"})
	}
	switch strings.ToLower(strings.TrimSpace(state.Health)) {
	case "degraded":
		chips = append(chips, railChip{Key: "health", Label: "HEALTH DEGRADED", Tone: "33"})
	case "blocked", "error":
		chips = append(chips, railChip{Key: "health", Label: "HEALTH BLOCKED", Tone: "31"})
	}
	if showSupervisorSummary(state) {
		chips = append(chips,
			railChip{Key: "fg", Label: fmt.Sprintf("FG %d", max(state.ForegroundRunCount, 0)), Tone: "36"},
			railChip{Key: "bg", Label: fmt.Sprintf("BG %d", max(state.BackgroundRunCount, 0)), Tone: "90"},
			railChip{Key: "paused_count", Label: fmt.Sprintf("PAUSED %d", max(state.PausedRunCount, 0)), Tone: "33"},
			railChip{Key: "attention", Label: fmt.Sprintf("ATTN %d", max(state.AttentionCount, 0)), Tone: "33"},
		)
	}

	left := make([]string, 0, len(chips))
	for _, chip := range chips {
		highlight := chip.Key == emphasis
		left = append(left, renderRailChip(tty, chip.Label, chip.Tone, highlight))
	}

	right := ""
	switch execution {
	case "running":
		right = "elapsed " + defaultString(state.Elapsed, "00:00")
	case "waiting approval":
		if id := strings.TrimSpace(state.ApprovalID); id != "" {
			right = "approval " + id
		} else {
			count := max(state.ApprovalCount, 1)
			right = fmt.Sprintf("approval %d", count)
		}
	case "paused":
		right = defaultString(state.RunID, "paused")
	case "completed":
		if strings.EqualFold(strings.TrimSpace(state.DeliveryState), "retrying") && strings.TrimSpace(state.DeliveryNext) != "" {
			right = "next " + state.DeliveryNext
		} else {
			right = "duration " + defaultString(state.Duration, "00:00")
		}
	case "error":
		right = defaultString(state.Duration, state.Elapsed)
	default:
		right = "ctx " + percentString(state.ContextPercent)
	}
	return strings.Join(left, " "), right
}

func showSupervisorSummary(state REPLViewState) bool {
	if state.ForegroundRunCount > 1 {
		return true
	}
	if state.BackgroundRunCount > 0 || state.PausedRunCount > 0 || state.AttentionCount > 0 {
		return true
	}
	return false
}

type bottomRailSegment struct {
	Text     string
	MinWidth int
}

func workbenchBottomRail(state REPLViewState, mode LayoutMode, railWidth int) (string, string) {
	cwd := abbreviatePath(state.CWD, 30)
	git := gitSummary(state)
	session := sessionDisplayKey(state.SessionKey)
	fullHint := hintForState(state)
	shortHint := compactHintForState(state)
	switch normalizedExecutionState(state.ExecutionState) {
	case "running":
		segments := []bottomRailSegment{{Text: defaultString(state.LastTool, "working"), MinWidth: 12}}
		shrinkOrder := []int{0}
		if session != "" {
			segments = append(segments, bottomRailSegment{Text: "conv:" + session, MinWidth: 7})
			shrinkOrder = append(shrinkOrder, len(segments)-1)
		}
		if mode == LayoutCompact {
			if phase := strings.TrimSpace(state.Phase); phase != "" && !strings.EqualFold(phase, "idle") {
				segments = append(segments, bottomRailSegment{Text: prefixedValue("phase", phase), MinWidth: 12})
				shrinkOrder = append(shrinkOrder, len(segments)-1)
			}
		}
		return fitWorkbenchBottomRail(segments, shrinkOrder, fullHint, shortHint, railWidth)
	case "waiting approval":
		segments := []bottomRailSegment{
			{Text: defaultString(state.LastTool, "approval"), MinWidth: 10},
			{Text: prefixedValue("scope", defaultString(state.ScopeSummary, "scope review")), MinWidth: 12},
		}
		shrinkOrder := []int{1, 0}
		if session != "" {
			segments = append(segments, bottomRailSegment{Text: "conv:" + session, MinWidth: 7})
			shrinkOrder = append(shrinkOrder, len(segments)-1)
		}
		return fitWorkbenchBottomRail(segments, shrinkOrder, fullHint, shortHint, railWidth)
	case "paused":
		segments := []bottomRailSegment{
			{Text: "last step " + defaultString(state.LastTool, "unknown"), MinWidth: 12},
		}
		shrinkOrder := []int{0}
		if session != "" {
			segments = append(segments, bottomRailSegment{Text: "conv:" + session, MinWidth: 7})
			shrinkOrder = append(shrinkOrder, len(segments)-1)
		}
		if scope := strings.TrimSpace(state.ScopeSummary); scope != "" {
			segments = append(segments, bottomRailSegment{Text: scope, MinWidth: 10})
			shrinkOrder = append(shrinkOrder, len(segments)-1)
		}
		return fitWorkbenchBottomRail(segments, shrinkOrder, fullHint, shortHint, railWidth)
	case "completed":
		if strings.EqualFold(strings.TrimSpace(state.DeliveryState), "retrying") {
			return fitWorkbenchBottomRail([]bottomRailSegment{
				{Text: defaultString(state.DeliverySummary, "delivery retry"), MinWidth: 12},
			}, []int{0}, "/last receipts · dismiss attention", "/last", railWidth)
		}
		segments := []bottomRailSegment{{Text: tokenSummary(state), MinWidth: 10}}
		shrinkOrder := []int{0}
		if session != "" {
			segments = append(segments, bottomRailSegment{Text: "conv:" + session, MinWidth: 7})
			shrinkOrder = append(shrinkOrder, len(segments)-1)
		}
		return fitWorkbenchBottomRail(segments, shrinkOrder, "/last receipts", "/last", railWidth)
	case "error":
		left, right := fitWorkbenchBottomRail([]bottomRailSegment{
			{Text: defaultString(state.LastFailure, "Run ended with an error."), MinWidth: 12},
		}, []int{0}, "/doctor · /last", "/doctor", railWidth)
		return compact(left, 40), right
	default:
		return fitWorkbenchBottomRail([]bottomRailSegment{
			{Text: cwd, MinWidth: 12},
			{Text: prefixedValue("git", git), MinWidth: 8},
			{Text: "conv:" + session, MinWidth: 7},
		}, []int{2, 0, 1}, fullHint, shortHint, railWidth)
	}
}

func hintForState(state REPLViewState) string {
	switch normalizedExecutionState(state.ExecutionState) {
	case "running":
		return "Esc pause · /runs manage"
	case "paused":
		return "Enter continue · x discard · /retry"
	case "cancelled":
		return "/last · /runs recent"
	case "waiting approval":
		return "y approve · n deny · v details"
	case "completed":
		return "/last receipts"
	case "error":
		return "/doctor · /last"
	default:
		return "/help"
	}
}

func compactHintForState(state REPLViewState) string {
	switch normalizedExecutionState(state.ExecutionState) {
	case "running":
		return "Esc pause"
	case "paused":
		return "Enter continue"
	case "cancelled":
		return "/last"
	case "waiting approval":
		return "y approve"
	case "completed":
		return "/last"
	case "error":
		return "/doctor"
	default:
		return "/help"
	}
}

func prefixedValue(prefix, value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	return strings.TrimSpace(prefix) + ":" + value
}

func topRailPrimaryEmphasis(state REPLViewState) string {
	switch {
	case normalizedExecutionState(state.ExecutionState) == "error":
		return "error"
	case strings.EqualFold(strings.TrimSpace(state.Health), "blocked"), strings.EqualFold(strings.TrimSpace(state.Health), "error"):
		return "health"
	case strings.EqualFold(strings.TrimSpace(state.Health), "degraded"):
		return "health"
	case strings.EqualFold(strings.TrimSpace(state.DeliveryState), "retrying"):
		return "delivery"
	case state.AttentionCount > 0:
		return "attention"
	case normalizedExecutionState(state.ExecutionState) == "waiting approval",
		normalizedExecutionState(state.ExecutionState) == "paused",
		normalizedExecutionState(state.ExecutionState) == "running",
		normalizedExecutionState(state.ExecutionState) == "completed":
		return "state"
	case strings.TrimSpace(state.Target) != "":
		return "remote"
	default:
		return "model"
	}
}

func renderRailChip(tty bool, label, tone string, highlight bool) string {
	text := badgeText(strings.TrimSpace(label))
	if !tty || text == "" {
		return text
	}
	if !highlight {
		if tone == "90" {
			return renderBadge(true, text, "90")
		}
		return text
	}
	return renderBadge(true, text, tone)
}

// railFrameOverhead is the display width of the frame decorations "├─" + "─┤".
var railFrameOverhead = displayWidth("├─") + displayWidth("─┤")

func framedRail(left, right string, innerWidth int) string {
	content := padBetween(strings.TrimSpace(left), strings.TrimSpace(right), max(innerWidth-railFrameOverhead, 8))
	return "├─" + content + "─┤"
}

// terminalWidthFallback returns the current terminal width, defaulting to 80.
func terminalWidthFallback() int {
	width, _, err := termGetSize(int(os.Stdout.Fd()))
	if err != nil || width <= 0 {
		return 80
	}
	return width
}

// plainSeparator renders a simple full-width "─" line.
func plainSeparator(width int, tty bool) string {
	line := repeatToDisplayWidth("─", separatorRenderWidth(width))
	if tty {
		return "\033[90m" + line + "\033[0m"
	}
	return line
}

func separatorRenderWidth(width int) int {
	switch {
	case width > 4:
		return width - 4
	case width > 0:
		return width
	default:
		return 0
	}
}

// repeatToDisplayWidth repeats char until the resulting string fills targetWidth
// display columns. Handles wide characters (e.g. box-drawing U+2500 = width 2).
func repeatToDisplayWidth(char string, targetWidth int) string {
	if targetWidth <= 0 {
		return ""
	}
	cw := displayWidth(char)
	if cw <= 0 {
		cw = 1
	}
	return strings.Repeat(char, targetWidth/cw)
}

// visibleLen returns the terminal display width, stripping ANSI escape sequences.
func visibleLen(s string) int {
	return displayWidth(s)
}

// statusTextLine renders "→ left-info                           right-info" with color.
func statusTextLine(left, right string, width int, tty bool) string {
	left = strings.TrimSpace(left)
	right = strings.TrimSpace(right)
	arrow := "→"
	if tty {
		arrow = "\033[36m→\033[0m" // cyan arrow
	}
	prefix := arrow + " "
	prefixVisualLen := visibleLen(prefix)

	available := width - prefixVisualLen
	if available <= 0 {
		return prefix
	}
	if right == "" {
		return prefix + compactVisible(left, available)
	}
	rightVisLen := visibleLen(right)
	leftLimit := available - rightVisLen - 1
	if leftLimit < 4 {
		return prefix + compactVisible(left, available)
	}
	leftText := left
	if visibleLen(leftText) > leftLimit {
		leftText = compactVisible(leftText, leftLimit)
	}
	gap := available - visibleLen(leftText) - rightVisLen
	if gap < 1 {
		return prefix + compactVisible(left, available)
	}
	result := prefix + leftText + strings.Repeat(" ", gap) + right
	if tty && right != "" {
		result = prefix + leftText + strings.Repeat(" ", gap) + "\033[90m" + right + "\033[0m"
	}
	return result
}

// separatorLine renders "──── embedded-left ── embedded-right ────────" full-width.
func separatorLine(left, right string, width int, tty bool) string {
	width = separatorRenderWidth(width)
	left = strings.TrimSpace(left)
	right = strings.TrimSpace(right)

	if left == "" && right == "" {
		line := repeatToDisplayWidth("─", width)
		if tty {
			return "\033[90m" + line + "\033[0m"
		}
		return line
	}
	if width <= 10 {
		line := repeatToDisplayWidth("─", max(width, 0))
		if tty {
			return "\033[90m" + line + "\033[0m"
		}
		return line
	}

	embedded := left
	if right != "" {
		if embedded != "" {
			embedded += " ── " + right
		} else {
			embedded = right
		}
	}
	embedded = compact(embedded, width-10)

	embeddedLen := displayWidth(embedded)
	leftDash := 4
	rightDash := width - leftDash - embeddedLen - 2
	if rightDash < 4 {
		rightDash = 4
	}

	line := repeatToDisplayWidth("─", leftDash) + " " + embedded + " " + repeatToDisplayWidth("─", rightDash)
	if padding := width - displayWidth(line); padding > 0 {
		line += repeatToDisplayWidth("─", padding)
	}
	if tty {
		return "\033[90m" + line + "\033[0m"
	}
	return line
}

func asciiSeparatorLine(content string, width int, tty bool) string {
	content = strings.TrimSpace(content)
	width = separatorRenderWidth(width)
	if width <= 0 {
		width = 79
	}
	if content == "" {
		line := repeatToDisplayWidth("-", width)
		if tty {
			return "\033[90m" + line + "\033[0m"
		}
		return line
	}
	if width <= 20 {
		return asciiSeparatorLine("", width, tty)
	}
	content = compact(content, max(width-20, 8))
	contentWidth := displayWidth(content)
	leftDash := 8
	rightDash := width - leftDash - contentWidth - 2
	if rightDash < 8 {
		rightDash = 8
	}
	line := repeatToDisplayWidth("-", leftDash) + " " + content + " " + repeatToDisplayWidth("-", rightDash)
	if padding := width - displayWidth(line); padding > 0 {
		line += repeatToDisplayWidth("-", padding)
	}
	if tty {
		return "\033[90m" + line + "\033[0m"
	}
	return line
}

func promptWorkbenchChromeLines(state REPLViewState, width int, tty bool) (string, string) {
	return asciiSeparatorLine(promptWorkbenchTopSummary(state), width, tty),
		asciiSeparatorLine(promptWorkbenchBottomSummary(state), width, tty)
}

func promptWorkbenchTopSummary(state REPLViewState) string {
	execution := normalizedExecutionState(state.ExecutionState)
	switch execution {
	case "running":
		parts := []string{"working"}
		if elapsed := strings.TrimSpace(state.Elapsed); elapsed != "" {
			parts = append(parts, elapsed)
		}
		return joinNonEmpty(" · ", parts...)
	case "waiting approval":
		return "approval"
	case "paused":
		return "paused"
	case "completed":
		return "done"
	case "error":
		return "error"
	default:
		return ""
	}
}

func promptWorkbenchBottomSummary(state REPLViewState) string {
	execution := normalizedExecutionState(state.ExecutionState)
	switch execution {
	case "running":
		return joinNonEmpty(" · ",
			defaultString(state.LastTool, "working"),
			"Esc pause",
		)
	case "waiting approval":
		return "y approve · n deny · v details"
	case "paused":
		return "Enter continue · x discard"
	case "completed":
		return "follow-up · /last"
	case "error":
		return "/doctor · /last"
	default:
		return "@ attach · Ctrl+V · /help · /quit"
	}
}

// workbenchStatusLine builds the main status text line content.
func workbenchStatusLine(state REPLViewState, mode LayoutMode, tty bool) (left, right string) {
	execution := normalizedExecutionState(state.ExecutionState)
	target := defaultString(state.Target, "local")

	// Project + git context
	project := defaultString(state.Project, projectFromCWD(state.CWD))
	git := gitSummary(state)

	parts := make([]string, 0, 6)
	if tty {
		parts = append(parts, "\033[1m"+project+"\033[0m")
	} else {
		parts = append(parts, project)
	}
	if git != "" {
		if tty {
			parts = append(parts, "\033[32mgit:("+git+")\033[0m")
		} else {
			parts = append(parts, "git:("+git+")")
		}
	}

	// Target + Model + State
	parts = append(parts, targetConnectionLabel(target, state.TargetKind))

	model := defaultString(state.Model, "(default)")
	parts = append(parts, model)

	stateLabel := stateDisplayLabel(execution, state, tty)
	parts = append(parts, stateLabel)

	left = strings.Join(parts, " ")

	// Right side: counter
	switch execution {
	case "running":
		right = "elapsed " + defaultString(state.Elapsed, "00:00")
	case "waiting approval":
		right = fmt.Sprintf("approval %d", max(state.ApprovalCount, 1))
	case "completed":
		if state.Duration != "" {
			right = "duration " + state.Duration
		}
	case "paused":
		right = defaultString(state.RunID, "")
	default:
		if state.ContextPercent > 0 {
			right = fmt.Sprintf("ctx:%d%%", state.ContextPercent)
		}
	}
	return
}

// workbenchSeparatorContent returns optional embedded text for the separator line.
func workbenchSeparatorContent(state REPLViewState) (left, right string) {
	execution := normalizedExecutionState(state.ExecutionState)
	switch execution {
	case "running":
		left = defaultString(state.LastTool, "working")
		right = "Esc pause · Ctrl+C quit"
	case "waiting approval":
		left = defaultString(state.LastTool, "approval")
		right = "y approve · n deny · v details"
	case "paused":
		left = "last step " + defaultString(state.LastTool, "unknown")
		right = "Enter continue · x discard · /retry"
	case "cancelled":
		left = "run cancelled"
		right = "/last · /runs recent"
	case "completed":
		if state.PromptTokens > 0 || state.CompletionTokens > 0 {
			left = fmt.Sprintf("tokens %s / %s", formatTokenCount(state.PromptTokens), formatTokenCount(state.CompletionTokens))
		}
		right = "/last"
	case "error":
		left = compact(defaultString(state.LastFailure, "error"), 40)
		right = "/doctor · /retry"
	default:
		right = "/help"
	}
	return
}

func stateDisplayLabel(execution string, state REPLViewState, tty bool) string {
	label := ""
	switch execution {
	case "running":
		label = "RUNNING"
		if tty {
			return "\033[32m" + label + "\033[0m"
		}
	case "waiting approval":
		label = "WAITING APPROVAL"
		if state.ApprovalRisk != "" {
			label += " · " + strings.ToUpper(state.ApprovalRisk)
		}
		if tty {
			return "\033[33m" + label + "\033[0m"
		}
	case "paused":
		label = "PAUSED"
		if state.Resumable {
			label += " · RESUMABLE"
		}
		if tty {
			return "\033[33m" + label + "\033[0m"
		}
	case "cancelled":
		label = "CANCELLED"
		if tty {
			return "\033[31m" + label + "\033[0m"
		}
	case "completed":
		label = "COMPLETED"
		if tty {
			return "\033[32m" + label + "\033[0m"
		}
	case "error":
		label = "ERROR"
		if tty {
			return "\033[31m" + label + "\033[0m"
		}
	default:
		label = "ready"
		if tty {
			return "\033[90m" + label + "\033[0m"
		}
	}
	return label
}

func projectFromCWD(cwd string) string {
	if cwd == "" {
		return "hopclaw"
	}
	parts := strings.Split(cwd, "/")
	for i := len(parts) - 1; i >= 0; i-- {
		if p := strings.TrimSpace(parts[i]); p != "" {
			return p
		}
	}
	return "hopclaw"
}

func padBetween(left, right string, width int) string {
	if width <= 0 {
		return ""
	}
	left = strings.TrimSpace(left)
	right = strings.TrimSpace(right)
	if right == "" {
		return padRight(left, width)
	}
	if left == "" {
		return padLeft(right, width)
	}
	if displayWidth(left)+displayWidth(right)+1 >= width {
		return compact(left+" "+right, width)
	}
	return left + strings.Repeat(" ", width-displayWidth(left)-displayWidth(right)) + right
}

func padRight(text string, width int) string {
	if width <= 0 {
		return ""
	}
	text = strings.TrimSpace(text)
	if displayWidth(text) >= width {
		return displayPrefix(text, width)
	}
	return text + strings.Repeat(" ", width-displayWidth(text))
}

func padLeft(text string, width int) string {
	if width <= 0 {
		return ""
	}
	text = strings.TrimSpace(text)
	if displayWidth(text) >= width {
		return displaySuffix(text, width)
	}
	return strings.Repeat(" ", width-displayWidth(text)) + text
}

// WorkbenchPromptLabel returns the prompt label for the given state.
func WorkbenchPromptLabel(state REPLViewState) string {
	return workbenchPromptLabel(state)
}

func workbenchPromptLabel(state REPLViewState) string {
	switch normalizedExecutionState(state.ExecutionState) {
	case "waiting approval":
		return "approval>"
	case "paused":
		return "paused>"
	default:
		return ">"
	}
}

func remoteChipLabel(target, kind string) string {
	return targetChipLabel(target, kind)
}

func badgeVisible(state REPLViewState) bool {
	return strings.TrimSpace(state.Badge) != "" && !strings.EqualFold(strings.TrimSpace(state.Badge), "off")
}

func appendWorkbenchGutter(line string, gutterRows []string, index int) string {
	if len(gutterRows) == 0 {
		return line
	}
	if index < 0 || index >= len(gutterRows) {
		return line + " " + strings.Repeat(" ", len(gutterRows[0]))
	}
	return line + " " + gutterRows[index]
}

func resolveBadgeSize(size int) int {
	switch {
	case size < 2:
		return 4
	case size > 6:
		return 6
	default:
		return size
	}
}

func avatarGutterWidth(size int) int {
	size = resolveBadgeSize(size)
	if size >= 5 {
		return 8
	}
	return 6
}

func avatarGutterRows(badge string, size int) []string {
	badge = strings.TrimSpace(badge)
	if badge == "" || strings.EqualFold(badge, "off") {
		return nil
	}
	height := max(3, resolveBadgeSize(size))
	width := avatarGutterWidth(size)
	fill := avatarMonogramRune(badge)
	if fill == ' ' {
		return nil
	}
	if fill == '#' {
		return blockAvatarRows(fill, width, height)
	}
	return rasterAvatarRows(fill, width, height)
}

func sessionDisplayKey(key string) string {
	key = strings.TrimSpace(key)
	if key == "" {
		return "default"
	}
	switch {
	case displayWidth(key) > 20:
		return displayPrefix(key, 8) + displayEllipsis
	case displayWidth(key) > 16:
		return compact(key, 16)
	default:
		return key
	}
}

func fitWorkbenchBottomRail(segments []bottomRailSegment, shrinkOrder []int, fullRight, shortRight string, innerWidth int) (string, string) {
	width := max(innerWidth, 8)
	right := strings.TrimSpace(fullRight)
	shortRight = defaultString(strings.TrimSpace(shortRight), right)
	items := append([]bottomRailSegment(nil), segments...)

	for {
		left := joinBottomRailSegments(items)
		if railContentFits(left, right, width) {
			return left, right
		}
		if right != shortRight {
			right = shortRight
			continue
		}
		shrunk := false
		for _, index := range shrinkOrder {
			if index < 0 || index >= len(items) {
				continue
			}
			next, ok := shrinkBottomRailSegment(items[index])
			if !ok {
				continue
			}
			items[index] = next
			shrunk = true
			break
		}
		if shrunk {
			continue
		}
		limit := width
		if right != "" {
			limit = max(width-displayWidth(right)-1, 8)
		}
		return compact(left, limit), right
	}
}

func joinBottomRailSegments(segments []bottomRailSegment) string {
	parts := make([]string, 0, len(segments))
	for _, item := range segments {
		if text := strings.TrimSpace(item.Text); text != "" {
			parts = append(parts, text)
		}
	}
	return strings.Join(parts, " · ")
}

func railContentFits(left, right string, width int) bool {
	if width <= 0 {
		return false
	}
	left = strings.TrimSpace(left)
	right = strings.TrimSpace(right)
	switch {
	case left == "" && right == "":
		return true
	case right == "":
		return displayWidth(left) <= width
	case left == "":
		return displayWidth(right) <= width
	default:
		return displayWidth(left)+displayWidth(right)+1 < width
	}
}

func shrinkBottomRailSegment(segment bottomRailSegment) (bottomRailSegment, bool) {
	text := strings.TrimSpace(segment.Text)
	if text == "" {
		return segment, false
	}
	minWidth := max(segment.MinWidth, 4)
	if displayWidth(text) <= minWidth {
		return segment, false
	}
	nextLimit := max(minWidth, displayWidth(text)-4)
	if nextLimit >= displayWidth(text) {
		return segment, false
	}
	segment.Text = compact(text, nextLimit)
	return segment, true
}

func fitAvatarGutterRows(rows []string, target int) []string {
	if len(rows) == 0 || target <= 0 {
		return nil
	}
	if len(rows) <= target {
		return append([]string(nil), rows...)
	}
	trimmed := make([]string, 0, len(rows))
	for _, row := range rows {
		if strings.TrimSpace(row) != "" {
			trimmed = append(trimmed, row)
		}
	}
	if len(trimmed) == 0 {
		return nil
	}
	if len(trimmed) <= target {
		return append([]string(nil), trimmed...)
	}
	result := make([]string, 0, target)
	for i := 0; i < target; i++ {
		start := i * len(trimmed) / target
		end := (i + 1) * len(trimmed) / target
		if end <= start {
			end = start + 1
		}
		best := trimmed[start]
		bestFill := strings.Count(best, string([]byte{' '}))
		bestFill = len(best) - bestFill
		for _, candidate := range trimmed[start:end] {
			fill := len(candidate) - strings.Count(candidate, string([]byte{' '}))
			if fill > bestFill {
				best = candidate
				bestFill = fill
			}
		}
		result = append(result, best)
	}
	return result
}

func avatarMonogramRune(badge string) rune {
	badge = strings.TrimSpace(badge)
	switch {
	case badge == "":
		return ' '
	case strings.HasPrefix(strings.ToLower(badge), "custom-"):
		return '#'
	default:
		return []rune(strings.ToUpper(string([]rune(badge)[0])))[0]
	}
}

func rasterAvatarRows(letter rune, width, height int) []string {
	rows := make([]string, height)
	img := badgepkg.RenderLetter(letter, "#ffffff")
	bounds := img.Bounds()
	srcWidth := max(bounds.Dx(), 1)
	srcHeight := max(bounds.Dy(), 1)
	for y := 0; y < height; y++ {
		var row strings.Builder
		for x := 0; x < width; x++ {
			x0 := x * srcWidth / width
			x1 := max((x+1)*srcWidth/width, x0+1)
			y0 := y * srcHeight / height
			y1 := max((y+1)*srcHeight/height, y0+1)
			filled := false
			for py := y0; py < y1 && !filled; py++ {
				for px := x0; px < x1; px++ {
					if img.RGBAAt(bounds.Min.X+px, bounds.Min.Y+py).A > 0 {
						filled = true
						break
					}
				}
			}
			if filled {
				row.WriteRune(letter)
			} else {
				row.WriteByte(' ')
			}
		}
		rows[y] = row.String()
	}
	return rows
}

func blockAvatarRows(fill rune, width, height int) []string {
	rows := make([]string, height)
	coreWidth := max(2, width-2)
	for y := 0; y < height; y++ {
		runWidth := coreWidth
		if y == 0 || y == height-1 {
			runWidth = max(2, coreWidth-2)
		}
		left := max((width-runWidth)/2, 0)
		rows[y] = strings.Repeat(" ", left) + strings.Repeat(string(fill), runWidth) + strings.Repeat(" ", max(width-left-runWidth, 0))
	}
	return rows
}

func gitSummary(state REPLViewState) string {
	if branch := strings.TrimSpace(state.GitBranch); branch != "" {
		summary := branch
		if dirty := formatGitDirty(state.GitAdded, state.GitModified); dirty != "clean" {
			summary += " " + dirty
		}
		return summary
	}
	return ""
}

func tokenSummary(state REPLViewState) string {
	if state.PromptTokens <= 0 && state.CompletionTokens <= 0 {
		return "tokens -"
	}
	return fmt.Sprintf("tokens %s", formatTokenUsage(state.PromptTokens, state.CompletionTokens))
}

func normalizedExecutionState(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "idle", "ready":
		return "ready"
	case "streaming", "running", "thinking", "planning", "executing_tools", "processing_results", "delivering":
		return "running"
	case "waiting approval", "waiting_approval":
		return "waiting approval"
	case "paused":
		return "paused"
	case "cancelled", "canceled":
		return "cancelled"
	case "completed", "done":
		return "completed"
	case "failed", "error":
		return "error"
	default:
		return strings.ToLower(strings.TrimSpace(value))
	}
}

func formatClockDuration(value time.Duration) string {
	if value <= 0 {
		return "00:00"
	}
	totalSeconds := int(value.Round(time.Second).Seconds())
	minutes := totalSeconds / 60
	seconds := totalSeconds % 60
	if minutes >= 60 {
		hours := minutes / 60
		minutes = minutes % 60
		return fmt.Sprintf("%02d:%02d:%02d", hours, minutes, seconds)
	}
	return fmt.Sprintf("%02d:%02d", minutes, seconds)
}

// formatHumanDuration formats a duration in human-friendly form: "6s", "1m 23s", "1h 5m".
func formatHumanDuration(value time.Duration) string {
	if value <= 0 {
		return "0s"
	}
	totalSeconds := int(value.Round(time.Second).Seconds())
	if totalSeconds < 60 {
		return fmt.Sprintf("%ds", totalSeconds)
	}
	minutes := totalSeconds / 60
	seconds := totalSeconds % 60
	if minutes < 60 {
		if seconds == 0 {
			return fmt.Sprintf("%dm", minutes)
		}
		return fmt.Sprintf("%dm %ds", minutes, seconds)
	}
	hours := minutes / 60
	minutes = minutes % 60
	if minutes == 0 {
		return fmt.Sprintf("%dh", hours)
	}
	return fmt.Sprintf("%dh %dm", hours, minutes)
}

func formatTokenCount(value int) string {
	if value < 1000 {
		return fmt.Sprintf("%d", value)
	}
	return fmt.Sprintf("%.1fk", float64(value)/1000)
}

func requestIDSubtitle(input string) string {
	for _, part := range strings.Split(input, "|") {
		part = strings.TrimSpace(part)
		if strings.HasPrefix(part, "request_id=") {
			return strings.TrimPrefix(part, "request_id=")
		}
	}
	return ""
}

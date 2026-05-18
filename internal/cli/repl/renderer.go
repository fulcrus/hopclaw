package repl

import (
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/fulcrus/hopclaw/acp"
)

type Renderer struct {
	out              io.Writer
	assistantOut     io.Writer
	splitOutput      bool
	tty              bool
	mu               sync.Mutex
	lineOpen         bool
	assistantStarted bool // true after first visible assistant char in this run
	spinnerStop      chan struct{}
	spinnerDone      chan struct{}
	lastDock         string
	toolTracker      toolEventTracker
}

type toolEventTracker struct {
	name   string
	count  int
	latest string
}

// BannerInfo holds data for the startup banner.
type BannerInfo struct {
	Version       string
	Target        string
	TargetKind    string
	Model         string
	Session       string
	ContextWindow int
	UpdateAvail   string
}

func NewRenderer(out io.Writer, tty bool) *Renderer {
	if out == nil {
		out = io.Discard
	}
	return &Renderer{
		out:          out,
		assistantOut: out,
		tty:          tty,
	}
}

func NewSplitRenderer(statusOut, assistantOut io.Writer, tty bool) *Renderer {
	renderer := NewRenderer(statusOut, tty)
	if assistantOut == nil {
		assistantOut = renderer.out
	}
	renderer.assistantOut = assistantOut
	renderer.splitOutput = true
	return renderer
}

func (r *Renderer) statusWriter() io.Writer {
	if r == nil || r.out == nil {
		return io.Discard
	}
	return r.out
}

func (r *Renderer) assistantWriter() io.Writer {
	if r == nil {
		return io.Discard
	}
	if r.assistantOut != nil {
		return r.assistantOut
	}
	return r.statusWriter()
}

func (r *Renderer) Banner(info BannerInfo) {
	r.mu.Lock()
	r.flushToolEventTrackerLocked()
	r.mu.Unlock()
	model := info.Model
	if model == "" {
		model = "auto"
	}
	line1 := "HopClaw"
	if version := strings.TrimSpace(info.Version); version != "" {
		line1 += " " + version
	}
	if r.tty {
		fmt.Fprintln(r.statusWriter(), joinNonEmpty(" · ", line1, bannerTargetLabel(info.Target, info.TargetKind), model))
	} else {
		line2Parts := []string{
			targetConnectionLabel(info.Target, info.TargetKind),
			model,
			"conversation " + sessionDisplayKey(info.Session),
		}
		fmt.Fprintln(r.statusWriter(), line1)
		fmt.Fprintln(r.statusWriter(), joinNonEmpty(" · ", line2Parts...))
	}
	if info.UpdateAvail != "" {
		fmt.Fprintf(r.statusWriter(), "update available: %s → %s (run: hopclaw update)\n", info.Version, info.UpdateAvail)
	}
}

func bannerTargetLabel(target, kind string) string {
	label := strings.TrimSpace(targetConnectionLabel(target, kind))
	if label == "local" {
		return ""
	}
	return label
}

func (r *Renderer) SystemLine(text string) {
	r.RenderSystemEvent(text)
}

func (r *Renderer) RenderSystemEvent(text string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.stopSpinnerLocked()
	r.flushToolEventTrackerLocked()
	r.flushCurrentLineLocked(true)
	if strings.TrimSpace(text) == "" {
		fmt.Fprintln(r.statusWriter())
		return
	}
	if r.tty {
		text = compact(text, max(historyBodyWidth(9), 24))
		fmt.Fprintf(r.statusWriter(), "\033[90m[system]\033[0m %s\n", text)
		return
	}
	fmt.Fprintf(r.statusWriter(), "[system] %s\n", text)
}

func (r *Renderer) RenderPhase(phase Phase, toolName string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.stopSpinnerLocked()
	r.flushToolEventTrackerLocked()
	r.flushCurrentLineLocked(true)
	line := formatPhaseLine(phase, toolName)
	if r.tty {
		line = compact(line, max(historyBodyWidth(0), 24))
	}
	fmt.Fprintln(r.statusWriter(), line)
}

func (r *Renderer) RenderDock(state REPLViewState) {
	r.RenderWorkbench(state)
}

func (r *Renderer) RenderWorkbench(state REPLViewState) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.stopSpinnerLocked()
	r.flushCurrentLineLocked(true)

	mode := resolveDockLayout(state, r.tty)
	lines := workbenchLines(state, mode, r.tty)
	if len(lines) == 0 {
		return
	}
	dock := strings.Join(lines, "\n") + "\n"
	if dock == r.lastDock {
		return
	}
	r.lastDock = dock
	fmt.Fprint(r.statusWriter(), dock)
}

func (r *Renderer) ClearScreen() {
	if !r.tty {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.stopSpinnerLocked()
	r.flushToolEventTrackerLocked()
	r.flushCurrentLineLocked(false)
	r.lastDock = ""
	fmt.Fprint(r.statusWriter(), "\033[2J\033[H")
}

func (r *Renderer) StartSpinner(label string) {
	if !r.tty {
		return
	}
	r.mu.Lock()
	if r.spinnerStop != nil {
		r.mu.Unlock()
		return
	}
	stop := make(chan struct{})
	done := make(chan struct{})
	r.spinnerStop = stop
	r.spinnerDone = done
	r.mu.Unlock()

	go func() {
		defer close(done)
		frames := []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
		index := 0
		for {
			select {
			case <-stop:
				fmt.Fprint(r.statusWriter(), "\r\033[2K")
				return
			default:
				fmt.Fprintf(r.statusWriter(), "\r\033[2K%s %s", frames[index], label)
				index = (index + 1) % len(frames)
			}
			select {
			case <-stop:
				fmt.Fprint(r.statusWriter(), "\r\033[2K")
				return
			case <-timeAfter(spinnerInterval):
			}
		}
	}()
}

func (r *Renderer) StopSpinner() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.stopSpinnerLocked()
}

func (r *Renderer) WriteDelta(delta string) {
	r.RenderAssistantDelta(delta)
}

func (r *Renderer) RenderAssistantDelta(delta string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.stopSpinnerLocked()
	r.flushToolEventTrackerLocked()
	for _, ch := range delta {
		if ch == '\r' {
			continue
		}
		// Before the first visible character of a new run, skip all whitespace
		// (including newlines and spaces) to prevent the indentation artifact
		// from model output that starts with "\n\n    Hello!".
		if !r.assistantStarted {
			if ch == ' ' || ch == '\t' || ch == '\n' {
				continue
			}
			r.assistantStarted = true
		}
		fmt.Fprintf(r.assistantWriter(), "%c", ch)
		r.lineOpen = ch != '\n'
	}
}

// ResetAssistantState resets the assistant output tracking for a new run.
func (r *Renderer) ResetAssistantState() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.assistantStarted = false
}

func (r *Renderer) ToolStatus(name string, output string) {
	r.RenderToolEvent(name, output)
}

func (r *Renderer) RenderToolEvent(name string, output string) {
	if strings.TrimSpace(name) == "" {
		return
	}
	output = strings.TrimSpace(output)
	if output == "" {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.stopSpinnerLocked()
	r.flushCurrentLineLocked(true)
	if r.toolTracker.name != "" && r.toolTracker.name != name {
		r.flushToolEventTrackerLocked()
	}
	r.toolTracker.name = name
	r.toolTracker.count++
	r.toolTracker.latest = output
	if r.toolTracker.count <= 2 {
		r.printToolEventLineLocked(name, output)
	}
}

func (r *Renderer) RenderError(err error, hint string) {
	if err == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.stopSpinnerLocked()
	r.flushToolEventTrackerLocked()
	r.flushCurrentLineLocked(true)
	line := err.Error()
	if strings.TrimSpace(hint) != "" {
		line += " " + strings.TrimSpace(hint)
	}
	if r.tty {
		r.renderCardLocked(CardSpec{
			Title:   "Task Failed",
			Rows:    []CardRow{{Label: "Effect", Value: err.Error()}, {Label: "Next", Value: defaultString(strings.TrimSpace(hint), "/doctor · /last")}},
			Actions: "/doctor  /last  Esc back",
			Width:   72,
		})
		return
	}
	fmt.Fprintf(r.statusWriter(), "[error] %s\n", line)
}

func (r *Renderer) RenderApprovalCard(card ApprovalCard, expanded bool) {
	rows := []CardRow{
		{Label: "Tool", Value: defaultString(card.Action, "(unknown)")},
		{Label: "Reason", Value: defaultString(card.Reason, "(unspecified)")},
		{Label: "Scope", Value: defaultString(card.Scope, "once")},
		{Label: "Risk", Value: defaultString(card.Impact, "(unspecified)")},
		{Label: "Input", Value: defaultString(compact(card.Input, 72), "(none)")},
	}
	if expanded {
		rows = append(rows, CardRow{Label: "Details", Value: defaultString(card.Input, "(none)")})
	}
	actions := "[y] approve once  [n] deny  [v] details"
	if card.AllowSession {
		actions = "[y] approve once  [a] allow for conversation  [n] deny  [v] details"
	}
	r.RenderCard(CardSpec{
		Title:    "Approval Required",
		Subtitle: defaultString(strings.TrimSpace(card.RequestID), requestIDSubtitle(card.Input)),
		Rows:     rows,
		Actions:  actions,
		Width:    84,
	})
}

func (r *Renderer) RenderPanel(title string, rows []string, actions string, width int) {
	r.RenderPanelSpec(PanelSpec{
		Title:   title,
		Body:    rows,
		Actions: actions,
		Width:   width,
	})
}

func (r *Renderer) TimelineSummary(entries []ToolTimelineEntry) string {
	if len(entries) == 0 {
		return ""
	}
	parts := []string{fmt.Sprintf("timeline: %d", len(entries))}
	maxItems := min(len(entries), 2)
	for i := 0; i < maxItems; i++ {
		parts = append(parts, fmt.Sprintf("%s %s", entries[i].Name, defaultString(entries[i].Status, "ok")))
	}
	return strings.Join(parts, " · ")
}

func (r *Renderer) TimelineLines(entries []ToolTimelineEntry) []string {
	if len(entries) == 0 {
		return nil
	}
	lines := make([]string, 0, len(entries)*2)
	for i, entry := range entries {
		line := fmt.Sprintf("%d. %s · %s · %s", i+1, defaultString(entry.Name, "tool"), defaultString(entry.Status, "ok"), formatTimelineDuration(entry.Duration))
		lines = append(lines, line)
		if summary := strings.TrimSpace(entry.Summary); summary != "" {
			lines = append(lines, "   "+compact(summary, 72))
		}
	}
	return lines
}

func (r *Renderer) RenderPausedCard(runID, lastStep string) {
	r.RenderCard(CardSpec{
		Title:    "Task Paused",
		Subtitle: defaultString(runID, "(current conversation)"),
		Rows: []CardRow{
			{Label: "Reason", Value: "interrupted by user"},
			{Label: "Last step", Value: defaultString(lastStep, "(none)")},
		},
		Actions: "Enter continue  x discard  /retry",
		Width:   72,
	})
}

func (r *Renderer) RenderCancelledCard(runID string) {
	r.RenderCard(CardSpec{
		Title:    "Task Cancelled",
		Subtitle: defaultString(runID, "(current conversation)"),
		Rows: []CardRow{
			{Label: "Reason", Value: "cancelled by user"},
		},
		Actions: "/last  /runs recent",
		Width:   64,
	})
}

func (r *Renderer) RenderCompletionCard(duration time.Duration, usage *acp.UsageInfo) {
	rows := []CardRow{
		{Label: "Duration", Value: formatClockDuration(duration)},
	}
	if usage != nil {
		rows = append(rows, CardRow{Label: "Tokens", Value: fmt.Sprintf("%s in / %s out", formatTokenCount(usage.PromptTokens), formatTokenCount(usage.CompletionTokens))})
	}
	r.RenderCard(CardSpec{
		Title: "Task Completed",
		Rows:  rows,
		Width: 64,
	})
}

func (r *Renderer) Finish() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.stopSpinnerLocked()
	r.flushToolEventTrackerLocked()
	r.flushCurrentLineLocked(true)
}

func (r *Renderer) UsageLine(prompt, completion, total int) {
	if total <= 0 {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.flushToolEventTrackerLocked()
	if !r.tty {
		fmt.Fprintf(r.statusWriter(), "tokens: %d in · %d out · %d total\n", prompt, completion, total)
		return
	}
	fmt.Fprintf(r.statusWriter(), "\033[90mtokens: %d in · %d out · %d total\033[0m\n", prompt, completion, total)
}

func (r *Renderer) flushCurrentLineLocked(withNewline bool) {
	if !r.lineOpen {
		return
	}
	if withNewline {
		fmt.Fprint(r.assistantWriter(), "\n")
	}
	r.lineOpen = false
}

func (r *Renderer) stopSpinnerLocked() {
	if r.spinnerStop == nil {
		return
	}
	close(r.spinnerStop)
	stop := r.spinnerStop
	done := r.spinnerDone
	r.spinnerStop = nil
	r.spinnerDone = nil
	r.mu.Unlock()
	<-done
	r.mu.Lock()
	_ = stop
}

func (r *Renderer) flushToolEventTrackerLocked() {
	if r.toolTracker.count >= 3 && strings.TrimSpace(r.toolTracker.name) != "" {
		r.printToolEventLineLocked(r.toolTracker.name, fmt.Sprintf("%d calls, latest: %s", r.toolTracker.count, compact(r.toolTracker.latest, 84)))
	}
	r.toolTracker = toolEventTracker{}
}

func (r *Renderer) printToolEventLineLocked(name string, output string) {
	line := strings.TrimSpace(name) + " — " + strings.TrimSpace(output)
	if r.tty {
		line = compact(line, max(historyBodyWidth(7), 24))
	}
	if r.tty {
		fmt.Fprintf(r.statusWriter(), "\033[90m[tool]\033[0m %s\n", line)
		return
	}
	fmt.Fprintf(r.statusWriter(), "[tool] %s\n", line)
}

func compact(text string, limit int) string {
	return compactDisplay(text, limit)
}

func historyBodyWidth(prefixWidth int) int {
	return max(terminalWidthFallback()-max(prefixWidth, 0), 0)
}

func formatTimelineDuration(value time.Duration) string {
	if value <= 0 {
		return "0s"
	}
	if value < time.Second {
		return value.Round(10 * time.Millisecond).String()
	}
	return value.Round(100 * time.Millisecond).String()
}

func resolveDockLayout(state REPLViewState, tty bool) LayoutMode {
	switch state.LayoutMode {
	case LayoutFull, LayoutCompact, LayoutPlain, LayoutMinimal:
		return state.LayoutMode
	}
	if !tty {
		return LayoutPlain
	}
	width := state.TerminalWidth
	if width <= 0 {
		width = terminalWidthFallback()
	}
	switch {
	case width >= 120:
		return LayoutFull
	case width >= 80:
		return LayoutCompact
	default:
		return LayoutPlain
	}
}

func dockLines(state REPLViewState, mode LayoutMode, tty bool) []string {
	target := renderBadge(tty && mode != LayoutPlain, badgeText(targetChipLabel(state.Target, state.TargetKind)), "34")
	model := renderBadge(tty && mode != LayoutPlain, badgeText("MODEL "+defaultString(state.Model, "(default)")), "34")
	execState := renderBadge(tty && mode != LayoutPlain, badgeText(strings.ToUpper(defaultString(state.ExecutionState, "ready"))), executionColor(state.ExecutionState))
	health := renderBadge(tty && mode != LayoutPlain, badgeText(strings.ToUpper(defaultString(state.Health, "ok"))), healthColor(state.Health))
	approval := ""
	if strings.EqualFold(strings.TrimSpace(state.ExecutionState), "waiting approval") || strings.EqualFold(strings.TrimSpace(state.Phase), string(PhaseWaitingApproval)) {
		approval = renderBadge(tty && mode != LayoutPlain, badgeText("WAITING APPROVAL"), "33")
	}
	delivery := ""
	if strings.EqualFold(strings.TrimSpace(state.DeliveryState), "retrying") {
		delivery = renderBadge(tty && mode != LayoutPlain, badgeText("DELIVERY RETRY"), "33")
	}

	switch mode {
	case LayoutFull:
		line1 := joinNonEmpty(" ",
			target,
			model,
			execState,
			approval,
			delivery,
			healthIfNotOK(health, state.Health),
		)
		gitPart := ""
		if branch := strings.TrimSpace(state.GitBranch); branch != "" {
			gitPart = "git:" + branch
			if state.GitAdded > 0 {
				gitPart += fmt.Sprintf(" +%d", state.GitAdded)
			}
			if state.GitModified > 0 {
				gitPart += fmt.Sprintf(" ~%d", state.GitModified)
			}
		}
		line2 := "| " + joinNonEmpty("  ",
			abbreviatePath(state.CWD, 48),
			gitPart,
			optionalField("conv", state.SessionKey),
		)
		line3 := "| " + joinNonEmpty("  ",
			optionalField("tool", state.LastTool),
			optionalField("ctx", percentString(state.ContextPercent)),
			supervisorDockSummary(state),
		)
		lines := []string{"| " + line1, line2, line3}
		if hint := dockHintLine(state); hint != "" {
			lines = append(lines, "| "+hint)
		}
		return lines
	case LayoutCompact:
		line1 := "| " + joinNonEmpty(" ",
			target,
			model,
			execState,
			renderBadge(tty, badgeText("CTX "+percentString(state.ContextPercent)), "90"),
			approval,
			delivery,
			healthIfNotOK(health, state.Health),
		)
		line2 := "| " + joinNonEmpty("  ",
			abbreviatePath(state.CWD, 28),
			defaultString(state.GitBranch, ""),
			optionalField("conv", state.SessionKey),
			optionalField("tool", state.LastTool),
		)
		lines := []string{line1, line2}
		if hint := dockHintLine(state); hint != "" {
			lines = append(lines, "| "+hint)
		}
		return lines
	default:
		return []string{
			"HopClaw " + targetConnectionLabel(state.Target, state.TargetKind) + " " + defaultString(state.Model, "(default)") + " conversation " + defaultString(state.SessionKey, "default") + " " + defaultString(state.ExecutionState, "ready"),
		}
	}
}

func healthIfNotOK(rendered, health string) string {
	switch strings.ToLower(strings.TrimSpace(health)) {
	case "", "ok", "ready":
		return ""
	default:
		return rendered
	}
}

func supervisorDockSummary(state REPLViewState) string {
	parts := make([]string, 0, 4)
	if state.ForegroundRunCount > 1 {
		parts = append(parts, fmt.Sprintf("fg:%d", state.ForegroundRunCount))
	}
	if state.BackgroundRunCount > 0 {
		parts = append(parts, fmt.Sprintf("bg:%d", state.BackgroundRunCount))
	}
	if state.PausedRunCount > 0 {
		parts = append(parts, fmt.Sprintf("paused:%d", state.PausedRunCount))
	}
	if state.AttentionCount > 0 {
		parts = append(parts, fmt.Sprintf("attn:%d", state.AttentionCount))
	}
	return strings.Join(parts, " ")
}

func dockProfileLine4(state REPLViewState, profile string) string {
	switch normalizeProfile(profile) {
	case ProfileOps:
		return joinNonEmpty("  ",
			optionalField("phase", defaultString(state.Phase, "idle")),
			optionalField("remote health", defaultString(state.Health, "ok")),
			optionalField("queue", fmt.Sprintf("%d", state.QueueDepth)),
			optionalField("sandbox", defaultString(state.Sandbox, "local")),
			optionalField("quality", defaultString(state.Quality, "ok")),
			optionalField("last failure", defaultString(state.LastFailure, "-")),
		)
	case ProfileChannel:
		return joinNonEmpty("  ",
			optionalField("phase", defaultString(state.Phase, "idle")),
			optionalField("channel", defaultString(state.Channel, "cli")),
			optionalField("delivery", dockActivity(state.LastTool, "deliver", "delivery")),
			optionalField("webhook", dockActivity(state.LastTool, "webhook", "hook")),
			optionalField("automation", dockActivity(state.LastTool, "automation", "cron", "watch", "wakeup")),
			optionalField("retry queue", fmt.Sprintf("%d", state.QueueDepth)),
		)
	case ProfileAutomation:
		return joinNonEmpty("  ",
			optionalField("phase", defaultString(state.Phase, "idle")),
			optionalField("cron", dockActivity(state.LastTool, "cron")),
			optionalField("active runs", fmt.Sprintf("%d", activeRunCount(state))),
			optionalField("queued runs", fmt.Sprintf("%d", state.QueueDepth)),
			optionalField("next trigger", "-"),
			optionalField("dead letters", deadLetterSummary(state)),
		)
	default:
		return joinNonEmpty("  ",
			optionalField("phase", defaultString(state.Phase, "idle")),
			optionalField("tool", defaultString(state.LastTool, "-")),
			optionalField("ctx", percentString(state.ContextPercent)),
			optionalField("tokens", formatTokenUsage(state.PromptTokens, state.CompletionTokens)),
			optionalField("tests", testsStatus(state.Quality)),
			optionalField("git dirty", formatGitDirty(state.GitAdded, state.GitModified)),
		)
	}
}

func formatTokenUsage(promptTokens, completionTokens int) string {
	return fmt.Sprintf("%d / %d", promptTokens, completionTokens)
}

func testsStatus(quality string) string {
	switch strings.ToLower(strings.TrimSpace(quality)) {
	case "degraded", "error":
		return "failing"
	case "warn":
		return "attention"
	case "":
		return "pending"
	default:
		return "passing"
	}
}

func formatGitDirty(added, modified int) string {
	parts := make([]string, 0, 2)
	if added > 0 {
		parts = append(parts, fmt.Sprintf("+%d", added))
	}
	if modified > 0 {
		parts = append(parts, fmt.Sprintf("~%d", modified))
	}
	if len(parts) == 0 {
		return "clean"
	}
	return strings.Join(parts, " ")
}

func dockActivity(lastTool string, keywords ...string) string {
	lastTool = strings.TrimSpace(lastTool)
	if lastTool == "" {
		return "-"
	}
	lowerTool := strings.ToLower(lastTool)
	for _, keyword := range keywords {
		if strings.Contains(lowerTool, strings.ToLower(strings.TrimSpace(keyword))) {
			return lastTool
		}
	}
	return "-"
}

func activeRunCount(state REPLViewState) int {
	switch strings.ToLower(strings.TrimSpace(state.ExecutionState)) {
	case "streaming", "waiting approval":
		return 1
	default:
		return 0
	}
}

func deadLetterSummary(state REPLViewState) string {
	if strings.Contains(strings.ToLower(strings.TrimSpace(state.LastFailure)), "dead") {
		return state.LastFailure
	}
	return "-"
}

func dockHintLine(state REPLViewState) string {
	switch dockInteractionState(state) {
	case "approval":
		return "hint: y approve once · a always · n deny · v details"
	case "paused":
		return "hint: Enter continue · x discard · Ctrl+C quit terminal"
	case "cancelled":
		return "hint: /last inspect result · /runs recent"
	case "running":
		return "hint: Esc pause task · Ctrl+C quit terminal"
	default:
		return ""
	}
}

func dockInteractionState(state REPLViewState) string {
	execution := strings.ToLower(strings.TrimSpace(state.ExecutionState))
	phase := strings.ToLower(strings.TrimSpace(state.Phase))
	switch {
	case execution == "waiting approval", phase == string(PhaseWaitingApproval):
		return "approval"
	case execution == "paused", phase == string(PhasePaused):
		return "paused"
	case execution == "cancelled", phase == string(PhaseCancelled):
		return "cancelled"
	case execution == "streaming":
		return "running"
	case phase != "" && phase != string(PhaseIdle) && phase != string(PhaseCompleted) && phase != string(PhaseCancelled) && phase != string(PhaseError):
		return "running"
	default:
		return ""
	}
}

func renderBadge(enabled bool, text, color string) string {
	if text == "" {
		return ""
	}
	if !enabled || color == "" {
		return text
	}
	return "\033[" + color + "m" + text + "\033[0m"
}

func badgeText(text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	return "[" + text + "]"
}

func optionalBadge(tty bool, text, color string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	return renderBadge(tty, badgeText(text), color)
}

func defaultString(value, fallback string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback
	}
	return value
}

func joinNonEmpty(sep string, parts ...string) string {
	filtered := parts[:0]
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		filtered = append(filtered, part)
	}
	return strings.Join(filtered, sep)
}

func optionalField(label, value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	return label + ": " + value
}

func executionColor(state string) string {
	switch strings.ToLower(strings.TrimSpace(state)) {
	case "error":
		return "31"
	case "waiting approval", "paused":
		return "33"
	case "done":
		return "32"
	default:
		return "32"
	}
}

func healthColor(state string) string {
	switch strings.ToLower(strings.TrimSpace(state)) {
	case "warn":
		return "33"
	case "degraded", "error":
		return "31"
	default:
		return "32"
	}
}

func percentString(value int) string {
	if value <= 0 {
		return "0%"
	}
	return fmt.Sprintf("%d%%", value)
}

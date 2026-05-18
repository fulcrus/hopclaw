package repl

import (
	"fmt"
	"strings"
	"time"
)

type TaskSnapshotCard struct {
	Kind    string
	RunID   string
	Title   string
	Rows    []SnapshotRow
	Context string
	Actions string
	Width   int
}

type SnapshotRow struct {
	Label   string
	Content string
	Status  string
}

type snapshotTracker struct {
	lastByKey        map[string]time.Time
	planShown        bool
	lastProgressTool string
}

func (t *snapshotTracker) reset() {
	if t == nil {
		return
	}
	t.lastByKey = make(map[string]time.Time)
	t.planShown = false
	t.lastProgressTool = ""
}

func (t *snapshotTracker) allow(runID, kind string, now time.Time) bool {
	if t == nil {
		return false
	}
	if t.lastByKey == nil {
		t.lastByKey = make(map[string]time.Time)
	}
	key := strings.TrimSpace(runID) + ":" + strings.TrimSpace(kind)
	if last, ok := t.lastByKey[key]; ok && now.Sub(last) < 10*time.Second {
		return false
	}
	t.lastByKey[key] = now
	return true
}

func (r *Renderer) RenderTaskSnapshot(spec TaskSnapshotCard) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.stopSpinnerLocked()
	r.flushToolEventTrackerLocked()
	r.flushCurrentLineLocked(true)
	r.renderTaskSnapshotLocked(spec)
}

func (r *Renderer) renderTaskSnapshotLocked(spec TaskSnapshotCard) {
	if !r.tty {
		header := "[task] " + defaultString(spec.Title, "Task Snapshot")
		if strings.TrimSpace(spec.RunID) != "" {
			header += " · " + strings.TrimSpace(spec.RunID)
		}
		fmt.Fprintln(r.statusWriter(), header)
		for _, row := range spec.Rows {
			fmt.Fprintf(r.statusWriter(), "[task] label=%s content=%s status=%s\n",
				defaultString(strings.TrimSpace(row.Label), "item"),
				defaultString(strings.TrimSpace(row.Content), "-"),
				defaultString(snapshotStatusText(strings.TrimSpace(row.Status)), "attention needed"),
			)
		}
		if strings.TrimSpace(spec.Context) != "" {
			fmt.Fprintln(r.statusWriter(), spec.Context)
		}
		if strings.TrimSpace(spec.Actions) != "" {
			fmt.Fprintln(r.statusWriter(), "Actions:", spec.Actions)
		}
		return
	}
	width := spec.Width
	if width <= 0 {
		width = 80
	}
	// Only use 3-column table if terminal is truly wide enough (>=110 after padding)
	if width >= 110 {
		r.renderWideTaskSnapshotLocked(spec, width)
		return
	}
	rows := make([]CardRow, 0, len(spec.Rows))
	for _, row := range spec.Rows {
		value := strings.TrimSpace(row.Content)
		if status := strings.TrimSpace(row.Status); status != "" {
			value = joinNonEmpty(" · ", value, snapshotStatusText(status))
		}
		rows = append(rows, CardRow{
			Label: row.Label,
			Value: value,
		})
	}
	r.renderCardLocked(CardSpec{
		Title:    defaultString(spec.Title, "Task Snapshot"),
		Subtitle: strings.TrimSpace(spec.RunID),
		Rows:     rows,
		Actions:  strings.TrimSpace(spec.Actions),
		Footer:   strings.TrimSpace(spec.Context),
		Width:    min(max(width, 72), 96),
	})
}

func (r *Renderer) renderWideTaskSnapshotLocked(spec TaskSnapshotCard, width int) {
	title := defaultString(spec.Title, "Task Snapshot")
	if strings.TrimSpace(spec.RunID) != "" {
		title += " · " + strings.TrimSpace(spec.RunID)
	}
	labelWidth := 8
	for _, row := range spec.Rows {
		labelWidth = min(max(labelWidth, len(strings.TrimSpace(row.Label))), 14)
	}
	maxContent := max(width-labelWidth-20, 30)

	// Plain text snapshot — no box-drawing.
	fmt.Fprintf(r.statusWriter(), "\033[1m%s\033[0m\n", compact(title, width))
	for _, row := range spec.Rows {
		label := compact(defaultString(strings.TrimSpace(row.Label), "-"), labelWidth)
		content := compact(defaultString(strings.TrimSpace(row.Content), "-"), maxContent)
		status := snapshotStatusText(strings.TrimSpace(row.Status))
		if status != "" {
			fmt.Fprintf(r.statusWriter(), "  %-*s  %s \033[90m· %s\033[0m\n", labelWidth, label, content, status)
		} else {
			fmt.Fprintf(r.statusWriter(), "  %-*s  %s\n", labelWidth, label, content)
		}
	}
	if footer := strings.TrimSpace(spec.Context); footer != "" {
		fmt.Fprintf(r.statusWriter(), "\033[90m  %s\033[0m\n", compact(footer, max(width-2, 30)))
	}
	if actions := strings.TrimSpace(spec.Actions); actions != "" {
		fmt.Fprintf(r.statusWriter(), "\033[90m  Actions: %s\033[0m\n", compact(actions, max(width-11, 24)))
	}
}

func snapshotStatusText(status string) string {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "done":
		return "completed"
	case "running":
		return "in progress"
	case "input":
		return "waiting for input"
	case "action":
		return "action needed"
	case "waiting":
		return "queued"
	case "blocked":
		return "blocked"
	case "attention":
		return "attention needed"
	case "info":
		return "confirmed"
	default:
		return ""
	}
}

func (r *REPL) snapshotSubjectID() string {
	if r == nil {
		return ""
	}
	return defaultString(
		strings.TrimSpace(r.currentRunID),
		defaultString(strings.TrimSpace(r.lastRunID), defaultString(strings.TrimSpace(r.sessionKey), strings.TrimSpace(r.sessionID))),
	)
}

func (r *REPL) maybeRenderPlanSnapshot(toolName string) {
	if r == nil {
		return
	}
	r.ensureCurrentRunID()
	if r.snapshotTracker.planShown {
		return
	}
	now := time.Now()
	runID := r.snapshotSubjectID()
	if !r.snapshotTracker.allow(runID, "plan", now) {
		return
	}
	r.snapshotTracker.planShown = true
	r.snapshotTracker.lastProgressTool = strings.TrimSpace(toolName)
	r.renderSnapshot(TaskSnapshotCard{
		Kind:  "plan",
		RunID: runID,
		Title: "Current Task",
		Rows: []SnapshotRow{
			{Label: "Plan", Content: "Confirm the execution path and change boundary", Status: "done"},
			{Label: "Exec", Content: "Working through " + defaultString(toolName, "task planning"), Status: "running"},
			{Label: "Risk", Content: defaultString(r.targetName, "local"), Status: "info"},
			{Label: "QA", Content: "build affected packages + tests", Status: "waiting"},
		},
		Context: r.snapshotContext(""),
		Actions: "Esc pause  /runs manage",
	})
}

func (r *REPL) maybeRenderProgressSnapshot(toolName string) {
	if r == nil {
		return
	}
	toolName = strings.TrimSpace(toolName)
	if toolName == "" || toolName == r.snapshotTracker.lastProgressTool {
		return
	}
	if r.runStartedAt.IsZero() || time.Since(r.runStartedAt) < 15*time.Second {
		r.snapshotTracker.lastProgressTool = toolName
		return
	}
	r.ensureCurrentRunID()
	now := time.Now()
	runID := r.snapshotSubjectID()
	if !r.snapshotTracker.allow(runID, "progress", now) {
		r.snapshotTracker.lastProgressTool = toolName
		return
	}
	r.snapshotTracker.lastProgressTool = toolName
	r.renderSnapshot(TaskSnapshotCard{
		Kind:  "progress",
		RunID: runID,
		Title: "Current Task",
		Rows: []SnapshotRow{
			{Label: "Exec", Content: toolName, Status: "running"},
			{Label: "Risk", Content: defaultString(r.approvalState.Scope, "Execution can continue"), Status: "info"},
			{Label: "Next", Content: "Keep the run in foreground", Status: "waiting"},
		},
		Context: r.snapshotContext(""),
		Actions: "Esc pause  /runs manage",
	})
}

func (r *REPL) renderApprovalSnapshot() {
	if r == nil {
		return
	}
	r.ensureCurrentRunID()
	runID := r.snapshotSubjectID()
	if !r.snapshotTracker.allow(runID, "approval_wait", time.Now()) {
		return
	}
	r.renderSnapshot(TaskSnapshotCard{
		Kind:  "approval_wait",
		RunID: runID,
		Title: "Approval Pending",
		Rows: []SnapshotRow{
			{Label: "Exec", Content: defaultString(r.approvalState.Tool, "approval"), Status: "blocked"},
			{Label: "Risk", Content: defaultString(r.approvalState.Scope, "scope review"), Status: "info"},
			{Label: "Next", Content: "y approve once · a allow for conversation · n deny", Status: "input"},
		},
		Context: r.snapshotContext(r.approvalState.Scope),
		Actions: "y approve  n deny  v details",
	})
}

func (r *REPL) renderPausedSnapshot(lastStep string) {
	if r == nil {
		return
	}
	r.ensureCurrentRunID()
	runID := r.snapshotSubjectID()
	if !r.snapshotTracker.allow(runID, "paused", time.Now()) {
		return
	}
	r.renderSnapshot(TaskSnapshotCard{
		Kind:  "paused",
		RunID: runID,
		Title: "Task Paused",
		Rows: []SnapshotRow{
			{Label: "Recovery", Content: "User interrupted the current run", Status: "info"},
			{Label: "Exec", Content: fmt.Sprintf("%s · last step %s", defaultString(runID, "(current conversation)"), defaultString(lastStep, "(none)")), Status: "blocked"},
			{Label: "Next", Content: "Enter continue · x discard · /retry rerun", Status: "input"},
		},
		Context: r.snapshotContext(""),
		Actions: "Enter continue  x discard  /retry",
	})
}

func (r *REPL) renderCancelledSnapshot() {
	if r == nil {
		return
	}
	r.ensureCurrentRunID()
	runID := r.snapshotSubjectID()
	if !r.snapshotTracker.allow(runID, "cancelled", time.Now()) {
		return
	}
	r.renderSnapshot(TaskSnapshotCard{
		Kind:  "cancelled",
		RunID: runID,
		Title: "Task Cancelled",
		Rows: []SnapshotRow{
			{Label: "Exec", Content: defaultString(runID, "(current conversation)"), Status: "blocked"},
			{Label: "Recovery", Content: "User cancelled the current run", Status: "info"},
			{Label: "Next", Content: "/last inspect result · /runs recent inspect history", Status: "waiting"},
		},
		Context: r.snapshotContext(""),
		Actions: "/last  /runs recent",
	})
}

func (r *REPL) renderCompletedSnapshot() {
	if r == nil {
		return
	}
	r.ensureCurrentRunID()
	runID := r.snapshotSubjectID()
	if !r.snapshotTracker.allow(runID, "completed", time.Now()) {
		return
	}
	rows := []SnapshotRow{
		{Label: "Exec", Content: "Primary result is ready", Status: "done"},
		{Label: "QA", Content: "duration " + defaultString(formatClockDuration(r.lastRunDuration), "00:00"), Status: "done"},
	}
	if r.lastUsage != nil {
		rows = append(rows, SnapshotRow{Label: "QA", Content: fmt.Sprintf("tokens %s in / %s out", formatTokenCount(r.lastUsage.PromptTokens), formatTokenCount(r.lastUsage.CompletionTokens)), Status: "info"})
	}
	r.renderSnapshot(TaskSnapshotCard{
		Kind:    "completed",
		RunID:   runID,
		Title:   "Task Completed",
		Rows:    rows,
		Context: r.snapshotContext(""),
		Actions: "/last  /runs recent",
	})
}

func (r *REPL) renderFailedSnapshot(errText string) {
	if r == nil {
		return
	}
	r.ensureCurrentRunID()
	runID := r.snapshotSubjectID()
	if !r.snapshotTracker.allow(runID, "failed", time.Now()) {
		return
	}
	r.renderSnapshot(TaskSnapshotCard{
		Kind:  "failed",
		RunID: runID,
		Title: "Task Failed",
		Rows: []SnapshotRow{
			{Label: "Recovery", Content: defaultString(errText, "run failed"), Status: "blocked"},
			{Label: "Next", Content: defaultString(currentToolName(r.lastToolStatus), "(none)"), Status: "waiting"},
		},
		Context: r.snapshotContext(""),
		Actions: "/last  /doctor  /runs recent",
	})
}

func (r *REPL) renderSnapshot(spec TaskSnapshotCard) {
	if r == nil || r.renderer == nil {
		return
	}
	if r.oneShot {
		return
	}
	if r.suppressPromptWorkbenchRuntimeNoise() && !promptWorkbenchSnapshotAllowed(spec.Kind) {
		return
	}
	if spec.Width <= 0 {
		spec.Width = r.terminalWidth()
	}
	r.renderer.RenderTaskSnapshot(spec)
}

func promptWorkbenchSnapshotAllowed(kind string) bool {
	switch strings.TrimSpace(kind) {
	case "completed", "delivery_attention":
		return true
	default:
		return false
	}
}

func (r *REPL) snapshotContext(scope string) string {
	parts := []string{
		defaultString(r.targetName, "local"),
		"conversation " + defaultString(r.sessionKey, "default"),
	}
	if strings.TrimSpace(scope) != "" {
		parts = append(parts, strings.TrimSpace(scope))
	}
	if deliveryState := strings.TrimSpace(r.deliveryState.State); deliveryState != "" {
		parts = append(parts, "delivery "+deliveryState)
	}
	if strip := strings.TrimSpace(r.viewState.MemoryStrip); strip != "" {
		parts = append(parts, strip)
	}
	if !r.runStartedAt.IsZero() {
		parts = append(parts, "elapsed "+formatClockDuration(time.Since(r.runStartedAt)))
	}
	return strings.Join(parts, " · ")
}

func (r *REPL) renderBackgroundedSnapshot() {
	if r == nil {
		return
	}
	r.ensureCurrentRunID()
	runID := r.snapshotSubjectID()
	if !r.snapshotTracker.allow(runID, "backgrounded", time.Now()) {
		return
	}
	r.renderSnapshot(TaskSnapshotCard{
		Kind:  "backgrounded",
		RunID: runID,
		Title: "Background Run",
		Rows: []SnapshotRow{
			{Label: "Exec", Content: defaultString(currentToolName(r.lastToolStatus), "run continues in background"), Status: "running"},
			{Label: "Risk", Content: "Foreground focus stays on the current conversation", Status: "info"},
			{Label: "Next", Content: "Use /runs or /last to inspect details", Status: "action"},
		},
		Context: r.snapshotContext(""),
		Actions: "/runs  /last  /fg <run_id>",
	})
}

func (r *REPL) renderRecoverySnapshot(errText string) {
	if r == nil {
		return
	}
	r.ensureCurrentRunID()
	runID := r.snapshotSubjectID()
	if !r.snapshotTracker.allow(runID, "recovery", time.Now()) {
		return
	}
	safeFallback, nextActions := recoveryHint(defaultString(errText, r.lastFailure), r.targetName, r.targetKind)
	r.renderSnapshot(TaskSnapshotCard{
		Kind:  "recovery",
		RunID: runID,
		Title: "Task Failed",
		Rows: []SnapshotRow{
			{Label: "Recovery", Content: safeFallback, Status: "info"},
			{Label: "Risk", Content: defaultString(errText, "run failed"), Status: "blocked"},
			{Label: "Next", Content: nextActions, Status: "action"},
		},
		Context: r.snapshotContext(""),
		Actions: "/doctor  /remote  /last",
	})
}

func (r *REPL) renderDeliveryAttentionSnapshot() {
	if r == nil {
		return
	}
	r.ensureCurrentRunID()
	runID := r.snapshotSubjectID()
	if !r.snapshotTracker.allow(runID, "delivery_attention", time.Now()) {
		return
	}
	rows := []SnapshotRow{
		{Label: "Exec", Content: "Primary result is ready", Status: "done"},
		{Label: "Delivery", Content: defaultString(r.deliveryState.Summary, "delivery retrying"), Status: "attention"},
	}
	if next := strings.TrimSpace(r.deliveryState.Next); next != "" {
		rows = append(rows, SnapshotRow{Label: "Next", Content: "retry at " + next, Status: "waiting"})
	} else {
		rows = append(rows, SnapshotRow{Label: "Next", Content: "open /last for receipts", Status: "waiting"})
	}
	r.renderSnapshot(TaskSnapshotCard{
		Kind:    "delivery_attention",
		RunID:   runID,
		Title:   "Delivery Still Running",
		Rows:    rows,
		Context: r.snapshotContext(""),
		Actions: "/last  /runs recent",
	})
}

func (r *REPL) renderAutomationPromotionSnapshot(sourceRunID, prompt, delivery string) {
	if r == nil {
		return
	}
	runID := defaultString(strings.TrimSpace(sourceRunID), r.snapshotSubjectID())
	if !r.snapshotTracker.allow(runID, "automation_promotion", time.Now()) {
		return
	}
	r.renderSnapshot(TaskSnapshotCard{
		Kind:  "automation_promotion",
		RunID: runID,
		Title: "Suggested Automation",
		Rows: []SnapshotRow{
			{Label: "Exec", Content: defaultString(runID, "-"), Status: "done"},
			{Label: "Recovery", Content: defaultString(strings.TrimSpace(prompt), "ready to promote"), Status: "info"},
			{Label: "Delivery", Content: defaultString(strings.TrimSpace(delivery), "reuse recent delivery"), Status: "attention"},
		},
		Context: r.snapshotContext(""),
		Actions: "/promote",
	})
}

func snapshotRows(items ...string) []SnapshotRow {
	rows := make([]SnapshotRow, 0, len(items)/2)
	for i := 0; i+1 < len(items); i += 2 {
		rows = append(rows, SnapshotRow{Label: items[i], Content: items[i+1]})
	}
	return rows
}

func recoveryHint(errText, target, kind string) (string, string) {
	lower := strings.ToLower(strings.TrimSpace(errText))
	targetKind := normalizeTargetKind(kind, target)
	switch {
	case strings.Contains(lower, "remote"),
		strings.Contains(lower, "gateway"),
		strings.Contains(lower, "unreachable"),
		strings.Contains(lower, "timeout"),
		strings.Contains(lower, "degraded"),
		strings.Contains(lower, "readiness"),
		strings.Contains(lower, "delivery"),
		strings.Contains(lower, "webhook"),
		targetKind == "remote":
		if targetKind == "remote" {
			return "switch remote or keep working local", "/remote list · /doctor · /last"
		}
		return "inspect remote health before retrying", "/doctor · /last"
	default:
		return "", ""
	}
}

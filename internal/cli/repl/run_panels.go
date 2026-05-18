package repl

import (
	"context"
	"fmt"
	"slices"
	"strings"
)

func (r *REPL) renderHistory(ctx context.Context) error {
	if r.sessionID == "" && r.sessionKey == "" {
		return fmt.Errorf("no active conversation")
	}
	detail, _, err := r.currentServiceSession(ctx)
	if err != nil {
		return err
	}
	if detail == nil || len(detail.Messages) == 0 {
		r.renderer.SystemLine("No history yet.")
		return nil
	}
	start := max(0, len(detail.Messages)-12)
	lines := make([]string, 0, len(detail.Messages)-start)
	for _, message := range detail.Messages[start:] {
		lines = append(lines, fmt.Sprintf("%-10s %s", "["+message.Role+"]", compact(message.Content, 88)))
	}
	r.openInfoPanel("Recent History", lines, "/history  Esc back")
	return nil
}

func (r *REPL) renderRuns(ctx context.Context, sessionID string, limit int) error {
	r.refreshSupervisorProjection(ctx, true)
	var items []RunSummary
	snapshot := r.supervisorSnapshot
	if snapshot != nil && len(snapshot.Items) > 0 {
		items = append(items, snapshot.Items...)
	} else {
		loaded, err := r.service.ListRuns(ctx, sessionID, limit)
		if err != nil {
			return err
		}
		items = loaded
	}
	if len(items) == 0 {
		r.renderer.SystemLine("No runs found.")
		return nil
	}
	if sessionID != "" {
		filtered := make([]RunSummary, 0, len(items))
		for _, item := range items {
			if item.SessionID == sessionID {
				filtered = append(filtered, item)
			}
		}
		if len(filtered) > 0 {
			items = filtered
		}
	}
	currentSessionID, _ := r.currentServiceSessionID(ctx)
	panelItems := make([]panelItem, 0, len(items))
	for _, item := range items {
		panelItems = append(panelItems, panelItem{
			ID:         item.ID,
			Text:       runPanelRow(item, snapshot, currentSessionID, r.sessionKey, r.targetName, r.targetKind),
			SearchText: strings.Join([]string{item.ID, item.SessionID, item.SessionKey, item.Status, item.Phase, item.ToolName, item.ScopeSummary}, " "),
		})
	}
	panel := newSelectionPanel(r, "Runs", "", "Enter inspect run  f foreground  b background  c continue run  p pause current  Esc back", panelItems)
	panel.onConfirm = func(item panelItem) (string, error) {
		return "/last " + internalInspectArg + " " + item.ID, nil
	}
	panel.hotkeys = map[rune]func(*selectionPanel, panelItem) (string, error){
		'f': func(panel *selectionPanel, item panelItem) (string, error) {
			if err := r.foregroundRun(ctx, item.ID); err != nil {
				return "", err
			}
			panel.status = "Foreground focus set to " + item.ID + "."
			return "", nil
		},
		'b': func(panel *selectionPanel, item panelItem) (string, error) {
			if err := r.backgroundRun(ctx, item.ID); err != nil {
				return "", err
			}
			panel.status = "Backgrounded " + item.ID + "."
			return "", nil
		},
		'c': func(_ *selectionPanel, item panelItem) (string, error) {
			return "/continue " + item.ID, nil
		},
		'p': func(panel *selectionPanel, item panelItem) (string, error) {
			if strings.TrimSpace(item.ID) != strings.TrimSpace(r.currentRunID) || !r.running {
				panel.status = "Pause is available only for the current foreground run."
				return "", nil
			}
			return "/pause", nil
		},
	}
	r.openPromptPanel(panel)
	return nil
}

func (r *REPL) backgroundRun(ctx context.Context, runID string) error {
	runID = strings.TrimSpace(firstNonEmpty(runID, r.currentRunID, r.foregroundRunID))
	if runID == "" {
		return fmt.Errorf("no active run to background")
	}
	if r.isBackgroundRun(runID) {
		return nil
	}
	sessionID, _ := r.currentServiceSessionID(ctx)
	if detail, err := r.service.GetRunDetail(ctx, runID); err == nil && detail != nil && strings.TrimSpace(detail.Run.SessionID) != "" {
		sessionID = strings.TrimSpace(detail.Run.SessionID)
	}
	r.rememberBackgroundRun(runID, sessionID)
	if runID == strings.TrimSpace(r.currentRunID) {
		r.currentRunID = ""
	}
	if runID == strings.TrimSpace(r.foregroundRunID) {
		r.foregroundRunID = ""
	}
	r.lastRunID = runID
	r.refreshSupervisorProjection(ctx, true)
	r.refreshViewState()
	r.renderDock()
	return nil
}

func (r *REPL) foregroundRun(ctx context.Context, runID string) error {
	runID = strings.TrimSpace(runID)
	if runID == "" {
		return fmt.Errorf("run id is required")
	}
	currentSessionID, _ := r.currentServiceSessionID(ctx)
	if sessionID := strings.TrimSpace(r.backgroundRunSessions[runID]); sessionID != "" && sessionID != strings.TrimSpace(currentSessionID) {
		if session, err := r.service.GetSession(ctx, sessionID); err == nil && session != nil && strings.TrimSpace(session.Summary.Key) != "" {
			if err := r.switchSession(ctx, session.Summary.Key, false); err != nil {
				return err
			}
		}
	}
	r.removeBackgroundRun(runID)
	r.foregroundRunID = runID
	r.currentRunID = runID
	r.lastRunID = runID
	r.refreshSupervisorProjection(ctx, true)
	r.refreshViewState()
	r.renderDock()
	return nil
}

func (r *REPL) rememberBackgroundRun(runID, sessionID string) {
	runID = strings.TrimSpace(runID)
	if runID == "" || r == nil {
		return
	}
	if !r.isBackgroundRun(runID) {
		r.backgroundRuns = append(r.backgroundRuns, runID)
	}
	if strings.TrimSpace(sessionID) != "" {
		if r.backgroundRunSessions == nil {
			r.backgroundRunSessions = make(map[string]string)
		}
		r.backgroundRunSessions[runID] = strings.TrimSpace(sessionID)
	}
}

func (r *REPL) removeBackgroundRun(runID string) {
	runID = strings.TrimSpace(runID)
	if runID == "" || r == nil {
		return
	}
	r.backgroundRuns = slices.DeleteFunc(r.backgroundRuns, func(item string) bool {
		return strings.TrimSpace(item) == runID
	})
	if r.backgroundRunSessions != nil {
		delete(r.backgroundRunSessions, runID)
	}
}

func (r *REPL) isBackgroundRun(runID string) bool {
	if r == nil {
		return false
	}
	runID = strings.TrimSpace(runID)
	return slices.ContainsFunc(r.backgroundRuns, func(item string) bool {
		return strings.TrimSpace(item) == runID
	})
}

func (r *REPL) backgroundRunForSession(sessionID string) string {
	if r == nil || strings.TrimSpace(sessionID) == "" {
		return ""
	}
	for _, runID := range r.backgroundRuns {
		if strings.TrimSpace(r.backgroundRunSessions[strings.TrimSpace(runID)]) == strings.TrimSpace(sessionID) {
			return strings.TrimSpace(runID)
		}
	}
	return ""
}

func (r *REPL) renderLastRun(ctx context.Context) error {
	return r.renderRunDetail(ctx, "")
}

func (r *REPL) renderRunDetail(ctx context.Context, runID string) error {
	runID = strings.TrimSpace(runID)
	if runID == "" {
		items := []RunSummary(nil)
		if sessionID, err := r.currentServiceSessionID(ctx); err == nil && sessionID != "" {
			items, err = r.service.ListRuns(ctx, sessionID, 1)
			if err != nil {
				return err
			}
		}
		if len(items) == 0 {
			items, err := r.service.ListRuns(ctx, "", 1)
			if err != nil {
				return err
			}
			if len(items) == 0 {
				r.renderer.SystemLine("No runs found.")
				return nil
			}
			runID = items[0].ID
		} else {
			runID = items[0].ID
		}
	}
	detail, err := r.service.GetRunDetail(ctx, runID)
	if err != nil {
		return err
	}
	if detail == nil {
		r.renderer.SystemLine("Run detail is unavailable.")
		return nil
	}
	lines := []string{
		fmt.Sprintf("Phase      %s", defaultString(detail.Run.Phase, "-")),
		fmt.Sprintf("Tool       %s", defaultString(firstNonEmpty(detail.Tool, detail.Run.ToolName), "-")),
		fmt.Sprintf("Scope      %s", defaultString(firstNonEmpty(detail.Scope, detail.Run.ScopeSummary), "-")),
		fmt.Sprintf("Elapsed    %s", runElapsedLabel(detail.Run.CreatedAt)),
	}
	if strings.TrimSpace(detail.Run.Error) != "" {
		lines = append(lines, fmt.Sprintf("Error      %s", compact(detail.Run.Error, 92)))
	}
	if detail.ScopeDetails != nil {
		lines = append(lines, "")
		lines = append(lines, "Scope")
		if strings.TrimSpace(detail.ScopeDetails.SideEffectScope) != "" {
			lines = append(lines, "side effect: "+detail.ScopeDetails.SideEffectScope)
		}
		lines = append(lines, "destructive: "+boolLabel(detail.ScopeDetails.Destructive))
		if len(detail.ScopeDetails.Resources) > 0 {
			lines = append(lines, "resources: "+compact(strings.Join(detail.ScopeDetails.Resources, ", "), 92))
		}
		if strings.TrimSpace(detail.ScopeDetails.Summary) != "" {
			lines = append(lines, "summary: "+compact(detail.ScopeDetails.Summary, 92))
		}
	}
	if detail.Semantic != nil {
		lines = append(lines, "")
		lines = append(lines, "Semantic Signal")
		lines = append(lines, "language: "+defaultString(detail.Semantic.Language, "unknown"))
		lines = append(lines, "current info: "+boolLabel(detail.Semantic.RequiresCurrentInfo))
		lines = append(lines, "needs reference: "+boolLabel(detail.Semantic.NeedsReference))
		lines = append(lines, "needs confirmation: "+boolLabel(detail.Semantic.NeedsConfirmation))
		lines = append(lines, fmt.Sprintf("triage ready: %s  task contract ready: %s",
			boolLabel(detail.Semantic.TriageReady),
			boolLabel(detail.Semantic.TaskContractReady),
		))
		if len(detail.Semantic.SuggestedDomains) > 0 {
			lines = append(lines, "domains: "+compact(strings.Join(detail.Semantic.SuggestedDomains, ", "), 92))
		}
		if strings.TrimSpace(detail.Semantic.JobType) != "" {
			lines = append(lines, "job type: "+detail.Semantic.JobType)
		}
		if strings.TrimSpace(detail.Semantic.TargetSummary) != "" {
			lines = append(lines, "target: "+compact(detail.Semantic.TargetSummary, 92))
		}
		if len(detail.Semantic.CapabilityHints) > 0 {
			lines = append(lines, "capabilities: "+compact(strings.Join(detail.Semantic.CapabilityHints, ", "), 92))
		}
		if len(detail.Semantic.DeliverableKinds) > 0 {
			lines = append(lines, "deliverables: "+compact(strings.Join(detail.Semantic.DeliverableKinds, ", "), 92))
		}
		if len(detail.Semantic.MissingInfoIDs) > 0 {
			lines = append(lines, "missing info: "+compact(strings.Join(detail.Semantic.MissingInfoIDs, ", "), 92))
		}
		if strings.TrimSpace(detail.Semantic.Reason) != "" {
			lines = append(lines, "reason: "+compact(detail.Semantic.Reason, 92))
		}
	}
	if detail.Workflow != nil {
		lines = append(lines, "")
		lines = append(lines, "Workflow")
		lines = append(lines, "mode: "+defaultString(detail.Workflow.Mode, "direct"))
		if detail.Workflow.ContinuationIndex > 0 || detail.Workflow.TotalRoundsUsed > 0 {
			lines = append(lines, fmt.Sprintf("continuation: %d / total rounds used: %d", detail.Workflow.ContinuationIndex, detail.Workflow.TotalRoundsUsed))
		}
		lines = append(lines, "yielded: "+boolLabel(detail.Workflow.Yielded))
		if strings.TrimSpace(detail.Workflow.YieldReason) != "" {
			lines = append(lines, "yield reason: "+detail.Workflow.YieldReason)
		}
	}
	if detail.Delegation != nil {
		lines = append(lines, "")
		lines = append(lines, "Delegation")
		lines = append(lines, "enabled: "+boolLabel(detail.Delegation.Enabled))
		lines = append(lines, fmt.Sprintf("parallel tasks: %d", detail.Delegation.ParallelTasks))
		lines = append(lines, fmt.Sprintf("serial fallback: %d", detail.Delegation.SerialFallback))
		if strings.TrimSpace(detail.Delegation.SideEffectClass) != "" {
			lines = append(lines, "side effect class: "+detail.Delegation.SideEffectClass)
		}
	}
	if detail.ExecutionGraph != nil && len(detail.ExecutionGraph.Tasks) > 0 {
		lines = append(lines, "")
		lines = append(lines, "Execution Graph")
		if detail.ExecutionGraph.SingleSession || detail.ExecutionGraph.SessionLocking {
			lines = append(lines, fmt.Sprintf("conversation contract: single_conversation=%s  locking=%s", boolLabel(detail.ExecutionGraph.SingleSession), boolLabel(detail.ExecutionGraph.SessionLocking)))
		}
		for _, task := range detail.ExecutionGraph.Tasks {
			lines = append(lines, fmt.Sprintf("[%s] %s", defaultString(task.Status, "unknown"), compact(defaultString(task.Title, task.ID), 72)))
			if strings.TrimSpace(task.SideEffectScope) != "" || len(task.ResourceKeys) > 0 {
				lines = append(lines, fmt.Sprintf("  scope=%s  resources=%s",
					defaultString(task.SideEffectScope, "-"),
					compact(strings.Join(task.ResourceKeys, ", "), 56),
				))
			}
			if task.AttemptCount > 0 || strings.TrimSpace(task.MergeStrategy) != "" {
				lines = append(lines, fmt.Sprintf("  attempt=%d  merge=%s",
					task.AttemptCount,
					defaultString(task.MergeStrategy, "-"),
				))
			}
			if strings.TrimSpace(task.Summary) != "" {
				lines = append(lines, "  "+compact(task.Summary, 88))
			}
		}
	}
	if strings.TrimSpace(detail.Output) != "" {
		lines = append(lines, "")
		lines = append(lines, "Output")
		lines = append(lines, compact(detail.Output, 92))
	}
	timeline := detail.Timeline
	if len(timeline) == 0 {
		timeline = r.timelineForRun(detail.Run.ID)
	}
	if len(timeline) > 0 {
		lines = append(lines, "")
		lines = append(lines, "Timeline")
		lines = append(lines, r.renderer.TimelineLines(timeline)...)
	}
	deliveryDetail, _ := r.service.GetRunDelivery(ctx, detail.Run.ID)
	if deliveryDetail != nil {
		overallStatus := normalizeDeliveryDisplayStatus(deliveryDetail)
		lines = append(lines, "")
		lines = append(lines, fmt.Sprintf("Delivery   [%s]", overallStatus))
		if strings.TrimSpace(deliveryDetail.Summary) != "" {
			lines = append(lines, compact(deliveryDetail.Summary, 92))
		}
		for _, target := range deliveryDetail.Targets {
			status := normalizeTargetDeliveryStatus(strings.TrimSpace(target.Status))
			attempts := ""
			if target.Attempts > 0 {
				attempts = fmt.Sprintf(" %d attempt(s)", target.Attempts)
			}
			nextAt := ""
			if strings.TrimSpace(target.NextAt) != "" {
				nextAt = " next " + compact(target.NextAt, 20)
			}
			lines = append(lines, fmt.Sprintf("%-10s %-18s %s%s%s",
				compact(defaultString(target.Kind, "inline"), 10),
				compact(defaultString(target.Label, "-"), 18),
				compact(status, 14),
				attempts,
				nextAt,
			))
		}
		if len(deliveryDetail.Receipts) > 0 {
			lines = append(lines, "")
			lines = append(lines, "Receipts")
			for _, receipt := range deliveryDetail.Receipts[:min(len(deliveryDetail.Receipts), 5)] {
				status := normalizeTargetDeliveryStatus(strings.TrimSpace(receipt.Status))
				summary := compact(firstNonEmpty(receipt.Error, receipt.TargetLabel, receipt.Adapter), 64)
				lines = append(lines, fmt.Sprintf("%-20s %-12s %s",
					compact(defaultString(receipt.At, "-"), 20),
					compact(status, 12),
					summary,
				))
			}
		}
	} else if detail.Delivery != nil {
		lines = append(lines, "")
		lines = append(lines, fmt.Sprintf("Delivery   %s", deliverySummaryLine(detail.Delivery)))
	}
	if detail.Automation != nil {
		lines = append(lines, fmt.Sprintf("Automation %s", automationSummaryLine(detail.Automation)))
	}
	summary := fmt.Sprintf("status: %s  remote: %s  conversation: %s",
		defaultString(detail.Run.Status, "unknown"),
		defaultString(firstNonEmpty(detail.Target, detail.Run.Target, r.targetName), "local"),
		defaultString(firstNonEmpty(detail.Run.SessionKey, r.sessionKey, detail.Run.SessionID), "-"),
	)
	actions := "/continue <task|conversation>  /runs recent  Esc back"
	switch normalizedExecutionState(firstNonEmpty(detail.Run.Status, detail.Run.Phase)) {
	case "running":
		actions = "/pause  /runs recent  Esc back"
	case "paused":
		actions = "/continue " + detail.Run.ID + "  /runs recent  Esc back"
	}
	r.openInfoPanel("Run "+detail.Run.ID, append([]string{summary}, lines...), actions)
	r.refreshDeliveryState(detail.Run.ID)
	return nil
}

func boolLabel(value bool) string {
	if value {
		return "yes"
	}
	return "no"
}

func (r *REPL) resumeReference(ctx context.Context, ref string) error {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return r.resumePaused(ctx, false)
	}
	if detail, err := r.service.GetRunDetail(ctx, ref); err == nil && detail != nil && strings.TrimSpace(detail.Run.ID) != "" {
		return r.resumeRunReference(ctx, detail)
	}
	return r.resumeSessionReference(ctx, ref)
}

func (r *REPL) resumeRunReference(ctx context.Context, detail *RunDetail) error {
	if detail == nil {
		return fmt.Errorf("run detail is unavailable")
	}
	sessionKey := strings.TrimSpace(detail.Run.SessionKey)
	if sessionKey == "" && strings.TrimSpace(detail.Run.SessionID) != "" {
		session, err := r.service.GetSession(ctx, detail.Run.SessionID)
		if err == nil && session != nil {
			sessionKey = strings.TrimSpace(session.Summary.Key)
		}
	}
	if sessionKey == "" {
		return fmt.Errorf("task %s is not linked to a resumable conversation", detail.Run.ID)
	}
	if err := r.switchSession(ctx, sessionKey, false); err != nil {
		return err
	}
	lines := []string{
		fmt.Sprintf("Conversation: %s", r.sessionKey),
		fmt.Sprintf("Task: %s", detail.Run.ID),
		fmt.Sprintf("Status: %s", defaultString(detail.Run.Status, "unknown")),
		fmt.Sprintf("Phase: %s", defaultString(detail.Run.Phase, "-")),
	}
	if strings.TrimSpace(detail.Output) != "" {
		lines = append(lines, "Last output: "+compact(detail.Output, 92))
	}
	if strings.TrimSpace(detail.Run.Error) != "" {
		lines = append(lines, "Last error: "+compact(detail.Run.Error, 92))
	}
	r.openInfoPanel("Resumed Work", lines, "/history  /last  /context  Esc back")
	r.renderer.RenderSystemEvent(fmt.Sprintf("Resumed work from task %s in conversation %s.", detail.Run.ID, r.sessionKey))
	return nil
}

func (r *REPL) resumeSessionReference(ctx context.Context, sessionKey string) error {
	if err := r.switchSession(ctx, sessionKey, false); err != nil {
		return err
	}
	lines := []string{
		fmt.Sprintf("Conversation: %s", r.sessionKey),
		fmt.Sprintf("Model: %s", defaultString(r.sessionModel, "(default)")),
	}
	if detail, _, err := r.currentServiceSession(ctx); err == nil && detail != nil {
		lines = append(lines, fmt.Sprintf("Turns: %d", len(detail.Messages)))
	}
	r.openInfoPanel("Conversation Resumed", lines, "/history  /context  /runs recent  Esc back")
	r.renderer.RenderSystemEvent(fmt.Sprintf("Resumed conversation %s.", r.sessionKey))
	return nil
}

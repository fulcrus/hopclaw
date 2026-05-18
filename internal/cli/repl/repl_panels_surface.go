package repl

import (
	"context"
	"fmt"
	"strings"
)

func (r *REPL) renderQuitConfirmation() {
	if r == nil || r.renderer == nil {
		return
	}
	r.quitConfirmPending = true
	r.renderer.StopSpinner()
	r.renderer.RenderCard(CardSpec{
		Title: "Quit HopClaw?",
		Rows: []CardRow{
			{Label: "Status", Value: "Current task is still running."},
		},
		Actions: "[q] quit terminal and cancel task  [b] back",
		Width:   72,
	})
}

func (r *REPL) dismissQuitConfirmation() {
	if r == nil || !r.quitConfirmPending {
		return
	}
	r.quitConfirmPending = false
	r.refreshViewState()
	r.renderDock()
	if r.running && !r.seenReplyText && !r.pendingApproval {
		r.renderer.StartSpinner("Waiting for response…")
	}
}

func (r *REPL) clearQuitConfirmation() {
	if r == nil {
		return
	}
	r.quitConfirmPending = false
}

func (r *REPL) renderApprovals(ctx context.Context, status string, limit int) error {
	items, err := r.service.ListApprovals(ctx, status, limit)
	if err != nil {
		return err
	}
	if len(items) == 0 {
		r.renderer.SystemLine("No approval requests found.")
		return nil
	}
	approvalByID := make(map[string]ApprovalSummary, len(items))
	buildPanelItems := func(items []ApprovalSummary) []panelItem {
		approvalByID = make(map[string]ApprovalSummary, len(items))
		panelItems := make([]panelItem, 0, len(items))
		for _, item := range items {
			approvalByID[item.ID] = item
			panelItems = append(panelItems, approvalPanelItem(item))
		}
		return panelItems
	}
	panel := newSelectionPanel(r, "Approvals", "Search:", "Enter details  a approve  n deny  Esc back", buildPanelItems(items))
	panel.emptyText = "No approval requests found."
	panel.load = func(query string) ([]panelItem, error) {
		loaded, err := r.service.ListApprovals(ctx, status, limit)
		if err != nil {
			return nil, err
		}
		return matchPanelItems(buildPanelItems(loaded), query), nil
	}
	panel.onConfirm = func(item panelItem) (string, error) {
		approval, ok := approvalByID[item.ID]
		if !ok {
			panel.status = "Approval detail is unavailable."
			return "", nil
		}
		panel.status = joinNonEmpty("  ",
			"ID "+defaultString(approval.ID, "-"),
			defaultString(approval.Status, "pending"),
			defaultString(approval.ToolName, "tool"),
			compact(defaultString(approval.PolicySummary, "no policy summary"), 52),
		)
		return "", nil
	}
	resolveSelected := func(panel *selectionPanel, item panelItem, approved bool) (string, error) {
		if strings.TrimSpace(item.ID) == "" {
			return "", nil
		}
		resolved, err := r.service.ResolveApproval(ctx, item.ID, approved)
		if err != nil {
			return "", err
		}
		statusText := "denied"
		if approved {
			statusText = "approved"
		}
		if resolved != nil && strings.TrimSpace(resolved.Status) != "" {
			statusText = strings.TrimSpace(resolved.Status)
		}
		if err := panel.refresh(); err != nil {
			return "", err
		}
		panel.status = fmt.Sprintf("Approval %s %s.", strings.TrimSpace(item.ID), statusText)
		return "", nil
	}
	panel.hotkeys = map[rune]func(*selectionPanel, panelItem) (string, error){
		'a': func(panel *selectionPanel, item panelItem) (string, error) {
			return resolveSelected(panel, item, true)
		},
		'n': func(panel *selectionPanel, item panelItem) (string, error) {
			return resolveSelected(panel, item, false)
		},
	}
	r.openPromptPanel(panel)
	return nil
}

func (r *REPL) renderQualitySnapshot(ctx context.Context) error {
	snapshot, err := r.service.QualitySnapshot(ctx)
	if err != nil {
		return err
	}
	if snapshot == nil {
		r.renderer.SystemLine("Quality snapshot is unavailable.")
		return nil
	}
	status := defaultString(snapshot.Status, "ok")
	if snapshot.BlockerCount > 0 && status == "ok" {
		status = "warn"
	}
	readiness := "yes"
	if !snapshot.Ready {
		readiness = "no"
	}
	lines := []string{
		fmt.Sprintf("Release readiness: %s", readiness),
		fmt.Sprintf("Status: %s", status),
		fmt.Sprintf("Blockers: %d", snapshot.BlockerCount),
		fmt.Sprintf("Warnings: %d", len(snapshot.Warnings)),
		fmt.Sprintf("Last check: %s", defaultString(snapshot.LastCheck, "-")),
		fmt.Sprintf("Runs: %d total / %d terminal", snapshot.RunCount, snapshot.TerminalRunCount),
		fmt.Sprintf("Task success: %s", defaultString(snapshot.TaskSuccess, "n/a")),
		fmt.Sprintf("False success: %s", defaultString(snapshot.FalseSuccess, "n/a")),
		fmt.Sprintf("Verification failure: %s", defaultString(snapshot.VerificationFailure, "n/a")),
	}
	for _, blocker := range snapshot.Blockers {
		lines = append(lines, "- "+blocker)
	}
	for _, warning := range snapshot.Warnings {
		lines = append(lines, "- "+warning)
	}
	r.openInfoPanel("Quality Gate", lines, "/quality  Esc back")
	return nil
}

func (r *REPL) renderEvalSuites(ctx context.Context) error {
	items, err := r.service.ListEvalSuites(ctx)
	if err != nil {
		return err
	}
	if len(items) == 0 {
		r.renderer.SystemLine("No evaluation suites configured.")
		return nil
	}
	lines := make([]string, 0, len(items))
	for _, item := range items {
		name := defaultString(item.Name, item.ID)
		surface := defaultString(item.Surface, "-")
		lines = append(lines, fmt.Sprintf("%-18s %-10s %d cases", compact(name, 18), compact(surface, 10), item.CaseCount))
	}
	r.openInfoPanel("Eval Suites", lines, "/evals run <suite_id>  Esc back")
	return nil
}

func (r *REPL) showPanel(title string, lines []string, actions string) {
	r.panelController = nil
	r.activePanel = strings.TrimSpace(title)
	r.refreshViewState()
	if r.renderer != nil {
		r.renderer.RenderPanel(title, lines, actions, r.terminalWidth())
	}
}

func (r *REPL) clearPanel() {
	if strings.TrimSpace(r.activePanel) == "" && r.panelController == nil {
		return
	}
	r.panelController = nil
	r.activePanel = ""
	r.refreshViewState()
}

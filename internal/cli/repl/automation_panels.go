package repl

import (
	"context"
	"fmt"
	"strings"
)

func (r *REPL) renderAutomationPanel(ctx context.Context) error {
	items, err := r.service.ListAutomations(ctx, 20)
	if err != nil {
		return err
	}
	if len(items) == 0 {
		if !r.automationAvailable(ctx) {
			r.renderer.SystemLine("Automation requires a gateway connection.")
			return nil
		}
		r.renderer.SystemLine("No automations found.")
		return nil
	}

	automationByID := make(map[string]AutomationItem, len(items))
	buildPanelItems := func(items []AutomationItem) []panelItem {
		automationByID = make(map[string]AutomationItem, len(items))
		panelItems := make([]panelItem, 0, len(items))
		for _, item := range items {
			automationByID[item.ID] = item
			panelItems = append(panelItems, panelItem{
				ID:         item.ID,
				Text:       automationPanelRow(item),
				SearchText: strings.Join([]string{item.ID, item.Name, item.Kind, item.Status, item.Schedule, item.Delivery, item.NextRun, item.Health}, " "),
			})
		}
		return panelItems
	}
	panel := newSelectionPanel(r, "Automation", "", "Enter inspect  r resume  p pause  n run now  Esc back", buildPanelItems(items))
	panel.load = func(string) ([]panelItem, error) {
		loaded, err := r.service.ListAutomations(ctx, 20)
		if err != nil {
			return nil, err
		}
		return buildPanelItems(loaded), nil
	}
	panel.onConfirm = func(item panelItem) (string, error) {
		return "", r.renderAutomationDetail(ctx, automationByID[item.ID])
	}
	panel.hotkeys = map[rune]func(*selectionPanel, panelItem) (string, error){
		'r': func(panel *selectionPanel, item panelItem) (string, error) {
			automation, ok := automationByID[item.ID]
			if !ok {
				return "", nil
			}
			if err := r.service.ResumeAutomation(ctx, automation.Kind, automation.ID); err != nil {
				return "", err
			}
			if err := panel.refresh(); err != nil {
				return "", err
			}
			panel.status = "Resumed " + defaultString(automation.Name, automation.ID) + "."
			return "", nil
		},
		'p': func(panel *selectionPanel, item panelItem) (string, error) {
			automation, ok := automationByID[item.ID]
			if !ok {
				return "", nil
			}
			if err := r.service.PauseAutomation(ctx, automation.Kind, automation.ID); err != nil {
				return "", err
			}
			if err := panel.refresh(); err != nil {
				return "", err
			}
			panel.status = "Paused " + defaultString(automation.Name, automation.ID) + "."
			return "", nil
		},
		'n': func(panel *selectionPanel, item panelItem) (string, error) {
			automation, ok := automationByID[item.ID]
			if !ok {
				return "", nil
			}
			if err := r.service.RunAutomationNow(ctx, automation.Kind, automation.ID); err != nil {
				return "", err
			}
			if err := panel.refresh(); err != nil {
				return "", err
			}
			panel.status = "Triggered " + defaultString(automation.Name, automation.ID) + "."
			return "", nil
		},
	}
	r.openPromptPanel(panel)
	return nil
}

func (r *REPL) renderAutomationDetail(ctx context.Context, item AutomationItem) error {
	if detail, err := r.service.GetAutomationDetail(ctx, item.Kind, item.ID); err == nil && detail != nil {
		item = *detail
	}
	title := defaultString(strings.TrimSpace(item.Name), strings.TrimSpace(item.ID))
	kind := defaultString(item.Kind, "cron")
	id := defaultString(item.ID, "-")
	lines := []string{
		"Name: " + defaultString(item.Name, "-"),
		"Kind: " + kind,
		"Status: " + defaultString(item.Status, "-"),
		"Schedule: " + defaultString(item.Schedule, "-"),
		"Delivery: " + defaultString(item.Delivery, "-"),
		"Next run: " + defaultString(item.NextRun, "-"),
		"Health: " + defaultString(item.Health, "-"),
	}
	if item.SetupContract != nil && item.SetupContract.Status == "needs_input" {
		lines = append(lines, "", "Setup required: "+defaultString(item.SetupContract.Summary, "incomplete configuration"))
		for _, slot := range item.SetupContract.Slots {
			value := defaultString(slot.Value, "missing")
			lines = append(lines, fmt.Sprintf("  %s: %s", slot.Field, value))
			if slot.Question != "" && slot.Value == "" {
				lines = append(lines, fmt.Sprintf("    %s", slot.Question))
			}
		}
	}
	runs, _ := r.service.ListRuns(ctx, "", 10)
	automationRuns := filterRunsByAutomation(runs, item.ID, item.Kind)
	if len(automationRuns) > 0 {
		lines = append(lines, "", "Recent runs:")
		for _, run := range automationRuns[:min(len(automationRuns), 3)] {
			lines = append(lines, fmt.Sprintf("  %s  %s  %s",
				compact(run.ID, 12), defaultString(run.Status, "-"), compact(run.CreatedAt, 20)))
		}
	}
	actions := fmt.Sprintf("/automation run %s %s  /automation pause %s %s  /automation resume %s %s  Esc back",
		kind, id, kind, id, kind, id)
	r.openInfoPanel("Automation "+title, lines, actions)
	return nil
}

func filterRunsByAutomation(runs []RunSummary, automationID, automationKind string) []RunSummary {
	automationID = strings.TrimSpace(automationID)
	automationKind = strings.TrimSpace(automationKind)
	var filtered []RunSummary
	for _, run := range runs {
		if run.Automation == nil {
			continue
		}
		runAutomationID := strings.TrimSpace(run.Automation.ID)
		runAutomationKind := strings.TrimSpace(run.Automation.Kind)
		switch {
		case automationID != "" && runAutomationID != "":
			if runAutomationID == automationID {
				filtered = append(filtered, run)
			}
		case automationID != "" && runAutomationID == "" && strings.EqualFold(runAutomationKind, automationKind):
			filtered = append(filtered, run)
		case automationID == "" && strings.EqualFold(runAutomationKind, automationKind):
			filtered = append(filtered, run)
		}
	}
	return filtered
}

func (r *REPL) renderPromotePanel(ctx context.Context, schedule string) error {
	detail, err := r.promotionSourceRun(ctx)
	if err != nil {
		return err
	}
	if detail == nil {
		r.renderer.SystemLine("No completed runs available for promotion.")
		return nil
	}
	prompt := strings.TrimSpace(r.promotionPrompt(ctx, detail))
	if prompt == "" {
		prompt = "Prepare weekday ops briefing"
	}
	delivery := strings.TrimSpace(r.promotionDelivery(ctx, detail.Run.ID, detail))
	schedule = defaultString(strings.TrimSpace(schedule), "0 9 * * 1-5")
	panel := newSelectionPanel(r, "Promote To Automation", "Prompt:", "Enter create  e edit before save  Esc cancel", []panelItem{{
		ID: detail.Run.ID,
		Text: joinNonEmpty("  ",
			"Source run: "+defaultString(detail.Run.ID, "-"),
			"Suggested kind: cron",
			"Schedule: "+schedule,
			"Delivery: "+defaultString(delivery, "-"),
		),
		SearchText: strings.Join([]string{detail.Run.ID, detail.Run.SessionID, detail.Run.SessionKey, prompt, delivery}, " "),
	}})
	panel.query = prompt
	panel.onConfirm = func(_ panelItem) (string, error) {
		content := strings.TrimSpace(panel.query)
		if content == "" {
			panel.status = "Prompt is required before promotion."
			return "", nil
		}
		created, err := r.service.CreateAutomation(ctx, AutomationCreateRequest{
			Name:         promotedAutomationName(content, detail.Run.ID),
			Kind:         "cron",
			Prompt:       content,
			Model:        defaultString(detail.Run.Model, r.effectiveModel()),
			SessionKey:   defaultString(detail.Run.SessionKey, r.sessionKey),
			Delivery:     delivery,
			ScheduleKind: "cron",
			Expression:   schedule,
			Enabled:      true,
		})
		if err != nil {
			return "", err
		}
		if created == nil {
			panel.status = "Automation creation is unavailable on this runtime."
			return "", nil
		}
		r.renderAutomationPromotionSnapshot(detail.Run.ID, content, delivery)
		r.openInfoPanel("Automation Created", []string{
			fmt.Sprintf("Source run: %s", detail.Run.ID),
			fmt.Sprintf("Automation: %s (%s)", defaultString(created.Name, created.ID), defaultString(created.Kind, "cron")),
			fmt.Sprintf("Schedule: %s", defaultString(created.Schedule, "0 9 * * 1-5")),
			fmt.Sprintf("Delivery: %s", defaultString(delivery, "-")),
		}, "/automation  Esc back")
		return "", nil
	}
	panel.hotkeys = map[rune]func(*selectionPanel, panelItem) (string, error){
		'e': func(panel *selectionPanel, _ panelItem) (string, error) {
			panel.status = "Edit the prompt line, then press Enter to create."
			return "", nil
		},
	}
	r.openPromptPanel(panel)
	return nil
}

func (r *REPL) promotionSourceRun(ctx context.Context) (*RunDetail, error) {
	sessionID, err := r.currentServiceSessionID(ctx)
	if err == nil && sessionID != "" {
		candidates, listErr := r.service.ListRuns(ctx, sessionID, 10)
		if listErr != nil {
			return nil, listErr
		}
		if detail := r.firstCompletedPromotionRun(ctx, candidates); detail != nil {
			return detail, nil
		}
	}
	candidates, err := r.service.ListRuns(ctx, "", 10)
	if err != nil {
		return nil, err
	}
	return r.firstCompletedPromotionRun(ctx, candidates), nil
}

func (r *REPL) firstCompletedPromotionRun(ctx context.Context, items []RunSummary) *RunDetail {
	for _, item := range items {
		if normalizedExecutionState(firstNonEmpty(item.Status, item.Phase)) != "completed" {
			continue
		}
		detail, err := r.service.GetRunDetail(ctx, item.ID)
		if err == nil && detail != nil {
			return detail
		}
	}
	return nil
}

func (r *REPL) promotionPrompt(ctx context.Context, detail *RunDetail) string {
	if detail == nil {
		return strings.TrimSpace(r.lastSubmitText)
	}
	if sessionID := strings.TrimSpace(detail.Run.SessionID); sessionID != "" {
		if session, err := r.service.GetSession(ctx, sessionID); err == nil && session != nil {
			for index := len(session.Messages) - 1; index >= 0; index-- {
				if strings.EqualFold(strings.TrimSpace(session.Messages[index].Role), "user") && strings.TrimSpace(session.Messages[index].Content) != "" {
					return strings.TrimSpace(session.Messages[index].Content)
				}
			}
		}
	}
	if strings.TrimSpace(r.lastSubmitText) != "" {
		return strings.TrimSpace(r.lastSubmitText)
	}
	return strings.TrimSpace(detail.Output)
}

func (r *REPL) promotionDelivery(ctx context.Context, runID string, detail *RunDetail) string {
	if delivery, err := r.service.GetRunDelivery(ctx, runID); err == nil && delivery != nil {
		if len(delivery.Targets) > 0 {
			target := delivery.Targets[0]
			return strings.TrimSpace(joinNonEmpty("/", target.Kind, target.Label))
		}
		if strings.TrimSpace(delivery.Summary) != "" {
			return strings.TrimSpace(delivery.Summary)
		}
	}
	if detail != nil && detail.Delivery != nil {
		return strings.TrimSpace(detail.Delivery.Summary)
	}
	return ""
}

func promotedAutomationName(prompt, runID string) string {
	trimmed := strings.TrimSpace(prompt)
	if trimmed == "" {
		return "promoted-" + strings.TrimSpace(runID)
	}
	words := strings.Fields(trimmed)
	if len(words) > 4 {
		words = words[:4]
	}
	return strings.ToLower(strings.Join(words, "-"))
}

func (r *REPL) automationAvailable(ctx context.Context) bool {
	if r == nil {
		return false
	}
	r.refreshReadinessProjection(ctx, false)
	if r.readinessSnapshot == nil {
		return false
	}
	for _, item := range r.readinessSnapshot.Categories {
		if item.ID != "automation_runtime" {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(item.Status), "unknown") {
			return false
		}
		return true
	}
	return false
}

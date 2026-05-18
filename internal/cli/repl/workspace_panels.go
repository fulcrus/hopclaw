package repl

import (
	"context"
	"fmt"
	"os"
	"slices"
	"strings"

	"github.com/fulcrus/hopclaw/agent"
	"github.com/fulcrus/hopclaw/contextengine"
)

func (r *REPL) renderSessionPicker(ctx context.Context) error {
	sessions, err := r.service.ListSessions(ctx)
	if err != nil {
		return err
	}
	if len(sessions) == 0 {
		r.renderer.SystemLine("No conversations found.")
		return nil
	}
	slices.SortFunc(sessions, func(a, b SessionSummary) int {
		return strings.Compare(a.Key, b.Key)
	})
	items := make([]panelItem, 0, len(sessions))
	currentIndex := 0
	currentSessionID, _ := r.currentServiceSessionID(ctx)
	for index, item := range sessions {
		items = append(items, sessionPanelItem(item, currentSessionID))
		items[index].ID = item.Key
		if item.ID == currentSessionID {
			currentIndex = index
		}
	}
	panel := newSelectionPanel(r, "Conversations", "Search:", "Enter switch conversation  n new conversation  i inspect conversation  r reset conversation  Esc back", items)
	panel.selected = currentIndex
	panel.onConfirm = func(item panelItem) (string, error) {
		return "/session " + item.ID, nil
	}
	panel.hotkeys = map[rune]func(*selectionPanel, panelItem) (string, error){
		'n': func(panel *selectionPanel, _ panelItem) (string, error) {
			key := strings.TrimSpace(strings.Join(strings.Fields(panel.query), "-"))
			if key == "" {
				panel.status = "Type a conversation key, then press n."
				return "", nil
			}
			return "/session new " + key, nil
		},
		'i': func(panel *selectionPanel, item panelItem) (string, error) {
			panel.status = "Switch first, then use /history or /last to inspect " + item.ID + "."
			return "", nil
		},
		'r': func(panel *selectionPanel, item panelItem) (string, error) {
			if strings.TrimSpace(item.ID) != strings.TrimSpace(r.sessionKey) {
				panel.status = "Reset only applies to the current conversation."
				return "", nil
			}
			r.setPromptPanel(switchConfirmPanel(r, "Reset Conversation", []string{
				fmt.Sprintf("Reset conversation %s?", r.sessionKey),
				"This clears the current conversation state and reopens it.",
			}, "/reset "+internalConfirmedArg, panel))
			return "", nil
		},
	}
	r.openPromptPanel(panel)
	return nil
}

func (r *REPL) renderRemotePicker(ctx context.Context) error {
	if r.targetManager == nil {
		r.renderer.SystemLine("Current runtime: " + targetDescriptor(r.targetName, r.targetKind))
		return nil
	}
	targets, err := r.targetManager.ListTargets(ctx)
	if err != nil {
		return remoteCommandError(err)
	}
	if len(targets) == 0 {
		r.renderer.SystemLine("Current runtime: " + targetDescriptor(r.targetName, r.targetKind))
		return nil
	}
	items := make([]panelItem, 0, len(targets))
	currentIndex := 0
	for index, item := range targets {
		items = append(items, remotePanelItem(item, r.targetName))
		if item.Name == r.targetName {
			currentIndex = index
		}
	}
	panel := newSelectionPanel(r, "Remotes", "", "Enter switch  l login  i inspect  o logout  Esc back", items)
	panel.selected = currentIndex
	panel.onConfirm = func(item panelItem) (string, error) {
		selectedKind := ""
		for _, candidate := range targets {
			if candidate.Name == item.ID {
				selectedKind = candidate.Kind
				break
			}
		}
		r.setPromptPanel(switchConfirmPanel(r, "Switch Remote", []string{
			fmt.Sprintf("Switch to %s?", targetDescriptor(item.ID, selectedKind)),
			"Current conversation binding will move to the selected runtime target.",
			"Your local working directory will remain unchanged.",
		}, "/remote "+internalConfirmedArg+" "+item.ID, panel))
		return "", nil
	}
	panel.hotkeys = map[rune]func(*selectionPanel, panelItem) (string, error){
		'l': func(_ *selectionPanel, item panelItem) (string, error) {
			if item.ID == "local" {
				return "", nil
			}
			return "/remote login " + item.ID, nil
		},
		'o': func(panel *selectionPanel, item panelItem) (string, error) {
			if item.ID == "local" {
				panel.status = "Local has no remote credentials to clear."
				return "", nil
			}
			return "/remote logout " + item.ID, nil
		},
		'i': func(panel *selectionPanel, item panelItem) (string, error) {
			selectedKind := ""
			for _, candidate := range targets {
				if candidate.Name == item.ID {
					selectedKind = candidate.Kind
					break
				}
			}
			panel.status = "Selected runtime: " + targetDescriptor(item.ID, selectedKind)
			return "", nil
		},
	}
	r.openPromptPanel(panel)
	return nil
}

func (r *REPL) renderModelPicker(ctx context.Context) error {
	models, err := r.loadModels(ctx)
	if err != nil {
		return err
	}
	current := r.effectiveModel()
	if current == "" {
		current = "(default)"
	}
	if len(models) == 0 {
		r.renderer.SystemLine(fmt.Sprintf("Current model: %s", current))
		return nil
	}
	items := make([]panelItem, 0, len(models))
	currentIndex := 0
	modelMap := make(map[string]ModelInfo, len(models))
	for index, item := range models {
		modelMap[item.ID] = item
		items = append(items, modelPanelItem(item, current))
		if item.ID == current {
			currentIndex = index
		}
	}
	panel := newSelectionPanel(r, "Models", "", "Enter choose model  t toggle think  Esc back", items)
	panel.selected = currentIndex
	panel.onConfirm = func(item panelItem) (string, error) {
		return "/model " + internalModelSelectArg + " " + item.ID, nil
	}
	panel.hotkeys = map[rune]func(*selectionPanel, panelItem) (string, error){
		't': func(panel *selectionPanel, item panelItem) (string, error) {
			model := modelMap[item.ID]
			if !model.SupportsThinking {
				panel.status = "Thinking mode is unavailable for the selected model."
				return "", nil
			}
			next := "on"
			if item.ID == current && r.thinking {
				next = "off"
			}
			return "/model " + internalModelThinkArg + " " + item.ID + " " + next, nil
		},
	}
	r.openPromptPanel(panel)
	return nil
}

func (r *REPL) renderContextPanel(ctx context.Context) error {
	if r.sessionID == "" && r.sessionKey == "" {
		return fmt.Errorf("no active conversation")
	}
	detail, _, err := r.currentServiceSession(ctx)
	if err != nil {
		return err
	}
	if detail == nil {
		detail = &SessionDetail{
			Summary: SessionSummary{
				ID:    strings.TrimSpace(r.serviceSessionID),
				Key:   strings.TrimSpace(r.sessionKey),
				Model: strings.TrimSpace(r.sessionModel),
			},
		}
	}
	r.refreshTransparencyProjection(ctx, true)

	messages := make([]contextengine.Message, 0, len(detail.Messages))
	for _, item := range detail.Messages {
		role := contextengine.RoleAssistant
		switch strings.ToLower(strings.TrimSpace(item.Role)) {
		case "user":
			role = contextengine.RoleUser
		case "tool":
			role = contextengine.RoleTool
		}
		messages = append(messages, contextengine.NewTextMessage(role, item.Content))
	}

	estimate := contextengine.CharRatioEstimator{}.EstimateMessages(messages)
	window := 0
	model := r.effectiveModel()
	for _, item := range r.modelCache {
		if item.ID == model {
			window = item.ContextWindow
			break
		}
	}
	percent := 0
	if window > 0 {
		percent = int(float64(estimate) / float64(window) * 100)
	}
	recommendation := "no action needed"
	switch {
	case percent >= 90:
		recommendation = "compact now"
	case percent >= 75:
		recommendation = "consider /compact soon"
	}
	userCount := 0
	assistantCount := 0
	toolCount := 0
	for _, item := range detail.Messages {
		switch strings.ToLower(strings.TrimSpace(item.Role)) {
		case "user":
			userCount++
		case "tool":
			toolCount++
		default:
			assistantCount++
		}
	}

	projectID := ""
	if project, err := r.ensureCurrentProject(ctx); err == nil && project != nil {
		projectID = project.ID
	}
	memories, _ := r.service.RecallMemories(ctx, r.sessionKey, projectID)
	keptItems := len(messages) + len(memories)
	trimmedItems := 0
	if pressure := r.contextPressure; pressure != nil {
		if pressure.WindowSize > 0 {
			window = pressure.WindowSize
		}
		estimate = pressure.UsedTokens
		percent = pressure.UsedPercent
		recommendation = pressure.Recommendation
		if pressure.KeptItems > 0 {
			keptItems = pressure.KeptItems
		}
		trimmedItems = pressure.TrimmedItems
	}
	lines := []string{
		fmt.Sprintf("[CTX %d%%] memory %d · kept %d · trimmed %d", percent, len(memories), keptItems, trimmedItems),
		"",
		fmt.Sprintf("Window        %s", contextWindowLabel(window)),
		fmt.Sprintf("Used          %d tokens (%d%%)", estimate, percent),
		fmt.Sprintf("Messages      %d", len(messages)),
		fmt.Sprintf("Memories      %d", len(memories)),
		fmt.Sprintf("Kept items    %d", keptItems),
		fmt.Sprintf("Trimmed items %d", trimmedItems),
		fmt.Sprintf("Attachments   %d", len(r.lastImages)+attachmentBlockCount(r.lastContentBlocks)),
		fmt.Sprintf("Headroom      %s", contextHeadroom(percent)),
		fmt.Sprintf("Recommendation %s", normalizeContextRecommendation(recommendation)),
	}
	if strip := strings.TrimSpace(memoryStripSummary(r.memoryUsage)); strip != "" {
		lines = append(lines, fmt.Sprintf("Using         %s", strip))
	}
	r.openInfoPanel("Context Window", lines, "/compact  Esc back")
	return nil
}

func (r *REPL) confirmRemoteSwitch(ctx context.Context, name string) bool {
	name = strings.TrimSpace(name)
	if name == "" {
		return false
	}
	if r.supportsInteractivePanels() {
		body := []string{
			fmt.Sprintf("Switch to %s?", r.remoteSwitchDescriptor(ctx, name)),
			"Current conversation binding will move to the selected runtime target.",
			"Your local working directory will remain unchanged.",
		}
		r.setPromptPanel(switchConfirmPanel(r, "Switch Remote", body, "/remote "+internalConfirmedArg+" "+name, nil))
		return false
	}
	if r.prompter == nil {
		return true
	}
	line, err := r.prompter.ReadLine(fmt.Sprintf("Switch to %s? [y/N] ", r.remoteSwitchDescriptor(ctx, name)), r.commands)
	if err != nil {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(line), "y") || strings.EqualFold(strings.TrimSpace(line), "yes")
}

func (r *REPL) remoteSwitchDescriptor(ctx context.Context, name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return "remote"
	}
	if r != nil && r.targetManager != nil {
		if targets, err := r.targetManager.ListTargets(ctx); err == nil {
			for _, target := range targets {
				if strings.EqualFold(strings.TrimSpace(target.Name), name) {
					return targetDescriptor(target.Name, target.Kind)
				}
			}
		}
	}
	return targetDescriptor(name, "")
}

func (r *REPL) confirmResetSession(_ context.Context) bool {
	if strings.TrimSpace(r.sessionKey) == "" {
		return false
	}
	if r.supportsInteractivePanels() {
		body := []string{
			fmt.Sprintf("Reset conversation %s?", r.sessionKey),
			"This clears the current conversation state and opens a fresh one under the same key.",
		}
		r.setPromptPanel(switchConfirmPanel(r, "Reset Conversation", body, "/reset "+internalConfirmedArg, nil))
		return false
	}
	if r.prompter == nil {
		return true
	}
	line, err := r.prompter.ReadLine(fmt.Sprintf("Reset conversation %s? [y/N] ", r.sessionKey), r.commands)
	if err != nil {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(line), "y") || strings.EqualFold(strings.TrimSpace(line), "yes")
}

func (r *REPL) confirmCompactSession(_ context.Context) bool {
	if strings.TrimSpace(r.sessionID) == "" {
		return false
	}
	if r.supportsInteractivePanels() {
		body := []string{
			"Compact conversation history?",
			"This keeps the conversation, but condenses older context into a shorter summary.",
		}
		r.setPromptPanel(switchConfirmPanel(r, "Compact Conversation", body, "/compact "+internalConfirmedArg, nil))
		return false
	}
	if r.prompter == nil {
		return true
	}
	line, err := r.prompter.ReadLine("Compact conversation history? [y/N] ", r.commands)
	if err != nil {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(line), "y") || strings.EqualFold(strings.TrimSpace(line), "yes")
}

func (r *REPL) ensureCurrentProject(ctx context.Context) (*agent.Project, error) {
	if r.currentProject != nil {
		return r.currentProject, nil
	}
	cwd, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	project, err := r.service.FindOrCreateProject(ctx, cwd)
	if err != nil {
		return nil, err
	}
	r.currentProject = project
	return project, nil
}

func (r *REPL) describeContext(ctx context.Context) (string, error) {
	if r.sessionID == "" && r.sessionKey == "" {
		return "", fmt.Errorf("no active conversation")
	}
	detail, _, err := r.currentServiceSession(ctx)
	if err != nil {
		return "", err
	}
	if detail == nil {
		detail = &SessionDetail{}
	}

	messages := make([]contextengine.Message, 0, len(detail.Messages))
	for _, item := range detail.Messages {
		role := contextengine.RoleAssistant
		switch strings.ToLower(strings.TrimSpace(item.Role)) {
		case "user":
			role = contextengine.RoleUser
		case "tool":
			role = contextengine.RoleTool
		}
		messages = append(messages, contextengine.NewTextMessage(role, item.Content))
	}

	estimate := contextengine.CharRatioEstimator{}.EstimateMessages(messages)
	window := 0
	model := r.effectiveModel()
	for _, item := range r.modelCache {
		if item.ID == model {
			window = item.ContextWindow
			break
		}
	}
	if window <= 0 {
		return fmt.Sprintf("Estimated context usage: ~%d tokens across %d messages", estimate, len(messages)), nil
	}
	percent := int(float64(estimate) / float64(window) * 100)
	recommendation := "context headroom looks healthy"
	switch {
	case percent >= 90:
		recommendation = "compact now"
	case percent >= 75:
		recommendation = "consider /compact soon"
	}
	return fmt.Sprintf("Estimated context usage: ~%d / %d tokens (%d%%) across %d messages; %s", estimate, window, percent, len(messages), recommendation), nil
}

func currentMarker(current bool) string {
	if current {
		return "  current"
	}
	return ""
}

func contextWindowLabel(window int) string {
	if window <= 0 {
		return "unknown"
	}
	return fmt.Sprintf("%dk", window/1000)
}

func contextHeadroom(percent int) string {
	switch {
	case percent >= 90:
		return "critical"
	case percent >= 75:
		return "tight"
	default:
		return "healthy"
	}
}

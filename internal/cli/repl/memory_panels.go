package repl

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/fulcrus/hopclaw/agent"
)

func (r *REPL) renderMemoryPanel(ctx context.Context, query string, limit int) error {
	items, err := r.service.ListMemory(ctx, query, limit)
	if err != nil {
		return err
	}
	if len(items) == 0 {
		r.renderer.SystemLine("No memory entries found.")
		return nil
	}
	panelItems := make([]panelItem, 0, len(items))
	for _, item := range items {
		panelItems = append(panelItems, memoryPanelItem(
			item.Key,
			memoryPanelLabel(item),
			memoryPreview(item.Value),
			memorySourceLabel(item),
			memoryScopeMarker(r, item),
		))
	}
	panel := newSelectionPanel(r, "Memory", "Search:", "Enter inspect memory  p pin memory  d delete memory  Esc back", panelItems)
	panel.query = strings.TrimSpace(query)
	panel.load = func(query string) ([]panelItem, error) {
		entries, err := r.service.ListMemory(ctx, query, limit)
		if err != nil {
			return nil, err
		}
		loaded := make([]panelItem, 0, len(entries))
		for _, entry := range entries {
			loaded = append(loaded, memoryPanelItem(
				entry.Key,
				memoryPanelLabel(entry),
				memoryPreview(entry.Value),
				memorySourceLabel(entry),
				memoryScopeMarker(r, entry),
			))
		}
		return loaded, nil
	}
	panel.onConfirm = func(item panelItem) (string, error) {
		return "/memory inspect " + internalInspectArg + " " + item.ID, nil
	}
	panel.hotkeys = map[rune]func(*selectionPanel, panelItem) (string, error){
		'd': func(panel *selectionPanel, item panelItem) (string, error) {
			if strings.TrimSpace(item.ID) == "" {
				return "", nil
			}
			r.setPromptPanel(switchConfirmPanel(r, "Delete Memory", []string{
				fmt.Sprintf("Delete memory %q?", item.ID),
				"This cannot be undone.",
			}, "/memory delete "+internalConfirmedArg+" "+item.ID, panel))
			return "", nil
		},
		'p': func(panel *selectionPanel, _ panelItem) (string, error) {
			text := strings.TrimSpace(panel.query)
			if text == "" {
				panel.status = "Type memory text in the query line, then press p."
				return "", nil
			}
			return "/memory pin " + text, nil
		},
	}
	if err := panel.refresh(); err != nil {
		return err
	}
	r.openPromptPanel(panel)
	return nil
}

func (r *REPL) inspectMemory(ctx context.Context, key string) error {
	entry, err := r.service.GetMemory(ctx, key)
	if err != nil {
		return err
	}
	if entry == nil {
		r.renderer.SystemLine("Memory entry not found.")
		return nil
	}
	lines := []string{
		"Key: " + defaultString(entry.Key, "-"),
		"Label: " + defaultString(entry.Label, "-"),
		"Kind: " + memoryKindLabel(*entry),
		"Saved by: " + memorySourceLabel(*entry),
		"Applies to: " + memoryAppliesToLabel(r, *entry),
		"State: " + memoryStateLabel(*entry),
		fmt.Sprintf("Updates: %d", entry.CorrectionCount),
		fmt.Sprintf("Supporting items: %d", entry.EvidenceCount),
		"Updated: " + entry.UpdatedAt.UTC().Format(time.RFC3339),
		"",
	}
	if len(entry.PreviousValues) > 0 {
		lines = append(lines, "Previous values")
		for index, value := range entry.PreviousValues[:min(len(entry.PreviousValues), 5)] {
			lines = append(lines, fmt.Sprintf("%d. %s", index+1, compact(strings.TrimSpace(value), 88)))
		}
		lines = append(lines, "")
	}
	for _, line := range splitMemoryValue(entry.Value) {
		lines = append(lines, line)
	}
	r.openInfoPanel("Memory Detail", lines, "/memory delete "+key+"  /memory  Esc back")
	return nil
}

func (r *REPL) pinMemory(ctx context.Context, text string) error {
	text = strings.TrimSpace(text)
	if text == "" {
		return fmt.Errorf("memory text is required")
	}
	projectID := ""
	if project, err := r.ensureCurrentProject(ctx); err == nil && project != nil {
		projectID = project.ID
	}
	entry, err := r.service.SaveMemory(ctx, "", text, "pinned", r.sessionKey, projectID)
	if err != nil {
		return err
	}
	if entry == nil {
		r.renderer.SystemLine("Pinned memory.")
		return nil
	}
	lines := []string{
		"Pinned memory.",
		"Key: " + defaultString(entry.Key, "(auto)"),
		"Label: " + defaultString(entry.Label, "pinned"),
		"Value: " + compact(memoryPreview(entry.Value), 88),
	}
	r.openInfoPanel("Memory Pinned", lines, "/memory  /memory inspect <key>  Esc back")
	return nil
}

func (r *REPL) deleteMemory(ctx context.Context, key string, confirmed bool) error {
	key = strings.TrimSpace(key)
	if key == "" {
		return fmt.Errorf("memory key is required")
	}
	if !confirmed {
		approved, err := r.confirmMemoryDeletion(key)
		if err != nil {
			return err
		}
		if !approved {
			if r.panelController != nil {
				return nil
			}
			r.renderer.SystemLine("Memory deletion cancelled.")
			return nil
		}
	}
	if err := r.service.DeleteMemory(ctx, key); err != nil {
		return err
	}
	r.clearPanel()
	r.renderer.SystemLine("Deleted memory " + key + ".")
	return nil
}

func (r *REPL) confirmMemoryDeletion(key string) (bool, error) {
	if r.supportsInteractivePanels() {
		title := "Delete Memory?"
		body := []string{
			fmt.Sprintf("Delete memory %q?", key),
			"This cannot be undone.",
		}
		r.setPromptPanel(switchConfirmPanel(r, title, body, "/memory delete "+internalConfirmedArg+" "+key, nil))
		return false, nil
	}
	if r.prompter == nil {
		return true, nil
	}
	line, err := r.prompter.ReadLine(fmt.Sprintf("Delete memory %q? [y/N] ", key), r.commands)
	if err != nil {
		if err == ErrPromptInterrupted {
			return false, nil
		}
		return false, err
	}
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "y", "yes":
		return true, nil
	default:
		return false, nil
	}
}

func (r *REPL) renderMemoryConflicts(ctx context.Context) error {
	entries, err := r.service.ListMemoryConflicts(ctx)
	if err != nil {
		return err
	}
	if len(entries) == 0 {
		r.openInfoPanel("Memory Conflicts", []string{"No memory conflicts detected."}, "/memory  Esc back")
		return nil
	}
	lines := make([]string, 0, len(entries)*3)
	for _, entry := range entries {
		lines = append(lines,
			fmt.Sprintf("Key: %s  (source: %s)", entry.Key, defaultString(entry.Source, "-")),
			fmt.Sprintf("  Current value: %s", compact(entry.Value, 60)),
			fmt.Sprintf("  Conflicts with: %s (source: %s)", defaultString(entry.ConflictWith, "-"), defaultString(entry.ConflictSource, "-")),
			"",
		)
	}
	r.openInfoPanel("Memory Conflicts", lines, "/memory  Esc back")
	return nil
}

func (r *REPL) renderMemoryPending(ctx context.Context) error {
	entries, err := r.service.ListPendingMemoryWrites(ctx)
	if err != nil {
		return err
	}
	if len(entries) == 0 {
		r.openInfoPanel("Pending Memory Writes", []string{"No pending memory writes."}, "/memory  Esc back")
		return nil
	}
	lines := make([]string, 0, len(entries)*3)
	for _, entry := range entries {
		lines = append(lines,
			fmt.Sprintf("Key: %s", entry.Key),
			fmt.Sprintf("  Source: %s", defaultString(entry.PendingWriteSource, "-")),
			fmt.Sprintf("  Value: %s", compact(defaultString(entry.PendingWriteValue, "-"), 60)),
			"",
		)
	}
	r.openInfoPanel("Pending Memory Writes", lines, "/memory pending  /memory  Esc back")
	return nil
}

func memoryPreview(value string) string {
	value = strings.TrimSpace(strings.ReplaceAll(value, "\n", " "))
	if value == "" {
		return "-"
	}
	return value
}

func memoryScopeMarker(r *REPL, item agent.MemoryEntry) string {
	scope := memoryScopeLabel(item)
	if r == nil {
		return scope
	}
	marker := ""
	switch {
	case strings.TrimSpace(item.SessionKey) != "" && strings.TrimSpace(item.SessionKey) == strings.TrimSpace(r.sessionKey):
		marker = "current"
	case r.currentProject != nil && strings.TrimSpace(item.ProjectID) != "" && strings.TrimSpace(item.ProjectID) == strings.TrimSpace(r.currentProject.ID):
		marker = "current"
	}
	return joinNonEmpty(" · ", scope, marker)
}

func memoryStateLabel(item agent.MemoryEntry) string {
	if state := strings.TrimSpace(string(item.State)); state != "" {
		return state
	}
	return string(agent.MemoryActive)
}

func memoryScopeLabel(item agent.MemoryEntry) string {
	switch {
	case strings.TrimSpace(item.SessionKey) != "":
		return "conversation"
	case strings.TrimSpace(item.ProjectID) != "":
		return "project"
	case strings.TrimSpace(item.ScopeKey) != "":
		return "task"
	default:
		namespace := strings.ToLower(strings.TrimSpace(item.Namespace))
		if strings.Contains(namespace, "task") {
			return "task"
		}
		return "saved"
	}
}

func splitMemoryValue(value string) []string {
	value = strings.TrimSpace(value)
	if value == "" {
		return []string{"Value: -"}
	}
	lines := strings.Split(value, "\n")
	out := make([]string, 0, min(len(lines), 5))
	for index, line := range lines {
		if index >= 5 {
			out = append(out, "…")
			break
		}
		if index == 0 {
			out = append(out, "Value: "+line)
			continue
		}
		out = append(out, "  "+line)
	}
	return out
}

func memoryStripSummary(items []MemoryUsageItem) string {
	if len(items) == 0 {
		return ""
	}
	pinned := 0
	conversation := 0
	project := 0
	task := 0
	recalled := 0
	for _, item := range items {
		switch strings.ToLower(strings.TrimSpace(item.Scope)) {
		case "conversation":
			conversation++
		case "project":
			project++
		case "task":
			task++
		}
		switch strings.ToLower(strings.TrimSpace(item.Reason)) {
		case "pinned":
			pinned++
		case "recalled":
			recalled++
		}
	}
	parts := make([]string, 0, 5)
	if pinned > 0 {
		parts = append(parts, fmt.Sprintf("pinned %d", pinned))
	}
	if conversation > 0 {
		parts = append(parts, fmt.Sprintf("conversation %d", conversation))
	}
	if project > 0 {
		parts = append(parts, fmt.Sprintf("project %d", project))
	}
	if task > 0 {
		parts = append(parts, fmt.Sprintf("task %d", task))
	}
	if recalled > 0 {
		parts = append(parts, fmt.Sprintf("recalled %d", recalled))
	}
	if len(parts) == 0 {
		return ""
	}
	return "using: " + strings.Join(parts, " · ")
}

func memoryPanelLabel(item agent.MemoryEntry) string {
	if label := strings.TrimSpace(item.Label); label != "" {
		return label
	}
	if source := strings.TrimSpace(memorySourceLabel(item)); source != "" {
		return source
	}
	return memoryKindLabel(item)
}

func memorySourceLabel(item agent.MemoryEntry) string {
	switch strings.ToLower(strings.TrimSpace(item.Source)) {
	case "user":
		return "you"
	case "agent", "assistant":
		return "assistant"
	default:
		return strings.TrimSpace(item.Source)
	}
}

func memoryKindLabel(item agent.MemoryEntry) string {
	switch memoryScopeLabel(item) {
	case "conversation":
		return "conversation memory"
	case "project":
		return "project memory"
	case "task":
		return "task memory"
	default:
		if strings.EqualFold(strings.TrimSpace(item.Source), "user") {
			return "pinned memory"
		}
		return "saved memory"
	}
}

func memoryAppliesToLabel(r *REPL, item agent.MemoryEntry) string {
	switch memoryScopeLabel(item) {
	case "conversation":
		if r != nil && strings.TrimSpace(item.SessionKey) != "" && strings.TrimSpace(item.SessionKey) == strings.TrimSpace(r.sessionKey) {
			return "current conversation"
		}
		return "saved conversation context"
	case "project":
		if r != nil && r.currentProject != nil && strings.TrimSpace(item.ProjectID) != "" && strings.TrimSpace(item.ProjectID) == strings.TrimSpace(r.currentProject.ID) {
			return "current project"
		}
		return "saved project context"
	case "task":
		return "current task"
	default:
		return "future conversations"
	}
}

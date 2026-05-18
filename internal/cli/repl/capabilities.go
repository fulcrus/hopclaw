package repl

import (
	"context"
	"fmt"
	"sort"
	"strings"
)

func (r *REPL) renderToolsPanel(ctx context.Context, query string) error {
	items, err := r.loadToolSummaries(ctx)
	if err != nil {
		return err
	}
	if len(items) == 0 {
		r.openInfoPanel("Tools", []string{
			"No tools available for the current runtime.",
			"Tool visibility follows the active target and session context.",
		}, "/tools check  Esc back")
		return nil
	}

	panel := newSelectionPanel(r, "Tools", "Search:", "Enter inspect  c check  Esc back", buildToolPanelItems(items))
	panel.query = strings.TrimSpace(query)
	panel.load = func(query string) ([]panelItem, error) {
		loaded, err := r.loadToolSummaries(ctx)
		if err != nil {
			return nil, err
		}
		return filterToolPanelItems(loaded, query), nil
	}
	panel.onConfirm = func(item panelItem) (string, error) {
		return "/tools info " + internalInspectArg + " " + item.ID, nil
	}
	panel.hotkeys = map[rune]func(*selectionPanel, panelItem) (string, error){
		'c': func(_ *selectionPanel, item panelItem) (string, error) {
			return "/tools check " + item.ID, nil
		},
	}
	if err := panel.refresh(); err != nil {
		return err
	}
	r.openPromptPanel(panel)
	return nil
}

func (r *REPL) renderToolDetail(ctx context.Context, name string) error {
	items, err := r.loadToolSummaries(ctx)
	if err != nil {
		return err
	}
	for _, item := range items {
		if !strings.EqualFold(strings.TrimSpace(item.Name), strings.TrimSpace(name)) {
			continue
		}
		lines := []string{
			"Name: " + defaultString(item.Name, "-"),
			"Description: " + defaultString(item.Description, "-"),
			"Source: " + defaultString(item.Source, "-"),
			"Side effects: " + defaultString(item.SideEffectClass, "-"),
			fmt.Sprintf("Requires approval: %t", item.RequiresApproval),
			fmt.Sprintf("Eligible: %t", item.Eligible),
			"Input schema: " + schemaKeySummary(item.InputSchema),
			"Output schema: " + schemaKeySummary(item.OutputSchema),
		}
		r.openInfoPanel("Tool Detail", lines, "/tools  /tools check "+item.Name+"  Esc back")
		return nil
	}
	return fmt.Errorf("tool %q not found", strings.TrimSpace(name))
}

func (r *REPL) renderToolCheck(ctx context.Context, name string) error {
	items, err := r.loadToolSummaries(ctx)
	if err != nil {
		return err
	}
	name = strings.TrimSpace(name)
	if name != "" {
		for _, item := range items {
			if !strings.EqualFold(strings.TrimSpace(item.Name), name) {
				continue
			}
			status := "[ok]"
			if !item.Eligible {
				status = "[--]"
			}
			lines := []string{
				status + " " + defaultString(item.Name, name),
				"Source: " + defaultString(item.Source, "-"),
				"Side effects: " + defaultString(item.SideEffectClass, "-"),
			}
			if item.Eligible {
				lines = append(lines, "Status: eligible in the current runtime context.")
			} else {
				lines = append(lines, "Status: unavailable in the current runtime context.")
			}
			r.openInfoPanel("Tool Check", lines, "/tools info "+item.Name+"  /tools  Esc back")
			return nil
		}
		return fmt.Errorf("tool %q not found", name)
	}

	total := len(items)
	eligible := 0
	requiresApproval := 0
	ineligibleNames := make([]string, 0, len(items))
	for _, item := range items {
		if item.Eligible {
			eligible++
		} else {
			ineligibleNames = append(ineligibleNames, item.Name)
		}
		if item.RequiresApproval {
			requiresApproval++
		}
	}
	lines := []string{
		fmt.Sprintf("Eligible tools: %d / %d", eligible, total),
		fmt.Sprintf("Requires approval: %d", requiresApproval),
	}
	if len(ineligibleNames) > 0 {
		lines = append(lines, "Unavailable: "+strings.Join(ineligibleNames[:min(len(ineligibleNames), 4)], ", "))
	}
	r.openInfoPanel("Tool Check", lines, "/tools  Esc back")
	return nil
}

func (r *REPL) loadToolSummaries(ctx context.Context) ([]ToolSummary, error) {
	sessionKey := strings.TrimSpace(r.sessionKey)
	items, err := r.service.ListTools(ctx, sessionKey)
	if err != nil {
		return nil, err
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].Eligible != items[j].Eligible {
			return items[i].Eligible
		}
		return strings.Compare(items[i].Name, items[j].Name) < 0
	})
	return items, nil
}

func buildToolPanelItems(items []ToolSummary) []panelItem {
	out := make([]panelItem, 0, len(items))
	for _, item := range items {
		status := "[ok]"
		if !item.Eligible {
			status = "[--]"
		}
		out = append(out, panelItem{
			ID: item.Name,
			Text: fmt.Sprintf("%-4s %-24s %-14s %s",
				status,
				compact(defaultString(item.Name, "-"), 24),
				compact(defaultString(item.SideEffectClass, "-"), 14),
				compact(defaultString(item.Description, "-"), 34),
			),
			SearchText: strings.Join([]string{
				item.Name,
				item.Description,
				item.Source,
				item.SideEffectClass,
			}, " "),
		})
	}
	return out
}

func filterToolPanelItems(items []ToolSummary, query string) []panelItem {
	return matchPanelItems(buildToolPanelItems(items), query)
}

func (r *REPL) renderSkillsPanel(ctx context.Context) error {
	items, err := r.service.ListSkills(ctx)
	if err != nil {
		return err
	}
	if len(items) == 0 {
		r.openInfoPanel("Skills", []string{
			"No installed skills found.",
			"Use /skills search <query> to browse the skill catalog.",
		}, "/skills search <query>  /skills install <name-or-path>  Esc back")
		return nil
	}

	panel := newSelectionPanel(r, "Skills", "Search:", "Enter inspect  d remove  s search catalog  Esc back", buildSkillPanelItems(items))
	panel.load = func(query string) ([]panelItem, error) {
		loaded, err := r.service.ListSkills(ctx)
		if err != nil {
			return nil, err
		}
		return matchPanelItems(buildSkillPanelItems(loaded), query), nil
	}
	panel.onConfirm = func(item panelItem) (string, error) {
		return "/skills info " + internalInspectArg + " " + item.ID, nil
	}
	panel.hotkeys = map[rune]func(*selectionPanel, panelItem) (string, error){
		'd': func(panel *selectionPanel, item panelItem) (string, error) {
			name := strings.TrimSpace(item.ID)
			if name == "" {
				return "", nil
			}
			r.setPromptPanel(switchConfirmPanel(r, "Remove Skill", []string{
				fmt.Sprintf("Remove skill %q?", name),
				"This removes the installed skill from the current runtime.",
			}, "/skills remove "+internalConfirmedArg+" "+name, panel))
			return "", nil
		},
		's': func(panel *selectionPanel, item panelItem) (string, error) {
			query := strings.TrimSpace(panel.query)
			if query == "" {
				query = strings.TrimSpace(item.ID)
			}
			if query == "" {
				panel.status = "Type a catalog query, then press s."
				return "", nil
			}
			return "/skills search " + query, nil
		},
	}
	r.openPromptPanel(panel)
	return nil
}

func (r *REPL) renderSkillCatalogPanel(ctx context.Context, query string) error {
	query = strings.TrimSpace(query)
	if query == "" {
		return fmt.Errorf("skill search query is required")
	}
	items, err := r.service.SearchSkillCatalog(ctx, query)
	if err != nil {
		return err
	}
	if len(items) == 0 {
		r.renderer.SystemLine(fmt.Sprintf("No skills matching %q.", query))
		return nil
	}

	panel := newSelectionPanel(r, "Skill Catalog", "Search:", "Enter inspect  i install  Esc back", buildSkillCatalogPanelItems(items))
	panel.query = query
	panel.load = func(query string) ([]panelItem, error) {
		if strings.TrimSpace(query) == "" {
			return nil, nil
		}
		loaded, err := r.service.SearchSkillCatalog(ctx, query)
		if err != nil {
			return nil, err
		}
		return buildSkillCatalogPanelItems(loaded), nil
	}
	panel.onConfirm = func(item panelItem) (string, error) {
		return "/skills info " + internalInspectArg + " " + item.ID, nil
	}
	panel.hotkeys = map[rune]func(*selectionPanel, panelItem) (string, error){
		'i': func(_ *selectionPanel, item panelItem) (string, error) {
			return "/skills install " + item.ID, nil
		},
	}
	if err := panel.refresh(); err != nil {
		return err
	}
	r.openPromptPanel(panel)
	return nil
}

func (r *REPL) renderSkillDetail(ctx context.Context, name string) error {
	detail, err := r.service.GetSkill(ctx, strings.TrimSpace(name))
	if err != nil {
		return err
	}
	if detail == nil || (detail.Installed == nil && detail.Catalog == nil) {
		return fmt.Errorf("skill %q not found", strings.TrimSpace(name))
	}

	lines := make([]string, 0, 18)
	actions := []string{"/skills"}
	skillRef := strings.TrimSpace(name)
	if detail.Installed != nil {
		item := detail.Installed
		skillRef = firstNonEmpty(strings.TrimSpace(item.ID), strings.TrimSpace(item.Name), skillRef)
		lines = append(lines,
			"Installed",
			"Name: "+skillDisplayName(item.Name, item.ID),
			"ID: "+defaultString(item.ID, "-"),
			"Version: "+defaultString(item.Version, "-"),
			"Status: "+defaultString(item.Status, "-"),
			"Trust: "+defaultString(item.Trust, "-"),
			"Location: "+defaultString(skillLocation(*item), "-"),
			fmt.Sprintf("Pinned: %t", item.Pinned),
			fmt.Sprintf("Ready: %t", item.Ready),
			fmt.Sprintf("Eligible: %t", item.Eligible),
		)
		if text := strings.TrimSpace(firstNonEmpty(item.Description, item.Summary)); text != "" {
			lines = append(lines, "Summary: "+text)
		}
		lines = append(lines, "")
		actions = append(actions, "/skills remove "+skillRef)
	}
	if detail.Catalog != nil {
		item := detail.Catalog
		skillRef = firstNonEmpty(strings.TrimSpace(item.ID), strings.TrimSpace(item.Name), skillRef)
		lines = append(lines,
			"Catalog",
			"Name: "+skillDisplayName(item.Name, item.ID),
			"ID: "+defaultString(item.ID, "-"),
			"Version: "+defaultString(item.Version, "-"),
			"Source: "+defaultString(item.SourceKind, "-"),
			fmt.Sprintf("Installed: %t", item.Installed),
			fmt.Sprintf("Ready: %t", item.Ready),
			fmt.Sprintf("Eligible: %t", item.Eligible),
		)
		if text := strings.TrimSpace(firstNonEmpty(item.Description, item.Summary)); text != "" {
			lines = append(lines, "Summary: "+text)
		}
		lines = append(lines, "")
		actions = append(actions, "/skills install "+skillRef, "/skills search "+skillRef)
	}
	r.openInfoPanel("Skill Detail", trimTrailingBlankLines(lines), strings.Join(uniqueActionList(actions), "  ")+"  Esc back")
	return nil
}

func (r *REPL) installSkill(ctx context.Context, raw string, version string) error {
	result, err := r.service.InstallSkill(ctx, strings.TrimSpace(raw), strings.TrimSpace(version))
	if err != nil {
		return err
	}
	if result == nil {
		r.renderer.SystemLine("Skill install is unavailable for the current target.")
		return nil
	}
	lines := []string{
		"Installed skill " + defaultString(result.SkillID, strings.TrimSpace(raw)) + ".",
		"Version: " + defaultString(result.Version, "-"),
		"Install dir: " + defaultString(result.InstallDir, "-"),
		fmt.Sprintf("Installer steps: %d", result.InstallerStepCount),
	}
	if strings.TrimSpace(result.LockFile) != "" {
		lines = append(lines, "Lock file: "+result.LockFile)
	}
	ref := firstNonEmpty(strings.TrimSpace(result.SkillID), strings.TrimSpace(raw))
	r.openInfoPanel("Skill Installed", lines, "/skills info "+ref+"  /skills  Esc back")
	return nil
}

func (r *REPL) removeSkill(ctx context.Context, name string, confirmed bool) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("skill name is required")
	}
	if !confirmed {
		approved, err := r.confirmSkillRemoval(name)
		if err != nil {
			return err
		}
		if !approved {
			if r.panelController != nil {
				return nil
			}
			r.renderer.SystemLine("Skill removal cancelled.")
			return nil
		}
	}
	if err := r.service.RemoveSkill(ctx, name); err != nil {
		return err
	}
	r.clearPanel()
	r.renderer.SystemLine("Removed skill " + name + ".")
	return nil
}

func (r *REPL) confirmSkillRemoval(name string) (bool, error) {
	if r.supportsInteractivePanels() {
		r.setPromptPanel(switchConfirmPanel(r, "Remove Skill", []string{
			fmt.Sprintf("Remove skill %q?", name),
			"This removes the installed skill from the current runtime.",
		}, "/skills remove "+internalConfirmedArg+" "+name, nil))
		return false, nil
	}
	if r.prompter == nil {
		return true, nil
	}
	line, err := r.prompter.ReadLine(fmt.Sprintf("Remove skill %q? [y/N] ", name), r.commands)
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

func buildSkillPanelItems(items []SkillSummary) []panelItem {
	out := make([]panelItem, 0, len(items))
	for _, item := range items {
		ref := firstNonEmpty(strings.TrimSpace(item.ID), strings.TrimSpace(item.Name))
		out = append(out, panelItem{
			ID: ref,
			Text: fmt.Sprintf("%-20s %-10s %-10s %-10s %s",
				compact(skillDisplayName(item.Name, item.ID), 20),
				compact(defaultString(item.Version, "-"), 10),
				compact(defaultString(item.Status, "-"), 10),
				compact(defaultString(item.Trust, "-"), 10),
				compact(defaultString(skillLocation(item), firstNonEmpty(item.Description, item.Summary, "-")), 34),
			),
			SearchText: strings.Join([]string{
				item.ID,
				item.Name,
				item.Status,
				item.Trust,
				item.Summary,
				item.Description,
				item.InstallDir,
				item.BundleDir,
			}, " "),
		})
	}
	return out
}

func buildSkillCatalogPanelItems(items []SkillCatalogSummary) []panelItem {
	out := make([]panelItem, 0, len(items))
	for _, item := range items {
		ref := firstNonEmpty(strings.TrimSpace(item.ID), strings.TrimSpace(item.Name))
		installed := "catalog"
		if item.Installed {
			installed = "installed"
		}
		out = append(out, panelItem{
			ID: ref,
			Text: fmt.Sprintf("%-20s %-10s %-10s %s",
				compact(skillDisplayName(item.Name, item.ID), 20),
				compact(defaultString(item.Version, "-"), 10),
				installed,
				compact(defaultString(firstNonEmpty(item.Description, item.Summary), "-"), 36),
			),
			SearchText: strings.Join([]string{
				item.ID,
				item.Name,
				item.Version,
				item.Description,
				item.Summary,
				item.SourceKind,
			}, " "),
		})
	}
	return out
}

func skillDisplayName(name, id string) string {
	return defaultString(strings.TrimSpace(name), defaultString(strings.TrimSpace(id), "-"))
}

func skillLocation(item SkillSummary) string {
	return firstNonEmpty(strings.TrimSpace(item.InstallDir), strings.TrimSpace(item.BundleDir))
}

func schemaKeySummary(schema map[string]any) string {
	keys := schemaKeys(schema)
	if len(keys) == 0 {
		return "-"
	}
	if len(keys) > 6 {
		return strings.Join(keys[:6], ", ") + ", ..."
	}
	return strings.Join(keys, ", ")
}

func schemaKeys(schema map[string]any) []string {
	if len(schema) == 0 {
		return nil
	}
	if props, ok := schema["properties"].(map[string]any); ok {
		keys := make([]string, 0, len(props))
		for key := range props {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		return keys
	}
	keys := make([]string, 0, len(schema))
	for key := range schema {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func trimTrailingBlankLines(lines []string) []string {
	end := len(lines)
	for end > 0 && strings.TrimSpace(lines[end-1]) == "" {
		end--
	}
	return append([]string(nil), lines[:end]...)
}

func uniqueActionList(items []string) []string {
	out := make([]string, 0, len(items))
	seen := map[string]struct{}{}
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		out = append(out, item)
	}
	return out
}

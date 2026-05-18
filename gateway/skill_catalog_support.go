package gateway

import (
	"context"
	"os"
	"sort"
	"strings"

	"github.com/fulcrus/hopclaw/internal/support/normalize"
	"github.com/fulcrus/hopclaw/resultmodel"
	"github.com/fulcrus/hopclaw/skill"
)

func gatewaySkillReportPayload(report skill.SkillRuntimeReport) map[string]any {
	payload := map[string]any{
		"found":    report.Found,
		"loaded":   report.Loaded,
		"blocked":  report.Blocked,
		"eligible": report.Eligible,
		"ready":    report.Ready,
	}
	if report.Installed {
		payload["installed"] = true
	}
	if report.Name != "" {
		payload["name"] = report.Name
	}
	if report.SkillID != "" {
		payload["skill_id"] = report.SkillID
	}
	if report.ConfigKey != "" {
		payload["config_key"] = report.ConfigKey
	}
	if report.Description != "" {
		payload["description"] = report.Description
	}
	if report.Homepage != "" {
		payload["homepage"] = report.Homepage
	}
	if report.Location != "" {
		payload["location"] = report.Location
	}
	if report.Kind != "" {
		payload["kind"] = string(report.Kind)
	}
	if report.Status != "" {
		payload["status"] = string(report.Status)
	}
	if report.Trust != "" {
		payload["trust"] = string(report.Trust)
	}
	if report.SourceKind != "" {
		payload["source_kind"] = string(report.SourceKind)
	}
	if report.SourceRoot != "" {
		payload["source_root"] = report.SourceRoot
	}
	if report.SourceDir != "" {
		payload["source_dir"] = report.SourceDir
	}
	if report.SourceNameHint != "" {
		payload["source_name_hint"] = report.SourceNameHint
	}
	if report.SourcePriority != 0 {
		payload["source_priority"] = report.SourcePriority
	}
	if report.InstalledVersion != "" {
		payload["installed_version"] = report.InstalledVersion
	}
	if report.InstallDir != "" {
		payload["install_dir"] = report.InstallDir
	}
	if report.BundleDir != "" {
		payload["bundle_dir"] = report.BundleDir
	}
	if report.Pinned {
		payload["pinned"] = report.Pinned
	}
	if report.Always {
		payload["always"] = report.Always
	}
	if len(report.Reasons) > 0 {
		payload["reasons"] = append([]string(nil), report.Reasons...)
	}
	if len(report.Checks) > 0 {
		payload["checks"] = report.Checks
	}
	if len(report.InjectedEnv) > 0 {
		payload["injected_env"] = append([]string(nil), report.InjectedEnv...)
	}
	if len(report.Tools) > 0 {
		payload["tools"] = report.Tools
	}
	if len(report.InstallHints) > 0 {
		payload["install_hints"] = report.InstallHints
	}
	if len(report.Issues) > 0 {
		payload["issues"] = report.Issues
	}
	if len(report.NextActions) > 0 {
		payload["next_actions"] = append([]string(nil), report.NextActions...)
	}
	summary, blocks, actions := gatewaySkillReportPresentation(report)
	if installability := buildSkillInstallability(report); installability != nil {
		payload["installability"] = installability
	}
	if risk := buildSkillRiskProjection(report); risk != nil {
		payload["risk"] = risk
	}
	if tools := skillToolNames(report.Tools); len(tools) > 0 {
		payload["tool_names"] = tools
		payload["tool_count"] = len(tools)
	}
	if strings.TrimSpace(summary) != "" {
		payload["summary"] = summary
	}
	if len(blocks) > 0 {
		payload["blocks"] = blocks
	}
	if len(actions) > 0 {
		payload["actions"] = actions
	}
	return payload
}

func gatewaySkillReportPresentation(report skill.SkillRuntimeReport) (string, []resultmodel.ResultBlock, []resultmodel.ResultAction) {
	summaryParts := make([]string, 0, 3)
	if text := strings.TrimSpace(report.Description); text != "" {
		summaryParts = append(summaryParts, text)
	}
	if report.Status != "" {
		summaryParts = append(summaryParts, "status: "+string(report.Status))
	}
	if report.Trust != "" {
		summaryParts = append(summaryParts, "trust: "+string(report.Trust))
	}

	blocks := make([]resultmodel.ResultBlock, 0, 1+len(report.Reasons)+len(report.Checks)+len(report.Tools)+len(report.Issues)+len(report.InstallHints))
	overview := map[string]any{
		"ready":      report.Ready,
		"eligible":   report.Eligible,
		"kind":       report.Kind,
		"status":     report.Status,
		"trust":      report.Trust,
		"source":     report.SourceKind,
		"installed":  report.Installed,
		"pinned":     report.Pinned,
		"config_key": report.ConfigKey,
	}
	if report.Location != "" || report.InstallDir != "" || report.BundleDir != "" {
		overview["paths"] = map[string]any{
			"location":    report.Location,
			"install_dir": report.InstallDir,
			"bundle_dir":  report.BundleDir,
			"source_dir":  report.SourceDir,
		}
	}
	blocks = append(blocks, resultmodel.ResultBlock{
		Kind:    "json",
		Title:   "Overview",
		Content: strings.Join(summaryParts, " · "),
		Data:    overview,
	})
	for _, reason := range report.Reasons {
		if strings.TrimSpace(reason) == "" {
			continue
		}
		blocks = append(blocks, resultmodel.ResultBlock{
			Kind:    "warning",
			Title:   "Reason",
			Content: reason,
		})
	}
	for _, check := range report.Checks {
		title := strings.TrimSpace(check.Name)
		if title == "" {
			title = string(check.Kind)
		}
		blocks = append(blocks, resultmodel.ResultBlock{
			Kind:    "json",
			Title:   "Check · " + title,
			Content: strings.TrimSpace(check.Message),
			Data:    check,
		})
	}
	for _, tool := range report.Tools {
		blocks = append(blocks, resultmodel.ResultBlock{
			Kind:    "json",
			Title:   "Tool · " + tool.Name,
			Content: strings.TrimSpace(tool.Description),
			Data:    tool,
		})
	}
	for _, issue := range report.Issues {
		title := strings.TrimSpace(issue.Code)
		if title == "" {
			title = strings.TrimSpace(string(issue.Severity))
		}
		if title == "" {
			title = "issue"
		}
		blocks = append(blocks, resultmodel.ResultBlock{
			Kind:    "warning",
			Title:   "Issue · " + title,
			Content: strings.TrimSpace(issue.Message),
			Data:    issue,
		})
	}
	for _, hint := range report.InstallHints {
		title := strings.TrimSpace(hint.Label)
		if title == "" {
			title = strings.TrimSpace(hint.Kind)
		}
		if title == "" {
			title = "install"
		}
		blocks = append(blocks, resultmodel.ResultBlock{
			Kind:  "json",
			Title: "Install Hint · " + title,
			Data:  hint,
		})
	}

	actions := make([]resultmodel.ResultAction, 0, len(report.NextActions)+1)
	if safeHomepage := strings.TrimSpace(report.Homepage); safeHomepage != "" {
		actions = append(actions, resultmodel.ResultAction{
			Kind:   "open_url",
			Label:  "Open homepage",
			Target: safeHomepage,
		})
	}
	for _, action := range report.NextActions {
		if strings.TrimSpace(action) == "" {
			continue
		}
		actions = append(actions, resultmodel.ResultAction{
			Kind:   "followup",
			Label:  "Next step",
			Target: "",
			Params: map[string]any{"instruction": action},
		})
	}
	return strings.Join(summaryParts, " · "), blocks, actions
}

func (g *Gateway) findCatalogEntry(ctx context.Context, ref string) (skill.RegistrySkill, bool, error) {
	if g.skillHub == nil {
		return skill.RegistrySkill{}, false, nil
	}
	results, err := g.skillHub.Search(ctx, strings.TrimSpace(ref))
	if err != nil {
		return skill.RegistrySkill{}, false, err
	}
	needle := strings.ToLower(strings.TrimSpace(ref))
	for _, entry := range results {
		if strings.EqualFold(strings.TrimSpace(entry.ID), needle) || strings.EqualFold(strings.TrimSpace(entry.Name), needle) {
			return entry, true, nil
		}
	}
	for _, entry := range results {
		if strings.EqualFold(strings.TrimSpace(entry.ID), strings.TrimSpace(ref)) || strings.EqualFold(strings.TrimSpace(entry.Name), strings.TrimSpace(ref)) {
			return entry, true, nil
		}
	}
	return skill.RegistrySkill{}, false, nil
}

func (g *Gateway) buildCatalogSkillItem(ctx context.Context, entry skill.RegistrySkill, installed map[string]bool) skillCatalogItem {
	item := skillCatalogItem{
		ID:          entry.ID,
		Name:        entry.Name,
		Version:     entry.Version,
		Summary:     entry.Summary,
		Description: entry.Summary,
		Installed:   installed[entry.ID],
	}
	if report, ok := g.catalogSkillReport(ctx, entry); ok {
		item.Ready = report.Ready
		item.Eligible = report.Eligible
		item.SourceKind = string(report.SourceKind)
		item.DetailAvailable = true
		item.Description = normalize.FirstNonEmpty(report.Description, entry.Summary)
		item.Tools = skillToolNames(report.Tools)
		item.ToolCount = len(item.Tools)
		item.Installability = buildSkillInstallability(report)
		item.Risk = buildSkillRiskProjection(report)
		if text := strings.TrimSpace(report.Description); text != "" {
			item.Summary = text
		}
		return item
	}
	item.DetailAvailable = strings.TrimSpace(entry.BundleDir) != ""
	item.Installability = &skillInstallability{Score: 48, Label: "catalog_only"}
	item.Risk = &skillRiskProjection{Level: "medium", Tags: []string{"review_before_install"}}
	return item
}

func (g *Gateway) buildCatalogSkillDetailPayload(ctx context.Context, entry skill.RegistrySkill) map[string]any {
	payload := map[string]any{
		"id":          entry.ID,
		"skill_id":    entry.ID,
		"name":        normalize.FirstNonEmpty(entry.Name, entry.ID),
		"summary":     entry.Summary,
		"description": entry.Summary,
		"installed":   g.installedSkillSet()[entry.ID],
		"catalog":     true,
	}
	if report, ok := g.catalogSkillReport(ctx, entry); ok {
		reportPayload := gatewaySkillReportPayload(report)
		for key, value := range reportPayload {
			payload[key] = value
		}
		payload["catalog"] = true
		payload["detail_available"] = true
		return payload
	}
	payload["detail_available"] = strings.TrimSpace(entry.BundleDir) != ""
	payload["ready"] = false
	payload["eligible"] = false
	payload["source_kind"] = string(skill.SourceClawHub)
	payload["installability"] = &skillInstallability{Score: 48, Label: "catalog_only"}
	payload["risk"] = &skillRiskProjection{Level: "medium", Tags: []string{"review_before_install"}}
	payload["blocks"] = []resultmodel.ResultBlock{
		{
			Kind:    "warning",
			Title:   "Catalog preview only",
			Content: "This catalog entry has not been locally inspected yet. Install it or sync a local bundle to get full dependency, tool, and risk analysis.",
			Data: map[string]any{
				"bundle_dir": entry.BundleDir,
				"bundle_url": entry.BundleURL,
			},
		},
	}
	payload["actions"] = []resultmodel.ResultAction{
		{
			Kind:   "followup",
			Label:  "Install skill",
			Params: map[string]any{"skill_id": entry.ID},
		},
	}
	return payload
}

func (g *Gateway) catalogSkillReport(ctx context.Context, entry skill.RegistrySkill) (skill.SkillRuntimeReport, bool) {
	if report, ok := g.inspectInstalledSkill(entry.ID); ok {
		return report, true
	}
	source := strings.TrimSpace(entry.BundleDir)
	if source == "" {
		return skill.SkillRuntimeReport{}, false
	}
	if _, err := os.Stat(source); err != nil {
		return skill.SkillRuntimeReport{}, false
	}
	report, err := g.inspectSkillSource(ctx, source, string(skill.SourceClawHub))
	if err != nil {
		return skill.SkillRuntimeReport{}, false
	}
	return report, true
}

func buildSkillInstallability(report skill.SkillRuntimeReport) *skillInstallability {
	totalChecks := len(report.Checks)
	missing := 0
	warnings := 0
	for _, check := range report.Checks {
		switch check.Status {
		case skill.DependencyStatusMissing, skill.DependencyStatusUnsupported, skill.DependencyStatusDisabled:
			missing++
			warnings++
		}
	}
	errorIssues := 0
	warnIssues := 0
	for _, issue := range report.Issues {
		switch issue.Severity {
		case skill.SeverityError:
			errorIssues++
		case skill.SeverityWarning:
			warnIssues++
		}
	}
	score := 48
	label := "catalog_only"
	switch {
	case report.Blocked || report.Status == skill.StatusBlocked:
		score = 18
		label = "blocked"
	case report.Ready:
		score = 96
		label = "ready"
	case report.Eligible:
		score = 72
		label = "needs_setup"
	case report.Found:
		score = 42
		label = "review_required"
	}
	score -= missing * 12
	score -= errorIssues * 10
	score -= warnIssues * 4
	if score < 5 {
		score = 5
	}
	if score > 99 {
		score = 99
	}
	return &skillInstallability{
		Score:    score,
		Label:    label,
		Checks:   totalChecks,
		Missing:  missing,
		Warnings: warnings + warnIssues + errorIssues,
	}
}

func buildSkillRiskProjection(report skill.SkillRuntimeReport) *skillRiskProjection {
	tags := make([]string, 0, 6)
	appendTag := func(tag string) {
		tag = strings.TrimSpace(tag)
		if tag == "" {
			return
		}
		for _, existing := range tags {
			if existing == tag {
				return
			}
		}
		tags = append(tags, tag)
	}
	if report.Blocked || report.Status == skill.StatusBlocked {
		appendTag("blocked")
	}
	if report.Trust == skill.TrustUnknown || report.Trust == skill.TrustCommunity {
		appendTag("unverified_source")
	}
	for _, check := range report.Checks {
		if check.Status == skill.DependencyStatusMissing || check.Status == skill.DependencyStatusUnsupported || check.Status == skill.DependencyStatusDisabled {
			appendTag("missing_dependencies")
			break
		}
	}
	for _, issue := range report.Issues {
		if issue.Severity == skill.SeverityError || issue.Severity == skill.SeverityWarning {
			appendTag("runtime_issues")
			break
		}
	}
	for _, tool := range report.Tools {
		if tool.RequiresApproval {
			appendTag("approval_required")
		}
		sideEffect := strings.ToLower(strings.TrimSpace(tool.SideEffectClass))
		if sideEffect != "" && sideEffect != "read" && sideEffect != "readonly" && sideEffect != "observe" && sideEffect != "none" {
			appendTag("write_capabilities")
		}
	}
	level := "low"
	switch {
	case hasString(tags, "blocked") || hasString(tags, "runtime_issues"):
		level = "high"
	case hasString(tags, "unverified_source") || hasString(tags, "approval_required") || hasString(tags, "write_capabilities") || hasString(tags, "missing_dependencies"):
		level = "medium"
	}
	return &skillRiskProjection{
		Level: level,
		Tags:  tags,
	}
}

func skillToolNames(tools []skill.SkillRuntimeToolReport) []string {
	if len(tools) == 0 {
		return nil
	}
	out := make([]string, 0, len(tools))
	for _, tool := range tools {
		name := strings.TrimSpace(tool.Name)
		if name == "" {
			continue
		}
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

func hasString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

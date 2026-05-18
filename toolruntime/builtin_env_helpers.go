package toolruntime

import (
	"context"
	"path/filepath"
	"sort"
	"strings"

	"github.com/fulcrus/hopclaw/skill"
)

func builtinSkillRuntimeContext(b *Builtins) skill.RuntimeContext {
	if b != nil && b.config.RuntimeFacts != nil {
		return b.config.RuntimeFacts(b.rootAbs)
	}
	return skill.RuntimeContext{}
}

func inspectSkillSourceWithBuiltins(ctx context.Context, b *Builtins, dir string, sourceKind skill.SourceKind, runtimeCtx skill.RuntimeContext) (skill.SkillRuntimeReport, error) {
	if b.skillService != nil {
		return b.skillService.InspectSource(ctx, dir, sourceKind, runtimeCtx)
	}
	src := skill.SkillSource{
		Kind:     sourceKind,
		Root:     filepath.Dir(dir),
		Dir:      dir,
		NameHint: filepath.Base(dir),
	}
	return skill.InspectSource(ctx, src, skill.DefaultCompiler{}, skill.Evaluator{}, runtimeCtx)
}

func attachInstalledMetadata(report *skill.SkillRuntimeReport, hub skill.ClawHubClient) {
	if report == nil || hub == nil {
		return
	}
	locks, err := hub.Installed()
	if err != nil {
		return
	}
	for _, lock := range locks {
		if reportMatchesInstalledLock(*report, lock) {
			skill.ApplyInstalledLock(report, lock)
			return
		}
	}
}

func reportMatchesInstalledLock(report skill.SkillRuntimeReport, lock skill.InstalledSkillLock) bool {
	candidates := []string{
		report.Name,
		report.SkillID,
		report.ConfigKey,
		report.SourceNameHint,
		filepath.Base(report.SourceDir),
		filepath.Base(filepath.Dir(report.SourceDir)),
	}
	for _, candidate := range candidates {
		if strings.EqualFold(strings.TrimSpace(candidate), strings.TrimSpace(lock.SkillID)) {
			return true
		}
	}
	return false
}

func validationPayload(report skill.SkillRuntimeReport) map[string]any {
	return skillReportPayload(report)
}

func skillReportPayload(report skill.SkillRuntimeReport) map[string]any {
	payload := map[string]any{
		"found":    report.Found,
		"loaded":   report.Loaded,
		"blocked":  report.Blocked,
		"eligible": report.Eligible,
		"ready":    report.Ready,
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
	if report.Installed {
		payload["installed"] = true
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
		checks := make([]map[string]any, 0, len(report.Checks))
		for _, check := range report.Checks {
			item := map[string]any{
				"kind":    string(check.Kind),
				"status":  string(check.Status),
				"present": check.Present,
			}
			if check.Name != "" {
				item["name"] = check.Name
			}
			if len(check.Candidates) > 0 {
				item["candidates"] = append([]string(nil), check.Candidates...)
			}
			if check.Source != "" {
				item["source"] = check.Source
			}
			if check.Path != "" {
				item["path"] = check.Path
			}
			if check.Message != "" {
				item["message"] = check.Message
			}
			if check.Hint != "" {
				item["hint"] = check.Hint
			}
			checks = append(checks, item)
		}
		payload["checks"] = checks
	}
	if len(report.InjectedEnv) > 0 {
		payload["injected_env"] = append([]string(nil), report.InjectedEnv...)
	}
	if len(report.Tools) > 0 {
		tools := make([]map[string]any, 0, len(report.Tools))
		for _, tool := range report.Tools {
			item := map[string]any{
				"name":              tool.Name,
				"idempotent":        tool.Idempotent,
				"requires_approval": tool.RequiresApproval,
			}
			if len(tool.Aliases) > 0 {
				item["aliases"] = append([]string(nil), tool.Aliases...)
			}
			if tool.Description != "" {
				item["description"] = tool.Description
			}
			if tool.SideEffectClass != "" {
				item["side_effect_class"] = tool.SideEffectClass
			}
			if tool.ExecutionKey != "" {
				item["execution_key"] = tool.ExecutionKey
			}
			if tool.RuntimeEntry != "" {
				item["runtime_entry"] = tool.RuntimeEntry
			}
			if tool.RuntimeShell != "" {
				item["runtime_shell"] = tool.RuntimeShell
			}
			if tool.Timeout != "" {
				item["timeout"] = tool.Timeout
			}
			tools = append(tools, item)
		}
		payload["tools"] = tools
	}
	if len(report.InstallHints) > 0 {
		hints := make([]map[string]any, 0, len(report.InstallHints))
		for _, hint := range report.InstallHints {
			item := map[string]any{}
			if hint.ID != "" {
				item["id"] = hint.ID
			}
			if hint.Kind != "" {
				item["kind"] = hint.Kind
			}
			if hint.Label != "" {
				item["label"] = hint.Label
			}
			if len(hint.OS) > 0 {
				item["os"] = append([]string(nil), hint.OS...)
			}
			if len(hint.Bins) > 0 {
				item["bins"] = append([]string(nil), hint.Bins...)
			}
			if hint.Formula != "" {
				item["formula"] = hint.Formula
			}
			if hint.Package != "" {
				item["package"] = hint.Package
			}
			if hint.Module != "" {
				item["module"] = hint.Module
			}
			if hint.URL != "" {
				item["url"] = hint.URL
			}
			if hint.Archive != "" {
				item["archive"] = hint.Archive
			}
			if hint.TargetDir != "" {
				item["target_dir"] = hint.TargetDir
			}
			if hint.StripComponents > 0 {
				item["strip_components"] = hint.StripComponents
			}
			hints = append(hints, item)
		}
		payload["install_hints"] = hints
	}
	if len(report.Issues) > 0 {
		issues := make([]map[string]any, 0, len(report.Issues))
		for _, issue := range report.Issues {
			item := map[string]any{
				"severity": string(issue.Severity),
				"message":  issue.Message,
			}
			if issue.Code != "" {
				item["code"] = issue.Code
			}
			issues = append(issues, item)
		}
		payload["issues"] = issues
	}
	if len(report.NextActions) > 0 {
		payload["next_actions"] = append([]string(nil), report.NextActions...)
	}
	return payload
}

func inspectInstalledSkillValidation(b *Builtins, ref string) map[string]any {
	if b == nil || b.skillService == nil {
		return nil
	}
	report, ok := b.skillService.Inspect(ref, builtinSkillRuntimeContext(b))
	if !ok {
		return nil
	}
	attachInstalledMetadata(&report, b.clawHub)
	return validationPayload(report)
}

// ---------------------------------------------------------------------------
// skill.ensure
// ---------------------------------------------------------------------------

type skillEnsureCandidate struct {
	ID        string   `json:"id"`
	Name      string   `json:"name"`
	Version   string   `json:"version,omitempty"`
	Summary   string   `json:"summary,omitempty"`
	Installed bool     `json:"installed"`
	Loaded    bool     `json:"loaded"`
	Tools     []string `json:"tools,omitempty"`
	Score     int      `json:"score"`
}

func buildSkillEnsureQueries(query, goal string, requiredTools []string) []string {
	var queries []string
	if len(requiredTools) > 0 {
		queries = append(queries, strings.Join(requiredTools, " "))
		queries = append(queries, requiredTools...)
		for _, toolName := range requiredTools {
			queries = append(queries, splitSkillEnsureTokens(toolName)...)
		}
	}
	if query != "" {
		queries = append(queries, query)
		queries = append(queries, splitSkillEnsureTokens(query)...)
	}
	if goal != "" {
		queries = append(queries, goal)
		queries = append(queries, splitSkillEnsureTokens(goal)...)
	}
	return cleanStringSlice(queries)
}

func searchSkillEnsureCatalog(ctx context.Context, hub skill.ClawHubClient, queries []string) ([]skill.RegistrySkill, error) {
	merged := make([]skill.RegistrySkill, 0)
	seen := make(map[string]struct{})
	var firstErr error
	for _, query := range queries {
		results, err := hub.Search(ctx, query)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		for _, item := range results {
			key := strings.ToLower(strings.TrimSpace(item.ID))
			if key == "" {
				key = strings.ToLower(strings.TrimSpace(item.Name))
			}
			if key == "" {
				continue
			}
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			merged = append(merged, item)
		}
	}
	if len(merged) == 0 && firstErr != nil {
		return nil, firstErr
	}
	return merged, nil
}

func loadedSkillState(service *skill.Service) (map[string]bool, map[string]skillEnsureCandidate) {
	loadedByTool := make(map[string]bool)
	loadedBySkill := make(map[string]skillEnsureCandidate)
	if service == nil {
		return loadedByTool, loadedBySkill
	}

	snapshot := service.Snapshot()
	for _, pkg := range snapshot.Ordered {
		candidate := skillEnsureCandidate{
			ID:      pkg.ID,
			Name:    pkg.Name(),
			Version: "",
			Summary: pkg.Prompt.Description,
			Loaded:  true,
		}
		for _, manifest := range pkg.ToolManifests {
			candidate.Tools = append(candidate.Tools, manifest.Name)
			loadedByTool[strings.ToLower(strings.TrimSpace(manifest.Name))] = true
			for _, alias := range manifest.Aliases {
				loadedByTool[strings.ToLower(strings.TrimSpace(alias))] = true
			}
		}
		sort.Strings(candidate.Tools)
		loadedBySkill[strings.ToLower(strings.TrimSpace(pkg.ID))] = candidate
		loadedBySkill[strings.ToLower(strings.TrimSpace(pkg.Name()))] = candidate
	}
	return loadedByTool, loadedBySkill
}

func missingRequiredTools(requiredTools []string, loadedByTool map[string]bool) []string {
	var missing []string
	for _, toolName := range requiredTools {
		key := strings.ToLower(strings.TrimSpace(toolName))
		if key == "" {
			continue
		}
		if !loadedByTool[key] {
			missing = append(missing, toolName)
		}
	}
	return missing
}

func unresolvedRequiredTools(requiredTools []string, service *skill.Service) []string {
	if len(requiredTools) == 0 {
		return nil
	}
	loadedByTool, _ := loadedSkillState(service)
	return missingRequiredTools(requiredTools, loadedByTool)
}

func rankSkillEnsureCandidates(results []skill.RegistrySkill, query string, requiredTools []string, installedByID map[string]skill.InstalledSkillLock, loadedBySkill map[string]skillEnsureCandidate) []skillEnsureCandidate {
	out := make([]skillEnsureCandidate, 0, len(results))
	queryTokens := splitSkillEnsureTokens(query)
	for _, item := range results {
		keyID := strings.ToLower(strings.TrimSpace(item.ID))
		keyName := strings.ToLower(strings.TrimSpace(item.Name))
		candidate := skillEnsureCandidate{
			ID:      item.ID,
			Name:    item.Name,
			Version: item.Version,
			Summary: item.Summary,
			Score:   scoreSkillEnsureCandidate(item, query, queryTokens, requiredTools),
		}
		if installed, ok := installedByID[keyID]; ok {
			candidate.Installed = true
			if candidate.Version == "" {
				candidate.Version = installed.Version
			}
		}
		if loaded, ok := loadedBySkill[keyID]; ok {
			candidate.Loaded = true
			candidate.Tools = append([]string(nil), loaded.Tools...)
		} else if loaded, ok := loadedBySkill[keyName]; ok {
			candidate.Loaded = true
			candidate.Tools = append([]string(nil), loaded.Tools...)
		}
		out = append(out, candidate)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Score != out[j].Score {
			return out[i].Score > out[j].Score
		}
		if out[i].Installed != out[j].Installed {
			return out[i].Installed
		}
		if out[i].Name != out[j].Name {
			return out[i].Name < out[j].Name
		}
		return out[i].ID < out[j].ID
	})
	return out
}

func scoreSkillEnsureCandidate(item skill.RegistrySkill, query string, queryTokens, requiredTools []string) int {
	id := strings.ToLower(strings.TrimSpace(item.ID))
	name := strings.ToLower(strings.TrimSpace(item.Name))
	summary := strings.ToLower(strings.TrimSpace(item.Summary))
	score := 0

	lowerQuery := strings.ToLower(strings.TrimSpace(query))
	if lowerQuery != "" {
		switch {
		case id == lowerQuery || name == lowerQuery:
			score += 100
		case strings.Contains(id, lowerQuery) || strings.Contains(name, lowerQuery):
			score += 40
		case strings.Contains(summary, lowerQuery):
			score += 20
		}
	}
	for _, token := range queryTokens {
		switch {
		case token == id || token == name:
			score += 18
		case strings.Contains(id, token) || strings.Contains(name, token):
			score += 8
		case strings.Contains(summary, token):
			score += 3
		}
	}
	for _, toolName := range requiredTools {
		lowerTool := strings.ToLower(strings.TrimSpace(toolName))
		if lowerTool == "" {
			continue
		}
		if strings.Contains(id, lowerTool) || strings.Contains(name, lowerTool) || strings.Contains(summary, lowerTool) {
			score += 25
		}
		for _, token := range splitSkillEnsureTokens(lowerTool) {
			if strings.Contains(id, token) || strings.Contains(name, token) || strings.Contains(summary, token) {
				score += 5
			}
		}
	}
	return score
}

func splitSkillEnsureTokens(input string) []string {
	lower := strings.ToLower(strings.TrimSpace(input))
	if lower == "" {
		return nil
	}
	fields := strings.FieldsFunc(lower, func(r rune) bool {
		switch {
		case r >= 'a' && r <= 'z':
			return false
		case r >= '0' && r <= '9':
			return false
		default:
			return true
		}
	})
	return cleanStringSlice(fields)
}

func cleanStringSlice(items []string) []string {
	if len(items) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(items))
	out := make([]string, 0, len(items))
	for _, item := range items {
		trimmed := strings.TrimSpace(item)
		if trimmed == "" {
			continue
		}
		key := strings.ToLower(trimmed)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, trimmed)
	}
	return out
}

// ---------------------------------------------------------------------------
// skill.install
// ---------------------------------------------------------------------------

func installStepPayloads(steps []skill.InstallStepResult) []map[string]any {
	if len(steps) == 0 {
		return nil
	}
	out := make([]map[string]any, 0, len(steps))
	for _, step := range steps {
		out = append(out, map[string]any{
			"id":      step.ID,
			"kind":    step.Kind,
			"label":   step.Label,
			"status":  string(step.Status),
			"reason":  step.Reason,
			"command": append([]string(nil), step.Command...),
			"path":    step.Path,
		})
	}
	return out
}

// ---------------------------------------------------------------------------
// skill.remove
// ---------------------------------------------------------------------------

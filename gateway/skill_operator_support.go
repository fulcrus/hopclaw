package gateway

import (
	"context"
	"errors"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/fulcrus/hopclaw/config"
	"github.com/fulcrus/hopclaw/internal/modules"
	"github.com/fulcrus/hopclaw/internal/runtimeenv"
	"github.com/fulcrus/hopclaw/internal/support/normalize"
	"github.com/fulcrus/hopclaw/internal/telemetry"
	runtimeprobe "github.com/fulcrus/hopclaw/runtime"
	"github.com/fulcrus/hopclaw/skill"
)

var errSkillIDRequired = errors.New("skill id is required")

func (g *Gateway) installSkill(ctx context.Context, req skillInstallRequest) (*skill.InstallResult, int, error) {
	skillID, source, localSource := resolveSkillInstallRequest(req)
	if skillID == "" && !localSource {
		return nil, http.StatusBadRequest, errSkillIDRequired
	}

	var (
		result *skill.InstallResult
		err    error
	)
	if localSource {
		installer, ok := g.skillHub.(skill.LocalSourceInstaller)
		if !ok {
			return nil, http.StatusBadRequest, errors.New("local skill source install is not supported")
		}
		result, err = installer.InstallFromSource(ctx, skill.InstallRequest{
			SkillID: skillID,
			Version: strings.TrimSpace(req.Version),
		}, source)
		if err != nil {
			return nil, http.StatusBadRequest, err
		}
	}
	if result == nil && err == nil {
		result, err = g.skillHub.Install(ctx, skill.InstallRequest{
			SkillID: skillID,
			Version: strings.TrimSpace(req.Version),
		})
	}
	if err != nil {
		return nil, http.StatusBadGateway, err
	}
	sourceKind := "catalog"
	if localSource {
		sourceKind = "local"
	}
	go func(diagCfg config.DiagnosticsConfig, skillID, version, source string) {
		emitCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := telemetry.RecordSkillInstalled(emitCtx, diagCfg, skillID, version, source); err != nil {
			telemetry.DebugLog(diagCfg, "skill install telemetry failed", "skill_id", skillID, "error", err)
		}
	}(g.config.Diagnostics, result.SkillID, result.Version, sourceKind)
	return result, 0, nil
}

func resolveSkillInstallRequest(req skillInstallRequest) (skillID string, source string, localSource bool) {
	skillID = strings.TrimSpace(req.Name)
	source = strings.TrimSpace(req.Source)
	if source == "" {
		return skillID, source, false
	}
	if _, err := os.Stat(source); err == nil {
		return skillID, source, true
	}
	if skillID == "" {
		skillID = source
	}
	return skillID, source, false
}

func skillInstallPayload(result *skill.InstallResult) map[string]any {
	payload := map[string]any{
		"ok":            true,
		"skill_id":      result.SkillID,
		"version":       result.Version,
		"install_dir":   result.InstallDir,
		"lock_file":     result.LockFilePath,
		"install_steps": result.InstallerSteps,
	}
	return payload
}

func (g *Gateway) updateSkillConfig(name string, updates map[string]any) (skillConfigUpdateResponse, error) {
	configKey := g.resolveSkillConfigKey(name)
	err := g.modifyConfigFile(func(root map[string]any) error {
		skillsNode := ensureObjectMap(root["skills"])
		configNode := ensureObjectMap(skillsNode["config"])
		configNode[configKey] = updates
		skillsNode["config"] = configNode
		root["skills"] = skillsNode
		return nil
	})
	if err != nil {
		return skillConfigUpdateResponse{}, err
	}
	if err := g.triggerConfigReload(); err != nil {
		return skillConfigUpdateResponse{}, err
	}
	return skillConfigUpdateResponse{
		OK:         true,
		Name:       name,
		ConfigKey:  configKey,
		Config:     updates,
		ReloadPlan: config.AnalyzeReloadPlan([]string{"skills.config." + configKey}),
	}, nil
}

func (g *Gateway) buildSkillPreflight(ctx context.Context, req preflightRequest) (preflightResponse, int, error) {
	checks, ready := buildSkillPreflightChecks(req)

	skillPayload, skillReady, status, err := g.inspectSkillPreflightTarget(ctx, req)
	if err != nil {
		return preflightResponse{}, status, err
	}
	if skillPayload != nil {
		ready = ready && skillReady
	}
	return preflightResponse{
		Checks: checks,
		Ready:  ready,
		Skill:  skillPayload,
	}, 0, nil
}

func buildSkillPreflightChecks(req preflightRequest) ([]preflightCheck, bool) {
	checks := make([]preflightCheck, 0, len(req.Binaries)+len(req.EnvVars))
	allPresent := true

	for _, bin := range req.Binaries {
		bin = strings.TrimSpace(bin)
		if bin == "" {
			continue
		}
		_, err := exec.LookPath(bin)
		present := err == nil
		if !present {
			allPresent = false
		}
		checks = append(checks, preflightCheck{
			Name:    bin,
			Kind:    "binary",
			Present: present,
		})
	}

	for _, envVar := range req.EnvVars {
		envVar = strings.TrimSpace(envVar)
		if envVar == "" {
			continue
		}
		_, present := os.LookupEnv(envVar)
		if !present {
			allPresent = false
		}
		checks = append(checks, preflightCheck{
			Name:    envVar,
			Kind:    "env_var",
			Present: present,
		})
	}

	return checks, allPresent
}

func (g *Gateway) inspectSkillPreflightTarget(ctx context.Context, req preflightRequest) (map[string]any, bool, int, error) {
	if source := strings.TrimSpace(req.Source); source != "" {
		report, err := g.inspectSkillSource(ctx, source, strings.TrimSpace(req.Kind))
		if err != nil {
			return nil, false, http.StatusBadRequest, err
		}
		return gatewaySkillReportPayload(report), report.Ready, 0, nil
	}

	name := strings.TrimSpace(req.Name)
	if name == "" {
		return nil, true, 0, nil
	}
	if g.skillService == nil {
		return nil, false, http.StatusServiceUnavailable, errors.New("skill service not available")
	}
	report, ok := g.skillService.Inspect(name, g.skillRuntimeContext())
	if !ok {
		return nil, false, http.StatusNotFound, errors.New("skill not found")
	}
	g.attachInstalledSkill(&report)
	return gatewaySkillReportPayload(report), report.Ready, 0, nil
}

func (g *Gateway) listInstalledSkills(ctx context.Context) []skillSummary {
	items := make([]skillSummary, 0)
	locksByID := make(map[string]skill.InstalledSkillLock)
	if g.skillHub != nil {
		if installed, err := g.skillHub.Installed(); err == nil {
			for _, entry := range installed {
				locksByID[entry.SkillID] = entry
				items = append(items, skillSummary{
					ID:              entry.SkillID,
					Name:            entry.SkillID,
					Version:         entry.Version,
					InstallDir:      entry.InstallDir,
					BundleDir:       entry.BundleDir,
					Pinned:          entry.Pinned,
					Installed:       true,
					DetailAvailable: true,
					InstalledAt:     entry.InstalledAt.UTC().Format(time.RFC3339),
				})
			}
		}
	}

	projections := g.currentSkillProjections()
	if len(projections) == 0 && g.skillService == nil {
		for idx := range items {
			source := strings.TrimSpace(items[idx].InstallDir)
			if source == "" {
				source = strings.TrimSpace(items[idx].BundleDir)
			}
			if source == "" {
				continue
			}
			report, err := g.inspectSkillSource(ctx, source, string(skill.SourceClawHub))
			if err != nil {
				continue
			}
			applySkillProjection(&items[idx], report)
		}
		finalizeInstalledSkillSummaries(items)
		sort.Slice(items, func(i, j int) bool { return items[i].Name < items[j].Name })
		return items
	}

	if len(projections) > 0 {
		items = g.mergeProjectedSkillsIntoSummaries(items, locksByID, projections)
	} else {
		items = g.mergeServiceSkillsIntoSummaries(items, locksByID)
	}
	for idx := range items {
		if items[idx].Installability != nil && items[idx].Risk != nil {
			continue
		}
		source := strings.TrimSpace(items[idx].InstallDir)
		if source == "" {
			source = strings.TrimSpace(items[idx].BundleDir)
		}
		if source == "" {
			continue
		}
		report, err := g.inspectSkillSource(ctx, source, string(skill.SourceClawHub))
		if err != nil {
			continue
		}
		applySkillProjection(&items[idx], report)
	}
	finalizeInstalledSkillSummaries(items)
	sort.Slice(items, func(i, j int) bool { return items[i].Name < items[j].Name })
	return items
}

func (g *Gateway) currentSkillProjections() []modules.SkillProjection {
	if g == nil || g.moduleCatalog == nil {
		return nil
	}
	return g.moduleCatalog.SkillProjections()
}

func (g *Gateway) mergeProjectedSkillsIntoSummaries(items []skillSummary, locksByID map[string]skill.InstalledSkillLock, projections []modules.SkillProjection) []skillSummary {
	indexByID, indexByName := skillSummaryIndexes(items)
	for _, projection := range projections {
		summary := skillSummaryFromModuleProjection(projection)
		if lock, ok := lockForSkillProjection(locksByID, projection); ok {
			summary = applyInstalledLockToSummary(summary, lock)
		}
		if report, ok := g.inspectInstalledSkill(projection.Name); ok {
			applySkillProjection(&summary, report)
		}
		if idx, ok := summaryIndex(indexByID, indexByName, summary.ID, summary.Name); ok {
			items[idx] = mergeSkillSummary(items[idx], summary)
			continue
		}
		items = append(items, summary)
		indexByID, indexByName = skillSummaryIndexes(items)
	}
	return items
}

func (g *Gateway) mergeServiceSkillsIntoSummaries(items []skillSummary, locksByID map[string]skill.InstalledSkillLock) []skillSummary {
	if g == nil || g.skillService == nil {
		return items
	}
	indexByID, indexByName := skillSummaryIndexes(items)
	for _, pkg := range g.skillService.Snapshot().Ordered {
		if pkg == nil {
			continue
		}
		id := pkg.Name()
		if lock, ok := locksByID[pkg.Name()]; ok {
			id = lock.SkillID
		}
		summary := skillSummary{
			ID:            id,
			Name:          pkg.Name(),
			Kind:          string(pkg.Kind),
			Status:        string(pkg.Status),
			Trust:         string(pkg.Trust),
			SourceKind:    string(pkg.Source.Kind),
			UserInvocable: cloneBoolPtrGateway(&pkg.Prompt.UserInvocable),
		}
		report := skill.BuildRuntimeReport(pkg, g.skillRuntimeContext(), skill.Evaluator{})
		g.attachInstalledSkill(&report)
		applySkillProjection(&summary, report)
		if lock, ok := locksByID[id]; ok {
			summary = applyInstalledLockToSummary(summary, lock)
		}
		if idx, ok := summaryIndex(indexByID, indexByName, summary.ID, summary.Name); ok {
			items[idx] = mergeSkillSummary(items[idx], summary)
			continue
		}
		items = append(items, summary)
		indexByID, indexByName = skillSummaryIndexes(items)
	}
	return items
}

func skillSummaryFromModuleProjection(projection modules.SkillProjection) skillSummary {
	id := strings.TrimSpace(projection.ID)
	if id == "" {
		id = strings.TrimSpace(projection.Name)
	}
	userInvocable := projection.UserInvocable
	return skillSummary{
		ID:              id,
		Name:            strings.TrimSpace(projection.Name),
		Kind:            strings.TrimSpace(projection.Kind),
		Status:          strings.TrimSpace(projection.Status),
		Trust:           strings.TrimSpace(projection.Trust),
		SourceKind:      strings.TrimSpace(projection.SourceKind),
		Summary:         strings.TrimSpace(projection.Description),
		Description:     strings.TrimSpace(projection.Description),
		Ready:           !projection.Blocked,
		Eligible:        !projection.Blocked,
		Tools:           append([]string(nil), projection.ToolNames...),
		ToolCount:       projection.ToolCount,
		DetailAvailable: true,
		UserInvocable:   &userInvocable,
	}
}

func lockForSkillProjection(locksByID map[string]skill.InstalledSkillLock, projection modules.SkillProjection) (skill.InstalledSkillLock, bool) {
	for _, key := range []string{projection.Name, projection.ID, projection.ConfigKey} {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		if lock, ok := locksByID[key]; ok {
			return lock, true
		}
	}
	return skill.InstalledSkillLock{}, false
}

func applyInstalledLockToSummary(summary skillSummary, lock skill.InstalledSkillLock) skillSummary {
	if trimmed := strings.TrimSpace(lock.SkillID); trimmed != "" {
		summary.ID = trimmed
	}
	summary.Version = strings.TrimSpace(lock.Version)
	summary.InstallDir = strings.TrimSpace(lock.InstallDir)
	summary.BundleDir = strings.TrimSpace(lock.BundleDir)
	summary.Pinned = lock.Pinned
	if !lock.InstalledAt.IsZero() {
		summary.InstalledAt = lock.InstalledAt.UTC().Format(time.RFC3339)
	}
	return summary
}

func skillSummaryIndexes(items []skillSummary) (map[string]int, map[string]int) {
	indexByID := make(map[string]int, len(items))
	indexByName := make(map[string]int, len(items))
	for idx, item := range items {
		if key := skillSummaryIndexKey(item.ID); key != "" {
			indexByID[key] = idx
		}
		if key := skillSummaryIndexKey(item.Name); key != "" {
			indexByName[key] = idx
		}
	}
	return indexByID, indexByName
}

func summaryIndex(indexByID, indexByName map[string]int, id, name string) (int, bool) {
	if idx, ok := indexByID[skillSummaryIndexKey(id)]; ok {
		return idx, true
	}
	idx, ok := indexByName[skillSummaryIndexKey(name)]
	return idx, ok
}

func skillSummaryIndexKey(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func finalizeInstalledSkillSummaries(items []skillSummary) {
	for idx := range items {
		if items[idx].Installability == nil {
			items[idx].Installability = &skillInstallability{
				Score: 60,
				Label: "installed",
			}
		}
		if items[idx].Risk == nil {
			items[idx].Risk = &skillRiskProjection{
				Level: "medium",
				Tags:  []string{"runtime_report_pending"},
			}
		}
	}
}

func mergeSkillSummary(base, incoming skillSummary) skillSummary {
	if incoming.ID != "" {
		base.ID = incoming.ID
	}
	if incoming.Name != "" {
		base.Name = incoming.Name
	}
	if incoming.Kind != "" {
		base.Kind = incoming.Kind
	}
	if incoming.Status != "" {
		base.Status = incoming.Status
	}
	if incoming.Trust != "" {
		base.Trust = incoming.Trust
	}
	if incoming.Version != "" {
		base.Version = incoming.Version
	}
	if incoming.InstallDir != "" {
		base.InstallDir = incoming.InstallDir
	}
	if incoming.BundleDir != "" {
		base.BundleDir = incoming.BundleDir
	}
	if incoming.SourceKind != "" {
		base.SourceKind = incoming.SourceKind
	}
	if incoming.Summary != "" {
		base.Summary = incoming.Summary
	}
	if incoming.Description != "" {
		base.Description = incoming.Description
	}
	if incoming.Installed {
		base.Installed = true
	}
	if incoming.Ready {
		base.Ready = true
	}
	if incoming.Eligible {
		base.Eligible = true
	}
	if len(incoming.Tools) > 0 {
		base.Tools = append([]string(nil), incoming.Tools...)
	}
	if incoming.ToolCount != 0 {
		base.ToolCount = incoming.ToolCount
	}
	if incoming.DetailAvailable {
		base.DetailAvailable = true
	}
	if incoming.Installability != nil {
		value := *incoming.Installability
		base.Installability = &value
	}
	if incoming.Risk != nil {
		value := *incoming.Risk
		base.Risk = &value
	}
	if incoming.Pinned {
		base.Pinned = true
	}
	if incoming.InstalledAt != "" {
		base.InstalledAt = incoming.InstalledAt
	}
	return base
}

func (g *Gateway) refreshSkillRegistry(ctx context.Context) {
	if g.skillService == nil {
		return
	}
	if _, err := g.skillService.Refresh(ctx); err != nil {
		log.Warn("skill refresh failed", "error", err)
	}
}

func (g *Gateway) installedSkillSet() map[string]bool {
	installed := make(map[string]bool)
	if g.skillHub == nil {
		return installed
	}
	locks, err := g.skillHub.Installed()
	if err != nil {
		return installed
	}
	for _, entry := range locks {
		installed[entry.SkillID] = true
	}
	return installed
}

func (g *Gateway) resolveSkillConfigKey(name string) string {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return trimmed
	}
	for _, projection := range g.currentSkillProjections() {
		if strings.EqualFold(projection.Name, trimmed) || strings.EqualFold(projection.ID, trimmed) || strings.EqualFold(projection.ConfigKey, trimmed) {
			return projection.ConfigKey
		}
	}
	if g.skillService != nil {
		snapshot := g.skillService.Snapshot()
		for _, pkg := range snapshot.Ordered {
			if strings.EqualFold(pkg.Name(), trimmed) || strings.EqualFold(pkg.ID, trimmed) || strings.EqualFold(pkg.ConfigKey(), trimmed) {
				return pkg.ConfigKey()
			}
		}
	}
	return trimmed
}

func (g *Gateway) skillRuntimeContext() skill.RuntimeContext {
	workDir := ""
	if g.configPath != "" {
		workDir = filepath.Dir(g.configPath)
	}
	if workDir == "" {
		if cwd, err := os.Getwd(); err == nil {
			workDir = cwd
		}
	}
	if cfg, ok := g.currentOperatorConfig(); ok {
		return runtimeenv.BuildRuntimeFacts(workDir, cfg)
	}
	return runtimeprobe.DetectContext(workDir)
}

func (g *Gateway) attachInstalledSkill(report *skill.SkillRuntimeReport) {
	if report == nil || g.skillHub == nil {
		return
	}
	locks, err := g.skillHub.Installed()
	if err != nil {
		return
	}
	for _, lock := range locks {
		if strings.EqualFold(strings.TrimSpace(lock.SkillID), strings.TrimSpace(report.Name)) ||
			strings.EqualFold(strings.TrimSpace(lock.SkillID), strings.TrimSpace(report.SourceNameHint)) ||
			strings.EqualFold(strings.TrimSpace(lock.SkillID), strings.TrimSpace(report.ConfigKey)) {
			skill.ApplyInstalledLock(report, lock)
			return
		}
	}
}

func (g *Gateway) inspectInstalledSkill(ref string) (skill.SkillRuntimeReport, bool) {
	if g.skillService == nil {
		return skill.SkillRuntimeReport{}, false
	}
	report, ok := g.skillService.Inspect(ref, g.skillRuntimeContext())
	if !ok {
		return skill.SkillRuntimeReport{}, false
	}
	g.attachInstalledSkill(&report)
	return report, true
}

func (g *Gateway) inspectSkillSource(ctx context.Context, source, kind string) (skill.SkillRuntimeReport, error) {
	sourceKind := skill.SourceWorkspace
	if trimmed := strings.TrimSpace(kind); trimmed != "" {
		sourceKind = skill.SourceKind(trimmed)
	}
	runtimeCtx := g.skillRuntimeContext()
	if g.skillService != nil {
		report, err := g.skillService.InspectSource(ctx, source, sourceKind, runtimeCtx)
		if err != nil {
			return skill.SkillRuntimeReport{}, err
		}
		g.attachInstalledSkill(&report)
		return report, nil
	}
	src := skill.SkillSource{
		Kind:     sourceKind,
		Root:     filepath.Dir(source),
		Dir:      source,
		NameHint: filepath.Base(source),
	}
	report, err := skill.InspectSource(ctx, src, skill.DefaultCompiler{}, skill.Evaluator{}, runtimeCtx)
	if err != nil {
		return skill.SkillRuntimeReport{}, err
	}
	g.attachInstalledSkill(&report)
	return report, nil
}

func applySkillProjection(target *skillSummary, report skill.SkillRuntimeReport) {
	if target == nil {
		return
	}
	target.Summary = normalize.FirstNonEmpty(strings.TrimSpace(report.Description), strings.TrimSpace(target.Summary))
	target.Description = normalize.FirstNonEmpty(strings.TrimSpace(report.Description), strings.TrimSpace(target.Description))
	target.SourceKind = normalize.FirstNonEmpty(string(report.SourceKind), target.SourceKind)
	target.Ready = report.Ready
	target.Eligible = report.Eligible
	target.Installed = target.Installed || report.Installed
	target.Tools = skillToolNames(report.Tools)
	target.ToolCount = len(target.Tools)
	target.DetailAvailable = true
	target.Installability = buildSkillInstallability(report)
	target.Risk = buildSkillRiskProjection(report)
}

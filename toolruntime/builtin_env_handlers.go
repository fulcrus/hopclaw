package toolruntime

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/fulcrus/hopclaw/agent"
	"github.com/fulcrus/hopclaw/contextengine"
	"github.com/fulcrus/hopclaw/internal/modules"
	"github.com/fulcrus/hopclaw/internal/runtimeenv"
	"github.com/fulcrus/hopclaw/skill"
)

func handleEnvProbe(ctx context.Context, b *Builtins, call agent.ToolCall) (contextengine.ToolResult, error) {
	extraBins, err := stringSliceFrom(call.Input["bins"])
	if err != nil {
		return contextengine.ToolResult{}, err
	}
	overlay := sessionEnvOverlay(builtinSessionFromContext(ctx), builtinRunFromContext(ctx))

	hostname, _ := os.Hostname()

	shellPath := os.Getenv("SHELL")
	if shellPath == "" {
		shellPath = "unknown"
	}

	_, dockerEnvErr := os.Stat("/.dockerenv")
	inContainer := dockerEnvErr == nil

	// Merge common + extra bins, dedup.
	allBins := make([]string, 0, len(commonBins)+len(extraBins))
	seen := make(map[string]bool, len(commonBins)+len(extraBins))
	for _, name := range commonBins {
		if !seen[name] {
			seen[name] = true
			allBins = append(allBins, name)
		}
	}
	for _, name := range extraBins {
		if !seen[name] {
			seen[name] = true
			allBins = append(allBins, name)
		}
	}

	availableBins := make(map[string]string, len(allBins))
	checkedBins := make(map[string]bool, len(allBins))
	packageManagers := make([]string, 0)
	pmSet := map[string]bool{
		"brew": true, "apt-get": true, "apk": true,
		"yum": true, "dnf": true, "pip": true,
		"pip3": true, "npm": true, "cargo": true,
	}

	for _, name := range allBins {
		path, lookErr := runtimeenv.LookPathWithEnv(name, overlay)
		if lookErr == nil {
			availableBins[name] = path
			checkedBins[name] = true
			if pmSet[name] {
				packageManagers = append(packageManagers, name)
			}
		} else {
			availableBins[name] = ""
			checkedBins[name] = false
		}
	}

	result := map[string]any{
		"os":               runtime.GOOS,
		"arch":             runtime.GOARCH,
		"hostname":         hostname,
		"shell":            shellPath,
		"in_container":     inContainer,
		"package_managers": packageManagers,
		"available_bins":   availableBins,
		"checked_bins":     checkedBins,
	}

	// Include Layer 2 dormant group info if wired.
	if b.layer2 != nil {
		var dormantGroups []map[string]any
		for _, dg := range b.layer2.DormantGroups() {
			dormantGroups = append(dormantGroups, map[string]any{
				"group":        dg.Name,
				"tool_count":   dg.ToolCount,
				"missing":      dg.MissingBins,
				"install_hint": dg.InstallHint,
			})
		}
		result["dormant_groups"] = dormantGroups

		activeCount := 0
		for _, gs := range b.layer2.GroupStatuses() {
			if gs.Active {
				activeCount += gs.Tools
			}
		}
		result["layer2_active_tools"] = activeCount
	}

	return b.jsonResult(call, result)
}

// ---------------------------------------------------------------------------
// env.info
// ---------------------------------------------------------------------------

func handleEnvInfo(_ context.Context, b *Builtins, call agent.ToolCall) (contextengine.ToolResult, error) {
	hostname, _ := os.Hostname()
	return b.jsonResult(call, map[string]any{
		"os":         runtime.GOOS,
		"arch":       runtime.GOARCH,
		"hostname":   hostname,
		"cpus":       runtime.NumCPU(),
		"go_version": runtime.Version(),
	})
}

// ---------------------------------------------------------------------------
// env.get
// ---------------------------------------------------------------------------

func handleEnvGet(ctx context.Context, b *Builtins, call agent.ToolCall) (contextengine.ToolResult, error) {
	singleName, err := stringFrom(call.Input["name"])
	if err != nil {
		return contextengine.ToolResult{}, err
	}
	names, err := stringSliceFrom(call.Input["names"])
	if err != nil {
		return contextengine.ToolResult{}, err
	}
	runtimeCtx := builtinSkillRuntimeContext(b)
	overlay := sessionEnvOverlay(builtinSessionFromContext(ctx), builtinRunFromContext(ctx))

	// Bulk mode: names provided.
	if len(names) > 0 {
		// Include singleName if provided.
		if singleName != "" {
			found := false
			for _, n := range names {
				if n == singleName {
					found = true
					break
				}
			}
			if !found {
				names = append([]string{singleName}, names...)
			}
		}
		vars := make([]map[string]any, 0, len(names))
		for _, n := range names {
			vars = append(vars, envVisibilityPayload(strings.TrimSpace(n), runtimeCtx, overlay))
		}
		return b.jsonResult(call, map[string]any{
			"vars": vars,
		})
	}

	// Single mode.
	if singleName == "" {
		return contextengine.ToolResult{}, fmt.Errorf("name is required")
	}
	return b.jsonResult(call, envVisibilityPayload(strings.TrimSpace(singleName), runtimeCtx, overlay))
}

// ---------------------------------------------------------------------------
// env.set
// ---------------------------------------------------------------------------

func handleEnvSet(ctx context.Context, b *Builtins, call agent.ToolCall) (contextengine.ToolResult, error) {
	name, err := requiredString(call.Input, "name")
	if err != nil {
		return contextengine.ToolResult{}, err
	}
	value, err := requiredString(call.Input, "value")
	if err != nil {
		return contextengine.ToolResult{}, err
	}
	session := builtinSessionFromContext(ctx)
	if session == nil {
		return contextengine.ToolResult{}, fmt.Errorf("env.set requires session context")
	}
	run := builtinRunFromContext(ctx)
	setSessionEnvOverlay(session, run, name, value)
	scope := "session"
	if run != nil && strings.TrimSpace(run.ID) != "" {
		scope = "run"
	}
	return b.jsonResult(call, map[string]any{
		"name":    name,
		"scope":   scope,
		"message": "environment overlay stored without mutating the host process",
	})
}

// ---------------------------------------------------------------------------
// env.refresh
// ---------------------------------------------------------------------------

func handleEnvRefresh(ctx context.Context, b *Builtins, call agent.ToolCall) (contextengine.ToolResult, error) {
	hostname, _ := os.Hostname()
	overlay := sessionEnvOverlay(builtinSessionFromContext(ctx), builtinRunFromContext(ctx))
	availableCount := 0
	for _, name := range commonBins {
		if _, err := runtimeenv.LookPathWithEnv(name, overlay); err == nil {
			availableCount++
		}
	}

	var activated, deactivated []string
	var groupSummary []map[string]any

	// Re-probe Layer 2 eligibility — this is the key bootstrap wiring.
	if b.layer2 != nil {
		activated, deactivated = b.layer2.Probe()
		for _, gs := range b.layer2.GroupStatuses() {
			groupSummary = append(groupSummary, map[string]any{
				"group":  gs.Name,
				"active": gs.Active,
				"tools":  gs.Tools,
			})
		}
	}

	return b.jsonResult(call, map[string]any{
		"newly_active":  activated,
		"newly_dormant": deactivated,
		"groups":        groupSummary,
		"summary": map[string]any{
			"os":              runtime.GOOS,
			"arch":            runtime.GOARCH,
			"hostname":        hostname,
			"common_bins":     len(commonBins),
			"available_count": availableCount,
		},
	})
}

func envVisibilityPayload(name string, runtimeCtx skill.RuntimeContext, overlay map[string]string) map[string]any {
	payload := map[string]any{
		"name":     name,
		"exists":   false,
		"managed":  false,
		"redacted": false,
	}
	if strings.TrimSpace(name) == "" {
		return payload
	}

	if value := strings.TrimSpace(overlay[name]); value != "" {
		payload["exists"] = true
		payload["source"] = "overlay"
		payload["redacted"] = true
		return payload
	}

	if status, source, ok := managedEnvStatus(runtimeCtx, name); ok {
		payload["exists"] = status.Resolved
		payload["managed"] = true
		payload["redacted"] = status.Resolved
		if source != "" {
			payload["source"] = source
		}
		return payload
	}

	if status, ok := runtimeCtx.SecretPresence[name]; ok && status.Resolved {
		payload["exists"] = true
		payload["redacted"] = true
		payload["source"] = normalizeEnvSource(status.Source, "runtime_env")
		return payload
	}

	if value, ok := os.LookupEnv(name); ok && strings.TrimSpace(value) != "" {
		payload["exists"] = true
		payload["redacted"] = true
		payload["source"] = "runtime_env"
	}
	return payload
}

func managedEnvStatus(runtimeCtx skill.RuntimeContext, name string) (skill.SecretStatus, string, bool) {
	for _, entry := range runtimeCtx.Managed {
		status, ok := entry.InjectedEnv[name]
		if !ok || !status.Resolved {
			continue
		}
		return status, normalizeEnvSource(status.Source, "managed"), true
	}
	return skill.SecretStatus{}, "", false
}

func normalizeEnvSource(source, fallback string) string {
	trimmed := strings.TrimSpace(source)
	if trimmed == "" {
		return fallback
	}
	return trimmed
}

// ---------------------------------------------------------------------------
// skill.list
// ---------------------------------------------------------------------------

func handleSkillList(ctx context.Context, b *Builtins, call agent.ToolCall) (contextengine.ToolResult, error) {
	query, _ := stringFrom(call.Input["query"])

	// Installed skills from lock file.
	var installed []map[string]any
	if b.clawHub != nil {
		locks, err := b.clawHub.Installed()
		if err == nil {
			for _, l := range locks {
				installed = append(installed, map[string]any{
					"id":           l.SkillID,
					"version":      l.Version,
					"install_dir":  l.InstallDir,
					"pinned":       l.Pinned,
					"installed_at": l.InstalledAt.Format("2006-01-02T15:04:05Z"),
				})
			}
		}
	}

	// Registry snapshot — loaded skills with eligibility info.
	var loaded []map[string]any
	if b.moduleCatalog != nil {
		for _, projection := range b.moduleCatalog.SkillProjections() {
			loaded = append(loaded, skillListEntryFromProjection(projection))
		}
	} else if b.skillService != nil {
		snap := b.skillService.Snapshot()
		for _, pkg := range snap.Ordered {
			entry := map[string]any{
				"name":        pkg.Name(),
				"description": pkg.Prompt.Description,
				"kind":        string(pkg.Kind),
				"status":      string(pkg.Status),
				"source":      string(pkg.Source.Kind),
				"tools":       len(pkg.ToolManifests),
			}
			loaded = append(loaded, entry)
		}
	}

	// Catalog — searchable skills from ClawHub.
	var catalog []map[string]any
	if b.clawHub != nil {
		results, err := b.clawHub.Search(ctx, query)
		if err == nil {
			for _, r := range results {
				catalog = append(catalog, map[string]any{
					"id":      r.ID,
					"name":    r.Name,
					"version": r.Version,
					"summary": r.Summary,
				})
			}
		}
	}

	return b.jsonResult(call, map[string]any{
		"installed": installed,
		"loaded":    loaded,
		"catalog":   catalog,
	})
}

func skillListEntryFromProjection(projection modules.SkillProjection) map[string]any {
	entry := map[string]any{
		"name":        projection.Name,
		"description": projection.Description,
		"kind":        projection.Kind,
		"status":      projection.Status,
		"source":      projection.SourceKind,
		"tools":       projection.ToolCount,
	}
	if projection.ID != "" {
		entry["id"] = projection.ID
	}
	if projection.ConfigKey != "" {
		entry["config_key"] = projection.ConfigKey
	}
	if projection.Trust != "" {
		entry["trust"] = projection.Trust
	}
	return entry
}

// ---------------------------------------------------------------------------
// skill.inspect
// ---------------------------------------------------------------------------

func handleSkillInspect(ctx context.Context, b *Builtins, call agent.ToolCall) (contextengine.ToolResult, error) {
	name, _ := stringFrom(call.Input["name"])
	source, _ := stringFrom(call.Input["source"])
	sourceKindValue, _ := stringFrom(call.Input["source_kind"])

	runtimeCtx := builtinSkillRuntimeContext(b)
	if source != "" {
		sourceKind := skill.SourceWorkspace
		if trimmed := strings.TrimSpace(sourceKindValue); trimmed != "" {
			sourceKind = skill.SourceKind(trimmed)
		}
		report, err := inspectSkillSourceWithBuiltins(ctx, b, source, sourceKind, runtimeCtx)
		if err != nil {
			return b.jsonResult(call, map[string]any{
				"found":   false,
				"source":  source,
				"message": fmt.Sprintf("inspect source failed: %v", err),
			})
		}
		attachInstalledMetadata(&report, b.clawHub)
		payload := skillReportPayload(report)
		payload["message"] = "skill source inspected successfully"
		return b.jsonResult(call, payload)
	}

	ref := strings.TrimSpace(name)
	if ref == "" {
		return b.jsonResult(call, map[string]any{
			"found":   false,
			"message": "skill.inspect requires name or source",
		})
	}
	if b.skillService == nil {
		return b.jsonResult(call, map[string]any{
			"found":   false,
			"name":    ref,
			"message": "skill service not configured",
		})
	}

	report, ok := b.skillService.Inspect(ref, runtimeCtx)
	if !ok {
		return b.jsonResult(call, map[string]any{
			"found":   false,
			"name":    ref,
			"message": fmt.Sprintf("skill %q not found", ref),
		})
	}
	attachInstalledMetadata(&report, b.clawHub)
	payload := skillReportPayload(report)
	payload["message"] = "skill inspected successfully"
	return b.jsonResult(call, payload)
}

func handleSkillEnsure(ctx context.Context, b *Builtins, call agent.ToolCall) (contextengine.ToolResult, error) {
	query, _ := stringFrom(call.Input["query"])
	goal, _ := stringFrom(call.Input["goal"])
	requiredTools, err := stringSliceFrom(call.Input["required_tools"])
	if err != nil {
		return contextengine.ToolResult{}, err
	}

	requiredTools = cleanStringSlice(requiredTools)
	searchQueries := buildSkillEnsureQueries(strings.TrimSpace(query), strings.TrimSpace(goal), requiredTools)
	effectiveQuery := ""
	if len(searchQueries) > 0 {
		effectiveQuery = searchQueries[0]
	}
	if effectiveQuery == "" {
		return b.jsonResult(call, map[string]any{
			"success":        false,
			"resolved":       false,
			"installed":      false,
			"required_tools": requiredTools,
			"message":        "capability recovery requires goal, query, or required_tools",
		})
	}

	loadedByTool, loadedBySkill := loadedSkillState(b.skillService)
	if len(requiredTools) > 0 {
		missing := missingRequiredTools(requiredTools, loadedByTool)
		if len(missing) == 0 {
			return b.jsonResult(call, map[string]any{
				"success":        true,
				"resolved":       true,
				"installed":      false,
				"required_tools": requiredTools,
				"message":        "required capability is already available",
			})
		}
	}

	if b.clawHub == nil {
		return b.jsonResult(call, map[string]any{
			"success":        false,
			"resolved":       false,
			"installed":      false,
			"query":          effectiveQuery,
			"required_tools": requiredTools,
			"message":        "capability catalog not configured. Cannot search or install a missing capability package.",
		})
	}

	results, err := searchSkillEnsureCatalog(ctx, b.clawHub, searchQueries)
	if err != nil {
		return b.jsonResult(call, map[string]any{
			"success":        false,
			"resolved":       false,
			"installed":      false,
			"query":          effectiveQuery,
			"required_tools": requiredTools,
			"message":        fmt.Sprintf("capability search failed: %v", err),
		})
	}

	installedLocks, _ := b.clawHub.Installed()
	installedByID := make(map[string]skill.InstalledSkillLock, len(installedLocks))
	for _, entry := range installedLocks {
		installedByID[strings.ToLower(entry.SkillID)] = entry
	}

	candidates := rankSkillEnsureCandidates(results, effectiveQuery, requiredTools, installedByID, loadedBySkill)
	limit := b.config.SkillEnsureLimit
	if requested, err := intFrom(call.Input["limit"], limit); err == nil && requested > 0 {
		limit = requested
	}
	if limit > 0 && len(candidates) > limit {
		candidates = candidates[:limit]
	}

	if len(candidates) == 0 {
		// Provide actionable guidance when no skill is found
		fallbackHint := "Options: " +
			"1) Use exec.shell to run the command directly if the binary is installed. " +
			"2) Install the required binary manually (e.g., brew install <tool>, apt install <tool>). " +
			"3) Check if the capability can be achieved with existing built-in tools."
		return b.jsonResult(call, map[string]any{
			"success":        false,
			"resolved":       false,
			"installed":      false,
			"query":          effectiveQuery,
			"required_tools": requiredTools,
			"candidates":     []any{},
			"fallback_hint":  fallbackHint,
			"message":        "no matching capability package found in the catalog",
		})
	}

	selected := candidates[0]
	if selected.Installed {
		if b.skillService != nil {
			if _, refreshErr := b.skillService.Refresh(ctx); refreshErr != nil {
				return b.jsonResult(call, map[string]any{
					"success":        true,
					"resolved":       true,
					"installed":      false,
					"query":          effectiveQuery,
					"required_tools": requiredTools,
					"selected":       selected,
					"candidates":     candidates,
					"message":        fmt.Sprintf("capability package %q is already installed, but refresh failed: %v", selected.ID, refreshErr),
				})
			}
		}
		validation := inspectInstalledSkillValidation(b, selected.ID)
		if unresolved := unresolvedRequiredTools(requiredTools, b.skillService); len(unresolved) > 0 {
			payload := map[string]any{
				"success":          false,
				"resolved":         false,
				"installed":        false,
				"query":            effectiveQuery,
				"required_tools":   requiredTools,
				"missing_tools":    unresolved,
				"selected":         selected,
				"candidates":       candidates,
				"message":          fmt.Sprintf("capability package %q is installed, but required tools are still unavailable", selected.ID),
				"recovery_suggest": "refresh runtime context, check required env/config, or install the missing binary dependencies",
			}
			if validation != nil {
				payload["validation"] = validation
			}
			return b.jsonResult(call, payload)
		}
		payload := map[string]any{
			"success":        true,
			"resolved":       true,
			"installed":      false,
			"query":          effectiveQuery,
			"required_tools": requiredTools,
			"selected":       selected,
			"candidates":     candidates,
			"message":        fmt.Sprintf("required capability is ready via package %q", selected.ID),
		}
		if validation != nil {
			payload["validation"] = validation
		}
		return b.jsonResult(call, payload)
	}

	result, err := b.clawHub.Install(ctx, skill.InstallRequest{
		SkillID: selected.ID,
	})
	if err != nil {
		return b.jsonResult(call, map[string]any{
			"success":        false,
			"resolved":       false,
			"installed":      false,
			"query":          effectiveQuery,
			"required_tools": requiredTools,
			"selected":       selected,
			"candidates":     candidates,
			"message":        fmt.Sprintf("installation failed: %v", err),
		})
	}

	if b.skillService != nil {
		if _, refreshErr := b.skillService.Refresh(ctx); refreshErr != nil {
			return b.jsonResult(call, map[string]any{
				"success":         true,
				"resolved":        true,
				"installed":       true,
				"query":           effectiveQuery,
				"required_tools":  requiredTools,
				"selected":        selected,
				"candidates":      candidates,
				"name":            result.SkillID,
				"version":         result.Version,
				"install_dir":     result.InstallDir,
				"installer_steps": installStepPayloads(result.InstallerSteps),
				"message":         fmt.Sprintf("capability package installed successfully, but refresh failed: %v", refreshErr),
			})
		}
	}
	validation := inspectInstalledSkillValidation(b, result.SkillID)
	if unresolved := unresolvedRequiredTools(requiredTools, b.skillService); len(unresolved) > 0 {
		payload := map[string]any{
			"success":          false,
			"resolved":         false,
			"installed":        true,
			"query":            effectiveQuery,
			"required_tools":   requiredTools,
			"missing_tools":    unresolved,
			"selected":         selected,
			"candidates":       candidates,
			"name":             result.SkillID,
			"version":          result.Version,
			"install_dir":      result.InstallDir,
			"installer_steps":  installStepPayloads(result.InstallerSteps),
			"message":          fmt.Sprintf("capability package %q installed, but required tools are still unavailable", result.SkillID),
			"recovery_suggest": "refresh runtime context, check required env/config, or install the missing binary dependencies",
		}
		if validation != nil {
			payload["validation"] = validation
		}
		return b.jsonResult(call, payload)
	}

	payload := map[string]any{
		"success":         true,
		"resolved":        true,
		"installed":       true,
		"query":           effectiveQuery,
		"required_tools":  requiredTools,
		"selected":        selected,
		"candidates":      candidates,
		"name":            result.SkillID,
		"version":         result.Version,
		"install_dir":     result.InstallDir,
		"installer_steps": installStepPayloads(result.InstallerSteps),
		"message":         fmt.Sprintf("required capability is ready via package %q v%s", result.SkillID, result.Version),
	}
	if validation != nil {
		payload["validation"] = validation
	}
	return b.jsonResult(call, payload)
}

func handleSkillInstall(ctx context.Context, b *Builtins, call agent.ToolCall) (contextengine.ToolResult, error) {
	name, err := requiredString(call.Input, "name")
	if err != nil {
		return contextengine.ToolResult{}, err
	}
	version, _ := stringFrom(call.Input["version"])
	source, _ := stringFrom(call.Input["source"])

	if b.clawHub == nil {
		return b.jsonResult(call, map[string]any{
			"name":    name,
			"success": false,
			"message": "ClawHub client not configured. Configure skills.auto_detect or provide a ClawHub base URL.",
		})
	}

	var result *skill.InstallResult
	if source != "" {
		installer, ok := b.clawHub.(skill.LocalSourceInstaller)
		if !ok {
			return b.jsonResult(call, map[string]any{
				"name":    name,
				"success": false,
				"message": "local source install not supported by this ClawHub client",
			})
		}
		if _, statErr := os.Stat(source); statErr != nil {
			return b.jsonResult(call, map[string]any{
				"name":    name,
				"success": false,
				"message": fmt.Sprintf("source path not found: %v", statErr),
			})
		}
		result, err = installer.InstallFromSource(ctx, skill.InstallRequest{
			SkillID: name,
			Version: version,
		}, source)
	} else {
		result, err = b.clawHub.Install(ctx, skill.InstallRequest{
			SkillID: name,
			Version: version,
		})
	}
	if err != nil {
		return b.jsonResult(call, map[string]any{
			"name":    name,
			"success": false,
			"message": fmt.Sprintf("installation failed: %v", err),
		})
	}

	// Refresh skill service to pick up the newly installed skill.
	if b.skillService != nil {
		if _, refreshErr := b.skillService.Refresh(ctx); refreshErr != nil {
			return b.jsonResult(call, map[string]any{
				"name":            result.SkillID,
				"version":         result.Version,
				"install_dir":     result.InstallDir,
				"installer_steps": installStepPayloads(result.InstallerSteps),
				"success":         true,
				"message":         fmt.Sprintf("installed successfully, but refresh failed: %v", refreshErr),
			})
		}
	}
	validation := inspectInstalledSkillValidation(b, result.SkillID)

	payload := map[string]any{
		"name":            result.SkillID,
		"version":         result.Version,
		"install_dir":     result.InstallDir,
		"installer_steps": installStepPayloads(result.InstallerSteps),
		"success":         true,
		"message":         fmt.Sprintf("skill %q v%s installed successfully", result.SkillID, result.Version),
	}
	if validation != nil {
		payload["validation"] = validation
	}
	return b.jsonResult(call, payload)
}

func handleSkillRemove(ctx context.Context, b *Builtins, call agent.ToolCall) (contextengine.ToolResult, error) {
	name, err := requiredString(call.Input, "name")
	if err != nil {
		return contextengine.ToolResult{}, err
	}

	if b.clawHub == nil {
		return b.jsonResult(call, map[string]any{
			"name":    name,
			"success": false,
			"message": "ClawHub client not configured.",
		})
	}

	if err := b.clawHub.Remove(name); err != nil {
		return b.jsonResult(call, map[string]any{
			"name":    name,
			"success": false,
			"message": fmt.Sprintf("removal failed: %v", err),
		})
	}

	// Refresh skill service to drop the removed skill.
	if b.skillService != nil {
		if _, err := b.skillService.Refresh(ctx); err != nil {
			log.Warn("refresh skill service failed", "error", err)
		}
	}

	return b.jsonResult(call, map[string]any{
		"name":    name,
		"success": true,
		"message": fmt.Sprintf("skill %q removed successfully", name),
	})
}

// ---------------------------------------------------------------------------
// skill.publish
// ---------------------------------------------------------------------------

func handleSkillPublish(ctx context.Context, b *Builtins, call agent.ToolCall) (contextengine.ToolResult, error) {
	hub := b.clawHub
	if hub == nil {
		return contextengine.ToolResult{}, fmt.Errorf("skill.publish: clawhub client not configured")
	}

	skillDir, err := requiredString(call.Input, "skill_dir")
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("skill.publish: %w", err)
	}
	slug, err := requiredString(call.Input, "slug")
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("skill.publish: %w", err)
	}
	version, err := requiredString(call.Input, "version")
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("skill.publish: %w", err)
	}

	changelog := ""
	if v, ok := call.Input["changelog"]; ok {
		if s, ok := v.(string); ok {
			changelog = s
		}
	}

	absDir, err := filepath.Abs(skillDir)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("skill.publish: %w", err)
	}

	result, err := hub.Publish(ctx, skill.PublishRequest{
		SkillDir:  absDir,
		Slug:      slug,
		Version:   version,
		Changelog: changelog,
	})
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("skill.publish: %w", err)
	}

	return b.jsonResult(call, map[string]any{
		"ok":      true,
		"slug":    result.Slug,
		"version": result.Version,
		"url":     result.URL,
	})
}

// ---------------------------------------------------------------------------
// Input schemas
// ---------------------------------------------------------------------------

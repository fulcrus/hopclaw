package cli

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/fulcrus/hopclaw/config"
	pluginpkg "github.com/fulcrus/hopclaw/plugin"
	sdkplugin "github.com/fulcrus/hopclaw/sdk/plugin"
)

type doctorSkillSummary struct {
	ID       string `json:"id,omitempty"`
	Name     string `json:"name"`
	Ready    bool   `json:"ready"`
	Eligible bool   `json:"eligible"`
}

type doctorSkillsListResponse struct {
	Items []doctorSkillSummary `json:"items"`
	Count int                  `json:"count"`
}

type doctorSkillDependencyCheck struct {
	Name       string   `json:"name,omitempty"`
	Candidates []string `json:"candidates,omitempty"`
	Status     string   `json:"status"`
	Hint       string   `json:"hint,omitempty"`
}

type doctorSkillDetailResponse struct {
	Name        string                       `json:"name"`
	Ready       bool                         `json:"ready"`
	Eligible    bool                         `json:"eligible"`
	Checks      []doctorSkillDependencyCheck `json:"checks"`
	NextActions []string                     `json:"next_actions,omitempty"`
}

func checkSkillDirectory() checkResult {
	p := resolveConfigPath()
	if p == "" {
		return checkResult{
			Category: "skills",
			Name:     "Skill directory",
			Status:   "ok",
			Detail:   "no config file; skipped",
		}
	}

	cfg, err := config.Load(p)
	if err != nil {
		return checkResult{
			Category: "skills",
			Name:     "Skill directory",
			Status:   "warn",
			Detail:   fmt.Sprintf("cannot load config: %v", err),
		}
	}

	if len(cfg.Skills.Dirs) == 0 {
		return checkResult{
			Category: "skills",
			Name:     "Skill directory",
			Status:   "ok",
			Detail:   "no explicit skill dirs configured",
		}
	}

	var missing []string
	for _, dir := range cfg.Skills.Dirs {
		if _, err := os.Stat(dir); err != nil {
			missing = append(missing, dir)
		}
	}

	if len(missing) > 0 {
		return checkResult{
			Category: "skills",
			Name:     "Skill directory",
			Status:   "warn",
			Detail:   fmt.Sprintf("missing directories: %s", strings.Join(missing, ", ")),
			Fix:      "create the missing directories or update skills.dirs in config",
		}
	}

	return checkResult{
		Category: "skills",
		Name:     "Skill directory",
		Status:   "ok",
		Detail:   fmt.Sprintf("%d directory(ies) accessible", len(cfg.Skills.Dirs)),
	}
}

func checkSkillDependencies() checkResult {
	client, err := NewGatewayClient()
	if err != nil {
		return checkResult{
			Category: "skills",
			Name:     "Skill dependencies",
			Status:   "warn",
			Detail:   fmt.Sprintf("cannot create gateway client: %v", err),
			Fix:      "start the gateway and rerun 'hopclaw doctor skills' to validate skill binaries and env vars",
		}
	}
	client.HTTP.Timeout = doctorValidateTimeout

	ctx, cancel := context.WithTimeout(context.Background(), doctorValidateTimeout)
	defer cancel()

	var response doctorSkillsListResponse
	if err := client.Get(ctx, "/operator/skills", &response); err != nil {
		return checkResult{
			Category: "skills",
			Name:     "Skill dependencies",
			Status:   "warn",
			Detail:   "gateway unavailable; unable to inspect installed skill dependencies",
			Fix:      "start the gateway and rerun 'hopclaw doctor skills' to validate skill binaries and env vars",
		}
	}
	if len(response.Items) == 0 {
		return checkResult{
			Category: "skills",
			Name:     "Skill dependencies",
			Status:   "ok",
			Detail:   "no skills discovered",
		}
	}

	readyCount := 0
	degradedSkills := make([]string, 0)
	missingDeps := make([]string, 0)
	fixHints := make([]string, 0)
	seenSkills := make(map[string]struct{}, len(response.Items))
	seenDeps := make(map[string]struct{})
	seenHints := make(map[string]struct{})

	appendUnique := func(target []string, seen map[string]struct{}, value string) []string {
		value = strings.TrimSpace(value)
		if value == "" {
			return target
		}
		if _, ok := seen[value]; ok {
			return target
		}
		seen[value] = struct{}{}
		return append(target, value)
	}

	for _, item := range response.Items {
		name := strings.TrimSpace(item.Name)
		if name == "" {
			name = strings.TrimSpace(item.ID)
		}
		if name == "" {
			continue
		}
		if item.Ready {
			readyCount++
			continue
		}
		if _, ok := seenSkills[name]; !ok {
			seenSkills[name] = struct{}{}
			degradedSkills = append(degradedSkills, name)
		}

		detailCtx, detailCancel := context.WithTimeout(context.Background(), doctorValidateTimeout)
		var detail doctorSkillDetailResponse
		err := client.Get(detailCtx, "/operator/skills/"+url.PathEscape(name), &detail)
		detailCancel()
		if err != nil {
			fixHints = appendUnique(fixHints, seenHints, "run 'hopclaw skills info "+name+"' to inspect missing dependencies")
			continue
		}
		for _, check := range detail.Checks {
			switch strings.TrimSpace(strings.ToLower(check.Status)) {
			case "missing", "unsupported", "disabled":
				dependency := strings.TrimSpace(check.Name)
				if dependency == "" && len(check.Candidates) > 0 {
					dependency = strings.Join(check.Candidates, "/")
				}
				missingDeps = appendUnique(missingDeps, seenDeps, dependency)
				fixHints = appendUnique(fixHints, seenHints, check.Hint)
			}
		}
		for _, action := range detail.NextActions {
			fixHints = appendUnique(fixHints, seenHints, action)
		}
	}

	sort.Strings(degradedSkills)
	sort.Strings(missingDeps)
	sort.Strings(fixHints)

	if len(degradedSkills) == 0 {
		return checkResult{
			Category: "skills",
			Name:     "Skill dependencies",
			Status:   "ok",
			Detail:   fmt.Sprintf("%d/%d skill(s) ready", readyCount, len(response.Items)),
		}
	}

	status := "warn"
	if readyCount == 0 {
		status = "fail"
	}

	detail := fmt.Sprintf("%d/%d skill(s) ready", readyCount, len(response.Items))
	switch {
	case len(missingDeps) > 4:
		detail += fmt.Sprintf(" (missing: %s, +%d more)", strings.Join(missingDeps[:4], ", "), len(missingDeps)-4)
	case len(missingDeps) > 0:
		detail += fmt.Sprintf(" (missing: %s)", strings.Join(missingDeps, ", "))
	case len(degradedSkills) > 4:
		detail += fmt.Sprintf(" (attention: %s, +%d more)", strings.Join(degradedSkills[:4], ", "), len(degradedSkills)-4)
	default:
		detail += fmt.Sprintf(" (attention: %s)", strings.Join(degradedSkills, ", "))
	}

	fix := "install the missing binaries/env vars or inspect the affected skills with 'hopclaw skills info <name>'"
	if len(fixHints) > 0 {
		if len(fixHints) > 2 {
			fix = strings.Join(fixHints[:2], "; ")
		} else {
			fix = strings.Join(fixHints, "; ")
		}
	}

	return checkResult{
		Category: "skills",
		Name:     "Skill dependencies",
		Status:   status,
		Detail:   detail,
		Fix:      fix,
	}
}

func checkInstalledPlugins() checkResult {
	roots := doctorPluginRoots()
	if len(roots) == 0 {
		return checkResult{
			Category: "skills",
			Name:     "Plugin manifests",
			Status:   "ok",
			Detail:   "no plugin roots discovered",
		}
	}

	candidates := doctorPluginCandidateDirs(roots)
	if len(candidates) == 0 {
		return checkResult{
			Category: "skills",
			Name:     "Plugin manifests",
			Status:   "ok",
			Detail:   "no installed plugins found",
		}
	}

	var valid []string
	var invalid []string
	for _, dir := range candidates {
		loaded, err := pluginpkg.Load(dir)
		if err != nil {
			invalid = append(invalid, filepath.Base(dir)+": "+err.Error())
			continue
		}
		if errs := sdkplugin.ValidateManifest(loaded.Manifest); len(errs) > 0 {
			invalid = append(invalid, loaded.Manifest.Name+": "+errs[0].Error())
			continue
		}
		valid = append(valid, loaded.Manifest.Name)
	}

	sort.Strings(valid)
	sort.Strings(invalid)
	if len(invalid) == 0 {
		return checkResult{
			Category: "skills",
			Name:     "Plugin manifests",
			Status:   "ok",
			Detail:   fmt.Sprintf("%d plugin manifest(s) valid", len(valid)),
		}
	}

	status := "warn"
	if len(valid) == 0 {
		status = "fail"
	}
	return checkResult{
		Category: "skills",
		Name:     "Plugin manifests",
		Status:   status,
		Detail:   fmt.Sprintf("%d valid, %d invalid: %s", len(valid), len(invalid), strings.Join(invalid, ", ")),
		Fix:      "run 'hopclaw plugins validate <path>' for the failing plugin manifests",
	}
}

func doctorPluginRoots() []string {
	cwd, err := os.Getwd()
	if err != nil {
		return nil
	}
	return uniqueNonEmptyStrings(pluginpkg.DefaultPluginDirs(cwd))
}

func doctorPluginCandidateDirs(roots []string) []string {
	const (
		hopclawManifest  = "hopclaw.plugin.yaml"
		openclawManifest = "openclaw.plugin.json"
	)

	seen := make(map[string]struct{})
	var dirs []string
	for _, root := range roots {
		root = strings.TrimSpace(root)
		if root == "" {
			continue
		}
		if doctorHasPluginManifest(root, hopclawManifest, openclawManifest) {
			if abs, err := filepath.Abs(root); err == nil {
				if _, ok := seen[abs]; !ok {
					seen[abs] = struct{}{}
					dirs = append(dirs, abs)
				}
			}
		}

		entries, err := os.ReadDir(root)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			subdir := filepath.Join(root, entry.Name())
			if !entry.IsDir() {
				info, statErr := os.Stat(subdir)
				if statErr != nil || !info.IsDir() {
					continue
				}
			}
			if !doctorHasPluginManifest(subdir, hopclawManifest, openclawManifest) {
				continue
			}
			abs, err := filepath.Abs(subdir)
			if err != nil {
				continue
			}
			if _, ok := seen[abs]; ok {
				continue
			}
			seen[abs] = struct{}{}
			dirs = append(dirs, abs)
		}
	}
	sort.Strings(dirs)
	return dirs
}

func doctorHasPluginManifest(dir string, names ...string) bool {
	for _, name := range names {
		info, err := os.Stat(filepath.Join(dir, name))
		if err == nil && !info.IsDir() {
			return true
		}
	}
	return false
}

func uniqueNonEmptyStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		out = append(out, trimmed)
	}
	sort.Strings(out)
	return out
}

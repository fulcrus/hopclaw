package skill

import (
	"os/exec"
	"runtime"
	"slices"
	"sort"
	"strings"
)

type Evaluator struct {
	LookPath       func(file string) (string, error)
	SecretPresence func(keys []string) map[string]SecretStatus
}

func (e Evaluator) EnrichRuntimeContext(ctx RuntimeContext, pkgs []*SkillPackage) RuntimeContext {
	if e.SecretPresence == nil || len(pkgs) == 0 {
		return ctx
	}
	keys := collectRequiredEnvKeys(pkgs)
	if len(keys) == 0 {
		return ctx
	}
	missing := make([]string, 0, len(keys))
	for _, key := range keys {
		if _, ok := ctx.SecretPresence[key]; ok {
			continue
		}
		missing = append(missing, key)
	}
	if len(missing) == 0 {
		return ctx
	}
	statuses := e.SecretPresence(missing)
	if len(statuses) == 0 {
		return ctx
	}
	if ctx.SecretPresence == nil {
		ctx.SecretPresence = make(map[string]SecretStatus, len(statuses))
	}
	for key, status := range statuses {
		ctx.SecretPresence[key] = status
	}
	return ctx
}

func (e Evaluator) Evaluate(pkg *SkillPackage, ctx RuntimeContext) EligibilityResult {
	result := EligibilityResult{
		Eligible:    true,
		InjectedEnv: make([]string, 0),
		Checks:      make([]DependencyCheck, 0),
	}

	if ctx.GOOS == "" {
		ctx.GOOS = runtime.GOOS
	}
	lookup := e.LookPath
	if lookup == nil {
		lookup = exec.LookPath
	}
	entry := ctx.Managed[pkg.ConfigKey()]
	if entry.Enabled != nil && !*entry.Enabled {
		result.Eligible = false
		result.Reasons = append(result.Reasons, "disabled by managed config")
		result.Checks = append(result.Checks, DependencyCheck{
			Kind:    DependencyCheckManagedEnabled,
			Name:    pkg.ConfigKey(),
			Status:  DependencyStatusDisabled,
			Present: false,
			Source:  "managed",
			Message: "skill disabled by managed config",
			Hint:    "set skills.managed." + pkg.ConfigKey() + ".enabled to true",
		})
	} else if entry.Enabled != nil && *entry.Enabled {
		result.Checks = append(result.Checks, DependencyCheck{
			Kind:    DependencyCheckManagedEnabled,
			Name:    pkg.ConfigKey(),
			Status:  DependencyStatusSatisfied,
			Present: true,
			Source:  "managed",
			Message: "skill enabled by managed config",
		})
	}

	if len(pkg.OpenClaw.OS) > 0 {
		if !slices.Contains(pkg.OpenClaw.OS, ctx.GOOS) {
			result.Eligible = false
			result.Reasons = append(result.Reasons, "unsupported OS")
			result.Checks = append(result.Checks, DependencyCheck{
				Kind:       DependencyCheckOS,
				Name:       ctx.GOOS,
				Candidates: append([]string(nil), pkg.OpenClaw.OS...),
				Status:     DependencyStatusUnsupported,
				Present:    false,
				Message:    "skill does not support current OS",
				Hint:       "run this skill on one of: " + strings.Join(pkg.OpenClaw.OS, ", "),
			})
		} else {
			result.Checks = append(result.Checks, DependencyCheck{
				Kind:       DependencyCheckOS,
				Name:       ctx.GOOS,
				Candidates: append([]string(nil), pkg.OpenClaw.OS...),
				Status:     DependencyStatusSatisfied,
				Present:    true,
				Message:    "skill supports current OS",
			})
		}
	}

	injectedEnv := make(map[string]bool)
	for key, status := range entry.InjectedEnv {
		if !status.Resolved {
			continue
		}
		injectedEnv[key] = true
		result.InjectedEnv = append(result.InjectedEnv, key)
	}
	sort.Strings(result.InjectedEnv)

	if pkg.OpenClaw.Always {
		result.Always = true
		return result
	}

	for _, bin := range pkg.OpenClaw.Requires.Bins {
		if path, err := lookup(bin); err != nil {
			result.Eligible = false
			result.Reasons = append(result.Reasons, "missing binary: "+bin)
			result.Checks = append(result.Checks, DependencyCheck{
				Kind:    DependencyCheckBinary,
				Name:    bin,
				Status:  DependencyStatusMissing,
				Present: false,
				Message: "missing required binary",
				Hint:    installHintForBinary(pkg, bin),
			})
		} else {
			result.Checks = append(result.Checks, DependencyCheck{
				Kind:    DependencyCheckBinary,
				Name:    bin,
				Status:  DependencyStatusSatisfied,
				Present: true,
				Path:    path,
				Message: "required binary is available",
			})
		}
	}

	if anyBins := pkg.OpenClaw.Requires.AnyBins; len(anyBins) > 0 {
		found := false
		resolved := ""
		for _, bin := range anyBins {
			if path, err := lookup(bin); err == nil {
				found = true
				resolved = path
				break
			}
		}
		if !found {
			result.Eligible = false
			result.Reasons = append(result.Reasons, "missing any required binary")
			result.Checks = append(result.Checks, DependencyCheck{
				Kind:       DependencyCheckAnyBinary,
				Candidates: append([]string(nil), anyBins...),
				Status:     DependencyStatusMissing,
				Present:    false,
				Message:    "none of the acceptable binaries are available",
				Hint:       "install one of: " + strings.Join(anyBins, ", "),
			})
		} else {
			result.Checks = append(result.Checks, DependencyCheck{
				Kind:       DependencyCheckAnyBinary,
				Candidates: append([]string(nil), anyBins...),
				Status:     DependencyStatusSatisfied,
				Present:    true,
				Path:       resolved,
				Message:    "an acceptable binary is available",
			})
		}
	}

	for _, envKey := range pkg.OpenClaw.Requires.Env {
		source := ""
		status := DependencyStatusSatisfied
		message := "required environment variable is available"
		hint := ""
		if injectedEnv[envKey] {
			source = "managed"
			status = DependencyStatusInjected
			message = "required environment variable will be injected by managed config"
		} else if secretStatus, ok := ctx.SecretPresence[envKey]; ok && secretStatus.Resolved {
			source = normalizeDependencySource(secretStatus.Source, "runtime_env")
		} else {
			result.Eligible = false
			result.Reasons = append(result.Reasons, "missing env: "+envKey)
			status = DependencyStatusMissing
			message = "required environment variable is missing"
			if pkg.OpenClaw.PrimaryEnv == envKey {
				hint = "set env " + envKey + " or configure a managed integration that injects it"
			} else {
				hint = "set env " + envKey
			}
		}
		result.Checks = append(result.Checks, DependencyCheck{
			Kind:    DependencyCheckEnv,
			Name:    envKey,
			Status:  status,
			Present: status != DependencyStatusMissing,
			Source:  source,
			Message: message,
			Hint:    hint,
		})
	}

	for _, path := range pkg.OpenClaw.Requires.Config {
		source := ""
		status := DependencyStatusSatisfied
		message := "required config value is available"
		hint := ""
		if managedStatus, ok := entry.ConfigTruth[path]; ok && managedStatus.Present {
			source = normalizeDependencySource(managedStatus.Source, "managed")
			if !managedStatus.Truthy {
				result.Eligible = false
				result.Reasons = append(result.Reasons, "falsey config: "+path)
				status = DependencyStatusMissing
				message = "required config value is present but falsey"
				hint = "set skills.config." + pkg.ConfigKey() + "." + path + " to a truthy value"
			}
		} else if runtimeStatus, ok := ctx.ConfigTruth[path]; ok && runtimeStatus.Present {
			source = normalizeDependencySource(runtimeStatus.Source, "runtime_config")
			if !runtimeStatus.Truthy {
				result.Eligible = false
				result.Reasons = append(result.Reasons, "falsey config: "+path)
				status = DependencyStatusMissing
				message = "required config value is present but falsey"
				hint = "set " + path + " to a truthy value"
			}
		} else {
			result.Eligible = false
			result.Reasons = append(result.Reasons, "missing config: "+path)
			status = DependencyStatusMissing
			message = "required config value is missing"
			hint = "set skills.config." + pkg.ConfigKey() + "." + path
		}
		result.Checks = append(result.Checks, DependencyCheck{
			Kind:    DependencyCheckConfig,
			Name:    path,
			Status:  status,
			Present: status != DependencyStatusMissing,
			Source:  source,
			Message: message,
			Hint:    hint,
		})
	}

	return result
}

func normalizeDependencySource(source, fallback string) string {
	trimmed := strings.TrimSpace(source)
	if trimmed == "" {
		return fallback
	}
	return trimmed
}

func collectRequiredEnvKeys(pkgs []*SkillPackage) []string {
	if len(pkgs) == 0 {
		return nil
	}
	seen := make(map[string]bool)
	keys := make([]string, 0)
	for _, pkg := range pkgs {
		if pkg == nil {
			continue
		}
		for _, key := range pkg.OpenClaw.Requires.Env {
			trimmed := strings.TrimSpace(key)
			if trimmed == "" || seen[trimmed] {
				continue
			}
			seen[trimmed] = true
			keys = append(keys, trimmed)
		}
	}
	sort.Strings(keys)
	return keys
}

func truthyAtPath(root map[string]any, path string) bool {
	if len(root) == 0 || path == "" {
		return false
	}
	current := any(root)
	for _, part := range strings.Split(path, ".") {
		node, ok := current.(map[string]any)
		if !ok {
			return false
		}
		current, ok = node[part]
		if !ok {
			return false
		}
	}
	switch typed := current.(type) {
	case nil:
		return false
	case bool:
		return typed
	case string:
		return typed != ""
	case int:
		return typed != 0
	case int64:
		return typed != 0
	case float64:
		return typed != 0
	default:
		return true
	}
}

package skill

import (
	"context"
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"github.com/fulcrus/hopclaw/internal/support/normalize"
)

type DependencyCheckKind string

const (
	DependencyCheckManagedEnabled DependencyCheckKind = "managed_enabled"
	DependencyCheckOS             DependencyCheckKind = "os"
	DependencyCheckBinary         DependencyCheckKind = "binary"
	DependencyCheckAnyBinary      DependencyCheckKind = "any_binary"
	DependencyCheckEnv            DependencyCheckKind = "env"
	DependencyCheckConfig         DependencyCheckKind = "config"
)

type DependencyCheckStatus string

const (
	DependencyStatusSatisfied   DependencyCheckStatus = "satisfied"
	DependencyStatusMissing     DependencyCheckStatus = "missing"
	DependencyStatusUnsupported DependencyCheckStatus = "unsupported"
	DependencyStatusDisabled    DependencyCheckStatus = "disabled"
	DependencyStatusInjected    DependencyCheckStatus = "injected"
)

type DependencyCheck struct {
	Kind       DependencyCheckKind   `json:"kind"`
	Name       string                `json:"name,omitempty"`
	Candidates []string              `json:"candidates,omitempty"`
	Status     DependencyCheckStatus `json:"status"`
	Present    bool                  `json:"present"`
	Source     string                `json:"source,omitempty"`
	Path       string                `json:"path,omitempty"`
	Message    string                `json:"message,omitempty"`
	Hint       string                `json:"hint,omitempty"`
}

type SkillIssueReport struct {
	Severity IssueSeverity `json:"severity"`
	Code     string        `json:"code,omitempty"`
	Message  string        `json:"message"`
}

type SkillRuntimeToolReport struct {
	Name             string   `json:"name"`
	Aliases          []string `json:"aliases,omitempty"`
	Description      string   `json:"description,omitempty"`
	SideEffectClass  string   `json:"side_effect_class,omitempty"`
	Idempotent       bool     `json:"idempotent"`
	RequiresApproval bool     `json:"requires_approval"`
	ExecutionKey     string   `json:"execution_key,omitempty"`
	RuntimeEntry     string   `json:"runtime_entry,omitempty"`
	RuntimeShell     string   `json:"runtime_shell,omitempty"`
	Timeout          string   `json:"timeout,omitempty"`
}

type SkillInstallHint struct {
	ID              string   `json:"id,omitempty"`
	Kind            string   `json:"kind,omitempty"`
	Label           string   `json:"label,omitempty"`
	OS              []string `json:"os,omitempty"`
	Bins            []string `json:"bins,omitempty"`
	Formula         string   `json:"formula,omitempty"`
	Package         string   `json:"package,omitempty"`
	Module          string   `json:"module,omitempty"`
	URL             string   `json:"url,omitempty"`
	Archive         string   `json:"archive,omitempty"`
	TargetDir       string   `json:"target_dir,omitempty"`
	StripComponents int      `json:"strip_components,omitempty"`
}

type SkillRuntimeReport struct {
	Found            bool                     `json:"found"`
	Loaded           bool                     `json:"loaded"`
	Blocked          bool                     `json:"blocked,omitempty"`
	Installed        bool                     `json:"installed,omitempty"`
	Name             string                   `json:"name,omitempty"`
	SkillID          string                   `json:"skill_id,omitempty"`
	ConfigKey        string                   `json:"config_key,omitempty"`
	Description      string                   `json:"description,omitempty"`
	Homepage         string                   `json:"homepage,omitempty"`
	Location         string                   `json:"location,omitempty"`
	Kind             SkillKind                `json:"kind,omitempty"`
	Status           PackageStatus            `json:"status,omitempty"`
	Trust            TrustClass               `json:"trust,omitempty"`
	SourceKind       SourceKind               `json:"source_kind,omitempty"`
	SourceRoot       string                   `json:"source_root,omitempty"`
	SourceDir        string                   `json:"source_dir,omitempty"`
	SourceNameHint   string                   `json:"source_name_hint,omitempty"`
	SourcePriority   int                      `json:"source_priority,omitempty"`
	InstalledVersion string                   `json:"installed_version,omitempty"`
	InstallDir       string                   `json:"install_dir,omitempty"`
	BundleDir        string                   `json:"bundle_dir,omitempty"`
	Pinned           bool                     `json:"pinned,omitempty"`
	Eligible         bool                     `json:"eligible"`
	Ready            bool                     `json:"ready"`
	Always           bool                     `json:"always,omitempty"`
	Reasons          []string                 `json:"reasons,omitempty"`
	Checks           []DependencyCheck        `json:"checks,omitempty"`
	InjectedEnv      []string                 `json:"injected_env,omitempty"`
	Tools            []SkillRuntimeToolReport `json:"tools,omitempty"`
	InstallHints     []SkillInstallHint       `json:"install_hints,omitempty"`
	Issues           []SkillIssueReport       `json:"issues,omitempty"`
	NextActions      []string                 `json:"next_actions,omitempty"`
}

func BuildRuntimeReport(pkg *SkillPackage, runtimeCtx RuntimeContext, evaluator Evaluator) SkillRuntimeReport {
	eligibility := evaluator.Evaluate(pkg, runtimeCtx)
	report := SkillRuntimeReport{
		Found:          true,
		Loaded:         true,
		Name:           pkg.Name(),
		SkillID:        pkg.ID,
		ConfigKey:      pkg.ConfigKey(),
		Description:    pkg.Prompt.Description,
		Homepage:       pkg.Prompt.Homepage,
		Location:       pkg.Prompt.Location,
		Kind:           pkg.Kind,
		Status:         pkg.Status,
		Trust:          pkg.Trust,
		SourceKind:     pkg.Source.Kind,
		SourceRoot:     pkg.Source.Root,
		SourceDir:      pkg.Source.Dir,
		SourceNameHint: pkg.Source.NameHint,
		SourcePriority: pkg.Source.Priority,
		Eligible:       eligibility.Eligible,
		Ready:          eligibility.Eligible && pkg.Status != StatusBlocked,
		Always:         eligibility.Always,
		Reasons:        append([]string(nil), eligibility.Reasons...),
		Checks:         append([]DependencyCheck(nil), eligibility.Checks...),
		Tools:          buildToolReports(pkg.ToolManifests),
		InstallHints:   buildInstallHints(pkg.OpenClaw.Install),
		Issues:         buildIssueReports(pkg.Issues),
		NextActions:    buildNextActions(pkg, eligibility),
	}
	report.InjectedEnv = append([]string(nil), eligibility.InjectedEnv...)
	return report
}

func BuildBlockedRuntimeReport(blocked BlockedSkill) SkillRuntimeReport {
	report := SkillRuntimeReport{
		Found:          true,
		Loaded:         false,
		Blocked:        true,
		Name:           blocked.NameHint,
		Status:         StatusBlocked,
		SourceKind:     blocked.Source.Kind,
		SourceRoot:     blocked.Source.Root,
		SourceDir:      blocked.Source.Dir,
		SourceNameHint: blocked.Source.NameHint,
		SourcePriority: blocked.Source.Priority,
		Eligible:       false,
		Ready:          false,
		Issues:         buildIssueReports(blocked.Issues),
	}
	report.NextActions = buildBlockedNextActions(blocked)
	return report
}

func InspectSource(ctx context.Context, src SkillSource, compiler Compiler, evaluator Evaluator, runtimeCtx RuntimeContext) (SkillRuntimeReport, error) {
	spec, err := ParseDir(src.Dir)
	if err != nil {
		return SkillRuntimeReport{}, err
	}
	if compiler == nil {
		compiler = DefaultCompiler{}
	}
	pkg, err := compiler.Compile(ctx, src, spec)
	if err != nil {
		return SkillRuntimeReport{}, err
	}
	report := BuildRuntimeReport(pkg, runtimeCtx, evaluator)
	report.Loaded = false
	report.SourceKind = src.Kind
	report.SourceRoot = src.Root
	report.SourceDir = src.Dir
	report.SourceNameHint = src.NameHint
	report.SourcePriority = src.Priority
	return report, nil
}

func FindRuntimeReport(snapshot RegistrySnapshot, ref string, runtimeCtx RuntimeContext, evaluator Evaluator) (SkillRuntimeReport, bool) {
	if pkg, ok := FindPackage(snapshot, ref); ok {
		return BuildRuntimeReport(pkg, runtimeCtx, evaluator), true
	}
	if blocked, ok := FindBlockedSkill(snapshot, ref); ok {
		return BuildBlockedRuntimeReport(blocked), true
	}
	return SkillRuntimeReport{}, false
}

func FindPackage(snapshot RegistrySnapshot, ref string) (*SkillPackage, bool) {
	needle := strings.TrimSpace(ref)
	if needle == "" {
		return nil, false
	}
	for _, pkg := range snapshot.Ordered {
		if matchesSkillReference(pkg, needle) {
			return pkg, true
		}
	}
	return nil, false
}

func FindBlockedSkill(snapshot RegistrySnapshot, ref string) (BlockedSkill, bool) {
	needle := strings.TrimSpace(ref)
	if needle == "" {
		return BlockedSkill{}, false
	}
	for _, blocked := range snapshot.Blocked {
		if matchesBlockedReference(blocked, needle) {
			return blocked, true
		}
	}
	return BlockedSkill{}, false
}

func ApplyInstalledLock(report *SkillRuntimeReport, lock InstalledSkillLock) {
	report.Installed = true
	report.InstalledVersion = lock.Version
	report.InstallDir = lock.InstallDir
	report.BundleDir = lock.BundleDir
	report.Pinned = lock.Pinned
}

func buildIssueReports(issues []SkillIssue) []SkillIssueReport {
	if len(issues) == 0 {
		return nil
	}
	out := make([]SkillIssueReport, 0, len(issues))
	for _, issue := range issues {
		out = append(out, SkillIssueReport(issue))
	}
	return out
}

func buildToolReports(manifests []ToolManifest) []SkillRuntimeToolReport {
	if len(manifests) == 0 {
		return nil
	}
	out := make([]SkillRuntimeToolReport, 0, len(manifests))
	for _, manifest := range manifests {
		item := SkillRuntimeToolReport{
			Name:             manifest.Name,
			Aliases:          append([]string(nil), manifest.Aliases...),
			Description:      manifest.Description,
			SideEffectClass:  manifest.SideEffectClass,
			Idempotent:       manifest.Idempotent,
			RequiresApproval: manifest.RequiresApproval,
			ExecutionKey:     manifest.ExecutionKey,
			RuntimeEntry:     manifest.Runtime.Entry,
			RuntimeShell:     manifest.Runtime.Shell,
		}
		if manifest.Timeout > 0 {
			item.Timeout = manifest.Timeout.String()
		}
		out = append(out, item)
	}
	return out
}

func buildInstallHints(specs []InstallSpec) []SkillInstallHint {
	if len(specs) == 0 {
		return nil
	}
	out := make([]SkillInstallHint, 0, len(specs))
	for idx, spec := range specs {
		out = append(out, SkillInstallHint{
			ID:              spec.ResolvedID(idx),
			Kind:            spec.ResolvedKind(),
			Label:           strings.TrimSpace(spec.Label),
			OS:              append([]string(nil), spec.OS...),
			Bins:            append([]string(nil), spec.Bins...),
			Formula:         strings.TrimSpace(spec.Formula),
			Package:         strings.TrimSpace(spec.Package),
			Module:          strings.TrimSpace(spec.Module),
			URL:             strings.TrimSpace(spec.URL),
			Archive:         strings.TrimSpace(spec.Archive),
			TargetDir:       strings.TrimSpace(spec.TargetDir),
			StripComponents: spec.StripComponents,
		})
	}
	return out
}

func buildNextActions(pkg *SkillPackage, eligibility EligibilityResult) []string {
	actions := make([]string, 0)
	for _, check := range eligibility.Checks {
		switch check.Status {
		case DependencyStatusMissing, DependencyStatusDisabled, DependencyStatusUnsupported:
			if hint := strings.TrimSpace(check.Hint); hint != "" {
				actions = append(actions, hint)
			}
		}
	}
	for _, issue := range pkg.Issues {
		if issue.Severity != SeverityError {
			continue
		}
		actions = append(actions, issue.Message)
	}
	if len(actions) == 0 && pkg.Trust == TrustCommunity && len(pkg.ToolManifests) > 0 {
		actions = append(actions, "review this community skill before enabling it in production")
	}
	return normalize.DedupeStrings(actions)
}

func buildBlockedNextActions(blocked BlockedSkill) []string {
	actions := make([]string, 0, len(blocked.Issues))
	for _, issue := range blocked.Issues {
		actions = append(actions, issue.Message)
	}
	return normalize.DedupeStrings(actions)
}

func matchesSkillReference(pkg *SkillPackage, ref string) bool {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return false
	}
	candidates := []string{
		pkg.Name(),
		pkg.ID,
		pkg.ConfigKey(),
		pkg.Source.NameHint,
		filepath.Base(pkg.Source.Dir),
		filepath.Base(filepath.Dir(pkg.Source.Dir)),
	}
	for _, candidate := range candidates {
		if strings.EqualFold(strings.TrimSpace(candidate), ref) {
			return true
		}
	}
	return false
}

func matchesBlockedReference(blocked BlockedSkill, ref string) bool {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return false
	}
	candidates := []string{
		blocked.NameHint,
		blocked.Source.NameHint,
		filepath.Base(blocked.Source.Dir),
		filepath.Base(filepath.Dir(blocked.Source.Dir)),
	}
	for _, candidate := range candidates {
		if strings.EqualFold(strings.TrimSpace(candidate), ref) {
			return true
		}
	}
	return false
}

func installHintForBinary(pkg *SkillPackage, bin string) string {
	bin = strings.TrimSpace(bin)
	if bin == "" {
		return ""
	}
	for _, spec := range pkg.OpenClaw.Install {
		if len(spec.Bins) > 0 && !slices.Contains(spec.Bins, bin) {
			continue
		}
		switch spec.ResolvedKind() {
		case "brew":
			if spec.Formula != "" {
				return fmt.Sprintf("run brew install %s", spec.Formula)
			}
		case "go":
			if spec.Module != "" {
				return fmt.Sprintf("run go install %s", spec.Module)
			}
		case "node":
			if spec.Package != "" {
				return fmt.Sprintf("install npm package %s globally", spec.Package)
			}
		case "uv":
			if spec.Package != "" {
				return fmt.Sprintf("run uv tool install %s", spec.Package)
			}
		case "download":
			if spec.URL != "" {
				return fmt.Sprintf("download runtime dependency from %s", spec.URL)
			}
		case "shell":
			if label := strings.TrimSpace(spec.Label); label != "" {
				return fmt.Sprintf("run the skill installer step %q", label)
			}
			return "run the skill installer step and refresh the environment"
		}
	}
	return fmt.Sprintf("install binary %q and run env.refresh", bin)
}

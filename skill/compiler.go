package skill

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/fulcrus/hopclaw/bundle"
	"github.com/fulcrus/hopclaw/internal/support/normalize"
)

type Compiler interface {
	Compile(ctx context.Context, src SkillSource, spec *ExternalSkillSpec) (*SkillPackage, error)
}

type DefaultCompiler struct{}

const skillIDHexLengthBytes = 16

func (DefaultCompiler) Compile(_ context.Context, src SkillSource, spec *ExternalSkillSpec) (*SkillPackage, error) {
	name := strings.TrimSpace(spec.Name)
	if name == "" {
		return nil, fmt.Errorf("skill in %s is missing a name", src.Dir)
	}

	location := string(src.Kind) + ":" + filepath.Base(src.Dir)
	if rel, err := filepath.Rel(src.Root, src.Dir); err == nil && rel != "." {
		location = fmt.Sprintf("%s:%s", src.Kind, filepath.ToSlash(rel))
	}

	pkg := &SkillPackage{
		ID: makeSkillID(src, name),
		Source: SkillSource{
			Kind:     src.Kind,
			Root:     src.Root,
			Dir:      src.Dir,
			NameHint: src.NameHint,
			Priority: src.Priority,
		},
		Kind: SkillKindPrompt,
		Prompt: PromptSkill{
			Name:                   name,
			Description:            strings.TrimSpace(spec.Description),
			Instructions:           strings.TrimSpace(spec.Body),
			Location:               location,
			Homepage:               strings.TrimSpace(spec.Homepage),
			UserInvocable:          spec.UserInvocable,
			DisableModelInvocation: spec.DisableModelInvocation,
			Command: CommandDescriptor{
				Dispatch: strings.TrimSpace(spec.CommandDispatch),
				Tool:     strings.TrimSpace(spec.CommandTool),
				ArgMode:  strings.TrimSpace(spec.CommandArgMode),
			},
		},
		Status:     StatusReady,
		OpenClaw:   spec.OpenClaw,
		Trust:      inferTrust(src, spec),
		Raw:        *spec,
		LoadedAt:   time.Now().UTC(),
		Normalized: true,
	}
	if tools, issues := compileToolManifests(spec, pkg.Trust); len(tools) > 0 {
		pkg.ToolManifests = tools
		pkg.Kind = SkillKindExecutable
		pkg.Issues = append(pkg.Issues, issues...)
	}
	pkg.Issues = append(pkg.Issues, validatePackage(pkg)...)
	pkg.Issues = append(pkg.Issues, validateInstallSpecs(spec.OpenClaw.Install)...)

	// For plugin/community trust: warn if side effects are high-risk.
	if pkg.Trust == TrustCommunity || pkg.Trust == TrustUnknown {
		for _, tm := range pkg.ToolManifests {
			if tm.SideEffectClass == "destructive" || tm.SideEffectClass == "external_write" {
				pkg.Issues = append(pkg.Issues, SkillIssue{
					Severity: SeverityWarning,
					Message:  fmt.Sprintf("community skill declares %s side effect on tool %q — review before enabling", tm.SideEffectClass, tm.Name),
				})
			}
		}
	}

	pkg.Status = statusFromIssues(pkg.Issues)
	return pkg, nil
}

func makeSkillID(src SkillSource, name string) string {
	sum := sha256.Sum256([]byte(string(src.Kind) + "\x00" + src.Dir + "\x00" + name))
	return hex.EncodeToString(sum[:skillIDHexLengthBytes])
}

func inferTrust(src SkillSource, spec *ExternalSkillSpec) TrustClass {
	if spec.Companion != nil && spec.Companion.Security.Trust != "" {
		switch strings.ToLower(strings.TrimSpace(spec.Companion.Security.Trust)) {
		case "bundled":
			return TrustBundled
		case "internal":
			return TrustInternal
		case "verified":
			return TrustVerified
		case "community":
			return TrustCommunity
		}
	}

	switch src.Kind {
	case SourceBundled:
		return TrustBundled
	case SourceWorkspace, SourceUser:
		return TrustInternal
	case SourceClawHub:
		return TrustCommunity
	default:
		return TrustUnknown
	}
}

func compileToolManifests(spec *ExternalSkillSpec, trust TrustClass) ([]ToolManifest, []SkillIssue) {
	if spec.Bundle != nil {
		return compileBundleToolManifests(spec, trust)
	}
	tool, issues := compileCompanionToolManifest(spec, trust)
	if tool == nil {
		return nil, issues
	}
	return []ToolManifest{*tool}, issues
}

func compileCompanionToolManifest(spec *ExternalSkillSpec, trust TrustClass) (*ToolManifest, []SkillIssue) {
	if spec.Companion == nil {
		return nil, nil
	}
	var issues []SkillIssue
	name := strings.TrimSpace(spec.Companion.Tool.Name)
	if name == "" {
		name = strings.TrimSpace(spec.Name)
	}
	sideEffect := normalizeSideEffectClass(spec.Companion.Tool.SideEffectClass)
	if sideEffect != strings.TrimSpace(spec.Companion.Tool.SideEffectClass) && strings.TrimSpace(spec.Companion.Tool.SideEffectClass) != "" {
		issues = append(issues, SkillIssue{
			Severity: SeverityWarning,
			Code:     "normalized_side_effect_class",
			Message:  "invalid side effect class normalized to destructive",
		})
	}

	requiresApproval := false
	if spec.Companion.Security.RequiresApproval != nil {
		requiresApproval = *spec.Companion.Security.RequiresApproval
	}
	if spec.Companion.Tool.RequiresApproval != nil {
		requiresApproval = *spec.Companion.Tool.RequiresApproval
	}
	if sideEffect != "read" && !requiresApproval {
		requiresApproval = true
		issues = append(issues, SkillIssue{
			Severity: SeverityWarning,
			Code:     "forced_approval_for_side_effect",
			Message:  "write-capable skills require approval by default",
		})
	}
	if trust == TrustCommunity && !requiresApproval {
		requiresApproval = true
		issues = append(issues, SkillIssue{
			Severity: SeverityWarning,
			Code:     "forced_approval_for_community",
			Message:  "community executable skills require approval by default",
		})
	}

	executionKey := strings.TrimSpace(spec.Companion.Tool.ExecutionKey)
	if executionKey == "" {
		executionKey = "session:{id}"
		issues = append(issues, SkillIssue{
			Severity: SeverityWarning,
			Code:     "default_execution_key",
			Message:  "missing execution key; defaulted to session:{id}",
		})
	}

	timeout, timeoutValid := parseDuration(spec.Companion.Tool.Timeout)
	if strings.TrimSpace(spec.Companion.Tool.Timeout) != "" && !timeoutValid {
		issues = append(issues, SkillIssue{
			Severity: SeverityWarning,
			Code:     "invalid_timeout",
			Message:  "invalid timeout; runtime default will be used",
		})
	}

	manifest := &ToolManifest{
		Name:             name,
		Aliases:          append([]string(nil), spec.Companion.Tool.Aliases...),
		Description:      strings.TrimSpace(spec.Description),
		InputSchema:      cloneSchema(spec.Companion.Tool.InputSchema),
		OutputSchema:     cloneSchema(spec.Companion.Tool.OutputSchema),
		SideEffectClass:  sideEffect,
		Idempotent:       boolValue(spec.Companion.Tool.Idempotent, false),
		RequiresApproval: requiresApproval,
		ExecutionKey:     executionKey,
		Timeout:          timeout,
		Runtime:          spec.Companion.Runtime,
	}

	if manifest.Timeout == 0 {
		for _, install := range spec.OpenClaw.Install {
			if install.Script != "" {
				manifest.Timeout = 30 * time.Second
				break
			}
		}
	}
	return manifest, issues
}

func compileBundleToolManifests(spec *ExternalSkillSpec, trust TrustClass) ([]ToolManifest, []SkillIssue) {
	if spec.Bundle == nil {
		return nil, nil
	}
	runtimeType := bundle.RuntimeType(strings.ToLower(strings.TrimSpace(string(spec.Bundle.Runtime.Type))))
	switch runtimeType {
	case bundle.RuntimePrompt:
		return nil, nil
	case bundle.RuntimeExecutable:
	default:
		return nil, []SkillIssue{{
			Severity: SeverityError,
			Code:     "unsupported_bundle_runtime",
			Message:  fmt.Sprintf("bundle runtime type %q is not supported yet", spec.Bundle.Runtime.Type),
		}}
	}

	rt := spec.Bundle.Runtime.Executable
	if rt == nil || strings.TrimSpace(rt.Entry) == "" {
		return nil, []SkillIssue{{
			Severity: SeverityError,
			Code:     "missing_runtime_entry",
			Message:  "bundle executable runtime is missing entry",
		}}
	}

	var out []ToolManifest
	var issues []SkillIssue
	for _, tool := range spec.Bundle.Tools {
		name := strings.TrimSpace(tool.Name)
		if name == "" {
			issues = append(issues, SkillIssue{
				Severity: SeverityError,
				Code:     "missing_tool_name",
				Message:  "bundle tool is missing name",
			})
			continue
		}
		sideEffect := normalizeSideEffectClass(tool.SideEffectClass)
		if sideEffect != strings.TrimSpace(tool.SideEffectClass) && strings.TrimSpace(tool.SideEffectClass) != "" {
			issues = append(issues, SkillIssue{
				Severity: SeverityWarning,
				Code:     "normalized_side_effect_class",
				Message:  fmt.Sprintf("bundle tool %q has invalid side effect class normalized to destructive", name),
			})
		}
		requiresApproval := false
		if sideEffect != "read" {
			requiresApproval = true
		}
		if trust == TrustCommunity && !requiresApproval {
			requiresApproval = true
			issues = append(issues, SkillIssue{
				Severity: SeverityWarning,
				Code:     "forced_approval_for_community",
				Message:  fmt.Sprintf("community bundle tool %q requires approval by default", name),
			})
		}
		executionKey := strings.TrimSpace(tool.ExecutionKey)
		if executionKey == "" {
			executionKey = "session:{id}"
		}
		timeout, timeoutValid := parseDuration(tool.Timeout)
		if strings.TrimSpace(tool.Timeout) != "" && !timeoutValid {
			issues = append(issues, SkillIssue{
				Severity: SeverityWarning,
				Code:     "invalid_timeout",
				Message:  fmt.Sprintf("bundle tool %q has invalid timeout; runtime default will be used", name),
			})
			timeout = 0
		}
		out = append(out, ToolManifest{
			Name:             name,
			Aliases:          append([]string(nil), tool.Aliases...),
			Description:      normalize.FirstNonEmpty(tool.Description, spec.Bundle.Description, spec.Description),
			InputSchema:      cloneSchema(tool.InputSchema),
			OutputSchema:     cloneSchema(tool.OutputSchema),
			SideEffectClass:  sideEffect,
			Idempotent:       boolValue(tool.Idempotent, false),
			RequiresApproval: requiresApproval,
			ExecutionKey:     executionKey,
			Timeout:          timeout,
			Runtime:          *rt,
		})
	}
	return out, issues
}

func boolValue(v *bool, fallback bool) bool {
	if v == nil {
		return fallback
	}
	return *v
}

func parseDuration(v string) (time.Duration, bool) {
	if strings.TrimSpace(v) == "" {
		return 0, true
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return 0, false
	}
	return d, true
}

func cloneSchema(in JSONSchema) JSONSchema {
	if in == nil {
		return nil
	}
	out := make(JSONSchema, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func normalizeSideEffectClass(v string) string { return NormalizeSideEffectClass(v) }

func validatePackage(pkg *SkillPackage) []SkillIssue {
	var issues []SkillIssue
	if strings.TrimSpace(pkg.Prompt.Description) == "" {
		issues = append(issues, SkillIssue{
			Severity: SeverityWarning,
			Code:     "missing_description",
			Message:  "skill description is empty",
		})
	}
	if strings.TrimSpace(pkg.Prompt.Instructions) == "" {
		issues = append(issues, SkillIssue{
			Severity: SeverityWarning,
			Code:     "missing_instructions",
			Message:  "skill instructions are empty",
		})
	}
	for _, tool := range pkg.ToolManifests {
		entry := strings.TrimSpace(tool.Runtime.Entry)
		if entry == "" {
			issues = append(issues, SkillIssue{
				Severity: SeverityError,
				Code:     "missing_runtime_entry",
				Message:  "companion manifest is missing runtime entry",
			})
			continue
		}
		// Verify runtime entry doesn't escape skill directory.
		entryPath := filepath.Clean(entry)
		if filepath.IsAbs(entryPath) || strings.HasPrefix(entryPath, "..") {
			issues = append(issues, SkillIssue{
				Severity: SeverityError,
				Code:     "runtime_entry_path_escape",
				Message:  "runtime entry path must be relative and within skill directory",
			})
			continue
		}
		target := entry
		if !filepath.IsAbs(target) {
			target = filepath.Join(pkg.Source.Dir, entry)
		}
		info, err := os.Stat(target)
		if err != nil {
			issues = append(issues, SkillIssue{
				Severity: SeverityError,
				Code:     "runtime_entry_not_found",
				Message:  fmt.Sprintf("runtime entry %q does not exist", entry),
			})
			continue
		}
		if info.IsDir() {
			issues = append(issues, SkillIssue{
				Severity: SeverityError,
				Code:     "runtime_entry_is_directory",
				Message:  fmt.Sprintf("runtime entry %q points to a directory", entry),
			})
		}
	}
	return issues
}

func statusFromIssues(issues []SkillIssue) PackageStatus {
	status := StatusReady
	for _, issue := range issues {
		switch issue.Severity {
		case SeverityError:
			return StatusBlocked
		case SeverityWarning:
			status = StatusDegraded
		}
	}
	return status
}

// Install spec security validation patterns (ported from openclaw frontmatter.ts).
var (
	brewFormulaPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9@+._/-]*$`)
	goModulePattern    = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._~+\-/]*(?:@[A-Za-z0-9][A-Za-z0-9._~+\-/]*)?$`)
	npmScopedPattern   = regexp.MustCompile(`^@[a-z0-9][a-z0-9._~-]*/[a-z0-9][a-z0-9._~-]*$`)
	npmUnscopedPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._~-]*$`)
)

// validateInstallSpecs validates install spec fields for injection attacks.
func validateInstallSpecs(specs []InstallSpec) []SkillIssue {
	var issues []SkillIssue
	for i, spec := range specs {
		prefix := fmt.Sprintf("install[%d]", i)
		switch spec.Kind {
		case "brew":
			if f := strings.TrimSpace(spec.Formula); f != "" {
				if strings.HasPrefix(f, "-") || strings.Contains(f, `\`) || strings.Contains(f, "..") {
					issues = append(issues, SkillIssue{
						Severity: SeverityError,
						Code:     "unsafe_brew_formula",
						Message:  fmt.Sprintf("%s: unsafe brew formula %q", prefix, f),
					})
				} else if !brewFormulaPattern.MatchString(f) {
					issues = append(issues, SkillIssue{
						Severity: SeverityError,
						Code:     "invalid_brew_formula",
						Message:  fmt.Sprintf("%s: invalid brew formula %q", prefix, f),
					})
				}
			}
		case "node", "npm":
			if p := strings.TrimSpace(spec.Package); p != "" {
				if strings.HasPrefix(p, "-") || strings.Contains(p, "://") || strings.Contains(p, "#") {
					issues = append(issues, SkillIssue{
						Severity: SeverityError,
						Code:     "unsafe_npm_package",
						Message:  fmt.Sprintf("%s: unsafe npm package spec %q", prefix, p),
					})
				} else {
					name := p
					if idx := strings.LastIndex(p, "@"); idx > 0 {
						name = p[:idx]
					}
					if !npmScopedPattern.MatchString(name) && !npmUnscopedPattern.MatchString(name) {
						issues = append(issues, SkillIssue{
							Severity: SeverityError,
							Code:     "invalid_npm_package",
							Message:  fmt.Sprintf("%s: invalid npm package name %q", prefix, name),
						})
					}
				}
			}
		case "go":
			if m := strings.TrimSpace(spec.Module); m != "" {
				if strings.HasPrefix(m, "-") || strings.Contains(m, `\`) || strings.Contains(m, "://") {
					issues = append(issues, SkillIssue{
						Severity: SeverityError,
						Code:     "unsafe_go_module",
						Message:  fmt.Sprintf("%s: unsafe go module %q", prefix, m),
					})
				} else if !goModulePattern.MatchString(m) {
					issues = append(issues, SkillIssue{
						Severity: SeverityError,
						Code:     "invalid_go_module",
						Message:  fmt.Sprintf("%s: invalid go module path %q", prefix, m),
					})
				}
			}
		case "download":
			if u := strings.TrimSpace(spec.URL); u != "" {
				parsed, err := url.Parse(u)
				if err != nil || strings.ContainsAny(u, " \t\n\r") {
					issues = append(issues, SkillIssue{
						Severity: SeverityError,
						Code:     "invalid_download_url",
						Message:  fmt.Sprintf("%s: invalid download URL %q", prefix, u),
					})
				} else if parsed.Scheme != "http" && parsed.Scheme != "https" {
					issues = append(issues, SkillIssue{
						Severity: SeverityError,
						Code:     "unsafe_download_url",
						Message:  fmt.Sprintf("%s: only http/https download URLs allowed, got %q", prefix, parsed.Scheme),
					})
				}
			}
		}
		// Script field: block shell variable injection patterns.
		if s := strings.TrimSpace(spec.Script); s != "" {
			if strings.Contains(s, "$(") || strings.Contains(s, "`") {
				issues = append(issues, SkillIssue{
					Severity: SeverityWarning,
					Code:     "script_command_substitution",
					Message:  fmt.Sprintf("%s: script contains command substitution syntax", prefix),
				})
			}
		}
	}
	return issues
}

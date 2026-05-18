package modules

import (
	"crypto/sha256"
	"encoding/hex"
	"path/filepath"
	"strings"

	"github.com/fulcrus/hopclaw/skill"
)

const (
	ModuleKindSkill = "skill"

	skillMetadataID                     = "skill_id"
	skillMetadataKind                   = "skill_kind"
	skillMetadataStatus                 = "skill_status"
	skillMetadataTrust                  = "skill_trust"
	skillMetadataSourceKind             = "skill_source_kind"
	skillMetadataSourceDir              = "skill_source_dir"
	skillMetadataSourceRoot             = "skill_source_root"
	skillMetadataConfigKey              = "skill_config_key"
	skillMetadataUserInvocable          = "skill_user_invocable"
	skillMetadataDisableModelInvocation = "skill_disable_model_invocation"
	skillMetadataBlocked                = "skill_blocked"
	skillMetadataIssueCount             = "skill_issue_count"
)

func SkillModules(snapshot skill.RegistrySnapshot) []StaticModule {
	if len(snapshot.Ordered) == 0 && len(snapshot.Blocked) == 0 {
		return nil
	}
	out := make([]StaticModule, 0, len(snapshot.Ordered)+len(snapshot.Blocked))
	for _, pkg := range snapshot.Ordered {
		if pkg == nil {
			continue
		}
		out = append(out, skillModuleFromPackage(pkg))
	}
	for _, blocked := range snapshot.Blocked {
		out = append(out, skillModuleFromBlocked(blocked))
	}
	return out
}

func WithSkillModules(base Catalog, snapshot skill.RegistrySnapshot) Catalog {
	items := make([]StaticModule, 0, base.Len()+len(snapshot.Ordered)+len(snapshot.Blocked))
	for _, item := range base.Modules() {
		if isSkillModuleManifest(item.Manifest()) {
			continue
		}
		items = append(items, item)
	}
	items = append(items, SkillModules(snapshot)...)
	return BuildCatalog(items)
}

func isSkillModuleManifest(manifest Manifest) bool {
	return strings.EqualFold(strings.TrimSpace(manifest.Kind), ModuleKindSkill)
}

func skillModuleFromPackage(pkg *skill.SkillPackage) StaticModule {
	manifest := Manifest{
		ID:             "skill:" + strings.TrimSpace(pkg.ID),
		Name:           strings.TrimSpace(pkg.Name()),
		Version:        skillModuleVersion(pkg.Raw),
		Description:    strings.TrimSpace(pkg.Prompt.Description),
		Kind:           ModuleKindSkill,
		Source:         SourceExternal,
		Delivery:       skillModuleDelivery(pkg),
		Level:          ModuleLevelDeclared,
		Metadata:       skillModuleMetadata(pkg),
		DefaultEnabled: true,
	}
	contrib := Contributions{
		Tools: skillToolComponents(pkg),
	}
	health := HealthReport{
		Status:  skillModuleHealthStatus(pkg.Status),
		Summary: skillModuleHealthSummary(pkg.Issues),
		Details: map[string]any{
			"tool_count":  len(pkg.ToolManifests),
			"issue_count": len(pkg.Issues),
		},
	}
	if sourceDir := strings.TrimSpace(pkg.Source.Dir); sourceDir != "" {
		health.Details["source_dir"] = sourceDir
	}
	return StaticModule{
		ManifestValue:      manifest,
		ContributionsValue: contrib,
		HealthValue:        health,
	}
}

func skillModuleFromBlocked(blocked skill.BlockedSkill) StaticModule {
	name := strings.TrimSpace(blocked.NameHint)
	if name == "" {
		name = filepath.Base(strings.TrimSpace(blocked.Source.Dir))
	}
	manifest := Manifest{
		ID:             "skill:" + blockedSkillModuleID(blocked),
		Name:           name,
		Description:    skillModuleHealthSummary(blocked.Issues),
		Kind:           ModuleKindSkill,
		Source:         SourceExternal,
		Delivery:       DeliveryManifest,
		Level:          ModuleLevelDeclared,
		Metadata:       blockedSkillMetadata(blocked, name),
		DefaultEnabled: false,
	}
	health := HealthReport{
		Status:  HealthFailed,
		Summary: skillModuleHealthSummary(blocked.Issues),
		Details: map[string]any{
			"blocked":     true,
			"issue_count": len(blocked.Issues),
		},
	}
	if sourceDir := strings.TrimSpace(blocked.Source.Dir); sourceDir != "" {
		health.Details["source_dir"] = sourceDir
	}
	return StaticModule{
		ManifestValue: manifest,
		HealthValue:   health,
	}
}

func skillModuleVersion(spec skill.ExternalSkillSpec) string {
	if spec.Bundle != nil {
		return strings.TrimSpace(spec.Bundle.Version)
	}
	if spec.Companion != nil {
		return strings.TrimSpace(spec.Companion.Version)
	}
	return ""
}

func skillModuleMetadata(pkg *skill.SkillPackage) map[string]any {
	if pkg == nil {
		return nil
	}
	return map[string]any{
		skillMetadataID:                     strings.TrimSpace(pkg.ID),
		skillMetadataKind:                   strings.TrimSpace(string(pkg.Kind)),
		skillMetadataStatus:                 strings.TrimSpace(string(pkg.Status)),
		skillMetadataTrust:                  strings.TrimSpace(string(pkg.Trust)),
		skillMetadataSourceKind:             strings.TrimSpace(string(pkg.Source.Kind)),
		skillMetadataSourceDir:              strings.TrimSpace(pkg.Source.Dir),
		skillMetadataSourceRoot:             strings.TrimSpace(pkg.Source.Root),
		skillMetadataConfigKey:              strings.TrimSpace(pkg.ConfigKey()),
		skillMetadataUserInvocable:          pkg.Prompt.UserInvocable,
		skillMetadataDisableModelInvocation: pkg.Prompt.DisableModelInvocation,
		skillMetadataBlocked:                false,
		skillMetadataIssueCount:             len(pkg.Issues),
	}
}

func blockedSkillMetadata(blocked skill.BlockedSkill, name string) map[string]any {
	return map[string]any{
		skillMetadataID:                     blockedSkillModuleID(blocked),
		skillMetadataKind:                   "",
		skillMetadataStatus:                 string(skill.StatusBlocked),
		skillMetadataTrust:                  "",
		skillMetadataSourceKind:             strings.TrimSpace(string(blocked.Source.Kind)),
		skillMetadataSourceDir:              strings.TrimSpace(blocked.Source.Dir),
		skillMetadataSourceRoot:             strings.TrimSpace(blocked.Source.Root),
		skillMetadataConfigKey:              strings.TrimSpace(name),
		skillMetadataUserInvocable:          false,
		skillMetadataDisableModelInvocation: false,
		skillMetadataBlocked:                true,
		skillMetadataIssueCount:             len(blocked.Issues),
	}
}

func skillToolComponents(pkg *skill.SkillPackage) []Component {
	if pkg == nil || len(pkg.ToolManifests) == 0 {
		return nil
	}
	out := make([]Component, 0, len(pkg.ToolManifests))
	for _, manifest := range pkg.ToolManifests {
		name := strings.TrimSpace(manifest.Name)
		if name == "" {
			continue
		}
		metadata := map[string]any{
			"timeout":            manifest.Timeout.String(),
			"input_schema":       cloneMetadata(manifest.InputSchema),
			"output_schema":      cloneMetadata(manifest.OutputSchema),
			"side_effect_class":  strings.TrimSpace(manifest.SideEffectClass),
			"requires_approval":  manifest.RequiresApproval,
			"execution_key":      strings.TrimSpace(manifest.ExecutionKey),
			"runtime_shell":      strings.TrimSpace(manifest.Runtime.Shell),
			"runtime_entry":      skillToolRuntimeEntry(pkg.Source.Dir, manifest.Runtime.Entry),
			"skill_source_dir":   strings.TrimSpace(pkg.Source.Dir),
			"skill_source_kind":  strings.TrimSpace(string(pkg.Source.Kind)),
			"skill_package_id":   strings.TrimSpace(pkg.ID),
			"skill_package_name": strings.TrimSpace(pkg.Name()),
		}
		out = append(out, Component{
			Kind:        ComponentKindTool,
			Name:        name,
			Description: strings.TrimSpace(manifest.Description),
			Path:        metadataString(metadata, "runtime_entry"),
			Metadata:    metadata,
		})
	}
	return out
}

func skillToolRuntimeEntry(sourceDir, entry string) string {
	entry = strings.TrimSpace(entry)
	if entry == "" {
		return ""
	}
	if filepath.IsAbs(entry) {
		return filepath.Clean(entry)
	}
	sourceDir = strings.TrimSpace(sourceDir)
	if sourceDir == "" {
		return filepath.Clean(entry)
	}
	return filepath.Clean(filepath.Join(sourceDir, entry))
}

func skillModuleDelivery(pkg *skill.SkillPackage) Delivery {
	if pkg == nil || len(pkg.ToolManifests) == 0 {
		return DeliveryManifest
	}
	return DeliveryProcess
}

func skillModuleHealthStatus(status skill.PackageStatus) HealthStatus {
	switch status {
	case skill.StatusReady:
		return HealthReady
	case skill.StatusDegraded:
		return HealthDegraded
	case skill.StatusBlocked:
		return HealthFailed
	default:
		return HealthUnknown
	}
}

func skillModuleHealthSummary(issues []skill.SkillIssue) string {
	for _, issue := range issues {
		if message := strings.TrimSpace(issue.Message); message != "" {
			return message
		}
	}
	return ""
}

func blockedSkillModuleID(blocked skill.BlockedSkill) string {
	sum := sha256.Sum256([]byte(
		string(blocked.Source.Kind) + "\x00" +
			strings.TrimSpace(blocked.Source.Dir) + "\x00" +
			strings.TrimSpace(blocked.NameHint),
	))
	return hex.EncodeToString(sum[:16])
}

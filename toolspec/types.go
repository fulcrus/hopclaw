package toolspec

import (
	"strings"

	supportmaps "github.com/fulcrus/hopclaw/internal/support/maps"
	"github.com/fulcrus/hopclaw/internal/support/normalize"
	"github.com/fulcrus/hopclaw/skill"
)

type ToolAvailabilityStatus string

const (
	AvailabilityReady        ToolAvailabilityStatus = "ready"
	AvailabilityDegraded     ToolAvailabilityStatus = "degraded"
	AvailabilityBlocked      ToolAvailabilityStatus = "blocked"
	AvailabilityDiscoverable ToolAvailabilityStatus = "discoverable"
)

type ToolAvailabilityCheck struct {
	Name     string `json:"name,omitempty"`
	Status   string `json:"status,omitempty"`
	Detail   string `json:"detail,omitempty"`
	Required bool   `json:"required,omitempty"`
}

type ToolAvailability struct {
	Status       ToolAvailabilityStatus  `json:"status,omitempty"`
	Reasons      []string                `json:"reasons,omitempty"`
	Checks       []ToolAvailabilityCheck `json:"checks,omitempty"`
	InstallHints []string                `json:"install_hints,omitempty"`
}

type ToolPresentation struct {
	Label            string   `json:"label,omitempty"`
	ShortDescription string   `json:"short_description,omitempty"`
	Badges           []string `json:"badges,omitempty"`
	Group            string   `json:"group,omitempty"`
}

type ToolDefinition struct {
	Name               string           `json:"name"`
	Description        string           `json:"description,omitempty"`
	InputSchema        map[string]any   `json:"input_schema,omitempty"`
	OutputSchema       map[string]any   `json:"output_schema,omitempty"`
	SideEffectClass    string           `json:"side_effect_class,omitempty"`
	Idempotent         bool             `json:"idempotent,omitempty"`
	RequiresApproval   bool             `json:"requires_approval,omitempty"`
	ExecutionKey       string           `json:"execution_key,omitempty"`
	Domain             string           `json:"domain,omitempty"`
	Category           string           `json:"category,omitempty"`
	Source             string           `json:"source,omitempty"`
	SourceRef          string           `json:"source_ref,omitempty"`
	Trust              string           `json:"trust,omitempty"`
	Eligible           bool             `json:"eligible,omitempty"`
	EligibilityReasons []string         `json:"eligibility_reasons,omitempty"`
	Availability       ToolAvailability `json:"availability,omitempty"`
	Presentation       ToolPresentation `json:"presentation,omitempty"`
}

type ResolvedTool struct {
	Descriptor   ToolDefinition          `json:"descriptor"`
	SkillBinding *skill.BoundTool        `json:"-"`
	Manifest     skill.ToolManifest      `json:"manifest,omitempty"`
	Package      *skill.SkillPackage     `json:"package,omitempty"`
	Eligibility  skill.EligibilityResult `json:"eligibility,omitempty"`
	ExecutorRef  string                  `json:"executor_ref,omitempty"`
}

func NormalizeDefinition(def ToolDefinition) ToolDefinition {
	def.Name = strings.TrimSpace(def.Name)
	def.Description = strings.TrimSpace(def.Description)
	def.SideEffectClass = skill.NormalizeSideEffectClass(def.SideEffectClass)
	def.ExecutionKey = strings.TrimSpace(def.ExecutionKey)
	def.Domain = strings.TrimSpace(def.Domain)
	def.Category = strings.TrimSpace(def.Category)
	def.Source = strings.TrimSpace(def.Source)
	def.SourceRef = strings.TrimSpace(def.SourceRef)
	def.Trust = strings.TrimSpace(def.Trust)
	def.EligibilityReasons = append([]string(nil), def.EligibilityReasons...)
	def.InputSchema = supportmaps.Clone(def.InputSchema)
	def.OutputSchema = supportmaps.Clone(def.OutputSchema)
	def.Availability = normalizeAvailability(def.Availability, def.Eligible, def.EligibilityReasons)
	def.Presentation = normalizePresentation(def.Presentation)
	return def
}

func NormalizeResolvedTool(tool *ResolvedTool) *ResolvedTool {
	if tool == nil {
		return nil
	}
	normalized := *tool
	if tool.SkillBinding != nil {
		binding := *tool.SkillBinding
		binding.Manifest.InputSchema = supportmaps.Clone(tool.SkillBinding.Manifest.InputSchema)
		binding.Manifest.OutputSchema = supportmaps.Clone(tool.SkillBinding.Manifest.OutputSchema)
		binding.Manifest.Aliases = append([]string(nil), tool.SkillBinding.Manifest.Aliases...)
		normalized.SkillBinding = &binding
	}
	normalized.Manifest = cloneManifest(tool.Manifest)
	normalized.Eligibility = cloneEligibility(tool.Eligibility)
	if strings.TrimSpace(normalized.Manifest.Name) == "" {
		normalized.Manifest = mergeManifestFromDescriptor(normalized.Manifest, normalized.Descriptor)
	}
	if !normalized.Eligibility.Eligible && normalized.Descriptor.Eligible {
		normalized.Eligibility.Eligible = true
	}
	if len(normalized.Eligibility.Reasons) == 0 && len(normalized.Descriptor.EligibilityReasons) > 0 {
		normalized.Eligibility.Reasons = append([]string(nil), normalized.Descriptor.EligibilityReasons...)
	}
	normalized.Descriptor = NormalizeDefinition(mergeDescriptor(normalized.Descriptor, normalized.Manifest, normalized.Package, normalized.Eligibility))
	return &normalized
}

func ResolvedFromSkillBinding(binding *skill.BoundTool, descriptor ToolDefinition, executorRef string) *ResolvedTool {
	if binding == nil {
		return NormalizeResolvedTool(&ResolvedTool{
			Descriptor:  descriptor,
			ExecutorRef: strings.TrimSpace(executorRef),
		})
	}
	return NormalizeResolvedTool(&ResolvedTool{
		Descriptor:   descriptor,
		SkillBinding: binding,
		Manifest:     cloneManifest(binding.Manifest),
		Package:      binding.Package,
		Eligibility:  cloneEligibility(binding.Eligibility),
		ExecutorRef:  strings.TrimSpace(executorRef),
	})
}

func mergeDescriptor(def ToolDefinition, manifest skill.ToolManifest, pkg *skill.SkillPackage, eligibility skill.EligibilityResult) ToolDefinition {
	if strings.TrimSpace(def.Name) == "" {
		def.Name = strings.TrimSpace(manifest.Name)
	}
	if strings.TrimSpace(def.Description) == "" {
		def.Description = strings.TrimSpace(manifest.Description)
		if def.Description == "" && pkg != nil {
			def.Description = strings.TrimSpace(pkg.Prompt.Description)
		}
	}
	if len(def.InputSchema) == 0 {
		def.InputSchema = supportmaps.Clone(manifest.InputSchema)
	}
	if len(def.OutputSchema) == 0 {
		def.OutputSchema = supportmaps.Clone(manifest.OutputSchema)
	}
	if strings.TrimSpace(def.SideEffectClass) == "" {
		def.SideEffectClass = skill.NormalizeSideEffectClass(manifest.SideEffectClass)
	}
	if !def.Idempotent {
		def.Idempotent = manifest.Idempotent
	}
	if !def.RequiresApproval {
		def.RequiresApproval = manifest.RequiresApproval
	}
	if strings.TrimSpace(def.ExecutionKey) == "" {
		def.ExecutionKey = strings.TrimSpace(manifest.ExecutionKey)
	}
	if strings.TrimSpace(def.Trust) == "" && pkg != nil {
		def.Trust = string(pkg.Trust)
	}
	if strings.TrimSpace(def.SourceRef) == "" && pkg != nil {
		def.SourceRef = strings.TrimSpace(pkg.Source.Dir)
	}
	if !def.Eligible && eligibility.Eligible {
		def.Eligible = true
	}
	if len(def.EligibilityReasons) == 0 && len(eligibility.Reasons) > 0 {
		def.EligibilityReasons = append([]string(nil), eligibility.Reasons...)
	}
	if strings.TrimSpace(def.Source) == "" && pkg != nil {
		def.Source = "skill"
	}
	return def
}

func normalizeAvailability(av ToolAvailability, eligible bool, reasons []string) ToolAvailability {
	out := av
	out.Reasons = append([]string(nil), av.Reasons...)
	out.Checks = append([]ToolAvailabilityCheck(nil), av.Checks...)
	out.InstallHints = append([]string(nil), av.InstallHints...)
	if len(out.Reasons) == 0 && len(reasons) > 0 {
		out.Reasons = append([]string(nil), reasons...)
	}
	if out.Status == "" {
		switch {
		case eligible:
			out.Status = AvailabilityReady
		case len(out.Reasons) > 0:
			out.Status = AvailabilityBlocked
		}
	}
	return out
}

func normalizePresentation(in ToolPresentation) ToolPresentation {
	out := in
	out.Label = strings.TrimSpace(out.Label)
	out.ShortDescription = strings.TrimSpace(out.ShortDescription)
	out.Group = strings.TrimSpace(out.Group)
	out.Badges = append([]string(nil), in.Badges...)
	return out
}

func cloneManifest(in skill.ToolManifest) skill.ToolManifest {
	out := in
	out.InputSchema = supportmaps.Clone(in.InputSchema)
	out.OutputSchema = supportmaps.Clone(in.OutputSchema)
	out.Aliases = append([]string(nil), in.Aliases...)
	return out
}

func cloneEligibility(in skill.EligibilityResult) skill.EligibilityResult {
	out := in
	out.Reasons = append([]string(nil), in.Reasons...)
	out.InjectedEnv = append([]string(nil), in.InjectedEnv...)
	return out
}

func mergeManifestFromDescriptor(manifest skill.ToolManifest, definition ToolDefinition) skill.ToolManifest {
	manifest.Name = strings.TrimSpace(normalize.FirstNonEmpty(manifest.Name, definition.Name))
	manifest.Description = strings.TrimSpace(normalize.FirstNonEmpty(manifest.Description, definition.Description))
	if len(manifest.InputSchema) == 0 {
		manifest.InputSchema = supportmaps.Clone(definition.InputSchema)
	}
	if len(manifest.OutputSchema) == 0 {
		manifest.OutputSchema = supportmaps.Clone(definition.OutputSchema)
	}
	if strings.TrimSpace(manifest.SideEffectClass) == "" {
		manifest.SideEffectClass = skill.NormalizeSideEffectClass(definition.SideEffectClass)
	}
	if !manifest.Idempotent {
		manifest.Idempotent = definition.Idempotent
	}
	if !manifest.RequiresApproval {
		manifest.RequiresApproval = definition.RequiresApproval
	}
	if strings.TrimSpace(manifest.ExecutionKey) == "" {
		manifest.ExecutionKey = strings.TrimSpace(definition.ExecutionKey)
	}
	return manifest
}

package agent

import (
	"strings"

	"github.com/fulcrus/hopclaw/approval"
	"github.com/fulcrus/hopclaw/modelrouter"
	"github.com/fulcrus/hopclaw/skill"
	"github.com/fulcrus/hopclaw/toolspec"
)

func toApprovalCalls(calls []ToolCall) []approval.ToolCall {
	if len(calls) == 0 {
		return nil
	}
	out := make([]approval.ToolCall, len(calls))
	for i, call := range calls {
		out[i] = approval.ToolCall{
			ID:            call.ID,
			Name:          call.Name,
			ResourceScope: approval.ResourceScopeFromToolCall(call.Name, call.Input),
		}
		if call.Input != nil {
			out[i].Input = make(map[string]any, len(call.Input))
			for k, v := range call.Input {
				out[i].Input[k] = v
			}
		}
	}
	return out
}

func cloneToolCalls(calls []ToolCall) []ToolCall {
	if len(calls) == 0 {
		return nil
	}
	out := make([]ToolCall, len(calls))
	for i, call := range calls {
		out[i] = ToolCall{
			ID:   call.ID,
			Name: call.Name,
		}
		if call.Input != nil {
			out[i].Input = make(map[string]any, len(call.Input))
			for k, v := range call.Input {
				out[i].Input[k] = v
			}
		}
	}
	return out
}

func requiredCapabilities(tools []ToolDefinition) []modelrouter.Capability {
	required := []modelrouter.Capability{modelrouter.CapabilityChat}
	if len(tools) == 0 {
		return required
	}
	return append(required, modelrouter.CapabilityTools)
}

func buildAllowedToolDefinitions(snapshot skill.SessionSkillSnapshot, session *Session, executor ToolExecutor, run *Run) []ToolDefinition {
	return filterToolDefinitionsForRun(buildToolDefinitions(snapshot, session, executor), run)
}

func buildToolDefinitions(snapshot skill.SessionSkillSnapshot, session *Session, executor ToolExecutor) []ToolDefinition {
	var tools []ToolDefinition
	seen := make(map[string]struct{})
	if provider, ok := executor.(ToolDefinitionProvider); ok {
		for _, definition := range provider.ToolDefinitions(session) {
			if appendToolDefinition(&tools, seen, definition) {
				continue
			}
		}
	}
	for _, bound := range snapshot.Ordered {
		for _, manifest := range bound.Package.ToolManifests {
			appendToolDefinition(&tools, seen, ToolDefinition{
				Name:               manifest.Name,
				Description:        defaultString(manifest.Description, bound.Package.Prompt.Description),
				InputSchema:        cloneMap(manifest.InputSchema),
				OutputSchema:       cloneMap(manifest.OutputSchema),
				SideEffectClass:    manifest.SideEffectClass,
				Idempotent:         manifest.Idempotent,
				RequiresApproval:   manifest.RequiresApproval,
				ExecutionKey:       manifest.ExecutionKey,
				Source:             "skill",
				SourceRef:          bound.Package.Source.Dir,
				Trust:              string(bound.Package.Trust),
				Eligible:           bound.Eligibility.Eligible,
				EligibilityReasons: append([]string(nil), bound.Eligibility.Reasons...),
				Availability: ToolAvailability{
					Status: func() ToolAvailabilityStatus {
						if bound.Eligibility.Eligible {
							return AvailabilityReady
						}
						return AvailabilityBlocked
					}(),
					Reasons: append([]string(nil), bound.Eligibility.Reasons...),
				},
			})
		}
	}
	return canonicalToolDefinitions(tools)
}

func filterToolDefinitionsForRun(defs []ToolDefinition, run *Run) []ToolDefinition {
	if run == nil {
		return defs
	}
	if run.EffectiveProfile != nil && len(run.EffectiveProfile.AllowedTools) > 0 {
		allowed := make(map[string]struct{}, len(run.EffectiveProfile.AllowedTools))
		for _, name := range run.EffectiveProfile.AllowedTools {
			trimmed := strings.ToLower(strings.TrimSpace(name))
			if trimmed == "" {
				continue
			}
			allowed[trimmed] = struct{}{}
		}
		if len(allowed) > 0 {
			filtered := make([]ToolDefinition, 0, len(defs))
			for _, def := range defs {
				if _, ok := allowed[strings.ToLower(strings.TrimSpace(def.Name))]; !ok {
					continue
				}
				filtered = append(filtered, def)
			}
			defs = filtered
		}
	}
	if deriveEffectiveDelegationContract(run, run.Delegation) != nil {
		return defs
	}
	filtered := make([]ToolDefinition, 0, len(defs))
	for _, def := range defs {
		if extractToolDomain(def.Name) == DomainAgent {
			continue
		}
		filtered = append(filtered, def)
	}
	return filtered
}

func appendToolDefinition(out *[]ToolDefinition, seen map[string]struct{}, definition ToolDefinition) bool {
	name := strings.TrimSpace(definition.Name)
	if name == "" {
		return false
	}
	if _, exists := seen[name]; exists {
		return true
	}
	seen[name] = struct{}{}
	definition = normalizeToolDefinition(definition)
	*out = append(*out, definition)
	return false
}

func (a *AgentComponent) resolveTool(session *Session, snapshot skill.SessionSkillSnapshot, name string) *ResolvedTool {
	if resolver, ok := a.tools.(ToolResolver); ok {
		if resolved, ok := resolver.ResolveTool(session, name); ok {
			return resolved
		}
	}
	boundTool, ok := snapshot.ResolveTool(name)
	if !ok {
		return nil
	}
	copied := boundTool
	return toolspec.ResolvedFromSkillBinding(&copied, ToolDefinition{
		Name:               copied.Manifest.Name,
		Description:        defaultString(copied.Manifest.Description, copied.Package.Prompt.Description),
		InputSchema:        cloneMap(copied.Manifest.InputSchema),
		OutputSchema:       cloneMap(copied.Manifest.OutputSchema),
		SideEffectClass:    copied.Manifest.SideEffectClass,
		Idempotent:         copied.Manifest.Idempotent,
		RequiresApproval:   copied.Manifest.RequiresApproval,
		ExecutionKey:       copied.Manifest.ExecutionKey,
		Source:             "skill",
		SourceRef:          copied.Package.Source.Dir,
		Trust:              string(copied.Package.Trust),
		Eligible:           copied.Eligibility.Eligible,
		EligibilityReasons: append([]string(nil), copied.Eligibility.Reasons...),
		Availability: ToolAvailability{
			Status: func() ToolAvailabilityStatus {
				if copied.Eligibility.Eligible {
					return AvailabilityReady
				}
				return AvailabilityBlocked
			}(),
			Reasons: append([]string(nil), copied.Eligibility.Reasons...),
		},
	}, "session-skill")
}

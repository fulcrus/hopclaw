package toolruntime

import (
	"context"
	"strings"

	"github.com/fulcrus/hopclaw/agent"
	"github.com/fulcrus/hopclaw/contextengine"
	"github.com/fulcrus/hopclaw/skill"
)

// BuiltinHandler is the signature for tool implementation functions registered via addTools.
// Exported so that domain sub-packages can define handlers.
type BuiltinHandler func(ctx context.Context, b *Builtins, call agent.ToolCall) (contextengine.ToolResult, error)

// builtinHandler is a package-internal alias kept for source compatibility
// during the incremental migration to domain sub-packages.
type builtinHandler = BuiltinHandler

func wrapBuiltinManifests(manifests []skill.ToolManifest) []BuiltinToolDef {
	if len(manifests) == 0 {
		return nil
	}
	out := make([]BuiltinToolDef, 0, len(manifests))
	for _, manifest := range manifests {
		out = append(out, BuiltinToolDef{Manifest: manifest})
	}
	return out
}

// BuiltinToolDef pairs a manifest with its handler for bulk registration.
// Exported so that domain sub-packages can return tool definitions.
type BuiltinToolDef struct {
	Manifest skill.ToolManifest
	Handler  BuiltinHandler
}

// builtinToolDef is a package-internal alias kept for source compatibility.
type builtinToolDef = BuiltinToolDef

// addTools registers additional tools from category files.
func (b *Builtins) addTools(pkg *skill.SkillPackage, defs []BuiltinToolDef) {
	for _, d := range defs {
		if strings.TrimSpace(d.Manifest.Name) == "" {
			continue
		}
		pkg.ToolManifests = append(pkg.ToolManifests, skill.ToolManifest{
			Name:             d.Manifest.Name,
			Aliases:          append([]string(nil), d.Manifest.Aliases...),
			Description:      d.Manifest.Description,
			InputSchema:      cloneSchema(d.Manifest.InputSchema),
			OutputSchema:     cloneSchema(d.Manifest.OutputSchema),
			SideEffectClass:  d.Manifest.SideEffectClass,
			Idempotent:       d.Manifest.Idempotent,
			RequiresApproval: d.Manifest.RequiresApproval,
			ExecutionKey:     d.Manifest.ExecutionKey,
			Timeout:          d.Manifest.Timeout,
			Runtime:          d.Manifest.Runtime,
		})
		bound := skill.BoundTool{
			Package: pkg,
			Manifest: skill.ToolManifest{
				Name:             d.Manifest.Name,
				Aliases:          d.Manifest.Aliases,
				Description:      d.Manifest.Description,
				InputSchema:      cloneSchema(d.Manifest.InputSchema),
				OutputSchema:     cloneSchema(d.Manifest.OutputSchema),
				SideEffectClass:  d.Manifest.SideEffectClass,
				Idempotent:       d.Manifest.Idempotent,
				RequiresApproval: d.Manifest.RequiresApproval,
				ExecutionKey:     d.Manifest.ExecutionKey,
				Timeout:          d.Manifest.Timeout,
			},
			Eligibility: skill.EligibilityResult{Eligible: true},
		}
		b.tools[d.Manifest.Name] = bound
		if d.Handler != nil {
			b.handlers[d.Manifest.Name] = d.Handler
		}
		for _, alias := range d.Manifest.Aliases {
			b.tools[alias] = bound
			if d.Handler != nil {
				b.handlers[alias] = d.Handler
			}
		}
		b.definitions = append(b.definitions, agent.ToolDefinition{
			Name:             d.Manifest.Name,
			Description:      d.Manifest.Description,
			InputSchema:      cloneSchema(d.Manifest.InputSchema),
			OutputSchema:     cloneSchema(d.Manifest.OutputSchema),
			SideEffectClass:  d.Manifest.SideEffectClass,
			Idempotent:       d.Manifest.Idempotent,
			RequiresApproval: d.Manifest.RequiresApproval,
			ExecutionKey:     d.Manifest.ExecutionKey,
			Source:           "builtin",
			SourceRef:        "builtin:core",
			Trust:            string(skill.TrustBundled),
			Eligible:         true,
			Availability: agent.ToolAvailability{
				Status: agent.AvailabilityReady,
			},
		})
	}
}

func findBuiltinDefinition(definitions []agent.ToolDefinition, name string) (agent.ToolDefinition, bool) {
	trimmed := strings.TrimSpace(name)
	for _, definition := range definitions {
		if strings.EqualFold(strings.TrimSpace(definition.Name), trimmed) {
			return copyToolDefinition(definition), true
		}
	}
	return agent.ToolDefinition{}, false
}

func hiddenBuiltinToolName(name string) bool {
	switch strings.TrimSpace(name) {
	case "fs.diff", "fs.changes", "fs.revert", "net.serve":
		return true
	default:
		return false
	}
}

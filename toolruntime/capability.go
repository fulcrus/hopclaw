package toolruntime

import (
	"fmt"
	"sort"
	"strings"
)

// ToolState represents the three-state lifecycle of a registered tool.
type ToolState int

const (
	ToolActive       ToolState = iota // available, appears in model tool list
	ToolDormant                       // registered but deps missing, model knows it exists
	ToolDiscoverable                  // not registered, exists in a remote catalog
)

// DormantGroup describes a Layer 2 tool group whose dependencies are not met.
type DormantGroup struct {
	Name        string   // e.g. "git", "browser"
	ToolCount   int      // number of tools in the group
	ToolNames   []string // individual tool names
	MissingBins []string // binaries not found by LookPath
	InstallHint string   // human-readable install instruction
}

// CapabilityReport summarises the runtime's tool availability at a point in time.
type CapabilityReport struct {
	ActiveCount   int
	DormantCount  int
	ActiveTools   []string
	DormantGroups []DormantGroup
}

// DormantGroups returns structured info about Layer 2 groups that are
// currently unavailable because their runtime dependencies are missing.
// Groups disabled by config are excluded so the model does not treat them as
// installable capabilities.
func (r *Layer2Registry) DormantGroups() []DormantGroup {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var out []DormantGroup
	for _, g := range sortedToolGroups(r.groups) {
		if g.isActive || g.isDisabled {
			continue
		}
		missing := make([]string, 0)
		for _, bin := range g.requiredBins {
			if g.binPaths[bin] == "" {
				missing = append(missing, bin)
			}
		}
		sort.Strings(missing)
		names := make([]string, 0, len(g.entries))
		for _, e := range sortedToolGroupEntries(g.entries) {
			names = append(names, e.Manifest.Name)
		}
		hint := fmt.Sprintf("Use skill.ensure first. Missing runtime dependencies for this group: %s.",
			strings.Join(missing, ", "))
		out = append(out, DormantGroup{
			Name:        g.name,
			ToolCount:   len(g.entries),
			ToolNames:   names,
			MissingBins: missing,
			InstallHint: hint,
		})
	}
	return out
}

// BuildCapabilityReport produces a snapshot of active vs dormant tools.
func BuildCapabilityReport(builtins *Builtins, layer2 *Layer2Registry) CapabilityReport {
	report := CapabilityReport{}
	if builtins != nil {
		for _, d := range builtins.definitions {
			if hiddenBuiltinToolName(d.Name) {
				continue
			}
			report.ActiveCount++
			report.ActiveTools = append(report.ActiveTools, d.Name)
		}
	}
	if layer2 != nil {
		layer2.mu.RLock()
		for _, g := range sortedToolGroups(layer2.groups) {
			if g.isActive {
				report.ActiveCount += len(g.entries)
				for _, e := range sortedToolGroupEntries(g.entries) {
					report.ActiveTools = append(report.ActiveTools, e.Manifest.Name)
				}
			}
		}
		layer2.mu.RUnlock()
		dormant := layer2.DormantGroups()
		report.DormantGroups = dormant
		for _, dg := range dormant {
			report.DormantCount += dg.ToolCount
		}
	}
	sort.Strings(report.ActiveTools)
	return report
}

// BuildToolPrompt generates the dormant-tools section for inclusion in the
// model's system prompt. Active tool definitions are already provided via
// ToolDefinitions(); this adds awareness of installable but currently
// unavailable tools.
func BuildToolPrompt(builtins *Builtins, layer2 *Layer2Registry) string {
	if layer2 == nil {
		return ""
	}
	dormant := layer2.DormantGroups()
	if len(dormant) == 0 {
		return ""
	}

	var buf strings.Builder
	buf.WriteString("## Dormant Tools (installable)\n\n")
	buf.WriteString("The following tool groups are registered but currently unavailable because runtime dependencies are missing. ")
	buf.WriteString("Treat them as recoverable capabilities. If the user needs one, call `skill.ensure` first instead of manually installing packages or binaries.\n\n")
	for _, g := range dormant {
		buf.WriteString(fmt.Sprintf("- **%s** (%d tools): requires %v\n",
			g.Name, g.ToolCount, g.MissingBins))
		buf.WriteString(fmt.Sprintf("  → %s\n", g.InstallHint))
	}

	report := BuildCapabilityReport(builtins, layer2)
	buf.WriteString(fmt.Sprintf("\nCapability summary: %d active, %d dormant\n",
		report.ActiveCount, report.DormantCount))

	return buf.String()
}

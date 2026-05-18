package agent

import (
	"sort"
	"strings"
)

func canonicalToolDefinitions(defs []ToolDefinition) []ToolDefinition {
	if len(defs) == 0 {
		return nil
	}
	out := make([]ToolDefinition, len(defs))
	for i, def := range defs {
		out[i] = normalizeToolDefinition(def)
	}
	sort.SliceStable(out, func(i, j int) bool {
		return compareCanonicalToolDefinition(out[i], out[j]) < 0
	})
	return out
}

func compareCanonicalToolDefinition(left, right ToolDefinition) int {
	for _, pair := range [][2]string{
		{strings.TrimSpace(left.Name), strings.TrimSpace(right.Name)},
		{strings.TrimSpace(left.Source), strings.TrimSpace(right.Source)},
		{strings.TrimSpace(left.SourceRef), strings.TrimSpace(right.SourceRef)},
		{strings.TrimSpace(left.ExecutionKey), strings.TrimSpace(right.ExecutionKey)},
		{strings.TrimSpace(left.Description), strings.TrimSpace(right.Description)},
		{strings.TrimSpace(left.Domain), strings.TrimSpace(right.Domain)},
		{strings.TrimSpace(left.Category), strings.TrimSpace(right.Category)},
	} {
		switch {
		case pair[0] < pair[1]:
			return -1
		case pair[0] > pair[1]:
			return 1
		}
	}
	return 0
}

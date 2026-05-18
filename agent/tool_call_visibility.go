package agent

import (
	"fmt"
	"sort"
	"strings"

	"github.com/fulcrus/hopclaw/contextengine"
	"github.com/fulcrus/hopclaw/resultmodel"
)

func unavailableToolCalls(calls []ToolCall, available []ToolDefinition) []ToolCall {
	if len(calls) == 0 {
		return nil
	}
	if len(available) == 0 {
		return nil
	}
	allowed := make(map[string]struct{}, len(available))
	for _, tool := range available {
		name := strings.TrimSpace(tool.Name)
		if name == "" {
			continue
		}
		allowed[name] = struct{}{}
	}
	if len(allowed) == 0 {
		return nil
	}
	var out []ToolCall
	for _, call := range calls {
		name := strings.TrimSpace(call.Name)
		if name == "" {
			continue
		}
		if _, ok := allowed[name]; ok {
			continue
		}
		out = append(out, call)
	}
	return out
}

func buildUnavailableToolCallResults(calls []ToolCall, available []ToolDefinition) []contextengine.ToolResult {
	if len(calls) == 0 {
		return nil
	}
	availableNames := make([]string, 0, len(available))
	for _, tool := range available {
		name := strings.TrimSpace(tool.Name)
		if name == "" {
			continue
		}
		availableNames = append(availableNames, name)
	}
	sort.Strings(availableNames)
	availableSummary := strings.Join(availableNames, ", ")
	if len(availableNames) > 8 {
		availableSummary = strings.Join(availableNames[:8], ", ") + fmt.Sprintf(", +%d more", len(availableNames)-8)
	}

	results := make([]contextengine.ToolResult, 0, len(calls))
	for _, call := range calls {
		name := strings.TrimSpace(call.Name)
		message := fmt.Sprintf("tool %q is not available in this turn", name)
		recoveryHint := unavailableToolRecoveryHint(name, availableNames)
		if availableSummary != "" {
			message += ". Available tools: " + availableSummary
			if recoveryHint == "" {
				recoveryHint = "Retry using one of the available tools for this turn: " + availableSummary
			}
		}
		if recoveryHint == "" {
			recoveryHint = "Retry using only the tools available in this turn."
		}
		results = append(results, contextengine.ToolResult{
			ToolName:       name,
			ToolCallID:     strings.TrimSpace(call.ID),
			Status:         resultmodel.ToolResultError,
			TranscriptText: "error: " + message,
			Summary:        "tool not available in this turn",
			Error: &resultmodel.ResultError{
				Code:    "tool_not_available",
				Message: message,
			},
			Structured: map[string]any{
				"tool_not_available": true,
				"tool_name":          name,
				"tool_call_id":       strings.TrimSpace(call.ID),
				"available_tools":    append([]string(nil), availableNames...),
				"recovery_hint":      recoveryHint,
			},
			Actions: []resultmodel.ResultAction{{
				Kind:   resultmodel.ResultActionFollowUp,
				Label:  "retry with an available tool",
				Target: recoveryHint,
			}},
		})
	}
	return results
}

func unavailableToolRecoveryHint(name string, availableNames []string) string {
	switch strings.TrimSpace(name) {
	case "skill.ensure", "skill.install":
		if hasToolNamePrefix(availableNames, "browser.") {
			return "Do not call `skill.ensure` in this turn. Continue with the available browser tools instead."
		}
		if hasToolNamePrefix(availableNames, "desktop.") {
			return "Do not call `skill.ensure` in this turn. Continue with the available desktop tools instead."
		}
	}
	return ""
}

func hasToolNamePrefix(items []string, prefix string) bool {
	for _, item := range items {
		if strings.HasPrefix(strings.TrimSpace(item), prefix) {
			return true
		}
	}
	return false
}

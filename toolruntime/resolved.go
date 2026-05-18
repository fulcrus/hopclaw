package toolruntime

import (
	"strings"

	"github.com/fulcrus/hopclaw/agent"
	"github.com/fulcrus/hopclaw/skill"
	"github.com/fulcrus/hopclaw/toolspec"
)

func resolvedToolFromBinding(binding *skill.BoundTool, definition agent.ToolDefinition, executorRef string) *agent.ResolvedTool {
	if binding == nil {
		return toolspec.NormalizeResolvedTool(&agent.ResolvedTool{
			Descriptor:  definition,
			ExecutorRef: strings.TrimSpace(executorRef),
		})
	}
	return toolspec.ResolvedFromSkillBinding(binding, definition, executorRef)
}

func copyToolDefinition(definition agent.ToolDefinition) agent.ToolDefinition {
	return toolspec.NormalizeDefinition(definition)
}

func availabilityFromEligibility(eligible bool, reasons []string) agent.ToolAvailability {
	status := agent.AvailabilityReady
	if !eligible {
		status = agent.AvailabilityBlocked
	}
	return agent.ToolAvailability{
		Status:  status,
		Reasons: append([]string(nil), reasons...),
	}
}

package agent

import (
	"strings"

	planpkg "github.com/fulcrus/hopclaw/planner"
)

func (a *AgentComponent) toolRoundBudget(run *Run, task *planpkg.Task, prompt string) int {
	base := 16
	if a != nil && a.config.MaxToolRounds > 0 {
		base = a.config.MaxToolRounds
	}
	return base + buildRunHarnessSpec(run, task, prompt, nil).Budget.ExtraToolRounds
}

func hasCapability(capabilities []string, targets ...string) bool {
	if len(capabilities) == 0 || len(targets) == 0 {
		return false
	}
	targetSet := make(map[string]struct{}, len(targets))
	for _, target := range targets {
		targetSet[strings.ToLower(strings.TrimSpace(target))] = struct{}{}
	}
	for _, capability := range capabilities {
		if _, ok := targetSet[strings.ToLower(strings.TrimSpace(capability))]; ok {
			return true
		}
	}
	return false
}

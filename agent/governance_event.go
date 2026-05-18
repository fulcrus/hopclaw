package agent

import domaingov "github.com/fulcrus/hopclaw/internal/domain/governance"

func buildGovernanceEventAttrs(run *Run) map[string]any {
	if run == nil {
		return nil
	}
	return mergeEventAttrs(
		domaingov.EventAttrs(run.Scope, run.Governance),
		workflowBudgetEventAttrs(run),
	)
}

func BuildRunEventAttrs(run *Run) map[string]any {
	return buildGovernanceEventAttrs(run)
}

func mergeEventAttrs(base map[string]any, extras ...map[string]any) map[string]any {
	return domaingov.MergeEventAttrs(base, extras...)
}

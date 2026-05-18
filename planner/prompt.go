package planner

import (
	"encoding/json"
	"strings"
)

const systemPrompt = "You are HopClaw's task planner. Convert a user's task into a compact execution plan. Return JSON only. The plan must contain: goal, strategy, tasks, final_task. Use strategy serial, parallel, or mixed. Each task must have: id, kind, goal, depends_on, outputs, required_capabilities. A task may also include verification_hints when completion should leave concrete evidence for downstream verification or delivery. Use short verification_hints such as browser, desktop, spreadsheet, document, presentation, email, or watch, and include them only when they materially help execution quality. If task_contract is present, honor its suggested_domains, capability_hints, expected_deliverables, evidence_requirements, missing_info, acceptance_criteria, requires_external_effect, and requires_approval when decomposing work. If pinned_facts, session_state, or recalled_context are present, treat them as high-value context for planning rather than re-deriving those facts from scratch. If delegation_contract is present, keep the plan inside its allowed_domains, side_effect_class, and bounded-turn intent; do not assume unlimited sub-agents. Only split work when it materially improves execution, evidence quality, or progress reporting. If the user asks for multiple independent things, create separate tasks and merge them with a final deliver task. If a later task needs the result of an earlier task, put that dependency in depends_on. Allowed task kinds: research, translate, transform, write, execute, review, deliver."

func SystemPrompt() string {
	return systemPrompt
}

func BuildPayload(ctx Context) (string, error) {
	encoded, err := json.Marshal(ctx)
	if err != nil {
		return "", err
	}
	return string(encoded), nil
}

func TrivialPlan(goal string) *Plan {
	goal = strings.TrimSpace(goal)
	if goal == "" {
		goal = "Continue the current task."
	}
	return &Plan{
		Goal:     goal,
		Summary:  goal,
		Strategy: StrategySerial,
		Tasks: []Task{{
			ID:      "task_1",
			Kind:    TaskExecute,
			Goal:    goal,
			Outputs: []string{"result"},
		}},
		FinalTask: "task_1",
	}
}

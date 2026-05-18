package agent

import (
	"strings"

	planpkg "github.com/fulcrus/hopclaw/planner"
)

func compactPlanForExecution(plan *planpkg.Plan) {
	if plan == nil || len(plan.Tasks) == 0 {
		return
	}
	changed := true
	for changed {
		changed = compactFinalDeliverWrapper(plan)
	}
	planpkg.NormalizeExecution(plan)
}

func compactFinalDeliverWrapper(plan *planpkg.Plan) bool {
	if plan == nil || len(plan.Tasks) < 2 {
		return false
	}
	finalID := strings.TrimSpace(plan.FinalTask)
	if finalID == "" {
		return false
	}
	finalTask := planpkg.FindTask(plan, finalID)
	if finalTask == nil || !isCompactionCandidate(*finalTask) {
		return false
	}
	if len(finalTask.DependsOn) != 1 {
		return false
	}
	parent := planpkg.FindTask(plan, finalTask.DependsOn[0])
	if parent == nil {
		return false
	}
	if hasDistinctExecutionBoundary(*finalTask, *parent) {
		return false
	}
	parent.Outputs = appendUniqueStrings(parent.Outputs, finalTask.Outputs...)
	parent.RequiredCapabilities = appendUniqueStrings(parent.RequiredCapabilities, finalTask.RequiredCapabilities...)
	parent.VerificationHints = appendUniqueStrings(parent.VerificationHints, finalTask.VerificationHints...)
	replaceTaskDependency(plan, finalTask.ID, parent.ID)
	plan.FinalTask = parent.ID
	plan.Tasks = removePlanTask(plan.Tasks, finalTask.ID)
	if len(plan.Tasks) == 1 {
		plan.Strategy = planpkg.StrategySerial
	}
	return true
}

func isCompactionCandidate(task planpkg.Task) bool {
	if task.Kind != planpkg.TaskDeliver && task.Kind != "" {
		return false
	}
	return strings.TrimSpace(task.Title) == "" && strings.TrimSpace(task.Goal) == ""
}

func hasDistinctExecutionBoundary(task, parent planpkg.Task) bool {
	if len(task.RequiredCapabilities) > 0 {
		return true
	}
	if len(task.Outputs) > 0 {
		for _, output := range task.Outputs {
			if !containsString(parent.Outputs, output) {
				return true
			}
		}
	}
	return false
}

func replaceTaskDependency(plan *planpkg.Plan, fromID, toID string) {
	if plan == nil || fromID == "" || toID == "" || fromID == toID {
		return
	}
	for i := range plan.Tasks {
		task := &plan.Tasks[i]
		if task.ID == fromID {
			continue
		}
		if len(task.DependsOn) == 0 {
			continue
		}
		updated := make([]string, 0, len(task.DependsOn))
		for _, dep := range task.DependsOn {
			if strings.TrimSpace(dep) == fromID {
				dep = toID
			}
			if dep == "" || containsString(updated, dep) {
				continue
			}
			updated = append(updated, dep)
		}
		task.DependsOn = updated
	}
}

func removePlanTask(tasks []planpkg.Task, id string) []planpkg.Task {
	if len(tasks) == 0 {
		return nil
	}
	out := tasks[:0]
	for _, task := range tasks {
		if task.ID == id {
			continue
		}
		out = append(out, task)
	}
	return out
}

func appendUniqueStrings(dst []string, items ...string) []string {
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item == "" || containsString(dst, item) {
			continue
		}
		dst = append(dst, item)
	}
	if len(dst) == 0 {
		return nil
	}
	return dst
}

func containsString(items []string, target string) bool {
	target = strings.TrimSpace(target)
	for _, item := range items {
		if strings.TrimSpace(item) == target {
			return true
		}
	}
	return false
}

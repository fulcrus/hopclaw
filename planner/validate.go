package planner

import (
	"fmt"
	"strings"

	"github.com/fulcrus/hopclaw/internal/jsonrepair"
)

func Parse(raw string) (*Plan, error) {
	var plan Plan
	if err := jsonrepair.DecodeJSONObjectCandidate(raw, &plan); err != nil {
		return nil, err
	}
	if err := Validate(&plan); err != nil {
		return nil, err
	}
	return &plan, nil
}

func Validate(plan *Plan) error {
	if plan == nil {
		return fmt.Errorf("plan is required")
	}
	plan.Goal = strings.TrimSpace(plan.Goal)
	plan.Summary = strings.TrimSpace(plan.Summary)
	if plan.Goal == "" {
		return fmt.Errorf("goal is required")
	}
	if len(plan.Tasks) == 0 {
		return fmt.Errorf("at least one task is required")
	}
	switch plan.Strategy {
	case "", StrategySerial, StrategyParallel, StrategyMixed:
	default:
		return fmt.Errorf("invalid strategy %q", plan.Strategy)
	}
	if plan.Strategy == "" {
		plan.Strategy = StrategySerial
	}
	switch plan.FailurePolicy {
	case "", FailFast, ContinueOnError:
	default:
		return fmt.Errorf("invalid failure_policy %q", plan.FailurePolicy)
	}
	if plan.FailurePolicy == "" {
		plan.FailurePolicy = FailFast
	}

	seen := make(map[string]struct{}, len(plan.Tasks))
	for i := range plan.Tasks {
		task := &plan.Tasks[i]
		task.ID = strings.TrimSpace(task.ID)
		task.Title = strings.TrimSpace(task.Title)
		task.Goal = strings.TrimSpace(task.Goal)
		task.ResultSummary = strings.TrimSpace(task.ResultSummary)
		task.Error = strings.TrimSpace(task.Error)
		if task.ID == "" {
			task.ID = fmt.Sprintf("task_%d", i+1)
		}
		if task.Goal == "" {
			return fmt.Errorf("task %s goal is required", task.ID)
		}
		switch task.Kind {
		case "", TaskResearch, TaskTranslate, TaskTransform, TaskWrite, TaskExecute, TaskReview, TaskDeliver:
		default:
			return fmt.Errorf("task %s has invalid kind %q", task.ID, task.Kind)
		}
		switch task.Status {
		case "", TaskQueued, TaskRunning, TaskCompleted, TaskFailed, TaskCancelled, TaskSkipped:
		default:
			return fmt.Errorf("task %s has invalid status %q", task.ID, task.Status)
		}
		if task.Kind == "" {
			task.Kind = TaskExecute
		}
		if _, ok := seen[task.ID]; ok {
			return fmt.Errorf("duplicate task id %q", task.ID)
		}
		seen[task.ID] = struct{}{}
		task.DependsOn = trimNonEmpty(task.DependsOn)
		task.Outputs = trimNonEmpty(task.Outputs)
		task.RequiredCapabilities = trimNonEmpty(task.RequiredCapabilities)
		task.VerificationHints = trimNonEmpty(task.VerificationHints)
	}
	for _, task := range plan.Tasks {
		for _, dep := range task.DependsOn {
			if _, ok := seen[dep]; !ok {
				return fmt.Errorf("task %s depends on unknown task %q", task.ID, dep)
			}
		}
	}
	plan.FinalTask = strings.TrimSpace(plan.FinalTask)
	if plan.FinalTask == "" {
		plan.FinalTask = plan.Tasks[len(plan.Tasks)-1].ID
	}
	if _, ok := seen[plan.FinalTask]; !ok {
		return fmt.Errorf("final_task %q not found", plan.FinalTask)
	}
	plan.ActiveTask = strings.TrimSpace(plan.ActiveTask)
	if plan.ActiveTask != "" {
		if _, ok := seen[plan.ActiveTask]; !ok {
			return fmt.Errorf("active_task %q not found", plan.ActiveTask)
		}
	}
	return nil
}

func trimNonEmpty(items []string) []string {
	if len(items) == 0 {
		return nil
	}
	out := make([]string, 0, len(items))
	seen := make(map[string]struct{}, len(items))
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		out = append(out, item)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

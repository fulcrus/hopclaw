package channels

import (
	"strings"

	"github.com/fulcrus/hopclaw/eventbus"
)

func cloneNotificationTarget(in RunNotificationTarget) RunNotificationTarget {
	in.Metadata = cloneAnyMap(in.Metadata)
	return in
}

func cloneAnyMap(in map[string]any) map[string]any {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]any, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func stringsTrim(v string) string {
	return strings.TrimSpace(v)
}

func ExtractToolProgress(event eventbus.Event) (int, []string) {
	payload, _ := event.ToolExecutedPayload()
	return payload.ToolRound, payload.ToolNames
}

func ExtractPlanProgress(event eventbus.Event) (string, int, int) {
	payload, _ := event.PlanTaskPayload()
	activeTask := stringsTrim(payload.Title)
	if activeTask == "" {
		activeTask = stringsTrim(payload.Goal)
	}
	return activeTask, payload.CompletedCount, payload.TotalTasks
}

func ExtractRunPhaseChange(event eventbus.Event) (string, []string) {
	payload, _ := event.RunPhaseChangedPayload()
	return stringsTrim(payload.Phase), payload.ToolNames
}

func ExtractTaskProgress(event eventbus.Event) (string, int, int) {
	payload, _ := event.TaskProgressPayload()
	return stringsTrim(payload.CurrentTask), payload.Completed, payload.Total
}

package agent

import (
	"testing"

	planpkg "github.com/fulcrus/hopclaw/planner"
)

func TestCompactPlanForExecutionRemovesThinFinalDeliverTask(t *testing.T) {
	t.Parallel()

	plan := &planpkg.Plan{
		Goal:      "create a report",
		Strategy:  planpkg.StrategySerial,
		FinalTask: "deliver",
		Tasks: []planpkg.Task{
			{ID: "report", Kind: planpkg.TaskWrite, Goal: "write report", Outputs: []string{"report.docx"}},
			{ID: "deliver", Kind: planpkg.TaskDeliver, DependsOn: []string{"report"}},
		},
	}

	compactPlanForExecution(plan)

	if len(plan.Tasks) != 1 {
		t.Fatalf("len(plan.Tasks) = %d, want 1", len(plan.Tasks))
	}
	if plan.FinalTask != "report" {
		t.Fatalf("plan.FinalTask = %q, want report", plan.FinalTask)
	}
}

func TestCompactPlanForExecutionKeepsDeliverTaskWithDistinctCapabilities(t *testing.T) {
	t.Parallel()

	plan := &planpkg.Plan{
		Goal:      "email the report",
		Strategy:  planpkg.StrategySerial,
		FinalTask: "deliver",
		Tasks: []planpkg.Task{
			{ID: "report", Kind: planpkg.TaskWrite, Goal: "write report", Outputs: []string{"report.docx"}},
			{ID: "deliver", Kind: planpkg.TaskDeliver, Goal: "send the report", DependsOn: []string{"report"}, RequiredCapabilities: []string{"email.send"}},
		},
	}

	compactPlanForExecution(plan)

	if len(plan.Tasks) != 2 {
		t.Fatalf("len(plan.Tasks) = %d, want 2", len(plan.Tasks))
	}
	if plan.FinalTask != "deliver" {
		t.Fatalf("plan.FinalTask = %q, want deliver", plan.FinalTask)
	}
}

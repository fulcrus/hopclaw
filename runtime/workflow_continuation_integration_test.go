package runtime

import (
	"context"
	"testing"
	"time"

	"github.com/fulcrus/hopclaw/agent"
	"github.com/fulcrus/hopclaw/eventbus"
	"github.com/fulcrus/hopclaw/planner"
)

type workflowTestPlanner struct {
	plan *planner.Plan
}

func (p workflowTestPlanner) Plan(context.Context, agent.PlanningRequest) (*planner.Plan, error) {
	if p.plan == nil {
		return nil, nil
	}
	cloned := *p.plan
	cloned.Tasks = append([]planner.Task(nil), p.plan.Tasks...)
	return &cloned, nil
}

func TestWorkflowContinuationLifecycle(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	sessions := agent.NewInMemorySessionStore()
	runs := agent.NewInMemoryRunStore()
	queue := agent.NewInMemoryCoordinator()
	bus := eventbus.NewInMemoryBus()
	component := agent.NewComponent(agent.AgentConfig{
		DefaultModel:  "test-model",
		MaxToolRounds: 1,
		QueueMode:     agent.QueueEnqueue,
	}, sessions, runs, queue, newContextEngine(), mockModelClient{}, nil, nil).
		WithPlanner(workflowTestPlanner{plan: serialWorkflowPlan(6)}).
		WithEventBus(bus)
	svc := NewService(component, sessions, runs, nil, bus, nil)

	session, err := sessions.GetOrCreate(ctx, "workflow-lifecycle", "test-model")
	if err != nil {
		t.Fatalf("GetOrCreate() error = %v", err)
	}
	run, err := runs.Create(ctx, session.ID, agent.IncomingMessage{
		SessionKey: session.Key,
		Content:    "workflow continuation lifecycle",
	}, agent.AgentConfig{
		DefaultModel: "test-model",
		QueueMode:    agent.QueueEnqueue,
	})
	if err != nil {
		t.Fatalf("runs.Create() error = %v", err)
	}
	run.ExecutionMode = agent.ExecutionModeWorkflow
	run.Plan = serialWorkflowPlan(6)
	run.WorkflowState = &agent.WorkflowState{
		OriginalRunID:     run.ID,
		ContinuationIndex: 0,
		MaxContinuations:  agent.DefaultMaxContinuations,
		MaxTotalRounds:    agent.DefaultMaxTotalRounds,
	}
	if err := runs.Update(ctx, run); err != nil {
		t.Fatalf("runs.Update() error = %v", err)
	}

	if err := svc.dispatchRun(ctx, run.ID, false); err != nil {
		t.Fatalf("dispatchRun() error = %v", err)
	}
	waitForRunStatus(t, svc, run.ID, agent.RunCompleted)

	continuation := waitForContinuationRun(t, runs, session.ID, run.ID)
	waitForRunStatus(t, svc, continuation.ID, agent.RunCompleted)

	original, err := runs.Get(ctx, run.ID)
	if err != nil {
		t.Fatalf("runs.Get(original) error = %v", err)
	}
	if original.WorkflowState == nil || !original.WorkflowState.Yielded {
		t.Fatalf("original.WorkflowState = %#v, want yielded state", original.WorkflowState)
	}

	finalRun, err := runs.Get(ctx, continuation.ID)
	if err != nil {
		t.Fatalf("runs.Get(continuation) error = %v", err)
	}
	if finalRun.WorkflowState == nil {
		t.Fatal("final workflow state = nil")
	}
	if finalRun.WorkflowState.ContinuationIndex != 1 {
		t.Fatalf("ContinuationIndex = %d, want 1", finalRun.WorkflowState.ContinuationIndex)
	}
	if finalRun.WorkflowState.TotalRoundsUsed != 6 {
		t.Fatalf("TotalRoundsUsed = %d, want 6", finalRun.WorkflowState.TotalRoundsUsed)
	}
	if finalRun.Plan == nil || len(finalRun.Plan.Tasks) != 6 {
		t.Fatalf("Plan = %#v", finalRun.Plan)
	}
	for i, task := range finalRun.Plan.Tasks {
		if task.Status != planner.TaskCompleted {
			t.Fatalf("task %d status = %q, want completed", i, task.Status)
		}
	}

	yielded := 0
	continued := 0
	completed := 0
	for _, event := range bus.Snapshot() {
		switch event.Type {
		case eventbus.EventWorkflowYielded:
			yielded++
		case eventbus.EventWorkflowContinued:
			continued++
		case eventbus.EventWorkflowCompleted:
			completed++
		}
	}
	if yielded != 1 || continued != 1 || completed != 1 {
		t.Fatalf("workflow events = yielded:%d continued:%d completed:%d", yielded, continued, completed)
	}
}

func serialWorkflowPlan(total int) *planner.Plan {
	tasks := make([]planner.Task, 0, total)
	var prev string
	for i := 1; i <= total; i++ {
		task := planner.Task{
			ID:     "task-" + string(rune('0'+i)),
			Kind:   planner.TaskReview,
			Goal:   "step",
			Status: planner.TaskQueued,
		}
		if prev != "" {
			task.DependsOn = []string{prev}
		}
		prev = task.ID
		tasks = append(tasks, task)
	}
	return &planner.Plan{
		Goal:      "workflow",
		Strategy:  planner.StrategySerial,
		Tasks:     tasks,
		FinalTask: prev,
	}
}

func waitForContinuationRun(t *testing.T, runs agent.RunLister, sessionID, parentRunID string) *agent.Run {
	t.Helper()

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		items, err := runs.List(context.Background(), agent.RunListFilter{SessionID: sessionID})
		if err != nil {
			t.Fatalf("runs.List() error = %v", err)
		}
		for _, item := range items {
			if item != nil && item.ParentRunID == parentRunID {
				return item
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("timed out waiting for continuation run")
	return nil
}

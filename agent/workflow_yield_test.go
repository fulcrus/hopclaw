package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/fulcrus/hopclaw/eventbus"
	planpkg "github.com/fulcrus/hopclaw/planner"
)

func TestIsWorkflowWithIncompletePlan(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		run  *Run
		want bool
	}{
		{name: "nil run", run: nil, want: false},
		{name: "direct mode", run: &Run{ExecutionMode: ExecutionModeDirect}, want: false},
		{name: "workflow no plan", run: &Run{ExecutionMode: ExecutionModeWorkflow}, want: false},
		{
			name: "workflow plan done",
			run: &Run{
				ExecutionMode: ExecutionModeWorkflow,
				Plan: &planpkg.Plan{Tasks: []planpkg.Task{
					{ID: "t1", Status: planpkg.TaskCompleted},
				}},
			},
			want: false,
		},
		{
			name: "workflow plan incomplete",
			run: &Run{
				ExecutionMode: ExecutionModeWorkflow,
				Plan: &planpkg.Plan{Tasks: []planpkg.Task{
					{ID: "t1", Status: planpkg.TaskCompleted},
					{ID: "t2", Status: planpkg.TaskQueued},
				}},
			},
			want: true,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := isWorkflowWithIncompletePlan(tt.run); got != tt.want {
				t.Fatalf("isWorkflowWithIncompletePlan() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestBuildWorkflowRunSummary(t *testing.T) {
	t.Parallel()

	run := &Run{
		ToolRounds: 8,
		Plan: &planpkg.Plan{
			Tasks: []planpkg.Task{
				{ID: "t1", Status: planpkg.TaskCompleted},
				{ID: "t2", Status: planpkg.TaskCompleted},
				{ID: "t3", Status: planpkg.TaskQueued},
				{ID: "t4", Status: planpkg.TaskQueued},
			},
		},
		WorkflowState: &WorkflowState{ContinuationIndex: 2},
	}

	summary := BuildWorkflowRunSummary(run)
	if summary == "" {
		t.Fatal("expected non-empty summary")
	}
	for _, want := range []string{"2/4", "8 rounds", "continuation 2"} {
		if !strings.Contains(summary, want) {
			t.Fatalf("summary %q missing %q", summary, want)
		}
	}
}

func TestYieldWorkflowRunEmitsWorkflowYieldedEvent(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	sessions := NewInMemorySessionStore()
	runs := NewInMemoryRunStore()
	bus := eventbus.NewInMemoryBus()
	component := NewComponent(AgentConfig{
		DefaultModel: "test-model",
		QueueMode:    QueueEnqueue,
	}, sessions, runs, NewInMemoryCoordinator(), NewSlidingWindowEngineForTest(), &stubModelClient{}, nil, nil).
		WithEventBus(bus)

	session, err := sessions.GetOrCreate(ctx, "workflow-yield", "test-model")
	if err != nil {
		t.Fatalf("GetOrCreate() error = %v", err)
	}
	run, err := runs.Create(ctx, session.ID, IncomingMessage{
		SessionKey: session.Key,
		Content:    "continue workflow",
	}, AgentConfig{
		DefaultModel: "test-model",
		QueueMode:    QueueEnqueue,
	})
	if err != nil {
		t.Fatalf("runs.Create() error = %v", err)
	}
	run.Status = RunRunning
	run.ExecutionMode = ExecutionModeWorkflow
	run.ToolRounds = 2
	run.Plan = &planpkg.Plan{
		Tasks: []planpkg.Task{
			{ID: "t1", Status: planpkg.TaskCompleted},
			{ID: "t2", Status: planpkg.TaskQueued},
		},
	}
	run.WorkflowState = &WorkflowState{
		OriginalRunID:     run.ID,
		ContinuationIndex: 0,
		MaxContinuations:  DefaultMaxContinuations,
		TotalRoundsUsed:   5,
		MaxTotalRounds:    DefaultMaxTotalRounds,
	}
	if err := runs.Update(ctx, run); err != nil {
		t.Fatalf("runs.Update() error = %v", err)
	}

	if err := component.yieldWorkflowRun(ctx, run, session, YieldReasonRoundBudget); err != nil {
		t.Fatalf("yieldWorkflowRun() error = %v", err)
	}

	got, err := runs.Get(ctx, run.ID)
	if err != nil {
		t.Fatalf("runs.Get() error = %v", err)
	}
	if got.Status != RunCompleted {
		t.Fatalf("Status = %q, want %q", got.Status, RunCompleted)
	}
	if got.WorkflowState == nil || got.WorkflowState.TotalRoundsUsed != 5 {
		t.Fatalf("WorkflowState = %#v", got.WorkflowState)
	}
	if got.Plan == nil || got.Plan.Tasks[1].Status != planpkg.TaskQueued {
		t.Fatalf("Plan = %#v", got.Plan)
	}

	events := bus.Snapshot()
	foundYielded := false
	for _, event := range events {
		if event.Type != eventbus.EventWorkflowYielded {
			continue
		}
		foundYielded = true
		payload, ok := event.WorkflowYieldedPayload()
		if !ok {
			t.Fatal("WorkflowYieldedPayload() ok = false")
		}
		if payload.TotalRoundsUsed != 5 {
			t.Fatalf("payload.TotalRoundsUsed = %d, want 5", payload.TotalRoundsUsed)
		}
		if payload.YieldReason != YieldReasonRoundBudget {
			t.Fatalf("payload.YieldReason = %q, want %q", payload.YieldReason, YieldReasonRoundBudget)
		}
	}
	if !foundYielded {
		t.Fatal("expected workflow.yielded event")
	}
}

func TestYieldWorkflowRunFailsWhenBudgetExhausted(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	sessions := NewInMemorySessionStore()
	runs := NewInMemoryRunStore()
	bus := eventbus.NewInMemoryBus()
	component := NewComponent(AgentConfig{
		DefaultModel: "test-model",
		QueueMode:    QueueEnqueue,
	}, sessions, runs, NewInMemoryCoordinator(), NewSlidingWindowEngineForTest(), &stubModelClient{}, nil, nil).
		WithEventBus(bus)

	session, err := sessions.GetOrCreate(ctx, "workflow-budget-exhausted", "test-model")
	if err != nil {
		t.Fatalf("GetOrCreate() error = %v", err)
	}
	run, err := runs.Create(ctx, session.ID, IncomingMessage{
		SessionKey: session.Key,
		Content:    "continue workflow",
	}, AgentConfig{
		DefaultModel: "test-model",
		QueueMode:    QueueEnqueue,
	})
	if err != nil {
		t.Fatalf("runs.Create() error = %v", err)
	}
	run.Status = RunRunning
	run.ExecutionMode = ExecutionModeWorkflow
	run.Plan = &planpkg.Plan{
		Tasks: []planpkg.Task{
			{ID: "t1", Status: planpkg.TaskCompleted},
			{ID: "t2", Status: planpkg.TaskQueued},
		},
	}
	run.WorkflowState = &WorkflowState{
		OriginalRunID:     run.ID,
		ContinuationIndex: DefaultMaxContinuations,
		MaxContinuations:  DefaultMaxContinuations,
		TotalRoundsUsed:   DefaultMaxTotalRounds,
		MaxTotalRounds:    DefaultMaxTotalRounds,
	}
	if err := runs.Update(ctx, run); err != nil {
		t.Fatalf("runs.Update() error = %v", err)
	}

	err = component.yieldWorkflowRun(ctx, run, session, YieldReasonRoundBudget)
	if err == nil {
		t.Fatal("yieldWorkflowRun() error = nil, want failure")
	}

	got, getErr := runs.Get(ctx, run.ID)
	if getErr != nil {
		t.Fatalf("runs.Get() error = %v", getErr)
	}
	if got.Status != RunFailed {
		t.Fatalf("Status = %q, want %q", got.Status, RunFailed)
	}

	for _, event := range bus.Snapshot() {
		if event.Type == eventbus.EventWorkflowYielded {
			t.Fatal("did not expect workflow.yielded event when budget is exhausted")
		}
	}
}

func TestTrackWorkflowExecutionRoundCountsWorkflowTurnsOnly(t *testing.T) {
	t.Parallel()

	run := &Run{
		ExecutionMode: ExecutionModeWorkflow,
		WorkflowState: &WorkflowState{
			OriginalRunID:   "run-root",
			TotalRoundsUsed: 9,
		},
	}

	trackWorkflowExecutionRound(run)
	if got := run.WorkflowState.TotalRoundsUsed; got != 10 {
		t.Fatalf("TotalRoundsUsed after workflow round = %d, want 10", got)
	}

	directRun := &Run{
		ExecutionMode: ExecutionModeDirect,
		WorkflowState: &WorkflowState{TotalRoundsUsed: 5},
	}
	trackWorkflowExecutionRound(directRun)
	if got := directRun.WorkflowState.TotalRoundsUsed; got != 5 {
		t.Fatalf("direct workflow rounds = %d, want unchanged 5", got)
	}
}

package agent

import (
	"context"
	"testing"
	"time"

	"github.com/fulcrus/hopclaw/eventbus"
	"github.com/fulcrus/hopclaw/internal/metrics"
	"github.com/fulcrus/hopclaw/logging"
	planpkg "github.com/fulcrus/hopclaw/planner"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

func TestHasTraceID(t *testing.T) {
	if hasTraceID(nil) {
		t.Fatal("hasTraceID(nil) = true, want false")
	}

	ctx := logging.WithTraceID(context.Background(), "trace-123")
	if !hasTraceID(logging.FieldsFromContext(ctx)) {
		t.Fatal("hasTraceID() = false, want true")
	}
}

func TestEmitAddsTraceIDToEventAttrs(t *testing.T) {
	bus := eventbus.NewInMemoryBus()
	component := (&AgentComponent{}).WithEventBus(bus)

	ctx := logging.WithTraceID(context.Background(), "trace-emit")
	if err := component.emit(ctx, eventbus.Event{Type: eventbus.EventRunStarted}); err != nil {
		t.Fatalf("emit() error = %v", err)
	}

	events := bus.Snapshot()
	if len(events) != 1 {
		t.Fatalf("len(events) = %d, want 1", len(events))
	}
	if got := events[0].Attrs[logging.AttrKeyTraceID]; got != "trace-emit" {
		t.Fatalf("trace_id = %#v, want %q", got, "trace-emit")
	}
}

func TestEventBusStreamCallbackAddsTraceID(t *testing.T) {
	bus := eventbus.NewInMemoryBus()
	callback := &EventBusStreamCallback{
		Bus:       bus,
		RunID:     "run-stream",
		SessionID: "session-stream",
	}

	ctx := logging.WithTraceID(context.Background(), "trace-stream")
	callback.OnTextDelta(ctx, "hello")
	callback.OnReasoningDelta(ctx, "thinking")
	callback.OnComplete(ctx)

	events := bus.Snapshot()
	if len(events) != 3 {
		t.Fatalf("len(events) = %d, want 3", len(events))
	}
	for _, event := range events {
		if got := event.Attrs[logging.AttrKeyTraceID]; got != "trace-stream" {
			t.Fatalf("trace_id = %#v, want %q", got, "trace-stream")
		}
	}
}

func TestFinalizeRunIncrementsRunMetrics(t *testing.T) {
	sessions := NewInMemorySessionStore()
	runs := NewInMemoryRunStore()
	component := NewComponent(AgentConfig{DefaultModel: "test-model"}, sessions, runs, NewInMemoryCoordinator(), NewSlidingWindowEngineForTest(), nil, nil, nil)

	session, err := sessions.GetOrCreate(context.Background(), "metrics-finalize", "test-model")
	if err != nil {
		t.Fatalf("GetOrCreate() error = %v", err)
	}
	run, err := runs.Create(context.Background(), session.ID, IncomingMessage{
		SessionKey: "metrics-finalize",
		Content:    "finish",
	}, AgentConfig{DefaultModel: "test-model", QueueMode: QueueEnqueue})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	run.Status = RunRunning
	if err := runs.Update(context.Background(), run); err != nil {
		t.Fatalf("Update() error = %v", err)
	}

	before := testutil.ToFloat64(metrics.RunsTotal.WithLabelValues("completed"))
	if err := component.finalizeRun(context.Background(), run, runFinalization{status: RunCompleted}); err != nil {
		t.Fatalf("finalizeRun() error = %v", err)
	}
	after := testutil.ToFloat64(metrics.RunsTotal.WithLabelValues("completed"))
	if after <= before {
		t.Fatalf("completed counter = %v, want > %v", after, before)
	}
}

func TestWorkflowCompletedEventIncludesBudgetAttrs(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	sessions := NewInMemorySessionStore()
	runs := NewInMemoryRunStore()
	bus := eventbus.NewInMemoryBus()
	component := NewComponent(AgentConfig{DefaultModel: "test-model"}, sessions, runs, NewInMemoryCoordinator(), NewSlidingWindowEngineForTest(), &stubModelClient{}, nil, nil).
		WithEventBus(bus)

	session, err := sessions.GetOrCreate(ctx, "workflow-complete-budget", "test-model")
	if err != nil {
		t.Fatalf("GetOrCreate() error = %v", err)
	}
	run, err := runs.Create(ctx, session.ID, IncomingMessage{SessionKey: session.Key, Content: "complete"}, AgentConfig{DefaultModel: "test-model"})
	if err != nil {
		t.Fatalf("runs.Create() error = %v", err)
	}
	run.Status = RunRunning
	run.ExecutionMode = ExecutionModeWorkflow
	run.Plan = &planpkg.Plan{Tasks: []planpkg.Task{{ID: "t1", Status: planpkg.TaskCompleted}}}
	run.WorkflowState = &WorkflowState{
		OriginalRunID:    run.ID,
		MaxContinuations: DefaultMaxContinuations,
		TotalRoundsUsed:  4,
		MaxTotalRounds:   DefaultMaxTotalRounds,
		Budget: &WorkflowBudgetState{
			Policy: DefaultWorkflowBudgetPolicy(),
			Mode:   WorkflowBudgetModeEconomy,
			Usage: WorkflowBudgetUsage{
				StartedAt:        time.Now().UTC().Add(-2 * time.Minute),
				ModelTotalTokens: 12_345,
				EstimatedCost:    0.12,
			},
			Circuit: WorkflowCircuitBreakerState{
				State:  workflowCircuitStateClosed,
				Reason: "watch closely",
			},
		},
	}
	if err := runs.Update(ctx, run); err != nil {
		t.Fatalf("runs.Update() error = %v", err)
	}

	if err := component.completeRun(ctx, run, session); err != nil {
		t.Fatalf("completeRun() error = %v", err)
	}

	for _, event := range bus.Snapshot() {
		if event.Type != eventbus.EventWorkflowCompleted {
			continue
		}
		if got := event.Attrs["workflow_budget_mode"]; got != string(WorkflowBudgetModeEconomy) {
			t.Fatalf("workflow_budget_mode = %#v, want %q", got, WorkflowBudgetModeEconomy)
		}
		if got := event.Attrs["workflow_budget_model_tokens"]; got != 12_345 {
			t.Fatalf("workflow_budget_model_tokens = %#v, want 12345", got)
		}
		return
	}
	t.Fatal("expected workflow.completed event")
}

func TestWorkflowFailedEventIncludesBudgetAttrs(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	sessions := NewInMemorySessionStore()
	runs := NewInMemoryRunStore()
	bus := eventbus.NewInMemoryBus()
	component := NewComponent(AgentConfig{DefaultModel: "test-model"}, sessions, runs, NewInMemoryCoordinator(), NewSlidingWindowEngineForTest(), &stubModelClient{}, nil, nil).
		WithEventBus(bus)

	session, err := sessions.GetOrCreate(ctx, "workflow-fail-budget", "test-model")
	if err != nil {
		t.Fatalf("GetOrCreate() error = %v", err)
	}
	run, err := runs.Create(ctx, session.ID, IncomingMessage{SessionKey: session.Key, Content: "fail"}, AgentConfig{DefaultModel: "test-model"})
	if err != nil {
		t.Fatalf("runs.Create() error = %v", err)
	}
	run.Status = RunRunning
	run.ExecutionMode = ExecutionModeWorkflow
	run.Plan = &planpkg.Plan{Tasks: []planpkg.Task{{ID: "t1", Status: planpkg.TaskQueued}}}
	run.WorkflowState = &WorkflowState{
		OriginalRunID:    run.ID,
		MaxContinuations: DefaultMaxContinuations,
		TotalRoundsUsed:  4,
		MaxTotalRounds:   DefaultMaxTotalRounds,
		Budget: &WorkflowBudgetState{
			Policy: DefaultWorkflowBudgetPolicy(),
			Mode:   WorkflowBudgetModeStopped,
			Usage: WorkflowBudgetUsage{
				StartedAt:        time.Now().UTC().Add(-2 * time.Minute),
				ModelTotalTokens: 54_321,
				EstimatedCost:    0.54,
			},
			Circuit: WorkflowCircuitBreakerState{
				State:  workflowCircuitStateOpen,
				Reason: "3 no-progress continuations",
			},
		},
	}
	if err := runs.Update(ctx, run); err != nil {
		t.Fatalf("runs.Update() error = %v", err)
	}

	if err := component.failRun(ctx, run, context.DeadlineExceeded); err == nil {
		t.Fatal("failRun() error = nil, want propagated failure")
	}

	for _, event := range bus.Snapshot() {
		if event.Type != eventbus.EventWorkflowFailed {
			continue
		}
		if got := event.Attrs["workflow_budget_mode"]; got != string(WorkflowBudgetModeStopped) {
			t.Fatalf("workflow_budget_mode = %#v, want %q", got, WorkflowBudgetModeStopped)
		}
		if got := event.Attrs["workflow_circuit_state"]; got != workflowCircuitStateOpen {
			t.Fatalf("workflow_circuit_state = %#v, want %q", got, workflowCircuitStateOpen)
		}
		return
	}
	t.Fatal("expected workflow.failed event")
}

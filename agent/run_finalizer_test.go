package agent

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/fulcrus/hopclaw/eventbus"
	"github.com/fulcrus/hopclaw/hooks"
	domaingov "github.com/fulcrus/hopclaw/internal/domain/governance"
	domainscope "github.com/fulcrus/hopclaw/internal/domain/scope"
	"github.com/fulcrus/hopclaw/planner"
)

type recordedHookCall struct {
	trigger hooks.TriggerEvent
	phase   hooks.HookPhase
	payload map[string]any
}

type recordingHookDispatcher struct {
	mu    sync.Mutex
	calls []recordedHookCall
}

func (d *recordingHookDispatcher) Fire(_ context.Context, trigger hooks.TriggerEvent, phase hooks.HookPhase, payload map[string]any) []hooks.HookResult {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.calls = append(d.calls, recordedHookCall{
		trigger: trigger,
		phase:   phase,
		payload: cloneMap(payload),
	})
	return nil
}

func (d *recordingHookDispatcher) snapshot() []recordedHookCall {
	d.mu.Lock()
	defer d.mu.Unlock()
	out := make([]recordedHookCall, len(d.calls))
	copy(out, d.calls)
	return out
}

func TestCompleteRunAddsGovernanceAttrsAndCancelsResidualPlanTasks(t *testing.T) {
	t.Parallel()

	sessions := NewInMemorySessionStore()
	runs := NewInMemoryRunStore()
	bus := eventbus.NewInMemoryBus()
	component := NewComponent(AgentConfig{DefaultModel: "test-model"}, sessions, runs, NewInMemoryCoordinator(), NewSlidingWindowEngineForTest(), nil, nil, nil).
		WithEventBus(bus)

	session, err := sessions.GetOrCreate(context.Background(), "finalizer-complete", "test-model")
	if err != nil {
		t.Fatalf("GetOrCreate() error = %v", err)
	}
	run, err := runs.Create(context.Background(), session.ID, IncomingMessage{
		SessionKey: "finalizer-complete",
		Content:    "finish this run",
	}, AgentConfig{DefaultModel: "test-model", QueueMode: QueueEnqueue})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	run.Status = RunRunning
	run.Scope = domainscope.Ref{AutomationID: "auto-finalizer"}
	run.PendingTools = []ToolCall{{ID: "call-1", Name: "fs.write"}}
	run.ApprovalID = "approval-1"
	completeEval := domaingov.Evaluation{
		EffectiveConfigSnapshotID: "ecs-1",
		Decision: domaingov.Decision{
			Action:       domaingov.DecisionAllow,
			PolicySource: "policy.test/complete",
			Summary:      "allowed by governance test",
		},
		ToolNames: []string{"fs.write"},
	}.Normalized()
	run.Governance = &completeEval
	run.Plan = &planner.Plan{
		Tasks: []planner.Task{
			{ID: "task-running", Status: planner.TaskRunning},
			{ID: "task-queued", Status: planner.TaskQueued},
		},
		RunningTasks: []string{"task-running"},
		ActiveTask:   "task-running",
	}
	if err := runs.Update(context.Background(), run); err != nil {
		t.Fatalf("Update() error = %v", err)
	}

	if err := component.completeRun(context.Background(), run, session); err != nil {
		t.Fatalf("completeRun() error = %v", err)
	}

	got, err := runs.Get(context.Background(), run.ID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got.Status != RunCompleted {
		t.Fatalf("run.Status = %q", got.Status)
	}
	if got.ApprovalID != "" {
		t.Fatalf("run.ApprovalID = %q", got.ApprovalID)
	}
	if got.PendingTools != nil {
		t.Fatalf("run.PendingTools = %#v", got.PendingTools)
	}
	if got.Plan == nil || got.Plan.Tasks[0].Status != planner.TaskCancelled || got.Plan.Tasks[1].Status != planner.TaskSkipped {
		t.Fatalf("run.Plan = %#v", got.Plan)
	}

	events := bus.Snapshot()
	if len(events) == 0 {
		t.Fatal("expected completion event")
	}
	last := events[len(events)-1]
	if last.Type != eventbus.EventRunCompleted {
		t.Fatalf("last event = %q", last.Type)
	}
	if last.Attrs["effective_config_snapshot_id"] != "ecs-1" {
		t.Fatalf("effective_config_snapshot_id = %#v", last.Attrs["effective_config_snapshot_id"])
	}
	if last.Attrs["policy_source"] != "policy.test/complete" {
		t.Fatalf("policy_source = %#v", last.Attrs["policy_source"])
	}
}

func TestMarkBackgroundFailureClearsPendingStateAndFiresHook(t *testing.T) {
	t.Parallel()

	sessions := NewInMemorySessionStore()
	runs := NewInMemoryRunStore()
	bus := eventbus.NewInMemoryBus()
	hookDispatcher := &recordingHookDispatcher{}
	component := NewComponent(AgentConfig{DefaultModel: "test-model"}, sessions, runs, NewInMemoryCoordinator(), NewSlidingWindowEngineForTest(), nil, nil, nil).
		WithEventBus(bus).
		WithHooks(hookDispatcher)

	session, err := sessions.GetOrCreate(context.Background(), "finalizer-background-failure", "test-model")
	if err != nil {
		t.Fatalf("GetOrCreate() error = %v", err)
	}
	run, err := runs.Create(context.Background(), session.ID, IncomingMessage{
		SessionKey: "finalizer-background-failure",
		Content:    "run in background",
	}, AgentConfig{DefaultModel: "test-model", QueueMode: QueueEnqueue})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	run.Status = RunRunning
	run.PendingTools = []ToolCall{{ID: "call-1", Name: "fs.write"}}
	run.ApprovalID = "approval-1"
	backgroundEval := domaingov.Evaluation{
		EffectiveConfigSnapshotID: "ecs-background",
		Decision: domaingov.Decision{
			Action:       domaingov.DecisionDeny,
			PolicySource: "policy.test/background",
			Summary:      "background failure policy",
		},
		ToolNames: []string{"fs.write"},
	}.Normalized()
	run.Governance = &backgroundEval
	if err := runs.Update(context.Background(), run); err != nil {
		t.Fatalf("Update() error = %v", err)
	}

	cancelled := false
	component.cancels.Store(run.ID, context.CancelFunc(func() { cancelled = true }))

	if err := component.MarkBackgroundFailure(context.Background(), run.ID, errors.New("background boom")); err != nil {
		t.Fatalf("MarkBackgroundFailure() error = %v", err)
	}

	got, err := runs.Get(context.Background(), run.ID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got.Status != RunFailed {
		t.Fatalf("run.Status = %q", got.Status)
	}
	if got.ApprovalID != "" {
		t.Fatalf("run.ApprovalID = %q", got.ApprovalID)
	}
	if got.PendingTools != nil {
		t.Fatalf("run.PendingTools = %#v", got.PendingTools)
	}
	if !cancelled {
		t.Fatal("expected cancellation func to be invoked")
	}

	events := bus.Snapshot()
	if len(events) == 0 {
		t.Fatal("expected failure event")
	}
	last := events[len(events)-1]
	if last.Type != eventbus.EventRunFailed {
		t.Fatalf("last event = %q", last.Type)
	}
	if last.Attrs["effective_config_snapshot_id"] != "ecs-background" {
		t.Fatalf("effective_config_snapshot_id = %#v", last.Attrs["effective_config_snapshot_id"])
	}
	if last.Attrs["error"] != "background boom" {
		t.Fatalf("error = %#v", last.Attrs["error"])
	}

	calls := hookDispatcher.snapshot()
	if len(calls) == 0 {
		t.Fatal("expected hook call")
	}
	lastHook := calls[len(calls)-1]
	if lastHook.trigger != hooks.TriggerAfterAgentEnd || lastHook.phase != hooks.HookPhaseError {
		t.Fatalf("hook = %#v", lastHook)
	}
	if lastHook.payload["status"] != string(RunFailed) {
		t.Fatalf("hook payload status = %#v", lastHook.payload["status"])
	}
}

func TestCompleteRunEmitsWorkflowCompletedEvent(t *testing.T) {
	t.Parallel()

	sessions := NewInMemorySessionStore()
	runs := NewInMemoryRunStore()
	bus := eventbus.NewInMemoryBus()
	component := NewComponent(AgentConfig{DefaultModel: "test-model"}, sessions, runs, NewInMemoryCoordinator(), NewSlidingWindowEngineForTest(), nil, nil, nil).
		WithEventBus(bus)

	session, err := sessions.GetOrCreate(context.Background(), "workflow-complete", "test-model")
	if err != nil {
		t.Fatalf("GetOrCreate() error = %v", err)
	}
	run, err := runs.Create(context.Background(), session.ID, IncomingMessage{
		SessionKey: session.Key,
		Content:    "finish workflow",
	}, AgentConfig{DefaultModel: "test-model", QueueMode: QueueEnqueue})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	run.Status = RunRunning
	run.ExecutionMode = ExecutionModeWorkflow
	run.ToolRounds = 3
	run.Plan = &planner.Plan{
		Tasks: []planner.Task{
			{ID: "t1", Status: planner.TaskCompleted},
			{ID: "t2", Status: planner.TaskCompleted},
		},
	}
	run.WorkflowState = &WorkflowState{
		OriginalRunID:     run.ID,
		ContinuationIndex: 1,
		MaxContinuations:  DefaultMaxContinuations,
		TotalRoundsUsed:   7,
		MaxTotalRounds:    DefaultMaxTotalRounds,
	}
	if err := runs.Update(context.Background(), run); err != nil {
		t.Fatalf("Update() error = %v", err)
	}

	if err := component.completeRun(context.Background(), run, session); err != nil {
		t.Fatalf("completeRun() error = %v", err)
	}

	got, err := runs.Get(context.Background(), run.ID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got.WorkflowState == nil || got.WorkflowState.TotalRoundsUsed != 7 {
		t.Fatalf("WorkflowState = %#v", got.WorkflowState)
	}

	events := bus.Snapshot()
	foundCompleted := false
	for _, event := range events {
		if event.Type != eventbus.EventWorkflowCompleted {
			continue
		}
		foundCompleted = true
		payload, ok := event.WorkflowCompletedPayload()
		if !ok {
			t.Fatal("WorkflowCompletedPayload() ok = false")
		}
		if payload.TotalRoundsUsed != 7 {
			t.Fatalf("payload.TotalRoundsUsed = %d, want 7", payload.TotalRoundsUsed)
		}
		if payload.ContinuationIndex != 1 {
			t.Fatalf("payload.ContinuationIndex = %d, want 1", payload.ContinuationIndex)
		}
	}
	if !foundCompleted {
		t.Fatal("expected workflow.completed event")
	}
}

func TestSupersedeWaitingInputRunClearsPendingState(t *testing.T) {
	t.Parallel()

	sessions := NewInMemorySessionStore()
	runs := NewInMemoryRunStore()
	bus := eventbus.NewInMemoryBus()
	component := NewComponent(AgentConfig{DefaultModel: "test-model"}, sessions, runs, NewInMemoryCoordinator(), NewSlidingWindowEngineForTest(), nil, nil, nil).
		WithEventBus(bus)

	session, err := sessions.GetOrCreate(context.Background(), "finalizer-waiting-input", "test-model")
	if err != nil {
		t.Fatalf("GetOrCreate() error = %v", err)
	}
	run, err := runs.Create(context.Background(), session.ID, IncomingMessage{
		SessionKey: "finalizer-waiting-input",
		Content:    "needs clarification",
	}, AgentConfig{DefaultModel: "test-model", QueueMode: QueueEnqueue})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	run.Status = RunWaitingInput
	run.PendingTools = []ToolCall{{ID: "call-1", Name: "fs.write"}}
	run.ApprovalID = "approval-1"
	waitingEval := domaingov.Evaluation{
		EffectiveConfigSnapshotID: "ecs-waiting-input",
		Decision: domaingov.Decision{
			Action:       domaingov.DecisionRequireApproval,
			PolicySource: "policy.test/waiting",
			Summary:      "clarification policy context",
		},
	}.Normalized()
	run.Governance = &waitingEval
	if err := runs.Update(context.Background(), run); err != nil {
		t.Fatalf("Update() error = %v", err)
	}

	if err := component.SupersedeWaitingInputRun(context.Background(), run.ID, RunReasonClarificationSuperseded); err != nil {
		t.Fatalf("SupersedeWaitingInputRun() error = %v", err)
	}

	got, err := runs.Get(context.Background(), run.ID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got.Status != RunCancelled {
		t.Fatalf("run.Status = %q", got.Status)
	}
	if got.ApprovalID != "" {
		t.Fatalf("run.ApprovalID = %q", got.ApprovalID)
	}
	if got.PendingTools != nil {
		t.Fatalf("run.PendingTools = %#v", got.PendingTools)
	}

	events := bus.Snapshot()
	if len(events) == 0 {
		t.Fatal("expected cancellation event")
	}
	last := events[len(events)-1]
	if last.Type != eventbus.EventRunCancelled {
		t.Fatalf("last event = %q", last.Type)
	}
	if last.Attrs["reason"] != "preflight_clarification_superseded" {
		t.Fatalf("reason = %#v", last.Attrs["reason"])
	}
	if last.Attrs["effective_config_snapshot_id"] != "ecs-waiting-input" {
		t.Fatalf("effective_config_snapshot_id = %#v", last.Attrs["effective_config_snapshot_id"])
	}
}

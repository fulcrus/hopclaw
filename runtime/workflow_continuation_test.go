package runtime

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/fulcrus/hopclaw/agent"
	"github.com/fulcrus/hopclaw/eventbus"
	"github.com/fulcrus/hopclaw/planner"
	"github.com/fulcrus/hopclaw/usage"
)

func TestBuildContinuationPrompt(t *testing.T) {
	t.Parallel()

	parent := &agent.Run{
		ID: "run-001",
		Plan: &planner.Plan{
			Goal: "build the feature",
			Tasks: []planner.Task{
				{ID: "t1", Kind: planner.TaskWrite, Goal: "create file", Status: planner.TaskCompleted},
				{ID: "t2", Kind: planner.TaskExecute, Goal: "run tests", Status: planner.TaskQueued},
				{ID: "t3", Kind: planner.TaskReview, Goal: "review output", Status: planner.TaskQueued},
			},
		},
		WorkflowState: &agent.WorkflowState{
			OriginalRunID:     "run-001",
			ContinuationIndex: 0,
		},
	}

	prompt := buildContinuationPrompt(parent)
	if prompt == "" {
		t.Fatal("expected non-empty prompt")
	}
	if !strings.Contains(prompt, "1/3") {
		t.Fatalf("prompt missing progress: %q", prompt)
	}
	if !strings.Contains(prompt, "run tests") {
		t.Fatalf("prompt missing remaining task: %q", prompt)
	}
}

func TestClonePlanForContinuation(t *testing.T) {
	t.Parallel()

	plan := &planner.Plan{
		Goal:     "test",
		Strategy: planner.StrategySerial,
		Tasks: []planner.Task{
			{ID: "t1", Status: planner.TaskCompleted, ResultSummary: "done"},
			{ID: "t2", Status: planner.TaskRunning, Goal: "in progress"},
			{ID: "t3", Status: planner.TaskQueued, Goal: "waiting"},
		},
		ActiveTask:   "t2",
		RunningTasks: []string{"t2"},
	}

	clone := clonePlanForContinuation(plan)
	if clone == nil {
		t.Fatal("expected non-nil clone")
	}
	if clone.Tasks[0].Status != planner.TaskCompleted {
		t.Fatalf("task 0 status = %q, want %q", clone.Tasks[0].Status, planner.TaskCompleted)
	}
	if clone.Tasks[1].Status != planner.TaskQueued {
		t.Fatalf("task 1 status = %q, want %q", clone.Tasks[1].Status, planner.TaskQueued)
	}
	if clone.ActiveTask != "" {
		t.Fatalf("ActiveTask = %q, want empty", clone.ActiveTask)
	}
	if len(clone.RunningTasks) != 0 {
		t.Fatalf("RunningTasks = %v, want empty", clone.RunningTasks)
	}
	if plan.Tasks[1].Status != planner.TaskRunning {
		t.Fatal("original plan was mutated")
	}
}

func TestBuildContinuationWorkflowState(t *testing.T) {
	t.Parallel()

	parent := &agent.Run{
		ID:         "run-002",
		ToolRounds: 8,
		Plan: &planner.Plan{
			Tasks: []planner.Task{
				{ID: "t1", Status: planner.TaskCompleted},
				{ID: "t2", Status: planner.TaskQueued},
			},
		},
		WorkflowState: &agent.WorkflowState{
			OriginalRunID:     "run-001",
			ContinuationIndex: 1,
			MaxContinuations:  10,
			TotalRoundsUsed:   15,
			MaxTotalRounds:    100,
			PriorRunSummaries: []string{"Run run-001: completed 2/5 tasks in 7 rounds"},
			CompletedTaskIDs:  []string{"t0"},
			YieldReason:       agent.YieldReasonRoundBudget,
			Budget: &agent.WorkflowBudgetState{
				Policy: agent.DefaultWorkflowBudgetPolicy(),
				Mode:   agent.WorkflowBudgetModeEconomy,
				Usage: agent.WorkflowBudgetUsage{
					ModelTotalTokens:         42_000,
					StartedAt:                time.Now().UTC().Add(-2 * time.Minute),
					StartedContinuationCount: 2,
				},
			},
		},
	}

	ws := buildContinuationWorkflowState(parent)
	if ws == nil {
		t.Fatal("expected workflow state")
	}
	if ws.ContinuationIndex != 2 {
		t.Fatalf("ContinuationIndex = %d, want 2", ws.ContinuationIndex)
	}
	if ws.OriginalRunID != "run-001" {
		t.Fatalf("OriginalRunID = %q, want run-001", ws.OriginalRunID)
	}
	if len(ws.PriorRunSummaries) != 2 {
		t.Fatalf("PriorRunSummaries len = %d, want 2", len(ws.PriorRunSummaries))
	}
	if ws.Budget == nil {
		t.Fatal("Budget = nil, want cloned budget state")
	}
	if ws.Budget.Mode != agent.WorkflowBudgetModeEconomy {
		t.Fatalf("Budget.Mode = %q, want %q", ws.Budget.Mode, agent.WorkflowBudgetModeEconomy)
	}
	if ws.Budget.Usage.ModelTotalTokens != 42_000 {
		t.Fatalf("Budget.Usage.ModelTotalTokens = %d, want 42000", ws.Budget.Usage.ModelTotalTokens)
	}
	if ws.Budget.Usage.StartedContinuationCount != 3 {
		t.Fatalf("Budget.Usage.StartedContinuationCount = %d, want 3", ws.Budget.Usage.StartedContinuationCount)
	}
	if ws.YieldReason != "" {
		t.Fatalf("YieldReason = %q, want empty for fresh continuation state", ws.YieldReason)
	}
	ws.Budget.Usage.ModelTotalTokens = 7
	if parent.WorkflowState.Budget.Usage.ModelTotalTokens != 42_000 {
		t.Fatalf("parent budget mutated: %d", parent.WorkflowState.Budget.Usage.ModelTotalTokens)
	}
}

func TestCreateContinuationRun(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	sessions := agent.NewInMemorySessionStore()
	runs := agent.NewInMemoryRunStore()
	bus := eventbus.NewInMemoryBus()
	component := agent.NewComponent(agent.AgentConfig{
		DefaultModel: "test-model",
		QueueMode:    agent.QueueEnqueue,
	}, sessions, runs, agent.NewInMemoryCoordinator(), newContextEngine(), mockModelClient{}, nil, nil)
	svc := NewService(component, sessions, runs, nil, bus, nil)

	session, err := sessions.GetOrCreate(ctx, "workflow-continuation", "test-model")
	if err != nil {
		t.Fatalf("GetOrCreate() error = %v", err)
	}
	parent, err := runs.Create(ctx, session.ID, agent.IncomingMessage{
		SessionKey: session.Key,
		Content:    "start workflow",
	}, agent.AgentConfig{
		DefaultModel: "test-model",
		QueueMode:    agent.QueueEnqueue,
	})
	if err != nil {
		t.Fatalf("runs.Create() error = %v", err)
	}
	parent.Status = agent.RunCompleted
	parent.ExecutionMode = agent.ExecutionModeWorkflow
	parent.Plan = &planner.Plan{
		Goal: "ship workflow",
		Tasks: []planner.Task{
			{ID: "t1", Status: planner.TaskCompleted, Goal: "first"},
			{ID: "t2", Status: planner.TaskRunning, Goal: "second"},
		},
		ActiveTask:   "t2",
		RunningTasks: []string{"t2"},
	}
	parent.WorkflowState = &agent.WorkflowState{
		OriginalRunID:     parent.ID,
		ContinuationIndex: 0,
		MaxContinuations:  agent.DefaultMaxContinuations,
		TotalRoundsUsed:   3,
		MaxTotalRounds:    agent.DefaultMaxTotalRounds,
		PriorRunSummaries: []string{"Run bootstrap: set up workflow"},
		CompletedTaskIDs:  []string{"bootstrap"},
		Yielded:           true,
		YieldReason:       agent.YieldReasonRoundBudget,
		Budget: &agent.WorkflowBudgetState{
			Policy: agent.DefaultWorkflowBudgetPolicy(),
			Mode:   agent.WorkflowBudgetModeEconomy,
			Usage: agent.WorkflowBudgetUsage{
				StartedAt:        time.Now().UTC().Add(-2 * time.Minute),
				ModelTotalTokens: 42_000,
				EstimatedCost:    0.42,
			},
			Circuit: agent.WorkflowCircuitBreakerState{
				State:  "closed",
				Reason: "budget warning",
			},
		},
	}
	if err := runs.Update(ctx, parent); err != nil {
		t.Fatalf("runs.Update(parent) error = %v", err)
	}

	contRun, err := svc.createContinuationRun(ctx, parent)
	if err != nil {
		t.Fatalf("createContinuationRun() error = %v", err)
	}
	if contRun == nil {
		t.Fatal("expected continuation run")
	}
	if contRun.ParentRunID != parent.ID {
		t.Fatalf("ParentRunID = %q, want %q", contRun.ParentRunID, parent.ID)
	}
	if contRun.ExecutionMode != agent.ExecutionModeWorkflow {
		t.Fatalf("ExecutionMode = %q, want %q", contRun.ExecutionMode, agent.ExecutionModeWorkflow)
	}
	if contRun.WorkflowState == nil || contRun.WorkflowState.ContinuationIndex != 1 {
		t.Fatalf("WorkflowState = %#v", contRun.WorkflowState)
	}
	if contRun.Plan == nil || len(contRun.Plan.Tasks) != 2 {
		t.Fatalf("Plan = %#v", contRun.Plan)
	}
	if contRun.Plan.Tasks[1].Status != planner.TaskQueued {
		t.Fatalf("continuation task status = %q, want queued", contRun.Plan.Tasks[1].Status)
	}
	if contRun.Plan.ActiveTask != "" || len(contRun.Plan.RunningTasks) != 0 {
		t.Fatalf("continuation plan execution state = %#v", contRun.Plan)
	}
	foundContinued := false
	for _, event := range bus.Snapshot() {
		if event.Type != eventbus.EventWorkflowContinued {
			continue
		}
		foundContinued = true
		payload, ok := event.WorkflowContinuedPayload()
		if !ok {
			t.Fatal("WorkflowContinuedPayload() ok = false")
		}
		if payload.ContinuationIndex != 1 {
			t.Fatalf("payload.ContinuationIndex = %d, want 1", payload.ContinuationIndex)
		}
		if got := event.Attrs["workflow_budget_mode"]; got != string(agent.WorkflowBudgetModeEconomy) {
			t.Fatalf("workflow_budget_mode = %#v, want %q", got, agent.WorkflowBudgetModeEconomy)
		}
		if got := event.Attrs["workflow_budget_model_tokens"]; got != 42_000 {
			t.Fatalf("workflow_budget_model_tokens = %#v, want 42000", got)
		}
		if got := event.Attrs["workflow_circuit_reason"]; got != "budget warning" {
			t.Fatalf("workflow_circuit_reason = %#v, want %q", got, "budget warning")
		}
	}
	if !foundContinued {
		t.Fatal("expected workflow.continued event")
	}
}

func TestAdmitWorkflowContinuation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		setupRun       func(*agent.Run)
		seedUsage      func(context.Context, *usage.Tracker, *agent.Run)
		wantAllow      bool
		wantReason     string
		wantStopped    bool
		wantModelTotal int
	}{
		{
			name: "allow with remaining headroom",
			setupRun: func(run *agent.Run) {
				run.WorkflowState.TotalRoundsUsed = 4
				run.WorkflowState.Budget.Policy.HardModelTokens = 200_000
			},
			seedUsage: func(ctx context.Context, tracker *usage.Tracker, run *agent.Run) {
				_ = tracker.TrackModelCall(ctx, usage.Record{
					RunID:            run.ID,
					WorkflowID:       run.WorkflowState.OriginalRunID,
					SessionID:        run.SessionID,
					Model:            "gpt-4o",
					PromptTokens:     30_000,
					CompletionTokens: 10_000,
					TotalTokens:      40_000,
				})
			},
			wantAllow:      true,
			wantReason:     "",
			wantStopped:    false,
			wantModelTotal: 40_000,
		},
		{
			name: "deny predictive token admission",
			setupRun: func(run *agent.Run) {
				run.WorkflowState.TotalRoundsUsed = 4
				run.WorkflowState.Budget.Policy.HardModelTokens = 90_000
			},
			seedUsage: func(ctx context.Context, tracker *usage.Tracker, run *agent.Run) {
				_ = tracker.TrackModelCall(ctx, usage.Record{
					RunID:            run.ID,
					WorkflowID:       run.WorkflowState.OriginalRunID,
					SessionID:        run.SessionID,
					Model:            "gpt-4o",
					PromptTokens:     30_000,
					CompletionTokens: 10_000,
					TotalTokens:      40_000,
				})
			},
			wantAllow:      false,
			wantReason:     agent.YieldReasonAdmissionDenied,
			wantStopped:    true,
			wantModelTotal: 40_000,
		},
		{
			name: "deny hard token limit after resync",
			setupRun: func(run *agent.Run) {
				run.WorkflowState.TotalRoundsUsed = 1
				run.WorkflowState.Budget.Policy.HardModelTokens = 40_000
			},
			seedUsage: func(ctx context.Context, tracker *usage.Tracker, run *agent.Run) {
				_ = tracker.TrackModelCall(ctx, usage.Record{
					RunID:            run.ID,
					WorkflowID:       run.WorkflowState.OriginalRunID,
					SessionID:        run.SessionID,
					Model:            "gpt-4o",
					PromptTokens:     30_000,
					CompletionTokens: 10_000,
					TotalTokens:      40_000,
				})
			},
			wantAllow:      false,
			wantReason:     agent.YieldReasonBudgetHardLimit,
			wantStopped:    true,
			wantModelTotal: 40_000,
		},
		{
			name: "deny when circuit breaker is open",
			setupRun: func(run *agent.Run) {
				run.WorkflowState.Budget.Circuit.State = "open"
			},
			wantAllow:      false,
			wantReason:     agent.YieldReasonCircuitBreakerOpen,
			wantStopped:    true,
			wantModelTotal: 0,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			ctx := context.Background()
			sessions := agent.NewInMemorySessionStore()
			runs := agent.NewInMemoryRunStore()
			usageStore := usage.NewInMemoryStore()
			tracker := usage.NewTracker(usageStore)
			component := (&agent.AgentComponent{}).WithUsageTracker(tracker)
			svc := NewService(component, sessions, runs, nil, nil, nil)

			run, err := runs.Create(ctx, "sess-workflow", agent.IncomingMessage{SessionKey: "sess-workflow"}, agent.AgentConfig{DefaultModel: "test-model"})
			if err != nil {
				t.Fatalf("runs.Create() error = %v", err)
			}
			run.ExecutionMode = agent.ExecutionModeWorkflow
			run.WorkflowState = &agent.WorkflowState{
				OriginalRunID:     run.ID,
				ContinuationIndex: 0,
				MaxContinuations:  agent.DefaultMaxContinuations,
				MaxTotalRounds:    agent.DefaultMaxTotalRounds,
			}
			run.WorkflowState.EnsureBudget(time.Now().UTC())
			run.WorkflowState.Budget.Usage.ModelTotalTokens = 7
			if tt.setupRun != nil {
				tt.setupRun(run)
			}
			if err := runs.Update(ctx, run); err != nil {
				t.Fatalf("runs.Update() error = %v", err)
			}
			if tt.seedUsage != nil {
				tt.seedUsage(ctx, tracker, run)
			}

			allow, reason := svc.admitWorkflowContinuation(ctx, run)
			if allow != tt.wantAllow {
				t.Fatalf("allow = %v, want %v (reason=%q)", allow, tt.wantAllow, reason)
			}
			if reason != tt.wantReason {
				t.Fatalf("reason = %q, want %q", reason, tt.wantReason)
			}
			if got := run.WorkflowState.Budget.Usage.ModelTotalTokens; got != tt.wantModelTotal {
				t.Fatalf("ModelTotalTokens = %d, want %d", got, tt.wantModelTotal)
			}

			fresh, err := runs.Get(ctx, run.ID)
			if err != nil {
				t.Fatalf("runs.Get() error = %v", err)
			}
			if fresh.WorkflowState == nil || fresh.WorkflowState.Budget == nil {
				t.Fatalf("fresh.WorkflowState = %#v", fresh.WorkflowState)
			}
			if gotStopped := fresh.WorkflowState.Budget.Mode == agent.WorkflowBudgetModeStopped; gotStopped != tt.wantStopped {
				t.Fatalf("Budget.Mode stopped = %v, want %v", gotStopped, tt.wantStopped)
			}
			if tt.wantAllow {
				if fresh.WorkflowState.TerminalOutcome != "" || fresh.WorkflowState.TerminalReason != "" {
					t.Fatalf("terminal workflow state = (%q, %q), want empty", fresh.WorkflowState.TerminalOutcome, fresh.WorkflowState.TerminalReason)
				}
			} else {
				if fresh.WorkflowState.TerminalOutcome != agent.WorkflowTerminalOutcomeFailed {
					t.Fatalf("TerminalOutcome = %q, want %q", fresh.WorkflowState.TerminalOutcome, agent.WorkflowTerminalOutcomeFailed)
				}
				if strings.TrimSpace(fresh.WorkflowState.TerminalReason) == "" {
					t.Fatal("TerminalReason = empty, want workflow stop summary")
				}
			}
			if tt.wantStopped && fresh.WorkflowState.Budget.StopReason != tt.wantReason {
				t.Fatalf("Budget.StopReason = %q, want %q", fresh.WorkflowState.Budget.StopReason, tt.wantReason)
			}
			if tt.wantAllow && fresh.WorkflowState.Budget.PredictedNextRunTokens == 0 {
				t.Fatal("PredictedNextRunTokens = 0, want populated prediction")
			}
		})
	}
}

func TestCheckAndDispatchWorkflowContinuationMarksTerminalWorkflowFailure(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	sessions := agent.NewInMemorySessionStore()
	runs := agent.NewInMemoryRunStore()
	bus := eventbus.NewInMemoryBus()
	component := agent.NewComponent(agent.AgentConfig{
		DefaultModel: "test-model",
		QueueMode:    agent.QueueEnqueue,
	}, sessions, runs, agent.NewInMemoryCoordinator(), newContextEngine(), mockModelClient{}, nil, nil)
	svc := NewService(component, sessions, runs, nil, bus, nil)

	session, err := sessions.GetOrCreate(ctx, "workflow-denied", "test-model")
	if err != nil {
		t.Fatalf("GetOrCreate() error = %v", err)
	}
	run, err := runs.Create(ctx, session.ID, agent.IncomingMessage{
		SessionKey: session.Key,
		Content:    "continue workflow",
	}, agent.AgentConfig{
		DefaultModel: "test-model",
		QueueMode:    agent.QueueEnqueue,
	})
	if err != nil {
		t.Fatalf("runs.Create() error = %v", err)
	}
	run.Status = agent.RunCompleted
	run.ExecutionMode = agent.ExecutionModeWorkflow
	run.Plan = &planner.Plan{
		Tasks: []planner.Task{
			{ID: "t1", Status: planner.TaskCompleted},
			{ID: "t2", Status: planner.TaskQueued},
		},
	}
	run.WorkflowState = &agent.WorkflowState{
		OriginalRunID:     run.ID,
		ContinuationIndex: 0,
		MaxContinuations:  agent.DefaultMaxContinuations,
		MaxTotalRounds:    agent.DefaultMaxTotalRounds,
		Yielded:           true,
		YieldReason:       agent.YieldReasonRoundBudget,
	}
	run.WorkflowState.EnsureBudget(time.Now().UTC())
	run.WorkflowState.Budget.Circuit.State = "open"
	if err := runs.Update(ctx, run); err != nil {
		t.Fatalf("runs.Update() error = %v", err)
	}

	if continued := svc.checkAndDispatchWorkflowContinuation(ctx, run.ID); continued {
		t.Fatal("checkAndDispatchWorkflowContinuation() = true, want denied")
	}

	fresh, err := runs.Get(ctx, run.ID)
	if err != nil {
		t.Fatalf("runs.Get() error = %v", err)
	}
	if fresh.WorkflowState == nil {
		t.Fatal("WorkflowState = nil")
	}
	if fresh.WorkflowState.TerminalOutcome != agent.WorkflowTerminalOutcomeFailed {
		t.Fatalf("TerminalOutcome = %q, want %q", fresh.WorkflowState.TerminalOutcome, agent.WorkflowTerminalOutcomeFailed)
	}
	if fresh.WorkflowState.Yielded {
		t.Fatal("Yielded = true, want false after terminal denial")
	}
	if got := DeriveRunOutcome(fresh, nil, nil); got != RunOutcomeFailed {
		t.Fatalf("DeriveRunOutcome() = %q, want %q", got, RunOutcomeFailed)
	}

	foundWorkflowFailed := false
	for _, event := range bus.Snapshot() {
		if event.Type != eventbus.EventWorkflowFailed {
			continue
		}
		foundWorkflowFailed = true
		payload, ok := event.WorkflowFailedPayload()
		if !ok {
			t.Fatal("WorkflowFailedPayload() ok = false")
		}
		if payload.YieldReason != agent.YieldReasonCircuitBreakerOpen {
			t.Fatalf("payload.YieldReason = %q, want %q", payload.YieldReason, agent.YieldReasonCircuitBreakerOpen)
		}
		if !strings.Contains(payload.Summary, "workflow auto-continuation stopped") {
			t.Fatalf("payload.Summary = %q, want continuation stop summary", payload.Summary)
		}
		if got := event.Attrs["workflow_budget_mode"]; got != string(agent.WorkflowBudgetModeStopped) {
			t.Fatalf("workflow_budget_mode = %#v, want %q", got, agent.WorkflowBudgetModeStopped)
		}
		if got := event.Attrs["workflow_circuit_state"]; got != "open" {
			t.Fatalf("workflow_circuit_state = %#v, want %q", got, "open")
		}
	}
	if !foundWorkflowFailed {
		t.Fatal("expected workflow.failed event")
	}
}

func TestClonePlanForContinuationEdgeCases(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		plan      *planner.Plan
		assertion func(*testing.T, *planner.Plan, *planner.Plan)
	}{
		{
			name: "all completed stay completed",
			plan: &planner.Plan{
				Tasks: []planner.Task{
					{ID: "t1", Status: planner.TaskCompleted, ResultSummary: "done"},
					{ID: "t2", Status: planner.TaskCompleted, DependsOn: []string{"t1"}},
				},
				FinalTask: "t2",
			},
			assertion: func(t *testing.T, original, clone *planner.Plan) {
				if clone.Tasks[0].Status != planner.TaskCompleted || clone.Tasks[1].Status != planner.TaskCompleted {
					t.Fatalf("clone.Tasks = %#v", clone.Tasks)
				}
				if clone.Tasks[1].DependsOn[0] != "t1" {
					t.Fatalf("clone depends_on = %#v", clone.Tasks[1].DependsOn)
				}
				clone.Tasks[1].DependsOn[0] = "mutated"
				if original.Tasks[1].DependsOn[0] != "t1" {
					t.Fatalf("original mutated: %#v", original.Tasks[1].DependsOn)
				}
			},
		},
		{
			name: "failed tasks remain failed",
			plan: &planner.Plan{
				Tasks: []planner.Task{
					{ID: "t1", Status: planner.TaskFailed, Error: "boom"},
					{ID: "t2", Status: planner.TaskFailed, DependsOn: []string{"t1"}},
				},
			},
			assertion: func(t *testing.T, _ *planner.Plan, clone *planner.Plan) {
				if clone.Tasks[0].Status != planner.TaskFailed || clone.Tasks[1].Status != planner.TaskFailed {
					t.Fatalf("clone.Tasks = %#v", clone.Tasks)
				}
			},
		},
		{
			name: "mixed states preserve dependencies and reset running",
			plan: &planner.Plan{
				Tasks: []planner.Task{
					{ID: "t1", Status: planner.TaskCompleted},
					{ID: "t2", Status: planner.TaskRunning, DependsOn: []string{"t1"}},
					{ID: "t3", Status: planner.TaskQueued, DependsOn: []string{"t2"}},
				},
				ActiveTask:       "t2",
				RunningTasks:     []string{"t2"},
				CoverageWarnings: []string{"warn-1"},
				FailurePolicy:    planner.FailFast,
				Strategy:         planner.StrategySerial,
				FinalTask:        "t3",
			},
			assertion: func(t *testing.T, original, clone *planner.Plan) {
				if clone.Tasks[1].Status != planner.TaskQueued {
					t.Fatalf("running task status = %q, want queued", clone.Tasks[1].Status)
				}
				if clone.Tasks[2].DependsOn[0] != "t2" {
					t.Fatalf("clone depends_on = %#v", clone.Tasks[2].DependsOn)
				}
				if clone.ActiveTask != "" || len(clone.RunningTasks) != 0 {
					t.Fatalf("clone execution state = %#v", clone)
				}
				if len(clone.CoverageWarnings) != 1 || clone.CoverageWarnings[0] != "warn-1" {
					t.Fatalf("clone coverage warnings = %#v", clone.CoverageWarnings)
				}
				clone.CoverageWarnings[0] = "mutated"
				if original.CoverageWarnings[0] != "warn-1" {
					t.Fatalf("original coverage warnings mutated: %#v", original.CoverageWarnings)
				}
			},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			clone := clonePlanForContinuation(tt.plan)
			if clone == nil {
				t.Fatal("expected clone")
			}
			tt.assertion(t, tt.plan, clone)
		})
	}
}

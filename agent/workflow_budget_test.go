package agent

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/fulcrus/hopclaw/contextengine"
	"github.com/fulcrus/hopclaw/eventbus"
	planpkg "github.com/fulcrus/hopclaw/planner"
	"github.com/fulcrus/hopclaw/skill"
)

type spyContextEngine struct {
	inner        contextengine.ContextEngine
	compactCalls int
}

type autoCompactionSignalEngine struct {
	prepareCalls int
	compactCalls int
	compacted    bool
}

func (s *spyContextEngine) Prepare(ctx context.Context, session *contextengine.Session, run *contextengine.Run, runtimeCtx skill.RuntimeContext) (*contextengine.PreparedContext, contextengine.Budget, error) {
	return s.inner.Prepare(ctx, session, run, runtimeCtx)
}

func (s *spyContextEngine) AppendToolResults(ctx context.Context, session *contextengine.Session, results []contextengine.ToolResult) error {
	return s.inner.AppendToolResults(ctx, session, results)
}

func (s *spyContextEngine) Compact(ctx context.Context, session *contextengine.Session, reason contextengine.CompactReason) error {
	s.compactCalls++
	return s.inner.Compact(ctx, session, reason)
}

func (s *spyContextEngine) Inspect(ctx context.Context, session *contextengine.Session, run *contextengine.Run, runtimeCtx skill.RuntimeContext) (*contextengine.ContextReport, error) {
	return s.inner.Inspect(ctx, session, run, runtimeCtx)
}

func (e *autoCompactionSignalEngine) Prepare(_ context.Context, session *contextengine.Session, _ *contextengine.Run, _ skill.RuntimeContext) (*contextengine.PreparedContext, contextengine.Budget, error) {
	e.prepareCalls++
	if session == nil {
		return nil, contextengine.Budget{}, ErrContextEngineNil
	}
	return &contextengine.PreparedContext{
		SystemPrompt:     "system",
		Messages:         append([]contextengine.Message(nil), session.Messages...),
		NeedsCompaction:  !e.compacted,
		Skills:           skill.SessionSkillSnapshot{},
		SessionStatePrompt:    "",
		RecalledContextPrompt: "",
	}, contextengine.Budget{
		ContextWindow:  4096,
		MaxInputTokens: 2048,
		ReservedOutput: 256,
	}, nil
}

func (e *autoCompactionSignalEngine) AppendToolResults(_ context.Context, _ *contextengine.Session, _ []contextengine.ToolResult) error {
	return nil
}

func (e *autoCompactionSignalEngine) Compact(_ context.Context, session *contextengine.Session, _ contextengine.CompactReason) error {
	e.compactCalls++
	e.compacted = true
	if session != nil {
		session.Summary = "auto compacted"
		session.SummaryAt = time.Now().UTC()
	}
	return nil
}

func (e *autoCompactionSignalEngine) Inspect(_ context.Context, session *contextengine.Session, _ *contextengine.Run, _ skill.RuntimeContext) (*contextengine.ContextReport, error) {
	if session == nil {
		return nil, ErrContextEngineNil
	}
	return &contextengine.ContextReport{}, nil
}

func TestExecuteRunAutoCompactsBeforeModelCallWhenPrepareSignalsThreshold(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	sessions := NewInMemorySessionStore()
	runs := NewInMemoryRunStore()
	engine := &autoCompactionSignalEngine{}
	model := &stubModelClient{
		responses: []*ModelResponse{{
			Message: contextengine.Message{
				Role:    contextengine.RoleAssistant,
				Content: "done",
			},
		}},
	}

	component := NewComponent(AgentConfig{
		DefaultModel: "test-model",
		QueueMode:    QueueEnqueue,
	}, sessions, runs, NewInMemoryCoordinator(), engine, model, nil, nil)

	run, err := component.Submit(ctx, IncomingMessage{
		SessionKey: "auto-compact-threshold",
		Content:    "please help",
	})
	if err != nil {
		t.Fatalf("Submit() error = %v", err)
	}
	if err := component.ExecuteRun(ctx, run); err != nil {
		t.Fatalf("ExecuteRun() error = %v", err)
	}

	if engine.compactCalls != 1 {
		t.Fatalf("compactCalls = %d, want 1", engine.compactCalls)
	}
	if engine.prepareCalls < 2 {
		t.Fatalf("prepareCalls = %d, want at least 2", engine.prepareCalls)
	}
	if !strings.Contains(model.lastRequest.SystemPrompt, "system") {
		t.Fatalf("lastRequest.SystemPrompt = %q, want it to contain system", model.lastRequest.SystemPrompt)
	}

	session, err := sessions.GetByKey(ctx, "auto-compact-threshold")
	if err != nil {
		t.Fatalf("GetByKey() error = %v", err)
	}
	if session.Summary != "auto compacted" {
		t.Fatalf("session.Summary = %q, want auto compacted", session.Summary)
	}
}

func TestEvaluateWorkflowBudgetModes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		setup      func(*Run)
		wantMode   WorkflowBudgetMode
		wantStop   bool
		wantReason string
	}{
		{
			name: "soft budget enters economy",
			setup: func(run *Run) {
				run.WorkflowState.TotalRoundsUsed = 10
				run.WorkflowState.Budget.Usage.ModelTotalTokens = run.WorkflowState.Budget.Policy.SoftModelTokens
			},
			wantMode: WorkflowBudgetModeEconomy,
		},
		{
			name: "soft budget near hard enters finish_only",
			setup: func(run *Run) {
				run.WorkflowState.TotalRoundsUsed = 20
				run.WorkflowState.Budget.Policy.HardModelTokens = 460_000
				run.WorkflowState.Budget.Usage.ModelTotalTokens = run.WorkflowState.Budget.Policy.SoftModelTokens
			},
			wantMode: WorkflowBudgetModeFinishOnly,
		},
		{
			name: "hard budget stops workflow",
			setup: func(run *Run) {
				run.WorkflowState.TotalRoundsUsed = 20
				run.WorkflowState.Budget.Usage.ModelTotalTokens = run.WorkflowState.Budget.Policy.HardModelTokens
			},
			wantMode:   WorkflowBudgetModeStopped,
			wantStop:   true,
			wantReason: YieldReasonBudgetHardLimit,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			run := &Run{
				ExecutionMode: ExecutionModeWorkflow,
				WorkflowState: &WorkflowState{
					OriginalRunID:    "run-root",
					MaxContinuations: DefaultMaxContinuations,
					MaxTotalRounds:   DefaultMaxTotalRounds,
					Budget: &WorkflowBudgetState{
						Policy: DefaultWorkflowBudgetPolicy(),
						Mode:   WorkflowBudgetModeNormal,
						Usage: WorkflowBudgetUsage{
							StartedAt: time.Now().UTC().Add(-2 * time.Minute),
						},
					},
				},
			}
			if tt.setup != nil {
				tt.setup(run)
			}

			decision := evaluateWorkflowBudget(run)
			if decision.Mode != tt.wantMode {
				t.Fatalf("decision.Mode = %q, want %q", decision.Mode, tt.wantMode)
			}
			if gotStop := !decision.AllowCurrentTurn; gotStop != tt.wantStop {
				t.Fatalf("AllowCurrentTurn stopped = %v, want %v", gotStop, tt.wantStop)
			}
			if tt.wantReason != "" && decision.YieldReason != tt.wantReason {
				t.Fatalf("decision.YieldReason = %q, want %q", decision.YieldReason, tt.wantReason)
			}
		})
	}
}

func TestEvaluateWorkflowBudgetOpensCircuitAfterRepeatedNoProgress(t *testing.T) {
	t.Parallel()

	run := &Run{
		ExecutionMode: ExecutionModeWorkflow,
		WorkflowState: &WorkflowState{
			OriginalRunID:    "run-circuit",
			MaxContinuations: DefaultMaxContinuations,
			MaxTotalRounds:   DefaultMaxTotalRounds,
			Budget: &WorkflowBudgetState{
				Policy: DefaultWorkflowBudgetPolicy(),
				Usage: WorkflowBudgetUsage{
					StartedAt: time.Now().UTC().Add(-3 * time.Minute),
				},
			},
		},
	}

	for i := 0; i < 3; i++ {
		run.WorkflowState.ContinuationIndex = i
		run.WorkflowState.TotalRoundsUsed = (i + 1) * 5
		run.WorkflowState.YieldReason = YieldReasonRoundBudget
		run.WorkflowState.Budget.Usage.ModelTotalTokens = (i + 1) * 10_000
		observeWorkflowContinuationOutcome(run)
		decision := evaluateWorkflowBudget(run)
		if i < 2 && decision.Mode == WorkflowBudgetModeStopped {
			t.Fatalf("continuation %d stopped early", i)
		}
	}

	if got := run.WorkflowState.Budget.Circuit.State; got != workflowCircuitStateOpen {
		t.Fatalf("Circuit.State = %q, want %q", got, workflowCircuitStateOpen)
	}
	if got := run.WorkflowState.Budget.Circuit.TripCount; got != 1 {
		t.Fatalf("Circuit.TripCount = %d, want 1", got)
	}
	if got := run.WorkflowState.Budget.Circuit.Reason; !strings.Contains(got, "no-progress") {
		t.Fatalf("Circuit.Reason = %q, want no-progress trip", got)
	}
}

func TestEvaluateWorkflowBudgetClosesHalfOpenAfterProgress(t *testing.T) {
	t.Parallel()

	run := &Run{
		ExecutionMode: ExecutionModeWorkflow,
		WorkflowState: &WorkflowState{
			OriginalRunID:     "run-half-open",
			ContinuationIndex: 2,
			MaxContinuations:  DefaultMaxContinuations,
			TotalRoundsUsed:   12,
			MaxTotalRounds:    DefaultMaxTotalRounds,
			CompletedTaskIDs:  []string{"t1", "t2"},
			Budget: &WorkflowBudgetState{
				Policy: DefaultWorkflowBudgetPolicy(),
				Usage: WorkflowBudgetUsage{
					StartedAt:              time.Now().UTC().Add(-3 * time.Minute),
					ModelTotalTokens:       50_000,
					CompletedTaskCount:     2,
					LastCompletedTaskCount: 1,
				},
				Circuit: WorkflowCircuitBreakerState{
					State:                         workflowCircuitStateHalfOpen,
					Reason:                        "manual retry",
					OpenedAt:                      time.Now().UTC().Add(-2 * time.Minute),
					LastObservedContinuationIndex: 1,
					LastObservedModelTokens:       40_000,
					LastObservedTotalRounds:       8,
					LastObservedBurnRate:          2_500,
				},
			},
		},
	}

	decision := evaluateWorkflowBudget(run)
	if decision.Mode != WorkflowBudgetModeFinishOnly {
		t.Fatalf("decision.Mode before observation = %q, want %q", decision.Mode, WorkflowBudgetModeFinishOnly)
	}

	observeWorkflowContinuationOutcome(run)

	decision = evaluateWorkflowBudget(run)
	if decision.Mode != WorkflowBudgetModeNormal {
		t.Fatalf("decision.Mode after observation = %q, want %q", decision.Mode, WorkflowBudgetModeNormal)
	}
	if got := run.WorkflowState.Budget.Circuit.State; got != workflowCircuitStateClosed {
		t.Fatalf("Circuit.State = %q, want %q", got, workflowCircuitStateClosed)
	}
	if got := run.WorkflowState.Budget.Circuit.Reason; got != "" {
		t.Fatalf("Circuit.Reason = %q, want empty", got)
	}
}

func TestDeriveEffectiveDelegationContract(t *testing.T) {
	t.Parallel()

	base := &DelegationContract{
		Goal:            "delegate work",
		AllowedDomains:  []string{string(DomainFS)},
		MaxTurns:        6,
		MaxBudgetTokens: 8_000,
	}

	tests := []struct {
		name   string
		run    *Run
		assert func(*testing.T, *DelegationContract)
	}{
		{
			name: "economy disables when policy says so",
			run: &Run{
				WorkflowState: &WorkflowState{
					TotalRoundsUsed: 8,
					Budget: &WorkflowBudgetState{
						Mode:   WorkflowBudgetModeEconomy,
						Policy: DefaultWorkflowBudgetPolicy(),
						Usage:  WorkflowBudgetUsage{ModelTotalTokens: 40_000},
					},
				},
			},
			assert: func(t *testing.T, contract *DelegationContract) {
				if contract != nil {
					t.Fatalf("contract = %#v, want nil", contract)
				}
			},
		},
		{
			name: "economy shrinks when policy allows delegation",
			run: &Run{
				WorkflowState: &WorkflowState{
					TotalRoundsUsed: 8,
					Budget: &WorkflowBudgetState{
						Mode: WorkflowBudgetModeEconomy,
						Policy: WorkflowBudgetPolicy{
							HardTotalRounds:              12,
							HardModelTokens:              20_000,
							MaxDelegatedTokenFraction:    0.40,
							DisableDelegationOnSoftLimit: false,
						},
						Usage: WorkflowBudgetUsage{ModelTotalTokens: 8_000},
					},
				},
			},
			assert: func(t *testing.T, contract *DelegationContract) {
				if contract == nil {
					t.Fatal("contract = nil, want shrunk contract")
				}
				if contract.MaxTurns != 2 {
					t.Fatalf("MaxTurns = %d, want 2", contract.MaxTurns)
				}
				if contract.MaxBudgetTokens != 1_500 {
					t.Fatalf("MaxBudgetTokens = %d, want 1500", contract.MaxBudgetTokens)
				}
			},
		},
		{
			name: "finish_only disables",
			run: &Run{
				WorkflowState: &WorkflowState{
					Budget: &WorkflowBudgetState{
						Mode:   WorkflowBudgetModeFinishOnly,
						Policy: DefaultWorkflowBudgetPolicy(),
					},
				},
			},
			assert: func(t *testing.T, contract *DelegationContract) {
				if contract != nil {
					t.Fatalf("contract = %#v, want nil", contract)
				}
			},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			tt.assert(t, deriveEffectiveDelegationContract(tt.run, base))
		})
	}
}

func TestExecuteLoopSkipsPlanExpansionAndCompactsInFinishOnly(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	sessions := NewInMemorySessionStore()
	runs := NewInMemoryRunStore()
	model := &stubModelClient{
		responses: []*ModelResponse{{
			Message: contextengine.Message{
				Role:    contextengine.RoleAssistant,
				Content: "done",
			},
		}},
	}
	planner := &capturingPlanner{}
	contextSpy := &spyContextEngine{inner: NewSlidingWindowEngineForTest()}
	component := NewComponent(AgentConfig{
		DefaultModel: "test-model",
		QueueMode:    QueueEnqueue,
	}, sessions, runs, NewInMemoryCoordinator(), contextSpy, model, stubRuntimeToolExecutor{
		definitions: []ToolDefinition{
			{Name: "agent.spawn", SideEffectClass: "local_write"},
			{Name: "fs.read", SideEffectClass: "read"},
		},
	}, nil).WithPlanner(planner)

	session, err := sessions.GetOrCreate(ctx, "workflow-finish-only", "test-model")
	if err != nil {
		t.Fatalf("GetOrCreate() error = %v", err)
	}
	run, err := runs.Create(ctx, session.ID, IncomingMessage{
		SessionKey: session.Key,
		Content:    "finish the workflow",
	}, AgentConfig{
		DefaultModel: "test-model",
		QueueMode:    QueueEnqueue,
	})
	if err != nil {
		t.Fatalf("runs.Create() error = %v", err)
	}
	run.Status = RunRunning
	run.ExecutionMode = ExecutionModeWorkflow
	run.Delegation = &DelegationContract{
		Goal:            "delegate work",
		AllowedDomains:  []string{string(DomainFS)},
		MaxTurns:        6,
		MaxBudgetTokens: 8_000,
	}
	run.WorkflowState = &WorkflowState{
		OriginalRunID:    run.ID,
		MaxContinuations: DefaultMaxContinuations,
		TotalRoundsUsed:  20,
		MaxTotalRounds:   DefaultMaxTotalRounds,
		Budget: &WorkflowBudgetState{
			Policy: DefaultWorkflowBudgetPolicy(),
			Usage: WorkflowBudgetUsage{
				StartedAt:        time.Now().UTC().Add(-3 * time.Minute),
				ModelTotalTokens: 400_000,
			},
		},
	}
	run.WorkflowState.Budget.Policy.HardModelTokens = 460_000
	if err := runs.Update(ctx, run); err != nil {
		t.Fatalf("runs.Update() error = %v", err)
	}

	if err := component.executeLoop(ctx, run, session, func() {}); err != nil {
		t.Fatalf("executeLoop() error = %v", err)
	}
	if contextSpy.compactCalls == 0 {
		t.Fatal("Compact() calls = 0, want budget-driven compaction")
	}
	if len(planner.requests) != 0 {
		t.Fatalf("planner.requests = %d, want 0 in finish_only", len(planner.requests))
	}
	if strings.Contains(model.lastRequest.SystemPrompt, "<delegation_contract>") {
		t.Fatalf("SystemPrompt unexpectedly contains delegation contract: %s", model.lastRequest.SystemPrompt)
	}
	for _, tool := range model.lastRequest.Tools {
		if tool.Name == "agent.spawn" {
			t.Fatalf("Tools unexpectedly include agent.spawn: %#v", model.lastRequest.Tools)
		}
	}
}

func TestExecuteLoopYieldsWhenCircuitBreakerStopsWorkflow(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	sessions := NewInMemorySessionStore()
	runs := NewInMemoryRunStore()
	bus := eventbus.NewInMemoryBus()
	model := &recordingModelClient{}
	component := NewComponent(AgentConfig{
		DefaultModel: "test-model",
		QueueMode:    QueueEnqueue,
	}, sessions, runs, NewInMemoryCoordinator(), NewSlidingWindowEngineForTest(), model, nil, nil).
		WithEventBus(bus)

	session, err := sessions.GetOrCreate(ctx, "workflow-circuit-stop", "test-model")
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
		OriginalRunID:    run.ID,
		MaxContinuations: DefaultMaxContinuations,
		TotalRoundsUsed:  5,
		MaxTotalRounds:   DefaultMaxTotalRounds,
		Budget: &WorkflowBudgetState{
			Policy: DefaultWorkflowBudgetPolicy(),
			Usage: WorkflowBudgetUsage{
				StartedAt:        time.Now().UTC().Add(-3 * time.Minute),
				ModelTotalTokens: 55_000,
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

	if err := component.executeLoop(ctx, run, session, func() {}); err != nil {
		t.Fatalf("executeLoop() error = %v", err)
	}
	if got := len(model.requests); got != 0 {
		t.Fatalf("len(model.requests) = %d, want 0", got)
	}

	fresh, err := runs.Get(ctx, run.ID)
	if err != nil {
		t.Fatalf("runs.Get() error = %v", err)
	}
	if fresh.Status != RunCompleted {
		t.Fatalf("Status = %q, want %q", fresh.Status, RunCompleted)
	}

	found := false
	for _, event := range bus.Snapshot() {
		if event.Type != eventbus.EventWorkflowYielded {
			continue
		}
		found = true
		if got := event.Attrs["workflow_budget_mode"]; got != string(WorkflowBudgetModeStopped) {
			t.Fatalf("workflow_budget_mode = %#v, want %q", got, WorkflowBudgetModeStopped)
		}
		if got := event.Attrs["workflow_circuit_state"]; got != workflowCircuitStateOpen {
			t.Fatalf("workflow_circuit_state = %#v, want %q", got, workflowCircuitStateOpen)
		}
		if got := event.Attrs["workflow_circuit_reason"]; got != "3 no-progress continuations" {
			t.Fatalf("workflow_circuit_reason = %#v", got)
		}
	}
	if !found {
		t.Fatal("expected workflow.yielded event")
	}
}

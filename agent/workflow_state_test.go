package agent

import (
	"testing"
	"time"
)

func TestWorkflowStateNeedsContinuation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		ws   *WorkflowState
		want bool
	}{
		{name: "nil", ws: nil, want: false},
		{name: "not yielded", ws: &WorkflowState{Yielded: false, MaxContinuations: 10}, want: false},
		{name: "yielded under limit", ws: &WorkflowState{Yielded: true, ContinuationIndex: 3, MaxContinuations: 10}, want: true},
		{name: "yielded at limit", ws: &WorkflowState{Yielded: true, ContinuationIndex: 10, MaxContinuations: 10}, want: false},
		{name: "terminal workflow", ws: &WorkflowState{Yielded: true, MaxContinuations: 10, TerminalOutcome: WorkflowTerminalOutcomeFailed}, want: false},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := tt.ws.NeedsContinuation(); got != tt.want {
				t.Fatalf("NeedsContinuation() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestWorkflowStateBudgetExhausted(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		ws   *WorkflowState
		want bool
	}{
		{name: "nil", ws: nil, want: false},
		{name: "under limits", ws: &WorkflowState{ContinuationIndex: 3, MaxContinuations: 10, TotalRoundsUsed: 50, MaxTotalRounds: 100}, want: false},
		{name: "continuation limit", ws: &WorkflowState{ContinuationIndex: 10, MaxContinuations: 10}, want: true},
		{name: "round limit", ws: &WorkflowState{ContinuationIndex: 3, MaxContinuations: 10, TotalRoundsUsed: 100, MaxTotalRounds: 100}, want: true},
		{name: "no round cap", ws: &WorkflowState{ContinuationIndex: 3, MaxContinuations: 10, TotalRoundsUsed: 999, MaxTotalRounds: 0}, want: false},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := tt.ws.BudgetExhausted(); got != tt.want {
				t.Fatalf("BudgetExhausted() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestWorkflowStateEnsureBudgetInitializesDefaults(t *testing.T) {
	t.Parallel()

	ws := &WorkflowState{
		OriginalRunID:     "run-root",
		ContinuationIndex: 2,
		MaxContinuations:  DefaultMaxContinuations,
		MaxTotalRounds:    DefaultMaxTotalRounds,
	}

	now := time.Now().UTC()
	budget := ws.EnsureBudget(now)
	if budget == nil {
		t.Fatal("EnsureBudget() = nil, want budget")
	}
	if budget.Mode != WorkflowBudgetModeNormal {
		t.Fatalf("budget.Mode = %q, want %q", budget.Mode, WorkflowBudgetModeNormal)
	}
	if budget.Policy.HardContinuations != DefaultMaxContinuations {
		t.Fatalf("budget.Policy.HardContinuations = %d, want %d", budget.Policy.HardContinuations, DefaultMaxContinuations)
	}
	if budget.Policy.HardTotalRounds != DefaultMaxTotalRounds {
		t.Fatalf("budget.Policy.HardTotalRounds = %d, want %d", budget.Policy.HardTotalRounds, DefaultMaxTotalRounds)
	}
	if budget.Usage.StartedAt.IsZero() {
		t.Fatal("budget.Usage.StartedAt = zero, want initialized")
	}
	if budget.Usage.StartedContinuationCount != 3 {
		t.Fatalf("budget.Usage.StartedContinuationCount = %d, want 3", budget.Usage.StartedContinuationCount)
	}
}

func TestWorkflowStateBudgetStopReasonHonorsHardBudgetPolicy(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()
	ws := &WorkflowState{
		ContinuationIndex: 1,
		MaxContinuations:  DefaultMaxContinuations,
		Budget: &WorkflowBudgetState{
			Policy: WorkflowBudgetPolicy{
				HardModelTokens: 100,
				HardWallClock:   10 * time.Minute,
			},
			Usage: WorkflowBudgetUsage{
				ModelTotalTokens: 100,
				StartedAt:        now.Add(-11 * time.Minute),
			},
		},
	}

	if got := ws.BudgetStopReason(now); got != YieldReasonBudgetHardLimit {
		t.Fatalf("BudgetStopReason() = %q, want %q", got, YieldReasonBudgetHardLimit)
	}
}

func TestWorkflowStateTerminalLifecycle(t *testing.T) {
	t.Parallel()

	ws := &WorkflowState{
		Yielded: true,
	}

	ws.MarkTerminal(WorkflowTerminalOutcomeFailed, "budget exhausted")
	if !ws.Terminal() {
		t.Fatal("Terminal() = false, want true")
	}
	if ws.Yielded {
		t.Fatal("Yielded = true, want false after terminal mark")
	}
	if ws.TerminalOutcome != WorkflowTerminalOutcomeFailed {
		t.Fatalf("TerminalOutcome = %q, want %q", ws.TerminalOutcome, WorkflowTerminalOutcomeFailed)
	}
	if ws.TerminalReason != "budget exhausted" {
		t.Fatalf("TerminalReason = %q, want %q", ws.TerminalReason, "budget exhausted")
	}

	ws.ClearTerminal()
	if ws.Terminal() {
		t.Fatal("Terminal() = true, want false after clear")
	}
	if ws.TerminalOutcome != "" || ws.TerminalReason != "" {
		t.Fatalf("terminal fields = (%q, %q), want empty", ws.TerminalOutcome, ws.TerminalReason)
	}
}

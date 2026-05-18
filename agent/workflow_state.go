package agent

import (
	"strings"
	"time"
)

type WorkflowBudgetMode string

const (
	WorkflowBudgetModeNormal     WorkflowBudgetMode = "normal"
	WorkflowBudgetModeEconomy    WorkflowBudgetMode = "economy"
	WorkflowBudgetModeFinishOnly WorkflowBudgetMode = "finish_only"
	WorkflowBudgetModeStopped    WorkflowBudgetMode = "stopped"
)

type WorkflowBudgetPolicy struct {
	SoftContinuations int `json:"soft_continuations,omitempty"`
	HardContinuations int `json:"hard_continuations,omitempty"`

	SoftTotalRounds int `json:"soft_total_rounds,omitempty"`
	HardTotalRounds int `json:"hard_total_rounds,omitempty"`

	SoftModelTokens int `json:"soft_model_tokens,omitempty"`
	HardModelTokens int `json:"hard_model_tokens,omitempty"`

	SoftCostEstimate float64 `json:"soft_cost_estimate,omitempty"`
	HardCostEstimate float64 `json:"hard_cost_estimate,omitempty"`

	SoftWallClock time.Duration `json:"soft_wall_clock,omitempty"`
	HardWallClock time.Duration `json:"hard_wall_clock,omitempty"`

	NextRunMultiplier     float64 `json:"next_run_multiplier,omitempty"`
	ReserveModelTokens    int     `json:"reserve_model_tokens,omitempty"`
	ReserveCostEstimate   float64 `json:"reserve_cost_estimate,omitempty"`
	MinContinuationTokens int     `json:"min_continuation_tokens,omitempty"`
	MinContinuationCost   float64 `json:"min_continuation_cost,omitempty"`

	DisableDelegationOnSoftLimit bool    `json:"disable_delegation_on_soft_limit,omitempty"`
	MaxDelegatedTokenFraction    float64 `json:"max_delegated_token_fraction,omitempty"`

	Circuit WorkflowCircuitBreakerPolicy `json:"circuit,omitempty"`
}

type WorkflowCircuitBreakerPolicy struct {
	MaxNoProgressContinuations int     `json:"max_no_progress_continuations,omitempty"`
	MaxRepeatedYieldReason     int     `json:"max_repeated_yield_reason,omitempty"`
	TripBurnRateMultiplier     float64 `json:"trip_burn_rate_multiplier,omitempty"`
	WarnBurnRateMultiplier     float64 `json:"warn_burn_rate_multiplier,omitempty"`
}

type WorkflowCircuitBreakerState struct {
	State                         string    `json:"state,omitempty"`
	Reason                        string    `json:"reason,omitempty"`
	OpenedAt                      time.Time `json:"opened_at,omitempty"`
	TripCount                     int       `json:"trip_count,omitempty"`
	NoProgressContinuations       int       `json:"no_progress_continuations,omitempty"`
	RepeatedYieldReasonCount      int       `json:"repeated_yield_reason_count,omitempty"`
	LastObservedBurnRate          float64   `json:"last_observed_burn_rate,omitempty"`
	LastObservedBurnRateRatio     float64   `json:"last_observed_burn_rate_ratio,omitempty"`
	LastObservedContinuationIndex int       `json:"last_observed_continuation_index,omitempty"`
	LastObservedYieldReason       string    `json:"last_observed_yield_reason,omitempty"`
	LastObservedModelTokens       int       `json:"last_observed_model_tokens,omitempty"`
	LastObservedEstimatedCost     float64   `json:"last_observed_estimated_cost,omitempty"`
	LastObservedTotalRounds       int       `json:"last_observed_total_rounds,omitempty"`
}

type WorkflowBudgetUsage struct {
	ModelPromptTokens     int           `json:"model_prompt_tokens,omitempty"`
	ModelCompletionTokens int           `json:"model_completion_tokens,omitempty"`
	ModelTotalTokens      int           `json:"model_total_tokens,omitempty"`
	EstimatedCost         float64       `json:"estimated_cost,omitempty"`
	ModelCallCount        int           `json:"model_call_count,omitempty"`
	ToolExecutionCount    int           `json:"tool_execution_count,omitempty"`
	ToolExecutionDuration time.Duration `json:"tool_execution_duration,omitempty"`

	UnknownCostCallCount int `json:"unknown_cost_call_count,omitempty"`

	DelegatedTurnsUsed        int `json:"delegated_turns_used,omitempty"`
	DelegatedBudgetTokensUsed int `json:"delegated_budget_tokens_used,omitempty"`

	StartedContinuationCount int       `json:"started_continuation_count,omitempty"`
	CompletedTaskCount       int       `json:"completed_task_count,omitempty"`
	LastCompletedTaskCount   int       `json:"last_completed_task_count,omitempty"`
	StartedAt                time.Time `json:"started_at,omitempty"`
	LastUpdatedAt            time.Time `json:"last_updated_at,omitempty"`
}

type WorkflowBudgetState struct {
	Policy WorkflowBudgetPolicy `json:"policy,omitempty"`
	Usage  WorkflowBudgetUsage  `json:"usage,omitempty"`

	Mode WorkflowBudgetMode `json:"mode,omitempty"`

	PredictedNextRunTokens int     `json:"predicted_next_run_tokens,omitempty"`
	PredictedNextRunCost   float64 `json:"predicted_next_run_cost,omitempty"`

	SoftLimitExceeded bool   `json:"soft_limit_exceeded,omitempty"`
	StopReason        string `json:"stop_reason,omitempty"`

	Circuit WorkflowCircuitBreakerState `json:"circuit,omitempty"`
}

type WorkflowTerminalOutcome string

const (
	WorkflowTerminalOutcomeCompleted WorkflowTerminalOutcome = "completed"
	WorkflowTerminalOutcomeFailed    WorkflowTerminalOutcome = "failed"
)

// WorkflowState tracks execution progress across continuation runs in a workflow.
// It is stored on each Run in the continuation chain.
type WorkflowState struct {
	OriginalRunID string `json:"original_run_id"`

	ContinuationIndex int `json:"continuation_index"`

	MaxContinuations int `json:"max_continuations"`

	TotalRoundsUsed int `json:"total_rounds_used"`

	MaxTotalRounds int `json:"max_total_rounds,omitempty"`

	PriorRunRollup string `json:"prior_run_rollup,omitempty"`

	PriorRunSummaries []string `json:"prior_run_summaries,omitempty"`

	CompletedTaskIDs []string `json:"completed_task_ids,omitempty"`

	Yielded bool `json:"yielded,omitempty"`

	YieldReason string `json:"yield_reason,omitempty"`

	TerminalOutcome WorkflowTerminalOutcome `json:"terminal_outcome,omitempty"`

	TerminalReason string `json:"terminal_reason,omitempty"`

	Budget *WorkflowBudgetState `json:"budget,omitempty"`
}

func (ws *WorkflowState) NeedsContinuation() bool {
	if ws == nil {
		return false
	}
	if ws.Terminal() {
		return false
	}
	if ws.Budget != nil && ws.Budget.Mode == WorkflowBudgetModeStopped {
		return false
	}
	return ws.Yielded && ws.ContinuationIndex < ws.MaxContinuations
}

func (ws *WorkflowState) Terminal() bool {
	if ws == nil {
		return false
	}
	return strings.TrimSpace(string(ws.TerminalOutcome)) != ""
}

func (ws *WorkflowState) MarkTerminal(outcome WorkflowTerminalOutcome, reason string) {
	if ws == nil {
		return
	}
	ws.Yielded = false
	ws.TerminalOutcome = outcome
	ws.TerminalReason = strings.TrimSpace(reason)
}

func (ws *WorkflowState) ClearTerminal() {
	if ws == nil {
		return
	}
	ws.TerminalOutcome = ""
	ws.TerminalReason = ""
}

func (ws *WorkflowState) BudgetExhausted() bool {
	return ws.BudgetStopReason(time.Now().UTC()) != ""
}

func (ws *WorkflowState) BudgetStopReason(now time.Time) string {
	if ws == nil {
		return ""
	}
	hardContinuations := ws.MaxContinuations
	hardTotalRounds := ws.MaxTotalRounds
	if ws.Budget != nil {
		if ws.Budget.Policy.HardContinuations > 0 {
			hardContinuations = ws.Budget.Policy.HardContinuations
		}
		if ws.Budget.Policy.HardTotalRounds > 0 {
			hardTotalRounds = ws.Budget.Policy.HardTotalRounds
		}
	}
	if hardContinuations > 0 && ws.ContinuationIndex >= hardContinuations {
		return YieldReasonBudgetHardLimit
	}
	if hardTotalRounds > 0 && ws.TotalRoundsUsed >= hardTotalRounds {
		return YieldReasonBudgetHardLimit
	}
	if ws.Budget == nil {
		return ""
	}
	if ws.Budget.Policy.HardWallClock > 0 &&
		!ws.Budget.Usage.StartedAt.IsZero() &&
		now.Sub(ws.Budget.Usage.StartedAt) >= ws.Budget.Policy.HardWallClock {
		return YieldReasonBudgetHardLimit
	}
	if ws.Budget.Policy.HardModelTokens > 0 && ws.Budget.Usage.ModelTotalTokens >= ws.Budget.Policy.HardModelTokens {
		return YieldReasonBudgetHardLimit
	}
	if workflowBudgetHasReliableCost(ws.Budget) &&
		ws.Budget.Policy.HardCostEstimate > 0 &&
		ws.Budget.Usage.EstimatedCost >= ws.Budget.Policy.HardCostEstimate {
		return YieldReasonBudgetHardLimit
	}
	return ""
}

const (
	DefaultMaxContinuations = 10
	DefaultMaxTotalRounds   = 100

	YieldReasonRoundBudget        = "round_budget"
	YieldReasonContextLimit       = "context_limit"
	YieldReasonBudgetSoftLimit    = "budget_soft_limit"
	YieldReasonBudgetHardLimit    = "budget_hard_limit"
	YieldReasonAdmissionDenied    = "continuation_admission_denied"
	YieldReasonCircuitBreakerOpen = "circuit_breaker_open"
)

func workflowIDForRun(run *Run) string {
	if run == nil {
		return ""
	}
	if run.WorkflowState != nil {
		return run.WorkflowState.OriginalRunID
	}
	return ""
}

func workflowContinuationIndex(run *Run) int {
	if run == nil || run.WorkflowState == nil {
		return 0
	}
	return run.WorkflowState.ContinuationIndex
}

func (ws *WorkflowState) EnsureBudget(now time.Time) *WorkflowBudgetState {
	if ws == nil {
		return nil
	}
	if ws.Budget == nil {
		ws.Budget = &WorkflowBudgetState{
			Policy: DefaultWorkflowBudgetPolicy(),
			Mode:   WorkflowBudgetModeNormal,
		}
	}
	if workflowBudgetPolicyEmpty(ws.Budget.Policy) {
		ws.Budget.Policy = DefaultWorkflowBudgetPolicy()
	}
	if ws.Budget.Mode == "" {
		ws.Budget.Mode = WorkflowBudgetModeNormal
	}
	if ws.Budget.Policy.HardContinuations <= 0 && ws.MaxContinuations > 0 {
		ws.Budget.Policy.HardContinuations = ws.MaxContinuations
	}
	if ws.Budget.Policy.HardTotalRounds <= 0 && ws.MaxTotalRounds > 0 {
		ws.Budget.Policy.HardTotalRounds = ws.MaxTotalRounds
	}
	if ws.MaxContinuations <= 0 && ws.Budget.Policy.HardContinuations > 0 {
		ws.MaxContinuations = ws.Budget.Policy.HardContinuations
	}
	if ws.MaxTotalRounds <= 0 && ws.Budget.Policy.HardTotalRounds > 0 {
		ws.MaxTotalRounds = ws.Budget.Policy.HardTotalRounds
	}
	if ws.Budget.Usage.StartedAt.IsZero() {
		ws.Budget.Usage.StartedAt = now
	}
	if ws.Budget.Usage.LastUpdatedAt.IsZero() {
		ws.Budget.Usage.LastUpdatedAt = now
	}
	startedContinuationCount := ws.ContinuationIndex + 1
	if startedContinuationCount > ws.Budget.Usage.StartedContinuationCount {
		ws.Budget.Usage.StartedContinuationCount = startedContinuationCount
	}
	return ws.Budget
}

func DefaultWorkflowBudgetPolicy() WorkflowBudgetPolicy {
	return WorkflowBudgetPolicy{
		SoftContinuations: 6,
		HardContinuations: DefaultMaxContinuations,
		SoftTotalRounds:   60,
		HardTotalRounds:   DefaultMaxTotalRounds,
		SoftModelTokens:   400_000,
		HardModelTokens:   1_000_000,
		SoftCostEstimate:  1.00,
		HardCostEstimate:  3.00,
		SoftWallClock:     15 * time.Minute,
		HardWallClock:     30 * time.Minute,

		NextRunMultiplier:     1.25,
		ReserveModelTokens:    50_000,
		ReserveCostEstimate:   0.25,
		MinContinuationTokens: 12_000,
		MinContinuationCost:   0.05,

		DisableDelegationOnSoftLimit: true,
		MaxDelegatedTokenFraction:    0.40,
		Circuit: WorkflowCircuitBreakerPolicy{
			MaxNoProgressContinuations: 2,
			MaxRepeatedYieldReason:     2,
			TripBurnRateMultiplier:     2.5,
			WarnBurnRateMultiplier:     2.0,
		},
	}
}

func workflowBudgetPolicyEmpty(policy WorkflowBudgetPolicy) bool {
	return policy == (WorkflowBudgetPolicy{})
}

func workflowBudgetHasReliableCost(budget *WorkflowBudgetState) bool {
	if budget == nil {
		return false
	}
	return budget.Usage.UnknownCostCallCount == 0
}

func updateWorkflowBudgetModelUsage(
	run *Run,
	promptTokens, completionTokens, totalTokens int,
	costEstimate float64,
	now time.Time,
) bool {
	if run == nil || run.WorkflowState == nil {
		return false
	}
	budget := run.WorkflowState.EnsureBudget(now)
	if budget == nil {
		return false
	}
	budget.Usage.ModelPromptTokens += promptTokens
	budget.Usage.ModelCompletionTokens += completionTokens
	budget.Usage.ModelTotalTokens += totalTokens
	budget.Usage.EstimatedCost += costEstimate
	budget.Usage.ModelCallCount++
	if totalTokens > 0 && costEstimate == 0 {
		budget.Usage.UnknownCostCallCount++
	}
	budget.Usage.LastUpdatedAt = now
	return true
}

func updateWorkflowBudgetToolUsage(run *Run, toolCallCount int, duration time.Duration, now time.Time) bool {
	if run == nil || run.WorkflowState == nil || toolCallCount <= 0 {
		return false
	}
	budget := run.WorkflowState.EnsureBudget(now)
	if budget == nil {
		return false
	}
	budget.Usage.ToolExecutionCount += toolCallCount
	budget.Usage.ToolExecutionDuration += duration
	budget.Usage.LastUpdatedAt = now
	return true
}

package agent

import (
	"fmt"
	"math"
	"strings"
	"time"

	planpkg "github.com/fulcrus/hopclaw/planner"
)

const (
	workflowCircuitStateClosed   = "closed"
	workflowCircuitStateOpen     = "open"
	workflowCircuitStateHalfOpen = "half_open"
)

type WorkflowBudgetDecision struct {
	Mode              WorkflowBudgetMode
	AllowCurrentTurn  bool
	AllowContinuation bool
	DisableDelegation bool
	ForceCompaction   bool
	YieldReason       string
	StopReason        string
}

func evaluateWorkflowBudget(run *Run) WorkflowBudgetDecision {
	decision := WorkflowBudgetDecision{
		Mode:              WorkflowBudgetModeNormal,
		AllowCurrentTurn:  true,
		AllowContinuation: true,
	}
	if run == nil || run.ExecutionMode != ExecutionModeWorkflow || run.WorkflowState == nil || run.WorkflowState.Budget == nil {
		return decision
	}

	now := time.Now().UTC()
	ws := run.WorkflowState
	budget := ws.EnsureBudget(now)
	if budget == nil {
		return decision
	}

	completedTaskCount := len(ws.CompletedTaskIDs)
	if run.Plan != nil {
		completedTaskCount = max(completedTaskCount, planpkg.CompletedCount(run.Plan))
	}
	budget.Usage.CompletedTaskCount = completedTaskCount
	budget.PredictedNextRunTokens = predictWorkflowContinuationTokens(budget.Policy, budget.Usage, ws.TotalRoundsUsed)
	budget.PredictedNextRunCost = predictWorkflowContinuationCost(budget.Policy, budget.Usage, ws.TotalRoundsUsed)

	if stopReason := ws.BudgetStopReason(now); stopReason != "" {
		budget.Mode = WorkflowBudgetModeStopped
		budget.StopReason = stopReason
		budget.SoftLimitExceeded = workflowBudgetSoftLimitExceeded(ws, now)
		return workflowBudgetStopDecision(stopReason, workflowBudgetStopDetail(stopReason, budget.Circuit.Reason))
	}

	switch strings.ToLower(strings.TrimSpace(budget.Circuit.State)) {
	case workflowCircuitStateOpen:
		budget.Mode = WorkflowBudgetModeStopped
		budget.StopReason = YieldReasonCircuitBreakerOpen
		budget.SoftLimitExceeded = true
		return workflowBudgetStopDecision(YieldReasonCircuitBreakerOpen, workflowBudgetStopDetail(YieldReasonCircuitBreakerOpen, budget.Circuit.Reason))
	case workflowCircuitStateHalfOpen:
		budget.Mode = WorkflowBudgetModeFinishOnly
		budget.StopReason = ""
		budget.SoftLimitExceeded = true
		return WorkflowBudgetDecision{
			Mode:              WorkflowBudgetModeFinishOnly,
			AllowCurrentTurn:  true,
			AllowContinuation: false,
			DisableDelegation: true,
			ForceCompaction:   true,
		}
	}

	softLimitExceeded := workflowBudgetSoftLimitExceeded(ws, now)
	budget.SoftLimitExceeded = softLimitExceeded

	mode := WorkflowBudgetModeNormal
	if softLimitExceeded {
		mode = WorkflowBudgetModeEconomy
		if workflowBudgetNearHardLimit(ws, now) || workflowBudgetHasCircuitWarning(budget) {
			mode = WorkflowBudgetModeFinishOnly
		}
	} else if workflowBudgetHasCircuitWarning(budget) {
		mode = WorkflowBudgetModeFinishOnly
	}

	budget.Mode = mode
	budget.StopReason = ""
	return WorkflowBudgetDecision{
		Mode:              mode,
		AllowCurrentTurn:  true,
		AllowContinuation: true,
		DisableDelegation: workflowBudgetDisablesDelegation(budget, mode),
		ForceCompaction:   mode == WorkflowBudgetModeEconomy || mode == WorkflowBudgetModeFinishOnly,
	}
}

func observeWorkflowContinuationOutcome(run *Run) {
	if run == nil || run.ExecutionMode != ExecutionModeWorkflow || run.WorkflowState == nil {
		return
	}
	ws := run.WorkflowState
	budget := ws.EnsureBudget(time.Now().UTC())
	if budget == nil {
		return
	}
	completedTaskCount := len(ws.CompletedTaskIDs)
	if run.Plan != nil {
		completedTaskCount = max(completedTaskCount, planpkg.CompletedCount(run.Plan))
	}
	budget.Usage.CompletedTaskCount = completedTaskCount
	updateCircuitBreakerObservation(ws)
}

func updateCircuitBreakerObservation(ws *WorkflowState) {
	if ws == nil || ws.Budget == nil {
		return
	}

	budget := ws.Budget
	circuit := &budget.Circuit
	policy := budget.Policy.Circuit
	if strings.TrimSpace(circuit.State) == "" {
		circuit.State = workflowCircuitStateClosed
	}
	if strings.EqualFold(strings.TrimSpace(circuit.State), workflowCircuitStateOpen) {
		return
	}

	currentContinuation := ws.ContinuationIndex
	if circuit.LastObservedContinuationIndex == currentContinuation &&
		(circuit.LastObservedTotalRounds != 0 ||
			circuit.LastObservedModelTokens != 0 ||
			circuit.LastObservedYieldReason != "" ||
			budget.Usage.LastCompletedTaskCount != 0 ||
			!circuit.OpenedAt.IsZero()) {
		return
	}

	currentCompleted := budget.Usage.CompletedTaskCount
	previousCompleted := budget.Usage.LastCompletedTaskCount
	progressMade := currentCompleted > previousCompleted
	if progressMade {
		circuit.NoProgressContinuations = 0
	} else if currentContinuation > 0 || previousCompleted > 0 || strings.TrimSpace(ws.YieldReason) != "" {
		circuit.NoProgressContinuations++
	}
	budget.Usage.LastCompletedTaskCount = currentCompleted

	currentYieldReason := strings.TrimSpace(ws.YieldReason)
	switch {
	case currentYieldReason == "":
		circuit.RepeatedYieldReasonCount = 0
		circuit.LastObservedYieldReason = ""
	case currentYieldReason == strings.TrimSpace(circuit.LastObservedYieldReason):
		circuit.RepeatedYieldReasonCount++
	default:
		circuit.RepeatedYieldReasonCount = 1
		circuit.LastObservedYieldReason = currentYieldReason
	}

	currentBurnRate, burnRateRatio := workflowCircuitBurnRate(ws)
	circuit.LastObservedBurnRateRatio = burnRateRatio

	if strings.EqualFold(strings.TrimSpace(circuit.State), workflowCircuitStateHalfOpen) {
		if !progressMade || workflowCircuitBurnRateTrips(policy, burnRateRatio) || workflowCircuitRepeatedYieldTrips(policy, circuit.RepeatedYieldReasonCount) {
			openWorkflowCircuit(circuit, workflowCircuitTripReason(ws, progressMade, burnRateRatio))
		} else {
			circuit.State = workflowCircuitStateClosed
			circuit.Reason = ""
			circuit.OpenedAt = time.Time{}
		}
	} else {
		switch {
		case workflowCircuitNoProgressTrips(policy, circuit.NoProgressContinuations):
			openWorkflowCircuit(circuit, fmt.Sprintf("%d no-progress continuations", circuit.NoProgressContinuations))
		case workflowCircuitRepeatedYieldTrips(policy, circuit.RepeatedYieldReasonCount):
			openWorkflowCircuit(circuit, fmt.Sprintf("repeated yield_reason=%s", currentYieldReason))
		case workflowCircuitBurnRateTrips(policy, burnRateRatio):
			openWorkflowCircuit(circuit, fmt.Sprintf("burn rate spike %.2fx", burnRateRatio))
		default:
			if warning := workflowCircuitWarningReason(ws, progressMade, burnRateRatio); warning != "" {
				circuit.Reason = warning
			} else if strings.EqualFold(strings.TrimSpace(circuit.State), workflowCircuitStateClosed) {
				circuit.Reason = ""
			}
		}
	}

	circuit.LastObservedBurnRate = currentBurnRate
	circuit.LastObservedModelTokens = budget.Usage.ModelTotalTokens
	circuit.LastObservedEstimatedCost = budget.Usage.EstimatedCost
	circuit.LastObservedTotalRounds = ws.TotalRoundsUsed
	circuit.LastObservedContinuationIndex = currentContinuation
}

func workflowBudgetSoftLimitExceeded(ws *WorkflowState, now time.Time) bool {
	if ws == nil || ws.Budget == nil {
		return false
	}
	budget := ws.Budget
	policy := budget.Policy
	if policy.SoftContinuations > 0 && ws.ContinuationIndex >= policy.SoftContinuations {
		return true
	}
	if policy.SoftTotalRounds > 0 && ws.TotalRoundsUsed >= policy.SoftTotalRounds {
		return true
	}
	if policy.SoftWallClock > 0 &&
		!budget.Usage.StartedAt.IsZero() &&
		now.Sub(budget.Usage.StartedAt) >= policy.SoftWallClock {
		return true
	}
	if policy.SoftModelTokens > 0 && budget.Usage.ModelTotalTokens >= policy.SoftModelTokens {
		return true
	}
	if workflowBudgetHasReliableCost(budget) &&
		policy.SoftCostEstimate > 0 &&
		budget.Usage.EstimatedCost >= policy.SoftCostEstimate {
		return true
	}
	return false
}

func workflowBudgetNearHardLimit(ws *WorkflowState, now time.Time) bool {
	if ws == nil || ws.Budget == nil {
		return false
	}
	budget := ws.Budget
	policy := budget.Policy
	if policy.HardContinuations > 0 && policy.HardContinuations-ws.ContinuationIndex <= 1 {
		return true
	}
	if policy.HardTotalRounds > 0 && policy.HardTotalRounds-ws.TotalRoundsUsed <= 1 {
		return true
	}
	if policy.HardWallClock > 0 && !budget.Usage.StartedAt.IsZero() {
		remaining := policy.HardWallClock - now.Sub(budget.Usage.StartedAt)
		if remaining <= 5*time.Minute {
			return true
		}
	}
	if remaining := workflowBudgetRemainingModelTokens(ws); remaining > 0 && budget.PredictedNextRunTokens > 0 {
		if remaining <= budget.PredictedNextRunTokens+budget.Policy.ReserveModelTokens {
			return true
		}
	}
	if workflowBudgetHasReliableCost(budget) {
		if remaining := workflowBudgetRemainingCost(ws); remaining > 0 && budget.PredictedNextRunCost > 0 {
			if remaining <= budget.PredictedNextRunCost+budget.Policy.ReserveCostEstimate {
				return true
			}
		}
	}
	return false
}

func workflowBudgetHasCircuitWarning(budget *WorkflowBudgetState) bool {
	if budget == nil {
		return false
	}
	policy := budget.Policy.Circuit
	if strings.EqualFold(strings.TrimSpace(budget.Circuit.State), workflowCircuitStateHalfOpen) {
		return true
	}
	if policy.MaxNoProgressContinuations > 0 && budget.Circuit.NoProgressContinuations >= policy.MaxNoProgressContinuations {
		return true
	}
	if policy.MaxRepeatedYieldReason > 0 && budget.Circuit.RepeatedYieldReasonCount >= policy.MaxRepeatedYieldReason {
		return true
	}
	return policy.WarnBurnRateMultiplier > 0 && budget.Circuit.LastObservedBurnRateRatio >= policy.WarnBurnRateMultiplier
}

func workflowBudgetDisablesDelegation(budget *WorkflowBudgetState, mode WorkflowBudgetMode) bool {
	if budget == nil {
		return false
	}
	switch mode {
	case WorkflowBudgetModeFinishOnly, WorkflowBudgetModeStopped:
		return true
	case WorkflowBudgetModeEconomy:
		return budget.Policy.DisableDelegationOnSoftLimit
	default:
		return false
	}
}

func workflowBudgetRemainingModelTokens(ws *WorkflowState) int {
	if ws == nil || ws.Budget == nil || ws.Budget.Policy.HardModelTokens <= 0 {
		return 0
	}
	return max(ws.Budget.Policy.HardModelTokens-ws.Budget.Usage.ModelTotalTokens, 0)
}

func workflowBudgetRemainingCost(ws *WorkflowState) float64 {
	if ws == nil || ws.Budget == nil || ws.Budget.Policy.HardCostEstimate <= 0 {
		return 0
	}
	return math.Max(ws.Budget.Policy.HardCostEstimate-ws.Budget.Usage.EstimatedCost, 0)
}

func workflowBudgetStopDecision(yieldReason, detail string) WorkflowBudgetDecision {
	if strings.TrimSpace(detail) == "" {
		detail = workflowBudgetStopDetail(yieldReason, "")
	}
	return WorkflowBudgetDecision{
		Mode:              WorkflowBudgetModeStopped,
		AllowCurrentTurn:  false,
		AllowContinuation: false,
		DisableDelegation: true,
		YieldReason:       yieldReason,
		StopReason:        detail,
	}
}

func workflowBudgetStopDetail(yieldReason, circuitReason string) string {
	switch strings.TrimSpace(yieldReason) {
	case YieldReasonCircuitBreakerOpen:
		if strings.TrimSpace(circuitReason) != "" {
			return "workflow auto-continuation stopped: " + strings.TrimSpace(circuitReason)
		}
		return "workflow auto-continuation stopped: circuit breaker opened"
	case YieldReasonBudgetHardLimit:
		return "workflow auto-continuation stopped: budget hard limit reached"
	default:
		if strings.TrimSpace(circuitReason) != "" {
			return strings.TrimSpace(circuitReason)
		}
		return strings.ReplaceAll(strings.TrimSpace(yieldReason), "_", " ")
	}
}

func workflowCircuitBurnRate(ws *WorkflowState) (float64, float64) {
	if ws == nil || ws.Budget == nil {
		return 0, 0
	}
	budget := ws.Budget
	circuit := budget.Circuit
	deltaRounds := ws.TotalRoundsUsed - circuit.LastObservedTotalRounds
	if deltaRounds <= 0 {
		return 0, 0
	}

	burnRate := 0.0
	if workflowBudgetHasReliableCost(budget) {
		if deltaCost := budget.Usage.EstimatedCost - circuit.LastObservedEstimatedCost; deltaCost > 0 {
			burnRate = deltaCost / float64(deltaRounds)
		}
	}
	if burnRate == 0 {
		if deltaTokens := budget.Usage.ModelTotalTokens - circuit.LastObservedModelTokens; deltaTokens > 0 {
			burnRate = float64(deltaTokens) / float64(deltaRounds)
		}
	}
	if circuit.LastObservedBurnRate <= 0 || burnRate <= 0 {
		return burnRate, 0
	}
	return burnRate, burnRate / circuit.LastObservedBurnRate
}

func workflowCircuitNoProgressTrips(policy WorkflowCircuitBreakerPolicy, count int) bool {
	return policy.MaxNoProgressContinuations > 0 && count > policy.MaxNoProgressContinuations
}

func workflowCircuitRepeatedYieldTrips(policy WorkflowCircuitBreakerPolicy, count int) bool {
	return policy.MaxRepeatedYieldReason > 0 && count > policy.MaxRepeatedYieldReason
}

func workflowCircuitBurnRateTrips(policy WorkflowCircuitBreakerPolicy, ratio float64) bool {
	return policy.TripBurnRateMultiplier > 0 && ratio >= policy.TripBurnRateMultiplier
}

func workflowCircuitWarningReason(ws *WorkflowState, progressMade bool, burnRateRatio float64) string {
	if ws == nil || ws.Budget == nil {
		return ""
	}
	policy := ws.Budget.Policy.Circuit
	switch {
	case !progressMade && policy.MaxNoProgressContinuations > 0 && ws.Budget.Circuit.NoProgressContinuations >= policy.MaxNoProgressContinuations:
		return fmt.Sprintf("%d no-progress continuations", ws.Budget.Circuit.NoProgressContinuations)
	case strings.TrimSpace(ws.YieldReason) != "" &&
		policy.MaxRepeatedYieldReason > 0 &&
		ws.Budget.Circuit.RepeatedYieldReasonCount >= policy.MaxRepeatedYieldReason:
		return fmt.Sprintf("repeated yield_reason=%s", strings.TrimSpace(ws.YieldReason))
	case policy.WarnBurnRateMultiplier > 0 && burnRateRatio >= policy.WarnBurnRateMultiplier:
		return fmt.Sprintf("burn rate spike %.2fx", burnRateRatio)
	default:
		return ""
	}
}

func workflowCircuitTripReason(ws *WorkflowState, progressMade bool, burnRateRatio float64) string {
	if ws == nil || ws.Budget == nil {
		return "circuit breaker opened"
	}
	if !progressMade {
		return "half-open continuation made no progress"
	}
	if workflowCircuitBurnRateTrips(ws.Budget.Policy.Circuit, burnRateRatio) {
		return fmt.Sprintf("burn rate spike %.2fx", burnRateRatio)
	}
	if strings.TrimSpace(ws.YieldReason) != "" {
		return fmt.Sprintf("repeated yield_reason=%s", strings.TrimSpace(ws.YieldReason))
	}
	return "circuit breaker opened"
}

func openWorkflowCircuit(circuit *WorkflowCircuitBreakerState, reason string) {
	if circuit == nil {
		return
	}
	circuit.State = workflowCircuitStateOpen
	circuit.Reason = strings.TrimSpace(reason)
	circuit.OpenedAt = time.Now().UTC()
	circuit.TripCount++
}

func workflowBudgetEventAttrs(run *Run) map[string]any {
	if run == nil || run.WorkflowState == nil || run.WorkflowState.Budget == nil {
		return nil
	}
	budget := run.WorkflowState.Budget
	out := map[string]any{
		"workflow_budget_mode":           string(budget.Mode),
		"workflow_budget_estimated_cost": budget.Usage.EstimatedCost,
		"workflow_budget_model_tokens":   budget.Usage.ModelTotalTokens,
		"workflow_circuit_state":         budget.Circuit.State,
		"workflow_circuit_reason":        budget.Circuit.Reason,
	}
	if budget.Policy.HardModelTokens > 0 {
		remainingTokens := workflowBudgetRemainingModelTokens(run.WorkflowState)
		out["workflow_budget_remaining_tokens"] = remainingTokens
	}
	if budget.Policy.HardCostEstimate > 0 {
		remainingCost := workflowBudgetRemainingCost(run.WorkflowState)
		out["workflow_budget_remaining_cost"] = remainingCost
	}
	return out
}

func predictWorkflowContinuationTokens(policy WorkflowBudgetPolicy, usageState WorkflowBudgetUsage, totalRoundsUsed int) int {
	rounds := max(totalRoundsUsed, 1)
	multiplier := policy.NextRunMultiplier
	if multiplier <= 0 {
		multiplier = 1
	}
	predicted := int(math.Ceil(float64(usageState.ModelTotalTokens) / float64(rounds) * multiplier))
	return max(predicted, policy.MinContinuationTokens)
}

func predictWorkflowContinuationCost(policy WorkflowBudgetPolicy, usageState WorkflowBudgetUsage, totalRoundsUsed int) float64 {
	rounds := max(totalRoundsUsed, 1)
	multiplier := policy.NextRunMultiplier
	if multiplier <= 0 {
		multiplier = 1
	}
	predicted := usageState.EstimatedCost / float64(rounds) * multiplier
	return math.Max(predicted, policy.MinContinuationCost)
}

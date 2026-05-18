package runtime

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"strings"
	"time"

	"github.com/fulcrus/hopclaw/agent"
	"github.com/fulcrus/hopclaw/eventbus"
	"github.com/fulcrus/hopclaw/logging"
	"github.com/fulcrus/hopclaw/planner"
	"github.com/fulcrus/hopclaw/usage"
)

func (s *Service) checkAndDispatchWorkflowContinuation(ctx context.Context, finishedRunID string) bool {
	if s == nil || s.agent == nil || strings.TrimSpace(finishedRunID) == "" {
		return false
	}
	run, err := s.runs.Get(ctx, finishedRunID)
	if err != nil || run == nil {
		return false
	}
	if run.WorkflowState == nil || !run.WorkflowState.NeedsContinuation() {
		return false
	}
	if allow, denyReason := s.admitWorkflowContinuation(ctx, run); !allow {
		slog.InfoContext(ctx, "workflow continuation denied",
			slog.String("parent_run_id", finishedRunID),
			slog.String("session_id", run.SessionID),
			slog.String("reason", denyReason),
		)
		return false
	}

	contRun, err := s.createContinuationRun(ctx, run)
	if err != nil {
		logging.LogIfErr(ctx, err, "workflow continuation creation failed",
			slog.String("parent_run_id", finishedRunID),
			slog.String("session_id", run.SessionID),
		)
		return false
	}

	if err := s.dispatchRun(ctx, contRun.ID, false); err != nil {
		logging.LogIfErr(ctx, err, "workflow continuation dispatch failed",
			slog.String("continuation_run_id", contRun.ID),
			slog.String("parent_run_id", finishedRunID),
		)
		return false
	}

	slog.InfoContext(ctx, "workflow continuation dispatched",
		slog.String("parent_run_id", finishedRunID),
		slog.String("continuation_run_id", contRun.ID),
		slog.Int("continuation_index", contRun.WorkflowState.ContinuationIndex),
	)
	return true
}

func (s *Service) admitWorkflowContinuation(ctx context.Context, run *agent.Run) (bool, string) {
	if s == nil || run == nil || run.WorkflowState == nil {
		return false, ""
	}

	now := time.Now().UTC()
	if err := s.resyncWorkflowBudgetUsage(ctx, run, now); err != nil {
		logging.LogIfErr(ctx, err, "workflow continuation usage resync failed",
			slog.String("run_id", run.ID),
			slog.String("workflow_id", run.WorkflowState.OriginalRunID),
		)
	}

	budget := run.WorkflowState.EnsureBudget(now)
	if budget == nil {
		return false, agent.YieldReasonAdmissionDenied
	}
	if strings.EqualFold(strings.TrimSpace(budget.Circuit.State), "open") {
		s.markWorkflowContinuationDenied(ctx, run, now, agent.YieldReasonCircuitBreakerOpen)
		return false, agent.YieldReasonCircuitBreakerOpen
	}
	if strings.EqualFold(strings.TrimSpace(budget.Circuit.State), "half_open") {
		s.markWorkflowContinuationDenied(ctx, run, now, agent.YieldReasonAdmissionDenied)
		return false, agent.YieldReasonAdmissionDenied
	}
	if stopReason := run.WorkflowState.BudgetStopReason(now); stopReason != "" {
		s.markWorkflowContinuationDenied(ctx, run, now, stopReason)
		return false, stopReason
	}

	policy := budget.Policy
	budget.PredictedNextRunTokens = predictWorkflowContinuationTokens(policy, budget.Usage, run.WorkflowState.TotalRoundsUsed)
	budget.PredictedNextRunCost = predictWorkflowContinuationCost(policy, budget.Usage, run.WorkflowState.TotalRoundsUsed)

	if remaining := workflowRemainingContinuations(run.WorkflowState, policy); remaining < 1 {
		s.markWorkflowContinuationDenied(ctx, run, now, agent.YieldReasonBudgetHardLimit)
		return false, agent.YieldReasonBudgetHardLimit
	}
	if remaining := workflowRemainingRounds(run.WorkflowState, policy); remaining < 1 {
		s.markWorkflowContinuationDenied(ctx, run, now, agent.YieldReasonBudgetHardLimit)
		return false, agent.YieldReasonBudgetHardLimit
	}
	if policy.HardModelTokens > 0 {
		remainingTokens := policy.HardModelTokens - budget.Usage.ModelTotalTokens
		if remainingTokens < budget.PredictedNextRunTokens+policy.ReserveModelTokens {
			s.markWorkflowContinuationDenied(ctx, run, now, agent.YieldReasonAdmissionDenied)
			return false, agent.YieldReasonAdmissionDenied
		}
	}
	if policy.HardCostEstimate > 0 && budget.Usage.UnknownCostCallCount == 0 {
		remainingCost := policy.HardCostEstimate - budget.Usage.EstimatedCost
		if remainingCost < budget.PredictedNextRunCost+policy.ReserveCostEstimate {
			s.markWorkflowContinuationDenied(ctx, run, now, agent.YieldReasonAdmissionDenied)
			return false, agent.YieldReasonAdmissionDenied
		}
	}

	budget.StopReason = ""
	logging.LogIfErr(ctx, s.runs.Update(ctx, run), "persist workflow continuation admission failed", slog.String("run_id", run.ID))
	return true, ""
}

func (s *Service) resyncWorkflowBudgetUsage(ctx context.Context, run *agent.Run, now time.Time) error {
	if s == nil || s.agent == nil || run == nil || run.WorkflowState == nil {
		return nil
	}
	store := s.agent.UsageStore()
	if store == nil {
		return nil
	}

	workflowID := strings.TrimSpace(run.WorkflowState.OriginalRunID)
	if workflowID == "" {
		workflowID = strings.TrimSpace(run.ID)
	}
	if workflowID == "" {
		return nil
	}

	records, err := store.Query(ctx, usage.QueryFilter{WorkflowID: workflowID})
	if err != nil {
		return fmt.Errorf("query workflow usage: %w", err)
	}

	budget := run.WorkflowState.EnsureBudget(now)
	if budget == nil {
		return nil
	}
	resynced := budget.Usage
	resynced.ModelPromptTokens = 0
	resynced.ModelCompletionTokens = 0
	resynced.ModelTotalTokens = 0
	resynced.EstimatedCost = 0
	resynced.ModelCallCount = 0
	resynced.ToolExecutionCount = 0
	resynced.ToolExecutionDuration = 0
	resynced.UnknownCostCallCount = 0
	if startedContinuationCount := run.WorkflowState.ContinuationIndex + 1; startedContinuationCount > resynced.StartedContinuationCount {
		resynced.StartedContinuationCount = startedContinuationCount
	}

	if resynced.StartedAt.IsZero() {
		resynced.StartedAt = now
	}
	lastUpdatedAt := resynced.LastUpdatedAt
	if lastUpdatedAt.IsZero() {
		lastUpdatedAt = now
	}
	for _, rec := range records {
		if !rec.CreatedAt.IsZero() && rec.CreatedAt.Before(resynced.StartedAt) {
			resynced.StartedAt = rec.CreatedAt
		}
		if rec.CreatedAt.After(lastUpdatedAt) {
			lastUpdatedAt = rec.CreatedAt
		}
		switch normalizedUsageRecordType(rec) {
		case usage.RecordTypeToolExecution:
			resynced.ToolExecutionCount++
			resynced.ToolExecutionDuration += rec.Duration
		default:
			resynced.ModelPromptTokens += rec.PromptTokens
			resynced.ModelCompletionTokens += rec.CompletionTokens
			resynced.ModelTotalTokens += rec.TotalTokens
			resynced.EstimatedCost += rec.CostEstimate
			resynced.ModelCallCount++
			if rec.TotalTokens > 0 && rec.CostEstimate == 0 {
				resynced.UnknownCostCallCount++
			}
		}
	}
	resynced.LastUpdatedAt = lastUpdatedAt
	budget.Usage = resynced
	return nil
}

func (s *Service) createContinuationRun(ctx context.Context, parent *agent.Run) (*agent.Run, error) {
	if s == nil || s.agent == nil {
		return nil, fmt.Errorf("runtime service is not configured")
	}
	if parent == nil || parent.WorkflowState == nil {
		return nil, fmt.Errorf("parent run has no workflow state")
	}

	reader := agent.SessionReaderCapability(s.sessions)
	if reader == nil {
		return nil, fmt.Errorf("session reader capability is unavailable")
	}
	session, err := reader.Get(ctx, parent.SessionID)
	if err != nil {
		return nil, fmt.Errorf("load continuation session: %w", err)
	}
	if session == nil || strings.TrimSpace(session.Key) == "" {
		return nil, fmt.Errorf("cannot resolve session key for continuation (session_id=%s)", parent.SessionID)
	}

	contRun, err := s.submit(ctx, SubmitRequest{
		SessionID:   session.ID,
		SessionKey:  session.Key,
		ParentRunID: parent.ID,
		Content:     buildContinuationPrompt(parent),
		Model:       parent.Model,
		Metadata: map[string]any{
			"workflow_continuation": true,
			"workflow_original_id":  parent.WorkflowState.OriginalRunID,
			"continuation_index":    parent.WorkflowState.ContinuationIndex + 1,
		},
		Execute: continuationBoolPtr(false),
	}, submitOptions{
		skipRateLimit:  true,
		skipAgentRoute: true,
	})
	if err != nil {
		return nil, fmt.Errorf("continuation submit failed: %w", err)
	}

	contRun.ExecutionMode = agent.ExecutionModeWorkflow
	contRun.Plan = clonePlanForContinuation(parent.Plan)
	contRun.WorkflowState = buildContinuationWorkflowState(parent)

	if err := s.runs.Update(ctx, contRun); err != nil {
		return nil, fmt.Errorf("continuation state update failed: %w", err)
	}
	contRun, err = s.runs.Get(ctx, contRun.ID)
	if err != nil {
		return nil, err
	}
	logging.LogIfErr(ctx, s.publish(ctx, eventbus.NewWorkflowContinuedEvent(
		contRun.ID,
		contRun.SessionID,
		eventbus.WorkflowEventAttrs{
			OriginalRunID:     contRun.WorkflowState.OriginalRunID,
			ContinuationIndex: contRun.WorkflowState.ContinuationIndex,
			TotalRoundsUsed:   contRun.WorkflowState.TotalRoundsUsed,
			CompletedTasks:    planner.CompletedCount(contRun.Plan),
			TotalTasks:        planner.TotalCount(contRun.Plan),
			Summary:           "workflow continuation created",
		},
		agent.BuildRunEventAttrs(contRun),
	)), "emit event failed", slog.String("kind", string(eventbus.EventWorkflowContinued)))
	return contRun, nil
}

func buildContinuationPrompt(parent *agent.Run) string {
	var builder strings.Builder
	builder.WriteString("Continue the workflow from where the previous round left off.\n\n")

	if parent != nil && parent.WorkflowState != nil && len(parent.WorkflowState.PriorRunSummaries) > 0 {
		builder.WriteString("## Prior rounds\n")
		for _, summary := range parent.WorkflowState.PriorRunSummaries {
			fmt.Fprintf(&builder, "- %s\n", summary)
		}
		builder.WriteString("\n")
	}

	if parent != nil && parent.Plan != nil {
		completed := 0
		total := len(parent.Plan.Tasks)
		var pending []string
		for _, task := range parent.Plan.Tasks {
			switch task.Status {
			case planner.TaskCompleted:
				completed++
			case planner.TaskQueued, planner.TaskRunning:
				pending = append(pending, fmt.Sprintf("- [%s] %s: %s", task.ID, task.Kind, task.Goal))
			}
		}
		fmt.Fprintf(&builder, "## Progress: %d/%d tasks completed\n\n", completed, total)
		if len(pending) > 0 {
			builder.WriteString("## Remaining tasks\n")
			for _, line := range pending {
				builder.WriteString(line)
				builder.WriteString("\n")
			}
			builder.WriteString("\n")
		}
	}

	builder.WriteString("Pick up from the next incomplete task. Do not redo completed work. ")
	builder.WriteString("If blocked, output BLOCKED: <reason> and stop.")
	return builder.String()
}

func clonePlanForContinuation(plan *planner.Plan) *planner.Plan {
	if plan == nil {
		return nil
	}
	cloned := *plan
	cloned.Tasks = make([]planner.Task, len(plan.Tasks))
	for i, task := range plan.Tasks {
		cloned.Tasks[i] = task
		cloned.Tasks[i].DependsOn = append([]string(nil), task.DependsOn...)
		cloned.Tasks[i].Outputs = append([]string(nil), task.Outputs...)
		cloned.Tasks[i].RequiredCapabilities = append([]string(nil), task.RequiredCapabilities...)
		cloned.Tasks[i].VerificationHints = append([]string(nil), task.VerificationHints...)
		if cloned.Tasks[i].Status == planner.TaskRunning {
			cloned.Tasks[i].Status = planner.TaskQueued
		}
	}
	cloned.ActiveTask = ""
	cloned.RunningTasks = nil
	cloned.CoverageWarnings = append([]string(nil), plan.CoverageWarnings...)
	return &cloned
}

func buildContinuationWorkflowState(parent *agent.Run) *agent.WorkflowState {
	if parent == nil || parent.WorkflowState == nil {
		return nil
	}

	now := time.Now().UTC()
	parentBudget := parent.WorkflowState.EnsureBudget(now)
	budget := cloneWorkflowBudgetState(parentBudget)
	if budget != nil {
		startedContinuationCount := parent.WorkflowState.ContinuationIndex + 2
		if startedContinuationCount > budget.Usage.StartedContinuationCount {
			budget.Usage.StartedContinuationCount = startedContinuationCount
		}
		budget.Usage.LastUpdatedAt = now
	}

	priorSummaries := append([]string(nil), parent.WorkflowState.PriorRunSummaries...)
	priorSummaries = append(priorSummaries, fmt.Sprintf("Run %s: %s", parent.ID, agent.BuildWorkflowRunSummary(parent)))

	state := &agent.WorkflowState{
		OriginalRunID:     parent.WorkflowState.OriginalRunID,
		ContinuationIndex: parent.WorkflowState.ContinuationIndex + 1,
		MaxContinuations:  parent.WorkflowState.MaxContinuations,
		TotalRoundsUsed:   parent.WorkflowState.TotalRoundsUsed,
		MaxTotalRounds:    parent.WorkflowState.MaxTotalRounds,
		PriorRunSummaries: priorSummaries,
		CompletedTaskIDs:  append([]string(nil), parent.WorkflowState.CompletedTaskIDs...),
		YieldReason:       "",
		Budget:            budget,
	}
	state.ClearTerminal()
	state.EnsureBudget(now)
	return state
}

func continuationBoolPtr(v bool) *bool {
	return &v
}

func workflowRemainingContinuations(ws *agent.WorkflowState, policy agent.WorkflowBudgetPolicy) int {
	if ws == nil || policy.HardContinuations <= 0 {
		return 1
	}
	return policy.HardContinuations - ws.ContinuationIndex
}

func workflowRemainingRounds(ws *agent.WorkflowState, policy agent.WorkflowBudgetPolicy) int {
	if ws == nil || policy.HardTotalRounds <= 0 {
		return 1
	}
	return policy.HardTotalRounds - ws.TotalRoundsUsed
}

func predictWorkflowContinuationTokens(policy agent.WorkflowBudgetPolicy, usageState agent.WorkflowBudgetUsage, totalRoundsUsed int) int {
	rounds := max(totalRoundsUsed, 1)
	multiplier := policy.NextRunMultiplier
	if multiplier <= 0 {
		multiplier = 1
	}
	predicted := int(math.Ceil(float64(usageState.ModelTotalTokens) / float64(rounds) * multiplier))
	return max(predicted, policy.MinContinuationTokens)
}

func predictWorkflowContinuationCost(policy agent.WorkflowBudgetPolicy, usageState agent.WorkflowBudgetUsage, totalRoundsUsed int) float64 {
	rounds := max(totalRoundsUsed, 1)
	multiplier := policy.NextRunMultiplier
	if multiplier <= 0 {
		multiplier = 1
	}
	predicted := usageState.EstimatedCost / float64(rounds) * multiplier
	return math.Max(predicted, policy.MinContinuationCost)
}

func normalizedUsageRecordType(rec usage.Record) usage.RecordType {
	if rec.RecordType == "" {
		return usage.RecordTypeModelCall
	}
	return rec.RecordType
}

func cloneWorkflowBudgetState(in *agent.WorkflowBudgetState) *agent.WorkflowBudgetState {
	if in == nil {
		return nil
	}
	out := *in
	return &out
}

func (s *Service) markWorkflowContinuationDenied(ctx context.Context, run *agent.Run, now time.Time, reason string) {
	if s == nil || run == nil || run.WorkflowState == nil {
		return
	}
	budget := run.WorkflowState.EnsureBudget(now)
	if budget == nil {
		return
	}
	budget.Mode = agent.WorkflowBudgetModeStopped
	budget.StopReason = strings.TrimSpace(reason)
	budget.Usage.LastUpdatedAt = now
	if reason == agent.YieldReasonCircuitBreakerOpen && !strings.EqualFold(strings.TrimSpace(budget.Circuit.State), "open") {
		budget.Circuit.State = "open"
	}
	run.WorkflowState.YieldReason = strings.TrimSpace(reason)
	run.WorkflowState.MarkTerminal(agent.WorkflowTerminalOutcomeFailed, workflowContinuationStopSummary(reason, budget.Circuit.Reason))
	logging.LogIfErr(ctx, s.runs.Update(ctx, run), "persist workflow continuation denial failed", slog.String("run_id", run.ID))
	if bus, ok := s.events.(eventbus.Bus); ok {
		completedTasks, totalTasks := workflowContinuationTaskCounts(run.Plan)
		logging.LogIfErr(ctx, bus.Publish(ctx, eventbus.NewWorkflowFailedEvent(
			run.ID,
			run.SessionID,
			eventbus.WorkflowEventAttrs{
				OriginalRunID:     run.WorkflowState.OriginalRunID,
				ContinuationIndex: run.WorkflowState.ContinuationIndex,
				TotalRoundsUsed:   run.WorkflowState.TotalRoundsUsed,
				CompletedTasks:    completedTasks,
				TotalTasks:        totalTasks,
				YieldReason:       strings.TrimSpace(reason),
				Summary:           run.WorkflowState.TerminalReason,
			},
			agent.BuildRunEventAttrs(run),
		)), "emit workflow continuation denial event failed", slog.String("run_id", run.ID))
	}
}

func workflowContinuationTaskCounts(plan *planner.Plan) (completed int, total int) {
	if plan == nil {
		return 0, 0
	}
	for _, task := range plan.Tasks {
		total++
		if task.Status == planner.TaskCompleted {
			completed++
		}
	}
	return completed, total
}

func workflowContinuationStopSummary(reason, circuitReason string) string {
	switch strings.TrimSpace(reason) {
	case agent.YieldReasonCircuitBreakerOpen:
		if strings.TrimSpace(circuitReason) != "" {
			return "workflow auto-continuation stopped: " + strings.TrimSpace(circuitReason)
		}
		return "workflow auto-continuation stopped: circuit breaker opened"
	case agent.YieldReasonBudgetHardLimit:
		return "workflow auto-continuation stopped: budget hard limit reached"
	case agent.YieldReasonAdmissionDenied:
		if strings.TrimSpace(circuitReason) != "" {
			return "workflow auto-continuation stopped: " + strings.TrimSpace(circuitReason)
		}
		return "workflow auto-continuation stopped: continuation admission denied"
	default:
		if strings.TrimSpace(circuitReason) != "" {
			return "workflow auto-continuation stopped: " + strings.TrimSpace(circuitReason)
		}
		trimmed := strings.TrimSpace(reason)
		if trimmed == "" {
			return "workflow auto-continuation stopped"
		}
		return "workflow auto-continuation stopped: " + strings.ReplaceAll(trimmed, "_", " ")
	}
}

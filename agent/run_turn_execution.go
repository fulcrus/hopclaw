package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/fulcrus/hopclaw/contextengine"
	"github.com/fulcrus/hopclaw/eventbus"
	"github.com/fulcrus/hopclaw/logging"
	planpkg "github.com/fulcrus/hopclaw/planner"
)

func appendAssistantToolCallsMessage(run *Run, session *Session, response *ModelResponse) {
	if run == nil || session == nil || response == nil {
		return
	}
	now := time.Now().UTC()
	refs := make([]contextengine.ToolCallRef, len(response.ToolCalls))
	for i, tc := range response.ToolCalls {
		args, _ := json.Marshal(tc.Input)
		refs[i] = contextengine.ToolCallRef{
			ID:        tc.ID,
			Name:      tc.Name,
			Arguments: string(args),
		}
	}
	session.Messages = append(session.Messages, contextengine.Message{
		Role:      contextengine.RoleAssistant,
		Content:   response.Message.Content,
		ToolCalls: refs,
		CreatedAt: now,
		Metadata:  messageMetadataForRun(response.Message.Metadata, run.ID),
	})
	session.MessageCount = len(session.Messages)
	session.UpdatedAt = now
}

func appendAssistantTextMessage(run *Run, session *Session, message contextengine.Message) {
	if session == nil {
		return
	}
	now := time.Now().UTC()
	runID := ""
	if run != nil {
		runID = run.ID
	}
	session.Messages = append(session.Messages, contextengine.Message{
		Role:       message.Role,
		Content:    message.Content,
		Name:       message.Name,
		ToolCallID: message.ToolCallID,
		CreatedAt:  now,
		Metadata:   messageMetadataForRun(message.Metadata, runID),
	})
	if session.Messages[len(session.Messages)-1].Role == "" {
		session.Messages[len(session.Messages)-1].Role = contextengine.RoleAssistant
	}
	session.MessageCount = len(session.Messages)
	session.UpdatedAt = now
}

func (a *AgentComponent) emitRunPhaseChanged(ctx context.Context, run *Run, session *Session, phase string, toolNames []string) {
	if a == nil || run == nil || session == nil {
		return
	}
	phase = strings.TrimSpace(phase)
	if phase == "" {
		return
	}
	logging.LogIfErr(ctx, a.emit(ctx, eventbus.NewRunPhaseChangedEvent(
		run.ID,
		session.ID,
		eventbus.RunPhaseChangedAttrs{
			Phase:     phase,
			ToolNames: toolNames,
			ToolCount: len(toolNames),
		},
		buildGovernanceEventAttrs(run),
	)), "emit event failed", slog.String("kind", string(eventbus.EventRunPhaseChanged)))
}

func (a *AgentComponent) commitAssistantToolCalls(ctx context.Context, run *Run, session *Session, response *ModelResponse) (int64, error) {
	appendAssistantToolCallsMessage(run, session, response)
	if err := a.saveSession(ctx, run, session); err != nil {
		return 0, err
	}
	transitionRun(run, RunRunning, PhaseExecutingTools,
		withRunPendingTools(response.ToolCalls),
		withRunError(""),
	)
	if err := a.runs.Update(ctx, run); err != nil {
		return 0, err
	}
	return session.Revision, nil
}

func (a *AgentComponent) commitAssistantText(ctx context.Context, run *Run, session *Session, message contextengine.Message) error {
	appendAssistantTextMessage(run, session, message)
	transitionRun(run, "", PhaseCommitting)
	if err := a.runs.Update(ctx, run); err != nil {
		return err
	}
	return a.saveSession(ctx, run, session)
}

func (a *AgentComponent) commitDetachedAssistantText(_ context.Context, run *Run, session *Session, message contextengine.Message) error {
	appendAssistantTextMessage(run, session, message)
	return nil
}

func (a *AgentComponent) completeTextRun(ctx context.Context, run *Run, session *Session, message contextengine.Message) error {
	if err := a.commitAssistantText(ctx, run, session, message); err != nil {
		return err
	}
	return a.completeRun(ctx, run, session)
}

func (a *AgentComponent) tryCompleteBestEffortWithoutMoreTools(ctx context.Context, run *Run, session *Session, failureReason string) (bool, error) {
	if a == nil || a.model == nil || run == nil || session == nil {
		return false, nil
	}
	if run.TaskContract != nil && run.TaskContract.RequiresExternalEffect {
		return false, nil
	}
	if !runHasBestEffortEvidence(session, run.ID) {
		return false, nil
	}

	run.Plan = nil
	clearExecutionGraph(run)
	run.PendingTools = nil
	seed, err := a.prepareTurnSeedLocked(ctx, run, session)
	if err != nil {
		return false, err
	}
	candidate, err := a.buildPreparedTurnCandidate(ctx, seed, turnPrepareOptions{
		prepareSystemPrompt: func(run *Run, session *Session, basePrompt string) string {
			instruction := "Final response mode: do not call any tools. Use only the evidence already gathered in this run. Provide the best concise, verifiable answer you can. If evidence is incomplete, say exactly what remains uncertain instead of inventing facts."
			if trimmed := strings.TrimSpace(failureReason); trimmed != "" {
				instruction += " The previous execution stopped because: " + trimmed + "."
			}
			return strings.TrimSpace(basePrompt + "\n\n" + instruction)
		},
		selectTools: func(_ *Run, _ []ToolDefinition, _ string) []ToolDefinition {
			return nil
		},
	})
	if err != nil {
		return false, err
	}
	plan, conflicted, err := a.commitPreparedTurnCandidate(ctx, run, session, seed, candidate)
	if err != nil {
		return false, err
	}
	if conflicted {
		return false, nil
	}
	prepared, err := a.executePreparedTurnModelCall(ctx, run, session, plan)
	if err != nil {
		return false, err
	}
	if prepared == nil || prepared.response == nil || len(prepared.response.ToolCalls) > 0 {
		return false, nil
	}
	if strings.TrimSpace(prepared.response.Message.Content) == "" {
		return false, nil
	}
	if err := a.completeTextRun(ctx, run, session, prepared.response.Message); err != nil {
		return false, err
	}
	return true, nil
}

func runHasBestEffortEvidence(session *Session, runID string) bool {
	if session == nil || strings.TrimSpace(runID) == "" {
		return false
	}
	for _, msg := range session.Messages {
		if msg.Role != contextengine.RoleTool {
			continue
		}
		if !messageMatchesRunID(msg.Metadata, runID) {
			continue
		}
		if strings.TrimSpace(msg.TextContent()) != "" {
			return true
		}
	}
	return false
}

func (a *AgentComponent) executeToolBatchEphemeral(ctx context.Context, run *Run, session *Session, calls []ToolCall) (detachedToolBatchResult, error) {
	originalRun := run
	defer func() {
		syncRunValue(originalRun, run)
	}()
	if err := a.ensureRunnable(ctx, &run); err != nil {
		return detachedToolBatchResult{}, err
	}
	result := detachedToolBatchResult{
		calls:      cloneToolCalls(calls),
		approvalID: run.ApprovalID,
	}
	run.ToolRounds++
	a.emitRunPhaseChanged(ctx, run, session, "executing_tools", toolCallNames(calls))
	toolBatchStart := time.Now()
	toolRun := cloneRun(run)
	if toolRun != nil {
		toolRun.Delegation = deriveEffectiveDelegationContract(run, run.Delegation)
	}
	results, err := a.tools.ExecuteBatch(ctx, toolRun, session, calls)
	toolBatchDuration := time.Since(toolBatchStart)
	if err != nil {
		if !shouldRecoverToolExecutionError(err) {
			return detachedToolBatchResult{}, err
		}
		budget := a.toolRecoveryBudget(run)
		attempt := run.ToolRecoveryCount + 1
		if attempt > budget {
			return detachedToolBatchResult{}, fmt.Errorf("tool execution failed after %d recovery attempt(s): %w", run.ToolRecoveryCount, err)
		}
		remaining := budget - attempt
		run.ToolRecoveryCount = attempt
		return detachedToolBatchResult{
			calls:                     cloneToolCalls(calls),
			results:                   buildToolExecutionFailureResults(calls, err, attempt, remaining),
			approvalID:                result.approvalID,
			executionError:            strings.TrimSpace(err.Error()),
			recovered:                 true,
			recoveryAttempt:           attempt,
			recoveryAttemptsRemaining: remaining,
		}, nil
	}
	a.trackToolBatchUsage(ctx, run, session, calls, toolBatchDuration)
	run.ToolRecoveryCount = 0
	result.results = results
	return result, nil
}

func (a *AgentComponent) executeToolBatchDetached(ctx context.Context, run *Run, session *Session, calls []ToolCall, clearApproval bool) (detachedToolBatchResult, error) {
	originalRun := run
	defer func() {
		syncRunValue(originalRun, run)
	}()
	if err := a.ensureRunnable(ctx, &run); err != nil {
		return detachedToolBatchResult{}, err
	}
	result := detachedToolBatchResult{
		calls:      cloneToolCalls(calls),
		approvalID: run.ApprovalID,
	}
	run.ToolRounds++
	a.emitRunPhaseChanged(ctx, run, session, "executing_tools", toolCallNames(calls))
	transitionRun(run, RunRunning, PhaseExecutingTools)
	if err := a.runs.Update(ctx, run); err != nil {
		return detachedToolBatchResult{}, err
	}
	toolBatchStart := time.Now()
	toolRun := cloneRun(run)
	if toolRun != nil {
		toolRun.Delegation = deriveEffectiveDelegationContract(run, run.Delegation)
	}
	results, err := a.tools.ExecuteBatch(ctx, toolRun, session, calls)
	toolBatchDuration := time.Since(toolBatchStart)
	if err != nil {
		if !shouldRecoverToolExecutionError(err) {
			return detachedToolBatchResult{}, err
		}
		budget := a.toolRecoveryBudget(run)
		attempt := run.ToolRecoveryCount + 1
		if attempt > budget {
			return detachedToolBatchResult{}, fmt.Errorf("tool execution failed after %d recovery attempt(s): %w", run.ToolRecoveryCount, err)
		}
		remaining := budget - attempt
		run.ToolRecoveryCount = attempt
		if updateErr := a.runs.Update(ctx, run); updateErr != nil {
			return detachedToolBatchResult{}, updateErr
		}
		return detachedToolBatchResult{
			calls:                     cloneToolCalls(calls),
			results:                   buildToolExecutionFailureResults(calls, err, attempt, remaining),
			approvalID:                result.approvalID,
			executionError:            strings.TrimSpace(err.Error()),
			recovered:                 true,
			recoveryAttempt:           attempt,
			recoveryAttemptsRemaining: remaining,
		}, nil
	}
	a.trackToolBatchUsage(ctx, run, session, calls, toolBatchDuration)
	if run.ToolRecoveryCount != 0 {
		run.ToolRecoveryCount = 0
		if updateErr := a.runs.Update(ctx, run); updateErr != nil {
			return detachedToolBatchResult{}, updateErr
		}
	}
	if clearApproval {
		transitionRun(run, "", "",
			withRunPendingTools(nil),
			withRunApproval(""),
			withRunError(""),
		)
		if updateErr := a.runs.Update(ctx, run); updateErr != nil {
			return detachedToolBatchResult{}, updateErr
		}
	}
	result.results = results
	return result, nil
}

func (a *AgentComponent) resolveToolCommitConflict(ctx context.Context, run *Run, session *Session, expectedRevision int64) (bool, error) {
	originalRun := run
	defer func() {
		syncRunValue(originalRun, run)
	}()
	if err := a.ensureRunnable(ctx, &run); err != nil {
		if errors.Is(err, ErrRunCancelled) {
			return true, nil
		}
		return false, err
	}
	if session.Revision == expectedRevision {
		return false, nil
	}
	transitionRun(run, RunRunning, PhasePreparing,
		withRunPendingTools(nil),
		withRunApproval(""),
		withRunError(""),
		withRunLastSessionRevision(session.Revision),
	)
	return true, a.runs.Update(ctx, run)
}

func (a *AgentComponent) commitToolResults(ctx context.Context, run *Run, session *Session, batch detachedToolBatchResult) error {
	transitionRun(run, "", PhaseCommitting)
	if err := a.runs.Update(ctx, run); err != nil {
		return err
	}
	batch.results = toolResultsForRun(batch.results, run.ID)
	if err := a.context.AppendToolResults(ctx, &session.Session, batch.results); err != nil {
		return err
	}
	session.UpdatedAt = time.Now().UTC()
	if err := a.saveSession(ctx, run, session); err != nil {
		return err
	}
	transitionRun(run, "", "",
		withRunPendingTools(nil),
		withRunApproval(""),
		withRunError(""),
	)
	if err := a.runs.Update(ctx, run); err != nil {
		return err
	}
	a.emitRunPhaseChanged(ctx, run, session, "processing_results", toolCallNames(batch.calls))
	payload := buildToolExecutedPayload(run, batch.calls, batch.results, batch.approvalID, run.ToolRounds)
	if batch.executionError != "" {
		payload.ExecutionError = batch.executionError
		payload.Recovered = batch.recovered
		payload.RecoveryAttempt = batch.recoveryAttempt
		payload.RecoveryAttemptsRemaining = batch.recoveryAttemptsRemaining
	}
	logging.LogIfErr(ctx, a.emit(ctx, eventbus.NewToolExecutedEvent(
		run.ID,
		session.ID,
		payload,
		buildGovernanceEventAttrs(run),
	)), "emit event failed", slog.String("kind", string(eventbus.EventToolExecuted)))
	return nil
}

func (a *AgentComponent) resumePendingTools(ctx context.Context, run *Run, lease *sessionLease) (bool, error) {
	if lease == nil || lease.session == nil || len(run.PendingTools) == 0 {
		return false, nil
	}
	result, err := a.resumePendingToolsStaged(ctx, run, stagedPendingToolsRequest{
		session: lease.session,
		release: func() {
			lease.release()
			lease.session = nil
		},
		reacquire: func(ctx context.Context) (*sessionLease, error) {
			if err := lease.reload(ctx, a.sessions, run.SessionID); err != nil {
				return nil, err
			}
			return lease, nil
		},
		keepLease: true,
	})
	if result.session != nil {
		lease.session = result.session
	}
	return result.resumed, err
}

func (a *AgentComponent) executeLoop(ctx context.Context, run *Run, session *Session, unlock func()) error {
	originalRun := run
	defer func() {
		syncRunValue(originalRun, run)
	}()
	lease := &sessionLease{session: session, unlock: unlock}
	defer lease.close()
	if err := a.ensureRunnable(ctx, &run); err != nil {
		return a.handleRunExecutionError(ctx, &run, err)
	}
	if len(run.PendingTools) > 0 {
		resumed, err := a.resumePendingTools(ctx, run, lease)
		if err != nil {
			return a.handleRunExecutionError(ctx, &run, err)
		}
		if resumed {
			session = lease.session
		}
		if run.Status == RunWaitingApproval {
			return nil
		}
	}

	aggregator := NewTaskResultAggregatorForRun(run)

	toolState := stagedToolLoopState{}
	roundBudget := a.toolRoundBudget(run, nil, lastUserMessageForRun(session, run.ID))
	planRecoveryAttempts := 0

	for round := run.ToolRounds; round < roundBudget; round++ {
		if err := a.ensureRunnable(ctx, &run); err != nil {
			return a.handleRunExecutionError(ctx, &run, err)
		}
		if run.ExecutionMode == ExecutionModeWorkflow && run.WorkflowState != nil && run.WorkflowState.Budget != nil {
			before := *run.WorkflowState.Budget
			decision := evaluateWorkflowBudget(run)
			if run.WorkflowState.Budget != nil && before != *run.WorkflowState.Budget {
				if err := a.runs.Update(ctx, run); err != nil {
					return err
				}
			}
			if decision.ForceCompaction && a.context != nil {
				if err := a.context.Compact(ctx, &session.Session, contextengine.CompactEmergency); err != nil {
					return a.handleRunExecutionError(ctx, &run, err)
				}
				if err := a.saveSession(ctx, run, session); err != nil {
					return err
				}
			}
			if !decision.AllowCurrentTurn {
				stopReason := strings.TrimSpace(decision.StopReason)
				if stopReason == "" {
					stopReason = workflowBudgetStopDetail(decision.YieldReason, "")
				}
				if decision.YieldReason == YieldReasonBudgetHardLimit {
					return a.failRun(ctx, run, errors.New(stopReason))
				}
				if isWorkflowWithIncompletePlan(run) {
					return a.yieldWorkflowRun(ctx, run, session, defaultString(decision.YieldReason, YieldReasonCircuitBreakerOpen))
				}
				return a.failRun(ctx, run, errors.New(stopReason))
			}
		}
		replanned, err := a.applySessionDirectives(ctx, run, session)
		if err != nil {
			return a.handleRunExecutionError(ctx, &run, err)
		}
		skipPlanExpansion := run.ExecutionMode == ExecutionModeWorkflow &&
			run.WorkflowState != nil &&
			run.WorkflowState.Budget != nil &&
			run.WorkflowState.Budget.Mode == WorkflowBudgetModeFinishOnly
		if shouldUsePlanner(run, a.planner != nil) && !skipPlanExpansion {
			runtimeCtx, err := a.runtime.Current(ctx, session, run)
			if err != nil {
				return a.handleRunExecutionError(ctx, &run, err)
			}
			if err := a.ensurePlan(ctx, run, session, runtimeCtx, replanned); err != nil {
				return a.handleRunExecutionError(ctx, &run, err)
			}
		}

		if run.Plan != nil {
			planpkg.NormalizeExecution(run.Plan)

			if planpkg.Terminal(run.Plan) {
				if planpkg.IsDone(run.Plan) {
					return a.completeRun(ctx, run, session)
				}
				recovered, err := a.tryRecoverFailedPlan(ctx, run, session, &planRecoveryAttempts, summarizePlanFailure(run.Plan))
				if err != nil {
					return a.handleRunExecutionError(ctx, &run, err)
				}
				if recovered {
					continue
				}
				return a.failRun(ctx, run, fmt.Errorf("plan terminated with failures"))
			}

			readyTasks := planpkg.ReadyTasks(run.Plan)
			if len(readyTasks) == 0 {
				if running := planpkg.Running(run.Plan); len(running) > 0 {
					readyTasks = running
				}
			} else {
				readyTasks = selectExecutionBatch(run, readyTasks)
			}

			if len(readyTasks) > 0 {
				trackWorkflowExecutionRound(run)
				transitionRun(run, "", PhaseWaitingModel)
				if err := a.runs.Update(ctx, run); err != nil {
					return err
				}

				batchOutcome, err := a.executeReadyBatch(ctx, run, lease, readyTasks, aggregator)
				if err != nil {
					return a.handleRunExecutionError(ctx, &run, err)
				}
				if batchOutcome.retry {
					if lease != nil && lease.session == nil {
						if err := lease.reload(ctx, a.sessions, run.SessionID); err != nil {
							return a.handleRunExecutionError(ctx, &run, err)
						}
					}
					session = lease.session
					toolState.resetLoop()
					continue
				}
				results := batchOutcome.results
				session = lease.session
				if latest, latestErr := a.runs.Get(ctx, run.ID); latestErr == nil && latest.Status == RunCancelled {
					*run = *cloneRun(latest)
					return nil
				}
				terminal, err := a.applyBatchResults(ctx, run, results)
				if err != nil {
					return a.handleRunExecutionError(ctx, &run, err)
				}
				if terminal {
					if planpkg.IsDone(run.Plan) {
						return a.completeRun(ctx, run, session)
					}
					recovered, recoverErr := a.tryRecoverFailedPlan(ctx, run, session, &planRecoveryAttempts, summarizePlanFailure(run.Plan))
					if recoverErr != nil {
						return a.handleRunExecutionError(ctx, &run, recoverErr)
					}
					if recovered {
						continue
					}
					return a.failRun(ctx, run, fmt.Errorf("plan terminated with failures"))
				}
				if batchHasWaitingApproval(results) {
					lease.release()
					lease.session = nil
					return nil
				}
				continue
			}

			recovered, err := a.tryRecoverFailedPlan(ctx, run, session, &planRecoveryAttempts, "the previous plan reached a stuck state with no dispatchable task")
			if err != nil {
				return a.handleRunExecutionError(ctx, &run, err)
			}
			if recovered {
				continue
			}
			return a.failRun(ctx, run, fmt.Errorf("execution plan for run %s has no dispatchable task", run.ID))
		}

		prompt := lastUserMessageForRun(session, run.ID)
		harness := buildRunHarnessSpec(run, nil, prompt, a.transparentRecoveryToolDefinitions(session, run))
		recovered, err := a.maybeAttemptTransparentCapabilityRecoveryIntent(ctx, run, session, harness.Recovery.TransparentIntent)
		if err != nil {
			return a.handleRunExecutionError(ctx, &run, err)
		}
		if recovered {
			if err := a.saveSession(ctx, run, session); err != nil {
				return err
			}
			continue
		}

		trackWorkflowExecutionRound(run)
		outcome, err := a.executeSerialPreparedTurn(ctx, run, serialPreparedTurnRequest{
			lease:     lease,
			toolState: &toolState,
		})
		if err != nil {
			return a.handleRunExecutionError(ctx, &run, err)
		}
		if outcome.waitingApproval {
			return nil
		}
		if outcome.session != nil {
			session = outcome.session
		}
		if outcome.completed {
			return nil
		}
		if outcome.retry {
			continue
		}
	}

	if err := a.context.Compact(ctx, &session.Session, contextengine.CompactEmergency); err != nil {
		return a.handleRunExecutionError(ctx, &run, err)
	}
	if err := a.saveSession(ctx, run, session); err != nil {
		return err
	}
	if isWorkflowWithIncompletePlan(run) {
		return a.yieldWorkflowRun(ctx, run, session, YieldReasonRoundBudget)
	}
	completed, err := a.tryCompleteBestEffortWithoutMoreTools(ctx, run, session, ErrTooManyToolRounds.Error())
	if err != nil {
		return a.handleRunExecutionError(ctx, &run, err)
	}
	if completed {
		return nil
	}
	return a.failRun(ctx, run, ErrTooManyToolRounds)
}

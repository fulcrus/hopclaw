package agent

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/fulcrus/hopclaw/approval"
	"github.com/fulcrus/hopclaw/contextengine"
	"github.com/fulcrus/hopclaw/internal/meta"
	"github.com/fulcrus/hopclaw/skill"
)

type stagedToolLoopState struct {
	lastToolSig      string
	repeatCount      int
	recoveryAttempts int
	progressTracker  toolProgressTracker
}

func (s *stagedToolLoopState) resetLoop() {
	if s == nil {
		return
	}
	s.lastToolSig = ""
	s.repeatCount = 0
}

type stagedExecutionMode int

const (
	stagedExecutionModeSerial stagedExecutionMode = iota
	stagedExecutionModeDetached
)

type serialPreparedTurnRequest struct {
	lease     *sessionLease
	toolState *stagedToolLoopState
	prepare   turnPrepareOptions
	onText    func(context.Context, *Run, *Session, contextengine.Message) error
}

type serialPreparedTurnResult struct {
	session         *Session
	waitingApproval bool
	retry           bool
	completed       bool
}

type stagedPreparedTurnRequest struct {
	mode      stagedExecutionMode
	lease     *sessionLease
	session   *Session
	toolState *stagedToolLoopState
	prepare   turnPrepareOptions
	onText    func(context.Context, *Run, *Session, contextengine.Message) error
}

type stagedPreparedTurnResult struct {
	session         *Session
	waitingApproval bool
	retry           bool
	completed       bool
	requiresSerial  bool
	toolResults     []contextengine.ToolResult
}

type stagedPreparedTurnFinishRequest struct {
	mode      stagedExecutionMode
	session   *Session
	turn      *preparedRunTurn
	toolState *stagedToolLoopState
	release   func()
	reacquire func(context.Context) (*sessionLease, error)
	keepLease bool
	onText    func(context.Context, *Run, *Session, contextengine.Message) error
}

type stagedToolExecutionRequest struct {
	mode          stagedExecutionMode
	session       *Session
	release       func()
	reacquire     func(context.Context) (*sessionLease, error)
	keepLease     bool
	skillSnapshot skill.SessionSkillSnapshot
	available     []ToolDefinition
	response      *ModelResponse
}

type stagedToolExecutionResult struct {
	session         *Session
	waitingApproval bool
	retry           bool
	requiresSerial  bool
	toolResults     []contextengine.ToolResult
}

func (a *AgentComponent) executeStagedPreparedTurn(
	ctx context.Context,
	run *Run,
	req stagedPreparedTurnRequest,
) (stagedPreparedTurnResult, error) {
	if req.toolState == nil {
		return stagedPreparedTurnResult{}, fmt.Errorf("tool loop state is required")
	}

	switch req.mode {
	case stagedExecutionModeSerial:
		if req.lease == nil || req.lease.session == nil {
			return stagedPreparedTurnResult{}, fmt.Errorf("session lease is required")
		}

		turnPlan, conflicted, err := a.prepareModelTurnStaged(ctx, run, req.lease, req.prepare)
		if err != nil {
			return stagedPreparedTurnResult{}, err
		}
		if conflicted {
			if err := req.lease.reload(ctx, a.sessions, run.SessionID); err != nil {
				return stagedPreparedTurnResult{}, err
			}
			return stagedPreparedTurnResult{
				session: req.lease.session,
				retry:   true,
			}, nil
		}
		guidanceInput := strings.TrimSpace(req.prepare.guidanceInput)
		if guidanceInput == "" {
			guidanceInput = lastUserMessageForRun(req.lease.session, run.ID)
		}
		harness := buildRunHarnessSpec(run, nil, guidanceInput, turnPlan.snapshot.Tools)
		recovered, err := a.maybeAttemptTransparentCapabilityRecoveryIntent(ctx, run, req.lease.session, harness.Recovery.TransparentIntent)
		if err != nil {
			return stagedPreparedTurnResult{}, err
		}
		if recovered {
			if err := a.saveSession(ctx, run, req.lease.session); err != nil {
				return stagedPreparedTurnResult{}, err
			}
			return stagedPreparedTurnResult{
				session: req.lease.session,
				retry:   true,
			}, nil
		}

		preparedTurn, err := a.executePreparedTurnModelCall(ctx, run, turnPlan.session, turnPlan)
		if err != nil {
			return stagedPreparedTurnResult{}, err
		}
		if err := req.lease.reload(ctx, a.sessions, run.SessionID); err != nil {
			return stagedPreparedTurnResult{}, err
		}
		session := req.lease.session
		conflicted, err = a.resolvePreparedTurnConflict(ctx, run, session, turnPlan.snapshot.SessionRevision)
		if err != nil {
			return stagedPreparedTurnResult{}, err
		}
		if conflicted {
			return stagedPreparedTurnResult{
				session: session,
				retry:   true,
			}, nil
		}

		return a.finishStagedPreparedTurn(ctx, run, stagedPreparedTurnFinishRequest{
			mode:      stagedExecutionModeSerial,
			session:   session,
			turn:      preparedTurn,
			toolState: req.toolState,
			release: func() {
				req.lease.release()
				req.lease.session = nil
			},
			reacquire: func(ctx context.Context) (*sessionLease, error) {
				if err := req.lease.reload(ctx, a.sessions, run.SessionID); err != nil {
					return nil, err
				}
				return req.lease, nil
			},
			keepLease: true,
			onText:    req.onText,
		})
	case stagedExecutionModeDetached:
		if req.session == nil {
			return stagedPreparedTurnResult{}, fmt.Errorf("session is required")
		}

		seed := &prepareTurnSeed{
			sessionSnapshot: cloneSession(req.session),
			runSnapshot:     cloneRun(run),
			sessionRevision: req.session.Revision,
		}
		candidate, err := a.buildPreparedTurnCandidate(ctx, seed, req.prepare)
		if err != nil {
			return stagedPreparedTurnResult{}, err
		}
		if candidate.needsCompaction {
			compacted, err := a.autoCompactPreparedTurnSnapshot(ctx, req.session)
			if err != nil {
				return stagedPreparedTurnResult{}, err
			}
			if compacted {
				return stagedPreparedTurnResult{
					session: req.session,
					retry:   true,
				}, nil
			}
		}
		if candidate.modelSelection.Model != "" {
			run.Model = candidate.modelSelection.Model
			req.session.Model = candidate.modelSelection.Model
		}
		guidanceInput := strings.TrimSpace(req.prepare.guidanceInput)
		if guidanceInput == "" {
			guidanceInput = lastUserMessageForRun(req.session, run.ID)
		}
		harness := buildRunHarnessSpec(run, nil, guidanceInput, candidate.snapshot.Tools)
		recovered, err := a.maybeAttemptTransparentCapabilityRecoveryIntent(ctx, run, req.session, harness.Recovery.TransparentIntent)
		if err != nil {
			return stagedPreparedTurnResult{}, err
		}
		if recovered {
			return stagedPreparedTurnResult{
				session: req.session,
				retry:   true,
			}, nil
		}

		preparedTurn, err := a.executePreparedTurnModelCall(ctx, run, candidate.sessionSnapshot, &turnModelPlan{
			session:       candidate.sessionSnapshot,
			skillSnapshot: candidate.skillSnapshot,
			snapshot:      candidate.snapshot,
			request:       candidate.request,
			provider:      candidate.provider,
		})
		if err != nil {
			return stagedPreparedTurnResult{}, err
		}

		return a.finishStagedPreparedTurn(ctx, run, stagedPreparedTurnFinishRequest{
			mode:      stagedExecutionModeDetached,
			session:   req.session,
			turn:      preparedTurn,
			toolState: req.toolState,
			onText:    req.onText,
		})
	default:
		return stagedPreparedTurnResult{}, fmt.Errorf("unsupported staged execution mode %d", req.mode)
	}
}

func (a *AgentComponent) finishStagedPreparedTurn(
	ctx context.Context,
	run *Run,
	req stagedPreparedTurnFinishRequest,
) (stagedPreparedTurnResult, error) {
	if req.session == nil {
		return stagedPreparedTurnResult{}, fmt.Errorf("session is required")
	}
	if req.turn == nil {
		return stagedPreparedTurnResult{}, fmt.Errorf("prepared turn is required")
	}
	if req.toolState == nil {
		return stagedPreparedTurnResult{}, fmt.Errorf("tool loop state is required")
	}
	req.session.SkillSnapshot = cloneSkillSnapshot(req.turn.skillSnapshot)

	if req.turn.response.IsToolCalls() {
		outcome, err := a.executeStagedToolTurnCommon(ctx, run, req.toolState, stagedToolExecutionRequest{
			mode:          req.mode,
			session:       req.session,
			release:       req.release,
			reacquire:     req.reacquire,
			keepLease:     req.keepLease,
			skillSnapshot: req.turn.skillSnapshot,
			available:     req.turn.snapshot.Tools,
			response:      req.turn.response,
		})
		if err != nil {
			return stagedPreparedTurnResult{}, err
		}
		return stagedPreparedTurnResult{
			session:         outcome.session,
			waitingApproval: outcome.waitingApproval,
			retry:           outcome.retry,
			requiresSerial:  outcome.requiresSerial,
			toolResults:     outcome.toolResults,
		}, nil
	}

	if req.turn.response.Message.Content != "" {
		onText := req.onText
		if onText == nil {
			switch req.mode {
			case stagedExecutionModeDetached:
				onText = a.commitDetachedAssistantText
			default:
				onText = a.completeTextRun
			}
		}
		if err := onText(ctx, run, req.session, req.turn.response.Message); err != nil {
			return stagedPreparedTurnResult{}, err
		}
		return stagedPreparedTurnResult{
			session:   req.session,
			completed: true,
		}, nil
	}

	return stagedPreparedTurnResult{session: req.session}, nil
}

func (a *AgentComponent) executeSerialPreparedTurn(
	ctx context.Context,
	run *Run,
	req serialPreparedTurnRequest,
) (serialPreparedTurnResult, error) {
	outcome, err := a.executeStagedPreparedTurn(ctx, run, stagedPreparedTurnRequest{
		mode:      stagedExecutionModeSerial,
		lease:     req.lease,
		toolState: req.toolState,
		prepare:   req.prepare,
		onText:    req.onText,
	})
	if err != nil {
		return serialPreparedTurnResult{}, err
	}
	return serialPreparedTurnResult{
		session:         outcome.session,
		waitingApproval: outcome.waitingApproval,
		retry:           outcome.retry,
		completed:       outcome.completed,
	}, nil
}

func (a *AgentComponent) executeStagedToolTurnCommon(
	ctx context.Context,
	run *Run,
	state *stagedToolLoopState,
	req stagedToolExecutionRequest,
) (stagedToolExecutionResult, error) {
	if req.session == nil {
		return stagedToolExecutionResult{}, fmt.Errorf("session is required")
	}
	if req.response == nil {
		return stagedToolExecutionResult{}, fmt.Errorf("model response is required")
	}
	if a.tools == nil {
		return stagedToolExecutionResult{}, ErrToolExecutorNil
	}

	sig := toolCallSignature(req.response.ToolCalls)
	if sig == state.lastToolSig {
		state.repeatCount++
	} else {
		state.repeatCount = 0
		state.lastToolSig = sig
	}
	if state.repeatCount >= maxToolRepeatBeforeBreak {
		recovered, err := a.tryRecoverRepeatedToolLoopForMode(
			ctx,
			run,
			req.mode,
			req.session,
			&state.recoveryAttempts,
			req.response.ToolCalls,
			state.repeatCount+1,
		)
		if err != nil {
			return stagedToolExecutionResult{}, err
		}
		if recovered {
			state.resetLoop()
			return stagedToolExecutionResult{session: req.session, retry: true}, nil
		}
		return stagedToolExecutionResult{}, fmt.Errorf(
			"tool call loop detected: %s called %d times consecutively",
			req.response.ToolCalls[0].Name,
			state.repeatCount+1,
		)
	}

	var (
		session         = req.session
		progressResults []contextengine.ToolResult
	)

	switch req.mode {
	case stagedExecutionModeSerial:
		expectedRevision, err := a.commitAssistantToolCalls(ctx, run, req.session, req.response)
		if err != nil {
			return stagedToolExecutionResult{}, err
		}
		toolSession := cloneSession(req.session)
		if req.release != nil {
			req.release()
		}

		waiting, err := a.evaluateToolCalls(ctx, run, toolSession, req.skillSnapshot, req.response.ToolCalls, nil)
		if err != nil {
			return stagedToolExecutionResult{}, err
		}
		if waiting {
			return stagedToolExecutionResult{waitingApproval: true}, nil
		}

		unavailable := unavailableToolCalls(req.response.ToolCalls, req.available)
		if len(unavailable) > 0 {
			session := req.session
			if req.reacquire != nil {
				lease, leaseErr := req.reacquire(ctx)
				if leaseErr != nil {
					return stagedToolExecutionResult{}, leaseErr
				}
				if lease == nil || lease.session == nil {
					return stagedToolExecutionResult{}, fmt.Errorf("session lease is required")
				}
				if !req.keepLease {
					defer lease.close()
				}
				session = lease.session
			}
			conflicted, err := a.resolveToolCommitConflict(ctx, run, session, expectedRevision)
			if err != nil {
				return stagedToolExecutionResult{}, err
			}
			if conflicted {
				state.resetLoop()
				return stagedToolExecutionResult{session: session, retry: true}, nil
			}
			batch := detachedToolBatchResult{
				calls:   cloneToolCalls(unavailable),
				results: toolResultsForRun(buildUnavailableToolCallResults(unavailable, req.available), run.ID),
			}
			if err := a.commitToolResults(ctx, run, session, batch); err != nil {
				return stagedToolExecutionResult{}, err
			}
			return stagedToolExecutionResult{
				session:     session,
				toolResults: append([]contextengine.ToolResult(nil), batch.results...),
			}, nil
		}

		batch, err := a.executeToolBatchDetached(ctx, run, toolSession, req.response.ToolCalls, false)
		if err != nil {
			return stagedToolExecutionResult{}, err
		}

		if req.reacquire != nil {
			lease, leaseErr := req.reacquire(ctx)
			if leaseErr != nil {
				return stagedToolExecutionResult{}, leaseErr
			}
			if lease == nil || lease.session == nil {
				return stagedToolExecutionResult{}, fmt.Errorf("session lease is required")
			}
			if !req.keepLease {
				defer lease.close()
			}
			session = lease.session
		}

		conflicted, err := a.resolveToolCommitConflict(ctx, run, session, expectedRevision)
		if err != nil {
			return stagedToolExecutionResult{}, err
		}
		if conflicted {
			state.resetLoop()
			return stagedToolExecutionResult{session: session, retry: true}, nil
		}
		if err := a.commitToolResults(ctx, run, session, batch); err != nil {
			return stagedToolExecutionResult{}, err
		}
		progressResults = batch.results
	case stagedExecutionModeDetached:
		appendAssistantToolCallsMessage(run, req.session, req.response)
		unavailable := unavailableToolCalls(req.response.ToolCalls, req.available)
		if len(unavailable) > 0 {
			results := toolResultsForRun(buildUnavailableToolCallResults(unavailable, req.available), run.ID)
			if err := a.context.AppendToolResults(ctx, &req.session.Session, results); err != nil {
				return stagedToolExecutionResult{}, err
			}
			req.session.UpdatedAt = time.Now().UTC()
			return stagedToolExecutionResult{
				session:     req.session,
				toolResults: append([]contextengine.ToolResult(nil), results...),
			}, nil
		}

		if err := a.denyCheckToolCalls(ctx, run, req.session, req.skillSnapshot, req.response.ToolCalls); err != nil {
			if errors.Is(err, ErrParallelApprovalRequired) {
				return stagedToolExecutionResult{
					session:        req.session,
					requiresSerial: true,
				}, nil
			}
			return stagedToolExecutionResult{}, err
		}

		batch, err := a.executeToolBatchEphemeral(ctx, run, req.session, req.response.ToolCalls)
		if err != nil {
			return stagedToolExecutionResult{}, err
		}
		progressResults = append([]contextengine.ToolResult(nil), batch.results...)
		batch.results = toolResultsForRun(batch.results, run.ID)
		if err := a.context.AppendToolResults(ctx, &req.session.Session, batch.results); err != nil {
			return stagedToolExecutionResult{}, err
		}
		req.session.UpdatedAt = time.Now().UTC()
		session = req.session
	default:
		return stagedToolExecutionResult{}, fmt.Errorf("unsupported staged execution mode %d", req.mode)
	}

	if state.repeatCount == 0 &&
		state.progressTracker.Observe(req.response.ToolCalls, progressResults) >= maxToolNoProgressBeforeBreak {
		recovered, err := a.tryRecoverStalledToolProgressForMode(
			ctx,
			run,
			req.mode,
			session,
			&state.recoveryAttempts,
			req.response.ToolCalls,
			progressResults,
		)
		if err != nil {
			return stagedToolExecutionResult{}, err
		}
		if recovered {
			state.resetLoop()
			return stagedToolExecutionResult{session: session, retry: true}, nil
		}
		return stagedToolExecutionResult{}, fmt.Errorf(
			"tool progress stalled after repeated unchanged results from %s",
			req.response.ToolCalls[0].Name,
		)
	}

	return stagedToolExecutionResult{
		session:     session,
		toolResults: append([]contextengine.ToolResult(nil), progressResults...),
	}, nil
}

func (a *AgentComponent) tryRecoverRepeatedToolLoopForMode(
	ctx context.Context,
	run *Run,
	mode stagedExecutionMode,
	session *Session,
	attempts *int,
	calls []ToolCall,
	repeatCount int,
) (bool, error) {
	switch mode {
	case stagedExecutionModeDetached:
		return a.tryRecoverRepeatedToolLoopDetached(ctx, run, session, attempts, calls, repeatCount)
	default:
		return a.tryRecoverRepeatedToolLoop(ctx, run, session, attempts, calls, repeatCount)
	}
}

func (a *AgentComponent) tryRecoverStalledToolProgressForMode(
	ctx context.Context,
	run *Run,
	mode stagedExecutionMode,
	session *Session,
	attempts *int,
	calls []ToolCall,
	results []contextengine.ToolResult,
) (bool, error) {
	switch mode {
	case stagedExecutionModeDetached:
		return a.tryRecoverStalledToolProgressDetached(ctx, run, session, attempts, calls, results)
	default:
		return a.tryRecoverStalledToolProgress(ctx, run, session, attempts, calls, results)
	}
}

type stagedPendingToolsRequest struct {
	session   *Session
	release   func()
	reacquire func(context.Context) (*sessionLease, error)
	keepLease bool
}

type stagedPendingToolsResult struct {
	session *Session
	resumed bool
}

func (a *AgentComponent) resumePendingToolsStaged(
	ctx context.Context,
	run *Run,
	req stagedPendingToolsRequest,
) (stagedPendingToolsResult, error) {
	if req.session == nil || len(run.PendingTools) == 0 {
		return stagedPendingToolsResult{}, nil
	}
	if req.session.Revision != run.LastSessionRevision {
		transitionRun(run, RunRunning, PhasePreparing,
			withRunPendingTools(nil),
			withRunApproval(""),
			withRunLastSessionRevision(req.session.Revision),
		)
		return stagedPendingToolsResult{
			session: req.session,
			resumed: true,
		}, a.runs.Update(ctx, run)
	}
	var approvedTicket *approval.Ticket
	if a.approvals != nil {
		var lookupErr error
		approvedTicket, lookupErr = a.approvals.GetByRun(ctx, run.ID)
		if lookupErr != nil {
			return stagedPendingToolsResult{}, lookupErr
		}
	}
	waiting, err := a.evaluateToolCalls(ctx, run, req.session, req.session.SkillSnapshot, run.PendingTools, approvedTicket)
	if err != nil {
		return stagedPendingToolsResult{}, err
	}
	if waiting {
		return stagedPendingToolsResult{
			session: req.session,
			resumed: true,
		}, nil
	}

	toolSession := cloneSession(req.session)
	expectedRevision := req.session.Revision
	if req.release != nil {
		req.release()
	}

	batch, err := a.executeToolBatchDetached(ctx, run, toolSession, run.PendingTools, true)
	if err != nil {
		return stagedPendingToolsResult{}, err
	}

	session := req.session
	if req.reacquire != nil {
		lease, leaseErr := req.reacquire(ctx)
		if leaseErr != nil {
			return stagedPendingToolsResult{}, leaseErr
		}
		if lease == nil || lease.session == nil {
			return stagedPendingToolsResult{}, fmt.Errorf("session lease is required")
		}
		if !req.keepLease {
			defer lease.close()
		}
		session = lease.session
	}
	conflicted, err := a.resolveToolCommitConflict(ctx, run, session, expectedRevision)
	if err != nil {
		return stagedPendingToolsResult{}, err
	}
	if conflicted {
		return stagedPendingToolsResult{
			session: session,
			resumed: true,
		}, nil
	}
	return stagedPendingToolsResult{
		session: session,
		resumed: true,
	}, a.commitToolResults(ctx, run, session, batch)
}

func (a *AgentComponent) tryRecoverRepeatedToolLoopDetached(ctx context.Context, run *Run, session *Session, attempts *int, calls []ToolCall, repeatCount int) (bool, error) {
	return a.tryRecoverToolLoopDetached(ctx, run, session, attempts, buildToolLoopRecoveryDirective(calls, repeatCount))
}

func (a *AgentComponent) tryRecoverStalledToolProgressDetached(ctx context.Context, run *Run, session *Session, attempts *int, calls []ToolCall, results []contextengine.ToolResult) (bool, error) {
	return a.tryRecoverToolLoopDetached(ctx, run, session, attempts, buildToolNoProgressDirective(calls, results))
}

func (a *AgentComponent) tryRecoverToolLoopDetached(ctx context.Context, run *Run, session *Session, attempts *int, content string) (bool, error) {
	if a == nil || run == nil || session == nil || attempts == nil {
		return false, nil
	}
	if *attempts >= maxAutomaticToolLoopRecoveryAttempts {
		return false, nil
	}
	content = strings.TrimSpace(content)
	if content == "" {
		return false, nil
	}
	if err := a.context.Compact(ctx, &session.Session, contextengine.CompactEmergency); err != nil {
		return false, err
	}
	now := time.Now().UTC()
	session.Messages = append(session.Messages, contextengine.Message{
		Role:      contextengine.RoleUser,
		Content:   content,
		CreatedAt: now,
		Metadata: map[string]any{
			meta.KeyRunID:       run.ID,
			meta.KeyIngressKind: string(SessionDirectiveSteer),
			"synthetic_msg":     true,
			"auto_recovery":     true,
		},
	})
	session.UpdatedAt = now
	*attempts++
	return true, nil
}

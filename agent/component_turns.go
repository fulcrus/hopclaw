package agent

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"reflect"
	"strings"
	"time"

	"github.com/fulcrus/hopclaw/contextengine"
	"github.com/fulcrus/hopclaw/eventbus"
	"github.com/fulcrus/hopclaw/logging"
	"github.com/fulcrus/hopclaw/modelrouter"
	"github.com/fulcrus/hopclaw/skill"
)

type plannedModelSelection struct {
	RequestedModel string
	Model          string
	Provider       string
	FailoverFrom   string
	Reason         string
}

type prepareTurnSeed struct {
	sessionSnapshot *Session
	runSnapshot     *Run
	sessionRevision int64
}

type turnPrepareOptions struct {
	prepareSystemPrompt func(run *Run, session *Session, basePrompt string) string
	guidanceInput       string
	selectTools         func(run *Run, tools []ToolDefinition, guidanceInput string) []ToolDefinition
}

type preparedTurnCandidate struct {
	sessionSnapshot *Session
	skillSnapshot   skill.SessionSkillSnapshot
	snapshot        TurnSnapshot
	request         ChatRequest
	provider        string
	modelSelection  plannedModelSelection
	needsCompaction bool
}

type preparedRunTurn struct {
	skillSnapshot skill.SessionSkillSnapshot
	snapshot      TurnSnapshot
	response      *ModelResponse
}

func syncPreparedTurnSessionCache(session *Session, candidate *preparedTurnCandidate) {
	if session == nil || candidate == nil {
		return
	}
	session.SkillSnapshot = cloneSkillSnapshot(candidate.sessionSnapshot.SkillSnapshot)
}

type turnModelPlan struct {
	session       *Session
	skillSnapshot skill.SessionSkillSnapshot
	snapshot      TurnSnapshot
	request       ChatRequest
	provider      string
}

type detachedToolBatchResult struct {
	calls                     []ToolCall
	results                   []contextengine.ToolResult
	approvalID                string
	executionError            string
	recovered                 bool
	recoveryAttempt           int
	recoveryAttemptsRemaining int
}

type sessionLease struct {
	session *Session
	unlock  func()
}

func (l *sessionLease) close() {
	if l == nil || l.unlock == nil {
		return
	}
	l.unlock()
	l.unlock = nil
}

func (l *sessionLease) release() {
	l.close()
}

func (l *sessionLease) reload(ctx context.Context, store SessionStore, sessionID string) error {
	if l == nil {
		return fmt.Errorf("session lease is required")
	}
	l.close()
	session, unlock, err := store.LoadForExecution(ctx, sessionID)
	if err != nil {
		return err
	}
	l.session = session
	l.unlock = unlock
	return nil
}

func (a *AgentComponent) planModelSelection(ctx context.Context, run *Run, session *Session, prepared *contextengine.PreparedContext, budget contextengine.Budget) (plannedModelSelection, error) {
	requested := defaultString(run.Model, session.Model)
	if a.router == nil {
		return plannedModelSelection{
			RequestedModel: requested,
			Model:          requested,
			Provider:       inferProvider(requested),
		}, nil
	}

	req := modelrouter.RouteRequest{
		RequestedModel:   requested,
		Required:         routeCapabilitiesForTurn(run, lastUserMessageForRun(session, run.ID), buildAllowedToolDefinitions(prepared.Skills, session, a.tools, run)),
		MinContextWindow: budget.ContextWindow,
		MinOutputTokens:  budget.ReservedOutput,
	}
	decision, err := a.router.Select(ctx, req)
	if err != nil {
		if requested != "" {
			return plannedModelSelection{
				RequestedModel: requested,
				Model:          requested,
				Provider:       inferProvider(requested),
				Reason:         "router selection failed; falling back to requested model",
			}, nil
		}
		return plannedModelSelection{}, err
	}
	provider := decision.Model.Provider
	if provider == "" {
		provider = inferProvider(decision.Model.ID)
	}
	return plannedModelSelection{
		RequestedModel: requested,
		Model:          decision.Model.ID,
		Provider:       provider,
		FailoverFrom:   decision.FailoverFrom,
		Reason:         decision.Reason,
	}, nil
}

func (a *AgentComponent) commitModelSelection(ctx context.Context, run *Run, session *Session, selection plannedModelSelection) error {
	run.Model = selection.Model
	session.Model = selection.Model
	logging.LogIfErr(ctx, a.emit(ctx, eventbus.NewModelRoutedEvent(
		run.ID,
		session.ID,
		eventbus.ModelRoutedAttrs{
			RequestedModel: selection.RequestedModel,
			SelectedModel:  selection.Model,
			FailoverFrom:   selection.FailoverFrom,
			Reason:         selection.Reason,
		},
		nil,
	)), "emit event failed", slog.String("kind", string(eventbus.EventModelRouted)))
	if selection.FailoverFrom == "" {
		return nil
	}
	logging.LogIfErr(ctx, a.emit(ctx, eventbus.NewModelFailoverEvent(
		run.ID,
		session.ID,
		eventbus.ModelFailoverAttrs{
			FromModel: selection.FailoverFrom,
			ToModel:   selection.Model,
			Reason:    selection.Reason,
		},
		nil,
	)), "emit event failed", slog.String("kind", string(eventbus.EventModelFailover)))
	return nil
}

func inferProvider(model string) string {
	lower := strings.ToLower(model)
	switch {
	case strings.HasPrefix(lower, "claude-"):
		return "anthropic"
	case strings.HasPrefix(lower, "gpt-") || strings.HasPrefix(lower, "o1-") || strings.HasPrefix(lower, "o3-") || strings.HasPrefix(lower, "o4-"):
		return "openai"
	case strings.HasPrefix(lower, "gemini-"):
		return "google"
	case strings.HasPrefix(lower, "mistral-") || strings.HasPrefix(lower, "mixtral-"):
		return "mistral"
	case strings.HasPrefix(lower, "command-"):
		return "cohere"
	default:
		return ""
	}
}

func likelyThinkingCapableModel(model string) bool {
	lower := strings.ToLower(strings.TrimSpace(model))
	switch {
	case lower == "":
		return false
	case strings.Contains(lower, "thinking"):
		return true
	case strings.HasPrefix(lower, "o1"),
		strings.HasPrefix(lower, "o3"),
		strings.HasPrefix(lower, "o4"):
		return true
	default:
		return false
	}
}

func (a *AgentComponent) thinkingModeForPreparedTurn(run *Run, prompt string, tools []ToolDefinition, selection plannedModelSelection) ThinkingMode {
	harness := buildRunHarnessSpec(run, nil, prompt, tools)
	if !harness.Model.RequireThinking {
		return ThinkingDefault
	}
	if a != nil && a.router != nil {
		return ThinkingExtended
	}
	if likelyThinkingCapableModel(selection.Model) {
		return ThinkingExtended
	}
	return ThinkingDefault
}

func (a *AgentComponent) prepareTurnSeedLocked(ctx context.Context, run *Run, session *Session) (*prepareTurnSeed, error) {
	a.observeSessionRevision(run, session)
	transitionRun(run, "", PhasePreparing)
	if err := a.runs.Update(ctx, run); err != nil {
		return nil, err
	}
	if _, err := a.applySessionDirectives(ctx, run, session); err != nil {
		return nil, err
	}
	a.observeSessionRevision(run, session)
	return &prepareTurnSeed{
		sessionSnapshot: cloneSession(session),
		runSnapshot:     cloneRun(run),
		sessionRevision: session.Revision,
	}, nil
}

func (a *AgentComponent) buildPreparedTurnCandidate(ctx context.Context, seed *prepareTurnSeed, options turnPrepareOptions) (*preparedTurnCandidate, error) {
	if seed == nil || seed.sessionSnapshot == nil || seed.runSnapshot == nil {
		return nil, fmt.Errorf("prepare turn seed is required")
	}
	referenceSummary := sessionReferenceSummary(seed.sessionSnapshot)
	sessionSnapshot := cloneSession(seed.sessionSnapshot)
	runSnapshot := cloneRun(seed.runSnapshot)
	filterSessionMessagesForRun(sessionSnapshot, runSnapshot.ID)
	sessionSnapshot.Summary = referenceSummary
	runtimeCtx, err := a.runtime.Current(ctx, sessionSnapshot, runSnapshot)
	if err != nil {
		return nil, err
	}
	a.injectPromptMemoryFacts(ctx, sessionSnapshot, runtimeCtx)
	systemPrompt := a.systemPromptFor(runSnapshot, sessionSnapshot, runtimeCtx)
	if options.prepareSystemPrompt != nil {
		systemPrompt = options.prepareSystemPrompt(runSnapshot, sessionSnapshot, systemPrompt)
	}
	if reusePrompt := sessionBrowserReusePrompt(sessionSnapshot, lastUserMessageForRun(sessionSnapshot, runSnapshot.ID)); reusePrompt != "" {
		systemPrompt = strings.TrimSpace(systemPrompt + "\n\n" + reusePrompt)
	}
	prepared, budget, err := a.context.Prepare(ctx, &sessionSnapshot.Session, toContextRunWithPrompt(runSnapshot, systemPrompt), runtimeCtx)
	if err != nil {
		return nil, err
	}
	selected, err := a.planModelSelection(ctx, runSnapshot, sessionSnapshot, prepared, budget)
	if err != nil {
		return nil, err
	}
	allTools := buildAllowedToolDefinitions(prepared.Skills, sessionSnapshot, a.tools, runSnapshot)
	guidanceInput := strings.TrimSpace(options.guidanceInput)
	if guidanceInput == "" {
		guidanceInput = lastUserMessageForRun(sessionSnapshot, runSnapshot.ID)
	}
	selectedTools := allTools
	if options.selectTools != nil {
		selectedTools = options.selectTools(runSnapshot, allTools, guidanceInput)
	} else {
		selectedTools = a.prepareRunToolsWithSessionContext(runSnapshot, allTools, guidanceInput, sessionSnapshot.Summary)
	}
	finalSystemPrompt := prepared.SystemPrompt
	if guidance := BuildToolGuidancePrompt(toolGuidanceIntentForRun(runSnapshot, guidanceInput), selectedTools); guidance != "" {
		finalSystemPrompt = finalSystemPrompt + "\n\n" + guidance
	}
	if evidencePrompt := buildExecutionEvidencePrompt(runSnapshot, guidanceInput, selectedTools); evidencePrompt != "" {
		finalSystemPrompt = finalSystemPrompt + "\n\n" + evidencePrompt
	}
	if delegationPrompt := buildDelegationContractPrompt(runSnapshot, selectedTools); delegationPrompt != "" {
		finalSystemPrompt = finalSystemPrompt + "\n\n" + delegationPrompt
	}
	if catalog := buildToolCatalogPrompt(selectedTools); catalog != "" {
		finalSystemPrompt = finalSystemPrompt + "\n\n" + catalog
	}
	snapshot := TurnSnapshot{
		SessionID:        sessionSnapshot.ID,
		RunID:            runSnapshot.ID,
		SessionRevision:  seed.sessionRevision,
		Model:            selected.Model,
		SystemPrompt:     finalSystemPrompt,
		Messages:         append([]contextengine.Message(nil), prepared.Messages...),
		Tools:            append([]ToolDefinition(nil), selectedTools...),
		Budget:           budget,
		EffectiveProfile: cloneEffectiveAgentProfile(runSnapshot.EffectiveProfile),
	}
	return &preparedTurnCandidate{
		sessionSnapshot: sessionSnapshot,
		skillSnapshot:   prepared.Skills,
		snapshot:        snapshot,
		request: ChatRequest{
			SessionID:    sessionSnapshot.ID,
			RunID:        runSnapshot.ID,
			Model:        selected.Model,
			SystemPrompt: finalSystemPrompt,
			Messages:     prepared.Messages,
			Tools:        selectedTools,
			Budget:       budget,
			ThinkingMode: a.thinkingModeForPreparedTurn(runSnapshot, guidanceInput, selectedTools, selected),
		},
		provider:        selected.Provider,
		modelSelection:  selected,
		needsCompaction: prepared.NeedsCompaction,
	}, nil
}

func preparedTurnCompactionChanged(before, after *Session) bool {
	if before == nil || after == nil {
		return false
	}
	return !reflect.DeepEqual(before.Session, after.Session)
}

func (a *AgentComponent) autoCompactPreparedTurnSession(ctx context.Context, run *Run, session *Session) (bool, error) {
	if a == nil || a.context == nil || session == nil {
		return false, nil
	}
	before := cloneSession(session)
	if err := a.context.Compact(ctx, &session.Session, contextengine.CompactAutoThreshold); err != nil {
		return false, err
	}
	if !preparedTurnCompactionChanged(before, session) {
		return false, nil
	}
	session.MessageCount = len(session.Messages)
	session.UpdatedAt = time.Now().UTC()
	if err := a.saveSession(ctx, run, session); err != nil {
		return false, err
	}
	return true, nil
}

func (a *AgentComponent) autoCompactPreparedTurnSnapshot(ctx context.Context, session *Session) (bool, error) {
	if a == nil || a.context == nil || session == nil {
		return false, nil
	}
	before := cloneSession(session)
	if err := a.context.Compact(ctx, &session.Session, contextengine.CompactAutoThreshold); err != nil {
		return false, err
	}
	if !preparedTurnCompactionChanged(before, session) {
		return false, nil
	}
	session.MessageCount = len(session.Messages)
	session.UpdatedAt = time.Now().UTC()
	return true, nil
}

func (a *AgentComponent) commitPreparedTurnCandidate(ctx context.Context, run *Run, session *Session, seed *prepareTurnSeed, candidate *preparedTurnCandidate) (*turnModelPlan, bool, error) {
	if seed == nil || candidate == nil {
		return nil, false, fmt.Errorf("prepared turn candidate is required")
	}
	conflicted, err := a.resolvePreparedTurnConflict(ctx, run, session, seed.sessionRevision)
	if err != nil || conflicted {
		return nil, conflicted, err
	}
	syncPreparedTurnSessionCache(session, candidate)
	if err := a.commitModelSelection(ctx, run, session, candidate.modelSelection); err != nil {
		return nil, false, err
	}
	transitionRun(run, "", PhaseWaitingModel)
	if err := a.runs.Update(ctx, run); err != nil {
		return nil, false, err
	}
	return &turnModelPlan{
		session:       candidate.sessionSnapshot,
		skillSnapshot: candidate.skillSnapshot,
		snapshot:      candidate.snapshot,
		request:       candidate.request,
		provider:      candidate.provider,
	}, false, nil
}

func (a *AgentComponent) prepareModelTurnStaged(ctx context.Context, run *Run, lease *sessionLease, options turnPrepareOptions) (*turnModelPlan, bool, error) {
	if lease == nil || lease.session == nil {
		return nil, false, fmt.Errorf("session lease is required")
	}
	sessionID := lease.session.ID
	seed, err := a.prepareTurnSeedLocked(ctx, run, lease.session)
	if err != nil {
		lease.release()
		lease.session = nil
		return nil, false, err
	}
	lease.release()
	lease.session = nil
	candidate, err := a.buildPreparedTurnCandidate(ctx, seed, options)
	if err != nil {
		return nil, false, err
	}
	if err := lease.reload(ctx, a.sessions, sessionID); err != nil {
		return nil, false, err
	}
	if candidate.needsCompaction {
		conflicted, err := a.resolvePreparedTurnConflict(ctx, run, lease.session, seed.sessionRevision)
		if err != nil {
			lease.release()
			lease.session = nil
			return nil, false, err
		}
		if conflicted {
			lease.release()
			lease.session = nil
			return nil, true, nil
		}
		compacted, err := a.autoCompactPreparedTurnSession(ctx, run, lease.session)
		if err != nil {
			lease.release()
			lease.session = nil
			return nil, false, err
		}
		if compacted {
			lease.release()
			lease.session = nil
			return nil, true, nil
		}
	}
	plan, conflicted, err := a.commitPreparedTurnCandidate(ctx, run, lease.session, seed, candidate)
	lease.release()
	lease.session = nil
	return plan, conflicted, err
}

func (a *AgentComponent) executePreparedTurnModelCall(
	ctx context.Context,
	run *Run,
	session *Session,
	plan *turnModelPlan,
) (*preparedRunTurn, error) {
	originalRun := run
	defer func() {
		syncRunValue(originalRun, run)
	}()
	if plan == nil {
		return nil, fmt.Errorf("turn model plan is required")
	}
	chatReq := plan.request
	a.emitRunPhaseChanged(ctx, run, session, "thinking", nil)
	callStart := time.Now()
	response, err := a.retryModelCall(ctx, run, session, chatReq.Model, &chatReq, func() (*ModelResponse, error) {
		return a.chatWithStreaming(ctx, run, session, chatReq)
	})
	callDuration := time.Since(callStart)
	if err != nil {
		return nil, err
	}
	a.emitModelStreamComplete(ctx, run, session, response)
	a.trackModelUsage(ctx, run, session, chatReq.Model, actualProviderForModel(chatReq.Model, plan.provider), response, callDuration)
	response.ToolCalls = normalizeToolCalls(response.ToolCalls)
	if len(response.ToolCalls) == 0 {
		a.emitRunPhaseChanged(ctx, run, session, "processing_results", nil)
	}
	if err := a.ensureRunnable(ctx, &run); err != nil {
		return nil, err
	}
	return &preparedRunTurn{
		skillSnapshot: plan.skillSnapshot,
		snapshot:      plan.snapshot,
		response:      response,
	}, nil
}

func (a *AgentComponent) emitModelStreamComplete(ctx context.Context, run *Run, session *Session, response *ModelResponse) {
	if a == nil || a.bus == nil {
		return
	}
	payload := eventbus.ModelStreamCompleteAttrs{}
	if response != nil && response.Usage != nil {
		payload.PromptTokens = response.Usage.PromptTokens
		payload.CompletionTokens = response.Usage.CompletionTokens
		payload.TotalTokens = response.Usage.TotalTokens
	}
	logging.LogIfErr(ctx, a.emit(ctx, eventbus.NewModelStreamCompleteEvent(
		safeRunID(run),
		safeSessionID(session),
		payload,
		nil,
	)), "emit event failed", slog.String("kind", string(eventbus.EventModelStreamComplete)))
}

func (a *AgentComponent) resolvePreparedTurnConflict(ctx context.Context, run *Run, session *Session, expectedRevision int64) (bool, error) {
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
		withRunError(""),
		withRunLastSessionRevision(session.Revision),
	)
	return true, a.runs.Update(ctx, run)
}

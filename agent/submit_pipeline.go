package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/fulcrus/hopclaw/contextengine"
	"github.com/fulcrus/hopclaw/eventbus"
	"github.com/fulcrus/hopclaw/internal/meta"
	"github.com/fulcrus/hopclaw/logging"
	"github.com/fulcrus/hopclaw/triage"
)

const (
	submitCLIIdleEpisodeTimeout   = 2 * time.Hour
	submitGroupIdleEpisodeTimeout = 4 * time.Hour
)

type submitPipelineState struct {
	originalMessage  IncomingMessage
	effectiveMessage IncomingMessage
	effectiveProfile *EffectiveAgentProfile
	semantic         *SemanticSignal
	runTriageRequest triage.RunRequest
	triageMode       ExecutionMode
	triagePreflight  *RunPreflightReport
	triageTrace      *RunTriageTrace
	session          *Session
	run              *Run
}

func (a *AgentComponent) buildSubmitPipelineState(msg IncomingMessage) *submitPipelineState {
	effectiveProfile := parseEffectiveAgentProfile(msg.Metadata)
	effectiveMsg := msg
	effectiveMsg.Metadata = injectScopeMetadata(effectiveMsg.Metadata, effectiveMsg)
	if effectiveMsg.Model == "" && effectiveProfile != nil && strings.TrimSpace(effectiveProfile.Model) != "" {
		effectiveMsg.Model = strings.TrimSpace(effectiveProfile.Model)
	}
	return &submitPipelineState{
		originalMessage:  msg,
		effectiveMessage: effectiveMsg,
		effectiveProfile: cloneEffectiveAgentProfile(effectiveProfile),
	}
}

func (a *AgentComponent) loadSubmitSession(ctx context.Context, state *submitPipelineState) error {
	if state == nil {
		return fmt.Errorf("submit state is required")
	}
	session, err := a.sessions.GetOrCreate(
		ctx,
		state.effectiveMessage.SessionKey,
		defaultString(state.effectiveMessage.Model, a.config.DefaultModel),
		state.effectiveMessage.SessionID,
	)
	if err != nil {
		return err
	}
	state.session = session
	state.semantic = initializeSemanticSignal(state.effectiveMessage.Content, session, state.effectiveMessage.SemanticSignal)
	return nil
}

func (a *AgentComponent) prepareSubmitEpisode(ctx context.Context, state *submitPipelineState) error {
	if a == nil {
		return fmt.Errorf("agent component is required")
	}
	if state == nil {
		return fmt.Errorf("submit state is required")
	}
	if state.session == nil {
		return fmt.Errorf("submit session is required")
	}
	manager, ok := a.sessions.(SessionEpisodeManager)
	if !ok {
		return nil
	}
	reason, rotate := submitEpisodeBoundary(state.session, state.effectiveMessage, state.effectiveProfile)
	if rotate {
		_, err := manager.StartNewEpisode(ctx, state.session.ID, reason)
		return err
	}
	_, err := manager.EnsureActiveEpisode(ctx, state.session.ID, defaultString(reason, "default"))
	return err
}

func (a *AgentComponent) findOrCreateSubmitRun(ctx context.Context, state *submitPipelineState) (*Run, error) {
	if state == nil {
		return nil, fmt.Errorf("submit state is required")
	}
	if a.runs.Seen(ctx, state.effectiveMessage.ExternalEventID, a.config.DedupeWindow) {
		return a.runs.FindByExternalEvent(ctx, state.effectiveMessage.ExternalEventID)
	}
	run, err := a.runs.Create(ctx, state.session.ID, state.effectiveMessage, a.config)
	if err != nil {
		return nil, err
	}
	state.run = run
	return nil, nil
}

func (a *AgentComponent) analyzeSubmitRun(ctx context.Context, state *submitPipelineState) {
	if a == nil || state == nil || state.run == nil || state.session == nil {
		return
	}

	run := state.run
	effectiveMsg := state.effectiveMessage
	session := state.session
	plannerEnabled := a.planner != nil

	state.runTriageRequest = submitRunTriageRequest(effectiveMsg, session, a.config.DefaultModel, plannerEnabled, state.semantic)
	run.EffectiveProfile = cloneEffectiveAgentProfile(state.effectiveProfile)
	run.ExecutionMode, run.Preflight, run.Triage = a.triageRun(ctx, effectiveMsg, session, state.semantic)
	state.triageMode = run.ExecutionMode
	state.triagePreflight = cloneRunPreflightReport(run.Preflight)
	state.triageTrace = cloneRunTriageTrace(run.Triage)
	run.TaskContract = a.buildTaskContract(ctx, effectiveMsg, session, run.ExecutionMode, run.Preflight, run.Triage, state.semantic)
	run.ExecutionMode = refineExecutionModeWithTaskContract(
		run.ExecutionMode,
		plannerEnabled,
		run.Preflight,
		run.TaskContract,
	)
	if run.ExecutionMode == ExecutionModeWorkflow && run.WorkflowState == nil {
		run.WorkflowState = &WorkflowState{
			OriginalRunID:     run.ID,
			ContinuationIndex: 0,
			MaxContinuations:  DefaultMaxContinuations,
			MaxTotalRounds:    DefaultMaxTotalRounds,
			PriorRunSummaries: nil,
			CompletedTaskIDs:  nil,
			TotalRoundsUsed:   0,
			Yielded:           false,
			YieldReason:       "",
		}
		run.WorkflowState.EnsureBudget(time.Now().UTC())
	}
	run.Preflight = finalizeSubmitPreflight(a, effectiveMsg, session, run)
	run.Delegation = buildDelegationContract(strings.TrimSpace(effectiveMsg.Content), run.ExecutionMode, run.Preflight, run.TaskContract)
	run.Preflight = a.applyClarificationRoundLimit(ctx, run, effectiveMsg)
	if state.semantic != nil {
		state.semantic.ExecutionMode = run.ExecutionMode
	}
	run.SemanticSignal = diagnosticSemanticSignal(state.semantic)
	if run.Preflight != nil && run.Preflight.Blocking && run.ExecutionMode != ExecutionModeWatch {
		transitionRun(run, RunWaitingInput, PhasePreparing)
	}
}

func (a *AgentComponent) persistPreparedSubmitRun(ctx context.Context, state *submitPipelineState) error {
	if state == nil || state.run == nil {
		return fmt.Errorf("submit run is required")
	}
	return a.runs.Update(ctx, state.run)
}

func (a *AgentComponent) recordSubmitSession(ctx context.Context, state *submitPipelineState) error {
	if state == nil || state.run == nil || state.session == nil {
		return fmt.Errorf("submit state is incomplete")
	}

	messageForSession := state.effectiveMessage
	messageForSession.Metadata = messageMetadataForRun(state.effectiveMessage.Metadata, state.run.ID)
	if err := a.sessions.AppendUserMessage(ctx, state.session.ID, messageForSession); err != nil {
		return err
	}

	a.emitMessageReceivedHook(ctx, state.session, state.effectiveMessage)
	state.session.Revision++
	req := state.runTriageRequest
	if strings.TrimSpace(req.Model) == "" && strings.TrimSpace(req.Message) == "" && req.SemanticSignal == nil {
		req = submitRunTriageRequest(state.effectiveMessage, state.session, a.config.DefaultModel, a.planner != nil, state.semantic)
	}
	mode := state.triageMode
	if mode == "" {
		mode = state.run.ExecutionMode
	}
	report := state.triagePreflight
	if report == nil {
		report = state.run.Preflight
	}
	trace := state.triageTrace
	if trace == nil {
		trace = state.run.Triage
	}
	if rememberRunTriage(ctx, a.sessions, state.session.ID, req, mode, report, trace) {
		state.session.Revision++
	}
	state.run.LastSessionRevision = state.session.Revision
	return a.runs.Update(ctx, state.run)
}

func (a *AgentComponent) dispatchSubmittedRun(ctx context.Context, state *submitPipelineState) error {
	if state == nil || state.run == nil || state.session == nil {
		return fmt.Errorf("submit state is incomplete")
	}
	if state.run.Status == RunQueued && state.run.QueueMode == QueueInterrupt {
		if err := a.interruptSessionRuns(ctx, state.session.ID, state.run.ID); err != nil {
			return err
		}
	}
	if a.queue != nil && state.run.Status == RunQueued {
		if err := a.queue.EnqueueSessionRun(ctx, state.session.ID, state.run.ID, state.run.QueueMode); err != nil {
			return err
		}
	}
	a.emitSubmittedRunEvents(ctx, state)
	return nil
}

func (a *AgentComponent) emitSubmittedRunEvents(ctx context.Context, state *submitPipelineState) {
	if state == nil || state.run == nil || state.session == nil {
		return
	}
	run := state.run
	session := state.session
	logging.LogIfErr(ctx, a.emit(ctx, eventbus.NewRunSubmittedEvent(
		run.ID,
		session.ID,
		eventbus.RunSubmittedAttrs{
			QueueMode:     string(run.QueueMode),
			Model:         run.Model,
			ExecutionMode: string(run.ExecutionMode),
			Preflight:     preflightEventMap(run.Preflight),
			AgentProfile:  effectiveAgentProfileAttrs(run.EffectiveProfile),
			TaskContract:  taskContractEventMap(run.TaskContract),
		},
		nil,
	)), "emit event failed")
	if run.Preflight != nil {
		logging.LogIfErr(ctx, a.emit(ctx, eventbus.NewRunPreflightUpdatedEvent(
			run.ID,
			session.ID,
			preflightEventPayload(run.Preflight),
			nil,
		)), "emit event failed")
	}
	if run.Status == RunWaitingInput {
		logging.LogIfErr(ctx, a.emit(ctx, eventbus.NewRunWaitingInputEvent(
			run.ID,
			session.ID,
			preflightEventPayload(run.Preflight),
			nil,
		)), "emit event failed")
	}
}

func finalizeSubmitPreflight(a *AgentComponent, msg IncomingMessage, session *Session, run *Run) *RunPreflightReport {
	if run == nil || run.Preflight == nil {
		return run.Preflight
	}

	preflightDomains := append([]string(nil), run.Preflight.SuggestedDomains...)
	if run.TaskContract != nil {
		preflightDomains = normalizeSemanticDomains(append(preflightDomains, run.TaskContract.SuggestedDomains...))
	}
	preflightDomains = sanitizeSuggestedDomainsForMessage(strings.TrimSpace(msg.Content), preflightDomains)
	detectedDomains := sanitizeSuggestedDomainsForMessage(strings.TrimSpace(msg.Content), append([]string(nil), run.Preflight.DetectedDomains...))
	preflightReq := PreflightAnalysisRequest{
		Message:        strings.TrimSpace(msg.Content),
		SessionSummary: sessionReferenceSummary(session),
		ExecutionMode:  run.ExecutionMode,
	}
	needsReference := preflightHasCheck(run.Preflight, "reference_gap")
	if !needsReference && (a == nil || a.preflight == nil) {
		needsReference = inferredNeedsReferenceWithContract(preflightReq, preflightDomains, run.TaskContract)
	}
	return buildRunPreflightWithContract(strings.TrimSpace(msg.Content), PreflightAnalysis{
		NeedsReference:    needsReference,
		NeedsConfirmation: preflightHasCheck(run.Preflight, "expected_confirmation"),
		SuggestedDomains:  preflightDomains,
		DetectedDomains:   detectedDomains,
	}, run.TaskContract)
}

func submitRunTriageRequest(msg IncomingMessage, session *Session, defaultModel string, plannerAvailable bool, signal *SemanticSignal) triage.RunRequest {
	// Always use the mainline semantic path — no keyword fallback.
	mainSemanticPath := true
	languageHint := ""
	var semanticSignal *triage.RunSemanticSignal
	if signal != nil {
		languageHint = strings.TrimSpace(signal.Language.Family)
		semanticSignal = &triage.RunSemanticSignal{
			LanguageHint:        languageHint,
			MainSemanticPath:    mainSemanticPath,
			RequiresCurrentInfo: signal.RequiresCurrentInfo,
		}
	}
	return triage.RunRequest{
		Model:            defaultString(msg.Model, defaultModel),
		Message:          strings.TrimSpace(msg.Content),
		SessionSummary:   strings.TrimSpace(session.Summary),
		PlannerAvailable: plannerAvailable,
		LanguageHint:     languageHint,
		MainSemanticPath: mainSemanticPath,
		SemanticSignal:   semanticSignal,
	}
}

func (a *AgentComponent) applyClarificationRoundLimit(ctx context.Context, run *Run, msg IncomingMessage) *RunPreflightReport {
	if a == nil || run == nil || run.Preflight == nil {
		return run.Preflight
	}
	if !preflightNeedsClarification(run.Preflight) {
		return run.Preflight
	}
	rounds := a.clarificationRoundCount(ctx, run, msg)
	if rounds < a.config.MaxClarificationRounds {
		return run.Preflight
	}
	return relaxClarificationLimitPreflight(run.Preflight, rounds, a.config.MaxClarificationRounds)
}

func (a *AgentComponent) clarificationRoundCount(ctx context.Context, run *Run, msg IncomingMessage) int {
	if a == nil || run == nil {
		return 0
	}
	sourceRunID := clarificationSourceRunID(msg.Metadata)
	if sourceRunID == "" {
		return 0
	}
	currentID := strings.TrimSpace(run.ParentRunID)
	if currentID == "" {
		currentID = sourceRunID
	}
	rounds := 0
	seen := map[string]struct{}{}
	for currentID != "" {
		if _, ok := seen[currentID]; ok {
			break
		}
		seen[currentID] = struct{}{}
		rounds++
		parent, err := a.runs.Get(ctx, currentID)
		if err != nil || parent == nil || !runWasClarificationSuperseded(parent) {
			break
		}
		currentID = strings.TrimSpace(parent.ParentRunID)
	}
	return rounds
}

func clarificationSourceRunID(metadata map[string]any) string {
	if len(metadata) == 0 {
		return ""
	}
	value, ok := metadata[MetadataKeyClarificationSourceRunID]
	if !ok {
		return ""
	}
	runID := strings.TrimSpace(fmt.Sprint(value))
	if runID == "" || runID == "<nil>" {
		return ""
	}
	return runID
}

func runWasClarificationSuperseded(run *Run) bool {
	if run == nil || run.Status != RunCancelled {
		return false
	}
	return strings.TrimSpace(run.Error) == RunReasonClarificationSuperseded
}

func preflightNeedsClarification(report *RunPreflightReport) bool {
	if report == nil || !report.Blocking {
		return false
	}
	hasClarification := len(report.ClarificationSlots) > 0
	for _, check := range report.Checks {
		if !check.Blocking {
			continue
		}
		if isClarificationCheckID(check.ID) {
			hasClarification = true
			continue
		}
		return false
	}
	return hasClarification
}

func isClarificationCheckID(id string) bool {
	switch strings.TrimSpace(id) {
	case "reference_gap",
		taskMissingInfoSourceTarget,
		taskMissingInfoDeliveryTarget,
		taskMissingInfoSchedule,
		taskMissingInfoDeploymentScope:
		return true
	default:
		return false
	}
}

func relaxClarificationLimitPreflight(report *RunPreflightReport, rounds, maxRounds int) *RunPreflightReport {
	if report == nil {
		return nil
	}
	note := "I'll proceed with what I have. Some details may be incomplete."
	updated := *report
	updated.Blocking = false
	updated.State = RunPreflightAutoPreparing
	updated.Summary = note
	updated.Prompt = ""
	updated.Question = ""
	updated.ReplyHints = nil
	updated.ReplyTemplate = ""
	updated.ClarificationSlots = nil
	updated.ContinueHint = note
	updated.GeneratedAt = time.Now().UTC()
	checks := make([]RunPreflightCheck, 0, len(report.Checks)+1)
	for _, check := range report.Checks {
		if check.Blocking && isClarificationCheckID(check.ID) {
			check.Blocking = false
			check.State = RunPreflightAutoPreparing
		}
		checks = append(checks, check)
	}
	checks = append(checks, RunPreflightCheck{
		ID:     "clarification_limit_reached",
		Title:  "Clarification Limit Reached",
		State:  RunPreflightAutoPreparing,
		Detail: fmt.Sprintf("Reached the clarification limit (%d/%d). Proceeding with the best available information.", rounds, maxRounds),
	})
	updated.Checks = checks
	return &updated
}

func submitEpisodeBoundary(session *Session, msg IncomingMessage, profile *EffectiveAgentProfile) (string, bool) {
	if session == nil {
		return "", false
	}
	if submitIsClarificationFollowUp(msg) {
		return "default", false
	}
	if reason := explicitEpisodeBoundaryReason(msg.Metadata); reason != "" {
		return reason, true
	}
	if reason := idleEpisodeBoundaryReason(session, msg); reason != "" {
		return reason, true
	}
	if reason := profileEpisodeBoundaryReason(session, profile); reason != "" {
		return reason, true
	}
	return "default", false
}

func submitIsClarificationFollowUp(msg IncomingMessage) bool {
	if len(msg.Metadata) == 0 {
		return false
	}
	if strings.TrimSpace(clarificationSourceRunID(msg.Metadata)) != "" {
		return true
	}
	if value, ok := msg.Metadata["preflight_followup"].(bool); ok && value {
		return true
	}
	return false
}

func explicitEpisodeBoundaryReason(metadata map[string]any) string {
	if len(metadata) == 0 {
		return ""
	}
	value, ok := metadata[MetadataKeyEpisodeBoundaryReason]
	if !ok {
		return ""
	}
	reason := strings.TrimSpace(fmt.Sprint(value))
	if reason == "" || reason == "<nil>" {
		return ""
	}
	return reason
}

func idleEpisodeBoundaryReason(session *Session, msg IncomingMessage) string {
	timeout := submitIdleEpisodeTimeout(session, msg)
	if timeout <= 0 {
		return ""
	}
	last := submitLastConversationMessage(session)
	if last.IsZero() {
		return ""
	}
	if time.Since(last) >= timeout {
		return "idle_timeout"
	}
	return ""
}

func submitIdleEpisodeTimeout(session *Session, msg IncomingMessage) time.Duration {
	descriptor, _ := normalizedChannelCapabilityDescriptor(msg.Metadata)
	chatType := meta.NormalizeChatType(submitMetadataString(msg.Metadata, meta.KeyChatType))
	if !hasAnyChannelCapability(descriptor) || chatType == meta.ChatTypeUnknown {
		lastMeta := submitLastUserMetadata(session)
		if !hasAnyChannelCapability(descriptor) {
			descriptor, _ = normalizedChannelCapabilityDescriptor(lastMeta)
		}
		if chatType == meta.ChatTypeUnknown {
			chatType = meta.NormalizeChatType(submitMetadataString(lastMeta, meta.KeyChatType))
		}
	}
	if submitUsesLocalInteractiveIdleTimeout(descriptor, chatType) {
		return submitCLIIdleEpisodeTimeout
	}
	if chatType == meta.ChatTypeGroup {
		return submitGroupIdleEpisodeTimeout
	}
	return 0
}

func submitUsesLocalInteractiveIdleTimeout(descriptor map[string]any, chatType meta.ChatType) bool {
	if chatType != meta.ChatTypeDirect || !hasAnyChannelCapability(descriptor) {
		return false
	}
	return descriptorBool(descriptor, "interactive") &&
		!descriptorBool(descriptor, "inline_delivery") &&
		!descriptorBool(descriptor, "mobile")
}

func submitLastConversationMessage(session *Session) time.Time {
	if session == nil {
		return time.Time{}
	}
	for idx := len(session.Messages) - 1; idx >= 0; idx-- {
		if createdAt := session.Messages[idx].CreatedAt; !createdAt.IsZero() {
			return createdAt
		}
	}
	return time.Time{}
}

func profileEpisodeBoundaryReason(session *Session, current *EffectiveAgentProfile) string {
	previous := submitLastEffectiveAgentProfile(session)
	if previous == nil && current == nil {
		return ""
	}
	if effectiveAgentProfileFingerprint(previous) != effectiveAgentProfileFingerprint(current) {
		return "agent_profile_switch"
	}
	return ""
}

func submitLastEffectiveAgentProfile(session *Session) *EffectiveAgentProfile {
	if session == nil {
		return nil
	}
	for idx := len(session.Messages) - 1; idx >= 0; idx-- {
		msg := session.Messages[idx]
		if msg.Role != contextengine.RoleUser {
			continue
		}
		if profile := parseEffectiveAgentProfile(msg.Metadata); profile != nil {
			return profile
		}
	}
	return nil
}

func submitLastUserMetadata(session *Session) map[string]any {
	if session == nil {
		return nil
	}
	for idx := len(session.Messages) - 1; idx >= 0; idx-- {
		msg := session.Messages[idx]
		if msg.Role == contextengine.RoleUser {
			return msg.Metadata
		}
	}
	return nil
}

func effectiveAgentProfileFingerprint(profile *EffectiveAgentProfile) string {
	if profile == nil {
		return ""
	}
	cloned := cloneEffectiveAgentProfile(profile)
	slices.Sort(cloned.AllowedTools)
	slices.Sort(cloned.AllowedSkills)
	data, err := json.Marshal(cloned)
	if err != nil {
		return cloned.Name + "|" + cloned.Model + "|" + cloned.Source
	}
	return string(data)
}

func submitMetadataString(metadata map[string]any, keys ...string) string {
	for _, key := range keys {
		if strings.TrimSpace(key) == "" || len(metadata) == 0 {
			continue
		}
		if value, ok := metadata[key]; ok {
			text := strings.TrimSpace(fmt.Sprint(value))
			if text != "" && text != "<nil>" {
				return text
			}
		}
	}
	return ""
}

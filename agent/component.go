package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/fulcrus/hopclaw/approval"
	"github.com/fulcrus/hopclaw/contextengine"
	"github.com/fulcrus/hopclaw/eventbus"
	"github.com/fulcrus/hopclaw/internal/meta"
	"github.com/fulcrus/hopclaw/internal/support/normalize"
	"github.com/fulcrus/hopclaw/logging"
	"github.com/fulcrus/hopclaw/policy"
	"github.com/fulcrus/hopclaw/skill"
	"github.com/fulcrus/hopclaw/usage"
)

var log = logging.WithSubsystem("agent")

var ErrParallelApprovalRequired = errors.New("parallel task requires serial approval path")

const (
	defaultMaxRunDuration         = 10 * time.Minute
	defaultMaxClarificationRounds = 3
)

// AgentComponent orchestrates session state, model calls, and tool execution
// for a single agent runtime.
type AgentComponent struct {
	config          AgentConfig
	sessions        SessionStore
	runs            RunStore
	queue           Coordinator
	context         contextengine.ContextEngine
	memory          MemoryStore
	model           ModelClient
	tools           ToolExecutor
	runtime         RuntimeContextProvider
	router          ModelRouter
	planner         Planner
	modeSelector    ExecutionModeSelector
	runTriage       RunTriageAnalyzer
	preflight       PreflightAnalyzer
	taskContract    TaskContractAnalyzer
	watchFlow       WatchWorkflow
	directives      SessionDirectiveStore
	policy          PolicyEngine
	approvals       ApprovalStore
	grantStore      *approval.GrantStore
	hooks           HookDispatcher
	bus             EventBus
	usageTracker    *usage.Tracker
	cancels         sync.Map // runID -> runCancelEntry
	executing       sync.Map // runID -> runExecutionEntry
	runStateSweepAt atomic.Int64
}

// NewComponent constructs an agent runtime with normalized defaults for queue
// and tool-execution settings.
func NewComponent(cfg AgentConfig, sessions SessionStore, runs RunStore, queue Coordinator, ctxEngine contextengine.ContextEngine, model ModelClient, tools ToolExecutor, runtime RuntimeContextProvider) *AgentComponent {
	if cfg.MaxRunDuration <= 0 {
		cfg.MaxRunDuration = defaultMaxRunDuration
	}
	if cfg.MaxToolRounds <= 0 {
		cfg.MaxToolRounds = 16
	}
	if cfg.MaxToolRecoveryAttempts <= 0 {
		cfg.MaxToolRecoveryAttempts = 3
	}
	if cfg.MaxClarificationRounds <= 0 {
		cfg.MaxClarificationRounds = defaultMaxClarificationRounds
	}
	if cfg.QueueMode == "" {
		cfg.QueueMode = QueueEnqueue
	}
	if runtime == nil {
		runtime = StaticRuntimeContextProvider{}
	}
	return &AgentComponent{
		config:   cfg,
		sessions: sessions,
		runs:     runs,
		queue:    queue,
		context:  ctxEngine,
		model:    model,
		tools:    tools,
		runtime:  runtime,
	}
}

// WithRouter installs the optional model router used before model execution.
func (a *AgentComponent) WithRouter(router ModelRouter) *AgentComponent {
	a.router = router
	return a
}

// WithPlanner installs the planner used when a run enters planned execution.
func (a *AgentComponent) WithPlanner(planner Planner) *AgentComponent {
	a.planner = planner
	return a
}

// WithExecutionModeSelector installs the selector that chooses a run's
// execution mode before dispatch.
func (a *AgentComponent) WithExecutionModeSelector(selector ExecutionModeSelector) *AgentComponent {
	a.modeSelector = selector
	return a
}

// WithRunTriage installs the analyzer that derives execution hints from an
// inbound request.
func (a *AgentComponent) WithRunTriage(analyzer RunTriageAnalyzer) *AgentComponent {
	a.runTriage = analyzer
	return a
}

// WithPreflightAnalyzer installs the analyzer that decides whether a run needs
// clarification or confirmation before execution.
func (a *AgentComponent) WithPreflightAnalyzer(analyzer PreflightAnalyzer) *AgentComponent {
	a.preflight = analyzer
	return a
}

// WithTaskContractAnalyzer installs the analyzer that extracts structured task
// contracts from user requests.
func (a *AgentComponent) WithTaskContractAnalyzer(analyzer TaskContractAnalyzer) *AgentComponent {
	a.taskContract = analyzer
	return a
}

// WithWatchWorkflow installs the workflow used to create long-running watch
// jobs from agent runs.
func (a *AgentComponent) WithWatchWorkflow(workflow WatchWorkflow) *AgentComponent {
	a.watchFlow = workflow
	return a
}

// WithSessionDirectives installs the store used to load directive overlays for
// a session during execution.
func (a *AgentComponent) WithSessionDirectives(store SessionDirectiveStore) *AgentComponent {
	a.directives = store
	return a
}

// WithPolicy installs the policy engine consulted before tools are executed.
func (a *AgentComponent) WithPolicy(engine PolicyEngine) *AgentComponent {
	a.policy = engine
	if a.policy != nil {
		if a.grantStore == nil {
			a.grantStore = approval.NewGrantStore()
		}
		policy.WireGrantStore(a.policy, a.grantStore)
	}
	return a
}

// WithApprovals installs the approval store required for approval-gated runs.
func (a *AgentComponent) WithApprovals(store ApprovalStore) *AgentComponent {
	a.approvals = store
	return a
}

// WithGrantStore installs the approval grant store shared with policy
// evaluation and approval resolution.
func (a *AgentComponent) WithGrantStore(store *approval.GrantStore) *AgentComponent {
	a.grantStore = store
	if a.policy != nil {
		policy.WireGrantStore(a.policy, a.grantStore)
	}
	return a
}

// WithEventBus installs the event bus used for run and approval lifecycle
// notifications.
func (a *AgentComponent) WithEventBus(bus EventBus) *AgentComponent {
	a.bus = bus
	return a
}

// WithUsageTracker installs the optional usage tracker for model and tool
// accounting.
func (a *AgentComponent) WithUsageTracker(tracker *usage.Tracker) *AgentComponent {
	a.usageTracker = tracker
	return a
}

// WithMemoryStore installs the optional memory store used for prompt injection.
func (a *AgentComponent) WithMemoryStore(store MemoryStore) *AgentComponent {
	a.memory = store
	return a
}

// ToolExecutor returns the configured tool executor, or nil when the component
// itself is nil.
func (a *AgentComponent) ToolExecutor() ToolExecutor {
	if a == nil {
		return nil
	}
	return a.tools
}

// Coordinator returns the configured session queue coordinator, or nil when
// queueing is not enabled.
func (a *AgentComponent) Coordinator() Coordinator {
	if a == nil {
		return nil
	}
	return a.queue
}

// UsageStore returns the configured usage store, or nil when usage tracking is disabled.
func (a *AgentComponent) UsageStore() usage.Store {
	if a == nil || a.usageTracker == nil {
		return nil
	}
	return a.usageTracker.Store()
}

// Submit records an incoming message, creates or reuses a run, and dispatches
// it for execution. The returned run may be an existing deduplicated run.
func (a *AgentComponent) Submit(ctx context.Context, msg IncomingMessage) (*Run, error) {
	state := a.buildSubmitPipelineState(msg)
	if err := a.loadSubmitSession(ctx, state); err != nil {
		return nil, err
	}
	existing, err := a.findOrCreateSubmitRun(ctx, state)
	if err != nil || existing != nil {
		return existing, err
	}
	if err := a.prepareSubmitEpisode(ctx, state); err != nil {
		return nil, err
	}
	a.analyzeSubmitRun(ctx, state)
	if err := a.persistPreparedSubmitRun(ctx, state); err != nil {
		return nil, err
	}
	if err := a.recordSubmitSession(ctx, state); err != nil {
		return nil, err
	}
	if err := a.dispatchSubmittedRun(ctx, state); err != nil {
		return nil, err
	}
	return state.run, nil
}

// ExecuteRun advances a run until completion or a blocking state. It returns
// configuration errors for missing dependencies and treats cancelled runs as nil errors.
func (a *AgentComponent) ExecuteRun(ctx context.Context, run *Run) error {
	if a.context == nil {
		return fmt.Errorf("%w", ErrContextEngineNil)
	}
	if a.model == nil {
		return fmt.Errorf("%w", ErrModelClientNil)
	}
	if run == nil {
		return fmt.Errorf("run is required")
	}
	ctx, cancel := context.WithTimeout(ctx, a.config.MaxRunDuration)
	defer cancel()
	run = cloneRun(run)
	release, claimed := a.claimRunExecution(run.ID)
	if !claimed {
		return nil
	}
	defer release()

	if err := a.ensureRunnable(ctx, &run); err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, ErrRunCancelled) {
			return nil
		}
		if errors.Is(err, context.DeadlineExceeded) {
			return a.timeoutRun(ctx, &run, err)
		}
		return err
	}

	err := a.withRunCancellation(ctx, run.ID, func(execCtx context.Context) error {
		finishQueue, claimed, err := a.claimQueuedExecution(execCtx, run)
		if err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, ErrRunCancelled) {
				return nil
			}
			if errors.Is(err, context.DeadlineExceeded) {
				return a.timeoutRun(execCtx, &run, err)
			}
			return err
		}
		if !claimed {
			return nil
		}
		defer finishQueue()

		err = a.dispatchRunExecution(execCtx, run, runDispatchOptions{
			eventType: eventbus.EventRunStarted,
			eventAttrs: eventbus.RunDispatchAttrs{
				Model:         run.Model,
				ExecutionMode: string(run.ExecutionMode),
				AgentProfile:  effectiveAgentProfileAttrs(run.EffectiveProfile),
			}.ToMap(),
			allowWatch:   true,
			setStartedAt: true,
		})
		if errors.Is(err, context.Canceled) || errors.Is(err, ErrRunCancelled) {
			return nil
		}
		if errors.Is(err, context.DeadlineExceeded) {
			return a.timeoutRun(execCtx, &run, err)
		}
		return err
	})
	if errors.Is(err, context.Canceled) || errors.Is(err, ErrRunCancelled) {
		return nil
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return a.timeoutRun(ctx, &run, err)
	}
	return err
}

func syncRunValue(target *Run, source *Run) {
	if target == nil || source == nil || target == source {
		return
	}
	cloned := cloneRun(source)
	if cloned == nil {
		return
	}
	*target = *cloned
}

// ListTools returns the tools visible to a session key. When sessionKey is
// empty, it returns the global tool inventory without session preparation.
func (a *AgentComponent) ListTools(ctx context.Context, sessionKey string) ([]ToolDefinition, error) {
	if strings.TrimSpace(sessionKey) == "" {
		return filterToolDefinitionsForRun(buildToolDefinitions(skill.SessionSkillSnapshot{}, nil, a.tools), nil), nil
	}
	if a.context == nil {
		return nil, fmt.Errorf("%w", ErrContextEngineNil)
	}

	session, err := a.sessions.GetOrCreate(ctx, strings.TrimSpace(sessionKey), a.config.DefaultModel)
	if err != nil {
		return nil, err
	}
	runtimeCtx, err := a.runtime.Current(ctx, session, nil)
	if err != nil {
		return nil, err
	}
	a.injectPromptMemoryFacts(ctx, session, runtimeCtx)
	prepared, _, err := a.context.Prepare(ctx, &session.Session, toContextRun(nil, a.systemPromptFor(nil, session, runtimeCtx)), runtimeCtx)
	if err != nil {
		return nil, err
	}
	return buildAllowedToolDefinitions(prepared.Skills, session, a.tools, nil), nil
}

// ResumeRun continues a run that is waiting on approval. The run's approval
// ticket must already be approved or an error is returned.
func (a *AgentComponent) ResumeRun(ctx context.Context, runID string) error {
	if a.approvals == nil {
		return ErrApprovalStoreNil
	}
	ctx, cancel := context.WithTimeout(ctx, a.config.MaxRunDuration)
	defer cancel()
	release, claimed := a.claimRunExecution(runID)
	if !claimed {
		return nil
	}
	defer release()

	run, err := a.runs.Get(ctx, runID)
	if err != nil {
		return err
	}
	switch run.Status {
	case RunRunning:
		return nil
	case RunCompleted, RunFailed:
		return fmt.Errorf("run %s is already %s", runID, run.Status)
	}
	if err := a.ensureRunnable(ctx, &run); err != nil {
		if errors.Is(err, ErrRunCancelled) {
			return fmt.Errorf("%w: %s", ErrRunCancelled, runID)
		}
		if errors.Is(err, context.DeadlineExceeded) {
			return a.timeoutRun(ctx, &run, err)
		}
		return err
	}
	ticket, err := a.approvals.GetByRun(ctx, runID)
	if err != nil {
		return err
	}
	if ticket.Status != approval.StatusApproved {
		return fmt.Errorf("run %s approval status is %s", runID, ticket.Status)
	}

	err = a.withRunCancellation(ctx, run.ID, func(execCtx context.Context) error {
		finishQueue, claimed, err := a.claimQueuedExecution(execCtx, run)
		if err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, ErrRunCancelled) {
				return nil
			}
			if errors.Is(err, context.DeadlineExceeded) {
				return a.timeoutRun(execCtx, &run, err)
			}
			return err
		}
		if !claimed {
			return nil
		}
		defer finishQueue()

		err = a.dispatchRunExecution(execCtx, run, runDispatchOptions{
			eventType: eventbus.EventRunResumed,
			eventAttrs: eventbus.RunDispatchAttrs{
				ApprovalID:   ticket.ID,
				AgentProfile: effectiveAgentProfileAttrs(run.EffectiveProfile),
			}.ToMap(),
			clearError: true,
		})
		if errors.Is(err, context.Canceled) || errors.Is(err, ErrRunCancelled) {
			return nil
		}
		if errors.Is(err, context.DeadlineExceeded) {
			return a.timeoutRun(execCtx, &run, err)
		}
		return err
	})
	if errors.Is(err, context.Canceled) || errors.Is(err, ErrRunCancelled) {
		return nil
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return a.timeoutRun(ctx, &run, err)
	}
	return err
}

func (a *AgentComponent) systemPromptFor(run *Run, session *Session, runtimeCtx skill.RuntimeContext) string {
	return effectiveSystemPromptWithRuntimeContext(a.config.SystemPrompt, run, session, runtimeCtx)
}

func (a *AgentComponent) observeSessionRevision(run *Run, session *Session) {
	if run == nil || session == nil {
		return
	}
	run.LastSessionRevision = session.Revision
}

func parseEffectiveAgentProfile(metadata map[string]any) *EffectiveAgentProfile {
	if len(metadata) == 0 {
		return nil
	}
	var decoded effectiveAgentProfileMetadata
	if raw, err := json.Marshal(metadata); err == nil {
		_ = json.Unmarshal(raw, &decoded)
	}
	profile := &EffectiveAgentProfile{
		Name:         normalize.String(decoded.Name),
		Model:        normalize.String(decoded.Model),
		SystemPrompt: normalize.String(decoded.SystemPrompt),
		MaxTokens:    decoded.MaxTokens.Int(),
		Source:       normalize.String(decoded.Source),
	}
	profile.AllowedTools = decoded.AllowedTools.Values()
	profile.AllowedSkills = decoded.AllowedSkills.Values()
	if profile.Name == "" && profile.Model == "" && profile.SystemPrompt == "" &&
		len(profile.AllowedTools) == 0 && len(profile.AllowedSkills) == 0 &&
		profile.MaxTokens == 0 && profile.Source == "" {
		return nil
	}
	return profile
}

func cloneEffectiveAgentProfile(profile *EffectiveAgentProfile) *EffectiveAgentProfile {
	if profile == nil {
		return nil
	}
	cloned := *profile
	cloned.AllowedTools = append([]string(nil), profile.AllowedTools...)
	cloned.AllowedSkills = append([]string(nil), profile.AllowedSkills...)
	return &cloned
}

func allowedSkillsForRun(run *Run) []string {
	if run == nil || run.EffectiveProfile == nil {
		return nil
	}
	return append([]string(nil), run.EffectiveProfile.AllowedSkills...)
}

func maxOutputTokensForRun(run *Run) int {
	if run == nil || run.EffectiveProfile == nil || run.EffectiveProfile.MaxTokens <= 0 {
		return 0
	}
	return run.EffectiveProfile.MaxTokens
}

func effectiveAgentProfileAttrs(profile *EffectiveAgentProfile) map[string]any {
	if profile == nil {
		return nil
	}
	return map[string]any{
		"name":                 profile.Name,
		"model":                profile.Model,
		"max_tokens":           profile.MaxTokens,
		"allowed_tools_count":  len(profile.AllowedTools),
		"allowed_skills_count": len(profile.AllowedSkills),
		"source":               profile.Source,
	}
}

type effectiveAgentProfileMetadata struct {
	Name          string                 `json:"agent_profile_name"`
	Model         string                 `json:"agent_profile_model"`
	SystemPrompt  string                 `json:"agent_profile_system_prompt"`
	AllowedTools  profileMetadataStrings `json:"agent_profile_tools"`
	AllowedSkills profileMetadataStrings `json:"agent_profile_skills"`
	MaxTokens     profileMetadataInt     `json:"agent_profile_max_tokens"`
	Source        string                 `json:"agent_profile_source"`
}

type profileMetadataInt int

// UnmarshalJSON accepts integer-like JSON numbers for profile metadata and
// leaves unsupported values at zero.
func (v *profileMetadataInt) UnmarshalJSON(data []byte) error {
	var number json.Number
	if err := json.Unmarshal(data, &number); err == nil {
		if parsed, err := number.Int64(); err == nil {
			*v = profileMetadataInt(parsed)
			return nil
		}
	}

	var fallback float64
	if err := json.Unmarshal(data, &fallback); err == nil {
		*v = profileMetadataInt(int(fallback))
		return nil
	}
	return nil
}

// Int returns the decoded metadata value as a plain int.
func (v profileMetadataInt) Int() int {
	return int(v)
}

type profileMetadataStrings []string

// UnmarshalJSON accepts either a string slice or a generic JSON array and
// normalizes the result into deduplicated strings.
func (v *profileMetadataStrings) UnmarshalJSON(data []byte) error {
	var items []string
	if err := json.Unmarshal(data, &items); err == nil {
		*v = profileMetadataStrings(normalize.DedupeStrings(items))
		return nil
	}

	var rawItems []any
	if err := json.Unmarshal(data, &rawItems); err == nil {
		items = make([]string, 0, len(rawItems))
		for _, item := range rawItems {
			items = append(items, fmt.Sprint(item))
		}
		*v = profileMetadataStrings(normalize.DedupeStrings(items))
		return nil
	}
	return nil
}

// Values returns a copy of the decoded metadata slice.
func (v profileMetadataStrings) Values() []string {
	if len(v) == 0 {
		return nil
	}
	return append([]string(nil), v...)
}

func (a *AgentComponent) saveSession(ctx context.Context, run *Run, session *Session) error {
	persistBrowserReferenceSummary(session)
	if err := a.sessions.Save(ctx, session); err != nil {
		return err
	}
	previousRevision := int64(0)
	if run != nil {
		previousRevision = run.LastSessionRevision
	}
	a.observeSessionRevision(run, session)
	if run != nil && run.LastSessionRevision != previousRevision {
		if err := a.runs.Update(ctx, run); err != nil {
			return err
		}
	}
	return nil
}

func toContextRun(run *Run, systemPrompt string) *contextengine.Run {
	if run == nil {
		return &contextengine.Run{SystemPrompt: systemPrompt}
	}
	return &contextengine.Run{
		ID:              run.ID,
		SystemPrompt:    systemPrompt,
		Goal:            goalForContextRun(run),
		TargetSummary:   targetSummaryForContextRun(run),
		Model:           run.Model,
		JobType:         jobTypeForContextRun(run),
		DetectedDomains: detectedDomainsForContextRun(run),
		AllowedSkills:   allowedSkillsForRun(run),
		MaxOutputTokens: maxOutputTokensForRun(run),
	}
}

func goalForContextRun(run *Run) string {
	if run == nil {
		return ""
	}
	if run.TaskContract != nil {
		if goal := strings.TrimSpace(run.TaskContract.Goal); goal != "" {
			return goal
		}
	}
	if run.Plan != nil {
		return strings.TrimSpace(run.Plan.Goal)
	}
	return ""
}

func targetSummaryForContextRun(run *Run) string {
	if run == nil || run.TaskContract == nil {
		return ""
	}
	return strings.TrimSpace(run.TaskContract.TargetSummary)
}

func jobTypeForContextRun(run *Run) string {
	if run == nil || run.TaskContract == nil {
		return ""
	}
	return strings.TrimSpace(run.TaskContract.JobType)
}

func detectedDomainsForContextRun(run *Run) []string {
	if run == nil || run.Preflight == nil {
		return nil
	}
	return append([]string(nil), run.Preflight.DetectedDomains...)
}

// lastUserMessage returns the content of the most recent user message in the
// session, used to preserve recent request context across tool selection.
func lastUserMessage(session *Session) string {
	if session == nil {
		return ""
	}
	for i := len(session.Messages) - 1; i >= 0; i-- {
		if session.Messages[i].Role == contextengine.RoleUser {
			return session.Messages[i].Content
		}
	}
	return ""
}

func lastUserMessageForRun(session *Session, runID string) string {
	if session == nil || strings.TrimSpace(runID) == "" {
		return lastUserMessage(session)
	}
	for i := len(session.Messages) - 1; i >= 0; i-- {
		msg := session.Messages[i]
		if msg.Role != contextengine.RoleUser {
			continue
		}
		if !messageMatchesRunID(msg.Metadata, runID) {
			continue
		}
		if content := strings.TrimSpace(msg.Content); content != "" {
			return content
		}
	}
	return lastUserMessage(session)
}

// toolCallSignature returns a deterministic string fingerprint for a set of
// tool calls so consecutive duplicate invocations can be detected.
func toolCallSignature(calls []ToolCall) string {
	if len(calls) == 0 {
		return ""
	}
	var b strings.Builder
	for i, tc := range calls {
		if i > 0 {
			b.WriteByte(';')
		}
		b.WriteString(tc.Name)
		b.WriteByte(':')
		args, _ := json.Marshal(tc.Input)
		b.Write(args)
	}
	return b.String()
}

func messageMetadataForRun(metadata map[string]any, runID string) map[string]any {
	out := cloneMap(metadata)
	if strings.TrimSpace(runID) == "" {
		return out
	}
	if out == nil {
		out = make(map[string]any, 1)
	}
	out[meta.KeyRunID] = runID
	return out
}

func toolResultsForRun(results []contextengine.ToolResult, runID string) []contextengine.ToolResult {
	if len(results) == 0 {
		return nil
	}
	out := make([]contextengine.ToolResult, 0, len(results))
	for _, result := range results {
		normalized := result.Normalized()
		metadata := cloneMap(normalized.Metadata)
		if strings.TrimSpace(runID) != "" {
			if metadata == nil {
				metadata = make(map[string]any, 1)
			}
			metadata[meta.KeyRunID] = runID
		}
		normalized.Metadata = metadata
		out = append(out, normalized)
	}
	return out
}

func runCompletionSummary(session *Session, runID string) string {
	if session == nil || strings.TrimSpace(runID) == "" {
		return ""
	}
	for i := len(session.Messages) - 1; i >= 0; i-- {
		msg := session.Messages[i]
		if msg.Role != contextengine.RoleAssistant {
			continue
		}
		if !messageMatchesRunID(msg.Metadata, runID) {
			continue
		}
		if summary := compactRunSummary(msg.TextContent()); summary != "" {
			return summary
		}
	}
	return ""
}

func compactRunSummary(text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	text = strings.Join(strings.Fields(text), " ")
	const maxLen = 240
	runes := []rune(text)
	if len(runes) <= maxLen {
		return text
	}
	return strings.TrimSpace(string(runes[:maxLen])) + "..."
}

func messageMatchesRunID(metadata map[string]any, runID string) bool {
	if strings.TrimSpace(runID) == "" || metadata == nil {
		return false
	}
	value, ok := metadata[meta.KeyRunID]
	if !ok || value == nil {
		return false
	}
	return strings.TrimSpace(fmt.Sprint(value)) == runID
}

func (a *AgentComponent) evaluateToolCalls(ctx context.Context, run *Run, session *Session, snapshot skill.SessionSkillSnapshot, calls []ToolCall, approvedTicket *approval.Ticket) (bool, error) {
	if a.policy == nil {
		return false, nil
	}
	for _, call := range calls {
		bound := a.resolveTool(session, snapshot, call.Name)
		decision, err := a.policy.EvaluateTool(ctx, policy.ToolContext{
			RunID:     run.ID,
			SessionID: session.ID,
			ToolName:  call.Name,
			Input:     cloneMap(call.Input),
			Tool:      bound,
		})
		if err != nil {
			return false, err
		}
		switch decision.Action {
		case policy.ActionAllow:
			continue
		case policy.ActionRequireApproval:
			if approvedTicketAllowsToolCall(approvedTicket, call) {
				continue
			}
			if err := a.waitApproval(ctx, run, session, calls, decision); err != nil {
				return false, err
			}
			return true, nil
		case policy.ActionDeny:
			if err := a.recordGovernanceDecision(ctx, run, calls, decision); err != nil {
				return false, err
			}
			if len(decision.Reasons) == 0 {
				return false, ErrToolDenied
			}
			return false, fmt.Errorf("%w: %s", ErrToolDenied, strings.Join(decision.Reasons, "; "))
		default:
			return false, fmt.Errorf("%w: unsupported policy action %q", ErrToolDenied, decision.Action)
		}
	}
	return false, nil
}

func approvedTicketAllowsToolCall(ticket *approval.Ticket, call ToolCall) bool {
	if ticket == nil || ticket.Status != approval.StatusApproved {
		return false
	}
	switch ticket.Scope {
	case approval.ScopeOnce, approval.ScopeSession, approval.ScopeAlways:
	default:
		return false
	}
	for _, approved := range ticket.ToolCalls {
		if approvedToolCallMatches(approved, call) {
			return true
		}
	}
	return false
}

func approvedToolCallMatches(approved approval.ToolCall, call ToolCall) bool {
	if strings.TrimSpace(approved.Name) != strings.TrimSpace(call.Name) {
		return false
	}
	if approved.ID != "" && call.ID != "" && strings.TrimSpace(approved.ID) != strings.TrimSpace(call.ID) {
		return false
	}
	scope := approved.ResourceScope
	if scope.Empty() {
		scope = approval.ResourceScopeFromToolCall(approved.Name, approved.Input)
	}
	if scope.Empty() {
		return true
	}
	return scope.MatchesCall(call.Name, call.Input)
}

// denyCheckToolCalls is a lightweight policy check for parallel task execution.
// It only enforces Deny decisions — RequireApproval is treated as Allow because
// parallel goroutines cannot block on interactive user approval.
func (a *AgentComponent) denyCheckToolCalls(ctx context.Context, run *Run, session *Session, snapshot skill.SessionSkillSnapshot, calls []ToolCall) error {
	if a.policy == nil {
		return nil
	}
	for _, call := range calls {
		bound := a.resolveTool(session, snapshot, call.Name)
		decision, err := a.policy.EvaluateTool(ctx, policy.ToolContext{
			RunID:     run.ID,
			SessionID: session.ID,
			ToolName:  call.Name,
			Input:     cloneMap(call.Input),
			Tool:      bound,
		})
		if err != nil {
			return err
		}
		if decision.Action == policy.ActionRequireApproval {
			if err := a.recordGovernanceDecision(ctx, run, calls, decision); err != nil {
				return err
			}
			reasons := decision.Reasons
			if len(reasons) == 0 {
				reasons = []string{"tool requires approval and cannot continue in parallel mode"}
			}
			return fmt.Errorf("%w: %s", ErrParallelApprovalRequired, strings.Join(reasons, "; "))
		}
		if decision.Action == policy.ActionDeny {
			if err := a.recordGovernanceDecision(ctx, run, calls, decision); err != nil {
				return err
			}
			if len(decision.Reasons) == 0 {
				return ErrToolDenied
			}
			return fmt.Errorf("%w: %s", ErrToolDenied, strings.Join(decision.Reasons, "; "))
		}
	}
	return nil
}

// chatWithStreaming calls ChatStream if the model supports streaming and an
// eventbus is available, otherwise falls back to the synchronous Chat method.
func (a *AgentComponent) chatWithStreaming(ctx context.Context, run *Run, session *Session, req ChatRequest) (*ModelResponse, error) {
	if sc, ok := a.model.(StreamingModelClient); ok && a.bus != nil {
		cb := &EventBusStreamCallback{Bus: a.bus, RunID: run.ID, SessionID: session.ID}
		return sc.ChatStream(ctx, req, cb)
	}
	return a.model.Chat(ctx, req)
}

// trackModelUsage records token usage after a model call completes.
func (a *AgentComponent) trackModelUsage(ctx context.Context, run *Run, session *Session, model, provider string, resp *ModelResponse, elapsed time.Duration) {
	if resp == nil || resp.Usage == nil {
		return
	}
	now := time.Now().UTC()
	costEstimate := usage.EstimateCostWithCache(
		model,
		resp.Usage.PromptTokens,
		resp.Usage.CompletionTokens,
		resp.Usage.CacheCreationInputTokens,
		resp.Usage.CacheReadInputTokens,
	)
	if a.usageTracker != nil {
		logging.LogIfErr(ctx, a.usageTracker.TrackModelCall(ctx, usage.Record{
			RunID:             safeRunID(run),
			WorkflowID:        workflowIDForRun(run),
			ParentRunID:       run.ParentRunID,
			ContinuationIndex: workflowContinuationIndex(run),
			SessionID:         safeSessionID(session),
			Model:             model,
			Provider:          provider,
			PromptTokens:      resp.Usage.PromptTokens,
			CompletionTokens:  resp.Usage.CompletionTokens,
			TotalTokens:       resp.Usage.TotalTokens,
			CostEstimate:      costEstimate,
			Duration:          elapsed,
			RecordType:        usage.RecordTypeModelCall,
		}), "track model usage failed", slog.String("run_id", safeRunID(run)))
	}
	if updateWorkflowBudgetModelUsage(
		run,
		resp.Usage.PromptTokens,
		resp.Usage.CompletionTokens,
		resp.Usage.TotalTokens,
		costEstimate,
		now,
	) {
		a.persistWorkflowBudgetState(ctx, run)
	}
}

// trackToolBatchUsage records a tool_execution usage record for each tool call
// in a batch. The batch duration is split evenly across calls as individual
// per-tool timing is not available at this level.
func (a *AgentComponent) trackToolBatchUsage(ctx context.Context, run *Run, session *Session, calls []ToolCall, batchDuration time.Duration) {
	if len(calls) == 0 {
		return
	}
	perToolDuration := batchDuration / time.Duration(len(calls))
	if a.usageTracker != nil {
		for _, call := range calls {
			logging.LogIfErr(ctx, a.usageTracker.TrackToolExecution(ctx, usage.Record{
				RunID:             safeRunID(run),
				WorkflowID:        workflowIDForRun(run),
				ParentRunID:       run.ParentRunID,
				ContinuationIndex: workflowContinuationIndex(run),
				SessionID:         safeSessionID(session),
				ToolName:          call.Name,
				ToolCallID:        call.ID,
				Duration:          perToolDuration,
			}), "track tool usage failed", slog.String("tool", call.Name))
		}
	}
	if updateWorkflowBudgetToolUsage(run, len(calls), batchDuration, time.Now().UTC()) {
		a.persistWorkflowBudgetState(ctx, run)
	}
}

func (a *AgentComponent) persistWorkflowBudgetState(ctx context.Context, run *Run) {
	if a == nil || a.runs == nil || run == nil || run.WorkflowState == nil || run.WorkflowState.Budget == nil {
		return
	}
	logging.LogIfErr(ctx, a.runs.Update(ctx, run), "persist workflow budget failed", slog.String("run_id", safeRunID(run)))
}

func (a *AgentComponent) emit(ctx context.Context, event eventbus.Event) error {
	if a.bus == nil {
		return nil
	}
	if traceID := logging.TraceIDFromContext(ctx); traceID != "" {
		if event.Attrs == nil {
			event.Attrs = map[string]any{}
		}
		if _, ok := event.Attrs[logging.AttrKeyTraceID]; !ok {
			event.Attrs[logging.AttrKeyTraceID] = traceID
		}
	}
	return a.bus.Publish(ctx, event)
}

func (a *AgentComponent) refreshRun(ctx context.Context, run *Run) (*Run, bool, error) {
	latest, err := a.runs.Get(ctx, run.ID)
	if err != nil {
		return nil, false, err
	}
	refreshed := cloneRun(latest)
	return refreshed, refreshed.Status == RunCancelled, nil
}

func (a *AgentComponent) ensureRunnable(ctx context.Context, runRef **Run) error {
	if runRef == nil || *runRef == nil {
		return nil
	}
	refreshed, cancelled, err := a.refreshRun(ctx, *runRef)
	if err != nil {
		return err
	}
	*runRef = refreshed
	if cancelled {
		return ErrRunCancelled
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return nil
}

func (a *AgentComponent) claimRunExecution(runID string) (func(), bool) {
	if a == nil || strings.TrimSpace(runID) == "" {
		return func() {}, true
	}
	a.maybeSweepRunState(time.Now().UTC())
	if _, loaded := a.executing.LoadOrStore(runID, runExecutionEntry{claimedAt: time.Now().UTC()}); loaded {
		return func() {}, false
	}
	return func() {
		a.executing.Delete(runID)
	}, true
}

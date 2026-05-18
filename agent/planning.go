package agent

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"github.com/fulcrus/hopclaw/contextengine"
	"github.com/fulcrus/hopclaw/eventbus"
	"github.com/fulcrus/hopclaw/logging"
	planpkg "github.com/fulcrus/hopclaw/planner"
	"github.com/fulcrus/hopclaw/skill"
)

const DefaultPlannerTimeout = 5 * time.Second

type ModelPlanner struct {
	model        ModelClient
	timeout      time.Duration
	defaultModel string // if set, overrides req.Model for all plan calls
}

func NewModelPlanner(model ModelClient, timeout time.Duration) *ModelPlanner {
	if model == nil {
		return nil
	}
	if timeout <= 0 {
		timeout = DefaultPlannerTimeout
	}
	return &ModelPlanner{model: model, timeout: timeout}
}

// WithDefaultModel sets a dedicated model name for task planning.
// When set, this model is used instead of the run model.
func (p *ModelPlanner) WithDefaultModel(model string) *ModelPlanner {
	if p != nil {
		p.defaultModel = strings.TrimSpace(model)
	}
	return p
}

func (p *ModelPlanner) Plan(ctx context.Context, req PlanningRequest) (*planpkg.Plan, error) {
	if p == nil || p.model == nil {
		return nil, ErrModelClientNil
	}
	ctx, cancel := context.WithTimeout(ctx, p.timeout)
	defer cancel()

	payload, err := buildPlanningPayload(req)
	if err != nil {
		return nil, err
	}
	modelName := strings.TrimSpace(req.Model)
	if p.defaultModel != "" {
		modelName = p.defaultModel
	}
	resp, err := p.model.Chat(ctx, ChatRequest{
		Model:        modelName,
		SystemPrompt: planpkg.SystemPrompt(),
		Messages: []contextengine.Message{{
			Role:    contextengine.RoleUser,
			Content: payload,
		}},
		Budget: contextengine.Budget{
			ContextWindow:  4096,
			MaxInputTokens: 2048,
			ReservedOutput: 600,
		},
	})
	if err != nil {
		return nil, err
	}
	if resp == nil {
		return nil, ErrModelClientNil
	}
	return planpkg.Parse(resp.Message.Content)
}

func buildPlanningPayload(req PlanningRequest) (string, error) {
	ctx := planpkg.Context{
		LatestMessage:   strings.TrimSpace(req.LatestMessage),
		SessionSummary:  strings.TrimSpace(req.SessionSummary),
		RecentMessages:  make([]planpkg.Message, 0, len(req.RecentMessages)),
		AvailableTools:  describeToolsForPlanning(req.AvailableTools, 64),
		PinnedFacts:     cloneStrings(req.PinnedFacts),
		SessionState:    strings.TrimSpace(req.SessionState),
		RecalledContext: strings.TrimSpace(req.RecalledContext),
	}
	if req.TaskContract != nil {
		ctx.TaskContract = taskContractPlanningContext(req.TaskContract)
	}
	if req.Delegation != nil {
		ctx.Delegation = delegationPlanningContext(req.Delegation)
	}
	for _, msg := range req.RecentMessages {
		content := strings.TrimSpace(msg.Content)
		if content == "" {
			continue
		}
		ctx.RecentMessages = append(ctx.RecentMessages, planpkg.Message{
			Role:    string(msg.Role),
			Content: truncatePlanningText(content, 240),
		})
	}
	return planpkg.BuildPayload(ctx)
}

func fallbackPlanForRequest(req PlanningRequest) *planpkg.Plan {
	goal := strings.TrimSpace(req.LatestMessage)
	if goal == "" {
		goal = strings.TrimSpace(req.SessionSummary)
	}
	return planpkg.TrivialPlan(goal)
}

func truncatePlanningText(input string, limit int) string {
	input = strings.TrimSpace(input)
	if limit <= 0 || len(input) <= limit {
		return input
	}
	runes := []rune(input)
	if len(runes) <= limit {
		return input
	}
	if limit <= 1 {
		return string(runes[:limit])
	}
	return string(runes[:limit-1]) + "…"
}

func (a *AgentComponent) ensurePlan(ctx context.Context, run *Run, session *Session, runtimeCtx skill.RuntimeContext, force bool) error {
	if a == nil || run == nil || session == nil || !shouldUsePlanner(run, a.planner != nil) {
		return nil
	}
	if run.Plan != nil && !force {
		return nil
	}
	if a.context == nil {
		return ErrContextEngineNil
	}
	transitionRun(run, "", PhasePreparing)
	if err := a.runs.Update(ctx, run); err != nil {
		return err
	}

	planningSession := cloneSession(session)
	referenceSummary := sessionReferenceSummary(planningSession)
	filterSessionMessagesToRunOnly(planningSession, run.ID)
	planningSession.Summary = referenceSummary

	a.injectPromptMemoryFacts(ctx, planningSession, runtimeCtx)
	prepared, _, err := a.context.Prepare(ctx, &planningSession.Session, toContextRun(run, a.systemPromptFor(run, planningSession, runtimeCtx)), runtimeCtx)
	if err != nil {
		return err
	}
	req := PlanningRequest{
		Model:           run.Model,
		LatestMessage:   latestPlanningMessage(planningSession),
		SessionSummary:  planningSession.Summary,
		RecentMessages:  planningRecentMessages(planningSession, 6),
		AvailableTools:  buildAllowedToolDefinitions(prepared.Skills, planningSession, a.tools, run),
		TaskContract:    cloneTaskContract(run.TaskContract),
		Delegation:      cloneDelegationContract(deriveEffectiveDelegationContract(run, run.Delegation)),
		PinnedFacts:     planningPinnedFactsContext(planningSession.PinnedFacts),
		SessionState:    truncatePlanningText(prepared.SessionStatePrompt, 600),
		RecalledContext: truncatePlanningText(prepared.RecalledContextPrompt, 900),
	}
	plan, err := a.planner.Plan(ctx, req)
	if err != nil {
		plan = fallbackPlanForRequest(req)
	}
	if plan == nil {
		plan = fallbackPlanForRequest(req)
	}
	compactPlanForExecution(plan)
	enrichPlanVerificationHints(plan, req.LatestMessage, req.SessionSummary)
	applyPlanCoverageWarnings(plan, req.TaskContract)
	run.Plan = clonePlan(plan)
	if run.Plan == nil {
		run.Plan = fallbackPlanForRequest(req)
		compactPlanForExecution(run.Plan)
		enrichPlanVerificationHints(run.Plan, req.LatestMessage, req.SessionSummary)
		applyPlanCoverageWarnings(run.Plan, req.TaskContract)
	}
	planpkg.NormalizeExecution(run.Plan)
	ensureExecutionGraph(run)
	if err := a.runs.Update(ctx, run); err != nil {
		return err
	}
	logging.LogIfErr(ctx, a.emit(ctx, eventbus.NewRunPlannedEvent(
		run.ID,
		run.SessionID,
		eventbus.RunPlannedAttrs{
			TaskCount:        len(run.Plan.Tasks),
			Strategy:         string(run.Plan.Strategy),
			Replanned:        force,
			CoverageWarnings: cloneStrings(run.Plan.CoverageWarnings),
		},
		nil,
	)), "emit event failed", slog.String("kind", string(eventbus.EventRunPlanned)))
	for _, warning := range run.Plan.CoverageWarnings {
		slog.WarnContext(ctx, "planner coverage warning",
			slog.String("run_id", run.ID),
			slog.String("session_id", run.SessionID),
			slog.String("warning", warning),
		)
	}
	logging.LogIfErr(ctx, a.emitPlanSnapshot(ctx, run), "emit event failed", slog.String("kind", string(eventbus.EventPlanSnapshotUpdated)))
	return nil
}

func latestPlanningMessage(session *Session) string {
	if session == nil {
		return ""
	}
	for i := len(session.Messages) - 1; i >= 0; i-- {
		msg := session.Messages[i]
		if msg.Role != contextengine.RoleUser {
			continue
		}
		content := strings.TrimSpace(msg.Content)
		if content == "" {
			continue
		}
		return content
	}
	return strings.TrimSpace(session.Summary)
}

func filterSessionMessagesToRunOnly(session *Session, runID string) {
	if session == nil || strings.TrimSpace(runID) == "" || len(session.Messages) == 0 {
		return
	}
	filtered := make([]contextengine.Message, 0, len(session.Messages))
	for _, msg := range session.Messages {
		if !messageMatchesRunID(msg.Metadata, runID) {
			continue
		}
		filtered = append(filtered, msg)
	}
	session.Messages = filtered
	session.MessageCount = len(filtered)
}

func planningRecentMessages(session *Session, limit int) []contextengine.Message {
	if session == nil || limit <= 0 || len(session.Messages) == 0 {
		return nil
	}
	out := make([]contextengine.Message, 0, limit)
	for i := len(session.Messages) - 1; i >= 0 && len(out) < limit; i-- {
		msg := session.Messages[i]
		if msg.Role != contextengine.RoleUser && msg.Role != contextengine.RoleAssistant {
			continue
		}
		if strings.TrimSpace(msg.Content) == "" {
			continue
		}
		msg.ToolCalls = nil
		out = append(out, msg)
	}
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out
}

func taskContractPlanningContext(contract *TaskContract) *planpkg.Contract {
	if contract == nil {
		return nil
	}
	ctx := &planpkg.Contract{
		Goal:                   strings.TrimSpace(contract.Goal),
		JobType:                strings.TrimSpace(contract.JobType),
		TargetSummary:          strings.TrimSpace(contract.TargetSummary),
		SuggestedDomains:       cloneStrings(contract.SuggestedDomains),
		CapabilityHints:        cloneStrings(contract.CapabilityHints),
		RequiresExternalEffect: contract.RequiresExternalEffect,
		RequiresApproval:       contract.RequiresApproval,
	}
	for _, deliverable := range contract.ExpectedDeliverables {
		text := planningDeliverableText(deliverable)
		if strings.TrimSpace(text) != "" {
			ctx.ExpectedDeliverables = append(ctx.ExpectedDeliverables, text)
		}
		if evidence := planningEvidenceRequirement(deliverable); evidence != "" {
			ctx.EvidenceRequirements = append(ctx.EvidenceRequirements, evidence)
		}
	}
	for _, acceptance := range contract.AcceptanceCriteria {
		if trimmed := strings.TrimSpace(acceptance.Summary); trimmed != "" {
			ctx.AcceptanceCriteria = append(ctx.AcceptanceCriteria, trimmed)
		}
		for _, hint := range acceptance.EvidenceHints {
			if trimmed := strings.TrimSpace(hint); trimmed != "" {
				ctx.EvidenceRequirements = append(ctx.EvidenceRequirements, trimmed)
			}
		}
	}
	for _, item := range contract.MissingInfo {
		if !item.Required {
			continue
		}
		if trimmed := planningMissingInfoText(item); trimmed != "" {
			ctx.MissingInfo = append(ctx.MissingInfo, trimmed)
		}
	}
	ctx.SuggestedDomains = normalizeSemanticDomains(ctx.SuggestedDomains)
	ctx.CapabilityHints = normalizeCapabilityHints(ctx.CapabilityHints)
	ctx.EvidenceRequirements = normalizePlanningContextStrings(ctx.EvidenceRequirements)
	if len(ctx.ExpectedDeliverables) == 0 {
		ctx.ExpectedDeliverables = nil
	}
	if len(ctx.SuggestedDomains) == 0 {
		ctx.SuggestedDomains = nil
	}
	if len(ctx.CapabilityHints) == 0 {
		ctx.CapabilityHints = nil
	}
	if len(ctx.EvidenceRequirements) == 0 {
		ctx.EvidenceRequirements = nil
	}
	if len(ctx.AcceptanceCriteria) == 0 {
		ctx.AcceptanceCriteria = nil
	}
	if len(ctx.MissingInfo) == 0 {
		ctx.MissingInfo = nil
	}
	return ctx
}

func planningPinnedFactsContext(facts []contextengine.PinnedFact) []string {
	if len(facts) == 0 {
		return nil
	}
	out := make([]string, 0, len(facts))
	for _, fact := range facts {
		key := strings.TrimSpace(fact.Key)
		if key == "_memory_guide" {
			continue
		}
		content := strings.TrimSpace(fact.Content)
		if content == "" {
			continue
		}
		if key != "" && !strings.HasPrefix(key, "_") {
			content = "[" + key + "] " + content
		}
		out = append(out, truncatePlanningText(content, 220))
	}
	return normalizePlanningContextStrings(out)
}

func planningDeliverableText(deliverable TaskContractDeliverable) string {
	kind := strings.TrimSpace(deliverable.Kind)
	summary := strings.TrimSpace(deliverable.Summary)
	prefix := "expected"
	if deliverable.Required {
		prefix = "required"
	}
	switch {
	case kind != "" && summary != "":
		return prefix + " " + kind + ": " + summary
	case kind != "":
		return prefix + " " + kind
	case summary != "":
		return prefix + " deliverable: " + summary
	default:
		return ""
	}
}

func planningEvidenceRequirement(deliverable TaskContractDeliverable) string {
	text := planningDeliverableText(deliverable)
	switch strings.TrimSpace(deliverable.Kind) {
	case taskDeliverableBrowserEvidence, taskDeliverableDesktopEvidence, taskDeliverableMessageDelivery, taskDeliverableWatchAlert, taskDeliverableDeployment:
		return text
	default:
		return ""
	}
}

func planningMissingInfoText(item TaskContractMissingInfo) string {
	if summary := strings.TrimSpace(item.Summary); summary != "" {
		return summary
	}
	if question := strings.TrimSpace(item.Question); question != "" {
		return question
	}
	return strings.TrimSpace(item.ID)
}

func normalizePlanningContextStrings(items []string) []string {
	if len(items) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(items))
	out := make([]string, 0, len(items))
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		out = append(out, item)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func delegationPlanningContext(contract *DelegationContract) *planpkg.Delegation {
	if contract == nil {
		return nil
	}
	return &planpkg.Delegation{
		Goal:                strings.TrimSpace(contract.Goal),
		AllowedDomains:      cloneStrings(contract.AllowedDomains),
		SideEffectClass:     strings.TrimSpace(contract.SideEffectClass),
		MaxTurns:            contract.MaxTurns,
		MaxBudgetTokens:     contract.MaxBudgetTokens,
		RequiresApproval:    contract.RequiresApproval,
		VerificationPlanRef: strings.TrimSpace(contract.VerificationPlanRef),
	}
}

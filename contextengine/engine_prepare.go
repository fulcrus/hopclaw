package contextengine

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/fulcrus/hopclaw/internal/support/ints"
	"github.com/fulcrus/hopclaw/skill"
)

func (e *SlidingWindowEngine) Prepare(ctx context.Context, session *Session, run *Run, runtimeCtx skill.RuntimeContext) (*PreparedContext, Budget, error) {
	if session == nil {
		return nil, Budget{}, fmt.Errorf("session is required")
	}
	skillSnapshot := e.bindSkills(session, runtimeCtx)
	if run != nil {
		skillSnapshot = filterSkillSnapshot(skillSnapshot, run.AllowedSkills)
	}
	basePrompt := strings.TrimSpace(e.config.BaseSystemPrompt)
	runPrompt := ""
	if run != nil {
		runPrompt = strings.TrimSpace(run.SystemPrompt)
	}

	window := e.config.DefaultContextWindow
	if run != nil && run.MaxContextTokens > 0 {
		window = run.MaxContextTokens
	}
	reservedOutput := e.config.DefaultOutputTokens
	if run != nil && run.MaxOutputTokens > 0 {
		reservedOutput = run.MaxOutputTokens
	}
	maxInput := ints.Min(window-reservedOutput, int(float64(window)*e.config.MaxInputRatio))
	if maxInput < 0 {
		maxInput = 0
	}
	budgetPlan := PlanBudget(maxInput, budgetPlanJobType(run), budgetPlanDomains(run))

	summarySource := e.prepareSummarySource(ctx, session)
	summaryPrompt := e.summaryForPrompt(summarySource)
	pinnedFacts := e.refreshPinnedFacts(session)
	pinnedFactsPrompt := e.pinnedFactsForPrompt(pinnedFacts)
	pinnedFactsPrompt = e.trimPinnedFactsPromptToBudget(pinnedFactsPrompt, budgetPlan.PinnedFacts)
	statePrompt := e.prepareStatePrompt(ctx, session, budgetPlan)
	recalled := e.prepareRecalledPrompt(ctx, session, run, summarySource, budgetPlan)
	recalledPrompt := recalled.prompt
	skillPrompt := e.selectSkillPrompt(session, run, skillSnapshot)
	systemPrompt := joinPromptSections(basePrompt, runPrompt, skillPrompt, pinnedFactsPrompt, statePrompt, recalledPrompt)
	selected := e.selectMessages(session.Messages)
	if budgetPlan.RecentMessages <= 0 {
		selected = nil
	} else {
		// Until a dedicated knowledge prompt block exists, reclaim the budget
		// reserved for knowledge into recent messages so the allocation is not lost.
		recentMessageBudget := budgetPlan.RecentMessages + budgetPlan.Knowledge
		selected = e.trimMessagesToBudget("", summaryPrompt, selected, recentMessageBudget+e.config.Estimator.Estimate(summaryPrompt))
	}
	selected = e.trimMessagesToBudget(systemPrompt, summaryPrompt, selected, maxInput)

	segments := e.buildSegments(basePrompt, runPrompt, skillPrompt, pinnedFactsPrompt, statePrompt, recalledPrompt, summaryPrompt, selected)
	budget := Budget{
		ContextWindow:        window,
		ReservedOutput:       reservedOutput,
		MaxInputTokens:       maxInput,
		EstimatedInputTokens: segmentTokens(segments),
	}
	budget.RemainingInputTokens = ints.Max(0, budget.MaxInputTokens-budget.EstimatedInputTokens)

	// Check whether compaction is advisable by comparing the raw message token
	// count before trim against the threshold.
	needsCompaction := false
	if e.config.AutoCompactThreshold > 0 && budget.MaxInputTokens > 0 {
		rawMessageTokens := e.config.Estimator.EstimateMessages(session.Messages)
		systemTokens := e.config.Estimator.Estimate(systemPrompt) + e.config.Estimator.Estimate(summaryPrompt)
		rawTotalInput := rawMessageTokens + systemTokens
		usageRatio := float64(rawTotalInput) / float64(budget.MaxInputTokens)
		needsCompaction = usageRatio >= e.config.AutoCompactThreshold
	}

	prepared := &PreparedContext{
		SystemPrompt:          systemPrompt,
		Messages:              e.composeMessages(summaryPrompt, selected),
		Skills:                skillSnapshot,
		Segments:              segments,
		Budget:                budget,
		SessionStatePrompt:    statePrompt,
		RecalledContextPrompt: recalledPrompt,
		RetrievalReceipt:      recalled.receipt,
		NeedsCompaction:       needsCompaction,
	}
	return prepared, budget, nil
}

func (e *SlidingWindowEngine) Inspect(ctx context.Context, session *Session, run *Run, runtimeCtx skill.RuntimeContext) (*ContextReport, error) {
	prepared, budget, err := e.Prepare(ctx, session, run, runtimeCtx)
	if err != nil {
		return nil, err
	}
	report := &ContextReport{
		GeneratedAt:        time.Now().UTC(),
		SystemPrompt:       prepared.SystemPrompt,
		Segments:           append([]ContextSegment(nil), prepared.Segments...),
		Budget:             budget,
		SkillFingerprint:   prepared.Skills.Fingerprint,
		EligibleSkillCount: len(prepared.Skills.PromptCatalog),
		BlockedSkillCount:  len(prepared.Skills.Blocked),
		RetrievalReceipt:   prepared.RetrievalReceipt,
	}
	return report, nil
}

func (e *SlidingWindowEngine) prepareSummarySource(ctx context.Context, session *Session) string {
	if session == nil {
		return ""
	}
	if e.config.SegmentReader != nil && strings.TrimSpace(session.ID) != "" {
		segments, err := e.config.SegmentReader.RecentSegments(ctx, session.ID, 1, 1)
		if err != nil {
			log.Warn("failed to load recent summary segment, falling back to session summary", "session_id", session.ID, "error", err)
		} else if len(segments) > 0 {
			if text := strings.TrimSpace(segments[0].SummaryText); text != "" {
				return text
			}
		}
	}
	return session.Summary
}

func (e *SlidingWindowEngine) prepareStatePrompt(ctx context.Context, session *Session, budget BudgetPlan) string {
	if session == nil || e.config.StateReader == nil || strings.TrimSpace(session.ID) == "" {
		return ""
	}
	states, err := e.config.StateReader.ActiveStates(ctx, session.ID)
	if err != nil {
		log.Warn("failed to load session state, continuing without session_state prompt", "session_id", session.ID, "error", err)
		return ""
	}
	if len(states) == 0 || budget.TaskState <= 0 {
		return ""
	}
	return e.sessionStateForPrompt(states, budget.TaskState)
}

package contextengine

import (
	"context"
	"log/slog"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/fulcrus/hopclaw/skill"
)

// bindSkills returns the effective session skill snapshot and refreshes the
// cached session snapshot when the registry or runtime context changed.
func (e *SlidingWindowEngine) bindSkills(session *Session, runtimeCtx skill.RuntimeContext) skill.SessionSkillSnapshot {
	if session != nil && session.SkillSnapshot.Fingerprint != "" && isNilSkillBinder(e.skills) {
		return session.SkillSnapshot
	}
	if isNilSkillBinder(e.skills) {
		return skill.SessionSkillSnapshot{}
	}
	currentSnapshot := e.skills.Snapshot()
	currentCtxFingerprint := skill.FingerprintRuntimeContext(runtimeCtx)
	if session != nil &&
		session.SkillSnapshot.Fingerprint != "" &&
		session.SkillSnapshot.Fingerprint == currentSnapshot.Fingerprint &&
		session.SkillSnapshot.ContextFingerprint == currentCtxFingerprint {
		return session.SkillSnapshot
	}
	snapshot := e.skills.BindSession(runtimeCtx)
	if session != nil {
		session.SkillSnapshot = snapshot
	}
	return snapshot
}

func isNilSkillBinder(binder SkillBinder) bool {
	if binder == nil {
		return true
	}
	value := reflect.ValueOf(binder)
	switch value.Kind() {
	case reflect.Ptr, reflect.Interface, reflect.Map, reflect.Slice, reflect.Func:
		return value.IsNil()
	default:
		return false
	}
}

func (e *SlidingWindowEngine) composeSystemPrompt(run *Run, skillsSnapshot skill.SessionSkillSnapshot) (string, string, string, string) {
	basePrompt := strings.TrimSpace(e.config.BaseSystemPrompt)
	runPrompt := ""
	if run != nil {
		runPrompt = strings.TrimSpace(run.SystemPrompt)
	}
	skillPrompt := ""
	if e.config.IncludeSkillCatalog {
		skillPrompt = strings.TrimSpace(skillsSnapshot.PromptBlock)
	}
	if skillPrompt != "" {
		if overrides := detectSkillPromptOverride(skillPrompt); len(overrides) > 0 {
			attrs := []any{slog.Any("matches", overrides)}
			if run != nil && run.ID != "" {
				attrs = append(attrs, slog.String("run_id", run.ID))
			}
			slog.Warn("skill prompt contains override-style phrases; skill sections are advisory and cannot rewrite base/run directives", attrs...)
		}
	}
	return basePrompt, runPrompt, skillPrompt, joinPromptSections(basePrompt, runPrompt, skillPrompt)
}

// joinPromptSections concatenates prompt parts in a fixed priority contract:
//
//  1. Base system prompt — HopClaw invariants; authoritative and not overridable.
//  2. Run system prompt  — per-run task directives supplied by the caller.
//  3. Skill prompt       — advisory capability hints from bound skills.
//
// Sections are joined with blank-line separators; empty sections are dropped.
// Later sections may ADD guidance but MUST NOT attempt to rewrite earlier hard
// rules — skill authors should treat the base/run layer as read-only. Skill
// prompts containing override-style verbs ("ignore previous instructions",
// "override system rules", etc.) are logged at WARN by composeSystemPrompt so
// operators can audit the skill source. The authoritative separation between
// layers is enforced by priority order plus runtime governance, not by this
// heuristic.
func joinPromptSections(parts ...string) string {
	filtered := make([]string, 0, len(parts))
	for _, part := range parts {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			filtered = append(filtered, trimmed)
		}
	}
	return strings.Join(filtered, "\n\n")
}

// skillPromptOverridePatterns flags classic prompt-override phrasing that no
// legitimate skill needs. Matches are logged (not blocked) so operators can
// audit where such content originated. Detection is heuristic — it is NOT a
// security boundary, and absence of matches does NOT certify a skill safe.
var skillPromptOverridePatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)\bignore\s+(?:the\s+|all\s+|previous\s+|prior\s+|above\s+|these\s+|your\s+|my\s+)*(?:instructions?|rules?|guidelines?|system\s+prompt)`),
	regexp.MustCompile(`(?i)\bdisregard\s+(?:the\s+|all\s+|previous\s+|prior\s+|above\s+|these\s+|your\s+|my\s+)*(?:instructions?|rules?|guidelines?|system\s+prompt)`),
	regexp.MustCompile(`(?i)\boverride\s+(?:the\s+|all\s+|safety\s+|system\s+|base\s+)?(?:rules?|instructions?|guidelines?|prompt)`),
	regexp.MustCompile(`(?i)\bforget\s+(?:the\s+|all\s+|previous\s+|your\s+)*(?:instructions?|rules?|system\s+prompt)`),
	regexp.MustCompile(`(?i)\bdo\s+not\s+follow\s+(?:the\s+|any\s+|all\s+)?(?:above\s+|previous\s+|prior\s+)?(?:instructions?|rules?|system)`),
}

// detectSkillPromptOverride returns override-style phrases found in a skill
// prompt. Empty slice when clean. Match text is normalised (lower-case,
// whitespace collapsed, deduped) so identical phrases only surface once.
func detectSkillPromptOverride(skillPrompt string) []string {
	if strings.TrimSpace(skillPrompt) == "" {
		return nil
	}
	seen := make(map[string]struct{})
	var out []string
	for _, pattern := range skillPromptOverridePatterns {
		if m := pattern.FindString(skillPrompt); m != "" {
			phrase := strings.ToLower(strings.Join(strings.Fields(m), " "))
			if _, dup := seen[phrase]; dup {
				continue
			}
			seen[phrase] = struct{}{}
			out = append(out, phrase)
		}
	}
	return out
}

func (e *SlidingWindowEngine) selectSkillPrompt(session *Session, run *Run, skillsSnapshot skill.SessionSkillSnapshot) string {
	if !e.config.IncludeSkillCatalog || len(skillsSnapshot.PromptCatalog) == 0 {
		return ""
	}
	block := strings.TrimSpace(skill.FormatPromptCatalog(skillsSnapshot.PromptCatalog))
	if block == "" {
		block = strings.TrimSpace(skillsSnapshot.PromptBlock)
	}
	limit := e.config.SkillPromptMaxChars
	if limit <= 0 || len(block) <= limit {
		return block
	}

	contextText := skillPromptContext(session, run)
	var detectedDomains []string
	if run != nil {
		detectedDomains = append([]string(nil), run.DetectedDomains...)
	}
	ordered := prioritizePromptCatalog(skillsSnapshot.PromptCatalog, contextText, detectedDomains)
	selected := make([]skill.PromptCatalogEntry, 0, len(ordered))
	best := ""
	for _, entry := range ordered {
		candidate := append(selected[:len(selected):len(selected)], entry)
		formatted := skill.FormatPromptCatalogWithNotice(candidate, len(ordered)-len(candidate), skillPromptOmittedNotice)
		if len(formatted) > limit {
			if len(selected) == 0 {
				return softTrimContent(formatted, limit, 0)
			}
			break
		}
		selected = candidate
		best = formatted
	}
	if best == "" {
		best = skill.FormatPromptCatalogWithNotice(selected, len(ordered)-len(selected), skillPromptOmittedNotice)
	}
	return strings.TrimSpace(best)
}

func skillPromptContext(session *Session, run *Run) string {
	parts := make([]string, 0, 6)
	if run != nil && strings.TrimSpace(run.SystemPrompt) != "" {
		parts = append(parts, strings.TrimSpace(run.SystemPrompt))
	}
	if session != nil && strings.TrimSpace(session.Summary) != "" {
		parts = append(parts, stripCompactReasonMarkers(session.Summary))
	}
	if session != nil {
		for i := len(session.Messages) - 1; i >= 0 && len(parts) < 6; i-- {
			msg := session.Messages[i]
			if msg.Role != RoleUser {
				continue
			}
			if text := strings.TrimSpace(msg.TextContent()); text != "" {
				parts = append(parts, text)
			}
		}
	}
	return strings.ToLower(strings.Join(parts, "\n"))
}

func prioritizePromptCatalog(entries []skill.PromptCatalogEntry, contextText string, detectedDomains []string) []skill.PromptCatalogEntry {
	if len(entries) == 0 {
		return nil
	}
	if strings.TrimSpace(contextText) == "" && len(detectedDomains) == 0 {
		return append([]skill.PromptCatalogEntry(nil), entries...)
	}
	tokens := promptContextTokens(contextText)
	domainSet := promptCatalogDomainSet(detectedDomains)
	type scoredEntry struct {
		entry        skill.PromptCatalogEntry
		domainScore  int
		catalogScore int
		index        int
	}
	scored := make([]scoredEntry, 0, len(entries))
	for idx, entry := range entries {
		scored = append(scored, scoredEntry{
			entry:        entry,
			domainScore:  scorePromptCatalogEntryDomains(entry, domainSet),
			catalogScore: scorePromptCatalogEntry(entry, contextText, tokens),
			index:        idx,
		})
	}
	sort.SliceStable(scored, func(i, j int) bool {
		if scored[i].domainScore != scored[j].domainScore {
			return scored[i].domainScore > scored[j].domainScore
		}
		if scored[i].catalogScore != scored[j].catalogScore {
			return scored[i].catalogScore > scored[j].catalogScore
		}
		return scored[i].index < scored[j].index
	})
	out := make([]skill.PromptCatalogEntry, 0, len(scored))
	for _, item := range scored {
		out = append(out, item.entry)
	}
	return out
}

func promptCatalogDomainSet(items []string) map[string]struct{} {
	if len(items) == 0 {
		return nil
	}
	out := make(map[string]struct{}, len(items))
	for _, item := range items {
		item = strings.ToLower(strings.TrimSpace(item))
		if item == "" {
			continue
		}
		out[item] = struct{}{}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func scorePromptCatalogEntryDomains(entry skill.PromptCatalogEntry, detectedDomains map[string]struct{}) int {
	if len(detectedDomains) == 0 || len(entry.ToolDomains) == 0 {
		return 0
	}
	score := 0
	for _, domain := range entry.ToolDomains {
		if _, ok := detectedDomains[strings.ToLower(strings.TrimSpace(domain))]; ok {
			score++
		}
	}
	return score
}

func promptContextTokens(text string) map[string]struct{} {
	return semanticTextTokenSet(text, 3)
}

func scorePromptCatalogEntry(entry skill.PromptCatalogEntry, contextText string, contextTokens map[string]struct{}) int {
	score := 0
	name := strings.ToLower(strings.TrimSpace(entry.Name))
	desc := strings.ToLower(strings.TrimSpace(entry.Description))
	if name != "" && strings.Contains(contextText, name) {
		score += 30
	}
	for token := range contextTokens {
		if name != "" && strings.Contains(name, token) {
			score += 8
		}
		if desc != "" && strings.Contains(desc, token) {
			score += 3
		}
	}
	return score
}

func (e *SlidingWindowEngine) selectMessages(messages []Message) []Message {
	if len(messages) == 0 {
		return nil
	}
	totalKeep := e.config.KeepFirstN + e.config.KeepLastN
	if totalKeep <= 0 || len(messages) <= totalKeep {
		return append([]Message(nil), messages...)
	}

	out := make([]Message, 0, totalKeep)
	if e.config.KeepFirstN > 0 {
		out = append(out, messages[:e.config.KeepFirstN]...)
	}
	start := len(messages) - e.config.KeepLastN
	if start < e.config.KeepFirstN {
		start = e.config.KeepFirstN
	}
	out = append(out, messages[start:]...)
	return out
}

func (e *SlidingWindowEngine) refreshPinnedFacts(session *Session) []PinnedFact {
	if session == nil {
		return nil
	}
	messageFacts := collectPinnedFacts(session.Messages)
	durableFacts := LoadPinnedFactsFromMemory(context.Background(), e.config.MemoryReader)
	session.PinnedFacts = mergePinnedFacts(durableFacts, session.PinnedFacts, messageFacts)
	return clonePinnedFacts(session.PinnedFacts)
}

func extractLatestUserMessage(messages []Message) string {
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role != RoleUser {
			continue
		}
		if content := strings.TrimSpace(messages[i].TextContent()); content != "" {
			return content
		}
	}
	return ""
}

func (e *SlidingWindowEngine) composeMessages(summary string, selected []Message) []Message {
	out := make([]Message, 0, len(selected)+1)
	if strings.TrimSpace(summary) != "" {
		out = append(out, Message{
			Role:      RoleSystem,
			Name:      "session-summary",
			Content:   strings.TrimSpace(summary),
			CreatedAt: time.Now().UTC(),
		})
	}
	out = append(out, selected...)
	out = repairOrphanedToolCalls(out)
	return out
}

func (e *SlidingWindowEngine) summaryForPrompt(summary string) string {
	summary = stripCompactReasonMarkers(summary)
	if summary == "" {
		return ""
	}
	return compressSummaryText(summary, e.config.CompactSummaryChars)
}

func (e *SlidingWindowEngine) pinnedFactsForPrompt(facts []PinnedFact) string {
	if len(facts) == 0 {
		return ""
	}
	lines := []string{"Pinned facts: preserve these facts unless newer evidence explicitly contradicts them."}
	for _, fact := range facts {
		content := strings.TrimSpace(fact.Content)
		if content == "" {
			continue
		}
		if key := strings.TrimSpace(fact.Key); key != "" {
			lines = append(lines, "- ["+key+"] "+content)
			continue
		}
		lines = append(lines, "- "+content)
	}
	block := strings.TrimSpace(strings.Join(lines, "\n"))
	if block == "" {
		return ""
	}
	if e.config.PinnedFactsMaxChars > 0 && len(block) > e.config.PinnedFactsMaxChars {
		return softTrimContent(block, e.config.PinnedFactsMaxChars, 0)
	}
	return block
}

const maxSyntheticToolResponses = 20

func repairOrphanedToolCalls(messages []Message) []Message {
	calledIDs := make(map[string]bool, len(messages))
	for _, msg := range messages {
		if msg.Role == RoleAssistant {
			for _, tc := range msg.ToolCalls {
				calledIDs[tc.ID] = true
			}
		}
	}

	cleaned := make([]Message, 0, len(messages))
	for _, msg := range messages {
		if msg.Role == RoleTool && msg.ToolCallID != "" && !calledIDs[msg.ToolCallID] {
			continue
		}
		cleaned = append(cleaned, msg)
	}

	responded := make(map[string]bool, len(cleaned))
	for _, msg := range cleaned {
		if msg.Role == RoleTool && msg.ToolCallID != "" {
			responded[msg.ToolCallID] = true
		}
	}

	syntheticCount := 0
	var repaired []Message
	for i, msg := range cleaned {
		repaired = append(repaired, msg)
		if msg.Role != RoleAssistant || len(msg.ToolCalls) == 0 {
			continue
		}
		for _, tc := range msg.ToolCalls {
			if responded[tc.ID] {
				continue
			}
			found := false
			for _, later := range cleaned[i+1:] {
				if later.Role == RoleTool && later.ToolCallID == tc.ID {
					found = true
					break
				}
			}
			if found || syntheticCount >= maxSyntheticToolResponses {
				continue
			}
			repaired = append(repaired, Message{
				Role:       RoleTool,
				Name:       tc.Name,
				ToolCallID: tc.ID,
				Content:    "[pending: tool call awaiting approval or execution]",
				CreatedAt:  msg.CreatedAt,
			})
			responded[tc.ID] = true
			syntheticCount++
		}
	}
	return repaired
}

func (e *SlidingWindowEngine) buildSegments(basePrompt, runPrompt, skillPrompt, pinnedFactsPrompt, statePrompt, recalledPrompt, summary string, selected []Message) []ContextSegment {
	segments := make([]ContextSegment, 0, 7)
	if basePrompt != "" {
		segments = append(segments, ContextSegment{
			Kind:   SegmentBaseSystem,
			Tokens: e.config.Estimator.Estimate(basePrompt),
		})
	}
	if runPrompt != "" {
		segments = append(segments, ContextSegment{
			Kind:   SegmentRunSystem,
			Tokens: e.config.Estimator.Estimate(runPrompt),
		})
	}
	if skillPrompt != "" {
		segments = append(segments, ContextSegment{
			Kind:   SegmentSkillPrompt,
			Tokens: e.config.Estimator.Estimate(skillPrompt),
		})
	}
	if pinnedFactsPrompt != "" {
		segments = append(segments, ContextSegment{
			Kind:   SegmentPinnedFacts,
			Tokens: e.config.Estimator.Estimate(pinnedFactsPrompt),
		})
	}
	if statePrompt != "" {
		segments = append(segments, ContextSegment{
			Kind:   SegmentSessionState,
			Tokens: e.config.Estimator.Estimate(statePrompt),
		})
	}
	if recalledPrompt != "" {
		segments = append(segments, ContextSegment{
			Kind:   SegmentRecalled,
			Tokens: e.config.Estimator.Estimate(recalledPrompt),
		})
	}
	if strings.TrimSpace(summary) != "" {
		segments = append(segments, ContextSegment{
			Kind:   SegmentSummary,
			Tokens: e.config.Estimator.Estimate(summary),
		})
	}
	if len(selected) > 0 {
		segments = append(segments, ContextSegment{
			Kind:         SegmentMessages,
			Tokens:       e.config.Estimator.EstimateMessages(selected),
			MessageCount: len(selected),
		})
	}
	return segments
}

func segmentTokens(segments []ContextSegment) int {
	total := 0
	for _, segment := range segments {
		total += segment.Tokens
	}
	return total
}

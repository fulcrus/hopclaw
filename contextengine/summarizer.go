package contextengine

import (
	"context"
	"fmt"
	"math"
	"regexp"
	"strings"
)

// ModelChat is the minimal model interface needed by summarizers.
// Callers wire this to their model backend (e.g. agent.ModelClient).
type ModelChat interface {
	ChatSimple(ctx context.Context, systemPrompt string, userMessage string) (string, error)
}

// ---------------------------------------------------------------------------
// Simple ModelSummarizer (single-stage, kept for backward compatibility)
// ---------------------------------------------------------------------------

// ModelSummarizer uses a single model call to summarize messages.
type ModelSummarizer struct {
	Model ModelChat
}

func (s ModelSummarizer) Summarize(ctx context.Context, messages []Message, maxChars int) (string, error) {
	if s.Model == nil {
		return "", fmt.Errorf("model is nil")
	}
	if len(messages) == 0 {
		return "", nil
	}
	transcript := formatTranscript(messages)
	if maxChars > 0 && len(transcript) > maxChars*3 {
		transcript = transcript[:maxChars*3]
	}
	constraint := ""
	if maxChars > 0 {
		constraint = fmt.Sprintf("\n\nKeep the summary under %d characters.", maxChars)
	}
	userMsg := "Summarize this conversation:\n\n" + transcript + constraint

	summary, err := s.Model.ChatSimple(ctx, singleStageSystemPrompt, userMsg)
	if err != nil {
		return "", err
	}
	return clampSummary(summary, maxChars), nil
}

// ---------------------------------------------------------------------------
// Multi-Stage Summarizer (production-grade, ported from OpenClaw compaction.ts)
// ---------------------------------------------------------------------------

const (
	baseChunkRatio             = 0.4
	minChunkRatio              = 0.15
	safetyMargin               = 1.2
	summarizationOverhead      = 4096
	maxExtractedIdentifiers    = 12
	defaultQualityGuardRetries = 2
	defaultMinMessagesForSplit = 4
)

// CompactionConfig configures the multi-stage summarizer.
type CompactionConfig struct {
	Model               ModelChat
	Estimator           TokenEstimator
	ContextWindow       int    // Model context window in tokens.
	IdentifierPolicy    string // "strict", "off", or "custom".
	CustomInstructions  string
	QualityGuardEnabled bool
	QualityGuardRetries int
}

// MultiStageSummarizer implements Summarizer with chunked, hierarchical
// summarization, quality guards, and identifier preservation.
type MultiStageSummarizer struct {
	cfg CompactionConfig
}

func NewMultiStageSummarizer(cfg CompactionConfig) *MultiStageSummarizer {
	if cfg.ContextWindow <= 0 {
		cfg.ContextWindow = 128000
	}
	if cfg.QualityGuardRetries <= 0 {
		cfg.QualityGuardRetries = defaultQualityGuardRetries
	}
	if cfg.IdentifierPolicy == "" {
		cfg.IdentifierPolicy = "strict"
	}
	return &MultiStageSummarizer{cfg: cfg}
}

func (s *MultiStageSummarizer) Summarize(ctx context.Context, messages []Message, maxChars int) (string, error) {
	if s.cfg.Model == nil {
		return "", fmt.Errorf("model is nil")
	}
	if s.cfg.Estimator == nil {
		return "", fmt.Errorf("token estimator is nil")
	}
	if len(messages) == 0 {
		return "", nil
	}

	maxChunkTokens := resolveMaxChunkTokens(
		s.cfg.ContextWindow,
		computeAdaptiveChunkRatio(messages, s.cfg.ContextWindow, s.cfg.Estimator),
		summarizationOverhead,
	)

	summary, err := s.summarizeInStages(ctx, messages, maxChunkTokens, maxChars)
	if err != nil {
		return "", err
	}

	if !s.cfg.QualityGuardEnabled {
		return clampSummary(summary, maxChars), nil
	}

	// Quality guard loop.
	identifiers := extractOpaqueIdentifiers(formatTranscript(messages), maxExtractedIdentifiers)
	latestAsk := extractLatestUserAsk(messages)
	totalAttempts := s.cfg.QualityGuardRetries + 1
	bestSummary := summary

	for attempt := 0; attempt < totalAttempts; attempt++ {
		result := auditSummaryQuality(bestSummary, requiredSummarySections, identifiers, latestAsk, s.cfg.IdentifierPolicy)
		if result.OK {
			return clampSummary(bestSummary, maxChars), nil
		}
		if attempt >= totalAttempts-1 {
			break // Keep best effort.
		}
		// Retry with quality feedback.
		feedback := "Quality check failed. Issues: " + strings.Join(result.Reasons, "; ") +
			". Please fix all issues and include every required section."
		extra := s.cfg.CustomInstructions
		if extra != "" {
			extra += "\n\n"
		}
		extra += feedback
		retry, err := s.summarizeChunk(ctx, messages, extra, maxChars)
		if err != nil {
			break // Keep best effort on error.
		}
		bestSummary = retry
	}
	return clampSummary(bestSummary, maxChars), nil
}

// ---------------------------------------------------------------------------
// Chunking
// ---------------------------------------------------------------------------

// splitByTokenShare divides messages into parts balanced by token count.
func splitByTokenShare(messages []Message, parts int, est TokenEstimator) [][]Message {
	if parts <= 1 || len(messages) <= parts {
		return [][]Message{messages}
	}
	totalTokens := est.EstimateMessages(messages)
	targetTokens := totalTokens / parts
	if targetTokens <= 0 {
		return [][]Message{messages}
	}

	var chunks [][]Message
	var current []Message
	currentTokens := 0

	for _, msg := range messages {
		msgTokens := est.Estimate(msg.TextContent())
		if len(current) > 0 && currentTokens+msgTokens > targetTokens && len(chunks) < parts-1 {
			chunks = append(chunks, current)
			current = nil
			currentTokens = 0
		}
		current = append(current, msg)
		currentTokens += msgTokens
	}
	if len(current) > 0 {
		chunks = append(chunks, current)
	}
	return chunks
}

// chunkByMaxTokens splits messages so each chunk fits within maxTokens.
func chunkByMaxTokens(messages []Message, maxTokens int, est TokenEstimator) [][]Message {
	if maxTokens <= 0 {
		return [][]Message{messages}
	}

	var chunks [][]Message
	var current []Message
	currentTokens := 0

	for _, msg := range messages {
		msgTokens := est.Estimate(msg.TextContent())
		if len(current) > 0 && currentTokens+msgTokens > maxTokens {
			chunks = append(chunks, current)
			current = nil
			currentTokens = 0
		}
		current = append(current, msg)
		currentTokens += msgTokens
	}
	if len(current) > 0 {
		chunks = append(chunks, current)
	}
	return chunks
}

// computeAdaptiveChunkRatio adjusts chunk ratio based on average message size.
func computeAdaptiveChunkRatio(messages []Message, contextWindow int, est TokenEstimator) float64 {
	if len(messages) == 0 || est == nil || contextWindow <= 0 {
		return baseChunkRatio
	}
	totalTokens := est.EstimateMessages(messages)
	avgTokens := float64(totalTokens) / float64(len(messages))
	avgRatio := avgTokens / float64(contextWindow)

	if avgRatio > 0.1 {
		reduction := math.Min(avgRatio*2, baseChunkRatio-minChunkRatio)
		return math.Max(minChunkRatio, baseChunkRatio-reduction)
	}
	return baseChunkRatio
}

func resolveMaxChunkTokens(contextWindow int, ratio float64, overhead int) int {
	tokens := int(float64(contextWindow)*ratio) - overhead
	if tokens < 1 {
		return 1
	}
	return tokens
}

// ---------------------------------------------------------------------------
// Multi-stage summarization
// ---------------------------------------------------------------------------

func (s *MultiStageSummarizer) summarizeInStages(ctx context.Context, messages []Message, maxChunkTokens, maxChars int) (string, error) {
	totalTokens := s.cfg.Estimator.EstimateMessages(messages)

	// Single-stage if small enough.
	if len(messages) < defaultMinMessagesForSplit || totalTokens <= maxChunkTokens {
		return s.summarizeWithFallback(ctx, messages, maxChars)
	}

	// Split into 2 token-balanced parts.
	splits := splitByTokenShare(messages, 2, s.cfg.Estimator)
	if len(splits) <= 1 {
		return s.summarizeWithFallback(ctx, messages, maxChars)
	}

	// Summarize each part.
	var partials []string
	for _, chunk := range splits {
		summary, err := s.summarizeWithFallback(ctx, chunk, maxChars)
		if err != nil {
			return "", err
		}
		partials = append(partials, summary)
	}

	// Merge partial summaries.
	mergeMessages := make([]Message, len(partials))
	for i, p := range partials {
		mergeMessages[i] = Message{
			Role:    RoleUser,
			Content: fmt.Sprintf("Partial summary %d/%d:\n%s", i+1, len(partials), p),
		}
	}
	return s.summarizeChunk(ctx, mergeMessages, mergeSummariesInstructions, maxChars)
}

func (s *MultiStageSummarizer) summarizeWithFallback(ctx context.Context, messages []Message, maxChars int) (string, error) {
	// Try full summarization.
	summary, err := s.summarizeChunked(ctx, messages, maxChars)
	if err == nil {
		return summary, nil
	}

	// Fallback: exclude oversized messages.
	var small []Message
	var oversizedNotes []string
	for _, msg := range messages {
		if s.isOversized(msg) {
			tokens := s.cfg.Estimator.Estimate(msg.TextContent())
			oversizedNotes = append(oversizedNotes,
				fmt.Sprintf("[Large %s message (~%dk tokens) omitted]", msg.Role, tokens/1000))
		} else {
			small = append(small, msg)
		}
	}
	if len(small) == 0 {
		return fmt.Sprintf("Context contained %d messages (%d oversized). Summary unavailable due to size limits.",
			len(messages), len(oversizedNotes)), nil
	}

	summary, err = s.summarizeChunked(ctx, small, maxChars)
	if err != nil {
		return fmt.Sprintf("Context contained %d messages (%d oversized). Summary unavailable due to size limits.",
			len(messages), len(oversizedNotes)), nil
	}
	if len(oversizedNotes) > 0 {
		summary += "\n" + strings.Join(oversizedNotes, "\n")
	}
	return summary, nil
}

func (s *MultiStageSummarizer) summarizeChunked(ctx context.Context, messages []Message, maxChars int) (string, error) {
	maxTokens := resolveMaxChunkTokens(
		s.cfg.ContextWindow,
		computeAdaptiveChunkRatio(messages, s.cfg.ContextWindow, s.cfg.Estimator),
		summarizationOverhead,
	)
	chunks := chunkByMaxTokens(messages, maxTokens, s.cfg.Estimator)

	var summaries []string
	for _, chunk := range chunks {
		summary, err := s.summarizeChunk(ctx, chunk, s.cfg.CustomInstructions, maxChars)
		if err != nil {
			return "", err
		}
		summaries = append(summaries, summary)
	}
	if len(summaries) == 1 {
		return summaries[0], nil
	}
	return strings.Join(summaries, "\n\n---\n\n"), nil
}

func (s *MultiStageSummarizer) summarizeChunk(ctx context.Context, messages []Message, extraInstructions string, maxChars int) (string, error) {
	transcript := formatTranscript(messages)

	systemPrompt := multiStageSystemPrompt
	if s.cfg.IdentifierPolicy == "strict" {
		systemPrompt += "\n\n" + identifierPreservationInstructions
	}
	if extraInstructions != "" {
		systemPrompt += "\n\n" + extraInstructions
	}

	constraint := ""
	if maxChars > 0 {
		constraint = fmt.Sprintf("\n\nKeep the summary under %d characters.", maxChars)
	}
	userMsg := "Summarize this conversation:\n\n" + transcript + constraint

	summary, err := s.cfg.Model.ChatSimple(ctx, systemPrompt, userMsg)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(summary), nil
}

func (s *MultiStageSummarizer) isOversized(msg Message) bool {
	tokens := float64(s.cfg.Estimator.Estimate(msg.TextContent()))
	return tokens > float64(s.cfg.ContextWindow)*0.5
}

// ---------------------------------------------------------------------------
// Quality guard
// ---------------------------------------------------------------------------

var requiredSummarySections = []string{
	"## Decisions",
	"## Open TODOs",
	"## Constraints/Rules",
	"## Pending user asks",
	"## Exact identifiers",
}

// QualityResult captures audit outcome.
type QualityResult struct {
	OK      bool
	Reasons []string
}

func auditSummaryQuality(summary string, sections []string, identifiers []string, latestAsk string, idPolicy string) QualityResult {
	var reasons []string
	lines := strings.Split(summary, "\n")

	// Check required sections in order.
	cursor := 0
	for _, section := range sections {
		found := false
		for i := cursor; i < len(lines); i++ {
			if strings.TrimSpace(lines[i]) == section {
				cursor = i + 1
				found = true
				break
			}
		}
		if !found {
			reasons = append(reasons, "missing_section:"+section)
		}
	}

	// Check identifier preservation.
	if idPolicy == "strict" && len(identifiers) > 0 {
		var missing []string
		upper := strings.ToUpper(summary)
		for _, id := range identifiers {
			if isPureHex(id) {
				if !strings.Contains(upper, strings.ToUpper(id)) {
					missing = append(missing, id)
				}
			} else {
				if !strings.Contains(summary, id) {
					missing = append(missing, id)
				}
			}
		}
		if len(missing) > 0 {
			show := missing
			if len(show) > 3 {
				show = show[:3]
			}
			reasons = append(reasons, "missing_identifiers:"+strings.Join(show, ","))
		}
	}

	// Check latest user ask reflected.
	if latestAsk != "" {
		words := semanticTextTokens(latestAsk, 4)
		overlap := 0
		normalizedSummary := strings.ToLower(summary)
		for _, w := range words {
			if strings.Contains(normalizedSummary, w) {
				overlap++
			}
		}
		if len(words) > 0 && float64(overlap)/float64(len(words)) < 0.3 {
			reasons = append(reasons, "latest_user_ask_not_reflected")
		}
	}

	return QualityResult{OK: len(reasons) == 0, Reasons: reasons}
}

// ---------------------------------------------------------------------------
// Identifier extraction
// ---------------------------------------------------------------------------

var identifierPattern = regexp.MustCompile(
	`(?:` +
		`[A-Fa-f0-9]{8,}` + // hex IDs (UUID, hash)
		`|https?://\S+` + // URLs
		`|/[\w.\-]{2,}(?:/[\w.\-]+)+` + // Unix paths
		`|[A-Za-z]:\\[\w\\\.\-]+` + // Windows paths
		`|[A-Za-z0-9._\-]+\.[A-Za-z0-9._/\-]+:\d{1,5}` + // host:port
		`|\b\d{6,}\b` + // 6+ digit numbers
		`)`,
)

func extractOpaqueIdentifiers(text string, maxCount int) []string {
	matches := identifierPattern.FindAllString(text, -1)
	seen := make(map[string]bool)
	var result []string
	for _, m := range matches {
		m = strings.Trim(m, ".,;:!?()[]{}\"'")
		normalized := normalizeIdentifier(m)
		if len(normalized) < 4 || seen[normalized] {
			continue
		}
		seen[normalized] = true
		result = append(result, m)
		if maxCount > 0 && len(result) >= maxCount {
			break
		}
	}
	return result
}

func normalizeIdentifier(s string) string {
	if isPureHex(s) {
		return strings.ToUpper(s)
	}
	return s
}

var hexPattern = regexp.MustCompile(`^[A-Fa-f0-9]+$`)

func isPureHex(s string) bool {
	return len(s) >= 8 && hexPattern.MatchString(s)
}

func extractLatestUserAsk(messages []Message) string {
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == RoleUser {
			content := strings.TrimSpace(messages[i].TextContent())
			if len(content) > 200 {
				content = content[:200]
			}
			return content
		}
	}
	return ""
}

// ---------------------------------------------------------------------------
// Prompts
// ---------------------------------------------------------------------------

const singleStageSystemPrompt = `You are a conversation summarizer. Produce a concise summary of the conversation that preserves:
- Active tasks and their current status (in-progress, blocked, pending)
- The last thing the user requested and what was being done about it
- Pending approvals or items awaiting user confirmation
- Delivery targets, output format requirements, and citation/source requests stated by the user
- The user's language preference and communication style
- Decisions made and their rationale
- TODOs, open questions, and constraints
- All opaque identifiers exactly as written (UUIDs, hashes, file paths, URLs, ports, dates)

Output only the summary text, no preamble.`

const multiStageSystemPrompt = `You are a conversation summarizer. Produce a structured summary with these required sections:

## Decisions
Decisions made and their rationale.

## Open TODOs
Tasks not yet completed, with current status.

## Constraints/Rules
Active constraints, rules, language preference, communication style, and citation/reference requirements.

## Pending user asks
The last thing the user requested, any pending approvals or confirmations, required delivery targets, output format expectations, and current progress.

## Exact identifiers
All opaque identifiers mentioned (UUIDs, hashes, file paths, URLs, ports, IP addresses, dates, version numbers).

Preserve all identifiers exactly as written. Prioritize recent context over older history. Output only the summary.`

const identifierPreservationInstructions = `CRITICAL: Preserve all opaque identifiers exactly as written (no shortening or reconstruction), including UUIDs, hashes, IDs, tokens, API keys, hostnames, IPs, ports, URLs, and file names.`

const mergeSummariesInstructions = `Merge these partial summaries into a single cohesive summary with all required sections.
MUST PRESERVE:
- Active tasks and their current status (in-progress, blocked, pending)
- Batch operation progress (e.g., "5/17 items completed")
- The last thing the user requested and what was being done about it
- Pending approvals or items awaiting user confirmation
- Delivery targets, output format requirements, and citation/source requests stated by the user
- The user's language preference and communication style
- Decisions made and their rationale
- TODOs, open questions, and constraints
- Any commitments or follow-ups promised
PRIORITIZE recent context over older history.`

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func formatTranscript(messages []Message) string {
	var b strings.Builder
	for _, msg := range messages {
		content := strings.TrimSpace(msg.TextContent())
		if content == "" {
			continue
		}
		fmt.Fprintf(&b, "[%s] %s\n", msg.Role, content)
	}
	return b.String()
}

func clampSummary(summary string, maxChars int) string {
	summary = strings.TrimSpace(summary)
	if maxChars > 0 && len(summary) > maxChars {
		summary = strings.TrimSpace(summary[:maxChars]) + "..."
	}
	return summary
}

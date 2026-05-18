package contextengine

import (
	"context"
	"crypto/sha1"
	"fmt"
	"sort"
	"strings"
	"time"
)

func (e *SlidingWindowEngine) Compact(ctx context.Context, session *Session, reason CompactReason) error {
	if session == nil {
		return fmt.Errorf("session is required")
	}
	if len(session.Messages) <= e.config.CompactKeepLastN {
		return nil
	}

	compactStart := time.Now()
	preTokens := e.config.Estimator.EstimateMessages(session.Messages)

	cutByCount := len(session.Messages) - e.config.CompactKeepLastN

	cutByTokens := cutByCount
	if e.config.CompactKeepRecentTokens > 0 && e.config.Estimator != nil {
		tokenBudget := e.config.CompactKeepRecentTokens
		cutByTokens = len(session.Messages)
		for i := len(session.Messages) - 1; i >= 0 && tokenBudget > 0; i-- {
			tokenBudget -= e.config.Estimator.EstimateMessages(session.Messages[i : i+1])
			cutByTokens = i
		}
	}

	cut := cutByCount
	if cutByTokens < cut {
		cut = cutByTokens
	}
	if cut <= 0 {
		return nil
	}
	older := session.Messages[:cut]
	discardedPersisted := 0
	if len(session.LoadedMessageSeqs) > 0 {
		discardedPersisted = cut
		if discardedPersisted > len(session.LoadedMessageSeqs) {
			discardedPersisted = len(session.LoadedMessageSeqs)
		}
	}
	segmentSeqStart, segmentSeqEnd, segmentRangeOK := compactedDiscardedRange(session.LoadedMessageSeqs, cut, discardedPersisted)

	var memoryFlushCount int
	if e.config.PreCompactHook != nil {
		if err := e.config.PreCompactHook(ctx, older, session); err != nil {
			log.Warn("pre-compact memory flush failed, continuing", "error", err)
		} else {
			memoryFlushCount = countFlushableSignals(older)
		}
	}
	if e.config.StateWriter != nil {
		if err := e.writeCompactState(ctx, session, older); err != nil {
			return err
		}
	}

	nextSession := cloneSession(session)
	nextSession.Messages = cloneMessages(session.Messages[cut:])
	if discardedPersisted > 0 {
		nextSession.ExecutionWatermark = session.LoadedMessageSeqs[discardedPersisted-1]
		nextSession.LoadedMessageSeqs = append([]int64(nil), session.LoadedMessageSeqs[discardedPersisted:]...)
	}
	nextSession.PinnedFacts = mergePinnedFacts(collectPinnedFacts(older), session.PinnedFacts)

	if e.config.MemoryWriter != nil {
		memoryFlushCount += SyncPinnedFactsToMemory(ctx, nextSession.PinnedFacts, e.config.MemoryWriter)
	}

	var summary string
	if e.config.Summarizer != nil {
		var err error
		compactCtx, cancel := context.WithTimeout(ctx, compactionTimeout)
		defer cancel()
		summary, err = e.config.Summarizer.Summarize(compactCtx, older, e.config.CompactSummaryChars)
		if err != nil {
			summary = buildCompactSummary(older, e.config.CompactSummaryChars)
		}
	} else {
		summary = buildCompactSummary(older, e.config.CompactSummaryChars)
	}

	if e.config.SegmentWriter != nil {
		episodeID, wroteSegment, err := e.writeCompactSegment(ctx, session, older, summary, segmentSeqStart, segmentSeqEnd, segmentRangeOK)
		if err != nil {
			return err
		}
		if wroteSegment {
			if err := e.maybeGenerateL2(ctx, episodeID); err != nil {
				log.Warn("failed to generate l2 roll-up, continuing", "session_id", session.ID, "episode_id", episodeID, "error", err)
			}
		}
	}

	nextSession.Summary = mergeCompactSummaries(session.Summary, summary, reason, e.config.CompactSummaryChars)
	if nextSession.Summary != "" {
		nextSession.SummaryAt = time.Now().UTC()
	}

	postTokens := e.config.Estimator.EstimateMessages(nextSession.Messages)
	event := CompactEvent{
		Session:          nextSession,
		Reason:           reason,
		DiscardedCount:   len(older),
		PreTokens:        preTokens,
		PostTokens:       postTokens,
		SummaryChars:     len(nextSession.Summary),
		PinnedFactCount:  len(nextSession.PinnedFacts),
		MemoryFlushCount: memoryFlushCount,
		Duration:         time.Since(compactStart),
	}
	if e.config.OnCompact != nil {
		e.config.OnCompact(event)
	}
	if e.config.PostCompactHook != nil {
		if err := e.config.PostCompactHook(ctx, event); err != nil {
			return fmt.Errorf("post-compact hook: %w", err)
		}
	}
	*session = *nextSession
	return nil
}

func (e *SlidingWindowEngine) writeCompactSegment(ctx context.Context, session *Session, older []Message, summary string, seqStart, seqEnd int64, rangeOK bool) (string, bool, error) {
	if len(older) == 0 || strings.TrimSpace(summary) == "" {
		return "", false, nil
	}
	if session == nil || strings.TrimSpace(session.ID) == "" {
		log.Warn("skipping segment write because session id is empty")
		return "", false, nil
	}
	if !rangeOK {
		log.Warn("skipping segment write because compacted messages do not have a stable seq range", "session_id", session.ID, "discarded", len(older), "loaded_seqs", len(session.LoadedMessageSeqs))
		return "", false, nil
	}

	episodeID, ok, err := ensureActiveEpisode(ctx, e.config.SegmentWriter, session.ID)
	if err != nil {
		return "", false, err
	}
	if !ok {
		log.Warn("skipping segment write because configured segment writer does not manage episodes", "session_id", session.ID)
		return "", false, nil
	}

	segment := buildCompactSegment(session.ID, episodeID, older, summary, seqStart, seqEnd)
	if e.config.EmbeddingClient != nil && strings.TrimSpace(segment.SummaryText) != "" {
		vectors, err := e.config.EmbeddingClient.Embed(ctx, []string{segment.SummaryText})
		switch {
		case err != nil:
			log.Warn("failed to generate segment embedding, continuing without semantic search support", "session_id", session.ID, "episode_id", episodeID, "error", err)
		case len(vectors) > 0:
			segment.Embedding = append([]float32(nil), vectors[0]...)
		}
	}
	if err := e.config.SegmentWriter.InsertSegment(ctx, segment); err != nil {
		return "", false, err
	}
	return episodeID, true, nil
}

func (e *SlidingWindowEngine) maybeGenerateL2(ctx context.Context, episodeID string) error {
	if e == nil || e.config.SegmentWriter == nil || strings.TrimSpace(episodeID) == "" {
		return nil
	}
	reader := e.config.SegmentReader
	if reader == nil {
		if fallback, ok := e.config.SegmentWriter.(SegmentReader); ok {
			reader = fallback
		}
	}
	if reader == nil {
		return nil
	}
	return MaybeGenerateL2(ctx, reader, e.config.SegmentWriter, episodeID)
}

func compactedDiscardedRange(loadedSeqs []int64, discardedCount, discardedPersisted int) (int64, int64, bool) {
	if discardedPersisted <= 0 || discardedPersisted != discardedCount {
		return 0, 0, false
	}
	if len(loadedSeqs) < discardedPersisted {
		return 0, 0, false
	}
	return loadedSeqs[0], loadedSeqs[discardedPersisted-1], true
}

func ensureActiveEpisode(ctx context.Context, writer SegmentWriter, sessionID string) (string, bool, error) {
	reader, ok := writer.(EpisodeReader)
	if !ok {
		return "", false, nil
	}
	episodeID, err := reader.ActiveEpisode(ctx, sessionID)
	if err != nil {
		return "", false, err
	}
	if strings.TrimSpace(episodeID) != "" {
		return episodeID, true, nil
	}

	episodeWriter, ok := writer.(EpisodeWriter)
	if !ok {
		return "", false, nil
	}
	episodeID, err = episodeWriter.CreateEpisode(ctx, sessionID, "default")
	if err != nil {
		return "", false, err
	}
	return episodeID, true, nil
}

func buildCompactSegment(sessionID, episodeID string, messages []Message, summary string, seqStart, seqEnd int64) SummarySegment {
	tsStart, tsEnd := compactSegmentTimeRange(messages)
	decisions, todos, constraints := extractSegmentSignals(messages)
	entities := extractSegmentEntities(messages)
	return SummarySegment{
		SessionID:    sessionID,
		EpisodeID:    episodeID,
		Level:        1,
		SeqStart:     seqStart,
		SeqEnd:       seqEnd,
		TSStart:      tsStart,
		TSEnd:        tsEnd,
		SummaryText:  strings.TrimSpace(summary),
		Decisions:    decisions,
		TODOs:        todos,
		Constraints:  constraints,
		Entities:     entities,
		ArtifactRefs: extractArtifactRefs(entities),
		Keywords:     buildSegmentKeywords(summary, decisions, todos, constraints, entities),
		QualityScore: segmentQualityScore(summary, decisions, todos, constraints, entities),
		CreatedAt:    time.Now().UTC(),
	}
}

func (e *SlidingWindowEngine) writeCompactState(ctx context.Context, session *Session, older []Message) error {
	if session == nil || strings.TrimSpace(session.ID) == "" {
		log.Warn("skipping session state write because session id is empty")
		return nil
	}
	entries := buildCompactStateEntries(older)
	if len(entries) == 0 {
		return nil
	}
	return e.config.StateWriter.UpsertState(ctx, session.ID, entries)
}

func buildCompactStateEntries(messages []Message) []StateEntry {
	decisions, todos, constraints := extractSegmentSignals(messages)
	now := time.Now().UTC()
	entries := make([]StateEntry, 0, len(decisions)+len(todos)+len(constraints))
	appendEntries := func(category string, values []string) {
		for _, value := range values {
			entries = append(entries, StateEntry{
				Key:        compactStateKey(category, value),
				Category:   category,
				Value:      value,
				Status:     "active",
				Confidence: 1.0,
				CreatedAt:  now,
				UpdatedAt:  now,
			})
		}
	}
	appendEntries("decision", decisions)
	appendEntries("constraint", constraints)
	appendEntries("todo", todos)
	return entries
}

func compactStateKey(category, value string) string {
	category = strings.TrimSpace(strings.ToLower(category))
	value = strings.TrimSpace(value)
	sum := sha1.Sum([]byte(category + "\n" + value))
	return fmt.Sprintf("%s:%x", category, sum[:6])
}

func (e *SlidingWindowEngine) sessionStateForPrompt(states []StateEntry, maxTokens int) string {
	if len(states) == 0 || maxTokens == 0 {
		return ""
	}
	selected := make([]StateEntry, 0, len(states))
	for _, entry := range states {
		candidate := append(selected[:len(selected):len(selected)], entry)
		block := renderSessionStateBlock(candidate)
		if maxTokens > 0 && e.config.Estimator.Estimate(block) > maxTokens {
			if len(selected) == 0 {
				return renderTrimmedSessionState(entry, maxTokens)
			}
			break
		}
		selected = candidate
	}
	return renderSessionStateBlock(selected)
}

func renderSessionStateBlock(states []StateEntry) string {
	if len(states) == 0 {
		return ""
	}
	lines := make([]string, 0, len(states)+2)
	lines = append(lines, "<session_state>")
	for _, entry := range states {
		value := strings.TrimSpace(entry.Value)
		if value == "" {
			continue
		}
		lines = append(lines, fmt.Sprintf("- %s: %s", strings.TrimSpace(entry.Category), value))
	}
	lines = append(lines, "</session_state>")
	if len(lines) == 2 {
		return ""
	}
	return strings.Join(lines, "\n")
}

func renderTrimmedSessionState(entry StateEntry, maxTokens int) string {
	if maxTokens == 0 {
		return ""
	}
	line := fmt.Sprintf("- %s: %s", strings.TrimSpace(entry.Category), strings.TrimSpace(entry.Value))
	if maxTokens > 0 {
		line = softTrimContent(line, maxTokens*4, 0)
	}
	return strings.Join([]string{
		"<session_state>",
		line,
		"</session_state>",
	}, "\n")
}

func compactSegmentTimeRange(messages []Message) (time.Time, time.Time) {
	now := time.Now().UTC()
	if len(messages) == 0 {
		return now, now
	}
	start := messages[0].CreatedAt
	end := messages[len(messages)-1].CreatedAt
	if start.IsZero() {
		start = now
	}
	if end.IsZero() {
		end = start
	}
	if end.Before(start) {
		end = start
	}
	return start, end
}

func extractSegmentSignals(messages []Message) ([]string, []string, []string) {
	var decisions []string
	var todos []string
	var constraints []string
	for _, msg := range messages {
		msgDecisions, msgTODOs, msgConstraints := extractAnnotatedSignals(msg)
		decisions = append(decisions, msgDecisions...)
		todos = append(todos, msgTODOs...)
		constraints = append(constraints, msgConstraints...)
	}
	return dedupeStrings(decisions), dedupeStrings(todos), dedupeStrings(constraints)
}

func extractSegmentEntities(messages []Message) []string {
	if len(messages) == 0 {
		return nil
	}
	var text strings.Builder
	for _, msg := range messages {
		content := strings.TrimSpace(msg.TextContent())
		if content == "" {
			continue
		}
		if text.Len() > 0 {
			text.WriteByte('\n')
		}
		text.WriteString(content)
	}
	return dedupeStrings(extractOpaqueIdentifiers(text.String(), 16))
}

func extractArtifactRefs(entities []string) []string {
	if len(entities) == 0 {
		return nil
	}
	artifactRefs := make([]string, 0, len(entities))
	for _, entity := range entities {
		if strings.Contains(entity, "://") || strings.Contains(entity, "/") || strings.Contains(entity, `\`) {
			artifactRefs = append(artifactRefs, entity)
		}
	}
	return dedupeStrings(artifactRefs)
}

func buildSegmentKeywords(summary string, groups ...[]string) string {
	words := make(map[string]struct{})
	addWords := func(text string) {
		for _, field := range strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
			return !(r == '_' || r == '-' || (r >= '0' && r <= '9') || (r >= 'a' && r <= 'z'))
		}) {
			if len(field) < 3 {
				continue
			}
			words[field] = struct{}{}
		}
	}

	addWords(summary)
	for _, group := range groups {
		for _, item := range group {
			addWords(item)
		}
	}

	if len(words) == 0 {
		return ""
	}
	ordered := make([]string, 0, len(words))
	for word := range words {
		ordered = append(ordered, word)
	}
	sort.Strings(ordered)
	return strings.Join(ordered, " ")
}

func segmentQualityScore(summary string, decisions, todos, constraints, entities []string) float64 {
	score := 0.4
	if strings.TrimSpace(summary) != "" {
		score += 0.2
	}
	if len(decisions) > 0 {
		score += 0.1
	}
	if len(todos) > 0 {
		score += 0.1
	}
	if len(constraints) > 0 {
		score += 0.1
	}
	if len(entities) > 0 {
		score += 0.1
	}
	if score > 1.0 {
		return 1.0
	}
	return score
}

func dedupeStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		key := strings.ToLower(trimmed)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, trimmed)
	}
	return out
}

func mergeCompactSummaries(existing, incoming string, reason CompactReason, maxChars int) string {
	existing = stripCompactReasonMarkers(existing)
	incoming = stripCompactReasonMarkers(incoming)

	bodyParts := make([]string, 0, 2)
	if strings.TrimSpace(existing) != "" {
		bodyParts = append(bodyParts, strings.TrimSpace(existing))
	}
	if strings.TrimSpace(incoming) != "" {
		bodyParts = append(bodyParts, strings.TrimSpace(incoming))
	}
	body := strings.Join(bodyParts, "\n\n")
	if maxChars > 0 {
		body = compressSummaryText(body, maxChars)
	}
	if body == "" {
		return ""
	}

	reasonLine := "[compact_reason] " + string(reason)
	if maxChars > 0 && len(body)+1+len(reasonLine) > maxChars {
		body = compressSummaryText(body, maxChars-len(reasonLine)-1)
	}
	if body == "" {
		return reasonLine
	}
	return body + "\n" + reasonLine
}

func stripCompactReasonMarkers(summary string) string {
	if strings.TrimSpace(summary) == "" {
		return ""
	}
	lines := strings.Split(summary, "\n")
	filtered := make([]string, 0, len(lines))
	for _, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "[compact_reason] ") {
			continue
		}
		filtered = append(filtered, line)
	}
	return strings.TrimSpace(strings.Join(filtered, "\n"))
}

func compressSummaryText(summary string, maxChars int) string {
	summary = strings.TrimSpace(summary)
	if summary == "" || maxChars <= 0 || len(summary) <= maxChars {
		return summary
	}
	paragraphs := splitSummaryParagraphs(summary)
	if len(paragraphs) == 0 {
		return trimSummaryChunk(summary, maxChars)
	}
	const compactedMarker = "[previous summary compacted]"
	selected := make([]string, 0, len(paragraphs))
	used := 0
	for i := len(paragraphs) - 1; i >= 0; i-- {
		paragraph := paragraphs[i]
		extra := len(paragraph)
		if len(selected) > 0 {
			extra += 2
		}
		if used+extra > maxChars {
			if len(selected) == 0 {
				return trimSummaryChunk(paragraph, maxChars)
			}
			if used+len(compactedMarker)+2 <= maxChars {
				selected = append([]string{compactedMarker}, selected...)
			}
			break
		}
		selected = append([]string{paragraph}, selected...)
		used += extra
	}
	result := strings.Join(selected, "\n\n")
	if len(result) > maxChars {
		return trimSummaryChunk(result, maxChars)
	}
	return result
}

func splitSummaryParagraphs(summary string) []string {
	raw := strings.Split(summary, "\n\n")
	out := make([]string, 0, len(raw))
	for _, part := range raw {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

func trimSummaryChunk(text string, maxChars int) string {
	text = strings.TrimSpace(text)
	if maxChars <= 0 || len(text) <= maxChars {
		return text
	}
	if maxChars <= 24 {
		if len(text) > maxChars {
			return text[len(text)-maxChars:]
		}
		return text
	}
	const marker = "\n...[summary truncated]...\n"
	head := maxChars / 3
	tail := maxChars - len(marker) - head
	if tail < 8 {
		tail = 8
		head = maxChars - len(marker) - tail
	}
	if head < 8 {
		head = 8
	}
	if head+len(marker)+tail >= len(text) {
		return text
	}
	return strings.TrimSpace(text[:head]) + marker + strings.TrimSpace(text[len(text)-tail:])
}

func buildCompactSummary(messages []Message, maxChars int) string {
	if len(messages) == 0 {
		return ""
	}
	var b strings.Builder
	for _, msg := range messages {
		content := strings.TrimSpace(msg.TextContent())
		if content == "" {
			continue
		}
		if b.Len() > 0 {
			b.WriteByte('\n')
		}
		b.WriteString("[")
		b.WriteString(string(msg.Role))
		b.WriteString("] ")
		b.WriteString(content)
		if maxChars > 0 && b.Len() >= maxChars {
			break
		}
	}
	summary := b.String()
	if maxChars > 0 && len(summary) > maxChars {
		summary = strings.TrimSpace(summary[:maxChars]) + "..."
	}
	return summary
}

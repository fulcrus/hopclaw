package contextengine

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

type recalledPromptResult struct {
	prompt  string
	receipt *RetrievalReceipt
}

type recalledSearchHit struct {
	segment SummarySegment
	query   string
	score   float64
	order   int
}

type trackedRecalledHit struct {
	key     string
	segment SummarySegment
	block   string
	order   int
	queries []string
	hit     ReceiptHit
}

func (e *SlidingWindowEngine) prepareRecalledPrompt(ctx context.Context, session *Session, run *Run, summarySource string, budget BudgetPlan) recalledPromptResult {
	if session == nil || e.config.SegmentSearcher == nil || e.config.EmbeddingClient == nil || strings.TrimSpace(session.ID) == "" {
		return recalledPromptResult{}
	}

	lastUserMessage := extractLatestUserMessage(session.Messages)
	if lastUserMessage == "" {
		return recalledPromptResult{}
	}

	queries := dedupeStrings([]string{
		lastUserMessage,
		stripCompactReasonMarkers(summarySource),
		recalledPromptGoalQuery(run),
	})
	if len(queries) == 0 {
		return recalledPromptResult{}
	}

	resultsByQuery := make([][]SummarySegment, len(queries))
	var wg sync.WaitGroup
	for index, query := range queries {
		index := index
		query := query
		wg.Add(1)
		go func() {
			defer wg.Done()

			queryVectors, err := e.config.EmbeddingClient.Embed(ctx, []string{query})
			if err != nil {
				log.Warn("failed to generate query embedding for segment recall, skipping query", "session_id", session.ID, "error", err)
				return
			}
			if len(queryVectors) == 0 {
				return
			}

			recalled, err := e.config.SegmentSearcher.SearchSegments(ctx, session.ID, query, queryVectors[0], 3)
			if err != nil {
				log.Warn("failed to search recalled segments, skipping query", "session_id", session.ID, "error", err)
				return
			}
			resultsByQuery[index] = recalled
		}()
	}
	wg.Wait()

	rawHits := buildRecalledSearchHits(queries, resultsByQuery)
	if len(rawHits) == 0 {
		return recalledPromptResult{}
	}

	rerankLimit := 0
	if len(queries) > 1 {
		rerankLimit = 5
	}

	prompt, receipt := e.buildRecalledReceipt(queries, rawHits, rerankLimit, budget.RecalledContext)
	return recalledPromptResult{
		prompt:  prompt,
		receipt: receipt,
	}
}

func recalledPromptGoalQuery(run *Run) string {
	if run == nil {
		return ""
	}
	parts := make([]string, 0, 2)
	if goal := strings.TrimSpace(run.Goal); goal != "" {
		parts = append(parts, goal)
	}
	if target := strings.TrimSpace(run.TargetSummary); target != "" {
		if len(parts) == 0 || parts[0] != target {
			parts = append(parts, "target: "+target)
		}
	}
	return strings.TrimSpace(strings.Join(parts, "\n"))
}

func buildRecalledSearchHits(queries []string, resultsByQuery [][]SummarySegment) []recalledSearchHit {
	if len(resultsByQuery) == 0 {
		return nil
	}

	maxResults := 0
	total := 0
	for _, results := range resultsByQuery {
		if len(results) > maxResults {
			maxResults = len(results)
		}
		total += len(results)
	}
	if total == 0 {
		return nil
	}

	now := time.Now().UTC()
	hits := make([]recalledSearchHit, 0, total)
	order := 0
	for rank := 0; rank < maxResults; rank++ {
		for queryIndex, results := range resultsByQuery {
			if rank >= len(results) {
				continue
			}
			segment := results[rank]
			hits = append(hits, recalledSearchHit{
				segment: segment,
				query:   queries[queryIndex],
				score:   recalledSegmentScore(segment, order, total, now),
				order:   order,
			})
			order++
		}
	}
	return hits
}

func recalledSegmentScore(segment SummarySegment, index, total int, now time.Time) float64 {
	similarity := recalledSegmentSimilarityScore(index, total)
	recency := recalledSegmentRecencyScore(segment, now)
	coverage := recalledSegmentCoverageScore(segment)
	return 0.7*similarity + 0.2*recency + 0.1*coverage
}

func (e *SlidingWindowEngine) buildRecalledReceipt(queries []string, rawHits []recalledSearchHit, limit, maxTokens int) (string, *RetrievalReceipt) {
	if len(rawHits) == 0 {
		return "", nil
	}

	tracked := make([]trackedRecalledHit, 0, len(rawHits))
	for _, raw := range rawHits {
		block := renderRecalledSegmentBlock(raw.segment)
		tokens := 0
		if block != "" {
			tokens = e.config.Estimator.Estimate(block)
		}
		tracked = append(tracked, trackedRecalledHit{
			key:     recalledSegmentDedupKey(raw.segment),
			segment: raw.segment,
			block:   block,
			order:   raw.order,
			queries: []string{raw.query},
			hit: ReceiptHit{
				Kind:   "recalled_segment",
				ID:     receiptHitID(raw.segment),
				Score:  raw.score,
				Reason: recalledReceiptReason([]string{raw.query}),
				Tokens: tokens,
			},
		})
	}

	matchedQueries := make(map[string][]string, len(tracked))
	bestByKey := make(map[string]int, len(tracked))
	for index := range tracked {
		key := tracked[index].key
		matchedQueries[key] = dedupeStrings(append(matchedQueries[key], tracked[index].queries...))

		bestIndex, ok := bestByKey[key]
		if !ok || tracked[index].hit.Score > tracked[bestIndex].hit.Score || (tracked[index].hit.Score == tracked[bestIndex].hit.Score && tracked[index].order < tracked[bestIndex].order) {
			if ok {
				tracked[bestIndex].hit.TrimReason = "duplicate"
				tracked[bestIndex].hit.Reason = appendReceiptReason(tracked[bestIndex].hit.Reason, "deduplicated by a higher-ranked match")
			}
			bestByKey[key] = index
			continue
		}

		tracked[index].hit.TrimReason = "duplicate"
		tracked[index].hit.Reason = appendReceiptReason(tracked[index].hit.Reason, "duplicate segment match")
	}

	selected := make([]int, 0, len(bestByKey))
	for key, index := range bestByKey {
		tracked[index].queries = append([]string(nil), matchedQueries[key]...)
		tracked[index].hit.Reason = recalledReceiptReason(tracked[index].queries)
		selected = append(selected, index)
	}

	sort.SliceStable(selected, func(i, j int) bool {
		left := tracked[selected[i]]
		right := tracked[selected[j]]
		leftTime := recalledSegmentReferenceTime(left.segment)
		rightTime := recalledSegmentReferenceTime(right.segment)
		switch {
		case left.hit.Score != right.hit.Score:
			return left.hit.Score > right.hit.Score
		case !leftTime.Equal(rightTime):
			return leftTime.After(rightTime)
		case left.segment.QualityScore != right.segment.QualityScore:
			return left.segment.QualityScore > right.segment.QualityScore
		case strings.TrimSpace(left.segment.ID) != strings.TrimSpace(right.segment.ID):
			return strings.TrimSpace(left.segment.ID) < strings.TrimSpace(right.segment.ID)
		default:
			return left.order < right.order
		}
	})

	if limit > 0 && len(selected) > limit {
		for _, index := range selected[limit:] {
			tracked[index].hit.TrimReason = "low_score"
			tracked[index].hit.Reason = appendReceiptReason(tracked[index].hit.Reason, "dropped after rerank")
		}
		selected = selected[:limit]
	}

	prompt, totalTokens := e.applyRecalledPromptBudget(tracked, selected, maxTokens)
	receipt := &RetrievalReceipt{
		Queries:     append([]string(nil), queries...),
		Hits:        make([]ReceiptHit, 0, len(tracked)),
		Injected:    make([]ReceiptHit, 0, len(selected)),
		Trimmed:     make([]ReceiptHit, 0, len(tracked)),
		TotalTokens: totalTokens,
		GeneratedAt: time.Now().UTC(),
	}
	for _, entry := range tracked {
		receipt.Hits = append(receipt.Hits, entry.hit)
		if entry.hit.Injected {
			receipt.Injected = append(receipt.Injected, entry.hit)
		}
		if entry.hit.TrimReason != "" {
			receipt.Trimmed = append(receipt.Trimmed, entry.hit)
		}
	}
	return prompt, receipt
}

func (e *SlidingWindowEngine) applyRecalledPromptBudget(tracked []trackedRecalledHit, selected []int, maxTokens int) (string, int) {
	if len(selected) == 0 {
		return "", 0
	}
	if maxTokens == 0 {
		for _, index := range selected {
			tracked[index].hit.TrimReason = "budget_exceeded"
			tracked[index].hit.Reason = appendReceiptReason(tracked[index].hit.Reason, "not injected within recalled context budget")
		}
		return "", 0
	}

	selectedBlocks := make([]string, 0, len(selected))
	totalTokens := 0
	for position, index := range selected {
		if tracked[index].hit.TrimReason != "" {
			continue
		}
		if strings.TrimSpace(tracked[index].block) == "" {
			tracked[index].hit.TrimReason = "low_score"
			tracked[index].hit.Reason = appendReceiptReason(tracked[index].hit.Reason, "empty recalled content")
			continue
		}

		candidateBlocks := append(selectedBlocks[:len(selectedBlocks):len(selectedBlocks)], tracked[index].block)
		if maxTokens > 0 && e.config.Estimator.Estimate(strings.Join(candidateBlocks, "\n\n")) > maxTokens {
			if len(selectedBlocks) == 0 {
				trimmedBlock := renderTrimmedRecalledSegmentBlock(tracked[index].block, maxTokens)
				if strings.TrimSpace(trimmedBlock) == "" {
					tracked[index].hit.TrimReason = "budget_exceeded"
					tracked[index].hit.Reason = appendReceiptReason(tracked[index].hit.Reason, "not injected within recalled context budget")
				} else {
					tracked[index].hit.Injected = true
					tracked[index].hit.TrimReason = "budget_exceeded"
					tracked[index].hit.Reason = appendReceiptReason(tracked[index].hit.Reason, "trimmed to fit recalled context budget")
					tracked[index].hit.Tokens = e.config.Estimator.Estimate(trimmedBlock)
					selectedBlocks = append(selectedBlocks, trimmedBlock)
					totalTokens += tracked[index].hit.Tokens
				}
			} else {
				tracked[index].hit.TrimReason = "budget_exceeded"
				tracked[index].hit.Reason = appendReceiptReason(tracked[index].hit.Reason, "not injected within recalled context budget")
			}
			for _, remaining := range selected[position+1:] {
				if tracked[remaining].hit.TrimReason != "" {
					continue
				}
				tracked[remaining].hit.TrimReason = "budget_exceeded"
				tracked[remaining].hit.Reason = appendReceiptReason(tracked[remaining].hit.Reason, "not injected within recalled context budget")
			}
			break
		}

		tracked[index].hit.Injected = true
		tracked[index].hit.Tokens = e.config.Estimator.Estimate(tracked[index].block)
		selectedBlocks = append(selectedBlocks, tracked[index].block)
		totalTokens += tracked[index].hit.Tokens
	}

	return strings.TrimSpace(strings.Join(selectedBlocks, "\n\n")), totalTokens
}

func receiptHitID(segment SummarySegment) string {
	if id := strings.TrimSpace(segment.ID); id != "" {
		return id
	}
	return recalledSegmentSource(segment)
}

func recalledReceiptReason(queries []string) string {
	queries = dedupeStrings(queries)
	switch len(queries) {
	case 0:
		return "matched recalled segment search"
	case 1:
		return "matched query: " + queries[0]
	default:
		return "matched queries: " + strings.Join(queries, " | ")
	}
}

func appendReceiptReason(reason, suffix string) string {
	reason = strings.TrimSpace(reason)
	suffix = strings.TrimSpace(suffix)
	switch {
	case reason == "":
		return suffix
	case suffix == "":
		return reason
	default:
		return reason + "; " + suffix
	}
}

func recalledSegmentDedupKey(segment SummarySegment) string {
	if id := strings.TrimSpace(segment.ID); id != "" {
		return strings.ToLower(id)
	}
	return strings.ToLower(strings.TrimSpace(segment.SummaryText)) +
		"|" + segment.TSStart.UTC().Format(time.RFC3339Nano) +
		"|" + segment.TSEnd.UTC().Format(time.RFC3339Nano)
}

func recalledSegmentSimilarityScore(index, total int) float64 {
	if total <= 1 {
		return 1.0
	}
	if index < 0 {
		return 0
	}
	if index >= total {
		index = total - 1
	}
	return 1.0 - (float64(index) / float64(total-1))
}

func recalledSegmentRecencyScore(segment SummarySegment, now time.Time) float64 {
	reference := recalledSegmentReferenceTime(segment)
	if reference.IsZero() {
		return 0.5
	}
	if reference.After(now) {
		reference = now
	}

	age := now.Sub(reference)
	switch {
	case age <= 7*24*time.Hour:
		return 1.0
	case age <= 30*24*time.Hour:
		return 0.9
	case age <= 90*24*time.Hour:
		return 0.7
	default:
		return 0.5
	}
}

func recalledSegmentReferenceTime(segment SummarySegment) time.Time {
	start := segment.TSStart
	end := segment.TSEnd

	switch {
	case !start.IsZero() && !end.IsZero():
		if end.Before(start) {
			end = start
		}
		return start.Add(end.Sub(start) / 2)
	case !end.IsZero():
		return end
	case !start.IsZero():
		return start
	default:
		return segment.CreatedAt
	}
}

func recalledSegmentCoverageScore(segment SummarySegment) float64 {
	coverage := len(segment.Decisions) + len(segment.Constraints)
	if coverage <= 0 {
		return 0
	}
	score := float64(coverage) / 4.0
	if score > 1.0 {
		return 1.0
	}
	return score
}

func (e *SlidingWindowEngine) recalledSegmentsForPrompt(segments []SummarySegment, maxTokens int) string {
	if len(segments) == 0 || maxTokens == 0 {
		return ""
	}

	selected := make([]string, 0, len(segments))
	for index, segment := range segments {
		block := renderRecalledSegmentBlock(segment)
		if strings.TrimSpace(block) == "" {
			continue
		}
		candidate := append(selected[:len(selected):len(selected)], block)
		joined := strings.Join(candidate, "\n\n")
		if maxTokens > 0 && e.config.Estimator.Estimate(joined) > maxTokens {
			if len(selected) == 0 {
				return renderTrimmedRecalledSegmentBlock(block, maxTokens)
			}
			if index > 0 {
				break
			}
		}
		selected = candidate
	}
	return strings.TrimSpace(strings.Join(selected, "\n\n"))
}

func renderRecalledSegmentBlock(segment SummarySegment) string {
	summary := strings.TrimSpace(segment.SummaryText)
	if summary == "" {
		return ""
	}

	lines := []string{fmt.Sprintf(`<recalled_context source="%s">`, recalledSegmentSource(segment))}
	lines = append(lines, "Summary: "+summary)
	if len(segment.Decisions) > 0 {
		lines = append(lines, "Decisions: "+strings.Join(segment.Decisions, " | "))
	}
	if len(segment.TODOs) > 0 {
		lines = append(lines, "TODOs: "+strings.Join(segment.TODOs, " | "))
	}
	if len(segment.Constraints) > 0 {
		lines = append(lines, "Constraints: "+strings.Join(segment.Constraints, " | "))
	}
	lines = append(lines, "</recalled_context>")
	return strings.Join(lines, "\n")
}

func renderTrimmedRecalledSegmentBlock(block string, maxTokens int) string {
	if maxTokens == 0 {
		return ""
	}
	return softTrimContent(block, maxTokens*4, 0)
}

func recalledSegmentSource(segment SummarySegment) string {
	start := segment.TSStart
	if start.IsZero() {
		start = segment.CreatedAt
	}
	end := segment.TSEnd
	if end.IsZero() {
		end = start
	}
	if start.IsZero() {
		return fmt.Sprintf("segment %s", strings.TrimSpace(segment.ID))
	}
	return fmt.Sprintf("segment %s from %s to %s", strings.TrimSpace(segment.ID), start.Format("2006-01-02"), end.Format("2006-01-02"))
}

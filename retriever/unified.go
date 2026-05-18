package retriever

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
)

// UnifiedRetriever aggregates memory, recalled context segments, and knowledge
// sources under a single retrieval interface.
type UnifiedRetriever struct {
	Memory                 MemorySearcher
	Segments               SegmentSearcher
	Knowledge              KnowledgeSearcher
	Embedder               EmbeddingClient
	SegmentResultsPerQuery int
}

// Retrieve fans out to each configured source, normalizes the mixed hits, and
// applies a single rerank pass before returning the top results.
func (r *UnifiedRetriever) Retrieve(ctx context.Context, query Query) ([]Hit, error) {
	if r == nil {
		return nil, errors.New("retriever: nil unified retriever")
	}

	query = normalizeQuery(query)

	type sourceResult struct {
		hits []Hit
		err  error
	}

	resultsCh := make(chan sourceResult, 3)
	var wg sync.WaitGroup
	started := 0

	if r.Memory != nil {
		started++
		wg.Add(1)
		go func() {
			defer wg.Done()
			hits, err := r.Memory.SearchMemory(ctx, query)
			resultsCh <- sourceResult{
				hits: normalizeHits(hits, HitMemory, defaultAuthorityForKind(HitMemory)),
				err:  wrapSourceError("memory", err),
			}
		}()
	}

	if r.Segments != nil {
		started++
		wg.Add(1)
		go func() {
			defer wg.Done()
			hits, err := r.retrieveSegments(ctx, query)
			resultsCh <- sourceResult{
				hits: hits,
				err:  wrapSourceError("segments", err),
			}
		}()
	}

	if r.Knowledge != nil {
		started++
		wg.Add(1)
		go func() {
			defer wg.Done()
			hits, err := r.Knowledge.SearchKnowledge(ctx, query)
			resultsCh <- sourceResult{
				hits: normalizeHits(hits, HitKnowledge, defaultAuthorityForKind(HitKnowledge)),
				err:  wrapSourceError("knowledge", err),
			}
		}()
	}

	if started == 0 {
		return nil, errors.New("retriever: no sources configured")
	}

	go func() {
		wg.Wait()
		close(resultsCh)
	}()

	var (
		allHits []Hit
		errs    []error
	)
	for result := range resultsCh {
		if result.err != nil {
			errs = append(errs, result.err)
		}
		allHits = append(allHits, result.hits...)
	}

	if len(allHits) == 0 {
		if len(errs) == 0 {
			return nil, nil
		}
		return nil, errors.Join(errs...)
	}

	return Rerank(allHits, query.EffectiveMaxResults()), nil
}

func (r *UnifiedRetriever) retrieveSegments(ctx context.Context, query Query) ([]Hit, error) {
	if r.Segments == nil || strings.TrimSpace(query.SessionID) == "" {
		return nil, nil
	}

	queries := buildSegmentQueries(query)
	if len(queries) == 0 {
		return nil, nil
	}

	limit := r.segmentResultsPerQuery(query.EffectiveMaxResults())
	resultsByQuery := make([][]Hit, len(queries))
	errs := make([]error, 0, len(queries))
	var mu sync.Mutex
	var wg sync.WaitGroup

	for idx, segmentQuery := range queries {
		idx := idx
		segmentQuery := segmentQuery

		wg.Add(1)
		go func() {
			defer wg.Done()

			var queryEmbedding []float32
			if r.Embedder != nil {
				vectors, err := r.Embedder.Embed(ctx, []string{segmentQuery})
				if err != nil {
					mu.Lock()
					errs = append(errs, fmt.Errorf("embed %q: %w", segmentQuery, err))
					mu.Unlock()
					return
				}
				if len(vectors) > 0 {
					queryEmbedding = append([]float32(nil), vectors[0]...)
				}
			}

			hits, err := r.Segments.SearchSegments(ctx, query.SessionID, segmentQuery, queryEmbedding, limit)
			if err != nil {
				mu.Lock()
				errs = append(errs, fmt.Errorf("search %q: %w", segmentQuery, err))
				mu.Unlock()
				return
			}

			normalized := normalizeHits(hits, HitSegment, defaultAuthorityForKind(HitSegment))
			for i := range normalized {
				normalized[i].Reason = appendReason(normalized[i].Reason, "segment_query:"+segmentQuery)
			}
			resultsByQuery[idx] = normalized
		}()
	}

	wg.Wait()

	merged := dedupeSegmentHits(interleaveHits(resultsByQuery))
	if len(merged) > 0 {
		return merged, nil
	}
	if len(errs) > 0 {
		return nil, errors.Join(errs...)
	}
	return nil, nil
}

func normalizeQuery(query Query) Query {
	query.Text = strings.TrimSpace(query.Text)
	query.TargetSummary = strings.TrimSpace(query.TargetSummary)
	query.JobType = strings.TrimSpace(query.JobType)
	query.SessionID = strings.TrimSpace(query.SessionID)
	query.SessionKey = strings.TrimSpace(query.SessionKey)
	query.ProjectID = strings.TrimSpace(query.ProjectID)
	query.Domains = dedupeStrings(query.Domains)
	if query.MaxResults <= 0 {
		query.MaxResults = defaultMaxResults
	}
	return query
}

func (r *UnifiedRetriever) segmentResultsPerQuery(maxResults int) int {
	if r.SegmentResultsPerQuery > 0 {
		return r.SegmentResultsPerQuery
	}
	if maxResults > 0 && maxResults < defaultSegmentResultsPerQuery {
		return maxResults
	}
	return defaultSegmentResultsPerQuery
}

func buildSegmentQueries(query Query) []string {
	candidates := make([]string, 0, 4)

	if query.Text != "" {
		candidates = append(candidates, query.Text)
	}
	if query.TargetSummary != "" {
		candidates = append(candidates, query.TargetSummary)
	}
	if query.JobType != "" {
		if query.Text != "" {
			candidates = append(candidates, strings.TrimSpace(query.JobType+" "+query.Text))
		} else {
			candidates = append(candidates, query.JobType)
		}
	}
	if len(query.Domains) > 0 {
		domainQuery := strings.Join(query.Domains, " ")
		if query.Text != "" {
			candidates = append(candidates, strings.TrimSpace(query.Text+" "+domainQuery))
		} else {
			candidates = append(candidates, domainQuery)
		}
	}

	return dedupeStrings(candidates)
}

func interleaveHits(resultsByQuery [][]Hit) []Hit {
	maxLen := 0
	total := 0
	for _, hits := range resultsByQuery {
		total += len(hits)
		if len(hits) > maxLen {
			maxLen = len(hits)
		}
	}
	if total == 0 {
		return nil
	}

	merged := make([]Hit, 0, total)
	for rank := 0; rank < maxLen; rank++ {
		for _, hits := range resultsByQuery {
			if rank < len(hits) {
				merged = append(merged, hits[rank])
			}
		}
	}
	return merged
}

func dedupeSegmentHits(hits []Hit) []Hit {
	if len(hits) == 0 {
		return nil
	}

	type candidate struct {
		hit   Hit
		order int
	}
	bestByKey := make(map[string]candidate, len(hits))

	for idx, hit := range hits {
		key := segmentDedupKey(hit)
		existing, ok := bestByKey[key]
		if !ok || hit.Score > existing.hit.Score || (hit.Score == existing.hit.Score && idx < existing.order) {
			bestByKey[key] = candidate{hit: hit, order: idx}
		}
	}

	deduped := make([]Hit, 0, len(bestByKey))
	for _, item := range bestByKey {
		deduped = append(deduped, item.hit)
	}
	return deduped
}

func segmentDedupKey(hit Hit) string {
	switch {
	case strings.TrimSpace(hit.ID) != "":
		return hit.ID
	case strings.TrimSpace(hit.Citation) != "":
		return hit.Citation
	default:
		return hit.Content
	}
}

func appendReason(reason, extra string) string {
	reason = strings.TrimSpace(reason)
	extra = strings.TrimSpace(extra)
	switch {
	case reason == "":
		return extra
	case extra == "":
		return reason
	default:
		return reason + ", " + extra
	}
}

func wrapSourceError(source string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s retrieval failed: %w", source, err)
}

func dedupeStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}

	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

package agent

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/fulcrus/hopclaw/durablefact"
)

const (
	memorySearchDefaultLimit   = 20
	memorySearchChannelLimit   = 30
	memorySearchMMRLambda      = 0.7
	memorySearchLexicalWeight  = 0.4
	memorySearchSemanticWeight = 0.6
)

// MemorySearcher executes hybrid lexical + semantic retrieval over memory
// entries and reranks the merged candidates with MMR.
type MemorySearcher struct {
	store    MemoryStore
	embedder EmbeddingClient

	mu             sync.RWMutex
	entryEmbedding map[string]memoryEmbeddingCacheEntry
}

type memoryEmbeddingCacheEntry struct {
	fingerprint string
	vector      []float32
}

type memorySearchDocument struct {
	Entry         MemoryEntry
	lexicalFields []memorySearchField
	semanticText  string
	fingerprint   string
}

type memorySearchField struct {
	name string
	text string
}

type memorySearchCandidate struct {
	doc           memorySearchDocument
	lexicalScore  float64
	lexicalField  string
	semanticScore float64
	queryScore    float64
	finalScore    float64
	reason        string
	vector        []float32
}

type memorySearchDocumentSource interface {
	memorySearchDocuments(ctx context.Context) ([]memorySearchDocument, error)
}

// NewMemorySearcher builds a hybrid memory searcher. When embedder is nil, it
// attempts to reuse the embedding client exposed by the store.
func NewMemorySearcher(store MemoryStore, embedder EmbeddingClient) *MemorySearcher {
	if store == nil {
		return nil
	}
	if embedder == nil {
		if provider, ok := store.(interface{ EmbeddingClient() EmbeddingClient }); ok {
			embedder = provider.EmbeddingClient()
		}
	}
	return &MemorySearcher{
		store:          store,
		embedder:       embedder,
		entryEmbedding: make(map[string]memoryEmbeddingCacheEntry),
	}
}

// SearchMemories executes hybrid retrieval over the scoped memory set. It
// falls back to query-aware lexical retrieval when embeddings are unavailable.
func (s *MemorySearcher) SearchMemories(ctx context.Context, query MemoryQuery) ([]MemoryHit, error) {
	if s == nil || s.store == nil {
		return nil, fmt.Errorf("memory searcher requires a store")
	}

	documents, err := s.loadDocuments(ctx)
	if err != nil {
		return nil, err
	}
	documents = filterMemorySearchDocuments(documents, query)
	if len(documents) == 0 {
		return nil, nil
	}

	if recallQueryIsEmpty(query) {
		entries := make([]MemoryEntry, 0, len(documents))
		for _, document := range documents {
			entries = append(entries, document.Entry)
		}
		return RecallWithQuery(entries, query), nil
	}

	maxResults := query.MaxResults
	if maxResults <= 0 {
		maxResults = memorySearchDefaultLimit
	}

	lexical := memorySearchLexicalChannel(documents, query, memorySearchChannelLimit)
	if s.embedder == nil {
		return mmrRerank(memorySearchCandidatesToHits(lexical), memorySearchMMRLambda, maxResults), nil
	}

	semantic, err := s.memorySearchSemanticChannel(ctx, documents, query, memorySearchChannelLimit)
	if err != nil {
		return mmrRerank(memorySearchCandidatesToHits(lexical), memorySearchMMRLambda, maxResults), nil
	}

	merged := mergeMemorySearchCandidates(query, lexical, semantic)
	return mmrRerankCandidates(merged, memorySearchMMRLambda, maxResults), nil
}

// mmrRerank reranks the candidate hits with a Jaccard-based similarity fallback.
func mmrRerank(hits []MemoryHit, lambda float64, limit int) []MemoryHit {
	candidates := make([]memorySearchCandidate, 0, len(hits))
	for _, hit := range hits {
		document := newMemorySearchDocument(hit.Entry, "")
		candidates = append(candidates, memorySearchCandidate{
			doc:        document,
			queryScore: hit.RelevanceScore,
			finalScore: hit.RelevanceScore,
			reason:     hit.Reason,
		})
	}
	return mmrRerankCandidates(candidates, lambda, limit)
}

func (s *MemorySearcher) loadDocuments(ctx context.Context) ([]memorySearchDocument, error) {
	if provider, ok := s.store.(memorySearchDocumentSource); ok {
		documents, err := provider.memorySearchDocuments(ctx)
		if err != nil {
			return nil, err
		}
		sort.Slice(documents, func(i, j int) bool {
			return documents[i].Entry.Key < documents[j].Entry.Key
		})
		return documents, nil
	}

	entries, err := s.store.List(ctx)
	if err != nil {
		return nil, err
	}
	documents := make([]memorySearchDocument, 0, len(entries))
	for _, entry := range entries {
		documents = append(documents, newMemorySearchDocument(entry, ""))
	}
	return documents, nil
}

func filterMemorySearchDocuments(documents []memorySearchDocument, query MemoryQuery) []memorySearchDocument {
	filtered := make([]memorySearchDocument, 0, len(documents))
	for _, document := range documents {
		if !recallEntryMatchesScope(document.Entry, query.SessionKey, query.ProjectID) {
			continue
		}
		filtered = append(filtered, document)
	}
	return filtered
}

func memorySearchLexicalChannel(documents []memorySearchDocument, query MemoryQuery, limit int) []memorySearchCandidate {
	queryTokens := tokenizeRecallText(recallQueryText(query))
	if len(queryTokens) == 0 {
		return nil
	}

	candidates := make([]memorySearchCandidate, 0, len(documents))
	for _, document := range documents {
		lexicalScore, lexicalField := memorySearchLexicalMatch(queryTokens, document)
		if lexicalScore <= 0 {
			continue
		}
		candidates = append(candidates, memorySearchScoredCandidate(query, document, lexicalScore, lexicalField, 0, nil))
	}
	sortMemorySearchCandidates(candidates, func(candidate memorySearchCandidate) float64 {
		return candidate.lexicalScore
	})
	if limit > 0 && len(candidates) > limit {
		candidates = candidates[:limit]
	}
	return candidates
}

func (s *MemorySearcher) memorySearchSemanticChannel(ctx context.Context, documents []memorySearchDocument, query MemoryQuery, limit int) ([]memorySearchCandidate, error) {
	queryText := strings.TrimSpace(recallQueryText(query))
	if queryText == "" {
		return nil, nil
	}

	vectors, err := s.embedder.Embed(ctx, []string{queryText})
	if err != nil {
		return nil, err
	}
	if len(vectors) == 0 || len(vectors[0]) == 0 {
		return nil, nil
	}
	queryVector := vectors[0]

	documentVectors, err := s.ensureDocumentEmbeddings(ctx, documents)
	if err != nil {
		return nil, err
	}

	candidates := make([]memorySearchCandidate, 0, len(documents))
	for _, document := range documents {
		vector := documentVectors[document.Entry.Key]
		if len(vector) == 0 {
			continue
		}
		semanticScore := cosineSimilarity(queryVector, vector)
		if semanticScore <= 0 {
			continue
		}
		lexicalScore, lexicalField := memorySearchLexicalMatch(tokenizeRecallText(queryText), document)
		candidates = append(candidates, memorySearchScoredCandidate(query, document, lexicalScore, lexicalField, semanticScore, vector))
	}
	sortMemorySearchCandidates(candidates, func(candidate memorySearchCandidate) float64 {
		return candidate.semanticScore
	})
	if limit > 0 && len(candidates) > limit {
		candidates = candidates[:limit]
	}
	return candidates, nil
}

func (s *MemorySearcher) ensureDocumentEmbeddings(ctx context.Context, documents []memorySearchDocument) (map[string][]float32, error) {
	result := make(map[string][]float32, len(documents))
	missingTexts := make([]string, 0, len(documents))
	missingDocuments := make([]memorySearchDocument, 0, len(documents))

	for _, document := range documents {
		if strings.TrimSpace(document.semanticText) == "" {
			continue
		}
		if vector, ok := s.cachedDocumentEmbedding(document); ok {
			result[document.Entry.Key] = vector
			continue
		}
		missingTexts = append(missingTexts, document.semanticText)
		missingDocuments = append(missingDocuments, document)
	}

	if len(missingTexts) == 0 {
		return result, nil
	}

	vectors, err := s.embedder.Embed(ctx, missingTexts)
	if err != nil {
		return nil, err
	}

	for idx, document := range missingDocuments {
		if idx >= len(vectors) || len(vectors[idx]) == 0 {
			continue
		}
		vector := copyVector(vectors[idx])
		s.storeDocumentEmbedding(document, vector)
		result[document.Entry.Key] = vector
	}
	return result, nil
}

func (s *MemorySearcher) cachedDocumentEmbedding(document memorySearchDocument) ([]float32, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	entry, ok := s.entryEmbedding[document.Entry.Key]
	if !ok || entry.fingerprint != document.fingerprint {
		return nil, false
	}
	return copyVector(entry.vector), true
}

func (s *MemorySearcher) storeDocumentEmbedding(document memorySearchDocument, vector []float32) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.entryEmbedding == nil {
		s.entryEmbedding = make(map[string]memoryEmbeddingCacheEntry)
	}
	s.entryEmbedding[document.Entry.Key] = memoryEmbeddingCacheEntry{
		fingerprint: document.fingerprint,
		vector:      copyVector(vector),
	}
}

func mergeMemorySearchCandidates(query MemoryQuery, lexical, semantic []memorySearchCandidate) []memorySearchCandidate {
	merged := make(map[string]memorySearchCandidate, len(lexical)+len(semantic))
	merge := func(candidate memorySearchCandidate) {
		current, ok := merged[candidate.doc.Entry.Key]
		if !ok {
			merged[candidate.doc.Entry.Key] = candidate
			return
		}
		if candidate.lexicalScore > current.lexicalScore {
			current.lexicalScore = candidate.lexicalScore
			current.lexicalField = candidate.lexicalField
		}
		if candidate.semanticScore > current.semanticScore {
			current.semanticScore = candidate.semanticScore
			current.vector = copyVector(candidate.vector)
		}
		current.doc = candidate.doc
		merged[candidate.doc.Entry.Key] = current
	}

	for _, candidate := range lexical {
		merge(candidate)
	}
	for _, candidate := range semantic {
		merge(candidate)
	}

	out := make([]memorySearchCandidate, 0, len(merged))
	for _, candidate := range merged {
		out = append(out, memorySearchScoredCandidate(
			query,
			candidate.doc,
			candidate.lexicalScore,
			candidate.lexicalField,
			candidate.semanticScore,
			candidate.vector,
		))
	}
	sortMemorySearchCandidates(out, func(candidate memorySearchCandidate) float64 {
		return candidate.finalScore
	})
	return out
}

func memorySearchScoredCandidate(
	query MemoryQuery,
	document memorySearchDocument,
	lexicalScore float64,
	lexicalField string,
	semanticScore float64,
	vector []float32,
) memorySearchCandidate {
	queryScore, matchedDomain, jobTypeMatched := memorySearchQueryScore(document.Entry, lexicalScore, query)
	finalScore := queryScore
	if semanticScore > 0 {
		hybridScore := memorySearchLexicalWeight*lexicalScore + memorySearchSemanticWeight*semanticScore
		finalScore = clampRecallScore(0.55*queryScore + 0.45*hybridScore)
	}
	reason := memorySearchReason(document.Entry, lexicalField, semanticScore, matchedDomain, jobTypeMatched, query.JobType)
	return memorySearchCandidate{
		doc:           document,
		lexicalScore:  lexicalScore,
		lexicalField:  lexicalField,
		semanticScore: semanticScore,
		queryScore:    queryScore,
		finalScore:    finalScore,
		reason:        reason,
		vector:        copyVector(vector),
	}
}

func memorySearchQueryScore(entry MemoryEntry, lexicalScore float64, query MemoryQuery) (float64, string, bool) {
	authority := recallAuthorityScore(entry)
	recency := recallRecencyScore(entry)
	evidenceQuality := recallEvidenceQuality(entry)
	correctionPenalty := recallCorrectionPenalty(entry)
	matchedDomain := recallMatchingDomain(entry.Tags, normalizeRecallValues(query.Domains))
	jobTypeMatched := recallNamespaceMatchesJobType(entry.Namespace, query.JobType)

	score := 0.25*authority +
		0.30*lexicalScore +
		0.20*recency +
		0.15*evidenceQuality -
		0.10*correctionPenalty
	if matchedDomain != "" {
		score += 0.1
	}
	if jobTypeMatched {
		score += 0.05
	}
	return clampRecallScore(score), matchedDomain, jobTypeMatched
}

func memorySearchReason(entry MemoryEntry, lexicalField string, semanticScore float64, matchedDomain string, jobTypeMatched bool, jobType string) string {
	channelReason := "channel:lexical"
	switch {
	case semanticScore > 0 && lexicalField != "":
		channelReason = "channel:hybrid"
	case semanticScore > 0:
		channelReason = "channel:semantic"
	}

	baseReason := buildMemoryHitReason(entry, lexicalField, matchedDomain, jobTypeMatched, jobType)
	if baseReason == "" {
		return channelReason
	}
	return channelReason + ", " + baseReason
}

func memorySearchLexicalMatch(queryTokens map[string]struct{}, document memorySearchDocument) (float64, string) {
	bestScore := 0.0
	bestField := ""
	for _, field := range document.lexicalFields {
		score := recallJaccardSimilarity(queryTokens, tokenizeRecallText(field.text))
		if score > bestScore {
			bestScore = score
			bestField = field.name
		}
	}
	return bestScore, bestField
}

func sortMemorySearchCandidates(candidates []memorySearchCandidate, score func(memorySearchCandidate) float64) {
	sort.SliceStable(candidates, func(i, j int) bool {
		scoreI := score(candidates[i])
		scoreJ := score(candidates[j])
		if scoreI != scoreJ {
			return scoreI > scoreJ
		}
		if candidates[i].finalScore != candidates[j].finalScore {
			return candidates[i].finalScore > candidates[j].finalScore
		}
		if candidates[i].queryScore != candidates[j].queryScore {
			return candidates[i].queryScore > candidates[j].queryScore
		}
		return candidates[i].doc.Entry.Key < candidates[j].doc.Entry.Key
	})
}

func memorySearchCandidatesToHits(candidates []memorySearchCandidate) []MemoryHit {
	hits := make([]MemoryHit, 0, len(candidates))
	for _, candidate := range candidates {
		hits = append(hits, MemoryHit{
			Entry:          candidate.doc.Entry,
			RelevanceScore: candidate.finalScore,
			Reason:         candidate.reason,
		})
	}
	return hits
}

func mmrRerankCandidates(candidates []memorySearchCandidate, lambda float64, limit int) []MemoryHit {
	if len(candidates) == 0 {
		return nil
	}
	if lambda <= 0 {
		lambda = memorySearchMMRLambda
	}
	if lambda > 1 {
		lambda = 1
	}
	if limit <= 0 || limit > len(candidates) {
		limit = len(candidates)
	}

	selected := make([]memorySearchCandidate, 0, limit)
	used := make(map[string]struct{}, limit)

	for len(selected) < limit {
		bestIdx := -1
		bestMMR := math.Inf(-1)

		for idx, candidate := range candidates {
			if _, ok := used[candidate.doc.Entry.Key]; ok {
				continue
			}

			maxSimilarity := 0.0
			for _, picked := range selected {
				similarity := memorySearchCandidateSimilarity(candidate, picked)
				if similarity > maxSimilarity {
					maxSimilarity = similarity
				}
			}

			score := lambda*candidate.finalScore - (1-lambda)*maxSimilarity
			if score > bestMMR {
				bestIdx = idx
				bestMMR = score
				continue
			}
			if score == bestMMR && bestIdx >= 0 {
				if candidate.finalScore > candidates[bestIdx].finalScore {
					bestIdx = idx
					continue
				}
				if candidate.finalScore == candidates[bestIdx].finalScore && candidate.doc.Entry.Key < candidates[bestIdx].doc.Entry.Key {
					bestIdx = idx
				}
			}
		}

		if bestIdx < 0 {
			break
		}
		chosen := candidates[bestIdx]
		used[chosen.doc.Entry.Key] = struct{}{}
		selected = append(selected, chosen)
	}

	return memorySearchCandidatesToHits(selected)
}

func memorySearchCandidateSimilarity(left, right memorySearchCandidate) float64 {
	if len(left.vector) > 0 && len(right.vector) > 0 {
		if similarity := cosineSimilarity(left.vector, right.vector); similarity > 0 {
			return similarity
		}
	}
	return recallJaccardSimilarity(
		tokenizeRecallText(memorySearchSimilarityText(left.doc)),
		tokenizeRecallText(memorySearchSimilarityText(right.doc)),
	)
}

func memorySearchSimilarityText(document memorySearchDocument) string {
	if strings.TrimSpace(document.semanticText) != "" {
		return document.semanticText
	}
	return document.Entry.Value
}

func newMemorySearchDocument(entry MemoryEntry, evidenceSummary string) memorySearchDocument {
	fields := make([]memorySearchField, 0, 5)
	appendField := func(name, text string) {
		if strings.TrimSpace(text) == "" {
			return
		}
		fields = append(fields, memorySearchField{name: name, text: text})
	}

	appendField("field", entry.Field)
	appendField("label", entry.Label)
	appendField("value", entry.Value)
	appendField("tags", strings.Join(entry.Tags, " "))
	appendField("evidence", evidenceSummary)

	semanticParts := make([]string, 0, 2)
	if value := strings.TrimSpace(entry.Value); value != "" {
		semanticParts = append(semanticParts, value)
	}
	if summary := strings.TrimSpace(evidenceSummary); summary != "" {
		semanticParts = append(semanticParts, summary)
	}
	if len(semanticParts) == 0 {
		if label := strings.TrimSpace(entry.Label); label != "" {
			semanticParts = append(semanticParts, label)
		}
	}
	if len(semanticParts) == 0 {
		semanticParts = append(semanticParts, entry.Key)
	}

	semanticText := strings.Join(semanticParts, " ")
	return memorySearchDocument{
		Entry:         entry,
		lexicalFields: fields,
		semanticText:  semanticText,
		fingerprint: strings.Join([]string{
			entry.Key,
			semanticText,
			entry.UpdatedAt.UTC().Format(time.RFC3339Nano),
		}, "\x1f"),
	}
}

func firstMemoryEvidenceSummary(items []durablefact.Evidence) string {
	for _, item := range items {
		if summary := strings.TrimSpace(item.Summary); summary != "" {
			return summary
		}
		if value := strings.TrimSpace(item.Value); value != "" {
			return value
		}
	}
	return ""
}

func (s *SQLiteKVStore) memorySearchDocuments(ctx context.Context) ([]memorySearchDocument, error) {
	facts, err := s.facts.List(ctx, durablefact.Filter{ViewType: durablefact.ViewTypeContext})
	if err != nil {
		return nil, fmt.Errorf("list memory search documents: %w", err)
	}
	documents := make([]memorySearchDocument, 0, len(facts))
	for _, fact := range facts {
		entry, ok := memoryEntryFromFact(fact)
		if !ok {
			continue
		}
		documents = append(documents, newMemorySearchDocument(entry, firstMemoryEvidenceSummary(fact.Evidence)))
	}
	return documents, nil
}

func (s *GovernedMemoryStore) memorySearchDocuments(ctx context.Context) ([]memorySearchDocument, error) {
	if s == nil || s.inner == nil {
		return nil, nil
	}
	if provider, ok := s.inner.(memorySearchDocumentSource); ok {
		return provider.memorySearchDocuments(ctx)
	}
	entries, err := s.List(ctx)
	if err != nil {
		return nil, err
	}
	documents := make([]memorySearchDocument, 0, len(entries))
	for _, entry := range entries {
		documents = append(documents, newMemorySearchDocument(entry, ""))
	}
	return documents, nil
}

func (s *MirroredMemoryStore) memorySearchDocuments(ctx context.Context) ([]memorySearchDocument, error) {
	if s == nil || s.inner == nil {
		return nil, nil
	}
	if provider, ok := s.inner.(memorySearchDocumentSource); ok {
		return provider.memorySearchDocuments(ctx)
	}
	entries, err := s.List(ctx)
	if err != nil {
		return nil, err
	}
	documents := make([]memorySearchDocument, 0, len(entries))
	for _, entry := range entries {
		documents = append(documents, newMemorySearchDocument(entry, ""))
	}
	return documents, nil
}

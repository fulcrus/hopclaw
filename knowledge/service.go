package knowledge

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/fulcrus/hopclaw/agent"
)

const (
	defaultSearchLimit       = 8
	searchEmbeddingBatch     = 32
	minKeywordScoreToInclude = 0.1
)

type Service struct {
	store      Store
	embedding  agent.EmbeddingClient
	connectors map[SourceKind]Connector
}

func NewService(store Store, embedding agent.EmbeddingClient) (*Service, error) {
	if store == nil {
		return nil, fmt.Errorf("knowledge store is required")
	}
	return &Service{
		store:      store,
		embedding:  embedding,
		connectors: DefaultConnectors(),
	}, nil
}

func (s *Service) ListSources(ctx context.Context) ([]Source, error) {
	return s.store.ListSources(ctx)
}

func (s *Service) GetSource(ctx context.Context, id string) (*Source, error) {
	return s.store.GetSource(ctx, id)
}

func (s *Service) UpsertSource(ctx context.Context, source Source) (Source, error) {
	now := time.Now().UTC()
	normalized, err := NormalizeSource(source)
	if err != nil {
		return Source{}, err
	}
	source = normalized
	source.ID = strings.TrimSpace(source.ID)
	source.Locale = normalizeLocale(source.Locale)
	if source.ID == "" {
		source.ID = newSourceID(source.Kind, source.Name)
	}
	if source.CreatedAt.IsZero() {
		if existing, _ := s.store.GetSource(ctx, source.ID); existing != nil {
			source.CreatedAt = existing.CreatedAt
			if source.Stats == (SourceStats{}) {
				source.Stats = existing.Stats
			}
			if source.LastSyncAt.IsZero() {
				source.LastSyncAt = existing.LastSyncAt
			}
			if strings.TrimSpace(source.LastError) == "" {
				source.LastError = existing.LastError
			}
			if source.Status == "" {
				source.Status = existing.Status
			}
			if strings.TrimSpace(source.SyncCursor) == "" {
				source.SyncCursor = existing.SyncCursor
			}
			if source.Locale == "" {
				source.Locale = existing.Locale
			}
		} else {
			source.CreatedAt = now
		}
	}
	source.UpdatedAt = now
	if source.Status == "" {
		source.Status = SourceStatusReady
	}
	return s.store.UpsertSource(ctx, source)
}

func (s *Service) DeleteSource(ctx context.Context, id string) error {
	return s.store.DeleteSource(ctx, strings.TrimSpace(id))
}

func (s *Service) SyncSource(ctx context.Context, id string) (*SyncResult, error) {
	source, err := s.store.GetSource(ctx, id)
	if err != nil {
		return nil, err
	}
	if source == nil {
		return nil, fmt.Errorf("knowledge source %q not found", id)
	}
	if !source.Enabled {
		return nil, fmt.Errorf("knowledge source %q is disabled", id)
	}
	connector, ok := s.connectors[source.Kind]
	if !ok {
		return nil, fmt.Errorf("knowledge connector %q is not available", source.Kind)
	}

	now := time.Now().UTC()
	source.Status = SourceStatusSyncing
	source.UpdatedAt = now
	if _, err := s.store.UpsertSource(ctx, *source); err != nil {
		return nil, err
	}

	snapshots, syncErr := connector.Sync(ctx, *source)
	if syncErr != nil {
		source.Status = SourceStatusDegraded
		source.LastError = syncErr.Error()
		source.UpdatedAt = time.Now().UTC()
		if _, err := s.store.UpsertSource(ctx, *source); err != nil {
			return nil, err
		}
		return nil, syncErr
	}
	sort.Slice(snapshots, func(i, j int) bool {
		left := snapshots[i].Document
		right := snapshots[j].Document
		if left.Path != right.Path {
			return left.Path < right.Path
		}
		if left.Title != right.Title {
			return left.Title < right.Title
		}
		return left.ID < right.ID
	})

	existingDocuments, err := s.store.ListDocuments(ctx, source.ID)
	if err != nil {
		return nil, err
	}
	existingByID := make(map[string]Document, len(existingDocuments))
	for _, item := range existingDocuments {
		existingByID[item.ID] = item
	}

	seen := make(map[string]struct{}, len(snapshots))
	for _, snapshot := range snapshots {
		document, chunks, changed, err := s.prepareDocumentSync(*source, snapshot, existingByID, now)
		if err != nil {
			source.Status = SourceStatusDegraded
			source.LastError = err.Error()
			source.UpdatedAt = time.Now().UTC()
			if _, updateErr := s.store.UpsertSource(ctx, *source); updateErr != nil {
				return nil, updateErr
			}
			return nil, err
		}
		seen[document.ID] = struct{}{}
		if !changed {
			continue
		}
		if err := s.store.UpsertDocument(ctx, document, chunks); err != nil {
			source.Status = SourceStatusDegraded
			source.LastError = err.Error()
			source.UpdatedAt = time.Now().UTC()
			if _, updateErr := s.store.UpsertSource(ctx, *source); updateErr != nil {
				return nil, updateErr
			}
			return nil, err
		}
		if err := s.projectChunkVectors(ctx, document, chunks); err != nil {
			source.Status = SourceStatusDegraded
			source.LastError = err.Error()
			source.UpdatedAt = time.Now().UTC()
			if _, updateErr := s.store.UpsertSource(ctx, *source); updateErr != nil {
				return nil, updateErr
			}
			return nil, err
		}
	}

	removed := make([]string, 0)
	for _, item := range existingDocuments {
		if _, ok := seen[item.ID]; ok {
			continue
		}
		removed = append(removed, item.ID)
	}
	if err := s.store.DeleteDocuments(ctx, source.ID, removed); err != nil {
		source.Status = SourceStatusDegraded
		source.LastError = err.Error()
		source.UpdatedAt = time.Now().UTC()
		if _, updateErr := s.store.UpsertSource(ctx, *source); updateErr != nil {
			return nil, updateErr
		}
		return nil, err
	}

	stats, err := s.store.ComputeSourceStats(ctx, source.ID)
	if err != nil {
		return nil, err
	}
	source.Status = SourceStatusReady
	source.LastError = ""
	source.LastSyncAt = time.Now().UTC()
	source.SyncCursor = source.LastSyncAt.Format(time.RFC3339Nano)
	source.UpdatedAt = source.LastSyncAt
	source.Stats = stats
	if out, err := s.store.UpsertSource(ctx, *source); err == nil {
		*source = out
	} else {
		return nil, err
	}
	return &SyncResult{Source: *source, Stats: stats}, nil
}

func (s *Service) prepareDocumentSync(source Source, snapshot DocumentSnapshot, existingByID map[string]Document, now time.Time) (Document, []Chunk, bool, error) {
	content := normalizeChunkContent(snapshot.Content)
	document := snapshot.Document
	document.SourceID = source.ID
	document.Locale = normalizeLocale(firstNonEmpty(document.Locale, source.Locale))
	if strings.TrimSpace(document.ID) == "" {
		return Document{}, nil, false, fmt.Errorf("knowledge document id is required")
	}
	document.ID = strings.TrimSpace(document.ID)
	if document.Kind == "" {
		document.Kind = defaultDocumentKind(source.Kind)
	}
	if document.Title == "" {
		document.Title = firstNonEmpty(filepath.Base(document.Path), document.ID)
	}
	if document.Path == "" {
		document.Path = document.ID
	}
	if document.SourceUpdatedAt.IsZero() {
		document.SourceUpdatedAt = now
	}
	document = populateDocumentMetadata(document, content)
	chunks := buildDocumentChunks(document, content)
	document.ChunkCount = len(chunks)
	document.SyncedAt = now

	existing, ok := existingByID[document.ID]
	if ok && !documentChanged(&existing, document) {
		return existing, nil, false, nil
	}
	return document, chunks, true, nil
}

func (s *Service) projectChunkVectors(ctx context.Context, document Document, chunks []Chunk) error {
	if s.embedding == nil || len(chunks) == 0 {
		return nil
	}
	texts := make([]string, 0, len(chunks))
	order := make([]Chunk, 0, len(chunks))
	for _, chunk := range chunks {
		text := strings.TrimSpace(chunk.Title + "\n" + chunk.Content)
		if text == "" {
			continue
		}
		texts = append(texts, text)
		order = append(order, chunk)
	}
	for start := 0; start < len(texts); start += searchEmbeddingBatch {
		end := start + searchEmbeddingBatch
		if end > len(texts) {
			end = len(texts)
		}
		vectors, err := s.embedding.Embed(ctx, texts[start:end])
		if err != nil {
			return fmt.Errorf("embed knowledge chunks for %q [%d:%d]: %w", document.ID, start, end, err)
		}
		projected := make([]ChunkVector, 0, len(vectors))
		for i, vector := range vectors {
			chunk := order[start+i]
			projected = append(projected, ChunkVector{
				ChunkID:     chunk.ID,
				SourceID:    chunk.SourceID,
				DocumentID:  chunk.DocumentID,
				Locale:      chunk.Locale,
				ContentHash: chunk.Hash,
				Vector:      append([]float32(nil), vector...),
				ProjectedAt: time.Now().UTC(),
			})
		}
		if err := s.store.UpsertChunkVectors(ctx, projected); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) Search(ctx context.Context, filter SearchFilter) ([]SearchResult, error) {
	query := strings.TrimSpace(filter.Query)
	if query == "" {
		return nil, fmt.Errorf("query is required")
	}
	limit := filter.Limit
	if limit <= 0 {
		limit = defaultSearchLimit
	}
	filter.Query = query
	filter.Limit = limit
	filter.Locale = normalizeLocale(firstNonEmpty(filter.Locale, detectLocale(query)))

	keywordResults, err := s.store.SearchText(ctx, filter, filter.Locale)
	if err != nil {
		return nil, err
	}
	semanticResults, err := s.searchSemantic(ctx, filter)
	if err != nil {
		return nil, err
	}
	merged := mergeSearchResults(keywordResults, semanticResults)
	if len(merged) > limit {
		merged = merged[:limit]
	}
	return merged, nil
}

func (s *Service) searchSemantic(ctx context.Context, filter SearchFilter) ([]SearchResult, error) {
	if s.embedding == nil {
		return nil, nil
	}
	queryVectors, err := s.embedding.Embed(ctx, []string{filter.Query})
	if err != nil || len(queryVectors) == 0 {
		return nil, nil
	}
	chunkVectors, err := s.store.ListChunkVectors(ctx, filter.SourceID)
	if err != nil {
		return nil, err
	}
	if len(chunkVectors) == 0 {
		return nil, nil
	}

	var chunks []Chunk
	if strings.TrimSpace(filter.SourceID) != "" {
		chunks, err = s.store.ListChunks(ctx, filter.SourceID)
	} else {
		chunks, err = s.store.ListAllChunks(ctx)
	}
	if err != nil {
		return nil, err
	}
	chunksByID := make(map[string]Chunk, len(chunks))
	for _, chunk := range chunks {
		chunksByID[chunk.ID] = chunk
	}
	sources, err := s.store.ListSources(ctx)
	if err != nil {
		return nil, err
	}
	sourcesByID := make(map[string]Source, len(sources))
	for _, source := range sources {
		sourcesByID[source.ID] = source
	}

	results := make([]SearchResult, 0, len(chunkVectors))
	for _, item := range chunkVectors {
		chunk, ok := chunksByID[item.ChunkID]
		if !ok {
			continue
		}
		source, ok := sourcesByID[chunk.SourceID]
		if !ok || !source.Enabled {
			continue
		}
		if strings.TrimSpace(filter.SourceID) != "" && chunk.SourceID != strings.TrimSpace(filter.SourceID) {
			continue
		}
		score := cosineSimilarity32(queryVectors[0], item.Vector) + localeMatchBoost(filter.Locale, chunk.Locale)
		if score <= 0 {
			continue
		}
		results = append(results, SearchResult{
			ChunkID:      chunk.ID,
			SourceID:     chunk.SourceID,
			SourceName:   source.Name,
			SourceKind:   source.Kind,
			DocumentID:   chunk.DocumentID,
			Title:        chunk.Title,
			Path:         chunk.Path,
			URI:          chunk.URI,
			Locale:       chunk.Locale,
			Preview:      chunk.Preview,
			Score:        score,
			KeywordScore: 0,
			UpdatedAt:    chunk.UpdatedAt,
		})
	}
	sortSearchResults(results)
	maxResults := filter.Limit * 2
	if maxResults < filter.Limit {
		maxResults = filter.Limit
	}
	if len(results) > maxResults {
		results = results[:maxResults]
	}
	return results, nil
}

func mergeSearchResults(keyword, semantic []SearchResult) []SearchResult {
	seen := make(map[string]SearchResult, len(keyword)+len(semantic))
	for _, item := range semantic {
		seen[item.ChunkID] = item
	}
	for _, item := range keyword {
		if existing, ok := seen[item.ChunkID]; ok {
			existing.KeywordScore = item.KeywordScore
			existing.Score = existing.Score + item.KeywordScore
			if strings.TrimSpace(existing.Preview) == "" {
				existing.Preview = item.Preview
			}
			if strings.TrimSpace(existing.Locale) == "" {
				existing.Locale = item.Locale
			}
			seen[item.ChunkID] = existing
			continue
		}
		seen[item.ChunkID] = item
	}
	out := make([]SearchResult, 0, len(seen))
	for _, item := range seen {
		out = append(out, item)
	}
	sortSearchResults(out)
	return out
}

func extractQueryPreview(content, query string) string {
	content = normalizeChunkContent(content)
	lower := strings.ToLower(content)
	idx := strings.Index(lower, strings.ToLower(query))
	if idx < 0 {
		return buildPreview(content)
	}
	start := idx - 80
	if start < 0 {
		start = 0
	}
	end := idx + len(query) + 140
	if end > len(content) {
		end = len(content)
	}
	preview := strings.TrimSpace(content[start:end])
	if start > 0 {
		preview = "…" + preview
	}
	if end < len(content) {
		preview += "…"
	}
	return preview
}

func newSourceID(kind SourceKind, name string) string {
	base := strings.ToLower(strings.TrimSpace(name))
	base = strings.ReplaceAll(base, " ", "-")
	base = strings.ReplaceAll(base, "_", "-")
	if base == "" {
		base = "source"
	}
	return string(kind) + "-" + base
}

func defaultDocumentKind(kind SourceKind) DocumentKind {
	switch kind {
	case SourceKindLocalDir, SourceKindGitRepo:
		return DocumentKindFile
	case SourceKindWebURLs:
		return DocumentKindWebPage
	default:
		return DocumentKindRemoteDoc
	}
}

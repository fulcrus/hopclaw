package knowledge

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	storepkg "github.com/fulcrus/hopclaw/store"
)

const knowledgeStoreTimeFmt = time.RFC3339Nano

type SQLiteStore struct {
	db *sql.DB
}

func NewSQLiteStore(path string) (*SQLiteStore, error) {
	db, err := storepkg.OpenDB(path)
	if err != nil {
		return nil, fmt.Errorf("open knowledge sqlite store: %w", err)
	}
	store, err := NewSQLiteStoreFromDB(db)
	if err != nil {
		_ = db.Close()
		return nil, err
	}
	return store, nil
}

func NewSQLiteStoreFromDB(db *sql.DB) (*SQLiteStore, error) {
	if db == nil {
		return nil, fmt.Errorf("knowledge sqlite db is required")
	}
	store := &SQLiteStore{db: db}
	if err := store.ensureSchema(); err != nil {
		return nil, err
	}
	return store, nil
}

func (s *SQLiteStore) ensureSchema() error {
	if s == nil || s.db == nil {
		return fmt.Errorf("knowledge sqlite store is not initialized")
	}
	_, err := s.db.Exec(`
CREATE TABLE IF NOT EXISTS knowledge_sources (
	id TEXT PRIMARY KEY,
	name TEXT NOT NULL,
	kind TEXT NOT NULL,
	enabled INTEGER NOT NULL DEFAULT 1,
	locale TEXT NOT NULL DEFAULT '',
	path TEXT NOT NULL DEFAULT '',
	urls_json TEXT NOT NULL DEFAULT '[]',
	config_json TEXT NOT NULL DEFAULT '{}',
	include_globs_json TEXT NOT NULL DEFAULT '[]',
	exclude_globs_json TEXT NOT NULL DEFAULT '[]',
	status TEXT NOT NULL DEFAULT '',
	last_sync_at TEXT NOT NULL DEFAULT '',
	last_error TEXT NOT NULL DEFAULT '',
	sync_cursor TEXT NOT NULL DEFAULT '',
	stats_documents INTEGER NOT NULL DEFAULT 0,
	stats_chunks INTEGER NOT NULL DEFAULT 0,
	stats_bytes INTEGER NOT NULL DEFAULT 0,
	created_at TEXT NOT NULL DEFAULT '',
	updated_at TEXT NOT NULL DEFAULT '',
	connector_note TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_knowledge_sources_name ON knowledge_sources(name, id);

CREATE TABLE IF NOT EXISTS knowledge_documents (
	id TEXT NOT NULL,
	source_id TEXT NOT NULL REFERENCES knowledge_sources(id) ON DELETE CASCADE,
	kind TEXT NOT NULL DEFAULT '',
	title TEXT NOT NULL DEFAULT '',
	path TEXT NOT NULL DEFAULT '',
	uri TEXT NOT NULL DEFAULT '',
	locale TEXT NOT NULL DEFAULT '',
	content_hash TEXT NOT NULL DEFAULT '',
	bytes INTEGER NOT NULL DEFAULT 0,
	chunk_count INTEGER NOT NULL DEFAULT 0,
	source_updated_at TEXT NOT NULL DEFAULT '',
	synced_at TEXT NOT NULL DEFAULT '',
	metadata_json TEXT NOT NULL DEFAULT '{}',
	PRIMARY KEY (source_id, id)
);
CREATE INDEX IF NOT EXISTS idx_knowledge_documents_source ON knowledge_documents(source_id, path, id);

CREATE TABLE IF NOT EXISTS knowledge_chunks (
	id TEXT PRIMARY KEY,
	source_id TEXT NOT NULL REFERENCES knowledge_sources(id) ON DELETE CASCADE,
	document_id TEXT NOT NULL,
	ordinal INTEGER NOT NULL DEFAULT 0,
	title TEXT NOT NULL DEFAULT '',
	path TEXT NOT NULL DEFAULT '',
	uri TEXT NOT NULL DEFAULT '',
	locale TEXT NOT NULL DEFAULT '',
	content TEXT NOT NULL DEFAULT '',
	preview TEXT NOT NULL DEFAULT '',
	hash TEXT NOT NULL DEFAULT '',
	bytes INTEGER NOT NULL DEFAULT 0,
	start_rune INTEGER NOT NULL DEFAULT 0,
	end_rune INTEGER NOT NULL DEFAULT 0,
	updated_at TEXT NOT NULL DEFAULT '',
	metadata_json TEXT NOT NULL DEFAULT '{}',
	FOREIGN KEY (source_id, document_id) REFERENCES knowledge_documents(source_id, id) ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS idx_knowledge_chunks_source ON knowledge_chunks(source_id, document_id, ordinal, id);
CREATE INDEX IF NOT EXISTS idx_knowledge_chunks_document ON knowledge_chunks(document_id, ordinal, id);
CREATE INDEX IF NOT EXISTS idx_knowledge_chunks_locale ON knowledge_chunks(locale, id);

CREATE VIRTUAL TABLE IF NOT EXISTS knowledge_chunk_fts USING fts5(
	chunk_id UNINDEXED,
	source_id UNINDEXED,
	document_id UNINDEXED,
	locale UNINDEXED,
	title,
	content,
	tokenize='unicode61 remove_diacritics 2'
);

CREATE TABLE IF NOT EXISTS knowledge_chunk_vectors (
	chunk_id TEXT PRIMARY KEY REFERENCES knowledge_chunks(id) ON DELETE CASCADE,
	source_id TEXT NOT NULL DEFAULT '',
	document_id TEXT NOT NULL DEFAULT '',
	locale TEXT NOT NULL DEFAULT '',
	model TEXT NOT NULL DEFAULT '',
	content_hash TEXT NOT NULL DEFAULT '',
	vector_json TEXT NOT NULL DEFAULT '[]',
	projected_at TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_knowledge_chunk_vectors_source ON knowledge_chunk_vectors(source_id, chunk_id);
`)
	if err != nil {
		return fmt.Errorf("ensure knowledge schema: %w", err)
	}
	return nil
}

func (s *SQLiteStore) ListSources(ctx context.Context) ([]Source, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT id, name, kind, enabled, locale, path, urls_json, config_json, include_globs_json, exclude_globs_json,
       status, last_sync_at, last_error, sync_cursor, stats_documents, stats_chunks, stats_bytes,
       created_at, updated_at, connector_note
FROM knowledge_sources
ORDER BY name, id`)
	if err != nil {
		return nil, fmt.Errorf("list knowledge sources: %w", err)
	}
	defer rows.Close()

	out := make([]Source, 0)
	for rows.Next() {
		item, err := scanSource(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list knowledge sources: %w", err)
	}
	return out, nil
}

func (s *SQLiteStore) GetSource(ctx context.Context, id string) (*Source, error) {
	row := s.db.QueryRowContext(ctx, `
SELECT id, name, kind, enabled, locale, path, urls_json, config_json, include_globs_json, exclude_globs_json,
       status, last_sync_at, last_error, sync_cursor, stats_documents, stats_chunks, stats_bytes,
       created_at, updated_at, connector_note
FROM knowledge_sources
WHERE id = ?`, strings.TrimSpace(id))
	item, err := scanSource(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &item, nil
}

func (s *SQLiteStore) UpsertSource(ctx context.Context, source Source) (Source, error) {
	if _, err := s.db.ExecContext(ctx, `
INSERT INTO knowledge_sources (
	id, name, kind, enabled, locale, path, urls_json, config_json, include_globs_json, exclude_globs_json,
	status, last_sync_at, last_error, sync_cursor, stats_documents, stats_chunks, stats_bytes,
	created_at, updated_at, connector_note
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(id) DO UPDATE SET
	name = excluded.name,
	kind = excluded.kind,
	enabled = excluded.enabled,
	locale = excluded.locale,
	path = excluded.path,
	urls_json = excluded.urls_json,
	config_json = excluded.config_json,
	include_globs_json = excluded.include_globs_json,
	exclude_globs_json = excluded.exclude_globs_json,
	status = excluded.status,
	last_sync_at = excluded.last_sync_at,
	last_error = excluded.last_error,
	sync_cursor = excluded.sync_cursor,
	stats_documents = excluded.stats_documents,
	stats_chunks = excluded.stats_chunks,
	stats_bytes = excluded.stats_bytes,
	created_at = excluded.created_at,
	updated_at = excluded.updated_at,
	connector_note = excluded.connector_note`,
		source.ID,
		source.Name,
		string(source.Kind),
		boolToInt(source.Enabled),
		strings.TrimSpace(source.Locale),
		source.Path,
		marshalStringSlice(source.URLs),
		marshalMap(source.Config),
		marshalStringSlice(source.IncludeGlobs),
		marshalStringSlice(source.ExcludeGlobs),
		string(source.Status),
		formatKnowledgeTime(source.LastSyncAt),
		source.LastError,
		strings.TrimSpace(source.SyncCursor),
		source.Stats.Documents,
		source.Stats.Chunks,
		source.Stats.Bytes,
		formatKnowledgeTime(source.CreatedAt),
		formatKnowledgeTime(source.UpdatedAt),
		source.ConnectorNote,
	); err != nil {
		return Source{}, fmt.Errorf("upsert knowledge source %q: %w", source.ID, err)
	}
	return source, nil
}

func (s *SQLiteStore) DeleteSource(ctx context.Context, id string) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("delete knowledge source %q: %w", id, err)
	}
	defer rollbackKnowledgeTx(tx)

	if _, err := tx.ExecContext(ctx, `DELETE FROM knowledge_chunk_fts WHERE source_id = ?`, id); err != nil {
		return fmt.Errorf("delete knowledge source %q fts: %w", id, err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM knowledge_sources WHERE id = ?`, id); err != nil {
		return fmt.Errorf("delete knowledge source %q: %w", id, err)
	}
	return tx.Commit()
}

func (s *SQLiteStore) ListDocuments(ctx context.Context, sourceID string) ([]Document, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT id, source_id, kind, title, path, uri, locale, content_hash, bytes, chunk_count, source_updated_at, synced_at, metadata_json
FROM knowledge_documents
WHERE source_id = ?
ORDER BY path, title, id`, strings.TrimSpace(sourceID))
	if err != nil {
		return nil, fmt.Errorf("list knowledge documents for %q: %w", sourceID, err)
	}
	defer rows.Close()

	out := make([]Document, 0)
	for rows.Next() {
		item, err := scanDocument(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list knowledge documents for %q: %w", sourceID, err)
	}
	return out, nil
}

func (s *SQLiteStore) UpsertDocument(ctx context.Context, document Document, chunks []Chunk) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("upsert knowledge document %q/%q: %w", document.SourceID, document.ID, err)
	}
	defer rollbackKnowledgeTx(tx)

	chunkIDs, err := existingChunkIDs(ctx, tx, document.SourceID, document.ID)
	if err != nil {
		return err
	}
	if err := deleteChunkProjections(ctx, tx, chunkIDs); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
DELETE FROM knowledge_chunks
WHERE source_id = ? AND document_id = ?`, document.SourceID, document.ID); err != nil {
		return fmt.Errorf("delete prior chunks for %q/%q: %w", document.SourceID, document.ID, err)
	}

	if _, err := tx.ExecContext(ctx, `
INSERT INTO knowledge_documents (
	id, source_id, kind, title, path, uri, locale, content_hash, bytes, chunk_count, source_updated_at, synced_at, metadata_json
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(source_id, id) DO UPDATE SET
	kind = excluded.kind,
	title = excluded.title,
	path = excluded.path,
	uri = excluded.uri,
	locale = excluded.locale,
	content_hash = excluded.content_hash,
	bytes = excluded.bytes,
	chunk_count = excluded.chunk_count,
	source_updated_at = excluded.source_updated_at,
	synced_at = excluded.synced_at,
	metadata_json = excluded.metadata_json`,
		document.ID,
		document.SourceID,
		string(document.Kind),
		document.Title,
		document.Path,
		document.URI,
		document.Locale,
		document.ContentHash,
		document.Bytes,
		document.ChunkCount,
		formatKnowledgeTime(document.SourceUpdatedAt),
		formatKnowledgeTime(document.SyncedAt),
		marshalJSON(document.Metadata),
	); err != nil {
		return fmt.Errorf("upsert knowledge document %q/%q: %w", document.SourceID, document.ID, err)
	}

	for _, chunk := range chunks {
		if _, err := tx.ExecContext(ctx, `
INSERT INTO knowledge_chunks (
	id, source_id, document_id, ordinal, title, path, uri, locale, content, preview, hash, bytes, start_rune, end_rune, updated_at, metadata_json
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			chunk.ID,
			chunk.SourceID,
			chunk.DocumentID,
			chunk.Ordinal,
			chunk.Title,
			chunk.Path,
			chunk.URI,
			chunk.Locale,
			chunk.Content,
			chunk.Preview,
			chunk.Hash,
			chunk.Bytes,
			chunk.StartRune,
			chunk.EndRune,
			formatKnowledgeTime(chunk.UpdatedAt),
			marshalJSON(chunk.Metadata),
		); err != nil {
			return fmt.Errorf("insert knowledge chunk %q: %w", chunk.ID, err)
		}
		if _, err := tx.ExecContext(ctx, `
INSERT INTO knowledge_chunk_fts (chunk_id, source_id, document_id, locale, title, content)
VALUES (?, ?, ?, ?, ?, ?)`,
			chunk.ID,
			chunk.SourceID,
			chunk.DocumentID,
			chunk.Locale,
			chunk.Title,
			chunk.Content,
		); err != nil {
			return fmt.Errorf("insert knowledge fts chunk %q: %w", chunk.ID, err)
		}
	}
	return tx.Commit()
}

func (s *SQLiteStore) DeleteDocuments(ctx context.Context, sourceID string, documentIDs []string) error {
	if len(documentIDs) == 0 {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("delete knowledge documents for %q: %w", sourceID, err)
	}
	defer rollbackKnowledgeTx(tx)

	for _, documentID := range documentIDs {
		documentID = strings.TrimSpace(documentID)
		if documentID == "" {
			continue
		}
		chunkIDs, err := existingChunkIDs(ctx, tx, sourceID, documentID)
		if err != nil {
			return err
		}
		if err := deleteChunkProjections(ctx, tx, chunkIDs); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `
DELETE FROM knowledge_documents
WHERE source_id = ? AND id = ?`, sourceID, documentID); err != nil {
			return fmt.Errorf("delete knowledge document %q/%q: %w", sourceID, documentID, err)
		}
	}
	return tx.Commit()
}

func (s *SQLiteStore) ComputeSourceStats(ctx context.Context, sourceID string) (SourceStats, error) {
	var stats SourceStats
	row := s.db.QueryRowContext(ctx, `
SELECT COUNT(*), COALESCE(SUM(chunk_count), 0), COALESCE(SUM(bytes), 0)
FROM knowledge_documents
WHERE source_id = ?`, strings.TrimSpace(sourceID))
	if err := row.Scan(&stats.Documents, &stats.Chunks, &stats.Bytes); err != nil {
		return SourceStats{}, fmt.Errorf("compute source stats for %q: %w", sourceID, err)
	}
	return stats, nil
}

func (s *SQLiteStore) ListChunks(ctx context.Context, sourceID string) ([]Chunk, error) {
	return s.listChunks(ctx, strings.TrimSpace(sourceID), true)
}

func (s *SQLiteStore) ListAllChunks(ctx context.Context) ([]Chunk, error) {
	return s.listChunks(ctx, "", false)
}

func (s *SQLiteStore) listChunks(ctx context.Context, sourceID string, scoped bool) ([]Chunk, error) {
	query := `
SELECT id, source_id, document_id, ordinal, title, path, uri, locale, content, preview, hash, bytes, start_rune, end_rune, updated_at, metadata_json
FROM knowledge_chunks`
	args := []any{}
	if scoped {
		query += ` WHERE source_id = ?`
		args = append(args, sourceID)
	}
	query += ` ORDER BY source_id, path, ordinal, id`
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list knowledge chunks: %w", err)
	}
	defer rows.Close()

	out := make([]Chunk, 0)
	for rows.Next() {
		item, err := scanChunk(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list knowledge chunks: %w", err)
	}
	return out, nil
}

func (s *SQLiteStore) SearchText(ctx context.Context, filter SearchFilter, queryLocale string) ([]SearchResult, error) {
	queryText := strings.TrimSpace(filter.Query)
	if queryText == "" {
		return nil, nil
	}
	limit := filter.Limit
	if limit <= 0 {
		limit = defaultSearchLimit
	}
	scope := strings.TrimSpace(filter.SourceID)
	args := []any{buildFTSQuery(queryText)}
	where := []string{`knowledge_chunk_fts MATCH ?`, `sources.enabled = 1`}
	if scope != "" {
		where = append(where, `chunks.source_id = ?`)
		args = append(args, scope)
	}
	sqlLimit := limit * 4
	if sqlLimit < limit {
		sqlLimit = limit
	}
	args = append(args, sqlLimit)

	rows, err := s.db.QueryContext(ctx, `
SELECT chunks.id, chunks.source_id, sources.name, sources.kind, chunks.document_id, chunks.title, chunks.path, chunks.uri,
       chunks.locale, chunks.preview, chunks.updated_at, bm25(knowledge_chunk_fts)
FROM knowledge_chunk_fts
JOIN knowledge_chunks AS chunks ON chunks.id = knowledge_chunk_fts.chunk_id
JOIN knowledge_sources AS sources ON sources.id = chunks.source_id
WHERE `+strings.Join(where, " AND ")+`
ORDER BY bm25(knowledge_chunk_fts), chunks.updated_at DESC
LIMIT ?`, args...)
	if err == nil {
		defer rows.Close()
		results := make([]SearchResult, 0)
		for rows.Next() {
			var item SearchResult
			var rawKind string
			var updatedAt string
			var rawRank float64
			if err := rows.Scan(
				&item.ChunkID,
				&item.SourceID,
				&item.SourceName,
				&rawKind,
				&item.DocumentID,
				&item.Title,
				&item.Path,
				&item.URI,
				&item.Locale,
				&item.Preview,
				&updatedAt,
				&rawRank,
			); err != nil {
				return nil, fmt.Errorf("search knowledge text: %w", err)
			}
			item.SourceKind = SourceKind(rawKind)
			item.UpdatedAt = parseKnowledgeTime(updatedAt)
			item.KeywordScore = keywordScoreFromRank(rawRank) + localeMatchBoost(queryLocale, item.Locale)
			item.Score = item.KeywordScore
			results = append(results, item)
		}
		if err := rows.Err(); err != nil {
			return nil, fmt.Errorf("search knowledge text: %w", err)
		}
		if len(results) > 0 {
			sortSearchResults(results)
			if len(results) > limit {
				results = results[:limit]
			}
			return results, nil
		}
	}
	return s.searchTextFallback(ctx, filter, queryLocale)
}

func (s *SQLiteStore) searchTextFallback(ctx context.Context, filter SearchFilter, queryLocale string) ([]SearchResult, error) {
	queryText := strings.TrimSpace(filter.Query)
	limit := filter.Limit
	if limit <= 0 {
		limit = defaultSearchLimit
	}
	scope := strings.TrimSpace(filter.SourceID)
	pattern := "%" + strings.TrimSpace(queryText) + "%"
	args := []any{pattern, pattern, pattern}
	where := []string{`sources.enabled = 1`, `(chunks.title LIKE ? OR chunks.path LIKE ? OR chunks.content LIKE ?)`}
	if scope != "" {
		where = append(where, `chunks.source_id = ?`)
		args = append(args, scope)
	}
	args = append(args, limit*4)
	rows, err := s.db.QueryContext(ctx, `
SELECT chunks.id, chunks.source_id, sources.name, sources.kind, chunks.document_id, chunks.title, chunks.path, chunks.uri,
       chunks.locale, chunks.content, chunks.preview, chunks.updated_at
FROM knowledge_chunks AS chunks
JOIN knowledge_sources AS sources ON sources.id = chunks.source_id
WHERE `+strings.Join(where, " AND ")+`
ORDER BY chunks.updated_at DESC
LIMIT ?`, args...)
	if err != nil {
		return nil, fmt.Errorf("search knowledge text fallback: %w", err)
	}
	defer rows.Close()

	results := make([]SearchResult, 0)
	for rows.Next() {
		var item SearchResult
		var rawKind string
		var content string
		var updatedAt string
		if err := rows.Scan(
			&item.ChunkID,
			&item.SourceID,
			&item.SourceName,
			&rawKind,
			&item.DocumentID,
			&item.Title,
			&item.Path,
			&item.URI,
			&item.Locale,
			&content,
			&item.Preview,
			&updatedAt,
		); err != nil {
			return nil, fmt.Errorf("search knowledge text fallback: %w", err)
		}
		item.SourceKind = SourceKind(rawKind)
		item.UpdatedAt = parseKnowledgeTime(updatedAt)
		item.Preview = extractQueryPreview(firstNonEmpty(item.Preview, content), queryText)
		item.KeywordScore = float64(strings.Count(strings.ToLower(item.Title), strings.ToLower(queryText))*3+
			strings.Count(strings.ToLower(item.Path), strings.ToLower(queryText))*2+
			strings.Count(strings.ToLower(content), strings.ToLower(queryText))) +
			localeMatchBoost(queryLocale, item.Locale)
		item.Score = item.KeywordScore
		results = append(results, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("search knowledge text fallback: %w", err)
	}
	sortSearchResults(results)
	if len(results) > limit {
		results = results[:limit]
	}
	return results, nil
}

func (s *SQLiteStore) ListChunkVectors(ctx context.Context, sourceID string) ([]ChunkVector, error) {
	query := `
SELECT chunk_id, source_id, document_id, locale, model, content_hash, vector_json, projected_at
FROM knowledge_chunk_vectors`
	args := []any{}
	if strings.TrimSpace(sourceID) != "" {
		query += ` WHERE source_id = ?`
		args = append(args, strings.TrimSpace(sourceID))
	}
	query += ` ORDER BY chunk_id`
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list knowledge chunk vectors: %w", err)
	}
	defer rows.Close()

	out := make([]ChunkVector, 0)
	for rows.Next() {
		var item ChunkVector
		var rawVector string
		var projectedAt string
		if err := rows.Scan(
			&item.ChunkID,
			&item.SourceID,
			&item.DocumentID,
			&item.Locale,
			&item.Model,
			&item.ContentHash,
			&rawVector,
			&projectedAt,
		); err != nil {
			return nil, fmt.Errorf("list knowledge chunk vectors: %w", err)
		}
		if err := decodeKnowledgeJSON(rawVector, &item.Vector); err != nil {
			return nil, fmt.Errorf("decode knowledge chunk vector %q: %w", item.ChunkID, err)
		}
		item.ProjectedAt = parseKnowledgeTime(projectedAt)
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list knowledge chunk vectors: %w", err)
	}
	return out, nil
}

func (s *SQLiteStore) UpsertChunkVectors(ctx context.Context, vectors []ChunkVector) error {
	if len(vectors) == 0 {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("upsert knowledge chunk vectors: %w", err)
	}
	defer rollbackKnowledgeTx(tx)

	for _, item := range vectors {
		if _, err := tx.ExecContext(ctx, `
INSERT INTO knowledge_chunk_vectors (
	chunk_id, source_id, document_id, locale, model, content_hash, vector_json, projected_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(chunk_id) DO UPDATE SET
	source_id = excluded.source_id,
	document_id = excluded.document_id,
	locale = excluded.locale,
	model = excluded.model,
	content_hash = excluded.content_hash,
	vector_json = excluded.vector_json,
	projected_at = excluded.projected_at`,
			item.ChunkID,
			item.SourceID,
			item.DocumentID,
			item.Locale,
			item.Model,
			item.ContentHash,
			marshalJSON(item.Vector),
			formatKnowledgeTime(item.ProjectedAt),
		); err != nil {
			return fmt.Errorf("upsert knowledge chunk vector %q: %w", item.ChunkID, err)
		}
	}
	return tx.Commit()
}

func (s *SQLiteStore) DeleteChunkVectors(ctx context.Context, chunkIDs []string) error {
	if len(chunkIDs) == 0 {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("delete knowledge chunk vectors: %w", err)
	}
	defer rollbackKnowledgeTx(tx)
	for _, chunkID := range chunkIDs {
		chunkID = strings.TrimSpace(chunkID)
		if chunkID == "" {
			continue
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM knowledge_chunk_vectors WHERE chunk_id = ?`, chunkID); err != nil {
			return fmt.Errorf("delete knowledge chunk vector %q: %w", chunkID, err)
		}
	}
	return tx.Commit()
}

type knowledgeScanner interface {
	Scan(dest ...any) error
}

func scanSource(scanner knowledgeScanner) (Source, error) {
	var item Source
	var rawKind string
	var enabled int
	var urlsJSON string
	var configJSON string
	var includeJSON string
	var excludeJSON string
	var rawStatus string
	var lastSyncAt string
	var createdAt string
	var updatedAt string
	if err := scanner.Scan(
		&item.ID,
		&item.Name,
		&rawKind,
		&enabled,
		&item.Locale,
		&item.Path,
		&urlsJSON,
		&configJSON,
		&includeJSON,
		&excludeJSON,
		&rawStatus,
		&lastSyncAt,
		&item.LastError,
		&item.SyncCursor,
		&item.Stats.Documents,
		&item.Stats.Chunks,
		&item.Stats.Bytes,
		&createdAt,
		&updatedAt,
		&item.ConnectorNote,
	); err != nil {
		return Source{}, err
	}
	item.Kind = SourceKind(rawKind)
	item.Enabled = enabled != 0
	item.Status = SourceStatus(rawStatus)
	if err := decodeKnowledgeJSON(urlsJSON, &item.URLs); err != nil {
		return Source{}, fmt.Errorf("decode source %q urls: %w", item.ID, err)
	}
	if err := decodeKnowledgeJSON(configJSON, &item.Config); err != nil {
		return Source{}, fmt.Errorf("decode source %q config: %w", item.ID, err)
	}
	if err := decodeKnowledgeJSON(includeJSON, &item.IncludeGlobs); err != nil {
		return Source{}, fmt.Errorf("decode source %q include_globs: %w", item.ID, err)
	}
	if err := decodeKnowledgeJSON(excludeJSON, &item.ExcludeGlobs); err != nil {
		return Source{}, fmt.Errorf("decode source %q exclude_globs: %w", item.ID, err)
	}
	item.LastSyncAt = parseKnowledgeTime(lastSyncAt)
	item.CreatedAt = parseKnowledgeTime(createdAt)
	item.UpdatedAt = parseKnowledgeTime(updatedAt)
	return item, nil
}

func scanDocument(scanner knowledgeScanner) (Document, error) {
	var item Document
	var rawKind string
	var sourceUpdatedAt string
	var syncedAt string
	var metadataJSON string
	if err := scanner.Scan(
		&item.ID,
		&item.SourceID,
		&rawKind,
		&item.Title,
		&item.Path,
		&item.URI,
		&item.Locale,
		&item.ContentHash,
		&item.Bytes,
		&item.ChunkCount,
		&sourceUpdatedAt,
		&syncedAt,
		&metadataJSON,
	); err != nil {
		return Document{}, err
	}
	item.Kind = DocumentKind(rawKind)
	item.SourceUpdatedAt = parseKnowledgeTime(sourceUpdatedAt)
	item.SyncedAt = parseKnowledgeTime(syncedAt)
	if err := decodeKnowledgeJSON(metadataJSON, &item.Metadata); err != nil {
		return Document{}, fmt.Errorf("decode document %q/%q metadata: %w", item.SourceID, item.ID, err)
	}
	return item, nil
}

func scanChunk(scanner knowledgeScanner) (Chunk, error) {
	var item Chunk
	var updatedAt string
	var metadataJSON string
	if err := scanner.Scan(
		&item.ID,
		&item.SourceID,
		&item.DocumentID,
		&item.Ordinal,
		&item.Title,
		&item.Path,
		&item.URI,
		&item.Locale,
		&item.Content,
		&item.Preview,
		&item.Hash,
		&item.Bytes,
		&item.StartRune,
		&item.EndRune,
		&updatedAt,
		&metadataJSON,
	); err != nil {
		return Chunk{}, err
	}
	item.UpdatedAt = parseKnowledgeTime(updatedAt)
	if err := decodeKnowledgeJSON(metadataJSON, &item.Metadata); err != nil {
		return Chunk{}, fmt.Errorf("decode chunk %q metadata: %w", item.ID, err)
	}
	return item, nil
}

func existingChunkIDs(ctx context.Context, tx *sql.Tx, sourceID, documentID string) ([]string, error) {
	rows, err := tx.QueryContext(ctx, `
SELECT id
FROM knowledge_chunks
WHERE source_id = ? AND document_id = ?`, sourceID, documentID)
	if err != nil {
		return nil, fmt.Errorf("list existing chunks for %q/%q: %w", sourceID, documentID, err)
	}
	defer rows.Close()

	out := make([]string, 0)
	for rows.Next() {
		var chunkID string
		if err := rows.Scan(&chunkID); err != nil {
			return nil, fmt.Errorf("scan existing chunk for %q/%q: %w", sourceID, documentID, err)
		}
		out = append(out, chunkID)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list existing chunks for %q/%q: %w", sourceID, documentID, err)
	}
	return out, nil
}

func deleteChunkProjections(ctx context.Context, tx *sql.Tx, chunkIDs []string) error {
	for _, chunkID := range chunkIDs {
		chunkID = strings.TrimSpace(chunkID)
		if chunkID == "" {
			continue
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM knowledge_chunk_fts WHERE chunk_id = ?`, chunkID); err != nil {
			return fmt.Errorf("delete knowledge fts chunk %q: %w", chunkID, err)
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM knowledge_chunk_vectors WHERE chunk_id = ?`, chunkID); err != nil {
			return fmt.Errorf("delete knowledge vector chunk %q: %w", chunkID, err)
		}
	}
	return nil
}

func rollbackKnowledgeTx(tx *sql.Tx) {
	if tx != nil {
		_ = tx.Rollback()
	}
}

func buildFTSQuery(raw string) string {
	parts := strings.Fields(strings.TrimSpace(raw))
	if len(parts) == 0 {
		return strings.TrimSpace(raw)
	}
	quoted := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		part = strings.ReplaceAll(part, `"`, `""`)
		quoted = append(quoted, `"`+part+`"`)
	}
	return strings.Join(quoted, " AND ")
}

func formatKnowledgeTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(knowledgeStoreTimeFmt)
}

func parseKnowledgeTime(raw string) time.Time {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}
	}
	parsed, err := time.Parse(knowledgeStoreTimeFmt, raw)
	if err != nil {
		return time.Time{}
	}
	return parsed.UTC()
}

func decodeKnowledgeJSON(raw string, target any) error {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	return json.Unmarshal([]byte(raw), target)
}

func marshalJSON(value any) string {
	if value == nil {
		return "{}"
	}
	body, err := json.Marshal(value)
	if err != nil {
		return "{}"
	}
	return string(body)
}

func marshalMap(value map[string]any) string {
	if len(value) == 0 {
		return "{}"
	}
	body, err := json.Marshal(value)
	if err != nil {
		return "{}"
	}
	return string(body)
}

func marshalStringSlice(values []string) string {
	if len(values) == 0 {
		return "[]"
	}
	body, err := json.Marshal(values)
	if err != nil {
		return "[]"
	}
	return string(body)
}

func boolToInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func keywordScoreFromRank(rank float64) float64 {
	if rank == 0 {
		return 1
	}
	return 1 / (1 + math.Abs(rank))
}

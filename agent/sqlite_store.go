package agent

import (
	"context"
	"database/sql"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"strings"
	"sync"
	"time"

	"github.com/fulcrus/hopclaw/durablefact"
	"github.com/fulcrus/hopclaw/logging"

	_ "modernc.org/sqlite"
)

const (
	sqliteDriverName = "sqlite"

	createMemoryEntriesSQL = `CREATE TABLE IF NOT EXISTS memory_entries (
		key        TEXT PRIMARY KEY,
		value      TEXT NOT NULL,
		created_at TEXT NOT NULL,
		updated_at TEXT NOT NULL
	)`

	createMemoryVectorsSQL = `CREATE TABLE IF NOT EXISTS memory_vectors (
		key    TEXT PRIMARY KEY,
		vector BLOB NOT NULL,
		dim    INTEGER NOT NULL
	)`

	vectorBytesPerFloat32 = 4
)

type SQLiteKVStore struct {
	mu              sync.RWMutex
	db              *sql.DB
	facts           *durablefact.SQLiteStore
	ownedDB         bool
	embeddingClient EmbeddingClient
	vectorStore     *VectorStore
}

func NewSQLiteKVStore(path string) (*SQLiteKVStore, error) {
	db, err := sql.Open(sqliteDriverName, path)
	if err != nil {
		return nil, fmt.Errorf("open sqlite memory store: %w", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(0)
	if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
		logging.DebugIfErr(db.Close(), "close sqlite db after WAL failure")
		return nil, fmt.Errorf("set WAL mode: %w", err)
	}
	store, err := newSQLiteKVStore(db, true)
	if err != nil {
		logging.DebugIfErr(db.Close(), "close sqlite db after durable fact init failure")
		return nil, err
	}
	return store, nil
}

func NewSQLiteKVStoreFromDB(db *sql.DB) (*SQLiteKVStore, error) {
	return newSQLiteKVStore(db, false)
}

func newSQLiteKVStore(db *sql.DB, owned bool) (*SQLiteKVStore, error) {
	if db == nil {
		return nil, fmt.Errorf("sqlite db is required")
	}
	if err := EnsureSQLiteKVSchema(db); err != nil {
		return nil, err
	}
	facts, err := durablefact.NewSQLiteStore(db)
	if err != nil {
		return nil, err
	}
	store := &SQLiteKVStore{
		db:      db,
		facts:   facts,
		ownedDB: owned,
	}
	if err := store.migrateLegacyMemoryEntries(context.Background()); err != nil {
		return nil, err
	}
	return store, nil
}

func (s *SQLiteKVStore) ListContextViews(ctx context.Context, filter durablefact.Filter) ([]durablefact.ContextView, error) {
	facts, err := s.facts.List(ctx, filter)
	if err != nil {
		return nil, err
	}
	views := make([]durablefact.ContextView, 0, len(facts))
	for _, fact := range facts {
		view, ok := durablefact.ToContextView(fact)
		if !ok {
			continue
		}
		views = append(views, view)
	}
	return views, nil
}

func (s *SQLiteKVStore) ListOperatorViews(ctx context.Context, filter durablefact.Filter) ([]durablefact.OperatorView, error) {
	facts, err := s.facts.List(ctx, filter)
	if err != nil {
		return nil, err
	}
	views := make([]durablefact.OperatorView, 0, len(facts))
	for _, fact := range facts {
		if _, ok := durablefact.ToContextView(fact); !ok {
			continue
		}
		views = append(views, durablefact.ToOperatorView(fact))
	}
	return views, nil
}

func (s *SQLiteKVStore) StoreType() string {
	return "sqlite"
}

func EnsureSQLiteKVSchema(db *sql.DB) error {
	if err := durablefact.EnsureSQLiteSchema(db); err != nil {
		return err
	}
	for _, ddl := range []string{createMemoryEntriesSQL, createMemoryVectorsSQL} {
		if _, err := db.Exec(ddl); err != nil {
			return fmt.Errorf("create memory table: %w", err)
		}
	}
	return nil
}

func (s *SQLiteKVStore) Close() error {
	if !s.ownedDB {
		return nil
	}
	return s.db.Close()
}

func (s *SQLiteKVStore) SetEmbedding(client EmbeddingClient) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.embeddingClient = client
	s.vectorStore = NewVectorStore()

	if err := s.loadVectors(); err != nil {
		log.Warn("failed to load vectors from sqlite", "error", err)
	}
}

func (s *SQLiteKVStore) HasEmbedding() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.embeddingClient != nil && s.vectorStore != nil
}

func (s *SQLiteKVStore) EmbeddingClient() EmbeddingClient {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.embeddingClient
}

func (s *SQLiteKVStore) VectorStats() (int, int) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.vectorStore == nil {
		return 0, 0
	}
	return len(s.vectorStore.entries), s.vectorStore.dim
}

func (s *SQLiteKVStore) Reindex(ctx context.Context, force bool) (int, error) {
	entries, err := s.List(ctx)
	if err != nil {
		return 0, err
	}

	s.mu.RLock()
	embClient := s.embeddingClient
	s.mu.RUnlock()
	if embClient == nil {
		if force {
			if _, err := s.db.ExecContext(ctx, "DELETE FROM memory_vectors"); err != nil {
				return 0, fmt.Errorf("clear memory vectors: %w", err)
			}
			s.mu.Lock()
			s.vectorStore = nil
			s.mu.Unlock()
		}
		return len(entries), nil
	}

	type indexedVector struct {
		key   string
		value string
		vec   []float32
	}

	rebuilt := NewVectorStore()
	vectors := make([]indexedVector, 0, len(entries))
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return len(vectors), err
		}
		embedded, err := embClient.Embed(ctx, []string{entry.Key + " " + entry.Value})
		if err != nil {
			return len(vectors), fmt.Errorf("embed %s: %w", entry.Key, err)
		}
		if len(embedded) == 0 {
			continue
		}
		vec := embedded[0]
		if err := rebuilt.Upsert(entry.Key, entry.Value, vec); err != nil {
			return len(vectors), fmt.Errorf("index %s: %w", entry.Key, err)
		}
		vectors = append(vectors, indexedVector{key: entry.Key, value: entry.Value, vec: vec})
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("begin memory reindex: %w", err)
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM memory_vectors"); err != nil {
		_ = tx.Rollback()
		return 0, fmt.Errorf("clear memory vectors: %w", err)
	}
	for _, item := range vectors {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO memory_vectors (key, vector, dim) VALUES (?, ?, ?)
			 ON CONFLICT(key) DO UPDATE SET vector = excluded.vector, dim = excluded.dim`,
			item.key, encodeVector(item.vec), len(item.vec)); err != nil {
			_ = tx.Rollback()
			return 0, fmt.Errorf("persist vector %s: %w", item.key, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit memory reindex: %w", err)
	}

	s.mu.Lock()
	s.vectorStore = rebuilt
	s.mu.Unlock()
	return len(vectors), nil
}

func (s *SQLiteKVStore) Get(ctx context.Context, key string) (*MemoryEntry, error) {
	fact, err := s.facts.Get(ctx, strings.TrimSpace(key))
	if err != nil || fact == nil {
		return nil, err
	}
	entry, ok := memoryEntryFromFact(*fact)
	if !ok {
		return nil, nil
	}
	return &entry, nil
}

func (s *SQLiteKVStore) Set(ctx context.Context, key, value string) error {
	_, err := s.UpsertRecord(ctx, MemoryRecord{
		Key:     strings.TrimSpace(key),
		Value:   value,
		Managed: true,
		Source:  MemorySourceUser,
	})
	return err
}

func (s *SQLiteKVStore) UpsertRecord(ctx context.Context, record MemoryRecord) (*MemoryEntry, error) {
	normalized := normalizeMemoryRecord(record)
	if existing, err := s.Get(ctx, normalized.Key); err != nil {
		return nil, err
	} else if existing != nil {
		normalized = mergeMemoryRecord(memoryRecordFromEntry(*existing), normalized)
	}
	fact := durableFactFromMemoryRecord(normalized)
	if _, err := s.facts.Upsert(ctx, fact); err != nil {
		return nil, err
	}

	s.mu.RLock()
	embClient := s.embeddingClient
	vecStore := s.vectorStore
	s.mu.RUnlock()
	if embClient != nil && vecStore != nil {
		textToEmbed := fact.Key + " " + normalized.Value
		vectors, err := embClient.Embed(ctx, []string{textToEmbed})
		if err != nil {
			log.Warn("embedding generation failed", "key", fact.Key, "error", err)
		} else if len(vectors) > 0 {
			vec := vectors[0]
			if err := vecStore.Upsert(fact.Key, normalized.Value, vec); err != nil {
				log.Warn("vector store upsert failed", "key", fact.Key, "error", err)
			} else if err := s.persistVector(ctx, fact.Key, vec); err != nil {
				log.Warn("vector persist failed", "key", fact.Key, "error", err)
			}
		}
	}

	return s.Get(ctx, fact.Key)
}

func (s *SQLiteKVStore) Delete(ctx context.Context, key string) error {
	key = strings.TrimSpace(key)
	if err := s.facts.Delete(ctx, key); err != nil {
		return err
	}
	if _, err := s.db.ExecContext(ctx, "DELETE FROM memory_vectors WHERE key = ?", key); err != nil {
		logging.DebugIfErr(err, "delete memory vector row", slog.String("key", key))
	}

	s.mu.RLock()
	vecStore := s.vectorStore
	s.mu.RUnlock()
	if vecStore != nil {
		logging.DebugIfErr(vecStore.Delete(key), "delete vector store entry", slog.String("key", key))
	}
	return nil
}

func (s *SQLiteKVStore) Search(ctx context.Context, query string) ([]MemoryEntry, error) {
	keywordResults, err := s.keywordSearch(ctx, query)
	if err != nil {
		return nil, err
	}

	s.mu.RLock()
	embClient := s.embeddingClient
	vecStore := s.vectorStore
	s.mu.RUnlock()
	if embClient == nil || vecStore == nil {
		return keywordResults, nil
	}

	semanticResults, err := s.semanticSearchInternal(ctx, embClient, vecStore, query, defaultHybridSearchLimit)
	if err != nil {
		return keywordResults, nil
	}
	return mergeResults(keywordResults, semanticResults), nil
}

func (s *SQLiteKVStore) SemanticSearch(ctx context.Context, query string, limit int) ([]MemoryEntry, error) {
	s.mu.RLock()
	embClient := s.embeddingClient
	vecStore := s.vectorStore
	s.mu.RUnlock()
	if embClient == nil || vecStore == nil {
		return nil, fmt.Errorf("embedding client not configured")
	}
	if limit <= 0 {
		limit = defaultSemanticSearchLimit
	}
	return s.semanticSearchInternal(ctx, embClient, vecStore, query, limit)
}

func (s *SQLiteKVStore) SemanticSearchMMR(ctx context.Context, query string, limit int, lambda float64) ([]MemoryEntry, error) {
	s.mu.RLock()
	embClient := s.embeddingClient
	vecStore := s.vectorStore
	s.mu.RUnlock()
	if embClient == nil || vecStore == nil {
		return nil, fmt.Errorf("embedding client not configured")
	}
	if limit <= 0 {
		limit = defaultSemanticSearchLimit
	}
	vectors, err := embClient.Embed(ctx, []string{query})
	if err != nil {
		return nil, fmt.Errorf("failed to embed query: %w", err)
	}
	if len(vectors) == 0 {
		return nil, nil
	}
	hits := vecStore.SearchMMR(vectors[0], limit, lambda)
	return s.enrichResults(ctx, hits)
}

func (s *SQLiteKVStore) List(ctx context.Context) ([]MemoryEntry, error) {
	facts, err := s.facts.List(ctx, durablefact.Filter{ViewType: durablefact.ViewTypeContext})
	if err != nil {
		return nil, fmt.Errorf("list memory entries: %w", err)
	}
	return memoryEntriesFromFacts(facts), nil
}

func (s *SQLiteKVStore) ListFiltered(ctx context.Context, filter MemoryFilter) ([]MemoryEntry, error) {
	results, err := s.List(ctx)
	if err != nil {
		return nil, err
	}
	filter.Namespace = normalizeMemoryNamespace(filter.Namespace)
	filter.ScopeKey = strings.TrimSpace(filter.ScopeKey)
	filter.Query = strings.TrimSpace(filter.Query)
	if filter.Namespace == "" && filter.ScopeKey == "" && filter.Query == "" && !filter.ManagedOnly {
		return results, nil
	}
	filtered := make([]MemoryEntry, 0, len(results))
	for _, entry := range results {
		if filter.ManagedOnly && !entry.Managed {
			continue
		}
		if filter.Namespace != "" && entry.Namespace != filter.Namespace {
			continue
		}
		if filter.ScopeKey != "" && entry.ScopeKey != filter.ScopeKey {
			continue
		}
		if filter.Query != "" && !memoryEntryMatchesQuery(entry, filter.Query) {
			continue
		}
		filtered = append(filtered, entry)
	}
	return filtered, nil
}

func (s *SQLiteKVStore) keywordSearch(ctx context.Context, query string) ([]MemoryEntry, error) {
	facts, err := s.facts.Search(ctx, durablefact.Filter{
		ViewType: durablefact.ViewTypeContext,
		Query:    strings.TrimSpace(query),
	})
	if err != nil {
		return nil, err
	}
	return memoryEntriesFromFacts(facts), nil
}

func (s *SQLiteKVStore) semanticSearchInternal(
	ctx context.Context,
	embClient EmbeddingClient,
	vecStore *VectorStore,
	query string,
	limit int,
) ([]MemoryEntry, error) {
	vectors, err := embClient.Embed(ctx, []string{query})
	if err != nil {
		return nil, fmt.Errorf("failed to embed query: %w", err)
	}
	if len(vectors) == 0 {
		return nil, nil
	}
	hits := vecStore.Search(vectors[0], limit)
	return s.enrichResults(ctx, hits)
}

func (s *SQLiteKVStore) enrichResults(ctx context.Context, hits []VectorSearchResult) ([]MemoryEntry, error) {
	results := make([]MemoryEntry, 0, len(hits))
	for _, hit := range hits {
		entry, err := s.Get(ctx, hit.Key)
		if err != nil || entry == nil {
			continue
		}
		entry.Score = hit.Score
		results = append(results, *entry)
	}
	return results, nil
}

func (s *SQLiteKVStore) persistVector(ctx context.Context, key string, vec []float32) error {
	blob := encodeVector(vec)
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO memory_vectors (key, vector, dim) VALUES (?, ?, ?)
		 ON CONFLICT(key) DO UPDATE SET vector = excluded.vector, dim = excluded.dim`,
		key, blob, len(vec))
	if err != nil {
		return fmt.Errorf("persist vector: %w", err)
	}
	return nil
}

func (s *SQLiteKVStore) loadVectors() error {
	rows, err := s.db.Query(`
		SELECT v.key, f.value, v.vector FROM memory_vectors v
		JOIN durable_facts f ON v.key = f.key
		WHERE f.view_type = ?`, string(durablefact.ViewTypeContext))
	if err != nil {
		return fmt.Errorf("load vectors: %w", err)
	}
	defer rows.Close()

	var loaded int
	for rows.Next() {
		var key, value string
		var blob []byte
		if err := rows.Scan(&key, &value, &blob); err != nil {
			continue
		}
		vec := decodeVector(blob)
		if len(vec) == 0 {
			continue
		}
		if err := s.vectorStore.Upsert(key, value, vec); err != nil {
			log.Warn("skipped vector load", "key", key, "error", err)
			continue
		}
		loaded++
	}
	if loaded > 0 {
		log.Info("loaded vectors from sqlite", "count", loaded)
	}
	return rows.Err()
}

func (s *SQLiteKVStore) migrateLegacyMemoryEntries(ctx context.Context) error {
	rows, err := s.db.QueryContext(ctx, `
		SELECT key, value, created_at, updated_at
		FROM memory_entries
		ORDER BY key`)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "no such table") {
			return nil
		}
		return fmt.Errorf("list legacy memory entries: %w", err)
	}
	defer rows.Close()

	legacyFacts := make([]durablefact.Fact, 0)
	for rows.Next() {
		var (
			key        string
			value      string
			createdStr string
			updatedStr string
		)
		if err := rows.Scan(&key, &value, &createdStr, &updatedStr); err != nil {
			return fmt.Errorf("scan legacy memory entry: %w", err)
		}
		createdAt, err := parseMemoryEntryTimestamp("created_at", createdStr)
		if err != nil {
			return err
		}
		updatedAt, err := parseMemoryEntryTimestamp("updated_at", updatedStr)
		if err != nil {
			return err
		}
		_, record, err := decodeMemoryEntry(MemoryEntry{
			Key:       key,
			Value:     value,
			CreatedAt: createdAt,
			UpdatedAt: updatedAt,
		})
		if err != nil {
			return err
		}
		if record == nil {
			continue
		}
		legacyFacts = append(legacyFacts, durableFactFromMemoryRecord(*record))
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if err := rows.Close(); err != nil {
		return err
	}

	for _, fact := range legacyFacts {
		existing, err := s.facts.Get(ctx, fact.Key)
		if err != nil {
			return err
		}
		if existing != nil && !fact.UpdatedAt.After(existing.UpdatedAt) {
			continue
		}
		if _, err := s.facts.Upsert(ctx, fact); err != nil {
			return err
		}
	}
	return nil
}

func durableFactFromMemoryRecord(record MemoryRecord) durablefact.Fact {
	normalized := normalizeMemoryRecord(record)
	_, reviewRequired := classifyMemoryRecord(normalized)
	return durablefact.NormalizeFact(durablefact.Fact{
		Key:            normalized.Key,
		FactClass:      normalized.FactClass,
		ViewType:       durablefact.ViewTypeContext,
		Namespace:      normalized.Namespace,
		ScopeKey:       normalized.ScopeKey,
		Name:           normalized.Field,
		Label:          normalized.Label,
		Value:          normalized.Value,
		ValueType:      durablefact.ValueTypeText,
		Source:         normalized.Source,
		Managed:        normalized.Managed,
		Confidence:     normalized.Score,
		ReviewRequired: reviewRequired,
		Tags:           append([]string(nil), normalized.Tags...),
		PreviousValues: append([]string(nil), normalized.PreviousValues...),
		Evidence:       memoryEvidenceToDurable(normalized.Evidence),
		Metadata:       durableMemoryMetadata(normalized),
		CreatedAt:      normalized.CreatedAt,
		UpdatedAt:      normalized.UpdatedAt,
	})
}

func memoryEvidenceToDurable(items []MemoryRecordEvidence) []durablefact.Evidence {
	if len(items) == 0 {
		return nil
	}
	out := make([]durablefact.Evidence, 0, len(items))
	for _, item := range items {
		out = append(out, durablefact.Evidence{
			Source:     item.Source,
			Ref:        item.Ref,
			Summary:    item.Summary,
			Value:      item.Value,
			ObservedAt: item.ObservedAt,
		})
	}
	return out
}

func memoryEntryFromFact(fact durablefact.Fact) (MemoryEntry, bool) {
	view, ok := durablefact.ToContextView(fact)
	if !ok {
		return MemoryEntry{}, false
	}
	metadata := decodeDurableMemoryMetadata(fact.Metadata)
	return MemoryEntry{
		Key:             view.Key,
		Value:           view.Value,
		SessionKey:      metadata.SessionKey,
		ProjectID:       metadata.ProjectID,
		FactClass:       view.FactClass,
		Namespace:       view.Namespace,
		ScopeKey:        view.ScopeKey,
		Field:           view.Field,
		Label:           view.Label,
		Managed:         view.Managed,
		Source:          view.Source,
		Tags:            append([]string(nil), view.Tags...),
		PreviousValues:  append([]string(nil), view.PreviousValues...),
		EvidenceCount:   view.EvidenceCount,
		Score:           view.Confidence,
		State:           metadata.State,
		SupersededBy:    metadata.SupersededBy,
		MediaRefs:       append([]string(nil), metadata.MediaRefs...),
		UsedCount:       metadata.UsedCount,
		LastUsedAt:      metadata.LastUsedAt,
		CorrectionCount: metadata.CorrectionCount,
		CreatedAt:       view.CreatedAt,
		UpdatedAt:       view.UpdatedAt,
	}, true
}

func memoryEntriesFromFacts(facts []durablefact.Fact) []MemoryEntry {
	out := make([]MemoryEntry, 0, len(facts))
	for _, fact := range facts {
		entry, ok := memoryEntryFromFact(fact)
		if !ok {
			continue
		}
		out = append(out, entry)
	}
	return out
}

func memoryRecordFromEntry(entry MemoryEntry) MemoryRecord {
	return MemoryRecord{
		Key:             entry.Key,
		SessionKey:      entry.SessionKey,
		ProjectID:       entry.ProjectID,
		FactClass:       entry.FactClass,
		Namespace:       entry.Namespace,
		ScopeKey:        entry.ScopeKey,
		Field:           entry.Field,
		Label:           entry.Label,
		Value:           entry.Value,
		Managed:         entry.Managed,
		Source:          entry.Source,
		Score:           entry.Score,
		State:           entry.State,
		SupersededBy:    entry.SupersededBy,
		MediaRefs:       append([]string(nil), entry.MediaRefs...),
		UsedCount:       entry.UsedCount,
		LastUsedAt:      entry.LastUsedAt,
		CorrectionCount: entry.CorrectionCount,
		Tags:            append([]string(nil), entry.Tags...),
		PreviousValues:  append([]string(nil), entry.PreviousValues...),
		CreatedAt:       entry.CreatedAt,
		UpdatedAt:       entry.UpdatedAt,
	}
}

type durableMemoryMetadataPayload struct {
	State           MemoryState `json:"state,omitempty"`
	SupersededBy    string      `json:"superseded_by,omitempty"`
	SessionKey      string      `json:"session_key,omitempty"`
	ProjectID       string      `json:"project_id,omitempty"`
	MediaRefs       []string    `json:"media_refs,omitempty"`
	UsedCount       int         `json:"used_count,omitempty"`
	LastUsedAt      time.Time   `json:"last_used_at,omitempty"`
	CorrectionCount int         `json:"correction_count,omitempty"`
}

func durableMemoryMetadata(record MemoryRecord) map[string]any {
	payload := durableMemoryMetadataPayload{
		State:           record.State,
		SupersededBy:    record.SupersededBy,
		SessionKey:      record.SessionKey,
		ProjectID:       record.ProjectID,
		MediaRefs:       append([]string(nil), record.MediaRefs...),
		UsedCount:       record.UsedCount,
		LastUsedAt:      record.LastUsedAt,
		CorrectionCount: record.CorrectionCount,
	}
	if payload.State == "" && payload.SupersededBy == "" && payload.SessionKey == "" && payload.ProjectID == "" && len(payload.MediaRefs) == 0 && payload.UsedCount == 0 && payload.LastUsedAt.IsZero() && payload.CorrectionCount == 0 {
		return nil
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil
	}
	var out map[string]any
	if err := json.Unmarshal(body, &out); err != nil {
		return nil
	}
	return out
}

func decodeDurableMemoryMetadata(metadata map[string]any) durableMemoryMetadataPayload {
	if len(metadata) == 0 {
		return durableMemoryMetadataPayload{State: MemoryActive}
	}
	body, err := json.Marshal(metadata)
	if err != nil {
		return durableMemoryMetadataPayload{State: MemoryActive}
	}
	var payload durableMemoryMetadataPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		return durableMemoryMetadataPayload{State: MemoryActive}
	}
	payload.State = MemoryState(strings.TrimSpace(string(payload.State)))
	if payload.State == "" {
		payload.State = MemoryActive
	}
	payload.SupersededBy = strings.TrimSpace(payload.SupersededBy)
	payload.SessionKey = strings.TrimSpace(payload.SessionKey)
	payload.ProjectID = strings.TrimSpace(payload.ProjectID)
	payload.MediaRefs = uniqueSortedStrings(payload.MediaRefs)
	return payload
}

func parseMemoryEntryTimestamp(field, value string) (time.Time, error) {
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return time.Time{}, fmt.Errorf("parse memory entry %s: %w", field, err)
	}
	return parsed, nil
}

func encodeVector(v []float32) []byte {
	buf := make([]byte, len(v)*vectorBytesPerFloat32)
	for i, f := range v {
		binary.LittleEndian.PutUint32(buf[i*vectorBytesPerFloat32:], math.Float32bits(f))
	}
	return buf
}

func decodeVector(b []byte) []float32 {
	if len(b)%vectorBytesPerFloat32 != 0 {
		return nil
	}
	v := make([]float32, len(b)/vectorBytesPerFloat32)
	for i := range v {
		v[i] = math.Float32frombits(binary.LittleEndian.Uint32(b[i*vectorBytesPerFloat32:]))
	}
	return v
}

func escapeLike(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return r.Replace(s)
}

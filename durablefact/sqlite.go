package durablefact

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

const sqliteTimeFormat = time.RFC3339Nano

const durableFactSchema = `
CREATE TABLE IF NOT EXISTS durable_facts (
	key             TEXT PRIMARY KEY,
	fact_class      TEXT NOT NULL,
	view_type       TEXT NOT NULL,
	namespace       TEXT NOT NULL DEFAULT '',
	scope_key       TEXT NOT NULL DEFAULT '',
	name            TEXT NOT NULL DEFAULT '',
	label           TEXT NOT NULL DEFAULT '',
	value           TEXT NOT NULL DEFAULT '',
	value_type      TEXT NOT NULL DEFAULT 'text',
	source          TEXT NOT NULL DEFAULT '',
	managed         INTEGER NOT NULL DEFAULT 0,
	confidence      REAL NOT NULL DEFAULT 0,
	review_required INTEGER NOT NULL DEFAULT 0,
	tags            TEXT NOT NULL DEFAULT '[]',
	previous_values TEXT NOT NULL DEFAULT '[]',
	evidence        TEXT NOT NULL DEFAULT '[]',
	metadata        TEXT NOT NULL DEFAULT '{}',
	created_at      TEXT NOT NULL,
	updated_at      TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_durable_facts_view_type ON durable_facts (view_type);
CREATE INDEX IF NOT EXISTS idx_durable_facts_fact_class ON durable_facts (fact_class);
CREATE INDEX IF NOT EXISTS idx_durable_facts_namespace_scope ON durable_facts (namespace, scope_key);
CREATE INDEX IF NOT EXISTS idx_durable_facts_updated_at ON durable_facts (updated_at);
`

type SQLiteStore struct {
	db *sql.DB
}

func NewSQLiteStore(db *sql.DB) (*SQLiteStore, error) {
	if db == nil {
		return nil, fmt.Errorf("sqlite db is required")
	}
	if err := EnsureSQLiteSchema(db); err != nil {
		return nil, err
	}
	return &SQLiteStore{db: db}, nil
}

func EnsureSQLiteSchema(db *sql.DB) error {
	if db == nil {
		return fmt.Errorf("sqlite db is required")
	}
	if _, err := db.Exec(durableFactSchema); err != nil {
		return fmt.Errorf("create durable facts schema: %w", err)
	}
	return nil
}

func (s *SQLiteStore) Get(ctx context.Context, key string) (*Fact, error) {
	key = strings.TrimSpace(key)
	if key == "" {
		return nil, nil
	}
	row := s.db.QueryRowContext(ctx, `
		SELECT key, fact_class, view_type, namespace, scope_key, name, label, value, value_type,
		       source, managed, confidence, review_required, tags, previous_values, evidence, metadata,
		       created_at, updated_at
		FROM durable_facts WHERE key = ?`, key)
	fact, err := scanFactRow(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return fact, nil
}

func (s *SQLiteStore) Upsert(ctx context.Context, fact Fact) (*Fact, error) {
	fact = NormalizeFact(fact)
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO durable_facts (
			key, fact_class, view_type, namespace, scope_key, name, label, value, value_type,
			source, managed, confidence, review_required, tags, previous_values, evidence, metadata,
			created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(key) DO UPDATE SET
			fact_class = excluded.fact_class,
			view_type = excluded.view_type,
			namespace = excluded.namespace,
			scope_key = excluded.scope_key,
			name = excluded.name,
			label = excluded.label,
			value = excluded.value,
			value_type = excluded.value_type,
			source = excluded.source,
			managed = excluded.managed,
			confidence = excluded.confidence,
			review_required = excluded.review_required,
			tags = excluded.tags,
			previous_values = excluded.previous_values,
			evidence = excluded.evidence,
			metadata = excluded.metadata,
			updated_at = excluded.updated_at`,
		fact.Key,
		string(fact.FactClass),
		string(fact.ViewType),
		fact.Namespace,
		fact.ScopeKey,
		fact.Name,
		fact.Label,
		fact.Value,
		string(fact.ValueType),
		fact.Source,
		boolToInt(fact.Managed),
		fact.Confidence,
		boolToInt(fact.ReviewRequired),
		mustJSON(fact.Tags, "[]"),
		mustJSON(fact.PreviousValues, "[]"),
		mustJSON(fact.Evidence, "[]"),
		mustJSON(fact.Metadata, "{}"),
		formatTime(fact.CreatedAt),
		formatTime(fact.UpdatedAt),
	)
	if err != nil {
		return nil, fmt.Errorf("upsert durable fact %q: %w", fact.Key, err)
	}
	return s.Get(ctx, fact.Key)
}

func (s *SQLiteStore) Delete(ctx context.Context, key string) error {
	key = strings.TrimSpace(key)
	if key == "" {
		return nil
	}
	_, err := s.db.ExecContext(ctx, `DELETE FROM durable_facts WHERE key = ?`, key)
	if err != nil {
		return fmt.Errorf("delete durable fact %q: %w", key, err)
	}
	return nil
}

func (s *SQLiteStore) List(ctx context.Context, filter Filter) ([]Fact, error) {
	query, args := buildListQuery(filter)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list durable facts: %w", err)
	}
	defer rows.Close()

	var facts []Fact
	for rows.Next() {
		fact, err := scanFactRows(rows)
		if err != nil {
			return nil, err
		}
		facts = append(facts, *fact)
	}
	return facts, rows.Err()
}

func (s *SQLiteStore) Search(ctx context.Context, filter Filter) ([]Fact, error) {
	return s.List(ctx, filter)
}

type factScanner interface {
	Scan(dest ...any) error
}

func scanFactRow(row factScanner) (*Fact, error) {
	fact := &Fact{}
	var (
		factClass      string
		viewType       string
		valueType      string
		managed        int
		reviewRequired int
		tagsJSON       string
		previousJSON   string
		evidenceJSON   string
		metadataJSON   string
		createdAt      string
		updatedAt      string
	)
	if err := row.Scan(
		&fact.Key,
		&factClass,
		&viewType,
		&fact.Namespace,
		&fact.ScopeKey,
		&fact.Name,
		&fact.Label,
		&fact.Value,
		&valueType,
		&fact.Source,
		&managed,
		&fact.Confidence,
		&reviewRequired,
		&tagsJSON,
		&previousJSON,
		&evidenceJSON,
		&metadataJSON,
		&createdAt,
		&updatedAt,
	); err != nil {
		return nil, err
	}
	fact.FactClass = FactClass(factClass)
	fact.ViewType = ViewType(viewType)
	fact.ValueType = ValueType(valueType)
	fact.Managed = managed != 0
	fact.ReviewRequired = reviewRequired != 0
	if err := decodeJSON(tagsJSON, &fact.Tags, "tags", fact.Key); err != nil {
		return nil, err
	}
	if err := decodeJSON(previousJSON, &fact.PreviousValues, "previous_values", fact.Key); err != nil {
		return nil, err
	}
	if err := decodeJSON(evidenceJSON, &fact.Evidence, "evidence", fact.Key); err != nil {
		return nil, err
	}
	if err := decodeJSON(metadataJSON, &fact.Metadata, "metadata", fact.Key); err != nil {
		return nil, err
	}
	var err error
	fact.CreatedAt, err = parseTime(createdAt, fact.Key, "created_at")
	if err != nil {
		return nil, err
	}
	fact.UpdatedAt, err = parseTime(updatedAt, fact.Key, "updated_at")
	if err != nil {
		return nil, err
	}
	normalized := NormalizeFact(*fact)
	return &normalized, nil
}

func scanFactRows(rows *sql.Rows) (*Fact, error) {
	return scanFactRow(rows)
}

func buildListQuery(filter Filter) (string, []any) {
	clauses := []string{"1=1"}
	args := make([]any, 0, 8)
	if filter.ViewType != "" {
		clauses = append(clauses, "view_type = ?")
		args = append(args, strings.TrimSpace(string(filter.ViewType)))
	}
	if filter.FactClass != "" {
		clauses = append(clauses, "fact_class = ?")
		args = append(args, strings.TrimSpace(string(filter.FactClass)))
	}
	if namespace := strings.TrimSpace(filter.Namespace); namespace != "" {
		clauses = append(clauses, "namespace = ?")
		args = append(args, namespace)
	}
	if scopeKey := strings.TrimSpace(filter.ScopeKey); scopeKey != "" {
		clauses = append(clauses, "scope_key = ?")
		args = append(args, scopeKey)
	}
	if prefix := strings.TrimSpace(filter.Prefix); prefix != "" {
		clauses = append(clauses, "key LIKE ? ESCAPE '\\'")
		args = append(args, escapeLike(prefix)+"%")
	}
	if filter.ReviewRequired != nil {
		clauses = append(clauses, "review_required = ?")
		args = append(args, boolToInt(*filter.ReviewRequired))
	}
	if query := strings.TrimSpace(filter.Query); query != "" {
		pattern := "%" + escapeLike(query) + "%"
		clauses = append(clauses, `(key LIKE ? ESCAPE '\' COLLATE NOCASE
			OR value LIKE ? ESCAPE '\' COLLATE NOCASE
			OR namespace LIKE ? ESCAPE '\' COLLATE NOCASE
			OR scope_key LIKE ? ESCAPE '\' COLLATE NOCASE
			OR name LIKE ? ESCAPE '\' COLLATE NOCASE
			OR label LIKE ? ESCAPE '\' COLLATE NOCASE
			OR source LIKE ? ESCAPE '\' COLLATE NOCASE
			OR tags LIKE ? ESCAPE '\' COLLATE NOCASE)`)
		for range 8 {
			args = append(args, pattern)
		}
	}
	return `
		SELECT key, fact_class, view_type, namespace, scope_key, name, label, value, value_type,
		       source, managed, confidence, review_required, tags, previous_values, evidence, metadata,
		       created_at, updated_at
		FROM durable_facts
		WHERE ` + strings.Join(clauses, " AND ") + `
		ORDER BY view_type, namespace, scope_key, name, key`, args
}

func decodeJSON(raw string, target any, field, key string) error {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	if err := json.Unmarshal([]byte(raw), target); err != nil {
		return fmt.Errorf("decode durable fact %q %s: %w", strings.TrimSpace(key), strings.TrimSpace(field), err)
	}
	return nil
}

func mustJSON(value any, fallback string) string {
	if value == nil {
		return fallback
	}
	body, err := json.Marshal(value)
	if err != nil {
		return fallback
	}
	return string(body)
}

func formatTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(sqliteTimeFormat)
}

func parseTime(raw, key, field string) (time.Time, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}, nil
	}
	parsed, err := time.Parse(sqliteTimeFormat, raw)
	if err != nil {
		return time.Time{}, fmt.Errorf("parse durable fact %q %s: %w", strings.TrimSpace(key), strings.TrimSpace(field), err)
	}
	return parsed, nil
}

func boolToInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

func escapeLike(value string) string {
	replacer := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return replacer.Replace(value)
}

package store

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	stdlog "log"
	"strings"
	"sync/atomic"
	"time"

	_ "modernc.org/sqlite"
)

// ---------------------------------------------------------------------------
// Time format
// ---------------------------------------------------------------------------

const sqliteTimeFmt = time.RFC3339Nano

func formatTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(sqliteTimeFmt)
}

func parseTime(raw, storeKind, recordID, field string) (time.Time, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}, nil
	}
	t, err := time.Parse(sqliteTimeFmt, raw)
	if err != nil {
		return time.Time{}, fmt.Errorf("%s %s: parse %s: %w", strings.TrimSpace(storeKind), sqliteRecordID(recordID), strings.TrimSpace(field), err)
	}
	return t, nil
}

// ---------------------------------------------------------------------------
// JSON helpers
// ---------------------------------------------------------------------------

func marshalJSONValue(v any) (string, error) {
	if v == nil {
		return "{}", nil
	}
	data, err := json.Marshal(v)
	if err != nil {
		return "", fmt.Errorf("marshal json: %w", err)
	}
	return string(data), nil
}

func marshalJSONSliceValue(v any) (string, error) {
	if v == nil {
		return "[]", nil
	}
	data, err := json.Marshal(v)
	if err != nil {
		return "", fmt.Errorf("marshal json slice: %w", err)
	}
	return string(data), nil
}

func marshalJSON(v any) (string, error) {
	return marshalJSONValue(v)
}

func marshalJSONSlice(v any) (string, error) {
	return marshalJSONSliceValue(v)
}

func decodeJSONField(raw, storeKind, recordID, field string, target any) error {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	if err := json.Unmarshal([]byte(raw), target); err != nil {
		return fmt.Errorf("%s %s: decode %s: %w", strings.TrimSpace(storeKind), sqliteRecordID(recordID), strings.TrimSpace(field), err)
	}
	return nil
}

func decodeJSONMapField(raw, storeKind, recordID, field string) (map[string]any, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	var out map[string]any
	if err := decodeJSONField(raw, storeKind, recordID, field, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func decodeJSONStringSliceField(raw, storeKind, recordID, field string) ([]string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "[]" {
		return nil, nil
	}
	var out []string
	if err := decodeJSONField(raw, storeKind, recordID, field, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func sqliteRecordID(id string) string {
	id = strings.TrimSpace(id)
	if id == "" {
		return "<unknown>"
	}
	return id
}

func newSQLiteRandomID(prefix string) (string, error) {
	var raw [8]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	return fmt.Sprintf("%s-%s", strings.TrimSpace(prefix), hex.EncodeToString(raw[:])), nil
}

// ---------------------------------------------------------------------------
// ID recovery
// ---------------------------------------------------------------------------

var sqliteRecoveryFallbackCounter atomic.Uint64

var sqliteRecoverableTables = map[string]struct{}{
	"sessions":  {},
	"runs":      {},
	"events":    {},
	"approvals": {},
}

// recoverMaxID reads the maximum numeric suffix from IDs with the given
// prefix (e.g. "sess-" from "sess-000042") and returns the value.
func recoverMaxID(db *sql.DB, table, prefix string) (uint64, error) {
	if _, ok := sqliteRecoverableTables[strings.TrimSpace(table)]; !ok {
		return 0, fmt.Errorf("unsupported sqlite recovery table %q", table)
	}
	query := fmt.Sprintf(
		`SELECT COALESCE(MAX(CAST(SUBSTR(id, %d) AS INTEGER)), 0) FROM %s WHERE id LIKE ?`,
		len(prefix)+1, table,
	)
	var maxID uint64
	if err := db.QueryRow(query, prefix+"%").Scan(&maxID); err != nil {
		return 0, err
	}
	return maxID, nil
}

func recoverMaxIDCounter(db *sql.DB, table, prefix string) uint64 {
	maxID, err := recoverMaxID(db, table, prefix)
	if err == nil {
		return maxID
	}
	fallback := sqliteRecoveryFallbackSeed()
	stdlog.Printf("store sqlite: recover max id failed for table %s: %v; using fallback counter seed %d", table, err, fallback)
	return fallback
}

func sqliteRecoveryFallbackSeed() uint64 {
	seed := uint64(time.Now().UTC().UnixMilli())
	if seed == 0 {
		seed = 1
	}
	for {
		current := sqliteRecoveryFallbackCounter.Load()
		next := seed
		if next <= current {
			next = current + 1
		}
		if sqliteRecoveryFallbackCounter.CompareAndSwap(current, next) {
			return next
		}
	}
}

// ---------------------------------------------------------------------------
// Database open
// ---------------------------------------------------------------------------

// OpenDB opens (or creates) a SQLite database at the given path, applies
// PRAGMAs for optimal performance in WAL mode, and runs pending schema
// migrations.
func OpenDB(path string) (*sql.DB, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}

	// Single writer avoids SQLITE_BUSY entirely.
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(0)

	pragmas := []string{
		"PRAGMA journal_mode = WAL",
		"PRAGMA busy_timeout = 5000",
		"PRAGMA synchronous = NORMAL",
		"PRAGMA foreign_keys = ON",
		"PRAGMA cache_size = -8000",
		"PRAGMA temp_store = MEMORY",
		"PRAGMA mmap_size = 67108864",
	}
	for _, p := range pragmas {
		if _, err := db.Exec(p); err != nil {
			db.Close()
			return nil, fmt.Errorf("pragma %q: %w", p, err)
		}
	}

	if err := runMigrations(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrations: %w", err)
	}
	return db, nil
}

// ---------------------------------------------------------------------------
// Schema migrations
// ---------------------------------------------------------------------------

type migration struct {
	Version int
	SQL     string
}

var migrations = []migration{
	{Version: 1, SQL: migrationV1},
	{Version: 2, SQL: migrationV2},
	{Version: 3, SQL: migrationV3},
	{Version: 4, SQL: migrationV4},
	{Version: 5, SQL: migrationV5},
	{Version: 6, SQL: migrationV6},
	{Version: 7, SQL: migrationV7},
	{Version: 8, SQL: migrationV8},
	{Version: 9, SQL: migrationV9},
	{Version: 10, SQL: migrationV10},
	{Version: 11, SQL: migrationV11},
	{Version: 12, SQL: migrationV12},
	{Version: 13, SQL: migrationV13},
	{Version: 14, SQL: migrationV14},
	{Version: 15, SQL: migrationV15},
	{Version: 16, SQL: migrationV16},
	{Version: 17, SQL: migrationV17},
	{Version: 18, SQL: migrationV18},
	{Version: 19, SQL: migrationV19},
	{Version: 20, SQL: migrationV20},
}

const migrationV1 = `
CREATE TABLE IF NOT EXISTS sessions (
    id             TEXT PRIMARY KEY,
    key            TEXT NOT NULL UNIQUE,
    model          TEXT NOT NULL DEFAULT '',
    summary        TEXT NOT NULL DEFAULT '',
    summary_at     TEXT NOT NULL DEFAULT '',
    metadata       TEXT NOT NULL DEFAULT '{}',
    skill_snapshot TEXT NOT NULL DEFAULT '{}',
    created_at     TEXT NOT NULL,
    updated_at     TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_sessions_updated_at ON sessions (updated_at);

CREATE TABLE IF NOT EXISTS messages (
    id             INTEGER PRIMARY KEY,
    session_id     TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    role           TEXT NOT NULL,
    content        TEXT NOT NULL DEFAULT '',
    content_blocks TEXT NOT NULL DEFAULT '[]',
    name           TEXT NOT NULL DEFAULT '',
    tool_call_id   TEXT NOT NULL DEFAULT '',
    tool_calls     TEXT NOT NULL DEFAULT '[]',
    metadata       TEXT NOT NULL DEFAULT '{}',
    created_at     TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_messages_session ON messages (session_id, id);

CREATE TABLE IF NOT EXISTS runs (
    id              TEXT PRIMARY KEY,
    session_id      TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    parent_run_id   TEXT NOT NULL DEFAULT '',
    input_event_id  TEXT NOT NULL DEFAULT '',
    status          TEXT NOT NULL DEFAULT 'queued',
    queue_mode      TEXT NOT NULL DEFAULT 'enqueue',
    phase           TEXT NOT NULL DEFAULT 'ingest',
    model           TEXT NOT NULL DEFAULT '',
    tool_rounds     INTEGER NOT NULL DEFAULT 0,
    approval_id     TEXT NOT NULL DEFAULT '',
    pending_tools   TEXT NOT NULL DEFAULT '[]',
    error           TEXT NOT NULL DEFAULT '',
    plan            TEXT,
    started_at      TEXT NOT NULL DEFAULT '',
    updated_at      TEXT NOT NULL,
    finished_at     TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_runs_session_id ON runs (session_id);
CREATE INDEX IF NOT EXISTS idx_runs_status ON runs (status);
CREATE INDEX IF NOT EXISTS idx_runs_updated_at ON runs (updated_at);
CREATE INDEX IF NOT EXISTS idx_runs_input_event ON runs (input_event_id) WHERE input_event_id != '';

CREATE TABLE IF NOT EXISTS approvals (
    id           TEXT PRIMARY KEY,
    run_id       TEXT NOT NULL,
    session_id   TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    kind         TEXT NOT NULL DEFAULT 'tool_calls',
    status       TEXT NOT NULL DEFAULT 'pending',
    tool_calls   TEXT NOT NULL DEFAULT '[]',
    reasons      TEXT NOT NULL DEFAULT '[]',
    metadata     TEXT NOT NULL DEFAULT '{}',
    scope        TEXT NOT NULL DEFAULT '',
    note         TEXT NOT NULL DEFAULT '',
    resolved_by  TEXT NOT NULL DEFAULT '',
    created_at   TEXT NOT NULL,
    resolved_at  TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_approvals_run_id ON approvals (run_id);
CREATE INDEX IF NOT EXISTS idx_approvals_status ON approvals (status);
CREATE INDEX IF NOT EXISTS idx_approvals_session_id ON approvals (session_id);

CREATE TABLE IF NOT EXISTS approval_external_refs (
    approval_id  TEXT NOT NULL REFERENCES approvals(id) ON DELETE CASCADE,
    provider     TEXT NOT NULL,
    external_id  TEXT NOT NULL DEFAULT '',
    url          TEXT NOT NULL DEFAULT '',
    status       TEXT NOT NULL DEFAULT '',
    metadata     TEXT NOT NULL DEFAULT '{}',
    synced_at    TEXT NOT NULL DEFAULT '',
    PRIMARY KEY (approval_id, provider)
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_approval_external_provider_id
    ON approval_external_refs (provider, external_id)
    WHERE provider != '' AND external_id != '';
CREATE INDEX IF NOT EXISTS idx_approval_external_approval_id ON approval_external_refs (approval_id);

CREATE TABLE IF NOT EXISTS usage_records (
    id                TEXT PRIMARY KEY,
    session_id        TEXT NOT NULL DEFAULT '',
    run_id            TEXT NOT NULL DEFAULT '',
    model             TEXT NOT NULL DEFAULT '',
    provider          TEXT NOT NULL DEFAULT '',
    prompt_tokens     INTEGER NOT NULL DEFAULT 0,
    completion_tokens INTEGER NOT NULL DEFAULT 0,
    total_tokens      INTEGER NOT NULL DEFAULT 0,
    cost_estimate     REAL NOT NULL DEFAULT 0.0,
    duration_ns       INTEGER NOT NULL DEFAULT 0,
    tool_name         TEXT NOT NULL DEFAULT '',
    tool_call_id      TEXT NOT NULL DEFAULT '',
    record_type       TEXT NOT NULL DEFAULT 'model_call',
    created_at        TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_usage_session ON usage_records (session_id);
CREATE INDEX IF NOT EXISTS idx_usage_model ON usage_records (model);
CREATE INDEX IF NOT EXISTS idx_usage_created_at ON usage_records (created_at);
CREATE INDEX IF NOT EXISTS idx_usage_date ON usage_records (substr(created_at, 1, 10));

CREATE TABLE IF NOT EXISTS events (
    id         TEXT PRIMARY KEY,
    type       TEXT NOT NULL,
    run_id     TEXT NOT NULL DEFAULT '',
    session_id TEXT NOT NULL DEFAULT '',
    timestamp  TEXT NOT NULL,
    attrs      TEXT NOT NULL DEFAULT '{}'
);
CREATE INDEX IF NOT EXISTS idx_events_timestamp ON events (timestamp);
CREATE INDEX IF NOT EXISTS idx_events_type ON events (type);
CREATE INDEX IF NOT EXISTS idx_events_run ON events (run_id) WHERE run_id != '';
CREATE INDEX IF NOT EXISTS idx_events_session ON events (session_id) WHERE session_id != '';

CREATE TABLE IF NOT EXISTS artifacts (
    id           TEXT PRIMARY KEY,
    uri          TEXT NOT NULL,
    kind         TEXT NOT NULL DEFAULT 'tool_output',
    content_type TEXT NOT NULL DEFAULT 'text/plain; charset=utf-8',
    size         INTEGER NOT NULL DEFAULT 0,
    run_id       TEXT NOT NULL DEFAULT '',
    session_id   TEXT NOT NULL DEFAULT '',
    tool_name    TEXT NOT NULL DEFAULT '',
    tool_call_id TEXT NOT NULL DEFAULT '',
    metadata     TEXT NOT NULL DEFAULT '{}',
    created_at   TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_artifacts_session ON artifacts (session_id) WHERE session_id != '';
CREATE INDEX IF NOT EXISTS idx_artifacts_run ON artifacts (run_id) WHERE run_id != '';
CREATE INDEX IF NOT EXISTS idx_artifacts_created_at ON artifacts (created_at);

CREATE TABLE IF NOT EXISTS pairings (
    id              TEXT PRIMARY KEY,
    channel         TEXT NOT NULL,
    user_id         TEXT NOT NULL,
    display_name    TEXT NOT NULL DEFAULT '',
    status          TEXT NOT NULL DEFAULT 'pending',
    code            TEXT NOT NULL DEFAULT '',
    code_expires_at TEXT NOT NULL DEFAULT '',
    verified_at     TEXT NOT NULL DEFAULT '',
    created_at      TEXT NOT NULL,
    UNIQUE(channel, user_id)
);
CREATE INDEX IF NOT EXISTS idx_pairings_code ON pairings (code) WHERE code != '';
CREATE INDEX IF NOT EXISTS idx_pairings_status ON pairings (status);
`

const migrationV2 = `
ALTER TABLE sessions ADD COLUMN revision INTEGER NOT NULL DEFAULT 1;
ALTER TABLE runs ADD COLUMN last_session_revision INTEGER NOT NULL DEFAULT 0;
`

const migrationV3 = `
ALTER TABLE runs ADD COLUMN execution_mode TEXT NOT NULL DEFAULT '';
ALTER TABLE runs ADD COLUMN tool_recovery_count INTEGER NOT NULL DEFAULT 0;
ALTER TABLE runs ADD COLUMN triage TEXT NOT NULL DEFAULT '';
ALTER TABLE runs ADD COLUMN preflight TEXT NOT NULL DEFAULT '';
ALTER TABLE runs ADD COLUMN effective_agent_profile TEXT NOT NULL DEFAULT '';
`

const migrationV4 = `
ALTER TABLE runs ADD COLUMN task_contract TEXT NOT NULL DEFAULT '';
`

const migrationV5 = `
ALTER TABLE sessions ADD COLUMN scope TEXT NOT NULL DEFAULT '{}';
ALTER TABLE runs ADD COLUMN scope TEXT NOT NULL DEFAULT '{}';
`

const migrationV6 = `
ALTER TABLE runs ADD COLUMN governance TEXT NOT NULL DEFAULT '';
`

const migrationV7 = `
SELECT 1;
`

const migrationV8 = `
ALTER TABLE runs ADD COLUMN execution_graph TEXT NOT NULL DEFAULT '';
`

const migrationV9 = `
CREATE TABLE IF NOT EXISTS transcript_events (
    id                  INTEGER PRIMARY KEY,
    session_id          TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    session_revision    INTEGER NOT NULL,
    event_type          TEXT NOT NULL,
    message_count_delta INTEGER NOT NULL DEFAULT 0,
    metadata            TEXT NOT NULL DEFAULT '{}',
    created_at          TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_transcript_events_session
    ON transcript_events (session_id, id);
CREATE INDEX IF NOT EXISTS idx_transcript_events_session_revision
    ON transcript_events (session_id, session_revision);
`

const migrationV10 = `
ALTER TABLE runs ADD COLUMN delegation TEXT NOT NULL DEFAULT '';
`

const migrationV11 = `
ALTER TABLE messages ADD COLUMN seq INTEGER;
UPDATE messages SET seq = rowid WHERE seq IS NULL;
CREATE INDEX IF NOT EXISTS idx_messages_session_seq ON messages (session_id, seq);
`

const migrationV12 = `
ALTER TABLE sessions ADD COLUMN execution_watermark INTEGER NOT NULL DEFAULT 0;
`

const migrationV13 = `
CREATE TABLE IF NOT EXISTS session_episodes (
    id            TEXT PRIMARY KEY,
    session_id    TEXT NOT NULL,
    seq_num       INTEGER NOT NULL,
    status        TEXT NOT NULL DEFAULT 'active',
    started_at    TEXT NOT NULL,
    sealed_at     TEXT,
    msg_seq_start INTEGER,
    msg_seq_end   INTEGER,
    message_count INTEGER DEFAULT 0,
    trigger_reason TEXT,
    metadata      TEXT DEFAULT '{}',
    UNIQUE(session_id, seq_num)
);
CREATE INDEX IF NOT EXISTS idx_session_episodes_session_status
    ON session_episodes (session_id, status, seq_num);
`

const migrationV14 = `
CREATE TABLE IF NOT EXISTS summary_segments (
    id                TEXT PRIMARY KEY,
    session_id        TEXT NOT NULL,
    episode_id        TEXT NOT NULL,
    level             INTEGER NOT NULL DEFAULT 1,
    seq_start         INTEGER NOT NULL,
    seq_end           INTEGER NOT NULL,
    ts_start          TEXT NOT NULL,
    ts_end            TEXT NOT NULL,
    summary_text      TEXT NOT NULL,
    decisions_json    TEXT DEFAULT '[]',
    todos_json        TEXT DEFAULT '[]',
    constraints_json  TEXT DEFAULT '[]',
    entities_json     TEXT DEFAULT '[]',
    artifact_refs     TEXT DEFAULT '[]',
    embedding         BLOB,
    keywords          TEXT DEFAULT '',
    quality_score     REAL DEFAULT 0.0,
    parent_segment_id TEXT,
    created_at        TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_segments_session_level
    ON summary_segments (session_id, level, seq_start);
CREATE INDEX IF NOT EXISTS idx_segments_episode
    ON summary_segments (episode_id, level);
`

const migrationV15 = `
CREATE TABLE IF NOT EXISTS session_state (
    session_id     TEXT NOT NULL,
    key            TEXT NOT NULL,
    category       TEXT NOT NULL,
    value          TEXT NOT NULL,
    status         TEXT NOT NULL DEFAULT 'active',
    source_episode TEXT,
    source_segment TEXT,
    confidence     REAL DEFAULT 1.0,
    created_at     TEXT NOT NULL,
    updated_at     TEXT NOT NULL,
    expires_at     TEXT,
    superseded_by  TEXT,
    PRIMARY KEY (session_id, key)
);
`

const migrationV16 = `
CREATE TABLE IF NOT EXISTS session_episodes_new (
    id             TEXT PRIMARY KEY,
    session_id     TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    seq_num        INTEGER NOT NULL,
    status         TEXT NOT NULL DEFAULT 'active',
    started_at     TEXT NOT NULL,
    sealed_at      TEXT,
    msg_seq_start  INTEGER,
    msg_seq_end    INTEGER,
    message_count  INTEGER DEFAULT 0,
    trigger_reason TEXT,
    metadata       TEXT DEFAULT '{}',
    UNIQUE(session_id, seq_num)
);
INSERT INTO session_episodes_new (
    id, session_id, seq_num, status, started_at, sealed_at,
    msg_seq_start, msg_seq_end, message_count, trigger_reason, metadata
)
SELECT
    id, session_id, seq_num, status, started_at, sealed_at,
    msg_seq_start, msg_seq_end, message_count, trigger_reason, metadata
FROM session_episodes;
DROP TABLE session_episodes;
ALTER TABLE session_episodes_new RENAME TO session_episodes;
CREATE INDEX IF NOT EXISTS idx_session_episodes_session_status
    ON session_episodes (session_id, status, seq_num);

CREATE TABLE IF NOT EXISTS summary_segments_new (
    id                TEXT PRIMARY KEY,
    session_id        TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    episode_id        TEXT NOT NULL REFERENCES session_episodes(id) ON DELETE CASCADE,
    level             INTEGER NOT NULL DEFAULT 1,
    seq_start         INTEGER NOT NULL,
    seq_end           INTEGER NOT NULL,
    ts_start          TEXT NOT NULL,
    ts_end            TEXT NOT NULL,
    summary_text      TEXT NOT NULL,
    decisions_json    TEXT DEFAULT '[]',
    todos_json        TEXT DEFAULT '[]',
    constraints_json  TEXT DEFAULT '[]',
    entities_json     TEXT DEFAULT '[]',
    artifact_refs     TEXT DEFAULT '[]',
    embedding         BLOB,
    keywords          TEXT DEFAULT '',
    quality_score     REAL DEFAULT 0.0,
    parent_segment_id TEXT,
    created_at        TEXT NOT NULL
);
INSERT INTO summary_segments_new (
    id, session_id, episode_id, level, seq_start, seq_end, ts_start, ts_end,
    summary_text, decisions_json, todos_json, constraints_json, entities_json,
    artifact_refs, embedding, keywords, quality_score, parent_segment_id, created_at
)
SELECT
    id, session_id, episode_id, level, seq_start, seq_end, ts_start, ts_end,
    summary_text, decisions_json, todos_json, constraints_json, entities_json,
    artifact_refs, embedding, keywords, quality_score, parent_segment_id, created_at
FROM summary_segments;
DROP TABLE summary_segments;
ALTER TABLE summary_segments_new RENAME TO summary_segments;
CREATE INDEX IF NOT EXISTS idx_segments_session_level
    ON summary_segments (session_id, level, seq_start);
CREATE INDEX IF NOT EXISTS idx_segments_episode
    ON summary_segments (episode_id, level);

CREATE TABLE IF NOT EXISTS session_state_new (
    session_id     TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    key            TEXT NOT NULL,
    category       TEXT NOT NULL,
    value          TEXT NOT NULL,
    status         TEXT NOT NULL DEFAULT 'active',
    source_episode TEXT,
    source_segment TEXT,
    confidence     REAL DEFAULT 1.0,
    created_at     TEXT NOT NULL,
    updated_at     TEXT NOT NULL,
    expires_at     TEXT,
    superseded_by  TEXT,
    PRIMARY KEY (session_id, key)
);
INSERT INTO session_state_new (
    session_id, key, category, value, status,
    source_episode, source_segment, confidence,
    created_at, updated_at, expires_at, superseded_by
)
SELECT
    session_id, key, category, value, status,
    source_episode, source_segment, confidence,
    created_at, updated_at, expires_at, superseded_by
FROM session_state;
DROP TABLE session_state;
ALTER TABLE session_state_new RENAME TO session_state;
`

const migrationV17 = `
ALTER TABLE runs ADD COLUMN workflow_state TEXT NOT NULL DEFAULT '';
`

const migrationV18 = `
ALTER TABLE usage_records ADD COLUMN workflow_id TEXT NOT NULL DEFAULT '';
ALTER TABLE usage_records ADD COLUMN parent_run_id TEXT NOT NULL DEFAULT '';
ALTER TABLE usage_records ADD COLUMN continuation_index INTEGER NOT NULL DEFAULT 0;
CREATE INDEX IF NOT EXISTS idx_usage_workflow ON usage_records (workflow_id) WHERE workflow_id != '';
`

const migrationV19 = `
ALTER TABLE runs ADD COLUMN semantic_signal TEXT NOT NULL DEFAULT '';
`

const migrationV20 = `
ALTER TABLE sessions ADD COLUMN pinned_facts TEXT NOT NULL DEFAULT '[]';
`

func runMigrations(db *sql.DB) error {
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (
		version    INTEGER PRIMARY KEY,
		applied_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%f', 'now', 'utc'))
	)`); err != nil {
		return err
	}

	var current int
	if err := db.QueryRow("SELECT COALESCE(MAX(version), 0) FROM schema_migrations").Scan(&current); err != nil {
		return err
	}

	for _, m := range migrations {
		if m.Version <= current {
			continue
		}
		tx, err := db.BeginTx(context.Background(), nil)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(m.SQL); err != nil {
			tx.Rollback()
			return fmt.Errorf("migration v%d: %w", m.Version, err)
		}
		if _, err := tx.Exec("INSERT INTO schema_migrations (version) VALUES (?)", m.Version); err != nil {
			tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}

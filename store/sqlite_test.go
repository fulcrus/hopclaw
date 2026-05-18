package store

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/fulcrus/hopclaw/agent"
	"github.com/fulcrus/hopclaw/approval"
	"github.com/fulcrus/hopclaw/contextengine"
	"github.com/fulcrus/hopclaw/durablefact"
	"github.com/fulcrus/hopclaw/eventbus"
	"github.com/fulcrus/hopclaw/usage"
)

func openRawTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := OpenDB(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("OpenDB: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func seedSessionMessages(t *testing.T, db *sql.DB, sessionID string, startSeq, count int) {
	t.Helper()

	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatalf("BeginTx() error = %v", err)
	}
	defer tx.Rollback() //nolint:errcheck

	stmt, err := tx.Prepare(`
		INSERT INTO messages (
			session_id, seq, role, content, content_blocks, name, tool_call_id, tool_calls, metadata, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`)
	if err != nil {
		t.Fatalf("Prepare() error = %v", err)
	}
	defer stmt.Close()

	for i := 0; i < count; i++ {
		seq := startSeq + i
		if _, err := stmt.Exec(
			sessionID,
			seq,
			string(contextengine.RoleUser),
			fmt.Sprintf("message-%d", seq),
			"[]",
			"",
			"",
			"[]",
			"{}",
			formatTime(time.Unix(int64(seq), 0).UTC()),
		); err != nil {
			t.Fatalf("insert message %d: %v", seq, err)
		}
	}

	if err := tx.Commit(); err != nil {
		t.Fatalf("Commit() error = %v", err)
	}
}

func migrateSQLiteSchemaUpTo(t *testing.T, path string, maxVersion int) {
	t.Helper()

	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("sql.Open() error = %v", err)
	}
	defer db.Close()

	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (
		version    INTEGER PRIMARY KEY,
		applied_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%f', 'now'))
	)`); err != nil {
		t.Fatalf("create schema_migrations: %v", err)
	}

	for _, m := range migrations {
		if m.Version > maxVersion {
			break
		}
		if _, err := db.Exec(m.SQL); err != nil {
			t.Fatalf("apply migration v%d: %v", m.Version, err)
		}
		if _, err := db.Exec(`INSERT INTO schema_migrations (version) VALUES (?)`, m.Version); err != nil {
			t.Fatalf("record migration v%d: %v", m.Version, err)
		}
	}
}

func migrateLegacySQLiteSchema(t *testing.T, path string) {
	t.Helper()
	migrateSQLiteSchemaUpTo(t, path, 10)
}

type sqliteForeignKey struct {
	table    string
	from     string
	to       string
	onDelete string
}

func foreignKeysForTable(t *testing.T, db *sql.DB, table string) []sqliteForeignKey {
	t.Helper()

	rows, err := db.Query(fmt.Sprintf(`PRAGMA foreign_key_list(%s)`, table))
	if err != nil {
		t.Fatalf("foreign_key_list(%s): %v", table, err)
	}
	defer rows.Close()

	var out []sqliteForeignKey
	for rows.Next() {
		var (
			id, seq            int
			referencedTable    string
			fromColumn         string
			toColumn           string
			onUpdate, onDelete string
			match              string
		)
		if err := rows.Scan(&id, &seq, &referencedTable, &fromColumn, &toColumn, &onUpdate, &onDelete, &match); err != nil {
			t.Fatalf("scan foreign key(%s): %v", table, err)
		}
		out = append(out, sqliteForeignKey{
			table:    referencedTable,
			from:     fromColumn,
			to:       toColumn,
			onDelete: onDelete,
		})
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate foreign keys(%s): %v", table, err)
	}
	return out
}

func TestSQLitePairingListFailsClosedOnScanError(t *testing.T) {
	t.Parallel()

	db := openRawTestDB(t)
	if _, err := db.Exec(`INSERT INTO pairings (id, channel, user_id, display_name, status, code, code_expires_at, verified_at, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		"pair-1", "slack", "user-1", "Alice", "pending", "123456", "bad-time", "", "2026-01-01T00:00:00Z",
	); err != nil {
		t.Fatalf("insert pairing: %v", err)
	}

	store := NewSQLitePairingStore(db)
	if _, err := store.List(); err == nil {
		t.Fatal("expected List() to fail on invalid row data")
	}
}

func TestParseTimeRejectsInvalidTimestamp(t *testing.T) {
	t.Parallel()

	if _, err := parseTime("bad-time", "sqlite test", "row-1", "created_at"); err == nil {
		t.Fatal("expected parseTime() to reject invalid timestamp")
	}
	if ts, err := parseTime("", "sqlite test", "row-1", "created_at"); err != nil || !ts.IsZero() {
		t.Fatalf("parseTime(empty) = (%v, %v), want zero time and nil error", ts, err)
	}
}

func TestRecoverMaxIDCounterFallsBackWhenQueryFails(t *testing.T) {
	t.Parallel()

	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "broken.db"))
	if err != nil {
		t.Fatalf("sql.Open() error = %v", err)
	}
	defer db.Close()

	first := recoverMaxIDCounter(db, "missing_table", "sess-")
	second := recoverMaxIDCounter(db, "missing_table", "sess-")
	if first == 0 {
		t.Fatal("recoverMaxIDCounter() should return a non-zero fallback seed")
	}
	if second <= first {
		t.Fatalf("recoverMaxIDCounter() = %d then %d, want monotonic fallback seeds", first, second)
	}
}

func TestRecoverMaxIDRejectsUnsupportedTableName(t *testing.T) {
	t.Parallel()

	db := openRawTestDB(t)
	if _, err := recoverMaxID(db, "sessions; DROP TABLE sessions", "sess-"); err == nil {
		t.Fatal("recoverMaxID() error = nil, want unsupported table failure")
	}
}

func TestSQLiteSessionSaveRejectsUnserializableMetadata(t *testing.T) {
	t.Parallel()

	db := openRawTestDB(t)
	sessions := NewSQLiteSessionStore(db)
	ctx := context.Background()

	session, err := sessions.GetOrCreate(ctx, "session-bad-metadata", "gpt-4.1")
	if err != nil {
		t.Fatalf("GetOrCreate() error = %v", err)
	}
	loaded, release, err := sessions.LoadForExecution(ctx, session.ID)
	if err != nil {
		t.Fatalf("LoadForExecution() error = %v", err)
	}
	defer release()

	loaded.Metadata = map[string]any{"bad": func() {}}
	loaded.UpdatedAt = time.Now().UTC()
	if err := sessions.Save(ctx, loaded); err == nil {
		t.Fatal("Save() error = nil, want metadata marshal failure")
	} else if !strings.Contains(err.Error(), "marshal session metadata") {
		t.Fatalf("Save() error = %v", err)
	}
}

func TestSQLiteApprovalStoreCreateRejectsUnserializableMetadata(t *testing.T) {
	t.Parallel()

	db := openRawTestDB(t)
	store := NewSQLiteApprovalStore(db)

	_, err := store.Create(context.Background(), approval.Ticket{
		RunID:     "run-1",
		SessionID: "sess-1",
		Kind:      approval.KindToolCalls,
		Metadata: map[string]any{
			"bad": func() {},
		},
	})
	if err == nil {
		t.Fatal("Create() error = nil, want metadata marshal failure")
	}
	if !strings.Contains(err.Error(), "marshal approval metadata") {
		t.Fatalf("Create() error = %v", err)
	}
}

func TestSQLiteEventSinkHandleRejectsUnserializableAttrs(t *testing.T) {
	t.Parallel()

	db := openRawTestDB(t)
	sink := NewSQLiteEventSink(db)

	err := sink.Handle(context.Background(), eventbus.Event{
		Type: eventbus.EventRunFailed,
		Attrs: map[string]any{
			"bad": func() {},
		},
	})
	if err == nil {
		t.Fatal("Handle() error = nil, want attrs marshal failure")
	}
	if !strings.Contains(err.Error(), "marshal event attrs") {
		t.Fatalf("Handle() error = %v", err)
	}
}

func TestSQLiteEventReplayFailsClosedOnInvalidTimestamp(t *testing.T) {
	t.Parallel()

	db := openRawTestDB(t)
	if _, err := db.Exec(`INSERT INTO events (id, type, run_id, session_id, timestamp, attrs)
		VALUES (?, ?, ?, ?, ?, ?)`,
		"evt-bad", "run.failed", "run-1", "sess-1", "bad-time", `{}`,
	); err != nil {
		t.Fatalf("insert event: %v", err)
	}

	sink := NewSQLiteEventSink(db)
	if _, err := sink.Replay(); err == nil {
		t.Fatal("expected Replay() to fail on invalid event timestamp")
	}
}

func TestConfigStoreGetProviderFailsClosedOnInvalidTimestamp(t *testing.T) {
	t.Parallel()

	db := openRawTestDB(t)
	store, err := NewConfigStore(db)
	if err != nil {
		t.Fatalf("NewConfigStore() error = %v", err)
	}
	if _, err := db.Exec(`INSERT INTO durable_facts (
		key, fact_class, view_type, namespace, scope_key, name, label, value, value_type,
		source, managed, confidence, review_required, tags, previous_values, evidence, metadata,
		created_at, updated_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		"config.provider.provider-bad", "system_config", "config_provider", "provider", "", "provider-bad", "provider-bad",
		`{"api":"openai-completions","timeout_sec":0,"headers":"{}","enabled":true}`, "json",
		"api", 1, 1.0, 0, "[]", "[]", "[]", `{}`, "bad-time", "2026-01-01T00:00:00Z",
	); err != nil {
		t.Fatalf("insert provider: %v", err)
	}
	if _, err := store.GetProvider(context.Background(), "provider-bad"); err == nil {
		t.Fatal("expected GetProvider() to fail on invalid created_at timestamp")
	}
}

func TestConfigStoreMigratesLegacyRowsIntoDurableFacts(t *testing.T) {
	t.Parallel()

	db := openRawTestDB(t)
	if err := EnsureConfigSchema(db); err != nil {
		t.Fatalf("EnsureConfigSchema() error = %v", err)
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := db.Exec(`INSERT INTO config_providers (
		name, api, base_url, region, api_key, api_keys, access_key_id, secret_key, session_token,
		default_model, timeout_sec, headers, enabled, source, yaml_hash, created_at, updated_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		"openai", "openai-responses", "", "", "secret", `[]`, "", "", "", "gpt-4.1", 30, `{}`, 1, "yaml", "hash-provider", now, now,
	); err != nil {
		t.Fatalf("insert provider: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO config_channels (name, config, enabled, source, yaml_hash, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		"slack", `{"bot_token":"x"}`, 1, "api", "hash-channel", now, now,
	); err != nil {
		t.Fatalf("insert channel: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO dynamic_settings (key, value, source, yaml_hash, updated_at)
		VALUES (?, ?, ?, ?, ?)`,
		"section.agent", `{"default_model":"openai/gpt-4.1"}`, "yaml", "hash-setting", now,
	); err != nil {
		t.Fatalf("insert setting: %v", err)
	}

	store, err := NewConfigStore(db)
	if err != nil {
		t.Fatalf("NewConfigStore() error = %v", err)
	}

	provider, err := store.GetProvider(context.Background(), "openai")
	if err != nil {
		t.Fatalf("GetProvider() error = %v", err)
	}
	if provider.API != "openai-responses" || provider.DefaultModel != "gpt-4.1" {
		t.Fatalf("unexpected provider = %#v", provider)
	}

	channel, err := store.GetChannel(context.Background(), "slack")
	if err != nil {
		t.Fatalf("GetChannel() error = %v", err)
	}
	if channel.Config != `{"bot_token":"x"}` {
		t.Fatalf("unexpected channel = %#v", channel)
	}

	setting, err := store.GetSetting(context.Background(), "section.agent")
	if err != nil {
		t.Fatalf("GetSetting() error = %v", err)
	}
	if setting.Value != `{"default_model":"openai/gpt-4.1"}` {
		t.Fatalf("unexpected setting = %#v", setting)
	}

	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM durable_facts WHERE fact_class = ?`, string(durablefact.FactClassSystemConfig)).Scan(&count); err != nil {
		t.Fatalf("count durable facts: %v", err)
	}
	if count < 3 {
		t.Fatalf("durable system_config facts = %d, want at least 3", count)
	}
}

func TestConfigStoreListsDurableViews(t *testing.T) {
	t.Parallel()

	db := openRawTestDB(t)
	store, err := NewConfigStore(db)
	if err != nil {
		t.Fatalf("NewConfigStore() error = %v", err)
	}
	if err := store.UpsertProvider(context.Background(), &ProviderConfigRow{
		Name:         "openai",
		API:          "openai-responses",
		DefaultModel: "gpt-4.1",
		Source:       ConfigSourceAPI,
		CreatedAt:    time.Now().UTC().Add(-time.Minute),
		UpdatedAt:    time.Now().UTC(),
	}); err != nil {
		t.Fatalf("UpsertProvider() error = %v", err)
	}

	configViews, err := store.ListConfigViews(context.Background(), durablefact.Filter{})
	if err != nil {
		t.Fatalf("ListConfigViews() error = %v", err)
	}
	if len(configViews) != 1 || configViews[0].Kind != durablefact.ConfigViewKindProvider || configViews[0].Name != "openai" {
		t.Fatalf("unexpected config views: %#v", configViews)
	}

	operatorViews, err := store.ListOperatorViews(context.Background(), durablefact.Filter{})
	if err != nil {
		t.Fatalf("ListOperatorViews() error = %v", err)
	}
	if len(operatorViews) != 1 || operatorViews[0].ViewType != durablefact.ViewTypeConfigProvider {
		t.Fatalf("unexpected operator views: %#v", operatorViews)
	}
}

func TestSQLiteSessionGetOrCreate(t *testing.T) {
	t.Parallel()
	db, err := OpenDB(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	s := NewSQLiteSessionStore(db)
	ctx := context.Background()

	sess1, err := s.GetOrCreate(ctx, "feishu:chat-1", "gpt-4")
	if err != nil {
		t.Fatal(err)
	}
	if sess1.Revision != 1 {
		t.Fatalf("revision = %d, want 1", sess1.Revision)
	}
	if sess1.Key != "feishu:chat-1" {
		t.Fatalf("key = %q", sess1.Key)
	}
	if sess1.Model != "gpt-4" {
		t.Fatalf("model = %q", sess1.Model)
	}

	// Same key returns same session.
	sess2, err := s.GetOrCreate(ctx, "feishu:chat-1", "gpt-4o")
	if err != nil {
		t.Fatal(err)
	}
	if sess2.ID != sess1.ID {
		t.Fatalf("expected same ID, got %q vs %q", sess2.ID, sess1.ID)
	}
}

func TestSQLiteSessionAppendAndLoad(t *testing.T) {
	t.Parallel()
	db, err := OpenDB(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	s := NewSQLiteSessionStore(db)
	ctx := context.Background()

	sess, _ := s.GetOrCreate(ctx, "test:load", "model-1")
	if err := s.AppendUserMessage(ctx, sess.ID, agent.IncomingMessage{
		Content: "hello",
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.AppendUserMessage(ctx, sess.ID, agent.IncomingMessage{
		Content: "world",
	}); err != nil {
		t.Fatal(err)
	}

	loaded, unlock, err := s.LoadForExecution(ctx, sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	if loaded.Revision != 3 {
		t.Fatalf("revision = %d, want 3", loaded.Revision)
	}

	if len(loaded.Messages) != 2 {
		t.Fatalf("messages = %d, want 2", len(loaded.Messages))
	}
	if loaded.Messages[0].Content != "hello" {
		t.Fatalf("msg[0] = %q", loaded.Messages[0].Content)
	}
	if loaded.Messages[1].Content != "world" {
		t.Fatalf("msg[1] = %q", loaded.Messages[1].Content)
	}
}

func TestSQLiteMigrationsBackfillMessageSeqAndExecutionWatermark(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "legacy.db")
	migrateLegacySQLiteSchema(t, path)

	legacyDB, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("sql.Open() error = %v", err)
	}
	now := formatTime(time.Now().UTC())
	if _, err := legacyDB.Exec(`
		INSERT INTO sessions (
			id, key, model, summary, summary_at, metadata, skill_snapshot, created_at, updated_at, revision, scope
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, "sess-legacy", "legacy:key", "m1", "", "", "{}", "{}", now, now, 3, "{}"); err != nil {
		legacyDB.Close()
		t.Fatalf("insert legacy session: %v", err)
	}
	if _, err := legacyDB.Exec(`
		INSERT INTO messages (
			session_id, role, content, content_blocks, name, tool_call_id, tool_calls, metadata, created_at
		) VALUES
			(?, ?, ?, ?, ?, ?, ?, ?, ?),
			(?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		"sess-legacy", string(contextengine.RoleUser), "first", "[]", "", "", "[]", "{}", now,
		"sess-legacy", string(contextengine.RoleAssistant), "second", "[]", "", "", "[]", "{}", now,
	); err != nil {
		legacyDB.Close()
		t.Fatalf("insert legacy messages: %v", err)
	}
	if err := legacyDB.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	db, err := OpenDB(path)
	if err != nil {
		t.Fatalf("OpenDB() error = %v", err)
	}
	defer db.Close()

	rows, err := db.Query(`SELECT rowid, seq FROM messages WHERE session_id = ? ORDER BY rowid`, "sess-legacy")
	if err != nil {
		t.Fatalf("query messages: %v", err)
	}
	defer rows.Close()

	var rowCount int
	for rows.Next() {
		var rowid, seq int64
		if err := rows.Scan(&rowid, &seq); err != nil {
			t.Fatalf("scan migrated message: %v", err)
		}
		rowCount++
		if seq != rowid {
			t.Fatalf("message rowid=%d seq=%d, want seq backfilled from rowid", rowid, seq)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate migrated messages: %v", err)
	}
	if rowCount != 2 {
		t.Fatalf("migrated message count = %d, want 2", rowCount)
	}

	var watermark int64
	if err := db.QueryRow(`SELECT execution_watermark FROM sessions WHERE id = ?`, "sess-legacy").Scan(&watermark); err != nil {
		t.Fatalf("query execution_watermark: %v", err)
	}
	if watermark != 0 {
		t.Fatalf("execution_watermark = %d, want 0", watermark)
	}
}

func TestSQLiteMigrationV16AddsCascadeForeignKeys(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "legacy-v15.db")
	migrateSQLiteSchemaUpTo(t, path, 15)

	legacyDB, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("sql.Open() error = %v", err)
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := legacyDB.Exec(`INSERT INTO sessions (id, key, model, revision, scope, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		"sess-v16", "legacy-v16", "gpt-4.1", 1, "{}", now, now,
	); err != nil {
		legacyDB.Close()
		t.Fatalf("insert session: %v", err)
	}
	if _, err := legacyDB.Exec(`INSERT INTO session_episodes (
		id, session_id, seq_num, status, started_at, sealed_at,
		msg_seq_start, msg_seq_end, message_count, trigger_reason, metadata
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		"ep-v16", "sess-v16", 1, "active", now, nil, 1, 3, 3, "default", "{}",
	); err != nil {
		legacyDB.Close()
		t.Fatalf("insert episode: %v", err)
	}
	if _, err := legacyDB.Exec(`INSERT INTO summary_segments (
		id, session_id, episode_id, level, seq_start, seq_end, ts_start, ts_end,
		summary_text, decisions_json, todos_json, constraints_json, entities_json,
		artifact_refs, embedding, keywords, quality_score, parent_segment_id, created_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		"seg-v16", "sess-v16", "ep-v16", 1, 1, 3, now, now,
		"legacy summary", "[]", "[]", "[]", "[]", "[]", nil, "", 0.8, nil, now,
	); err != nil {
		legacyDB.Close()
		t.Fatalf("insert segment: %v", err)
	}
	if _, err := legacyDB.Exec(`INSERT INTO session_state (
		session_id, key, category, value, status,
		source_episode, source_segment, confidence,
		created_at, updated_at, expires_at, superseded_by
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		"sess-v16", "decision-1", "decision", "keep cascades", "active",
		"ep-v16", "seg-v16", 1.0, now, now, nil, nil,
	); err != nil {
		legacyDB.Close()
		t.Fatalf("insert session state: %v", err)
	}
	if err := legacyDB.Close(); err != nil {
		t.Fatalf("close legacy db: %v", err)
	}

	db, err := OpenDB(path)
	if err != nil {
		t.Fatalf("OpenDB() error = %v", err)
	}
	defer db.Close()

	sessionEpisodeFKs := foreignKeysForTable(t, db, "session_episodes")
	if len(sessionEpisodeFKs) != 1 || sessionEpisodeFKs[0].table != "sessions" || sessionEpisodeFKs[0].from != "session_id" || sessionEpisodeFKs[0].to != "id" || sessionEpisodeFKs[0].onDelete != "CASCADE" {
		t.Fatalf("session_episodes foreign keys = %#v", sessionEpisodeFKs)
	}
	summarySegmentFKs := foreignKeysForTable(t, db, "summary_segments")
	if len(summarySegmentFKs) != 2 {
		t.Fatalf("summary_segments foreign keys = %#v", summarySegmentFKs)
	}
	var foundSessionFK, foundEpisodeFK bool
	for _, fk := range summarySegmentFKs {
		if fk.table == "sessions" && fk.from == "session_id" && fk.to == "id" && fk.onDelete == "CASCADE" {
			foundSessionFK = true
		}
		if fk.table == "session_episodes" && fk.from == "episode_id" && fk.to == "id" && fk.onDelete == "CASCADE" {
			foundEpisodeFK = true
		}
	}
	if !foundSessionFK || !foundEpisodeFK {
		t.Fatalf("summary_segments foreign keys = %#v", summarySegmentFKs)
	}
	sessionStateFKs := foreignKeysForTable(t, db, "session_state")
	if len(sessionStateFKs) != 1 || sessionStateFKs[0].table != "sessions" || sessionStateFKs[0].from != "session_id" || sessionStateFKs[0].to != "id" || sessionStateFKs[0].onDelete != "CASCADE" {
		t.Fatalf("session_state foreign keys = %#v", sessionStateFKs)
	}

	if _, err := db.Exec(`DELETE FROM session_episodes WHERE id = ?`, "ep-v16"); err != nil {
		t.Fatalf("delete episode: %v", err)
	}
	var segmentCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM summary_segments WHERE session_id = ?`, "sess-v16").Scan(&segmentCount); err != nil {
		t.Fatalf("count segments after episode delete: %v", err)
	}
	if segmentCount != 0 {
		t.Fatalf("summary_segments after episode delete = %d, want 0", segmentCount)
	}

	if _, err := db.Exec(`INSERT INTO session_episodes (
		id, session_id, seq_num, status, started_at, sealed_at,
		msg_seq_start, msg_seq_end, message_count, trigger_reason, metadata
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		"ep-v16-2", "sess-v16", 2, "active", now, nil, 4, 5, 2, "manual", "{}",
	); err != nil {
		t.Fatalf("reinsert episode: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO summary_segments (
		id, session_id, episode_id, level, seq_start, seq_end, ts_start, ts_end,
		summary_text, decisions_json, todos_json, constraints_json, entities_json,
		artifact_refs, embedding, keywords, quality_score, parent_segment_id, created_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		"seg-v16-2", "sess-v16", "ep-v16-2", 1, 4, 5, now, now,
		"legacy summary 2", "[]", "[]", "[]", "[]", "[]", nil, "", 0.7, nil, now,
	); err != nil {
		t.Fatalf("reinsert segment: %v", err)
	}
	if _, err := db.Exec(`DELETE FROM sessions WHERE id = ?`, "sess-v16"); err != nil {
		t.Fatalf("delete session: %v", err)
	}

	var (
		episodeCount int
		stateCount   int
	)
	if err := db.QueryRow(`SELECT COUNT(*) FROM session_episodes WHERE session_id = ?`, "sess-v16").Scan(&episodeCount); err != nil {
		t.Fatalf("count episodes after session delete: %v", err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM session_state WHERE session_id = ?`, "sess-v16").Scan(&stateCount); err != nil {
		t.Fatalf("count state after session delete: %v", err)
	}
	if episodeCount != 0 {
		t.Fatalf("session_episodes after session delete = %d, want 0", episodeCount)
	}
	if stateCount != 0 {
		t.Fatalf("session_state after session delete = %d, want 0", stateCount)
	}
}

func TestSQLiteSchemaMigrationsUseUTCDefault(t *testing.T) {
	t.Parallel()

	db := openRawTestDB(t)

	var createSQL string
	if err := db.QueryRow(`SELECT sql FROM sqlite_master WHERE type = 'table' AND name = 'schema_migrations'`).Scan(&createSQL); err != nil {
		t.Fatalf("query schema_migrations DDL: %v", err)
	}
	if !strings.Contains(createSQL, "'utc'") {
		t.Fatalf("schema_migrations DDL = %q, want explicit utc modifier", createSQL)
	}
}

func TestSQLiteMigrationV20AddsPinnedFactsColumn(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "legacy-v19.db")
	migrateSQLiteSchemaUpTo(t, path, 19)

	legacyDB, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("sql.Open() error = %v", err)
	}
	now := formatTime(time.Now().UTC())
	if _, err := legacyDB.Exec(`INSERT INTO sessions (
		id, key, model, revision, summary, summary_at, scope, metadata, skill_snapshot, created_at, updated_at, execution_watermark
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		"sess-v20", "legacy-v20", "gpt-4.1", 1, "", "", "{}", "{}", "{}", now, now, 0,
	); err != nil {
		legacyDB.Close()
		t.Fatalf("insert legacy session: %v", err)
	}
	if err := legacyDB.Close(); err != nil {
		t.Fatalf("close legacy db: %v", err)
	}

	db, err := OpenDB(path)
	if err != nil {
		t.Fatalf("OpenDB() error = %v", err)
	}
	defer db.Close()

	var pinnedFactsJSON string
	if err := db.QueryRow(`SELECT pinned_facts FROM sessions WHERE id = ?`, "sess-v20").Scan(&pinnedFactsJSON); err != nil {
		t.Fatalf("query pinned_facts: %v", err)
	}
	if pinnedFactsJSON != "[]" {
		t.Fatalf("pinned_facts = %q, want []", pinnedFactsJSON)
	}
}

func TestSQLiteLoadExecutionSnapshotLoadsHotTailOnly(t *testing.T) {
	t.Parallel()

	db := openRawTestDB(t)
	sessions := NewSQLiteSessionStore(db)
	ctx := context.Background()

	sess, err := sessions.GetOrCreate(ctx, "test:hot-tail", "m1")
	if err != nil {
		t.Fatalf("GetOrCreate() error = %v", err)
	}
	seedSessionMessages(t, db, sess.ID, 1, 10000)
	if _, err := db.Exec(`UPDATE sessions SET execution_watermark = ? WHERE id = ?`, 9980, sess.ID); err != nil {
		t.Fatalf("update execution_watermark: %v", err)
	}

	snapshot, unlock, err := sessions.LoadExecutionSnapshot(ctx, sess.ID)
	if err != nil {
		t.Fatalf("LoadExecutionSnapshot() error = %v", err)
	}
	if snapshot.Session.MessageCount != 10000 {
		unlock()
		t.Fatalf("MessageCount = %d, want 10000", snapshot.Session.MessageCount)
	}
	if snapshot.Session.ExecutionWatermark != 9980 {
		unlock()
		t.Fatalf("ExecutionWatermark = %d, want 9980", snapshot.Session.ExecutionWatermark)
	}
	if len(snapshot.Session.Messages) != 20 {
		unlock()
		t.Fatalf("len(Messages) = %d, want 20", len(snapshot.Session.Messages))
	}
	if len(snapshot.Session.LoadedMessageSeqs) != 20 {
		unlock()
		t.Fatalf("len(LoadedMessageSeqs) = %d, want 20", len(snapshot.Session.LoadedMessageSeqs))
	}
	if snapshot.Session.Messages[0].Content != "message-9981" || snapshot.Session.Messages[19].Content != "message-10000" {
		unlock()
		t.Fatalf("loaded hot tail boundaries = [%q ... %q]", snapshot.Session.Messages[0].Content, snapshot.Session.Messages[19].Content)
	}
	if snapshot.Session.LoadedMessageSeqs[0] != 9981 || snapshot.Session.LoadedMessageSeqs[19] != 10000 {
		unlock()
		t.Fatalf("loaded seq boundaries = [%d ... %d], want [9981 ... 10000]",
			snapshot.Session.LoadedMessageSeqs[0], snapshot.Session.LoadedMessageSeqs[19])
	}
	unlock()

	loaded, release, err := sessions.LoadForExecution(ctx, sess.ID)
	if err != nil {
		t.Fatalf("LoadForExecution() error = %v", err)
	}
	defer release()
	if len(loaded.Messages) != 20 {
		t.Fatalf("LoadForExecution len(Messages) = %d, want 20", len(loaded.Messages))
	}
	if loaded.Messages[0].Content != "message-9981" || loaded.Messages[19].Content != "message-10000" {
		t.Fatalf("LoadForExecution boundaries = [%q ... %q]", loaded.Messages[0].Content, loaded.Messages[19].Content)
	}
}

func TestSQLiteSessionCompactAndSaveAdvanceExecutionWatermark(t *testing.T) {
	t.Parallel()

	db := openRawTestDB(t)
	sessions := NewSQLiteSessionStore(db)
	ctx := context.Background()

	sess, err := sessions.GetOrCreate(ctx, "test:compact-watermark", "m1")
	if err != nil {
		t.Fatalf("GetOrCreate() error = %v", err)
	}
	for i := 1; i <= 6; i++ {
		if err := sessions.AppendUserMessage(ctx, sess.ID, agent.IncomingMessage{Content: fmt.Sprintf("message-%d", i)}); err != nil {
			t.Fatalf("AppendUserMessage(%d) error = %v", i, err)
		}
	}

	loaded, unlock, err := sessions.LoadForExecution(ctx, sess.ID)
	if err != nil {
		t.Fatalf("LoadForExecution() error = %v", err)
	}

	engine := contextengine.NewSlidingWindowEngine(contextengine.Config{
		CompactKeepLastN: 2,
	}, nil)
	if err := engine.Compact(ctx, &loaded.Session, contextengine.CompactManual); err != nil {
		unlock()
		t.Fatalf("Compact() error = %v", err)
	}
	if loaded.ExecutionWatermark != 4 {
		unlock()
		t.Fatalf("ExecutionWatermark after Compact() = %d, want 4", loaded.ExecutionWatermark)
	}
	if len(loaded.Messages) != 2 {
		unlock()
		t.Fatalf("len(Messages) after Compact() = %d, want 2", len(loaded.Messages))
	}
	loaded.UpdatedAt = time.Now().UTC()
	if err := sessions.Save(ctx, loaded); err != nil {
		unlock()
		t.Fatalf("Save() after Compact() error = %v", err)
	}
	unlock()

	reloaded, release, err := sessions.LoadExecutionSnapshot(ctx, sess.ID)
	if err != nil {
		t.Fatalf("LoadExecutionSnapshot() error = %v", err)
	}
	if reloaded.Session.ExecutionWatermark != 4 {
		release()
		t.Fatalf("reloaded ExecutionWatermark = %d, want 4", reloaded.Session.ExecutionWatermark)
	}
	if reloaded.Session.MessageCount != 6 {
		release()
		t.Fatalf("reloaded MessageCount = %d, want 6", reloaded.Session.MessageCount)
	}
	if len(reloaded.Session.Messages) != 2 || reloaded.Session.Messages[0].Content != "message-5" || reloaded.Session.Messages[1].Content != "message-6" {
		release()
		t.Fatalf("reloaded hot tail = %#v", reloaded.Session.Messages)
	}

	reloaded.Session.Messages = append(reloaded.Session.Messages, contextengine.Message{
		Role:      contextengine.RoleAssistant,
		Content:   "message-7",
		CreatedAt: time.Now().UTC(),
	})
	reloaded.Session.UpdatedAt = time.Now().UTC()
	if err := sessions.Save(ctx, reloaded.Session); err != nil {
		release()
		t.Fatalf("Save() append after watermark error = %v", err)
	}
	release()

	finalSnapshot, done, err := sessions.LoadExecutionSnapshot(ctx, sess.ID)
	if err != nil {
		t.Fatalf("LoadExecutionSnapshot(final) error = %v", err)
	}
	defer done()

	if finalSnapshot.Session.ExecutionWatermark != 4 {
		t.Fatalf("final ExecutionWatermark = %d, want 4", finalSnapshot.Session.ExecutionWatermark)
	}
	if finalSnapshot.Session.MessageCount != 7 {
		t.Fatalf("final MessageCount = %d, want 7", finalSnapshot.Session.MessageCount)
	}
	if len(finalSnapshot.Session.Messages) != 3 {
		t.Fatalf("final len(Messages) = %d, want 3", len(finalSnapshot.Session.Messages))
	}
	if finalSnapshot.Session.Messages[0].Content != "message-5" ||
		finalSnapshot.Session.Messages[1].Content != "message-6" ||
		finalSnapshot.Session.Messages[2].Content != "message-7" {
		t.Fatalf("final hot tail = %#v", finalSnapshot.Session.Messages)
	}

	var (
		totalRows int
		watermark int64
	)
	if err := db.QueryRow(`SELECT COUNT(*) FROM messages WHERE session_id = ?`, sess.ID).Scan(&totalRows); err != nil {
		t.Fatalf("count messages: %v", err)
	}
	if err := db.QueryRow(`SELECT execution_watermark FROM sessions WHERE id = ?`, sess.ID).Scan(&watermark); err != nil {
		t.Fatalf("query execution_watermark: %v", err)
	}
	if totalRows != 7 {
		t.Fatalf("persisted message row count = %d, want 7", totalRows)
	}
	if watermark != 4 {
		t.Fatalf("persisted execution_watermark = %d, want 4", watermark)
	}
}

func TestSQLiteScopeFiltersSessionsAndRuns(t *testing.T) {
	t.Parallel()

	db := openRawTestDB(t)
	ctx := context.Background()
	sessions := NewSQLiteSessionStore(db)
	runs := NewSQLiteRunStore(db)

	sessionA, err := sessions.GetOrCreate(ctx, "scope:a", "model-a")
	if err != nil {
		t.Fatalf("GetOrCreate(sessionA) error = %v", err)
	}
	if err := sessions.AppendUserMessage(ctx, sessionA.ID, agent.IncomingMessage{
		Content: "tenant a",
	}); err != nil {
		t.Fatalf("AppendUserMessage(sessionA) error = %v", err)
	}
	sessionB, err := sessions.GetOrCreate(ctx, "scope:b", "model-b")
	if err != nil {
		t.Fatalf("GetOrCreate(sessionB) error = %v", err)
	}
	if err := sessions.AppendUserMessage(ctx, sessionB.ID, agent.IncomingMessage{
		Content: "tenant b",
	}); err != nil {
		t.Fatalf("AppendUserMessage(sessionB) error = %v", err)
	}

	_, err = runs.Create(ctx, sessionA.ID, agent.IncomingMessage{
		Content: "run a",
	}, agent.AgentConfig{DefaultModel: "model-a", QueueMode: agent.QueueEnqueue})
	if err != nil {
		t.Fatalf("runs.Create(runA) error = %v", err)
	}
	runB, err := runs.Create(ctx, sessionB.ID, agent.IncomingMessage{
		Content: "run b",
	}, agent.AgentConfig{DefaultModel: "model-b", QueueMode: agent.QueueEnqueue})
	if err != nil {
		t.Fatalf("runs.Create(runB) error = %v", err)
	}

	filter := agent.ScopeFilter{}
	filteredSessions, err := sessions.ListScoped(ctx, agent.SessionListFilter{Scope: filter})
	if err != nil {
		t.Fatalf("ListScoped() error = %v", err)
	}
	if len(filteredSessions) != 2 {
		t.Fatalf("filtered sessions = %#v", filteredSessions)
	}
	if _, err := sessions.GetScoped(ctx, sessionB.ID, filter); err != nil {
		t.Fatalf("GetScoped(sessionB) error = %v", err)
	}
	if _, err := sessions.GetByKeyScoped(ctx, "scope:b", filter); err != nil {
		t.Fatalf("GetByKeyScoped(sessionB) error = %v", err)
	}

	filteredRuns, err := runs.List(ctx, agent.RunListFilter{Scope: filter})
	if err != nil {
		t.Fatalf("runs.List() error = %v", err)
	}
	if len(filteredRuns) != 2 {
		t.Fatalf("filtered runs = %#v", filteredRuns)
	}
	if _, err := runs.GetScoped(ctx, runB.ID, filter); err != nil {
		t.Fatalf("GetScoped(runB) error = %v", err)
	}
}

func TestSQLiteSessionMetadataReadersAndRecentMessages(t *testing.T) {
	t.Parallel()

	db, err := OpenDB(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	sessions := NewSQLiteSessionStore(db)
	ctx := context.Background()

	sess, err := sessions.GetOrCreate(ctx, "test:metadata", "m1")
	if err != nil {
		t.Fatalf("GetOrCreate() error = %v", err)
	}
	if err := sessions.AppendUserMessage(ctx, sess.ID, agent.IncomingMessage{Content: "first"}); err != nil {
		t.Fatalf("AppendUserMessage(first) error = %v", err)
	}
	loaded, unlock, err := sessions.LoadForExecution(ctx, sess.ID)
	if err != nil {
		t.Fatalf("LoadForExecution() error = %v", err)
	}
	loaded.Messages = append(loaded.Messages, contextengine.Message{
		Role:    contextengine.RoleAssistant,
		Content: "second",
	})
	loaded.UpdatedAt = time.Now().UTC()
	if err := sessions.Save(ctx, loaded); err != nil {
		unlock()
		t.Fatalf("Save() error = %v", err)
	}
	unlock()

	meta, err := sessions.GetMetadata(ctx, sess.ID)
	if err != nil {
		t.Fatalf("GetMetadata() error = %v", err)
	}
	if meta.MessageCount != 2 {
		t.Fatalf("meta.MessageCount = %d, want 2", meta.MessageCount)
	}
	if len(meta.Messages) != 0 {
		t.Fatalf("len(meta.Messages) = %d, want 0", len(meta.Messages))
	}

	byKey, err := sessions.GetByKeyMetadata(ctx, sess.Key)
	if err != nil {
		t.Fatalf("GetByKeyMetadata() error = %v", err)
	}
	if byKey.MessageCount != 2 {
		t.Fatalf("byKey.MessageCount = %d, want 2", byKey.MessageCount)
	}

	recent, err := sessions.RecentMessages(ctx, sess.ID, 1)
	if err != nil {
		t.Fatalf("RecentMessages() error = %v", err)
	}
	if len(recent) != 1 || recent[0].Content != "second" {
		t.Fatalf("recent = %#v", recent)
	}

	list, err := sessions.ListScoped(ctx, agent.SessionListFilter{})
	if err != nil {
		t.Fatalf("ListScoped() error = %v", err)
	}
	if len(list) != 1 || list[0].MessageCount != 2 || len(list[0].Messages) != 0 {
		t.Fatalf("list = %#v", list)
	}
}

func TestSQLiteSessionSave(t *testing.T) {
	t.Parallel()
	db, err := OpenDB(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	s := NewSQLiteSessionStore(db)
	ctx := context.Background()

	sess, _ := s.GetOrCreate(ctx, "test:save", "m1")
	loaded, unlock, err := s.LoadForExecution(ctx, sess.ID)
	if err != nil {
		t.Fatal(err)
	}

	// Agent adds messages during execution.
	loaded.Messages = append(loaded.Messages, contextengine.Message{
		Role:    contextengine.RoleAssistant,
		Content: "I can help",
	})
	loaded.Summary = "conversation about help"
	loaded.UpdatedAt = time.Now().UTC()
	if err := s.Save(ctx, loaded); err != nil {
		unlock()
		t.Fatal(err)
	}
	if loaded.Revision != 2 {
		unlock()
		t.Fatalf("loaded.Revision = %d, want 2", loaded.Revision)
	}
	unlock()

	// Verify persisted.
	got, err := s.Get(ctx, sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Messages) != 1 {
		t.Fatalf("messages = %d, want 1", len(got.Messages))
	}
	if got.Revision != 2 {
		t.Fatalf("revision = %d, want 2", got.Revision)
	}
	if got.Summary != "conversation about help" {
		t.Fatalf("summary = %q", got.Summary)
	}
}

func TestSQLiteSessionSaveAppendsWithoutRewritingExistingTranscriptRows(t *testing.T) {
	t.Parallel()

	db, err := OpenDB(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	sessions := NewSQLiteSessionStore(db)
	ctx := context.Background()

	sess, err := sessions.GetOrCreate(ctx, "test:append-only", "m1")
	if err != nil {
		t.Fatalf("GetOrCreate() error = %v", err)
	}
	if err := sessions.AppendUserMessage(ctx, sess.ID, agent.IncomingMessage{Content: "first"}); err != nil {
		t.Fatalf("AppendUserMessage() error = %v", err)
	}

	var firstID int64
	if err := db.QueryRow(`SELECT id FROM messages WHERE session_id = ? ORDER BY id LIMIT 1`, sess.ID).Scan(&firstID); err != nil {
		t.Fatalf("query first message id: %v", err)
	}

	loaded, unlock, err := sessions.LoadForExecution(ctx, sess.ID)
	if err != nil {
		t.Fatalf("LoadForExecution() error = %v", err)
	}
	loaded.Messages = append(loaded.Messages, contextengine.Message{
		Role:    contextengine.RoleAssistant,
		Content: "second",
	})
	loaded.UpdatedAt = time.Now().UTC()
	if err := sessions.Save(ctx, loaded); err != nil {
		unlock()
		t.Fatalf("Save() error = %v", err)
	}
	unlock()

	rows, err := db.Query(`SELECT id, content FROM messages WHERE session_id = ? ORDER BY id`, sess.ID)
	if err != nil {
		t.Fatalf("query messages: %v", err)
	}
	defer rows.Close()

	var (
		ids      []int64
		contents []string
	)
	for rows.Next() {
		var (
			id      int64
			content string
		)
		if err := rows.Scan(&id, &content); err != nil {
			t.Fatalf("scan message row: %v", err)
		}
		ids = append(ids, id)
		contents = append(contents, content)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate message rows: %v", err)
	}

	if len(ids) != 2 {
		t.Fatalf("message row count = %d, want 2", len(ids))
	}
	if ids[0] != firstID {
		t.Fatalf("first persisted message id = %d, want preserved %d", ids[0], firstID)
	}
	if ids[1] <= ids[0] {
		t.Fatalf("message ids = %v, want appended ordering", ids)
	}
	if contents[0] != "first" || contents[1] != "second" {
		t.Fatalf("contents = %#v", contents)
	}
}

func TestSQLiteSessionSaveRejectsPersistedTranscriptRewrites(t *testing.T) {
	t.Parallel()

	db, err := OpenDB(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	sessions := NewSQLiteSessionStore(db)
	ctx := context.Background()

	sess, err := sessions.GetOrCreate(ctx, "test:append-only-reject", "m1")
	if err != nil {
		t.Fatalf("GetOrCreate() error = %v", err)
	}
	if err := sessions.AppendUserMessage(ctx, sess.ID, agent.IncomingMessage{Content: "first"}); err != nil {
		t.Fatalf("AppendUserMessage() error = %v", err)
	}

	loaded, unlock, err := sessions.LoadForExecution(ctx, sess.ID)
	if err != nil {
		t.Fatalf("LoadForExecution() error = %v", err)
	}
	loaded.Messages[0].Content = "rewritten"
	loaded.UpdatedAt = time.Now().UTC()
	if err := sessions.Save(ctx, loaded); err == nil {
		unlock()
		t.Fatal("Save() error = nil, want append-only mismatch")
	} else if !strings.Contains(err.Error(), "append-only transcript mismatch") {
		unlock()
		t.Fatalf("Save() error = %v", err)
	}
	unlock()

	got, err := sessions.Get(ctx, sess.ID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if len(got.Messages) != 1 || got.Messages[0].Content != "first" {
		t.Fatalf("persisted messages = %#v", got.Messages)
	}
}

func TestSQLiteSessionTranscriptEventsRecordAppendOnlyWrites(t *testing.T) {
	t.Parallel()

	db, err := OpenDB(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	sessions := NewSQLiteSessionStore(db)
	ctx := context.Background()

	sess, err := sessions.GetOrCreate(ctx, "test:transcript-events", "m1")
	if err != nil {
		t.Fatalf("GetOrCreate() error = %v", err)
	}
	if err := sessions.AppendUserMessage(ctx, sess.ID, agent.IncomingMessage{Content: "first"}); err != nil {
		t.Fatalf("AppendUserMessage() error = %v", err)
	}

	loaded, unlock, err := sessions.LoadForExecution(ctx, sess.ID)
	if err != nil {
		t.Fatalf("LoadForExecution() error = %v", err)
	}
	loaded.Messages = append(loaded.Messages, contextengine.Message{
		Role:      contextengine.RoleAssistant,
		Content:   "second",
		CreatedAt: time.Now().UTC(),
	})
	loaded.UpdatedAt = time.Now().UTC()
	if err := sessions.Save(ctx, loaded); err != nil {
		unlock()
		t.Fatalf("Save() error = %v", err)
	}
	unlock()

	rows, err := db.Query(`SELECT session_revision, event_type, message_count_delta FROM transcript_events WHERE session_id = ? ORDER BY id`, sess.ID)
	if err != nil {
		t.Fatalf("query transcript events: %v", err)
	}
	defer rows.Close()

	var (
		revisions  []int64
		eventTypes []string
		deltas     []int
	)
	for rows.Next() {
		var (
			revision  int64
			eventType string
			delta     int
		)
		if err := rows.Scan(&revision, &eventType, &delta); err != nil {
			t.Fatalf("scan transcript event: %v", err)
		}
		revisions = append(revisions, revision)
		eventTypes = append(eventTypes, eventType)
		deltas = append(deltas, delta)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate transcript events: %v", err)
	}

	if len(eventTypes) != 2 {
		t.Fatalf("transcript event count = %d, want 2", len(eventTypes))
	}
	if revisions[0] != 2 || revisions[1] != 3 {
		t.Fatalf("transcript revisions = %#v, want [2 3]", revisions)
	}
	if eventTypes[0] != transcriptEventUserAppended || eventTypes[1] != transcriptEventMessagesAppended {
		t.Fatalf("transcript event types = %#v", eventTypes)
	}
	if deltas[0] != 1 || deltas[1] != 1 {
		t.Fatalf("transcript deltas = %#v, want [1 1]", deltas)
	}
}

func TestSQLiteSessionSaveWithoutNewMessagesRecordsSessionUpdatedEvent(t *testing.T) {
	t.Parallel()

	db, err := OpenDB(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	sessions := NewSQLiteSessionStore(db)
	ctx := context.Background()

	sess, err := sessions.GetOrCreate(ctx, "test:transcript-session-updated", "m1")
	if err != nil {
		t.Fatalf("GetOrCreate() error = %v", err)
	}

	loaded, unlock, err := sessions.LoadForExecution(ctx, sess.ID)
	if err != nil {
		t.Fatalf("LoadForExecution() error = %v", err)
	}
	loaded.Summary = "updated summary"
	loaded.SummaryAt = time.Now().UTC()
	loaded.UpdatedAt = time.Now().UTC()
	if err := sessions.Save(ctx, loaded); err != nil {
		unlock()
		t.Fatalf("Save() error = %v", err)
	}
	unlock()

	var (
		revision  int64
		eventType string
		delta     int
	)
	if err := db.QueryRow(`SELECT session_revision, event_type, message_count_delta FROM transcript_events WHERE session_id = ? ORDER BY id DESC LIMIT 1`, sess.ID).
		Scan(&revision, &eventType, &delta); err != nil {
		t.Fatalf("query transcript event: %v", err)
	}
	if revision != 2 {
		t.Fatalf("session_revision = %d, want 2", revision)
	}
	if eventType != transcriptEventSessionUpdated {
		t.Fatalf("event_type = %q, want %q", eventType, transcriptEventSessionUpdated)
	}
	if delta != 0 {
		t.Fatalf("message_count_delta = %d, want 0", delta)
	}
}

func TestSQLiteSessionSavePersistsPinnedFacts(t *testing.T) {
	t.Parallel()

	db := openRawTestDB(t)
	sessions := NewSQLiteSessionStore(db)
	ctx := context.Background()

	sess, err := sessions.GetOrCreate(ctx, "test:pinned-facts", "m1")
	if err != nil {
		t.Fatalf("GetOrCreate() error = %v", err)
	}

	loaded, unlock, err := sessions.LoadForExecution(ctx, sess.ID)
	if err != nil {
		t.Fatalf("LoadForExecution() error = %v", err)
	}
	loaded.PinnedFacts = []contextengine.PinnedFact{{
		Key:       "deploy_env",
		Content:   "staging only",
		Source:    "user",
		UpdatedAt: time.Now().UTC().Round(0),
		Metadata: map[string]any{
			"scope": "release",
		},
	}}
	loaded.UpdatedAt = time.Now().UTC()
	if err := sessions.Save(ctx, loaded); err != nil {
		unlock()
		t.Fatalf("Save() error = %v", err)
	}
	unlock()

	got, err := sessions.Get(ctx, sess.ID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if len(got.PinnedFacts) != 1 {
		t.Fatalf("len(PinnedFacts) = %d, want 1", len(got.PinnedFacts))
	}
	if got.PinnedFacts[0].Key != "deploy_env" || got.PinnedFacts[0].Content != "staging only" {
		t.Fatalf("PinnedFacts = %#v", got.PinnedFacts)
	}
	if got.PinnedFacts[0].Metadata["scope"] != "release" {
		t.Fatalf("PinnedFacts metadata = %#v", got.PinnedFacts[0].Metadata)
	}
}

func TestScanSessionIgnoresWhitespaceOnlySkillSnapshot(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC().Round(0)
	sess, err := scanSession(scannedSessionRow{
		id:                 "sess-skill-whitespace",
		key:                "scope:test",
		model:              "gpt-4.1",
		revision:           1,
		summary:            "",
		summaryAtStr:       "",
		scopeJSON:          "{}",
		metadataJSON:       "{}",
		pinnedFactsJSON:    "[]",
		skillJSON:          " \n\t ",
		createdAtStr:       formatTime(now),
		updatedAtStr:       formatTime(now),
		executionWatermark: 0,
	})
	if err != nil {
		t.Fatalf("scanSession() error = %v", err)
	}
	if sess == nil {
		t.Fatal("scanSession() = nil, want session")
	}
	if sess.SkillSnapshot.Fingerprint != "" || len(sess.SkillSnapshot.Ordered) != 0 {
		t.Fatalf("SkillSnapshot = %#v, want zero-value snapshot", sess.SkillSnapshot)
	}
}

func TestSQLiteSessionAppendBlocksDuringExecutionLockAndPreservesMessages(t *testing.T) {
	t.Parallel()
	db, err := OpenDB(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	s := NewSQLiteSessionStore(db)
	ctx := context.Background()

	sess, _ := s.GetOrCreate(ctx, "test:serialize-append", "m1")
	if err := s.AppendUserMessage(ctx, sess.ID, agent.IncomingMessage{Content: "first"}); err != nil {
		t.Fatalf("AppendUserMessage(first) error = %v", err)
	}

	loaded, unlock, err := s.LoadForExecution(ctx, sess.ID)
	if err != nil {
		t.Fatalf("LoadForExecution() error = %v", err)
	}

	started := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		close(started)
		done <- s.AppendUserMessage(ctx, sess.ID, agent.IncomingMessage{Content: "second"})
	}()
	<-started

	select {
	case err := <-done:
		t.Fatalf("AppendUserMessage should block while execution lock is held, got err=%v", err)
	case <-time.After(20 * time.Millisecond):
	}

	loaded.Summary = "locked save"
	loaded.UpdatedAt = time.Now().UTC()
	if err := s.Save(ctx, loaded); err != nil {
		unlock()
		t.Fatalf("Save() error = %v", err)
	}
	unlock()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("AppendUserMessage(second) error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("AppendUserMessage did not complete after unlock")
	}

	got, err := s.Get(ctx, sess.ID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if len(got.Messages) != 2 {
		t.Fatalf("len(got.Messages) = %d, want 2", len(got.Messages))
	}
	if got.Messages[0].Content != "first" || got.Messages[1].Content != "second" {
		t.Fatalf("messages = %#v", got.Messages)
	}
}

func TestSQLiteSessionCascadeDelete(t *testing.T) {
	t.Parallel()
	db, err := OpenDB(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	sessions := NewSQLiteSessionStore(db)
	runs := NewSQLiteRunStore(db)
	approvalStore := NewSQLiteApprovalStore(db)
	ctx := context.Background()

	sess, _ := sessions.GetOrCreate(ctx, "test:cascade", "m1")
	_ = sessions.AppendUserMessage(ctx, sess.ID, agent.IncomingMessage{Content: "hi"})

	run, _ := runs.Create(ctx, sess.ID, agent.IncomingMessage{Content: "hi"}, agent.AgentConfig{
		DefaultModel: "m1", QueueMode: agent.QueueEnqueue,
	})
	_, _ = approvalStore.Create(ctx, approval.Ticket{
		RunID: run.ID, SessionID: sess.ID,
	})

	// Delete session — should cascade.
	if _, err := db.Exec("DELETE FROM sessions WHERE id = ?", sess.ID); err != nil {
		t.Fatal(err)
	}

	// Messages, runs, approvals should be gone.
	var msgCount, runCount, approvalCount, transcriptEventCount int
	db.QueryRow("SELECT COUNT(*) FROM messages WHERE session_id = ?", sess.ID).Scan(&msgCount)
	db.QueryRow("SELECT COUNT(*) FROM runs WHERE session_id = ?", sess.ID).Scan(&runCount)
	db.QueryRow("SELECT COUNT(*) FROM approvals WHERE session_id = ?", sess.ID).Scan(&approvalCount)
	db.QueryRow("SELECT COUNT(*) FROM transcript_events WHERE session_id = ?", sess.ID).Scan(&transcriptEventCount)

	if msgCount != 0 || runCount != 0 || approvalCount != 0 || transcriptEventCount != 0 {
		t.Fatalf("cascade failed: messages=%d runs=%d approvals=%d transcript_events=%d", msgCount, runCount, approvalCount, transcriptEventCount)
	}
}

func TestSQLiteApprovalStorePersistsExternalRefs(t *testing.T) {
	t.Parallel()

	db := openRawTestDB(t)
	sessions := NewSQLiteSessionStore(db)
	approvals := NewSQLiteApprovalStore(db)
	ctx := context.Background()

	sess, err := sessions.GetOrCreate(ctx, "test:approval-external", "m1")
	if err != nil {
		t.Fatalf("GetOrCreate() error = %v", err)
	}
	ticket, err := approvals.Create(ctx, approval.Ticket{
		RunID:     "run-ext-1",
		SessionID: sess.ID,
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if _, err := approvals.UpsertExternalRef(ctx, ticket.ID, approval.ExternalReference{
		Provider:   "jira",
		ExternalID: "jira-99",
		URL:        "https://jira.example/approvals/99",
		Status:     "pending_remote",
		SyncedAt:   time.Now().UTC(),
	}); err != nil {
		t.Fatalf("UpsertExternalRef() error = %v", err)
	}

	got, err := approvals.Get(ctx, ticket.ID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if len(got.External) != 1 {
		t.Fatalf("len(got.External) = %d", len(got.External))
	}
	if got.External[0].Provider != "jira" || got.External[0].ExternalID != "jira-99" {
		t.Fatalf("got.External = %#v", got.External)
	}

	byExternal, err := approvals.GetByExternal(ctx, "jira", "jira-99")
	if err != nil {
		t.Fatalf("GetByExternal() error = %v", err)
	}
	if byExternal.ID != ticket.ID {
		t.Fatalf("byExternal.ID = %q, want %q", byExternal.ID, ticket.ID)
	}
}

func TestSQLiteApprovalStoreListHydratesExternalRefs(t *testing.T) {
	t.Parallel()

	db := openRawTestDB(t)
	sessions := NewSQLiteSessionStore(db)
	approvals := NewSQLiteApprovalStore(db)
	ctx := context.Background()

	sess, err := sessions.GetOrCreate(ctx, "test:approval-list-external", "m1")
	if err != nil {
		t.Fatalf("GetOrCreate() error = %v", err)
	}
	first, err := approvals.Create(ctx, approval.Ticket{
		RunID:     "run-ext-list-1",
		SessionID: sess.ID,
	})
	if err != nil {
		t.Fatalf("Create(first) error = %v", err)
	}
	second, err := approvals.Create(ctx, approval.Ticket{
		RunID:     "run-ext-list-2",
		SessionID: sess.ID,
	})
	if err != nil {
		t.Fatalf("Create(second) error = %v", err)
	}
	if _, err := approvals.UpsertExternalRef(ctx, first.ID, approval.ExternalReference{
		Provider:   "jira",
		ExternalID: "jira-101",
	}); err != nil {
		t.Fatalf("UpsertExternalRef(first) error = %v", err)
	}
	if _, err := approvals.UpsertExternalRef(ctx, second.ID, approval.ExternalReference{
		Provider:   "slack",
		ExternalID: "slack-202",
	}); err != nil {
		t.Fatalf("UpsertExternalRef(second) error = %v", err)
	}

	list, err := approvals.List(ctx, approval.ListFilter{})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("len(list) = %d, want 2", len(list))
	}

	refsByID := map[string][]approval.ExternalReference{}
	for _, ticket := range list {
		refsByID[ticket.ID] = ticket.External
	}
	if len(refsByID[first.ID]) != 1 || refsByID[first.ID][0].Provider != "jira" {
		t.Fatalf("refsByID[%q] = %#v", first.ID, refsByID[first.ID])
	}
	if len(refsByID[second.ID]) != 1 || refsByID[second.ID][0].Provider != "slack" {
		t.Fatalf("refsByID[%q] = %#v", second.ID, refsByID[second.ID])
	}
}

func TestSQLiteRunCRUD(t *testing.T) {
	t.Parallel()
	db, err := OpenDB(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	sessions := NewSQLiteSessionStore(db)
	runs := NewSQLiteRunStore(db)
	ctx := context.Background()

	sess, _ := sessions.GetOrCreate(ctx, "test:run", "m1")
	run, err := runs.Create(ctx, sess.ID, agent.IncomingMessage{
		ExternalEventID: "evt-1", Content: "do it",
	}, agent.AgentConfig{DefaultModel: "m1", QueueMode: agent.QueueEnqueue})
	if err != nil {
		t.Fatal(err)
	}

	// Seen.
	if !runs.Seen(ctx, "evt-1", time.Minute) {
		t.Fatal("Seen() should be true")
	}

	// Update.
	run.Status = agent.RunRunning
	run.Phase = agent.PhaseWaitingModel
	run.LastSessionRevision = 9
	run.SemanticSignal = &agent.SemanticSignal{
		Language: agent.LanguageProfile{
			Family:           "es",
			Script:           "Latn",
			MainSemanticPath: true,
		},
		ExecutionMode:       agent.ExecutionModeWorkflow,
		RequiresCurrentInfo: true,
		SuggestedDomains:    []string{"browser", "fs"},
		JobType:             "report",
		TargetSummary:       "docs/tmp/summary.md",
		TriageReady:         true,
		TaskContractReady:   true,
		Reason:              "fresh_page_state",
	}
	run.TaskContract = &agent.TaskContract{
		Goal:    "do it",
		JobType: "report",
		ExpectedDeliverables: []agent.TaskContractDeliverable{{
			Kind:     "document",
			Required: true,
		}},
	}
	run.ExecutionGraph = &agent.ExecutionGraph{
		RunID:          run.ID,
		SessionID:      run.SessionID,
		Scope:          "single_session",
		SingleSession:  true,
		SessionLocking: true,
		MergeStrategy:  agent.MergeStrategyTaskOrder,
		Tasks: []agent.ExecutionTask{{
			ID:              "deliver",
			Kind:            "deliver",
			Goal:            "do it",
			ResourceKeys:    []string{"output:document"},
			SideEffectScope: agent.SideEffectScopeSessionAll,
			MergeStrategy:   agent.MergeStrategySerialOnly,
			IdempotencyKey:  "task:test",
			Status:          "queued",
		}},
	}
	run.WorkflowState = &agent.WorkflowState{
		OriginalRunID:     run.ID,
		ContinuationIndex: 1,
		MaxContinuations:  agent.DefaultMaxContinuations,
		TotalRoundsUsed:   6,
		MaxTotalRounds:    agent.DefaultMaxTotalRounds,
		PriorRunSummaries: []string{"Run run-000001: completed 1/2 tasks"},
		CompletedTaskIDs:  []string{"deliver-0"},
		Yielded:           true,
		YieldReason:       agent.YieldReasonRoundBudget,
		Budget: &agent.WorkflowBudgetState{
			Policy: agent.DefaultWorkflowBudgetPolicy(),
			Mode:   agent.WorkflowBudgetModeEconomy,
			Usage: agent.WorkflowBudgetUsage{
				ModelTotalTokens:         3210,
				EstimatedCost:            0.123,
				ModelCallCount:           2,
				StartedContinuationCount: 2,
				StartedAt:                time.Now().UTC().Add(-time.Minute),
				LastUpdatedAt:            time.Now().UTC(),
			},
			PredictedNextRunTokens: 12000,
			PredictedNextRunCost:   0.05,
			SoftLimitExceeded:      true,
		},
	}
	if err := runs.Update(ctx, run); err != nil {
		t.Fatal(err)
	}

	got, err := runs.Get(ctx, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != agent.RunRunning {
		t.Fatalf("status = %q", got.Status)
	}
	if got.LastSessionRevision != 9 {
		t.Fatalf("last_session_revision = %d, want 9", got.LastSessionRevision)
	}
	if got.SemanticSignal == nil {
		t.Fatal("semantic_signal = nil, want persisted diagnostics")
	}
	if got.SemanticSignal.Language.Family != "es" || got.SemanticSignal.Language.Script != "Latn" {
		t.Fatalf("semantic_signal.language = %#v", got.SemanticSignal.Language)
	}
	if !got.SemanticSignal.RequiresCurrentInfo || !got.SemanticSignal.TriageReady || !got.SemanticSignal.TaskContractReady {
		t.Fatalf("semantic_signal readiness = %#v", got.SemanticSignal)
	}
	if got.SemanticSignal.TargetSummary != "docs/tmp/summary.md" {
		t.Fatalf("semantic_signal.target_summary = %q, want docs/tmp/summary.md", got.SemanticSignal.TargetSummary)
	}
	if got.TaskContract == nil || got.TaskContract.JobType != "report" {
		t.Fatalf("task_contract = %#v", got.TaskContract)
	}
	if got.ExecutionGraph == nil || len(got.ExecutionGraph.Tasks) != 1 {
		t.Fatalf("execution_graph = %#v", got.ExecutionGraph)
	}
	if got.ExecutionGraph.Tasks[0].IdempotencyKey != "task:test" {
		t.Fatalf("execution_graph task idempotency_key = %q", got.ExecutionGraph.Tasks[0].IdempotencyKey)
	}
	if got.WorkflowState == nil {
		t.Fatal("workflow_state = nil, want populated state")
	}
	if got.WorkflowState.OriginalRunID != run.ID {
		t.Fatalf("workflow_state.original_run_id = %q, want %q", got.WorkflowState.OriginalRunID, run.ID)
	}
	if got.WorkflowState.YieldReason != agent.YieldReasonRoundBudget {
		t.Fatalf("workflow_state.yield_reason = %q, want %q", got.WorkflowState.YieldReason, agent.YieldReasonRoundBudget)
	}
	if got.WorkflowState.Budget == nil {
		t.Fatal("workflow_state.budget = nil, want persisted budget")
	}
	if got.WorkflowState.Budget.Mode != agent.WorkflowBudgetModeEconomy {
		t.Fatalf("workflow_state.budget.mode = %q, want %q", got.WorkflowState.Budget.Mode, agent.WorkflowBudgetModeEconomy)
	}
	if got.WorkflowState.Budget.Usage.ModelTotalTokens != 3210 {
		t.Fatalf("workflow_state.budget.usage.model_total_tokens = %d, want 3210", got.WorkflowState.Budget.Usage.ModelTotalTokens)
	}
	if got.WorkflowState.Budget.PredictedNextRunTokens != 12000 {
		t.Fatalf("workflow_state.budget.predicted_next_run_tokens = %d, want 12000", got.WorkflowState.Budget.PredictedNextRunTokens)
	}

	// List.
	list, err := runs.List(ctx, agent.RunListFilter{SessionID: sess.ID})
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 {
		t.Fatalf("list = %d", len(list))
	}
}

func TestSQLiteMigrationsAddRunSemanticSignalColumn(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "legacy-runs.db")
	migrateSQLiteSchemaUpTo(t, path, 18)

	db, err := OpenDB(path)
	if err != nil {
		t.Fatalf("OpenDB() error = %v", err)
	}
	defer db.Close()

	rows, err := db.Query(`PRAGMA table_info(runs)`)
	if err != nil {
		t.Fatalf("PRAGMA table_info(runs): %v", err)
	}
	defer rows.Close()

	found := false
	for rows.Next() {
		var (
			cid        int
			name       string
			typ        string
			notNull    int
			defaultVal any
			primaryKey int
		)
		if err := rows.Scan(&cid, &name, &typ, &notNull, &defaultVal, &primaryKey); err != nil {
			t.Fatalf("scan table_info: %v", err)
		}
		if name == "semantic_signal" {
			found = true
			if notNull != 1 {
				t.Fatalf("semantic_signal notnull = %d, want 1", notNull)
			}
			break
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate table_info: %v", err)
	}
	if !found {
		t.Fatal("expected semantic_signal column after migration to latest schema")
	}
}

func TestSQLiteRunStoreClaimQueuedRun(t *testing.T) {
	t.Parallel()

	db, err := OpenDB(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	sessions := NewSQLiteSessionStore(db)
	runs := NewSQLiteRunStore(db)
	ctx := context.Background()

	sess, _ := sessions.GetOrCreate(ctx, "test:claim", "m1")
	run, err := runs.Create(ctx, sess.ID, agent.IncomingMessage{
		ExternalEventID: "evt-claim",
		Content:         "start me",
	}, agent.AgentConfig{DefaultModel: "m1", QueueMode: agent.QueueEnqueue})
	if err != nil {
		t.Fatal(err)
	}

	claimed, ok, err := runs.ClaimQueuedRun(ctx, run.ID)
	if err != nil {
		t.Fatalf("ClaimQueuedRun(first) error = %v", err)
	}
	if !ok {
		t.Fatal("ClaimQueuedRun(first) = false, want true")
	}
	if claimed.Status != agent.RunRunning {
		t.Fatalf("claimed.Status = %q, want %q", claimed.Status, agent.RunRunning)
	}
	if claimed.StartedAt.IsZero() {
		t.Fatal("claimed.StartedAt should be set")
	}

	second, ok, err := runs.ClaimQueuedRun(ctx, run.ID)
	if err != nil {
		t.Fatalf("ClaimQueuedRun(second) error = %v", err)
	}
	if ok {
		t.Fatal("ClaimQueuedRun(second) = true, want false")
	}
	if second.Status != agent.RunRunning {
		t.Fatalf("second.Status = %q, want %q", second.Status, agent.RunRunning)
	}
}

func TestSQLiteRunStorePersistsDelegationContract(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "test.db")
	db, err := OpenDB(path)
	if err != nil {
		t.Fatal(err)
	}

	sessions := NewSQLiteSessionStore(db)
	runs := NewSQLiteRunStore(db)
	ctx := context.Background()

	sess, _ := sessions.GetOrCreate(ctx, "test:delegation", "m1")
	run, err := runs.Create(ctx, sess.ID, agent.IncomingMessage{
		ExternalEventID: "evt-delegation",
		Content:         "delegate this",
	}, agent.AgentConfig{DefaultModel: "m1", QueueMode: agent.QueueEnqueue})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	run.Delegation = &agent.DelegationContract{
		Goal:                "delegate this",
		AllowedDomains:      []string{"fs", "git"},
		AllowedTools:        []string{"read_file", "search_code"},
		SideEffectClass:     "read_only",
		MaxTurns:            3,
		MaxBudgetTokens:     1200,
		RequiresApproval:    true,
		VerificationPlanRef: "plan-42",
		Source:              "test",
		GeneratedAt:         time.Date(2026, 4, 1, 12, 0, 0, 0, time.UTC),
	}
	if err := runs.Update(ctx, run); err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	reopened, err := OpenDB(path)
	if err != nil {
		t.Fatalf("reopen OpenDB() error = %v", err)
	}
	defer reopened.Close()

	reloaded, err := NewSQLiteRunStore(reopened).Get(ctx, run.ID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if reloaded.Delegation == nil {
		t.Fatal("Delegation = nil, want preserved contract")
	}
	if reloaded.Delegation.Goal != "delegate this" {
		t.Fatalf("Delegation.Goal = %q, want %q", reloaded.Delegation.Goal, "delegate this")
	}
	if len(reloaded.Delegation.AllowedDomains) != 2 || reloaded.Delegation.AllowedDomains[1] != "git" {
		t.Fatalf("Delegation.AllowedDomains = %#v", reloaded.Delegation.AllowedDomains)
	}
	if len(reloaded.Delegation.AllowedTools) != 2 || reloaded.Delegation.AllowedTools[0] != "read_file" {
		t.Fatalf("Delegation.AllowedTools = %#v", reloaded.Delegation.AllowedTools)
	}
	if !reloaded.Delegation.RequiresApproval {
		t.Fatal("Delegation.RequiresApproval = false, want true")
	}
	if !reloaded.Delegation.GeneratedAt.Equal(time.Date(2026, 4, 1, 12, 0, 0, 0, time.UTC)) {
		t.Fatalf("Delegation.GeneratedAt = %v", reloaded.Delegation.GeneratedAt)
	}
}

func TestSQLiteUsageSummarize(t *testing.T) {
	t.Parallel()
	db, err := OpenDB(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	s := NewSQLiteUsageStore(db)
	ctx := context.Background()

	_ = s.Record(ctx, usage.Record{
		SessionID: "s1", Model: "gpt-4", Provider: "openai",
		PromptTokens: 100, CompletionTokens: 50, TotalTokens: 150,
		RecordType: usage.RecordTypeModelCall,
	})
	_ = s.Record(ctx, usage.Record{
		SessionID: "s1", Model: "gpt-4", Provider: "openai",
		PromptTokens: 200, CompletionTokens: 100, TotalTokens: 300,
		RecordType: usage.RecordTypeModelCall,
	})

	summary, err := s.Summarize(ctx, usage.QueryFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if summary.TotalTokens != 450 {
		t.Fatalf("total_tokens = %d, want 450", summary.TotalTokens)
	}
	if summary.RecordCount != 2 {
		t.Fatalf("record_count = %d, want 2", summary.RecordCount)
	}
	mu, ok := summary.ByModel["gpt-4"]
	if !ok {
		t.Fatal("missing gpt-4 in ByModel")
	}
	if mu.CallCount != 2 {
		t.Fatalf("gpt-4 call_count = %d", mu.CallCount)
	}
}

func TestSQLiteUsageSummarizeByWorkflowID(t *testing.T) {
	t.Parallel()

	db, err := OpenDB(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	s := NewSQLiteUsageStore(db)
	ctx := context.Background()

	_ = s.Record(ctx, usage.Record{
		SessionID:         "s1",
		RunID:             "run-1",
		WorkflowID:        "wf-1",
		ParentRunID:       "run-root",
		ContinuationIndex: 0,
		Model:             "gpt-4",
		Provider:          "openai",
		PromptTokens:      100,
		CompletionTokens:  50,
		TotalTokens:       150,
		RecordType:        usage.RecordTypeModelCall,
	})
	_ = s.Record(ctx, usage.Record{
		SessionID:         "s1",
		RunID:             "run-2",
		WorkflowID:        "wf-1",
		ParentRunID:       "run-1",
		ContinuationIndex: 1,
		ToolName:          "exec.run",
		RecordType:        usage.RecordTypeToolExecution,
	})
	_ = s.Record(ctx, usage.Record{
		SessionID:         "s1",
		RunID:             "run-3",
		WorkflowID:        "wf-2",
		ParentRunID:       "run-root",
		ContinuationIndex: 0,
		Model:             "gpt-4",
		Provider:          "openai",
		PromptTokens:      200,
		CompletionTokens:  100,
		TotalTokens:       300,
		RecordType:        usage.RecordTypeModelCall,
	})

	summary, err := s.Summarize(ctx, usage.QueryFilter{
		WorkflowID: "wf-1",
		RecordType: usage.RecordTypeModelCall,
	})
	if err != nil {
		t.Fatal(err)
	}
	if summary.TotalTokens != 150 {
		t.Fatalf("summary.TotalTokens = %d, want 150", summary.TotalTokens)
	}
	if summary.RecordCount != 1 {
		t.Fatalf("summary.RecordCount = %d, want 1", summary.RecordCount)
	}

	records, err := s.Query(ctx, usage.QueryFilter{
		WorkflowID: "wf-1",
		RecordType: usage.RecordTypeToolExecution,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 {
		t.Fatalf("len(records) = %d, want 1", len(records))
	}
	if records[0].ParentRunID != "run-1" {
		t.Fatalf("records[0].ParentRunID = %q, want run-1", records[0].ParentRunID)
	}
}

func TestSQLiteEventSink(t *testing.T) {
	t.Parallel()
	db, err := OpenDB(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	sink := NewSQLiteEventSink(db)
	ctx := context.Background()

	_ = sink.Handle(ctx, eventbus.Event{
		Type: eventbus.EventRunStarted, RunID: "run-1", SessionID: "s1",
	})
	_ = sink.Handle(ctx, eventbus.Event{
		Type: eventbus.EventRunCompleted, RunID: "run-1", SessionID: "s1",
	})

	events, err := sink.Replay()
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 {
		t.Fatalf("events = %d, want 2", len(events))
	}
	if events[0].Type != eventbus.EventRunStarted {
		t.Fatalf("event[0].type = %q", events[0].Type)
	}

	// ReplaySince.
	since, err := sink.ReplaySince(events[0].ID, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(since) != 1 {
		t.Fatalf("since = %d, want 1", len(since))
	}
}

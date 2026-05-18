package agent

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/fulcrus/hopclaw/durablefact"

	_ "modernc.org/sqlite"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func newTestSQLiteStore(t *testing.T) *SQLiteKVStore {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test_memory.db")
	store, err := NewSQLiteKVStore(dbPath)
	if err != nil {
		t.Fatalf("NewSQLiteKVStore(%q): %v", dbPath, err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

func TestSQLiteKVStore_GetSetDelete(t *testing.T) {
	store := newTestSQLiteStore(t)
	ctx := context.Background()

	// Get non-existent key returns nil.
	entry, err := store.Get(ctx, "missing")
	if err != nil {
		t.Fatalf("Get(missing): %v", err)
	}
	if entry != nil {
		t.Fatalf("expected nil, got %+v", entry)
	}

	// Set and Get.
	if err := store.Set(ctx, "greeting", "hello world"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	entry, err = store.Get(ctx, "greeting")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if entry == nil || entry.Value != "hello world" {
		t.Fatalf("unexpected entry: %+v", entry)
	}
	if entry.CreatedAt.IsZero() || entry.UpdatedAt.IsZero() {
		t.Fatalf("timestamps should be set")
	}

	// Update existing key.
	time.Sleep(time.Millisecond) // ensure different timestamp
	if err := store.Set(ctx, "greeting", "hi there"); err != nil {
		t.Fatalf("Set update: %v", err)
	}
	entry, err = store.Get(ctx, "greeting")
	if err != nil {
		t.Fatalf("Get after update: %v", err)
	}
	if entry.Value != "hi there" {
		t.Fatalf("expected updated value, got %q", entry.Value)
	}

	// Delete.
	if err := store.Delete(ctx, "greeting"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	entry, err = store.Get(ctx, "greeting")
	if err != nil {
		t.Fatalf("Get after delete: %v", err)
	}
	if entry != nil {
		t.Fatalf("expected nil after delete, got %+v", entry)
	}
}

func TestSQLiteKVStore_List(t *testing.T) {
	store := newTestSQLiteStore(t)
	ctx := context.Background()

	// List empty store.
	results, err := store.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("expected empty, got %d", len(results))
	}

	// Add entries and list.
	for _, kv := range []struct{ k, v string }{
		{"banana", "yellow"},
		{"apple", "red"},
		{"cherry", "dark red"},
	} {
		if err := store.Set(ctx, kv.k, kv.v); err != nil {
			t.Fatalf("Set(%s): %v", kv.k, err)
		}
	}

	results, err = store.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(results) != 3 {
		t.Fatalf("expected 3, got %d", len(results))
	}
	// Should be sorted by key.
	if results[0].Key != "apple" || results[1].Key != "banana" || results[2].Key != "cherry" {
		t.Fatalf("unexpected order: %s, %s, %s", results[0].Key, results[1].Key, results[2].Key)
	}
}

func TestSQLiteKVStoreListDurableViews(t *testing.T) {
	store := newTestSQLiteStore(t)
	ctx := context.Background()

	if _, err := store.UpsertRecord(ctx, MemoryRecord{
		Namespace: memoryNamespaceProfile,
		ScopeKey:  "user",
		Field:     "reply_language",
		Label:     "Reply Language",
		Value:     "zh-CN",
		Source:    MemorySourceUser,
	}); err != nil {
		t.Fatalf("UpsertRecord() error = %v", err)
	}

	contextViews, err := store.ListContextViews(ctx, durablefact.Filter{Namespace: memoryNamespaceProfile})
	if err != nil {
		t.Fatalf("ListContextViews() error = %v", err)
	}
	if len(contextViews) != 1 || contextViews[0].Field != "reply_language" {
		t.Fatalf("unexpected context views: %#v", contextViews)
	}

	operatorViews, err := store.ListOperatorViews(ctx, durablefact.Filter{Namespace: memoryNamespaceProfile})
	if err != nil {
		t.Fatalf("ListOperatorViews() error = %v", err)
	}
	if len(operatorViews) != 1 || operatorViews[0].ViewType != durablefact.ViewTypeContext {
		t.Fatalf("unexpected operator views: %#v", operatorViews)
	}
}

func TestSQLiteKVStore_KeywordSearch(t *testing.T) {
	store := newTestSQLiteStore(t)
	ctx := context.Background()

	entries := []struct{ k, v string }{
		{"project:alpha", "machine learning pipeline"},
		{"project:beta", "web application framework"},
		{"note:meeting", "discussed alpha project timeline"},
	}
	for _, e := range entries {
		if err := store.Set(ctx, e.k, e.v); err != nil {
			t.Fatalf("Set: %v", err)
		}
	}

	// Search by key.
	results, err := store.Search(ctx, "project")
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) != 3 { // all three contain "project"
		t.Fatalf("expected 3, got %d", len(results))
	}

	// Search by value.
	results, err = store.Search(ctx, "framework")
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) != 1 || results[0].Key != "project:beta" {
		t.Fatalf("unexpected: %+v", results)
	}

	// Case insensitive.
	results, err = store.Search(ctx, "ALPHA")
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results for ALPHA, got %d", len(results))
	}

	// No match.
	results, err = store.Search(ctx, "nonexistent")
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("expected 0, got %d", len(results))
	}
}

func TestSQLiteKVStore_SearchLIKEEscape(t *testing.T) {
	store := newTestSQLiteStore(t)
	ctx := context.Background()

	if err := store.Set(ctx, "100%_done", "complete"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if err := store.Set(ctx, "other", "not matching"); err != nil {
		t.Fatalf("Set: %v", err)
	}

	// Search for literal % character.
	results, err := store.Search(ctx, "100%")
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) != 1 || results[0].Key != "100%_done" {
		t.Fatalf("expected 1 result for literal %%, got %d", len(results))
	}
}

func TestSQLiteKVStore_Persistence(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "persist.db")
	ctx := context.Background()

	// Open, write, close.
	store1, err := NewSQLiteKVStore(dbPath)
	if err != nil {
		t.Fatalf("NewSQLiteKVStore: %v", err)
	}
	if err := store1.Set(ctx, "persistent", "survives restart"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if err := store1.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Reopen and verify.
	store2, err := NewSQLiteKVStore(dbPath)
	if err != nil {
		t.Fatalf("NewSQLiteKVStore reopen: %v", err)
	}
	defer store2.Close()

	entry, err := store2.Get(ctx, "persistent")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if entry == nil || entry.Value != "survives restart" {
		t.Fatalf("persistence failed: %+v", entry)
	}
}

func TestSQLiteKVStore_VectorPersistence(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "vec_persist.db")
	ctx := context.Background()

	// Use the existing mockEmbeddingClient from kvstore_semantic_test.go.
	mock := &mockEmbeddingClient{}

	// Open, set embedding, write entries.
	store1, err := NewSQLiteKVStore(dbPath)
	if err != nil {
		t.Fatalf("NewSQLiteKVStore: %v", err)
	}
	store1.SetEmbedding(mock)

	if err := store1.Set(ctx, "city", "tokyo"); err != nil {
		t.Fatalf("Set tokyo: %v", err)
	}
	if err := store1.Set(ctx, "country", "japan"); err != nil {
		t.Fatalf("Set japan: %v", err)
	}

	// Verify semantic search works before closing.
	results, err := store1.SemanticSearch(ctx, "city", 5)
	if err != nil {
		t.Fatalf("SemanticSearch: %v", err)
	}
	if len(results) == 0 {
		t.Fatalf("expected results from semantic search")
	}

	if err := store1.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Reopen with embedding and verify vectors are loaded from SQLite.
	store2, err := NewSQLiteKVStore(dbPath)
	if err != nil {
		t.Fatalf("NewSQLiteKVStore reopen: %v", err)
	}
	defer store2.Close()

	store2.SetEmbedding(mock)

	if !store2.HasEmbedding() {
		t.Fatal("expected HasEmbedding=true")
	}

	results, err = store2.SemanticSearch(ctx, "city", 5)
	if err != nil {
		t.Fatalf("SemanticSearch after reopen: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected results after reopening (vectors should be loaded from sqlite)")
	}
}

func TestSQLiteKVStore_SemanticSearchRequiresEmbedding(t *testing.T) {
	store := newTestSQLiteStore(t)
	ctx := context.Background()

	_, err := store.SemanticSearch(ctx, "query", 10)
	if err == nil {
		t.Fatal("expected error when embedding not configured")
	}

	_, err = store.SemanticSearchMMR(ctx, "query", 10, 0.5)
	if err == nil {
		t.Fatal("expected error when embedding not configured")
	}
}

func TestSQLiteKVStore_DeleteRemovesVector(t *testing.T) {
	store := newTestSQLiteStore(t)
	ctx := context.Background()

	store.SetEmbedding(&mockEmbeddingClient{})

	if err := store.Set(ctx, "item", "data"); err != nil {
		t.Fatalf("Set: %v", err)
	}

	// Verify vector row exists.
	var count int
	if err := store.db.QueryRow("SELECT COUNT(*) FROM memory_vectors WHERE key = ?", "item").Scan(&count); err != nil {
		t.Fatalf("QueryRow: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected 1 vector row before delete, got %d", count)
	}

	// Delete the entry.
	if err := store.Delete(ctx, "item"); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	// Vector should also be removed from SQLite.
	if err := store.db.QueryRow("SELECT COUNT(*) FROM memory_vectors WHERE key = ?", "item").Scan(&count); err != nil {
		t.Fatalf("QueryRow: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected vector row deleted, got count=%d", count)
	}
}

func TestSQLiteKVStore_InvalidPath(t *testing.T) {
	_, err := NewSQLiteKVStore("/nonexistent/path/to/db.sqlite")
	if err == nil {
		t.Fatal("expected error for invalid path")
	}
}

func TestEncodeDecodeVector(t *testing.T) {
	original := []float32{1.0, -2.5, 3.14159, 0.0, -0.001}
	blob := encodeVector(original)
	decoded := decodeVector(blob)

	if len(decoded) != len(original) {
		t.Fatalf("length mismatch: %d vs %d", len(decoded), len(original))
	}
	for i := range original {
		if decoded[i] != original[i] {
			t.Fatalf("index %d: %f != %f", i, decoded[i], original[i])
		}
	}

	// Invalid blob length.
	if got := decodeVector([]byte{1, 2, 3}); got != nil {
		t.Fatalf("expected nil for invalid blob, got %v", got)
	}
}

func TestEscapeLike(t *testing.T) {
	tests := []struct {
		input, want string
	}{
		{"hello", "hello"},
		{"100%", `100\%`},
		{"under_score", `under\_score`},
		{`back\slash`, `back\\slash`},
		{"a%b_c\\d", `a\%b\_c\\d`},
	}
	for _, tc := range tests {
		got := escapeLike(tc.input)
		if got != tc.want {
			t.Errorf("escapeLike(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

func TestSQLiteKVStore_FileCreated(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "created.db")
	store, err := NewSQLiteKVStore(dbPath)
	if err != nil {
		t.Fatalf("NewSQLiteKVStore: %v", err)
	}
	defer store.Close()

	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		t.Fatal("database file was not created")
	}
}

func TestSQLiteKVStoreConfiguresSingleConnectionPool(t *testing.T) {
	t.Parallel()

	store := newTestSQLiteStore(t)
	if got := store.db.Stats().MaxOpenConnections; got != 1 {
		t.Fatalf("MaxOpenConnections = %d, want 1", got)
	}
}

func TestSQLiteKVStore_GetRejectsInvalidTimestamp(t *testing.T) {
	t.Parallel()

	store := newTestSQLiteStore(t)
	ctx := context.Background()

	if _, err := store.db.ExecContext(ctx,
		`INSERT INTO durable_facts (
			key, fact_class, view_type, namespace, scope_key, name, label, value, value_type,
			source, managed, confidence, review_required, tags, previous_values, evidence, metadata,
			created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		"broken-get", "imported_note", "context", "general", "default", "value", "Value", "value", "text",
		"manual", 1, 1.0, 0, "[]", "[]", "[]", `{}`, "not-a-time", time.Now().UTC().Format(time.RFC3339Nano),
	); err != nil {
		t.Fatalf("insert broken entry: %v", err)
	}

	_, err := store.Get(ctx, "broken-get")
	if err == nil {
		t.Fatal("expected Get() to fail for invalid timestamp")
	}
	if !strings.Contains(err.Error(), "parse durable fact") {
		t.Fatalf("Get() error = %v", err)
	}
}

func TestSQLiteKVStore_ListRejectsInvalidTimestamp(t *testing.T) {
	t.Parallel()

	store := newTestSQLiteStore(t)
	ctx := context.Background()

	if _, err := store.db.ExecContext(ctx,
		`INSERT INTO durable_facts (
			key, fact_class, view_type, namespace, scope_key, name, label, value, value_type,
			source, managed, confidence, review_required, tags, previous_values, evidence, metadata,
			created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		"broken-list", "imported_note", "context", "general", "default", "value", "Value", "value", "text",
		"manual", 1, 1.0, 0, "[]", "[]", "[]", `{}`, time.Now().UTC().Format(time.RFC3339Nano), "not-a-time",
	); err != nil {
		t.Fatalf("insert broken entry: %v", err)
	}

	_, err := store.List(ctx)
	if err == nil {
		t.Fatal("expected List() to fail for invalid timestamp")
	}
	if !strings.Contains(err.Error(), "parse durable fact") {
		t.Fatalf("List() error = %v", err)
	}
}

func TestSQLiteKVStoreMigratesLegacyEnvelopeEntry(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "legacy_memory.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("sql.Open() error = %v", err)
	}
	if _, err := db.Exec(createMemoryEntriesSQL); err != nil {
		_ = db.Close()
		t.Fatalf("createMemoryEntriesSQL error = %v", err)
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := db.Exec(
		`INSERT INTO memory_entries (key, value, created_at, updated_at) VALUES (?, ?, ?, ?)`,
		"profile.user.reply_language",
		encodeMemoryRecord(MemoryRecord{
			Namespace: memoryNamespaceProfile,
			ScopeKey:  "user",
			Field:     "reply_language",
			Value:     "zh-CN",
			Source:    "runtime.submit",
		}),
		now,
		now,
	); err != nil {
		_ = db.Close()
		t.Fatalf("insert legacy memory entry: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("db.Close() error = %v", err)
	}

	store, err := NewSQLiteKVStore(dbPath)
	if err != nil {
		t.Fatalf("NewSQLiteKVStore() error = %v", err)
	}
	defer store.Close()

	entry, err := store.Get(context.Background(), "profile.user.reply_language")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if entry == nil {
		t.Fatal("expected migrated entry")
	}
	if entry.FactClass != durablefact.FactClassPreference {
		t.Fatalf("unexpected migrated entry = %#v", entry)
	}
	if entry.State != MemoryActive {
		t.Fatalf("entry.State = %q, want %q", entry.State, MemoryActive)
	}

	fact, err := store.facts.Get(context.Background(), "profile.user.reply_language")
	if err != nil {
		t.Fatalf("facts.Get() error = %v", err)
	}
	if fact == nil {
		t.Fatal("expected migrated durable fact")
	}
	if fact.ReviewRequired {
		t.Fatalf("fact.ReviewRequired = %v, want false", fact.ReviewRequired)
	}
}

func TestSQLiteKVStoreMigratesLegacyRawEntryAsActiveImportedNote(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "legacy_raw_memory.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("sql.Open() error = %v", err)
	}
	if _, err := db.Exec(createMemoryEntriesSQL); err != nil {
		_ = db.Close()
		t.Fatalf("createMemoryEntriesSQL error = %v", err)
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := db.Exec(
		`INSERT INTO memory_entries (key, value, created_at, updated_at) VALUES (?, ?, ?, ?)`,
		"legacy.note",
		"free-form imported note",
		now,
		now,
	); err != nil {
		_ = db.Close()
		t.Fatalf("insert legacy raw memory entry: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("db.Close() error = %v", err)
	}

	store, err := NewSQLiteKVStore(dbPath)
	if err != nil {
		t.Fatalf("NewSQLiteKVStore() error = %v", err)
	}
	defer store.Close()

	entry, err := store.Get(context.Background(), "legacy.note")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if entry == nil {
		t.Fatal("expected migrated legacy note")
	}
	if entry.FactClass != durablefact.FactClassImportedNote {
		t.Fatalf("unexpected migrated legacy note = %#v", entry)
	}
	if entry.State != MemoryActive {
		t.Fatalf("entry.State = %q, want %q", entry.State, MemoryActive)
	}

	fact, err := store.facts.Get(context.Background(), "legacy.note")
	if err != nil {
		t.Fatalf("facts.Get() error = %v", err)
	}
	if fact == nil {
		t.Fatal("expected migrated durable fact")
	}
	if !fact.ReviewRequired {
		t.Fatalf("fact.ReviewRequired = %v, want true", fact.ReviewRequired)
	}
}

func TestSQLiteKVStoreRoundTripsExtendedMemoryFields(t *testing.T) {
	store := newTestSQLiteStore(t)
	ctx := context.Background()
	lastUsedAt := time.Now().UTC().Truncate(time.Second)

	entry, err := store.UpsertRecord(ctx, MemoryRecord{
		Namespace:       memoryNamespaceProject,
		ScopeKey:        "repo",
		Field:           "status",
		Label:           "Project Status",
		Value:           "stable",
		Source:          MemorySourceAgent,
		Score:           0.81,
		State:           MemorySuperseded,
		SupersededBy:    "project.repo.status.current",
		SessionKey:      "sess-hopclaw",
		ProjectID:       "github.com/fulcrus/hopclaw",
		MediaRefs:       []string{"artifact://local/diagram-1"},
		UsedCount:       4,
		LastUsedAt:      lastUsedAt,
		CorrectionCount: 2,
	})
	if err != nil {
		t.Fatalf("UpsertRecord() error = %v", err)
	}
	if entry == nil {
		t.Fatal("expected stored entry")
	}
	if entry.Score != 0.81 {
		t.Fatalf("entry.Score = %v, want 0.81", entry.Score)
	}
	if entry.State != MemorySuperseded {
		t.Fatalf("entry.State = %q, want %q", entry.State, MemorySuperseded)
	}
	if entry.SupersededBy != "project.repo.status.current" {
		t.Fatalf("entry.SupersededBy = %q", entry.SupersededBy)
	}
	if len(entry.MediaRefs) != 1 || entry.MediaRefs[0] != "artifact://local/diagram-1" {
		t.Fatalf("entry.MediaRefs = %#v", entry.MediaRefs)
	}
	if entry.UsedCount != 4 {
		t.Fatalf("entry.UsedCount = %d", entry.UsedCount)
	}
	if !entry.LastUsedAt.Equal(lastUsedAt) {
		t.Fatalf("entry.LastUsedAt = %v, want %v", entry.LastUsedAt, lastUsedAt)
	}
	if entry.CorrectionCount != 2 {
		t.Fatalf("entry.CorrectionCount = %d", entry.CorrectionCount)
	}
	if entry.SessionKey != "sess-hopclaw" || entry.ProjectID != "github.com/fulcrus/hopclaw" {
		t.Fatalf("entry session/project = %q/%q", entry.SessionKey, entry.ProjectID)
	}
}

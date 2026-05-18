package cli

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/fulcrus/hopclaw/config"
	"github.com/fulcrus/hopclaw/durablefact"
	"github.com/fulcrus/hopclaw/knowledge"
	"github.com/fulcrus/hopclaw/store"

	_ "modernc.org/sqlite"
)

// ---------------------------------------------------------------------------
// checkVersion
// ---------------------------------------------------------------------------

func TestCheckVersion(t *testing.T) {

	r := checkVersion()
	if r.Status != "ok" {
		t.Errorf("expected status ok, got %q", r.Status)
	}
	if r.Category != "config" {
		t.Errorf("expected category config, got %q", r.Category)
	}
	if r.Detail == "" {
		t.Error("expected non-empty detail")
	}
}

// ---------------------------------------------------------------------------
// checkConfigFile
// ---------------------------------------------------------------------------

func TestCheckConfigFile_ValidYAML(t *testing.T) {
	// Modifies global flagConfig — cannot be parallel.
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	content := `server:
  address: "127.0.0.1:16280"
store:
  backend: memory
agent:
  default_model: "gpt-4o"
models:
  openai_compat:
    base_url: "https://api.openai.com/v1"
    api_key: "test-key"
    model: "gpt-4o"
tools:
  builtins:
    enabled: true
`
	if err := os.WriteFile(cfgPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	// Override the config path via the global flag.
	old := flagConfig
	flagConfig = cfgPath
	defer func() { flagConfig = old }()

	r := checkConfigFile()
	if r.Status != "ok" {
		t.Errorf("expected ok, got %q: %s", r.Status, r.Detail)
	}
}

func TestCheckConfigFile_InvalidYAML(t *testing.T) {
	// Modifies global flagConfig — cannot be parallel.
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	// Invalid YAML: tab indentation with broken structure.
	content := "server:\n\t\taddress: [\n"
	if err := os.WriteFile(cfgPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	old := flagConfig
	flagConfig = cfgPath
	defer func() { flagConfig = old }()

	r := checkConfigFile()
	if r.Status != "fail" {
		t.Errorf("expected fail for invalid YAML, got %q: %s", r.Status, r.Detail)
	}
}

func TestCheckConfigFile_Missing(t *testing.T) {
	// Modifies global flagConfig — cannot be parallel.
	old := flagConfig
	flagConfig = filepath.Join(t.TempDir(), "nonexistent.yaml")
	defer func() { flagConfig = old }()

	r := checkConfigFile()
	// resolveConfigPath returns the flagConfig value even if it doesn't exist,
	// so Load will fail.
	if r.Status == "ok" {
		t.Error("expected non-ok status for missing config file")
	}
}

// ---------------------------------------------------------------------------
// checkAPIKeys
// ---------------------------------------------------------------------------

func TestCheckAPIKeys_Found(t *testing.T) {
	old := flagConfig
	flagConfig = ""
	defer func() { flagConfig = old }()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("OPENAI_API_KEY", "sk-test-12345678")

	r := checkAPIKeys()
	if r.Status != "ok" {
		t.Errorf("expected ok, got %q", r.Status)
	}
	if r.Category != "auth" {
		t.Errorf("expected category auth, got %q", r.Category)
	}
}

func TestCheckAPIKeys_NotFound(t *testing.T) {
	old := flagConfig
	flagConfig = ""
	defer func() { flagConfig = old }()
	t.Setenv("HOME", t.TempDir())

	// Clear all known keys.
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("GOOGLE_API_KEY", "")
	t.Setenv("DEEPSEEK_API_KEY", "")
	t.Setenv("DASHSCOPE_API_KEY", "")
	t.Setenv("XIAOMI_API_KEY", "")

	r := checkAPIKeys()
	if r.Status != "warn" {
		t.Errorf("expected warn, got %q", r.Status)
	}
	if r.Fix == "" {
		t.Error("expected fix suggestion for missing API keys")
	}
}

// ---------------------------------------------------------------------------
// checkStateDir
// ---------------------------------------------------------------------------

func TestCheckStateDir_Exists(t *testing.T) {
	// Use a temp dir as HOME so StateDir points to a real location.
	dir := t.TempDir()
	stateDir := filepath.Join(dir, ".hopclaw")
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatal(err)
	}

	t.Setenv("HOME", dir)

	r := checkStateDir()
	if r.Status != "ok" {
		t.Errorf("expected ok, got %q: %s", r.Status, r.Detail)
	}
	if r.Category != "storage" {
		t.Errorf("expected category storage, got %q", r.Category)
	}
}

func TestCheckStateDir_Missing(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	// Don't create .hopclaw — it should be missing.

	r := checkStateDir()
	if r.Status != "warn" {
		t.Errorf("expected warn, got %q: %s", r.Status, r.Detail)
	}
	if r.Fix == "" {
		t.Error("expected a fix suggestion")
	}
}

// ---------------------------------------------------------------------------
// checkConfigSyntax
// ---------------------------------------------------------------------------

func TestCheckConfigSyntax_Valid(t *testing.T) {
	// Modifies global flagConfig — cannot be parallel.
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	content := "server:\n  address: \"127.0.0.1:16280\"\n"
	if err := os.WriteFile(cfgPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	old := flagConfig
	flagConfig = cfgPath
	defer func() { flagConfig = old }()

	r := checkConfigSyntax()
	if r.Status != "ok" {
		t.Errorf("expected ok, got %q: %s", r.Status, r.Detail)
	}
}

func TestCheckConfigSyntax_Invalid(t *testing.T) {
	// Modifies global flagConfig — cannot be parallel.
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	content := "server: [\n  invalid yaml\n"
	if err := os.WriteFile(cfgPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	old := flagConfig
	flagConfig = cfgPath
	defer func() { flagConfig = old }()

	r := checkConfigSyntax()
	if r.Status != "fail" {
		t.Errorf("expected fail, got %q: %s", r.Status, r.Detail)
	}
}

// ---------------------------------------------------------------------------
// checkConfigMigration
// ---------------------------------------------------------------------------

func TestCheckConfigMigration_DeprecatedKeys(t *testing.T) {
	// Modifies global flagConfig — cannot be parallel.
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	content := `server:
  address: "127.0.0.1:16280"
  auth_token: "my-secret-token"
`
	if err := os.WriteFile(cfgPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	old := flagConfig
	flagConfig = cfgPath
	defer func() { flagConfig = old }()

	r := checkConfigMigration()
	if r.Status != "warn" {
		t.Errorf("expected warn for deprecated key, got %q: %s", r.Status, r.Detail)
	}
}

func TestCheckConfigMigration_Clean(t *testing.T) {
	// Modifies global flagConfig — cannot be parallel.
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	content := `server:
  address: "127.0.0.1:16280"
auth:
  bearer_token: "my-token"
`
	if err := os.WriteFile(cfgPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	old := flagConfig
	flagConfig = cfgPath
	defer func() { flagConfig = old }()

	r := checkConfigMigration()
	// Note: "auth_token" does not appear in this file at all (only "bearer_token").
	if r.Status != "ok" {
		t.Errorf("expected ok, got %q: %s", r.Status, r.Detail)
	}
}

// ---------------------------------------------------------------------------
// checkSessionLocks
// ---------------------------------------------------------------------------

func TestCheckSessionLocks_NoStale(t *testing.T) {
	dir := t.TempDir()
	stateDir := filepath.Join(dir, ".hopclaw")
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// Create a recent lock file.
	lockPath := filepath.Join(stateDir, "session-001.lock")
	if err := os.WriteFile(lockPath, []byte("locked"), 0o644); err != nil {
		t.Fatal(err)
	}

	t.Setenv("HOME", dir)

	r := checkSessionLocks()
	if r.Status != "ok" {
		t.Errorf("expected ok (fresh lock), got %q: %s", r.Status, r.Detail)
	}
}

func TestCheckSessionLocks_StaleFound(t *testing.T) {
	dir := t.TempDir()
	stateDir := filepath.Join(dir, ".hopclaw")
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// Create a stale lock file (set mtime to 2 hours ago).
	lockPath := filepath.Join(stateDir, "session-old.lock")
	if err := os.WriteFile(lockPath, []byte("locked"), 0o644); err != nil {
		t.Fatal(err)
	}
	staleTime := time.Now().Add(-2 * time.Hour)
	if err := os.Chtimes(lockPath, staleTime, staleTime); err != nil {
		t.Fatal(err)
	}

	t.Setenv("HOME", dir)

	r := checkSessionLocks()
	if r.Status != "warn" {
		t.Errorf("expected warn for stale lock, got %q: %s", r.Status, r.Detail)
	}
}

// ---------------------------------------------------------------------------
// checkPlatformNotes
// ---------------------------------------------------------------------------

func TestCheckPlatformNotes_ReturnsResult(t *testing.T) {

	r := checkPlatformNotes()
	if r.Status == "" {
		t.Error("expected non-empty status")
	}
	if r.Category != "platform" {
		t.Errorf("expected category platform, got %q", r.Category)
	}
	// On the current OS, Detail should mention the platform.
	if !containsAny(r.Detail, runtime.GOOS, "detected", "no special notes") {
		t.Errorf("unexpected detail: %s", r.Detail)
	}
}

// ---------------------------------------------------------------------------
// Report output
// ---------------------------------------------------------------------------

func TestPrintDoctorReportIncludesSummaryAndFixes(t *testing.T) {

	checks := []checkResult{
		{Category: "config", Name: "Config file", Status: "ok", Detail: "/path/config.yaml"},
		{Category: "auth", Name: "Authentication", Status: "warn", Detail: "token missing", Fix: "set auth token"},
		{Category: "storage", Name: "Audit DB", Status: "fail", Detail: "corrupt", Fix: "restore backup"},
	}

	var buf bytes.Buffer
	printDoctorReport(&buf, checks)

	output := buf.String()
	for _, token := range []string{"✓", "⚠", "✗", "1 passed · 1 failed · 1 warning", "Fix: set auth token", "restore backup"} {
		if !strings.Contains(output, token) {
			t.Fatalf("doctor report missing %q in %q", token, output)
		}
	}
}

// ---------------------------------------------------------------------------
// JSON output
// ---------------------------------------------------------------------------

func TestDoctorJSONOutput(t *testing.T) {

	checks := []checkResult{
		{Category: "config", Name: "Version", Status: "ok", Detail: "1.0.0"},
		{Category: "auth", Name: "API keys", Status: "warn", Detail: "none", Fix: config.MissingAPIKeyDoctorFix()},
	}

	data, err := json.Marshal(checks)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded []checkResult
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if len(decoded) != 2 {
		t.Fatalf("expected 2 results, got %d", len(decoded))
	}
	if decoded[0].Category != "config" {
		t.Errorf("expected category config, got %q", decoded[0].Category)
	}
	if decoded[1].Fix != config.MissingAPIKeyDoctorFix() {
		t.Errorf("expected fix suggestion, got %q", decoded[1].Fix)
	}
}

// ---------------------------------------------------------------------------
// checkResult struct
// ---------------------------------------------------------------------------

func TestCheckResultFields(t *testing.T) {

	r := checkResult{
		Category: "config",
		Name:     "test",
		Status:   "ok",
		Detail:   "detail",
		Fix:      "fix suggestion",
	}

	data, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}

	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatal(err)
	}

	for _, key := range []string{"category", "name", "status", "detail", "fix"} {
		if _, ok := m[key]; !ok {
			t.Errorf("missing JSON key %q", key)
		}
	}
}

// ---------------------------------------------------------------------------
// iconForStatus
// ---------------------------------------------------------------------------

func TestIconForStatus(t *testing.T) {

	tests := []struct {
		input string
		want  string
	}{
		{"ok", "✓"},
		{"warn", "⚠"},
		{"fail", "✗"},
		{"unknown", "?"},
	}

	for _, tc := range tests {
		if got := iconForStatus(tc.input); got != tc.want {
			t.Errorf("iconForStatus(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

func TestNewDoctorCmdAddsSectionSubcommands(t *testing.T) {

	cmd := newDoctorCmd()
	for _, name := range []string{"auth", "config", "connectivity", "skills", "storage", "security", "platform"} {
		if _, _, err := cmd.Find([]string{name}); err != nil {
			t.Fatalf("doctor subcommand %q not found: %v", name, err)
		}
	}
}

func TestCollectDoctorChecksForSectionsFiltersRequestedSection(t *testing.T) {
	old := flagConfig
	flagConfig = ""
	defer func() { flagConfig = old }()
	t.Setenv("HOME", t.TempDir())

	checks := collectDoctorChecksForSections([]doctorSection{doctorSectionSecurity})
	if len(checks) == 0 {
		t.Fatal("expected security checks")
	}
	for _, check := range checks {
		if check.Category != "security" {
			t.Fatalf("unexpected category %q in %#v", check.Category, check)
		}
	}
}

func TestCheckConfigPermissionsWarnsOnBroadPermissions(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(cfgPath, []byte("server:\n  address: \"127.0.0.1:16280\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	old := flagConfig
	flagConfig = cfgPath
	defer func() { flagConfig = old }()

	result := checkConfigPermissions()
	if result.Status != "warn" {
		t.Fatalf("expected warn, got %#v", result)
	}
	if !strings.Contains(result.Fix, "chmod 600") {
		t.Fatalf("expected chmod fix, got %#v", result)
	}
}

func TestCheckSkillDependenciesSummarizesMissingChecks(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/operator/skills":
			writeJSONResponse(t, w, http.StatusOK, map[string]any{
				"items": []map[string]any{
					{"name": "ready-skill", "ready": true, "eligible": true},
					{"name": "needs-git", "ready": false, "eligible": false},
				},
				"count": 2,
			})
		case "/operator/skills/needs-git":
			writeJSONResponse(t, w, http.StatusOK, map[string]any{
				"name":     "needs-git",
				"ready":    false,
				"eligible": false,
				"checks": []map[string]any{
					{"name": "git", "status": "missing", "hint": "install git"},
				},
				"next_actions": []string{"set env API_TOKEN"},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	cfgPath := writeInteractiveConfig(t, server.URL)
	old := flagConfig
	flagConfig = cfgPath
	defer func() { flagConfig = old }()
	t.Setenv("HOME", t.TempDir())

	result := checkSkillDependencies()
	if result.Status != "warn" {
		t.Fatalf("expected warn, got %#v", result)
	}
	if !strings.Contains(result.Detail, "1/2 skill(s) ready") || !strings.Contains(result.Detail, "git") {
		t.Fatalf("unexpected detail %#v", result)
	}
	if !strings.Contains(result.Fix, "install git") {
		t.Fatalf("unexpected fix %#v", result)
	}
}

func TestCheckRuntimeDatabaseReportsIntegrity(t *testing.T) {
	result := runDoctorStorageDBCheck(t, "runtime.db", checkRuntimeDatabase)
	if result.Name != "Runtime DB" {
		t.Fatalf("result.Name = %q, want Runtime DB", result.Name)
	}
}

func TestCheckControlDatabaseReportsIntegrity(t *testing.T) {
	result := runDoctorStorageDBCheck(t, "control.db", checkControlDatabase)
	if result.Name != "Control DB" {
		t.Fatalf("result.Name = %q, want Control DB", result.Name)
	}
}

func TestCheckKnowledgeDatabaseReportsIntegrity(t *testing.T) {
	result := runDoctorStorageDBCheck(t, "knowledge.db", checkKnowledgeDatabase)
	if result.Name != "Knowledge DB" {
		t.Fatalf("result.Name = %q, want Knowledge DB", result.Name)
	}
}

func TestCheckKnowledgeIndexesReportsOK(t *testing.T) {
	root := t.TempDir()
	knowledgeStore, err := knowledge.NewSQLiteStore(filepath.Join(root, "knowledge.db"))
	if err != nil {
		t.Fatalf("knowledge.NewSQLiteStore: %v", err)
	}

	now := time.Now().UTC()
	if _, err := knowledgeStore.UpsertSource(context.Background(), knowledge.Source{
		ID:      "source-1",
		Name:    "Ops Docs",
		Kind:    knowledge.SourceKindLocalDir,
		Enabled: true,
		Status:  knowledge.SourceStatusReady,
	}); err != nil {
		t.Fatalf("UpsertSource: %v", err)
	}
	if err := knowledgeStore.UpsertDocument(context.Background(), knowledge.Document{
		ID:              "doc-1",
		SourceID:        "source-1",
		Kind:            knowledge.DocumentKindFile,
		Title:           "Rollback Guide",
		Path:            "rollback.md",
		URI:             "/tmp/rollback.md",
		Locale:          "zh-CN",
		ContentHash:     "hash-doc-1",
		Bytes:           16,
		ChunkCount:      1,
		SourceUpdatedAt: now,
		SyncedAt:        now,
	}, []knowledge.Chunk{{
		ID:         "chunk-1",
		SourceID:   "source-1",
		DocumentID: "doc-1",
		Ordinal:    0,
		Title:      "Rollback Guide",
		Path:       "rollback.md",
		URI:        "/tmp/rollback.md",
		Locale:     "zh-CN",
		Content:    "回滚指南",
		Preview:    "回滚指南",
		Hash:       "hash-chunk-1",
		Bytes:      12,
		UpdatedAt:  now,
	}}); err != nil {
		t.Fatalf("UpsertDocument: %v", err)
	}

	cfgPath := filepath.Join(t.TempDir(), "config.yaml")
	content := "store:\n  backend: sqlite\n  path: " + root + "\n"
	if err := os.WriteFile(cfgPath, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	old := flagConfig
	flagConfig = cfgPath
	defer func() { flagConfig = old }()

	result := checkKnowledgeIndexes()
	if result.Status != "ok" {
		t.Fatalf("expected ok, got %#v", result)
	}
	if !strings.Contains(result.Detail, "1 source(s), 1 document(s), 1 chunk(s), FTS=1, vectors=0, locales=1") {
		t.Fatalf("unexpected detail %#v", result)
	}
	if !strings.Contains(result.Detail, "semantic vectors not projected") {
		t.Fatalf("unexpected detail %#v", result)
	}
}

func TestCheckKnowledgeIndexesFailsWhenFTSProjectionIsMissing(t *testing.T) {
	root := t.TempDir()
	dbPath := filepath.Join(root, "knowledge.db")
	knowledgeStore, err := knowledge.NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatalf("knowledge.NewSQLiteStore: %v", err)
	}

	now := time.Now().UTC()
	if _, err := knowledgeStore.UpsertSource(context.Background(), knowledge.Source{
		ID:      "source-1",
		Name:    "Ops Docs",
		Kind:    knowledge.SourceKindLocalDir,
		Enabled: true,
		Status:  knowledge.SourceStatusReady,
	}); err != nil {
		t.Fatalf("UpsertSource: %v", err)
	}
	if err := knowledgeStore.UpsertDocument(context.Background(), knowledge.Document{
		ID:              "doc-1",
		SourceID:        "source-1",
		Kind:            knowledge.DocumentKindFile,
		Title:           "Rollback Guide",
		Path:            "rollback.md",
		URI:             "/tmp/rollback.md",
		Locale:          "en",
		ContentHash:     "hash-doc-1",
		Bytes:           16,
		ChunkCount:      1,
		SourceUpdatedAt: now,
		SyncedAt:        now,
	}, []knowledge.Chunk{{
		ID:         "chunk-1",
		SourceID:   "source-1",
		DocumentID: "doc-1",
		Ordinal:    0,
		Title:      "Rollback Guide",
		Path:       "rollback.md",
		URI:        "/tmp/rollback.md",
		Locale:     "en",
		Content:    "Rollback guide",
		Preview:    "Rollback guide",
		Hash:       "hash-chunk-1",
		Bytes:      14,
		UpdatedAt:  now,
	}}); err != nil {
		t.Fatalf("UpsertDocument: %v", err)
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	defer db.Close()
	if _, err := db.Exec(`DELETE FROM knowledge_chunk_fts WHERE chunk_id = ?`, "chunk-1"); err != nil {
		t.Fatalf("delete knowledge_chunk_fts: %v", err)
	}

	cfgPath := filepath.Join(t.TempDir(), "config.yaml")
	content := "store:\n  backend: sqlite\n  path: " + root + "\n"
	if err := os.WriteFile(cfgPath, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	old := flagConfig
	flagConfig = cfgPath
	defer func() { flagConfig = old }()

	result := checkKnowledgeIndexes()
	if result.Status != "fail" {
		t.Fatalf("expected fail, got %#v", result)
	}
	if !strings.Contains(result.Detail, "missing 1 FTS projection(s)") {
		t.Fatalf("unexpected detail %#v", result)
	}
	if !strings.Contains(result.Fix, "re-sync affected knowledge sources") {
		t.Fatalf("unexpected fix %#v", result)
	}
}

func TestCheckAuditDatabaseReportsIntegrity(t *testing.T) {
	result := runDoctorStorageDBCheck(t, "audit.db", checkAuditDatabase)
	if result.Name != "Audit DB" {
		t.Fatalf("result.Name = %q, want Audit DB", result.Name)
	}
}

func runDoctorStorageDBCheck(t *testing.T, dbName string, check func() checkResult) checkResult {
	t.Helper()

	root := t.TempDir()
	db, err := store.OpenDB(filepath.Join(root, dbName))
	if err != nil {
		t.Fatalf("OpenDB(%s): %v", dbName, err)
	}
	_ = db.Close()

	cfgPath := filepath.Join(t.TempDir(), "config.yaml")
	content := "store:\n  backend: sqlite\n  path: " + root + "\n"
	if err := os.WriteFile(cfgPath, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	old := flagConfig
	flagConfig = cfgPath
	defer func() { flagConfig = old }()

	result := check()
	if result.Status != "ok" {
		t.Fatalf("expected ok, got %#v", result)
	}
	if !strings.Contains(result.Detail, "integrity OK") {
		t.Fatalf("unexpected detail %#v", result)
	}
	return result
}

func TestCheckDurableFactsSummaryReportsOK(t *testing.T) {
	root := t.TempDir()
	knowledgeDB, err := store.OpenDB(filepath.Join(root, "knowledge.db"))
	if err != nil {
		t.Fatalf("OpenDB: %v", err)
	}
	defer knowledgeDB.Close()

	knowledgeFacts, err := durablefact.NewSQLiteStore(knowledgeDB)
	if err != nil {
		t.Fatalf("durablefact.NewSQLiteStore: %v", err)
	}
	if _, err := knowledgeFacts.Upsert(context.Background(), durablefact.Fact{
		Key:       "profile.user.reply_language",
		FactClass: durablefact.FactClassPreference,
		ViewType:  durablefact.ViewTypeContext,
		Namespace: "profile",
		ScopeKey:  "user",
		Name:      "reply_language",
		Value:     "zh-CN",
	}); err != nil {
		t.Fatalf("facts.Upsert: %v", err)
	}

	controlDB, err := store.OpenDB(filepath.Join(root, "control.db"))
	if err != nil {
		t.Fatalf("OpenDB(control): %v", err)
	}
	defer controlDB.Close()

	controlFacts, err := durablefact.NewSQLiteStore(controlDB)
	if err != nil {
		t.Fatalf("durablefact.NewSQLiteStore(control): %v", err)
	}
	if _, err := controlFacts.Upsert(context.Background(), durablefact.Fact{
		Key:       "config.provider.openai",
		FactClass: durablefact.FactClassSystemConfig,
		ViewType:  durablefact.ViewTypeConfigProvider,
		Namespace: "provider",
		Name:      "openai",
		Value:     `{"api":"openai-responses"}`,
		ValueType: durablefact.ValueTypeJSON,
		Source:    "yaml",
	}); err != nil {
		t.Fatalf("control facts.Upsert: %v", err)
	}

	cfgPath := filepath.Join(t.TempDir(), "config.yaml")
	content := "store:\n  backend: sqlite\n  path: " + root + "\n"
	if err := os.WriteFile(cfgPath, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	old := flagConfig
	flagConfig = cfgPath
	defer func() { flagConfig = old }()

	result := checkDurableFactsSummary()
	if result.Status != "ok" {
		t.Fatalf("expected ok, got %#v", result)
	}
	if !strings.Contains(result.Detail, "2 durable fact(s)") ||
		!strings.Contains(result.Detail, "context=1, config=1") ||
		!strings.Contains(result.Detail, "0 review_required") {
		t.Fatalf("unexpected detail %#v", result)
	}
}

func TestCheckDurableFactsSummaryReportsReviewBacklog(t *testing.T) {
	root := t.TempDir()
	knowledgeDB, err := store.OpenDB(filepath.Join(root, "knowledge.db"))
	if err != nil {
		t.Fatalf("OpenDB: %v", err)
	}
	defer knowledgeDB.Close()

	knowledgeFacts, err := durablefact.NewSQLiteStore(knowledgeDB)
	if err != nil {
		t.Fatalf("durablefact.NewSQLiteStore: %v", err)
	}
	if _, err := knowledgeFacts.Upsert(context.Background(), durablefact.Fact{
		Key:       "profile.user.reply_language",
		FactClass: durablefact.FactClassPreference,
		ViewType:  durablefact.ViewTypeContext,
		Namespace: "profile",
		ScopeKey:  "user",
		Name:      "reply_language",
		Value:     "zh-CN",
	}); err != nil {
		t.Fatalf("knowledge facts.Upsert: %v", err)
	}

	controlDB, err := store.OpenDB(filepath.Join(root, "control.db"))
	if err != nil {
		t.Fatalf("OpenDB(control): %v", err)
	}
	defer controlDB.Close()

	controlFacts, err := durablefact.NewSQLiteStore(controlDB)
	if err != nil {
		t.Fatalf("durablefact.NewSQLiteStore(control): %v", err)
	}
	if _, err := controlFacts.Upsert(context.Background(), durablefact.Fact{
		Key:            "general.default.note",
		FactClass:      durablefact.FactClassSystemConfig,
		ViewType:       durablefact.ViewTypeConfigSetting,
		Namespace:      "setting",
		Name:           "section.agent",
		Value:          `{"default_model":"gpt-4.1"}`,
		ValueType:      durablefact.ValueTypeJSON,
		ReviewRequired: true,
	}); err != nil {
		t.Fatalf("control facts.Upsert: %v", err)
	}

	cfgPath := filepath.Join(t.TempDir(), "config.yaml")
	content := "store:\n  backend: sqlite\n  path: " + root + "\n"
	if err := os.WriteFile(cfgPath, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	old := flagConfig
	flagConfig = cfgPath
	defer func() { flagConfig = old }()

	result := checkDurableFactsSummary()
	if result.Status != "warn" {
		t.Fatalf("expected warn, got %#v", result)
	}
	if !strings.Contains(result.Detail, "2 durable fact(s)") ||
		!strings.Contains(result.Detail, "context=1, config=1") ||
		!strings.Contains(result.Detail, "1 review_required") {
		t.Fatalf("unexpected detail %#v", result)
	}
	if !strings.Contains(result.Fix, "review ambiguous durable facts") {
		t.Fatalf("unexpected fix %#v", result)
	}
}

func TestCheckPlatformRuntimeIncludesGoVersion(t *testing.T) {

	result := checkPlatformRuntime()
	if result.Status == "" {
		t.Fatalf("expected non-empty status, got %#v", result)
	}
	if !strings.Contains(result.Detail, runtime.GOOS) || !strings.Contains(strings.ToLower(result.Detail), "go") {
		t.Fatalf("unexpected detail %#v", result)
	}
}

func TestCheckProviderConnectivity_UsesConfiguredBaseURL(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodHead {
			t.Fatalf("method = %s, want HEAD", r.Method)
		}
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()

	cfgPath := filepath.Join(t.TempDir(), "config.yaml")
	content := "models:\n  providers:\n    remote:\n      api: openai-completions\n      base_url: " + server.URL + "\n"
	if err := os.WriteFile(cfgPath, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	old := flagConfig
	flagConfig = cfgPath
	defer func() { flagConfig = old }()

	result := checkProviderConnectivity()
	if result.Status != "ok" {
		t.Fatalf("expected ok, got %#v", result)
	}
	if !strings.Contains(result.Detail, "1 provider(s) reachable") {
		t.Fatalf("unexpected detail %#v", result)
	}
}

func TestCheckGatewayPortDetectsConflict(t *testing.T) {
	server := httptest.NewServer(http.NotFoundHandler())
	defer server.Close()

	cfgPath := filepath.Join(t.TempDir(), "config.yaml")
	content := "server:\n  address: \"" + strings.TrimPrefix(server.URL, "http://") + "\"\n"
	if err := os.WriteFile(cfgPath, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	old := flagConfig
	flagConfig = cfgPath
	defer func() { flagConfig = old }()

	result := checkGatewayPort()
	if result.Status != "fail" {
		t.Fatalf("expected fail, got %#v", result)
	}
	if !strings.Contains(result.Detail, "already in use") {
		t.Fatalf("unexpected detail %#v", result)
	}
}

func TestCheckInstalledPluginsFindsValidManifest(t *testing.T) {
	dir := t.TempDir()
	pluginDir := filepath.Join(dir, ".hopclaw", "plugins", "demo")
	if err := os.MkdirAll(pluginDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pluginDir, "hopclaw.plugin.yaml"), []byte("name: demo\nversion: 1.0.0\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	oldWD, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = os.Chdir(oldWD)
	}()

	result := checkInstalledPlugins()
	if result.Status != "ok" {
		t.Fatalf("expected ok, got %#v", result)
	}
	if !strings.Contains(result.Detail, "1 plugin manifest(s) valid") {
		t.Fatalf("unexpected detail %#v", result)
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func containsAny(s string, substrs ...string) bool {
	for _, sub := range substrs {
		if len(sub) > 0 && contains(s, sub) {
			return true
		}
	}
	return false
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && searchString(s, substr)
}

func searchString(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

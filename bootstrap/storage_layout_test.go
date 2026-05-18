package bootstrap

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/fulcrus/hopclaw/artifact"
	"github.com/fulcrus/hopclaw/config"
	"github.com/fulcrus/hopclaw/eventbus"
	persiststore "github.com/fulcrus/hopclaw/store"
	"github.com/fulcrus/hopclaw/usage"
)

func openBootstrapTestDB(t *testing.T, path string) *sql.DB {
	t.Helper()
	db, err := persiststore.OpenDB(path)
	if err != nil {
		t.Fatalf("OpenDB(%s): %v", path, err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func TestResolveStorageLayoutDefaultsToSplitDatabases(t *testing.T) {
	t.Parallel()

	layout := resolveStorageLayout(config.Config{})
	if layout.root != defaultStorageRoot {
		t.Fatalf("layout.root = %q, want %q", layout.root, defaultStorageRoot)
	}
	if layout.runtimeDBPath != filepath.Join(defaultStorageRoot, "runtime.db") {
		t.Fatalf("runtimeDBPath = %q", layout.runtimeDBPath)
	}
	if layout.controlDBPath != filepath.Join(defaultStorageRoot, "control.db") {
		t.Fatalf("controlDBPath = %q", layout.controlDBPath)
	}
	if layout.knowledgeDBPath != filepath.Join(defaultStorageRoot, "knowledge.db") {
		t.Fatalf("knowledgeDBPath = %q", layout.knowledgeDBPath)
	}
	if layout.auditDBPath != filepath.Join(defaultStorageRoot, "audit.db") {
		t.Fatalf("auditDBPath = %q", layout.auditDBPath)
	}
}

func TestInitMemoryStoreUsesKnowledgeDBByDefault(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	cfg := config.Config{Store: config.StoreConfig{Backend: "sqlite", Path: root}}
	layout := resolveStorageLayout(cfg)
	knowledgeDB := openBootstrapTestDB(t, layout.knowledgeDBPath)
	runtimeDB := openBootstrapTestDB(t, layout.runtimeDBPath)

	memoryStore, err := initMemoryStore(cfg, knowledgeDB)
	if err != nil {
		t.Fatalf("initMemoryStore() error = %v", err)
	}
	if err := memoryStore.Set(context.Background(), "profile.reply_language", "zh-CN"); err != nil {
		t.Fatalf("memoryStore.Set() error = %v", err)
	}

	var knowledgeFacts int
	if err := knowledgeDB.QueryRow(`SELECT COUNT(*) FROM durable_facts WHERE view_type = ?`, "context").Scan(&knowledgeFacts); err != nil {
		t.Fatalf("knowledge durable_facts count: %v", err)
	}
	if knowledgeFacts != 1 {
		t.Fatalf("knowledge durable_facts = %d, want 1", knowledgeFacts)
	}

	var runtimeMemoryTables int
	if err := runtimeDB.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = 'durable_facts'`).Scan(&runtimeMemoryTables); err != nil {
		t.Fatalf("runtime durable_facts schema count: %v", err)
	}
	if runtimeMemoryTables != 0 {
		t.Fatalf("runtime durable_facts schema count = %d, want 0", runtimeMemoryTables)
	}
}

func TestResolveRuntimeArtifactStoreUsesRuntimeDB(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	cfg := config.Config{Store: config.StoreConfig{Backend: "sqlite", Path: root}}
	layout := resolveStorageLayout(cfg)
	runtimeDB := openBootstrapTestDB(t, layout.runtimeDBPath)
	controlDB := openBootstrapTestDB(t, layout.controlDBPath)

	artifactStore, err := resolveRuntimeArtifactStore(cfg, &preparedBootstrapFoundation{runtimeDB: runtimeDB}, nil)
	if err != nil {
		t.Fatalf("resolveRuntimeArtifactStore() error = %v", err)
	}
	if _, err := artifactStore.Put(context.Background(), artifact.PutRequest{
		Kind:        "test_output",
		ContentType: "text/plain",
		Body:        []byte("hello"),
	}); err != nil {
		t.Fatalf("artifactStore.Put() error = %v", err)
	}

	var runtimeArtifacts int
	if err := runtimeDB.QueryRow(`SELECT COUNT(*) FROM artifacts`).Scan(&runtimeArtifacts); err != nil {
		t.Fatalf("runtime artifacts count: %v", err)
	}
	if runtimeArtifacts != 1 {
		t.Fatalf("runtime artifacts = %d, want 1", runtimeArtifacts)
	}

	var controlArtifacts int
	if err := controlDB.QueryRow(`SELECT COUNT(*) FROM artifacts`).Scan(&controlArtifacts); err != nil {
		t.Fatalf("control artifacts count: %v", err)
	}
	if controlArtifacts != 0 {
		t.Fatalf("control artifacts = %d, want 0", controlArtifacts)
	}
}

func TestPrepareRuntimeUsageInfraSplitsUsageAndEventPersistence(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	cfg := config.Config{Store: config.StoreConfig{Backend: "sqlite", Path: root}}
	layout := resolveStorageLayout(cfg)
	controlDB := openBootstrapTestDB(t, layout.controlDBPath)
	auditDB := openBootstrapTestDB(t, layout.auditDBPath)
	foundation := &preparedBootstrapFoundation{
		storeDB: controlDB,
		auditDB: auditDB,
		bus:     eventbus.NewInMemoryBus(),
	}

	usageStore, reader, err := prepareRuntimeUsageInfra(cfg, foundation)
	if err != nil {
		t.Fatalf("prepareRuntimeUsageInfra() error = %v", err)
	}
	if reader == nil {
		t.Fatal("expected runtime event reader")
	}

	now := time.Now().UTC()
	if err := usageStore.Record(context.Background(), usage.Record{
		ID:         "usage-1",
		RunID:      "run-1",
		SessionID:  "sess-1",
		Model:      "gpt-test",
		RecordType: usage.RecordTypeModelCall,
		CreatedAt:  now,
	}); err != nil {
		t.Fatalf("usageStore.Record() error = %v", err)
	}
	if err := foundation.bus.Publish(context.Background(), eventbus.Event{
		ID:        "evt-usage-1",
		Type:      eventbus.EventRunCompleted,
		RunID:     "run-1",
		SessionID: "sess-1",
		Time:      now,
	}); err != nil {
		t.Fatalf("bus.Publish() error = %v", err)
	}

	replayed, err := reader.ReplayContext(context.Background())
	if err != nil {
		t.Fatalf("reader.ReplayContext() error = %v", err)
	}
	if len(replayed) != 1 || replayed[0].ID != "evt-usage-1" {
		t.Fatalf("replayed events = %#v", replayed)
	}

	var controlUsage int
	if err := controlDB.QueryRow(`SELECT COUNT(*) FROM usage_records`).Scan(&controlUsage); err != nil {
		t.Fatalf("control usage count: %v", err)
	}
	if controlUsage != 1 {
		t.Fatalf("control usage_records = %d, want 1", controlUsage)
	}

	var auditUsage int
	if err := auditDB.QueryRow(`SELECT COUNT(*) FROM usage_records`).Scan(&auditUsage); err != nil {
		t.Fatalf("audit usage count: %v", err)
	}
	if auditUsage != 0 {
		t.Fatalf("audit usage_records = %d, want 0", auditUsage)
	}

	var controlEvents int
	if err := controlDB.QueryRow(`SELECT COUNT(*) FROM events`).Scan(&controlEvents); err != nil {
		t.Fatalf("control events count: %v", err)
	}
	if controlEvents != 0 {
		t.Fatalf("control events = %d, want 0", controlEvents)
	}

	var auditEvents int
	if err := auditDB.QueryRow(`SELECT COUNT(*) FROM events`).Scan(&auditEvents); err != nil {
		t.Fatalf("audit events count: %v", err)
	}
	if auditEvents != 1 {
		t.Fatalf("audit events = %d, want 1", auditEvents)
	}
}

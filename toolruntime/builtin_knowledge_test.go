package toolruntime

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/fulcrus/hopclaw/agent"
	"github.com/fulcrus/hopclaw/knowledge"
)

func TestKnowledgeBuiltinsListSearchAndSync(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	docRoot := filepath.Join(root, "docs")
	if err := os.MkdirAll(docRoot, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(docRoot, "guide.md"), []byte("Deployment checklist and rollback steps."), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	store, err := knowledge.NewSQLiteStore(filepath.Join(t.TempDir(), "knowledge.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore() error = %v", err)
	}
	svc, err := knowledge.NewService(store, nil)
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	source, err := svc.UpsertSource(context.Background(), knowledge.Source{
		Name:    "Docs",
		Kind:    knowledge.SourceKindLocalDir,
		Enabled: true,
		Path:    docRoot,
	})
	if err != nil {
		t.Fatalf("UpsertSource() error = %v", err)
	}

	builtins := NewBuiltins(BuiltinsConfig{Root: root, MaxReadBytes: 64 * 1024})
	builtins.ApplyBindings(BuiltinsBindings{KnowledgeService: svc})
	run := &agent.Run{ID: "run-knowledge"}
	sess := &agent.Session{ID: "session-knowledge"}

	syncPayload := execBuiltinPayload(t, builtins, run, sess, "knowledge.sync", map[string]any{"source_id": source.ID})
	if ok, _ := syncPayload["success"].(bool); !ok {
		t.Fatalf("knowledge.sync success = %#v", syncPayload)
	}

	listPayload := execBuiltinPayload(t, builtins, run, sess, "knowledge.sources", map[string]any{"enabled_only": true})
	if count, _ := listPayload["count"].(float64); int(count) != 1 {
		t.Fatalf("knowledge.sources count = %#v", listPayload)
	}

	searchPayload := execBuiltinPayload(t, builtins, run, sess, "knowledge.search", map[string]any{"query": "rollback"})
	results, _ := searchPayload["results"].([]any)
	if len(results) == 0 {
		t.Fatalf("knowledge.search results = %#v", searchPayload)
	}
}

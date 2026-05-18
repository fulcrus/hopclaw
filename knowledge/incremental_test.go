package knowledge

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

type countingEmbeddingClient struct {
	mu         sync.Mutex
	callCount  int
	textsTotal int
}

func (c *countingEmbeddingClient) Embed(_ context.Context, texts []string) ([][]float32, error) {
	c.mu.Lock()
	c.callCount++
	c.textsTotal += len(texts)
	c.mu.Unlock()

	out := make([][]float32, 0, len(texts))
	for _, text := range texts {
		lower := strings.ToLower(strings.TrimSpace(text))
		switch {
		case strings.Contains(lower, "rollback"), strings.Contains(text, "回滚"):
			out = append(out, []float32{1, 0, 0})
		case strings.Contains(lower, "invoice"), strings.Contains(text, "发票"):
			out = append(out, []float32{0, 1, 0})
		default:
			out = append(out, []float32{0, 0, 1})
		}
	}
	return out, nil
}

func (c *countingEmbeddingClient) counts() (calls int, texts int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.callCount, c.textsTotal
}

func TestServiceSyncSourceEmbedsOnlyChangedDocuments(t *testing.T) {
	t.Parallel()

	docRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(docRoot, "a.md"), []byte("Rollback checklist alpha"), 0o644); err != nil {
		t.Fatalf("write a.md: %v", err)
	}
	if err := os.WriteFile(filepath.Join(docRoot, "b.md"), []byte("Invoice policy beta"), 0o644); err != nil {
		t.Fatalf("write b.md: %v", err)
	}

	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "knowledge.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore() error = %v", err)
	}
	embedding := &countingEmbeddingClient{}
	svc, err := NewService(store, embedding)
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	source, err := svc.UpsertSource(context.Background(), Source{
		Name:    "Docs",
		Kind:    SourceKindLocalDir,
		Enabled: true,
		Path:    docRoot,
	})
	if err != nil {
		t.Fatalf("UpsertSource() error = %v", err)
	}

	if _, err := svc.SyncSource(context.Background(), source.ID); err != nil {
		t.Fatalf("SyncSource(first) error = %v", err)
	}
	_, firstTexts := embedding.counts()
	if firstTexts != 2 {
		t.Fatalf("first sync embedded %d texts, want 2", firstTexts)
	}

	before, err := store.ListDocuments(context.Background(), source.ID)
	if err != nil {
		t.Fatalf("ListDocuments(before) error = %v", err)
	}
	beforeByID := make(map[string]Document, len(before))
	for _, item := range before {
		beforeByID[item.ID] = item
	}

	time.Sleep(10 * time.Millisecond)
	if err := os.WriteFile(filepath.Join(docRoot, "a.md"), []byte("Rollback checklist alpha updated"), 0o644); err != nil {
		t.Fatalf("rewrite a.md: %v", err)
	}
	if _, err := svc.SyncSource(context.Background(), source.ID); err != nil {
		t.Fatalf("SyncSource(second) error = %v", err)
	}
	_, secondTexts := embedding.counts()
	if secondTexts != 3 {
		t.Fatalf("second sync embedded %d texts total, want 3", secondTexts)
	}

	after, err := store.ListDocuments(context.Background(), source.ID)
	if err != nil {
		t.Fatalf("ListDocuments(after) error = %v", err)
	}
	afterByID := make(map[string]Document, len(after))
	for _, item := range after {
		afterByID[item.ID] = item
	}
	if afterByID["b.md"].ContentHash != beforeByID["b.md"].ContentHash {
		t.Fatalf("unchanged document hash changed: before=%q after=%q", beforeByID["b.md"].ContentHash, afterByID["b.md"].ContentHash)
	}
	if !afterByID["b.md"].SyncedAt.Equal(beforeByID["b.md"].SyncedAt) {
		t.Fatalf("unchanged document synced_at changed: before=%s after=%s", beforeByID["b.md"].SyncedAt, afterByID["b.md"].SyncedAt)
	}
	if afterByID["a.md"].ContentHash == beforeByID["a.md"].ContentHash {
		t.Fatalf("changed document hash did not change: before=%q after=%q", beforeByID["a.md"].ContentHash, afterByID["a.md"].ContentHash)
	}
	if !afterByID["a.md"].SyncedAt.After(beforeByID["a.md"].SyncedAt) {
		t.Fatalf("changed document synced_at did not advance: before=%s after=%s", beforeByID["a.md"].SyncedAt, afterByID["a.md"].SyncedAt)
	}
}

func TestServiceUsesPersistentIndexesAfterRestart(t *testing.T) {
	t.Parallel()

	docRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(docRoot, "en.md"), []byte("Rollback guide for production incidents"), 0o644); err != nil {
		t.Fatalf("write en.md: %v", err)
	}
	if err := os.WriteFile(filepath.Join(docRoot, "zh.md"), []byte("回滚指南与生产故障处理"), 0o644); err != nil {
		t.Fatalf("write zh.md: %v", err)
	}

	dbPath := filepath.Join(t.TempDir(), "knowledge.db")
	store, err := NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatalf("NewSQLiteStore() error = %v", err)
	}
	syncEmbedding := &countingEmbeddingClient{}
	svc, err := NewService(store, syncEmbedding)
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	source, err := svc.UpsertSource(context.Background(), Source{
		Name:    "Docs",
		Kind:    SourceKindLocalDir,
		Enabled: true,
		Path:    docRoot,
	})
	if err != nil {
		t.Fatalf("UpsertSource() error = %v", err)
	}
	if _, err := svc.SyncSource(context.Background(), source.ID); err != nil {
		t.Fatalf("SyncSource() error = %v", err)
	}

	reloadedStore, err := NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatalf("NewSQLiteStore(reload) error = %v", err)
	}
	searchEmbedding := &countingEmbeddingClient{}
	reloadedSvc, err := NewService(reloadedStore, searchEmbedding)
	if err != nil {
		t.Fatalf("NewService(reload) error = %v", err)
	}
	if calls, texts := searchEmbedding.counts(); calls != 0 || texts != 0 {
		t.Fatalf("service startup rebuilt vectors: calls=%d texts=%d", calls, texts)
	}

	results, err := reloadedSvc.Search(context.Background(), SearchFilter{Query: "回滚", Limit: 5})
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if len(results) < 2 {
		t.Fatalf("expected cross-locale results after restart, got %#v", results)
	}
	if results[0].Locale != "zh-CN" {
		t.Fatalf("expected locale-aware ranking to prefer zh-CN, got %#v", results[0])
	}
	foundEnglish := false
	for _, item := range results {
		if item.Path == "en.md" {
			foundEnglish = true
			break
		}
	}
	if !foundEnglish {
		t.Fatalf("expected cross-language retrieval to include English doc, got %#v", results)
	}
	if calls, texts := searchEmbedding.counts(); calls != 1 || texts != 1 {
		t.Fatalf("search embedding counts = (%d,%d), want (1,1)", calls, texts)
	}
}

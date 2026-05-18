package knowledge

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestServiceSyncLocalDirAndSearch(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "runbook.md"), []byte("# Incident Runbook\n\nLatency alerts should page the oncall and link the rollback guide."), 0o644); err != nil {
		t.Fatalf("write runbook: %v", err)
	}

	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "knowledge.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore() error = %v", err)
	}
	svc, err := NewService(store, nil)
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}

	source, err := svc.UpsertSource(context.Background(), Source{
		Name:    "Ops Docs",
		Kind:    SourceKindLocalDir,
		Enabled: true,
		Path:    root,
	})
	if err != nil {
		t.Fatalf("UpsertSource() error = %v", err)
	}
	result, err := svc.SyncSource(context.Background(), source.ID)
	if err != nil {
		t.Fatalf("SyncSource() error = %v", err)
	}
	if result.Stats.Documents != 1 {
		t.Fatalf("Documents = %d, want 1", result.Stats.Documents)
	}
	results, err := svc.Search(context.Background(), SearchFilter{Query: "rollback", Limit: 5})
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if len(results) == 0 {
		t.Fatalf("Search() returned no results")
	}
	if results[0].SourceName != "Ops Docs" {
		t.Fatalf("SourceName = %q, want Ops Docs", results[0].SourceName)
	}
}

func TestServiceSyncWebURLs(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("Customer FAQ: billing cycles, invoice exports, and refund rules."))
	}))
	defer server.Close()

	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "knowledge.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore() error = %v", err)
	}
	svc, err := NewService(store, nil)
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	source, err := svc.UpsertSource(context.Background(), Source{
		Name:    "FAQ",
		Kind:    SourceKindWebURLs,
		Enabled: true,
		URLs:    []string{server.URL},
	})
	if err != nil {
		t.Fatalf("UpsertSource() error = %v", err)
	}
	if _, err := svc.SyncSource(context.Background(), source.ID); err != nil {
		t.Fatalf("SyncSource() error = %v", err)
	}
	results, err := svc.Search(context.Background(), SearchFilter{Query: "invoice", Limit: 5})
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if len(results) == 0 {
		t.Fatalf("Search() returned no results")
	}
	if results[0].URI != server.URL {
		t.Fatalf("URI = %q, want %q", results[0].URI, server.URL)
	}
}

func TestSQLiteStorePersistsSourcesDocumentsAndChunks(t *testing.T) {
	t.Parallel()

	root := filepath.Join(t.TempDir(), "knowledge")
	store, err := NewSQLiteStore(root)
	if err != nil {
		t.Fatalf("NewSQLiteStore() error = %v", err)
	}
	source := Source{
		ID:      "local-docs",
		Name:    "Local Docs",
		Kind:    SourceKindLocalDir,
		Enabled: true,
		Path:    "/tmp/docs",
	}
	if _, err := store.UpsertSource(context.Background(), source); err != nil {
		t.Fatalf("UpsertSource() error = %v", err)
	}
	now := time.Now().UTC()
	document := Document{
		ID:              "doc-1",
		SourceID:        source.ID,
		Kind:            DocumentKindFile,
		Title:           "doc-1",
		Path:            "doc-1.txt",
		URI:             "/tmp/docs/doc-1.txt",
		Locale:          "en",
		ContentHash:     hashKnowledgeText("alpha"),
		Bytes:           int64(len("alpha")),
		ChunkCount:      1,
		SourceUpdatedAt: now,
		SyncedAt:        now,
		Metadata: DocumentMetadata{
			Extension: ".txt",
			MIMEType:  "text/plain; charset=utf-8",
		},
	}
	if err := store.UpsertDocument(context.Background(), document, []Chunk{{
		ID:         "chunk-1",
		SourceID:   source.ID,
		DocumentID: document.ID,
		Ordinal:    0,
		Title:      document.Title,
		Path:       document.Path,
		URI:        document.URI,
		Locale:     document.Locale,
		Content:    "alpha",
		Preview:    "alpha",
		Hash:       hashKnowledgeText("alpha"),
		Bytes:      int64(len("alpha")),
		UpdatedAt:  now,
	}}); err != nil {
		t.Fatalf("UpsertDocument() error = %v", err)
	}

	reloaded, err := NewSQLiteStore(root)
	if err != nil {
		t.Fatalf("NewSQLiteStore(reload) error = %v", err)
	}
	sources, err := reloaded.ListSources(context.Background())
	if err != nil {
		t.Fatalf("ListSources() error = %v", err)
	}
	if len(sources) != 1 || sources[0].ID != source.ID {
		t.Fatalf("sources = %#v", sources)
	}
	documents, err := reloaded.ListDocuments(context.Background(), source.ID)
	if err != nil {
		t.Fatalf("ListDocuments() error = %v", err)
	}
	if len(documents) != 1 || documents[0].ID != document.ID {
		t.Fatalf("documents = %#v", documents)
	}
	chunks, err := reloaded.ListChunks(context.Background(), source.ID)
	if err != nil {
		t.Fatalf("ListChunks() error = %v", err)
	}
	if len(chunks) != 1 || chunks[0].ID != "chunk-1" {
		t.Fatalf("chunks = %#v", chunks)
	}
}

func TestKnowledge_IngestAndSearch(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "runbook.md"), []byte("Rollback instructions for production incidents and paging."), 0o644); err != nil {
		t.Fatalf("write runbook.md: %v", err)
	}

	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "knowledge.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore() error = %v", err)
	}
	svc, err := NewService(store, nil)
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}

	source, err := svc.UpsertSource(context.Background(), Source{
		Name:    "Scenario Docs",
		Kind:    SourceKindLocalDir,
		Enabled: true,
		Path:    root,
	})
	if err != nil {
		t.Fatalf("UpsertSource() error = %v", err)
	}
	result, err := svc.SyncSource(context.Background(), source.ID)
	if err != nil {
		t.Fatalf("SyncSource() error = %v", err)
	}
	if result.Stats.Documents != 1 {
		t.Fatalf("result.Stats.Documents = %d, want 1", result.Stats.Documents)
	}

	results, err := svc.Search(context.Background(), SearchFilter{Query: "rollback", Limit: 5})
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected search results")
	}
	if results[0].SourceName != "Scenario Docs" {
		t.Fatalf("results[0].SourceName = %q, want %q", results[0].SourceName, "Scenario Docs")
	}
}

func TestKnowledge_HybridSearch_KeywordAndSemantic(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "english.md"), []byte("Rollback checklist for production incidents."), 0o644); err != nil {
		t.Fatalf("write english.md: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "chinese.md"), []byte("回滚指南与生产故障处理流程。"), 0o644); err != nil {
		t.Fatalf("write chinese.md: %v", err)
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
		Name:    "Hybrid Docs",
		Kind:    SourceKindLocalDir,
		Enabled: true,
		Path:    root,
	})
	if err != nil {
		t.Fatalf("UpsertSource() error = %v", err)
	}
	if _, err := svc.SyncSource(context.Background(), source.ID); err != nil {
		t.Fatalf("SyncSource() error = %v", err)
	}

	keywordResults, err := store.SearchText(context.Background(), SearchFilter{
		Query: "rollback",
		Limit: 5,
	}, "en")
	if err != nil {
		t.Fatalf("SearchText() error = %v", err)
	}
	semanticResults, err := svc.searchSemantic(context.Background(), SearchFilter{
		Query: "rollback",
		Limit: 5,
	})
	if err != nil {
		t.Fatalf("searchSemantic() error = %v", err)
	}
	hybridResults, err := svc.Search(context.Background(), SearchFilter{Query: "rollback", Limit: 5})
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}

	foundKeywordEnglish := false
	for _, item := range keywordResults {
		if item.Path == "english.md" {
			foundKeywordEnglish = true
		}
	}
	if !foundKeywordEnglish {
		t.Fatalf("keyword results missing english.md: %#v", keywordResults)
	}

	foundSemanticChinese := false
	for _, item := range semanticResults {
		if item.Path == "chinese.md" {
			foundSemanticChinese = true
		}
	}
	if !foundSemanticChinese {
		t.Fatalf("semantic results missing chinese.md: %#v", semanticResults)
	}

	foundHybridEnglish := false
	foundHybridChinese := false
	for _, item := range hybridResults {
		if item.Path == "english.md" {
			foundHybridEnglish = true
		}
		if item.Path == "chinese.md" {
			foundHybridChinese = true
		}
	}
	if !foundHybridEnglish || !foundHybridChinese {
		t.Fatalf("hybrid results = %#v, want both english.md and chinese.md", hybridResults)
	}
}

func TestKnowledge_ChunkOverlap(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	content := strings.Repeat("a", chunkRuneLimit+250)
	if err := os.WriteFile(filepath.Join(root, "long.txt"), []byte(content), 0o644); err != nil {
		t.Fatalf("write long.txt: %v", err)
	}

	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "knowledge.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore() error = %v", err)
	}
	svc, err := NewService(store, nil)
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}

	source, err := svc.UpsertSource(context.Background(), Source{
		Name:    "Chunk Docs",
		Kind:    SourceKindLocalDir,
		Enabled: true,
		Path:    root,
	})
	if err != nil {
		t.Fatalf("UpsertSource() error = %v", err)
	}
	if _, err := svc.SyncSource(context.Background(), source.ID); err != nil {
		t.Fatalf("SyncSource() error = %v", err)
	}

	chunks, err := store.ListChunks(context.Background(), source.ID)
	if err != nil {
		t.Fatalf("ListChunks() error = %v", err)
	}
	if len(chunks) < 2 {
		t.Fatalf("len(chunks) = %d, want at least 2", len(chunks))
	}
	if chunks[0].EndRune != chunkRuneLimit {
		t.Fatalf("chunks[0].EndRune = %d, want %d", chunks[0].EndRune, chunkRuneLimit)
	}
	if chunks[1].StartRune != chunkRuneLimit-chunkOverlapRuneSize {
		t.Fatalf("chunks[1].StartRune = %d, want %d", chunks[1].StartRune, chunkRuneLimit-chunkOverlapRuneSize)
	}
}

func TestKnowledge_SourceCRUD(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "knowledge.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore() error = %v", err)
	}
	svc, err := NewService(store, nil)
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}

	source, err := svc.UpsertSource(context.Background(), Source{
		Name:    "CRUD Docs",
		Kind:    SourceKindLocalDir,
		Enabled: true,
		Path:    root,
	})
	if err != nil {
		t.Fatalf("UpsertSource(create) error = %v", err)
	}

	sources, err := svc.ListSources(context.Background())
	if err != nil {
		t.Fatalf("ListSources() error = %v", err)
	}
	if len(sources) != 1 || sources[0].ID != source.ID {
		t.Fatalf("sources = %#v", sources)
	}

	updated, err := svc.UpsertSource(context.Background(), Source{
		ID:      source.ID,
		Name:    "CRUD Docs Updated",
		Kind:    SourceKindLocalDir,
		Enabled: true,
		Path:    root,
		Locale:  "en",
	})
	if err != nil {
		t.Fatalf("UpsertSource(update) error = %v", err)
	}
	if updated.Name != "CRUD Docs Updated" {
		t.Fatalf("updated.Name = %q, want %q", updated.Name, "CRUD Docs Updated")
	}

	got, err := svc.GetSource(context.Background(), source.ID)
	if err != nil {
		t.Fatalf("GetSource() error = %v", err)
	}
	if got == nil || got.Name != "CRUD Docs Updated" {
		t.Fatalf("got = %#v, want updated source", got)
	}

	if err := svc.DeleteSource(context.Background(), source.ID); err != nil {
		t.Fatalf("DeleteSource() error = %v", err)
	}
	got, err = svc.GetSource(context.Background(), source.ID)
	if err != nil {
		t.Fatalf("GetSource(after delete) error = %v", err)
	}
	if got != nil {
		t.Fatalf("got = %#v, want nil after delete", got)
	}
	sources, err = svc.ListSources(context.Background())
	if err != nil {
		t.Fatalf("ListSources(after delete) error = %v", err)
	}
	if len(sources) != 0 {
		t.Fatalf("len(sources) = %d, want 0", len(sources))
	}
}

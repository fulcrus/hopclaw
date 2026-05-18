package knowledge

import (
	"context"
	"testing"
	"time"
)

func TestSQLiteStoreIndexesRemainProjectionOnly(t *testing.T) {
	t.Parallel()

	store, err := NewSQLiteStore(t.TempDir() + "/knowledge.db")
	if err != nil {
		t.Fatalf("NewSQLiteStore() error = %v", err)
	}
	source := Source{
		ID:        "src-1",
		Name:      "Docs",
		Kind:      SourceKindLocalDir,
		Enabled:   true,
		Locale:    "en",
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}
	if _, err := store.UpsertSource(context.Background(), source); err != nil {
		t.Fatalf("UpsertSource() error = %v", err)
	}
	document := Document{
		ID:              "doc-1",
		SourceID:        source.ID,
		Kind:            DocumentKindFile,
		Title:           "Rollback",
		Path:            "rollback.md",
		URI:             "/tmp/rollback.md",
		Locale:          "en",
		ContentHash:     hashKnowledgeText("rollback guide"),
		Bytes:           int64(len("rollback guide")),
		ChunkCount:      1,
		SourceUpdatedAt: time.Now().UTC(),
		SyncedAt:        time.Now().UTC(),
	}
	chunk := Chunk{
		ID:         "chunk-1",
		SourceID:   source.ID,
		DocumentID: document.ID,
		Ordinal:    0,
		Title:      document.Title,
		Path:       document.Path,
		URI:        document.URI,
		Locale:     document.Locale,
		Content:    "rollback guide",
		Preview:    "rollback guide",
		Hash:       hashKnowledgeText("rollback guide"),
		Bytes:      int64(len("rollback guide")),
		UpdatedAt:  time.Now().UTC(),
	}
	if err := store.UpsertDocument(context.Background(), document, []Chunk{chunk}); err != nil {
		t.Fatalf("UpsertDocument() error = %v", err)
	}
	if err := store.UpsertChunkVectors(context.Background(), []ChunkVector{{
		ChunkID:     chunk.ID,
		SourceID:    chunk.SourceID,
		DocumentID:  chunk.DocumentID,
		Locale:      chunk.Locale,
		ContentHash: chunk.Hash,
		Vector:      []float32{1, 0, 0},
		ProjectedAt: time.Now().UTC(),
	}}); err != nil {
		t.Fatalf("UpsertChunkVectors() error = %v", err)
	}

	if _, err := store.db.Exec(`DELETE FROM knowledge_chunk_fts WHERE chunk_id = ?`, chunk.ID); err != nil {
		t.Fatalf("delete fts projection: %v", err)
	}
	if _, err := store.db.Exec(`DELETE FROM knowledge_chunk_vectors WHERE chunk_id = ?`, chunk.ID); err != nil {
		t.Fatalf("delete vector projection: %v", err)
	}

	documents, err := store.ListDocuments(context.Background(), source.ID)
	if err != nil {
		t.Fatalf("ListDocuments() error = %v", err)
	}
	if len(documents) != 1 || documents[0].ID != document.ID {
		t.Fatalf("documents = %#v", documents)
	}
	chunks, err := store.ListChunks(context.Background(), source.ID)
	if err != nil {
		t.Fatalf("ListChunks() error = %v", err)
	}
	if len(chunks) != 1 || chunks[0].ID != chunk.ID {
		t.Fatalf("chunks = %#v", chunks)
	}
	results, err := store.SearchText(context.Background(), SearchFilter{Query: "rollback", Limit: 5}, "en")
	if err != nil {
		t.Fatalf("SearchText() error = %v", err)
	}
	if len(results) == 0 || results[0].ChunkID != chunk.ID {
		t.Fatalf("expected search to fall back to truth rows, got %#v", results)
	}
}

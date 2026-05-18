package knowledge

import "context"

type Store interface {
	ListSources(ctx context.Context) ([]Source, error)
	GetSource(ctx context.Context, id string) (*Source, error)
	UpsertSource(ctx context.Context, source Source) (Source, error)
	DeleteSource(ctx context.Context, id string) error

	ListDocuments(ctx context.Context, sourceID string) ([]Document, error)
	UpsertDocument(ctx context.Context, document Document, chunks []Chunk) error
	DeleteDocuments(ctx context.Context, sourceID string, documentIDs []string) error
	ComputeSourceStats(ctx context.Context, sourceID string) (SourceStats, error)

	ListChunks(ctx context.Context, sourceID string) ([]Chunk, error)
	ListAllChunks(ctx context.Context) ([]Chunk, error)

	SearchText(ctx context.Context, filter SearchFilter, queryLocale string) ([]SearchResult, error)

	ListChunkVectors(ctx context.Context, sourceID string) ([]ChunkVector, error)
	UpsertChunkVectors(ctx context.Context, vectors []ChunkVector) error
	DeleteChunkVectors(ctx context.Context, chunkIDs []string) error
}

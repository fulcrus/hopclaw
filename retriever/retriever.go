package retriever

import "context"

const (
	defaultMaxResults             = 15
	defaultSegmentResultsPerQuery = 3
)

// HitKind distinguishes where a retrieval hit came from.
type HitKind string

const (
	HitMemory    HitKind = "memory_record"
	HitSegment   HitKind = "recalled_segment"
	HitKnowledge HitKind = "knowledge_chunk"
)

// Hit is the normalized retrieval output consumed by downstream prompt
// builders.
type Hit struct {
	Kind      HitKind
	ID        string
	Score     float64
	Reason    string
	Scope     string
	Content   string
	Citation  string
	Freshness float64
	Authority float64
	Tokens    int
}

// Query is a source-agnostic retrieval request.
type Query struct {
	Text          string
	TargetSummary string
	JobType       string
	Domains       []string
	SessionID     string
	SessionKey    string
	ProjectID     string
	MaxResults    int
}

// EffectiveMaxResults returns the configured retrieval limit, defaulting to
// the phase-specified value when the caller leaves it unset.
func (q Query) EffectiveMaxResults() int {
	if q.MaxResults > 0 {
		return q.MaxResults
	}
	return defaultMaxResults
}

// Retriever exposes a unified retrieval interface.
type Retriever interface {
	Retrieve(ctx context.Context, query Query) ([]Hit, error)
}

// EmbeddingClient embeds free-form queries for semantic retrieval.
type EmbeddingClient interface {
	Embed(ctx context.Context, texts []string) ([][]float32, error)
}

// MemorySearcher retrieves memory-backed hits for the request.
type MemorySearcher interface {
	SearchMemory(ctx context.Context, query Query) ([]Hit, error)
}

// SegmentSearcher retrieves recalled context segments. When queryEmbedding is
// nil, implementations may fall back to lexical retrieval.
type SegmentSearcher interface {
	SearchSegments(ctx context.Context, sessionID string, queryText string, queryEmbedding []float32, limit int) ([]Hit, error)
}

// KnowledgeSearcher retrieves knowledge-backed hits for the request.
type KnowledgeSearcher interface {
	SearchKnowledge(ctx context.Context, query Query) ([]Hit, error)
}

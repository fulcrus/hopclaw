package agent

import "context"

// EmbeddingClient generates vector embeddings from text inputs.
type EmbeddingClient interface {
	Embed(ctx context.Context, texts []string) ([][]float32, error)
}

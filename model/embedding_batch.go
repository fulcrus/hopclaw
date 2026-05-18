package model

import (
	"context"
	"fmt"

	"github.com/fulcrus/hopclaw/agent"
)

// BatchEmbeddingClient wraps an embedding client and automatically splits
// large requests into chunks that respect the model's MaxBatchSize limit.
// Results are reassembled in the original input order.
type BatchEmbeddingClient struct {
	inner agent.EmbeddingClient
	info  EmbeddingModelInfo
}

// NewBatchEmbeddingClient creates a batch-aware embedding client that chunks
// calls to inner according to info.MaxBatchSize.
func NewBatchEmbeddingClient(inner agent.EmbeddingClient, info EmbeddingModelInfo) *BatchEmbeddingClient {
	return &BatchEmbeddingClient{
		inner: inner,
		info:  info,
	}
}

// Embed splits texts into chunks of info.MaxBatchSize, calls inner.Embed per
// chunk, and reassembles results in the original order. The first error
// encountered is propagated immediately.
func (b *BatchEmbeddingClient) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, nil
	}

	batchSize := b.info.MaxBatchSize
	if batchSize <= 0 {
		batchSize = len(texts)
	}

	results := make([][]float32, len(texts))
	for start := 0; start < len(texts); start += batchSize {
		end := start + batchSize
		if end > len(texts) {
			end = len(texts)
		}

		chunk := texts[start:end]
		vectors, err := b.inner.Embed(ctx, chunk)
		if err != nil {
			return nil, fmt.Errorf("batch embedding: chunk [%d:%d] failed: %w", start, end, err)
		}

		if len(vectors) != len(chunk) {
			return nil, fmt.Errorf("batch embedding: expected %d vectors for chunk [%d:%d], got %d", len(chunk), start, end, len(vectors))
		}

		copy(results[start:end], vectors)
	}

	return results, nil
}

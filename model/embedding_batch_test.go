package model

import (
	"context"
	"errors"
	"sync"
	"testing"
)

// mockEmbedClient is a test double for agent.EmbeddingClient.
type mockEmbedClient struct {
	mu     sync.Mutex // guards calls
	calls  [][]string
	result func(texts []string) ([][]float32, error)
}

func (m *mockEmbedClient) Embed(_ context.Context, texts []string) ([][]float32, error) {
	m.mu.Lock()
	m.calls = append(m.calls, texts)
	m.mu.Unlock()
	if m.result != nil {
		return m.result(texts)
	}
	// Default: return one-element vectors with ascending values.
	out := make([][]float32, len(texts))
	for i := range texts {
		out[i] = []float32{float32(i)}
	}
	return out, nil
}

func TestBatchEmbeddingClientChunkSplitting(t *testing.T) {
	t.Parallel()

	const batchSize = 3
	mock := &mockEmbedClient{}
	client := NewBatchEmbeddingClient(mock, EmbeddingModelInfo{
		MaxBatchSize: batchSize,
	})

	// 7 texts should produce 3 chunks: [0:3], [3:6], [6:7]
	texts := []string{"a", "b", "c", "d", "e", "f", "g"}
	vectors, err := client.Embed(context.Background(), texts)
	if err != nil {
		t.Fatalf("embed: %v", err)
	}

	if len(vectors) != len(texts) {
		t.Fatalf("expected %d vectors, got %d", len(texts), len(vectors))
	}

	mock.mu.Lock()
	numCalls := len(mock.calls)
	mock.mu.Unlock()

	expectedChunks := 3 // ceil(7/3) = 3
	if numCalls != expectedChunks {
		t.Fatalf("expected %d inner calls, got %d", expectedChunks, numCalls)
	}

	// Verify chunk sizes.
	mock.mu.Lock()
	if len(mock.calls[0]) != 3 {
		t.Errorf("chunk 0: expected 3 texts, got %d", len(mock.calls[0]))
	}
	if len(mock.calls[1]) != 3 {
		t.Errorf("chunk 1: expected 3 texts, got %d", len(mock.calls[1]))
	}
	if len(mock.calls[2]) != 1 {
		t.Errorf("chunk 2: expected 1 text, got %d", len(mock.calls[2]))
	}
	mock.mu.Unlock()
}

func TestBatchEmbeddingClientReassemblyOrder(t *testing.T) {
	t.Parallel()

	const batchSize = 2
	callIndex := 0
	mock := &mockEmbedClient{
		result: func(texts []string) ([][]float32, error) {
			// Each chunk returns vectors tagged with unique values.
			out := make([][]float32, len(texts))
			for i, text := range texts {
				// Use text length as a unique marker.
				out[i] = []float32{float32(len(text))}
				_ = callIndex // suppress linter
			}
			return out, nil
		},
	}
	client := NewBatchEmbeddingClient(mock, EmbeddingModelInfo{
		MaxBatchSize: batchSize,
	})

	texts := []string{"a", "bb", "ccc", "dddd", "eeeee"}
	vectors, err := client.Embed(context.Background(), texts)
	if err != nil {
		t.Fatalf("embed: %v", err)
	}

	// Verify order matches input.
	for i, text := range texts {
		expected := float32(len(text))
		if vectors[i][0] != expected {
			t.Errorf("vector[%d]: got %f, want %f", i, vectors[i][0], expected)
		}
	}
}

func TestBatchEmbeddingClientErrorPropagation(t *testing.T) {
	t.Parallel()

	errTest := errors.New("test embedding error")
	callCount := 0
	mock := &mockEmbedClient{
		result: func(texts []string) ([][]float32, error) {
			callCount++
			if callCount == 2 {
				return nil, errTest
			}
			out := make([][]float32, len(texts))
			for i := range texts {
				out[i] = []float32{0.1}
			}
			return out, nil
		},
	}
	client := NewBatchEmbeddingClient(mock, EmbeddingModelInfo{
		MaxBatchSize: 2,
	})

	// 5 texts, batch 2 => 3 chunks, second chunk will fail.
	_, err := client.Embed(context.Background(), []string{"a", "b", "c", "d", "e"})
	if err == nil {
		t.Fatal("expected error from inner client")
	}
	if !errors.Is(err, errTest) {
		t.Fatalf("expected wrapped test error, got: %v", err)
	}
}

func TestBatchEmbeddingClientEmptyInput(t *testing.T) {
	t.Parallel()

	mock := &mockEmbedClient{}
	client := NewBatchEmbeddingClient(mock, EmbeddingModelInfo{
		MaxBatchSize: 10,
	})

	vectors, err := client.Embed(context.Background(), nil)
	if err != nil {
		t.Fatalf("expected nil error for empty input, got %v", err)
	}
	if vectors != nil {
		t.Fatalf("expected nil vectors for empty input, got %v", vectors)
	}

	mock.mu.Lock()
	numCalls := len(mock.calls)
	mock.mu.Unlock()

	if numCalls != 0 {
		t.Fatalf("expected 0 inner calls for empty input, got %d", numCalls)
	}
}

func TestBatchEmbeddingClientSingleChunk(t *testing.T) {
	t.Parallel()

	mock := &mockEmbedClient{}
	client := NewBatchEmbeddingClient(mock, EmbeddingModelInfo{
		MaxBatchSize: 100,
	})

	texts := []string{"a", "b", "c"}
	vectors, err := client.Embed(context.Background(), texts)
	if err != nil {
		t.Fatalf("embed: %v", err)
	}
	if len(vectors) != 3 {
		t.Fatalf("expected 3 vectors, got %d", len(vectors))
	}

	mock.mu.Lock()
	numCalls := len(mock.calls)
	mock.mu.Unlock()

	if numCalls != 1 {
		t.Fatalf("expected 1 inner call when all fit in one batch, got %d", numCalls)
	}
}

func TestBatchEmbeddingClientZeroBatchSize(t *testing.T) {
	t.Parallel()

	mock := &mockEmbedClient{}
	// MaxBatchSize 0 should default to sending all texts in one call.
	client := NewBatchEmbeddingClient(mock, EmbeddingModelInfo{
		MaxBatchSize: 0,
	})

	texts := []string{"a", "b", "c", "d"}
	vectors, err := client.Embed(context.Background(), texts)
	if err != nil {
		t.Fatalf("embed: %v", err)
	}
	if len(vectors) != 4 {
		t.Fatalf("expected 4 vectors, got %d", len(vectors))
	}

	mock.mu.Lock()
	numCalls := len(mock.calls)
	mock.mu.Unlock()

	if numCalls != 1 {
		t.Fatalf("expected 1 inner call for zero batch size, got %d", numCalls)
	}
}

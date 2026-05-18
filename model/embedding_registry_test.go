package model

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/fulcrus/hopclaw/agent"
)

// staticEmbedClient returns a fixed set of vectors for any input.
type staticEmbedClient struct {
	vectors [][]float32
	err     error
}

func (s *staticEmbedClient) Embed(_ context.Context, texts []string) ([][]float32, error) {
	if s.err != nil {
		return nil, s.err
	}
	out := make([][]float32, len(texts))
	for i := range texts {
		if i < len(s.vectors) {
			out[i] = s.vectors[i]
		} else {
			out[i] = []float32{0.0}
		}
	}
	return out, nil
}

func TestEmbeddingRegistryDefaultSelection(t *testing.T) {
	t.Parallel()

	registry := NewEmbeddingRegistry()
	registry.Register("alpha", &staticEmbedClient{
		vectors: [][]float32{{1.0}},
	})
	registry.Register("beta", &staticEmbedClient{
		vectors: [][]float32{{2.0}},
	})
	registry.SetDefault("alpha")

	vectors, err := registry.Embed(context.Background(), []string{"test"})
	if err != nil {
		t.Fatalf("embed: %v", err)
	}
	if vectors[0][0] != 1.0 {
		t.Fatalf("expected vector from alpha (1.0), got %f", vectors[0][0])
	}
}

func TestEmbeddingRegistryFallbackOnError(t *testing.T) {
	t.Parallel()

	registry := NewEmbeddingRegistry()
	registry.Register("primary", &staticEmbedClient{
		err: errors.New("primary down"),
	})
	registry.Register("secondary", &staticEmbedClient{
		vectors: [][]float32{{9.9}},
	})
	registry.SetDefault("primary")
	registry.SetFallback("secondary")

	vectors, err := registry.Embed(context.Background(), []string{"test"})
	if err != nil {
		t.Fatalf("expected fallback to succeed, got: %v", err)
	}
	if vectors[0][0] != 9.9 {
		t.Fatalf("expected vector from secondary (9.9), got %f", vectors[0][0])
	}
}

func TestEmbeddingRegistryNoFallback(t *testing.T) {
	t.Parallel()

	registry := NewEmbeddingRegistry()
	registry.Register("primary", &staticEmbedClient{
		err: errors.New("primary down"),
	})
	registry.SetDefault("primary")

	_, err := registry.Embed(context.Background(), []string{"test"})
	if err == nil {
		t.Fatal("expected error when default fails and no fallback is set")
	}
}

func TestEmbeddingRegistryFallbackSameName(t *testing.T) {
	t.Parallel()

	registry := NewEmbeddingRegistry()
	registry.Register("only", &staticEmbedClient{
		err: errors.New("only client down"),
	})
	registry.SetDefault("only")
	registry.SetFallback("only")

	_, err := registry.Embed(context.Background(), []string{"test"})
	if err == nil {
		t.Fatal("expected error when fallback is same as default and default fails")
	}
}

func TestEmbeddingRegistryNoDefault(t *testing.T) {
	t.Parallel()

	registry := NewEmbeddingRegistry()
	registry.Register("alpha", &staticEmbedClient{
		vectors: [][]float32{{1.0}},
	})
	// No SetDefault called.

	_, err := registry.Embed(context.Background(), []string{"test"})
	if err == nil {
		t.Fatal("expected error when no default is set")
	}
}

func TestEmbeddingRegistryEmptyInput(t *testing.T) {
	t.Parallel()

	registry := NewEmbeddingRegistry()
	registry.Register("default", &staticEmbedClient{
		vectors: [][]float32{{1.0}},
	})
	registry.SetDefault("default")

	vectors, err := registry.Embed(context.Background(), nil)
	if err != nil {
		t.Fatalf("expected nil error for empty input, got %v", err)
	}
	if vectors != nil {
		t.Fatalf("expected nil vectors for empty input, got %v", vectors)
	}
}

func TestEmbeddingRegistryGet(t *testing.T) {
	t.Parallel()

	registry := NewEmbeddingRegistry()

	alpha := &staticEmbedClient{vectors: [][]float32{{1.0}}}
	registry.Register("alpha", alpha)

	got := registry.Get("alpha")
	if got != alpha {
		t.Fatal("Get should return the registered client")
	}

	if got := registry.Get("nonexistent"); got != nil {
		t.Fatalf("Get should return nil for unknown name, got %v", got)
	}
}

func TestEmbeddingRegistryRegisterReplace(t *testing.T) {
	t.Parallel()

	registry := NewEmbeddingRegistry()

	old := &staticEmbedClient{vectors: [][]float32{{1.0}}}
	registry.Register("alpha", old)

	replacement := &staticEmbedClient{vectors: [][]float32{{2.0}}}
	registry.Register("alpha", replacement)

	got := registry.Get("alpha")
	if got != replacement {
		t.Fatal("Register should replace existing client with same name")
	}
}

func TestEmbeddingRegistryConcurrentAccess(t *testing.T) {
	t.Parallel()

	registry := NewEmbeddingRegistry()
	registry.Register("default", &staticEmbedClient{
		vectors: [][]float32{{1.0}},
	})
	registry.SetDefault("default")

	const goroutines = 50
	var wg sync.WaitGroup
	errs := make(chan error, goroutines)

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()

			// Mix of reads and writes.
			if idx%5 == 0 {
				name := "dynamic"
				registry.Register(name, &staticEmbedClient{
					vectors: [][]float32{{float32(idx)}},
				})
			}

			vectors, err := registry.Embed(context.Background(), []string{"test"})
			if err != nil {
				errs <- err
				return
			}
			if len(vectors) != 1 {
				errs <- errors.New("unexpected vector count")
			}
		}(i)
	}

	wg.Wait()
	close(errs)

	for err := range errs {
		t.Errorf("concurrent error: %v", err)
	}
}

// Verify EmbeddingRegistry implements agent.EmbeddingClient at compile time.
var _ agent.EmbeddingClient = (*EmbeddingRegistry)(nil)

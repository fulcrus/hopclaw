package model

import (
	"context"
	"fmt"
	"sync"

	"github.com/fulcrus/hopclaw/agent"
)

// EmbeddingRegistry is a thread-safe multi-provider embedding registry that
// implements agent.EmbeddingClient. It delegates to a default client and
// falls back to a secondary client on error.
type EmbeddingRegistry struct {
	mu          sync.RWMutex // guards clients, defaultName, fallback
	clients     map[string]agent.EmbeddingClient
	defaultName string
	fallback    string
}

// NewEmbeddingRegistry creates an empty embedding registry.
func NewEmbeddingRegistry() *EmbeddingRegistry {
	return &EmbeddingRegistry{
		clients: make(map[string]agent.EmbeddingClient),
	}
}

// Register adds a named embedding client to the registry. If a client with
// the same name already exists it is replaced.
func (r *EmbeddingRegistry) Register(name string, client agent.EmbeddingClient) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.clients[name] = client
}

// SetDefault sets the name of the default embedding client.
func (r *EmbeddingRegistry) SetDefault(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.defaultName = name
}

// SetFallback sets the name of the fallback embedding client used when the
// default client returns an error.
func (r *EmbeddingRegistry) SetFallback(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.fallback = name
}

// Get returns a named embedding client, or nil if not found.
func (r *EmbeddingRegistry) Get(name string) agent.EmbeddingClient {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.clients[name]
}

// Embed delegates to the default client. If the default client returns an
// error and a fallback is configured, the fallback client is tried.
func (r *EmbeddingRegistry) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, nil
	}

	r.mu.RLock()
	defaultClient := r.clients[r.defaultName]
	fallbackName := r.fallback
	fallbackClient := r.clients[fallbackName]
	r.mu.RUnlock()

	if defaultClient == nil {
		return nil, fmt.Errorf("embedding registry: no default client %q registered", r.defaultName)
	}

	vectors, err := defaultClient.Embed(ctx, texts)
	if err == nil {
		return vectors, nil
	}

	if fallbackClient == nil || fallbackName == r.defaultName {
		return nil, err
	}

	return fallbackClient.Embed(ctx, texts)
}

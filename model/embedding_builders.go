package model

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/fulcrus/hopclaw/agent"
)

type EmbeddingClientBuildInput struct {
	BaseURL string
	APIKey  string
	Model   string
	Timeout time.Duration
}

type EmbeddingClientBuilder func(EmbeddingClientBuildInput) (agent.EmbeddingClient, error)

var (
	embeddingClientBuildersMu sync.RWMutex
	embeddingClientBuilders   = map[EmbeddingProviderAPI]EmbeddingClientBuilder{}
)

func RegisterEmbeddingClientBuilder(api EmbeddingProviderAPI, build EmbeddingClientBuilder) {
	if strings.TrimSpace(string(api)) == "" || build == nil {
		return
	}
	embeddingClientBuildersMu.Lock()
	embeddingClientBuilders[api] = build
	embeddingClientBuildersMu.Unlock()
}

func embeddingClientBuilder(api EmbeddingProviderAPI) (EmbeddingClientBuilder, bool) {
	embeddingClientBuildersMu.RLock()
	build, ok := embeddingClientBuilders[api]
	embeddingClientBuildersMu.RUnlock()
	return build, ok
}

func NewEmbeddingClientForProvider(api EmbeddingProviderAPI, input EmbeddingClientBuildInput) (agent.EmbeddingClient, error) {
	if strings.TrimSpace(string(api)) == "" {
		api = EmbedOpenAI
	}
	build, ok := embeddingClientBuilder(api)
	if !ok {
		build, ok = embeddingClientBuilder(EmbedOpenAI)
	}
	if !ok {
		return nil, fmt.Errorf("embedding provider %q is not registered", api)
	}
	return build(input)
}

package model

import "github.com/fulcrus/hopclaw/agent"

const (
	defaultMistralEmbeddingBaseURL = "https://api.mistral.ai"
	defaultMistralEmbeddingModel   = "mistral-embed"
)

// MistralEmbeddingConfig configures a Mistral embedding client.
type MistralEmbeddingConfig struct {
	BaseURL string
	APIKey  string
	Model   string // default: "mistral-embed"
}

func init() {
	RegisterEmbeddingClientBuilder(EmbedMistral, func(input EmbeddingClientBuildInput) (agent.EmbeddingClient, error) {
		return NewMistralEmbeddingClient(MistralEmbeddingConfig{
			BaseURL: input.BaseURL,
			APIKey:  input.APIKey,
			Model:   input.Model,
		}), nil
	})
}

// NewMistralEmbeddingClient creates a Mistral embedding client.
// Mistral uses the same wire format as OpenAI (/v1/embeddings), so this
// delegates to the existing EmbeddingClient with the Mistral default base URL.
func NewMistralEmbeddingClient(cfg MistralEmbeddingConfig) *EmbeddingClient {
	baseURL := cfg.BaseURL
	if baseURL == "" {
		baseURL = defaultMistralEmbeddingBaseURL
	}
	model := cfg.Model
	if model == "" {
		model = defaultMistralEmbeddingModel
	}
	return NewEmbeddingClient(EmbeddingConfig{
		BaseURL: baseURL,
		APIKey:  cfg.APIKey,
		Model:   model,
	})
}

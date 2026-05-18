package model

import "github.com/fulcrus/hopclaw/agent"

const (
	defaultVoyageEmbeddingBaseURL = "https://api.voyageai.com"
	defaultVoyageEmbeddingModel   = "voyage-3"
)

// VoyageEmbeddingConfig configures a Voyage AI embedding client.
type VoyageEmbeddingConfig struct {
	BaseURL string
	APIKey  string
	Model   string // default: "voyage-3"
}

func init() {
	RegisterEmbeddingClientBuilder(EmbedVoyage, func(input EmbeddingClientBuildInput) (agent.EmbeddingClient, error) {
		return NewVoyageEmbeddingClient(VoyageEmbeddingConfig{
			BaseURL: input.BaseURL,
			APIKey:  input.APIKey,
			Model:   input.Model,
		}), nil
	})
}

// NewVoyageEmbeddingClient creates a Voyage AI embedding client.
// Voyage uses the same wire format as OpenAI (/v1/embeddings), so this
// delegates to the existing EmbeddingClient with the Voyage default base URL.
func NewVoyageEmbeddingClient(cfg VoyageEmbeddingConfig) *EmbeddingClient {
	baseURL := cfg.BaseURL
	if baseURL == "" {
		baseURL = defaultVoyageEmbeddingBaseURL
	}
	model := cfg.Model
	if model == "" {
		model = defaultVoyageEmbeddingModel
	}
	return NewEmbeddingClient(EmbeddingConfig{
		BaseURL: baseURL,
		APIKey:  cfg.APIKey,
		Model:   model,
	})
}

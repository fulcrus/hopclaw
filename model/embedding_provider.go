package model

// EmbeddingProviderAPI identifies the wire protocol used by an embedding provider.
type EmbeddingProviderAPI string

const (
	EmbedOpenAI  EmbeddingProviderAPI = "openai"
	EmbedGemini  EmbeddingProviderAPI = "gemini"
	EmbedVoyage  EmbeddingProviderAPI = "voyage"
	EmbedOllama  EmbeddingProviderAPI = "ollama"
	EmbedMistral EmbeddingProviderAPI = "mistral"
	EmbedBedrock EmbeddingProviderAPI = "bedrock"
)

// EmbeddingModelInfo describes the capabilities and constraints of a known
// embedding model.
type EmbeddingModelInfo struct {
	Provider     EmbeddingProviderAPI
	Model        string
	Dimensions   int
	MaxTokens    int // per-text token limit
	MaxBatchSize int // texts per API call
}

// embeddingModelCatalog maps known model names to their info.
var embeddingModelCatalog = map[string]EmbeddingModelInfo{
	"text-embedding-3-small":       {Provider: EmbedOpenAI, Model: "text-embedding-3-small", Dimensions: 1536, MaxTokens: 8191, MaxBatchSize: 2048},
	"text-embedding-3-large":       {Provider: EmbedOpenAI, Model: "text-embedding-3-large", Dimensions: 3072, MaxTokens: 8191, MaxBatchSize: 2048},
	"text-embedding-004":           {Provider: EmbedGemini, Model: "text-embedding-004", Dimensions: 768, MaxTokens: 2048, MaxBatchSize: 100},
	"voyage-3":                     {Provider: EmbedVoyage, Model: "voyage-3", Dimensions: 1024, MaxTokens: 32000, MaxBatchSize: 128},
	"nomic-embed-text":             {Provider: EmbedOllama, Model: "nomic-embed-text", Dimensions: 768, MaxTokens: 8192, MaxBatchSize: 1},
	"mistral-embed":                {Provider: EmbedMistral, Model: "mistral-embed", Dimensions: 1024, MaxTokens: 16384, MaxBatchSize: 16},
	"amazon.titan-embed-text-v2:0": {Provider: EmbedBedrock, Model: "amazon.titan-embed-text-v2:0", Dimensions: 1024, MaxTokens: 8192, MaxBatchSize: 1},
	"cohere.embed-english-v3":      {Provider: EmbedBedrock, Model: "cohere.embed-english-v3", Dimensions: 1024, MaxTokens: 512, MaxBatchSize: 1},
}

// LookupEmbeddingModel returns the model info for a known model name.
func LookupEmbeddingModel(name string) (EmbeddingModelInfo, bool) {
	info, ok := embeddingModelCatalog[name]
	return info, ok
}

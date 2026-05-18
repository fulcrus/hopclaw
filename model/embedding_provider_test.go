package model

import "testing"

func TestLookupEmbeddingModelKnown(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		wantDim   int
		wantProv  EmbeddingProviderAPI
		wantBatch int
	}{
		{name: "text-embedding-3-small", wantDim: 1536, wantProv: EmbedOpenAI, wantBatch: 2048},
		{name: "text-embedding-3-large", wantDim: 3072, wantProv: EmbedOpenAI, wantBatch: 2048},
		{name: "text-embedding-004", wantDim: 768, wantProv: EmbedGemini, wantBatch: 100},
		{name: "voyage-3", wantDim: 1024, wantProv: EmbedVoyage, wantBatch: 128},
		{name: "nomic-embed-text", wantDim: 768, wantProv: EmbedOllama, wantBatch: 1},
		{name: "mistral-embed", wantDim: 1024, wantProv: EmbedMistral, wantBatch: 16},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			info, ok := LookupEmbeddingModel(tc.name)
			if !ok {
				t.Fatalf("expected model %q to be found", tc.name)
			}
			if info.Dimensions != tc.wantDim {
				t.Errorf("dimensions: got %d, want %d", info.Dimensions, tc.wantDim)
			}
			if info.Provider != tc.wantProv {
				t.Errorf("provider: got %q, want %q", info.Provider, tc.wantProv)
			}
			if info.MaxBatchSize != tc.wantBatch {
				t.Errorf("max batch size: got %d, want %d", info.MaxBatchSize, tc.wantBatch)
			}
			if info.Model != tc.name {
				t.Errorf("model: got %q, want %q", info.Model, tc.name)
			}
		})
	}
}

func TestLookupEmbeddingModelUnknown(t *testing.T) {
	t.Parallel()

	_, ok := LookupEmbeddingModel("nonexistent-model-xyz")
	if ok {
		t.Fatal("expected unknown model to return false")
	}
}

func TestLookupEmbeddingModelEmptyName(t *testing.T) {
	t.Parallel()

	_, ok := LookupEmbeddingModel("")
	if ok {
		t.Fatal("expected empty name to return false")
	}
}

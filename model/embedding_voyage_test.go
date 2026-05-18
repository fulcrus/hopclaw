package model

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestVoyageEmbeddingClientEmbed(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/embeddings" {
			t.Errorf("unexpected path: %s", r.URL.Path)
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		if r.Method != http.MethodPost {
			t.Errorf("unexpected method: %s", r.Method)
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if auth := r.Header.Get("Authorization"); auth != "Bearer voyage-test-key" {
			t.Errorf("unexpected authorization: %s", auth)
		}

		var req embeddingRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if req.Model != "voyage-3" {
			t.Errorf("unexpected model: %s", req.Model)
		}

		resp := embeddingResponse{
			Data: []embeddingData{
				{Embedding: []float32{0.7, 0.8}, Index: 0},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewVoyageEmbeddingClient(VoyageEmbeddingConfig{
		BaseURL: server.URL,
		APIKey:  "voyage-test-key",
	})

	vectors, err := client.Embed(context.Background(), []string{"hello"})
	if err != nil {
		t.Fatalf("embed: %v", err)
	}
	if len(vectors) != 1 {
		t.Fatalf("expected 1 vector, got %d", len(vectors))
	}
	if vectors[0][0] != 0.7 {
		t.Fatalf("unexpected vector: %v", vectors[0])
	}
}

func TestVoyageEmbeddingClientDefaults(t *testing.T) {
	t.Parallel()

	client := NewVoyageEmbeddingClient(VoyageEmbeddingConfig{
		APIKey: "key",
	})

	if client.config.BaseURL != defaultVoyageEmbeddingBaseURL {
		t.Errorf("base URL: got %q, want %q", client.config.BaseURL, defaultVoyageEmbeddingBaseURL)
	}
	if client.config.Model != defaultVoyageEmbeddingModel {
		t.Errorf("model: got %q, want %q", client.config.Model, defaultVoyageEmbeddingModel)
	}
}

func TestVoyageEmbeddingClientCustomModel(t *testing.T) {
	t.Parallel()

	client := NewVoyageEmbeddingClient(VoyageEmbeddingConfig{
		APIKey: "key",
		Model:  "voyage-code-3",
	})

	if client.config.Model != "voyage-code-3" {
		t.Errorf("model: got %q, want %q", client.config.Model, "voyage-code-3")
	}
}

package model

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestMistralEmbeddingClientEmbed(t *testing.T) {
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
		if auth := r.Header.Get("Authorization"); auth != "Bearer mistral-test-key" {
			t.Errorf("unexpected authorization: %s", auth)
		}

		var req embeddingRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if req.Model != "mistral-embed" {
			t.Errorf("unexpected model: %s", req.Model)
		}

		resp := embeddingResponse{
			Data: []embeddingData{
				{Embedding: []float32{0.3, 0.4}, Index: 0},
				{Embedding: []float32{0.5, 0.6}, Index: 1},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewMistralEmbeddingClient(MistralEmbeddingConfig{
		BaseURL: server.URL,
		APIKey:  "mistral-test-key",
	})

	vectors, err := client.Embed(context.Background(), []string{"hello", "world"})
	if err != nil {
		t.Fatalf("embed: %v", err)
	}
	if len(vectors) != 2 {
		t.Fatalf("expected 2 vectors, got %d", len(vectors))
	}
	if vectors[0][0] != 0.3 || vectors[1][0] != 0.5 {
		t.Fatalf("unexpected vectors: %v", vectors)
	}
}

func TestMistralEmbeddingClientDefaults(t *testing.T) {
	t.Parallel()

	client := NewMistralEmbeddingClient(MistralEmbeddingConfig{
		APIKey: "key",
	})

	if client.config.BaseURL != defaultMistralEmbeddingBaseURL {
		t.Errorf("base URL: got %q, want %q", client.config.BaseURL, defaultMistralEmbeddingBaseURL)
	}
	if client.config.Model != defaultMistralEmbeddingModel {
		t.Errorf("model: got %q, want %q", client.config.Model, defaultMistralEmbeddingModel)
	}
}

func TestMistralEmbeddingClientCustomModel(t *testing.T) {
	t.Parallel()

	client := NewMistralEmbeddingClient(MistralEmbeddingConfig{
		APIKey: "key",
		Model:  "mistral-embed-v2",
	})

	if client.config.Model != "mistral-embed-v2" {
		t.Errorf("model: got %q, want %q", client.config.Model, "mistral-embed-v2")
	}
}

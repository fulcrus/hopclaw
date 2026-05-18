package model

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestEmbeddingClientEmbed(t *testing.T) {
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
		if ct := r.Header.Get("Content-Type"); ct != "application/json" {
			t.Errorf("unexpected content-type: %s", ct)
		}
		if auth := r.Header.Get("Authorization"); auth != "Bearer test-key" {
			t.Errorf("unexpected authorization: %s", auth)
		}

		var req embeddingRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if req.Model != "text-embedding-3-small" {
			t.Errorf("unexpected model: %s", req.Model)
		}
		if len(req.Input) != 2 {
			t.Errorf("expected 2 inputs, got %d", len(req.Input))
		}

		resp := embeddingResponse{
			Data: []embeddingData{
				{Embedding: []float32{0.1, 0.2, 0.3}, Index: 0},
				{Embedding: []float32{0.4, 0.5, 0.6}, Index: 1},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewEmbeddingClient(EmbeddingConfig{
		BaseURL: server.URL,
		APIKey:  "test-key",
	})

	vectors, err := client.Embed(context.Background(), []string{"hello", "world"})
	if err != nil {
		t.Fatalf("embed: %v", err)
	}
	if len(vectors) != 2 {
		t.Fatalf("expected 2 vectors, got %d", len(vectors))
	}
	if vectors[0][0] != 0.1 || vectors[1][0] != 0.4 {
		t.Fatalf("unexpected vectors: %v", vectors)
	}
}

func TestEmbeddingClientEmptyInput(t *testing.T) {
	t.Parallel()

	client := NewEmbeddingClient(EmbeddingConfig{
		BaseURL: "http://localhost:0",
	})

	vectors, err := client.Embed(context.Background(), nil)
	if err != nil {
		t.Fatalf("expected nil error for empty input, got %v", err)
	}
	if vectors != nil {
		t.Fatalf("expected nil vectors for empty input, got %v", vectors)
	}
}

func TestEmbeddingClientAPIError(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error": "rate limit"}`, http.StatusTooManyRequests)
	}))
	defer server.Close()

	client := NewEmbeddingClient(EmbeddingConfig{
		BaseURL: server.URL,
	})

	_, err := client.Embed(context.Background(), []string{"test"})
	if err == nil {
		t.Fatal("expected error for API error response")
	}
}

func TestEmbeddingClientOutOfOrderIndex(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := embeddingResponse{
			Data: []embeddingData{
				{Embedding: []float32{0.4, 0.5}, Index: 1},
				{Embedding: []float32{0.1, 0.2}, Index: 0},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewEmbeddingClient(EmbeddingConfig{
		BaseURL: server.URL,
	})

	vectors, err := client.Embed(context.Background(), []string{"a", "b"})
	if err != nil {
		t.Fatalf("embed: %v", err)
	}
	// Index 0 should have [0.1, 0.2], index 1 should have [0.4, 0.5].
	if vectors[0][0] != 0.1 {
		t.Fatalf("expected first vector [0.1, ...], got %v", vectors[0])
	}
	if vectors[1][0] != 0.4 {
		t.Fatalf("expected second vector [0.4, ...], got %v", vectors[1])
	}
}

func TestEmbeddingClientCountMismatch(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := embeddingResponse{
			Data: []embeddingData{
				{Embedding: []float32{0.1, 0.2}, Index: 0},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewEmbeddingClient(EmbeddingConfig{
		BaseURL: server.URL,
	})

	_, err := client.Embed(context.Background(), []string{"a", "b"})
	if err == nil {
		t.Fatal("expected error for count mismatch")
	}
}

func TestNewEmbeddingClientDefaults(t *testing.T) {
	t.Parallel()

	client := NewEmbeddingClient(EmbeddingConfig{
		BaseURL: "http://example.com",
	})

	if client.config.Model != defaultEmbeddingModel {
		t.Fatalf("expected default model %q, got %q", defaultEmbeddingModel, client.config.Model)
	}
}

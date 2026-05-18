package model

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestOllamaEmbeddingClientEmbed(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/embed" {
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
		// Ollama should not require auth by default.
		if auth := r.Header.Get("Authorization"); auth != "" {
			t.Errorf("unexpected authorization header: %s", auth)
		}

		var req ollamaEmbedRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if req.Model != "nomic-embed-text" {
			t.Errorf("unexpected model: %s", req.Model)
		}
		if len(req.Input) != 2 {
			t.Errorf("expected 2 inputs, got %d", len(req.Input))
		}

		resp := ollamaEmbedResponse{
			Embeddings: [][]float32{
				{0.1, 0.2, 0.3},
				{0.4, 0.5, 0.6},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewOllamaEmbeddingClient(OllamaEmbeddingConfig{
		BaseURL: server.URL,
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

func TestOllamaEmbeddingClientEmptyInput(t *testing.T) {
	t.Parallel()

	client := NewOllamaEmbeddingClient(OllamaEmbeddingConfig{
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

func TestOllamaEmbeddingClientAPIError(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error": "model not found"}`, http.StatusNotFound)
	}))
	defer server.Close()

	client := NewOllamaEmbeddingClient(OllamaEmbeddingConfig{
		BaseURL: server.URL,
	})

	_, err := client.Embed(context.Background(), []string{"test"})
	if err == nil {
		t.Fatal("expected error for API error response")
	}
	if !strings.Contains(err.Error(), "status 404") {
		t.Fatalf("expected status 404 in error, got: %v", err)
	}
}

func TestOllamaEmbeddingClientWithAuth(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if auth := r.Header.Get("Authorization"); auth != "Bearer ollama-key" {
			t.Errorf("expected authorization header, got: %s", auth)
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}

		resp := ollamaEmbedResponse{
			Embeddings: [][]float32{{0.9}},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewOllamaEmbeddingClient(OllamaEmbeddingConfig{
		BaseURL: server.URL,
		APIKey:  "ollama-key",
	})

	vectors, err := client.Embed(context.Background(), []string{"test"})
	if err != nil {
		t.Fatalf("embed: %v", err)
	}
	if len(vectors) != 1 {
		t.Fatalf("expected 1 vector, got %d", len(vectors))
	}
}

func TestOllamaEmbeddingClientDefaults(t *testing.T) {
	t.Parallel()

	client := NewOllamaEmbeddingClient(OllamaEmbeddingConfig{})

	if client.config.BaseURL != defaultOllamaEmbeddingBaseURL {
		t.Errorf("base URL: got %q, want %q", client.config.BaseURL, defaultOllamaEmbeddingBaseURL)
	}
	if client.config.Model != defaultOllamaEmbeddingModel {
		t.Errorf("model: got %q, want %q", client.config.Model, defaultOllamaEmbeddingModel)
	}
}

func TestOllamaEmbeddingClientCountMismatch(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := ollamaEmbedResponse{
			Embeddings: [][]float32{{0.1}},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewOllamaEmbeddingClient(OllamaEmbeddingConfig{
		BaseURL: server.URL,
	})

	_, err := client.Embed(context.Background(), []string{"a", "b"})
	if err == nil {
		t.Fatal("expected error for count mismatch")
	}
}

package model

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestGeminiEmbeddingClientEmbed(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("unexpected method: %s", r.Method)
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		expectedPath := "/v1beta/models/text-embedding-004:batchEmbedContents"
		if r.URL.Path != expectedPath {
			t.Errorf("unexpected path: got %s, want %s", r.URL.Path, expectedPath)
			http.Error(w, "not found", http.StatusNotFound)
			return
		}

		if key := r.URL.Query().Get("key"); key != "test-gemini-key" {
			t.Errorf("unexpected API key: %s", key)
		}

		if ct := r.Header.Get("Content-Type"); ct != "application/json" {
			t.Errorf("unexpected content-type: %s", ct)
		}

		var req geminiBatchEmbedRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if len(req.Requests) != 2 {
			t.Errorf("expected 2 requests, got %d", len(req.Requests))
		}
		for _, r := range req.Requests {
			if r.Model != "models/text-embedding-004" {
				t.Errorf("unexpected model ref: %s", r.Model)
			}
		}

		resp := geminiBatchEmbedResponse{
			Embeddings: []geminiEmbeddingValues{
				{Values: []float32{0.1, 0.2, 0.3}},
				{Values: []float32{0.4, 0.5, 0.6}},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewGeminiEmbeddingClient(GeminiEmbeddingConfig{
		BaseURL: server.URL,
		APIKey:  "test-gemini-key",
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

func TestGeminiEmbeddingClientEmptyInput(t *testing.T) {
	t.Parallel()

	client := NewGeminiEmbeddingClient(GeminiEmbeddingConfig{
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

func TestGeminiEmbeddingClientAPIError(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error": {"message": "quota exceeded"}}`, http.StatusTooManyRequests)
	}))
	defer server.Close()

	client := NewGeminiEmbeddingClient(GeminiEmbeddingConfig{
		BaseURL: server.URL,
		APIKey:  "test-key",
	})

	_, err := client.Embed(context.Background(), []string{"test"})
	if err == nil {
		t.Fatal("expected error for API error response")
	}
	if !strings.Contains(err.Error(), "status 429") {
		t.Fatalf("expected status 429 in error, got: %v", err)
	}
}

func TestGeminiEmbeddingClientCountMismatch(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := geminiBatchEmbedResponse{
			Embeddings: []geminiEmbeddingValues{
				{Values: []float32{0.1, 0.2}},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewGeminiEmbeddingClient(GeminiEmbeddingConfig{
		BaseURL: server.URL,
		APIKey:  "test-key",
	})

	_, err := client.Embed(context.Background(), []string{"a", "b"})
	if err == nil {
		t.Fatal("expected error for count mismatch")
	}
}

func TestGeminiEmbeddingClientDefaults(t *testing.T) {
	t.Parallel()

	client := NewGeminiEmbeddingClient(GeminiEmbeddingConfig{})

	if client.config.BaseURL != defaultGeminiEmbeddingBaseURL {
		t.Errorf("base URL: got %q, want %q", client.config.BaseURL, defaultGeminiEmbeddingBaseURL)
	}
	if client.config.Model != defaultGeminiEmbeddingModel {
		t.Errorf("model: got %q, want %q", client.config.Model, defaultGeminiEmbeddingModel)
	}
}

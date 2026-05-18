package model

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestDiscoverModelsOpenAI(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Fatalf("expected GET, got %s", r.Method)
		}
		if r.URL.Path != "/v1/models" {
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Fatalf("unexpected Authorization header %q", r.Header.Get("Authorization"))
		}

		resp := openAIModelsResponse{
			Data: []DiscoveredModel{
				{ID: "gpt-4o", Object: "model", OwnedBy: "openai", Created: 1234567890},
				{ID: "gpt-4o-mini", Object: "model", OwnedBy: "openai", Created: 1234567891},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	models, err := DiscoverModels(context.Background(), server.URL+"/v1", "test-key", nil)
	if err != nil {
		t.Fatalf("DiscoverModels() error = %v", err)
	}
	if len(models) != 2 {
		t.Fatalf("len(models) = %d, want 2", len(models))
	}
	if models[0].ID != "gpt-4o" {
		t.Fatalf("models[0].ID = %q, want gpt-4o", models[0].ID)
	}
	if models[1].ID != "gpt-4o-mini" {
		t.Fatalf("models[1].ID = %q, want gpt-4o-mini", models[1].ID)
	}
}

func TestDiscoverModelsPlainArray(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Some providers return a plain JSON array instead of {"data": [...]}.
		models := []DiscoveredModel{
			{ID: "custom-model", Object: "model"},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(models)
	}))
	defer server.Close()

	models, err := DiscoverModels(context.Background(), server.URL, "", nil)
	if err != nil {
		t.Fatalf("DiscoverModels() error = %v", err)
	}
	if len(models) != 1 {
		t.Fatalf("len(models) = %d, want 1", len(models))
	}
	if models[0].ID != "custom-model" {
		t.Fatalf("models[0].ID = %q, want custom-model", models[0].ID)
	}
}

func TestDiscoverModelsEmptyResponse(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"data": []}`))
	}))
	defer server.Close()

	models, err := DiscoverModels(context.Background(), server.URL, "", nil)
	if err != nil {
		t.Fatalf("DiscoverModels() error = %v", err)
	}
	if len(models) != 0 {
		t.Fatalf("len(models) = %d, want 0", len(models))
	}
}

func TestDiscoverModelsError(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"error": "invalid api key"}`))
	}))
	defer server.Close()

	_, err := DiscoverModels(context.Background(), server.URL, "bad-key", nil)
	if err == nil {
		t.Fatal("expected error for 401 response")
	}
}

func TestDiscoverModelsMaxLimit(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Return more models than discoveryMaxModels.
		models := make([]DiscoveredModel, discoveryMaxModels+50)
		for i := range models {
			models[i] = DiscoveredModel{ID: "model-" + string(rune('A'+i%26))}
		}
		resp := openAIModelsResponse{Data: models}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	models, err := DiscoverModels(context.Background(), server.URL, "", nil)
	if err != nil {
		t.Fatalf("DiscoverModels() error = %v", err)
	}
	if len(models) != discoveryMaxModels {
		t.Fatalf("len(models) = %d, want %d", len(models), discoveryMaxModels)
	}
}

func TestDiscoverModelsCustomHeaders(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Custom") != "value" {
			t.Fatalf("missing custom header, got %q", r.Header.Get("X-Custom"))
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"data": [{"id": "m1"}]}`))
	}))
	defer server.Close()

	models, err := DiscoverModels(context.Background(), server.URL, "", map[string]string{"X-Custom": "value"})
	if err != nil {
		t.Fatalf("DiscoverModels() error = %v", err)
	}
	if len(models) != 1 {
		t.Fatalf("len(models) = %d, want 1", len(models))
	}
}

func TestOllamaDiscoverModels(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Fatalf("expected GET, got %s", r.Method)
		}
		if r.URL.Path != "/api/tags" {
			t.Fatalf("unexpected path %q, want /api/tags", r.URL.Path)
		}
		resp := ollamaTagsResponse{
			Models: []ollamaModelEntry{
				{Name: "llama3:latest", Model: "llama3:latest"},
				{Name: "codellama:7b", Model: "codellama:7b"},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	// Pass URL with /v1 suffix to verify it is stripped.
	models, err := OllamaDiscoverModels(context.Background(), server.URL+"/v1")
	if err != nil {
		t.Fatalf("OllamaDiscoverModels() error = %v", err)
	}
	if len(models) != 2 {
		t.Fatalf("len(models) = %d, want 2", len(models))
	}
	if models[0].ID != "llama3:latest" {
		t.Fatalf("models[0].ID = %q, want llama3:latest", models[0].ID)
	}
	if models[1].ID != "codellama:7b" {
		t.Fatalf("models[1].ID = %q, want codellama:7b", models[1].ID)
	}
}

func TestOllamaDiscoverModelsEmptyList(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"models": []}`))
	}))
	defer server.Close()

	models, err := OllamaDiscoverModels(context.Background(), server.URL)
	if err != nil {
		t.Fatalf("OllamaDiscoverModels() error = %v", err)
	}
	if len(models) != 0 {
		t.Fatalf("len(models) = %d, want 0", len(models))
	}
}

func TestOllamaModelInfo(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/api/show" {
			t.Fatalf("unexpected path %q, want /api/show", r.URL.Path)
		}

		var body ollamaShowRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("failed to decode request body: %v", err)
		}
		if body.Name != "llama3:latest" {
			t.Fatalf("body.Name = %q, want llama3:latest", body.Name)
		}

		resp := ollamaShowResponse{
			ModelInfo: map[string]any{
				"general.architecture":   "llama",
				"llama.context_length":   float64(8192),
				"llama.embedding_length": float64(4096),
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	// Pass URL with /v1 suffix to verify it is stripped.
	ctxWindow, err := OllamaModelInfo(context.Background(), server.URL+"/v1", "llama3:latest")
	if err != nil {
		t.Fatalf("OllamaModelInfo() error = %v", err)
	}
	if ctxWindow != 8192 {
		t.Fatalf("context window = %d, want 8192", ctxWindow)
	}
}

func TestOllamaModelInfoTopLevelKey(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Some models report context_length at the top level of model_info.
		resp := ollamaShowResponse{
			ModelInfo: map[string]any{
				"context_length": float64(4096),
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	ctxWindow, err := OllamaModelInfo(context.Background(), server.URL, "test-model")
	if err != nil {
		t.Fatalf("OllamaModelInfo() error = %v", err)
	}
	if ctxWindow != 4096 {
		t.Fatalf("context window = %d, want 4096", ctxWindow)
	}
}

func TestOllamaModelInfoNotFound(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := ollamaShowResponse{
			ModelInfo: map[string]any{
				"general.architecture": "llama",
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	_, err := OllamaModelInfo(context.Background(), server.URL, "unknown-model")
	if err == nil {
		t.Fatal("expected error when context window is not found")
	}
}

func TestStripV1Suffix(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input string
		want  string
	}{
		{"http://localhost:11434/v1", "http://localhost:11434"},
		{"http://localhost:11434/v1/", "http://localhost:11434"},
		{"http://localhost:11434", "http://localhost:11434"},
		{"http://localhost:11434/", "http://localhost:11434"},
	}
	for _, tt := range tests {
		if got := stripV1Suffix(tt.input); got != tt.want {
			t.Errorf("stripV1Suffix(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

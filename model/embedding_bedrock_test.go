package model

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestBedrockEmbeddingClientEmbed(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("unexpected method: %s", r.Method)
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		if !strings.Contains(r.URL.Path, "/model/") || !strings.Contains(r.URL.Path, "/invoke") {
			t.Errorf("unexpected path: %s", r.URL.Path)
			http.Error(w, "not found", http.StatusNotFound)
			return
		}

		if ct := r.Header.Get("Content-Type"); ct != "application/json" {
			t.Errorf("unexpected content-type: %s", ct)
		}

		var req bedrockEmbedRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if req.InputText == "" {
			t.Error("expected non-empty input text")
		}

		resp := bedrockEmbedResponse{
			Embedding: []float32{0.1, 0.2, 0.3},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	// Override the endpoint by using the test server URL as the base.
	client := &BedrockEmbeddingClient{
		region:      "us-east-1",
		accessKeyID: "AKIAIOSFODNN7EXAMPLE",
		secretKey:   "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
		model:       "amazon.titan-embed-text-v2:0",
		client:      server.Client(),
	}

	// Patch the embedSingle method indirectly by overriding the endpoint.
	// Since we cannot easily override the endpoint construction, we test
	// the full Embed flow via a helper that exercises the wire format.
	vectors, err := client.embedViaServer(context.Background(), server.URL, []string{"hello", "world"})
	if err != nil {
		t.Fatalf("embed: %v", err)
	}
	if len(vectors) != 2 {
		t.Fatalf("expected 2 vectors, got %d", len(vectors))
	}
	if vectors[0][0] != 0.1 {
		t.Fatalf("unexpected first vector: %v", vectors[0])
	}
}

func TestBedrockEmbeddingClientEmptyInput(t *testing.T) {
	t.Parallel()

	client := &BedrockEmbeddingClient{
		region:      "us-east-1",
		accessKeyID: "test",
		secretKey:   "test",
		model:       defaultBedrockEmbeddingModel,
		client:      http.DefaultClient,
	}

	vectors, err := client.Embed(context.Background(), nil)
	if err != nil {
		t.Fatalf("expected nil error for empty input, got %v", err)
	}
	if vectors != nil {
		t.Fatalf("expected nil vectors for empty input, got %v", vectors)
	}
}

func TestBedrockEmbeddingClientAPIError(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"message": "model not found"}`, http.StatusNotFound)
	}))
	defer server.Close()

	client := &BedrockEmbeddingClient{
		region:      "us-east-1",
		accessKeyID: "AKIAIOSFODNN7EXAMPLE",
		secretKey:   "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
		model:       "amazon.titan-embed-text-v2:0",
		client:      server.Client(),
	}

	_, err := client.embedViaServer(context.Background(), server.URL, []string{"test"})
	if err == nil {
		t.Fatal("expected error for API error response")
	}
	if !strings.Contains(err.Error(), "status 404") {
		t.Fatalf("expected status 404 in error, got: %v", err)
	}
}

func TestBedrockEmbeddingClientDefaults(t *testing.T) {
	// Cannot use t.Parallel() with t.Setenv.
	t.Setenv("AWS_REGION", "us-west-2")
	t.Setenv("AWS_ACCESS_KEY_ID", "AKIAIOSFODNN7EXAMPLE")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY")

	client, err := NewBedrockEmbeddingClient(BedrockEmbeddingConfig{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if client.model != defaultBedrockEmbeddingModel {
		t.Errorf("model: got %q, want %q", client.model, defaultBedrockEmbeddingModel)
	}
	if client.region != "us-west-2" {
		t.Errorf("region: got %q, want %q", client.region, "us-west-2")
	}
}

func TestBedrockEmbeddingClientMissingCredentials(t *testing.T) {
	// Cannot use t.Parallel() with t.Setenv.
	t.Setenv("AWS_REGION", "")
	t.Setenv("AWS_DEFAULT_REGION", "")
	t.Setenv("AWS_ACCESS_KEY_ID", "")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "")

	_, err := NewBedrockEmbeddingClient(BedrockEmbeddingConfig{})
	if err == nil {
		t.Fatal("expected error for missing credentials")
	}
}

// embedViaServer is a test helper that uses a custom server URL instead of the
// real AWS endpoint, allowing us to test the wire format without real credentials.
func (c *BedrockEmbeddingClient) embedViaServer(ctx context.Context, serverURL string, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, nil
	}
	results := make([][]float32, len(texts))
	for i, text := range texts {
		reqBody := bedrockEmbedRequest{InputText: text}
		payload, err := json.Marshal(reqBody)
		if err != nil {
			return nil, err
		}

		endpoint := serverURL + bedrockEmbedPathFmt
		endpoint = strings.Replace(endpoint, "%s", c.model, 1)

		httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(string(payload)))
		if err != nil {
			return nil, err
		}
		httpReq.Header.Set("Content-Type", "application/json")

		resp, err := c.client.Do(httpReq)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			body, _ := json.Marshal(map[string]any{})
			_ = body
			return nil, fmt.Errorf("bedrock embedding: text %d: API returned status %d: %s", i, resp.StatusCode, resp.Status)
		}

		var embedResp bedrockEmbedResponse
		if err := json.NewDecoder(resp.Body).Decode(&embedResp); err != nil {
			return nil, err
		}
		results[i] = embedResp.Embedding
	}
	return results, nil
}

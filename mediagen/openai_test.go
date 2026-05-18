package mediagen

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestOpenAIProviderGenerateImage(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/images/generations" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if payload["model"] != openAIDefaultImageModel {
			t.Fatalf("model = %v", payload["model"])
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"model": openAIDefaultImageModel,
			"data": []map[string]any{{
				"b64_json":       base64.StdEncoding.EncodeToString([]byte("png-bytes")),
				"revised_prompt": "better prompt",
			}},
		})
	}))
	defer server.Close()

	provider, err := NewOpenAIProvider(OpenAIConfig{BaseURL: server.URL, APIKey: "sk-test"})
	if err != nil {
		t.Fatalf("NewOpenAIProvider() error = %v", err)
	}
	result, err := provider.GenerateImage(context.Background(), ImageRequest{Prompt: "draw"})
	if err != nil {
		t.Fatalf("GenerateImage() error = %v", err)
	}
	if len(result.Images) != 1 {
		t.Fatalf("len(result.Images) = %d", len(result.Images))
	}
	if string(result.Images[0].Buffer) != "png-bytes" {
		t.Fatalf("image bytes = %q", string(result.Images[0].Buffer))
	}
	if len(result.RevisedPrompts) != 1 || result.RevisedPrompts[0] != "better prompt" {
		t.Fatalf("RevisedPrompts = %#v", result.RevisedPrompts)
	}
}

func TestOpenAIProviderGenerateVideo(t *testing.T) {
	t.Parallel()

	var pollCount int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/videos":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":     "vid_123",
				"model":  openAIDefaultVideoModel,
				"status": "queued",
			})
		case "/videos/vid_123":
			pollCount++
			status := "in_progress"
			if pollCount > 1 {
				status = "completed"
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":      "vid_123",
				"model":   openAIDefaultVideoModel,
				"status":  status,
				"seconds": "4",
				"size":    "1280x720",
			})
		case "/videos/vid_123/content":
			w.Header().Set("Content-Type", "video/mp4")
			_, _ = io.WriteString(w, "video-bytes")
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer server.Close()

	provider, err := NewOpenAIProvider(OpenAIConfig{BaseURL: server.URL, APIKey: "sk-test"})
	if err != nil {
		t.Fatalf("NewOpenAIProvider() error = %v", err)
	}
	result, err := provider.GenerateVideo(context.Background(), VideoRequest{Prompt: "animate", DurationSeconds: 5})
	if err != nil {
		t.Fatalf("GenerateVideo() error = %v", err)
	}
	if len(result.Videos) != 1 || string(result.Videos[0].Buffer) != "video-bytes" {
		t.Fatalf("Videos = %#v", result.Videos)
	}
	if strings.TrimSpace(result.Model) != openAIDefaultVideoModel {
		t.Fatalf("Model = %q", result.Model)
	}
}

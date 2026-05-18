package mediagen

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestFalProviderGenerateImage(t *testing.T) {
	t.Parallel()

	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/fal-ai/flux/dev":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"prompt": "rewritten prompt",
				"images": []map[string]any{{
					"url":          server.URL + "/generated/image.png",
					"content_type": "image/png",
				}},
			})
		case "/generated/image.png":
			w.Header().Set("Content-Type", "image/png")
			_, _ = io.WriteString(w, "png-bytes")
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer server.Close()

	provider, err := NewFalProvider(FalConfig{BaseURL: server.URL, APIKey: "fal_test"})
	if err != nil {
		t.Fatalf("NewFalProvider() error = %v", err)
	}
	result, err := provider.GenerateImage(context.Background(), ImageRequest{Prompt: "draw a fox"})
	if err != nil {
		t.Fatalf("GenerateImage() error = %v", err)
	}
	if len(result.Images) != 1 || string(result.Images[0].Buffer) != "png-bytes" {
		t.Fatalf("Images = %#v", result.Images)
	}
}

func TestFalProviderGenerateVideo(t *testing.T) {
	t.Parallel()

	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/fal-ai/minimax/video-01-live":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"status_url":   server.URL + "/status/req-1",
				"response_url": server.URL + "/response/req-1",
				"request_id":   "req-1",
			})
		case "/status/req-1":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"status": "COMPLETED",
			})
		case "/response/req-1":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"prompt": "animate a paper kite",
				"video": map[string]any{
					"url":          server.URL + "/generated/video.mp4",
					"content_type": "video/mp4",
				},
			})
		case "/generated/video.mp4":
			w.Header().Set("Content-Type", "video/mp4")
			_, _ = io.WriteString(w, "video-bytes")
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer server.Close()

	provider, err := NewFalProvider(FalConfig{
		BaseURL:      server.URL,
		QueueBaseURL: server.URL,
		APIKey:       "fal_test",
	})
	if err != nil {
		t.Fatalf("NewFalProvider() error = %v", err)
	}
	result, err := provider.GenerateVideo(context.Background(), VideoRequest{Prompt: "animate a kite"})
	if err != nil {
		t.Fatalf("GenerateVideo() error = %v", err)
	}
	if len(result.Videos) != 1 || string(result.Videos[0].Buffer) != "video-bytes" {
		t.Fatalf("Videos = %#v", result.Videos)
	}
}

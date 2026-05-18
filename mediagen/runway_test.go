package mediagen

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRunwayProviderGenerateVideoTextToVideo(t *testing.T) {
	t.Parallel()

	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/text_to_video":
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "task-1"})
		case "/v1/tasks/task-1":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":     "task-1",
				"status": "SUCCEEDED",
				"output": []string{server.URL + "/generated/video.mp4"},
			})
		case "/generated/video.mp4":
			w.Header().Set("Content-Type", "video/mp4")
			_, _ = io.WriteString(w, "video-bytes")
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer server.Close()

	provider, err := NewRunwayProvider(RunwayConfig{BaseURL: server.URL, APIKey: "runway_secret"})
	if err != nil {
		t.Fatalf("NewRunwayProvider() error = %v", err)
	}
	result, err := provider.GenerateVideo(context.Background(), VideoRequest{
		Prompt:          "animate a paper kite",
		AspectRatio:     "16:9",
		DurationSeconds: 5,
	})
	if err != nil {
		t.Fatalf("GenerateVideo() error = %v", err)
	}
	if len(result.Videos) != 1 || string(result.Videos[0].Buffer) != "video-bytes" {
		t.Fatalf("Videos = %#v", result.Videos)
	}
}

func TestRunwayProviderGenerateVideoImageToVideo(t *testing.T) {
	t.Parallel()

	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/image_to_video":
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "task-2"})
		case "/v1/tasks/task-2":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":     "task-2",
				"status": "SUCCEEDED",
				"output": []string{server.URL + "/generated/image-video.mp4"},
			})
		case "/generated/image-video.mp4":
			w.Header().Set("Content-Type", "video/mp4")
			_, _ = io.WriteString(w, "video-edit-bytes")
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer server.Close()

	provider, err := NewRunwayProvider(RunwayConfig{BaseURL: server.URL, APIKey: "runway_secret"})
	if err != nil {
		t.Fatalf("NewRunwayProvider() error = %v", err)
	}
	result, err := provider.GenerateVideo(context.Background(), VideoRequest{
		Prompt:      "animate this still image",
		AspectRatio: "4:3",
		InputImages: []InputAsset{{
			Buffer:   []byte("png"),
			MIMEType: "image/png",
			FileName: "frame.png",
		}},
	})
	if err != nil {
		t.Fatalf("GenerateVideo() error = %v", err)
	}
	if len(result.Videos) != 1 || string(result.Videos[0].Buffer) != "video-edit-bytes" {
		t.Fatalf("Videos = %#v", result.Videos)
	}
}

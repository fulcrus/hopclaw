package mediagen

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestMinimaxMusicProviderGenerateMusic(t *testing.T) {
	t.Parallel()

	var baseURL string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/music_generation":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"task_id":   "task-1",
				"audio_url": baseURL + "/download/track.mp3",
				"lyrics":    "sample lyrics",
				"base_resp": map[string]any{
					"status_code": 0,
					"status_msg":  "ok",
				},
			})
		case "/download/track.mp3":
			w.Header().Set("Content-Type", "audio/mpeg")
			_, _ = io.WriteString(w, "mp3-bytes")
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer server.Close()
	baseURL = server.URL

	provider, err := NewMinimaxMusicProvider(MinimaxConfig{BaseURL: server.URL, APIKey: "sk-minimax"})
	if err != nil {
		t.Fatalf("NewMinimaxMusicProvider() error = %v", err)
	}
	result, err := provider.GenerateMusic(context.Background(), MusicRequest{Prompt: "lofi piano"})
	if err != nil {
		t.Fatalf("GenerateMusic() error = %v", err)
	}
	if len(result.Tracks) != 1 || string(result.Tracks[0].Buffer) != "mp3-bytes" {
		t.Fatalf("Tracks = %#v", result.Tracks)
	}
	if len(result.Lyrics) != 1 || result.Lyrics[0] != "sample lyrics" {
		t.Fatalf("Lyrics = %#v", result.Lyrics)
	}
}

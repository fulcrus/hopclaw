package main

import (
	"crypto/subtle"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"strings"
)

type request struct {
	Action    string         `json:"action"`
	SessionID string         `json:"session_id,omitempty"`
	Params    map[string]any `json:"params,omitempty"`
}

func main() {
	token := strings.TrimSpace(os.Getenv("SAMPLE_HOST_TOKEN"))
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	})
	mux.HandleFunc("/sample-host/v1", func(w http.ResponseWriter, r *http.Request) {
		if token != "" && !authorized(r, token) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}

		var req request
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		resp := map[string]any{"ok": true}
		switch strings.TrimSpace(req.Action) {
		case "create_session":
			resp["session_id"] = "sess-demo-go-1"
			resp["data"] = map[string]any{"session_id": "sess-demo-go-1"}
		case "ping":
			resp["data"] = map[string]any{"message": "pong", "language": "go"}
		default:
			resp["ok"] = false
			resp["error"] = "unsupported action"
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})

	log.Println("listening on http://127.0.0.1:18083/sample-host/v1")
	log.Fatal(http.ListenAndServe("127.0.0.1:18083", mux))
}

func authorized(r *http.Request, token string) bool {
	raw := strings.TrimSpace(r.Header.Get("Authorization"))
	if !strings.HasPrefix(strings.ToLower(raw), "bearer ") {
		return false
	}
	candidate := strings.TrimSpace(raw[len("Bearer "):])
	return subtle.ConstantTimeCompare([]byte(candidate), []byte(token)) == 1
}

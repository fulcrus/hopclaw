package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
)

func main() {
	mux := http.NewServeMux()
	mux.HandleFunc("/callback", func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		defer r.Body.Close()

		var payload any
		if len(body) > 0 {
			if err := json.Unmarshal(body, &payload); err != nil {
				payload = string(body)
			}
		}

		fmt.Println("=== HopClaw callback received ===")
		fmt.Println("path:", r.URL.Path)
		fmt.Println("headers:", r.Header)
		encoded, _ := json.MarshalIndent(payload, "", "  ")
		fmt.Println("payload:", string(encoded))

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	})

	log.Println("listening on http://127.0.0.1:18081/callback")
	log.Fatal(http.ListenAndServe("127.0.0.1:18081", mux))
}

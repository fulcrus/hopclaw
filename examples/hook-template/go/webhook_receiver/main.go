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
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

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

		fmt.Println("=== HopClaw hook received ===")
		fmt.Println("path:", r.URL.Path)
		fmt.Println("headers:", r.Header)
		encoded, _ := json.MarshalIndent(payload, "", "  ")
		fmt.Println(string(encoded))

		w.WriteHeader(http.StatusNoContent)
	})

	log.Println("listening on http://127.0.0.1:18084")
	log.Fatal(http.ListenAndServe("127.0.0.1:18084", mux))
}

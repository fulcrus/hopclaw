package main

import (
	"encoding/json"
	"log"
	"net/http"
)

type executeRequest struct {
	ToolName string         `json:"tool_name"`
	Input    map[string]any `json:"input"`
}

func main() {
	mux := http.NewServeMux()
	mux.HandleFunc("/invoke", func(w http.ResponseWriter, r *http.Request) {
		var req executeRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		text, _ := req.Input["text"].(string)
		resp := map[string]any{
			"protocol_version": "hopclaw.tool/v1",
			"ok":               true,
			"status":           "success",
			"summary":          "Echoed input text",
			"content":          "Echo: " + text,
			"data": map[string]any{
				"echoed_text": text,
				"tool_name":   req.ToolName,
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})

	log.Println("listening on http://127.0.0.1:18082/invoke")
	log.Fatal(http.ListenAndServe("127.0.0.1:18082", mux))
}

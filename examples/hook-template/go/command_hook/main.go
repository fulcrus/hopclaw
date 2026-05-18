package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
)

func main() {
	raw, err := io.ReadAll(os.Stdin)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	payload := map[string]any{}
	if len(bytes.TrimSpace(raw)) > 0 {
		if err := json.Unmarshal(raw, &payload); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	}

	hookContext, _ := payload["_hook_context"].(map[string]any)
	resp := map[string]any{
		"ok":         true,
		"language":   "go",
		"event_type": stringField(payload, "event_type"),
		"run_id":     stringField(payload, "run_id"),
		"phase":      stringField(hookContext, "phase"),
		"message": fmt.Sprintf(
			"Handled %s for run %s",
			stringField(payload, "event_type"),
			stringField(payload, "run_id"),
		),
	}

	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(resp); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func stringField(payload map[string]any, key string) string {
	if payload == nil {
		return ""
	}
	value, _ := payload[key].(string)
	return value
}

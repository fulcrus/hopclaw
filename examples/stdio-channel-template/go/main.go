package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
)

const (
	jsonRPCVersion  = "2.0"
	protocolVersion = "2025-03-15"
)

type message struct {
	JSONRPC string         `json:"jsonrpc"`
	ID      any            `json:"id,omitempty"`
	Method  string         `json:"method,omitempty"`
	Params  map[string]any `json:"params,omitempty"`
	Result  any            `json:"result,omitempty"`
	Error   any            `json:"error,omitempty"`
}

func main() {
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		var msg message
		if err := json.Unmarshal(line, &msg); err != nil {
			write(message{
				JSONRPC: jsonRPCVersion,
				ID:      nil,
				Error: map[string]any{
					"code":    -32700,
					"message": fmt.Sprintf("parse error: %v", err),
				},
			})
			continue
		}

		switch msg.Method {
		case "initialize":
			write(message{
				JSONRPC: jsonRPCVersion,
				ID:      msg.ID,
				Result: map[string]any{
					"protocol_version": protocolVersion,
					"plugin_name":      "sample-echo-channel-go",
					"plugin_version":   "0.1.0",
					"capabilities": map[string]any{
						"send_text":      true,
						"send_rich_text": false,
						"send_file":      false,
						"edit":           false,
						"delete":         false,
						"react":          false,
						"history":        false,
					},
				},
			})
		case "connect":
			write(message{JSONRPC: jsonRPCVersion, ID: msg.ID, Result: map[string]any{"ok": true}})
			write(message{JSONRPC: jsonRPCVersion, Method: "channel/status", Params: map[string]any{
				"status":  "connected",
				"message": "go template connected",
			}})
		case "disconnect":
			write(message{JSONRPC: jsonRPCVersion, ID: msg.ID, Result: map[string]any{"ok": true}})
			write(message{JSONRPC: jsonRPCVersion, Method: "channel/status", Params: map[string]any{
				"status":  "disconnected",
				"message": "go template disconnected",
			}})
			return
		case "send":
			write(message{JSONRPC: jsonRPCVersion, ID: msg.ID, Result: map[string]any{
				"ok":         true,
				"message_id": "echo-msg-go-1",
			}})
			write(message{JSONRPC: jsonRPCVersion, Method: "channel/inbound", Params: map[string]any{
				"channel_id":  stringValue(msg.Params["channel_id"], "sample-echo"),
				"sender_id":   stringValue(msg.Params["target_id"], "unknown"),
				"sender_name": "Echo Template Go",
				"content":     "Echo: " + stringValue(msg.Params["content"], ""),
				"raw_event": map[string]any{
					"template": true,
					"language": "go",
				},
			}})
		default:
			write(message{
				JSONRPC: jsonRPCVersion,
				ID:      msg.ID,
				Error: map[string]any{
					"code":    -32601,
					"message": "method not found",
				},
			})
		}
	}
}

func write(msg message) {
	enc := json.NewEncoder(os.Stdout)
	_ = enc.Encode(msg)
}

func stringValue(v any, fallback string) string {
	s, ok := v.(string)
	if !ok || s == "" {
		return fallback
	}
	return s
}

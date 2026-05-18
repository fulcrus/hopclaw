package model

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/fulcrus/hopclaw/agent"
	"github.com/fulcrus/hopclaw/contextengine"
)

func TestOpenAICompatClientChatText(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]any
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("Decode(request) error = %v", err)
		}
		if req["model"] != "gpt-test" {
			t.Fatalf("req.model = %#v", req["model"])
		}
		// Respond with SSE stream format (Chat now always streams).
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"role\":\"assistant\",\"content\":\"hello from model\"}}]}\n\n")
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer server.Close()

	client, err := NewOpenAICompatClient(OpenAICompatConfig{
		BaseURL:      server.URL + "/v1",
		DefaultModel: "gpt-test",
	})
	if err != nil {
		t.Fatalf("NewOpenAICompatClient() error = %v", err)
	}
	resp, err := client.Chat(context.Background(), agent.ChatRequest{
		SystemPrompt: "be concise",
		Messages: []contextengine.Message{{
			Role:    contextengine.RoleUser,
			Content: "hello",
		}},
	})
	if err != nil {
		t.Fatalf("Chat() error = %v", err)
	}
	if resp.Message.Content != "hello from model" {
		t.Fatalf("resp.Message.Content = %q", resp.Message.Content)
	}
}

func TestOpenAICompatClientChatToolCalls(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify that the tool name is sanitized on the wire.
		var req map[string]any
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("Decode(request) error = %v", err)
		}
		tools, _ := req["tools"].([]any)
		if len(tools) > 0 {
			tool := tools[0].(map[string]any)
			fn := tool["function"].(map[string]any)
			if fn["name"] != "fs_x2E_read" {
				t.Fatalf("expected wire tool name fs_x2E_read, got %v", fn["name"])
			}
		}

		// Respond with SSE stream format — tool call chunks.
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"role\":\"assistant\",\"tool_calls\":[{\"index\":0,\"id\":\"call-1\",\"type\":\"function\",\"function\":{\"name\":\"fs_x2E_read\",\"arguments\":\"\"}}]}}]}\n\n")
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"function\":{\"arguments\":\"{\\\"path\\\":\\\"README.md\\\"}\"}}]}}]}\n\n")
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer server.Close()

	client, err := NewOpenAICompatClient(OpenAICompatConfig{
		BaseURL:      server.URL,
		DefaultModel: "gpt-test",
	})
	if err != nil {
		t.Fatalf("NewOpenAICompatClient() error = %v", err)
	}
	resp, err := client.Chat(context.Background(), agent.ChatRequest{
		Model: "gpt-test",
		Tools: []agent.ToolDefinition{{
			Name: "fs.read",
		}},
	})
	if err != nil {
		t.Fatalf("Chat() error = %v", err)
	}
	if len(resp.ToolCalls) != 1 {
		t.Fatalf("len(resp.ToolCalls) = %d", len(resp.ToolCalls))
	}
	// The tool name should be restored to the original dotted form.
	if resp.ToolCalls[0].Name != "fs.read" {
		t.Fatalf("resp.ToolCalls[0].Name = %q, want fs.read", resp.ToolCalls[0].Name)
	}
	if resp.ToolCalls[0].Input["path"] != "README.md" {
		t.Fatalf("resp.ToolCalls[0].Input = %#v", resp.ToolCalls[0].Input)
	}
}

func TestOpenAICompatClientNormalizesToolSchemas(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]any
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("Decode(request) error = %v", err)
		}
		tools, _ := req["tools"].([]any)
		if len(tools) != 1 {
			t.Fatalf("len(req.tools) = %d, want 1", len(tools))
		}
		tool := tools[0].(map[string]any)
		fn := tool["function"].(map[string]any)
		parameters := fn["parameters"].(map[string]any)

		properties, ok := parameters["properties"].(map[string]any)
		if !ok || properties == nil {
			t.Fatalf("parameters.properties = %#v, want empty object", parameters["properties"])
		}

		nested := parameters["nested"].(map[string]any)
		nestedProperties, ok := nested["properties"].(map[string]any)
		if !ok || nestedProperties == nil {
			t.Fatalf("nested.properties = %#v, want empty object", nested["properties"])
		}

		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"role\":\"assistant\",\"content\":\"ok\"}}]}\n\n")
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer server.Close()

	client, err := NewOpenAICompatClient(OpenAICompatConfig{
		BaseURL:      server.URL,
		DefaultModel: "gpt-test",
	})
	if err != nil {
		t.Fatalf("NewOpenAICompatClient() error = %v", err)
	}

	_, err = client.Chat(context.Background(), agent.ChatRequest{
		Model: "gpt-test",
		Tools: []agent.ToolDefinition{{
			Name: "channel.list",
			InputSchema: map[string]any{
				"type":       "object",
				"properties": nil,
				"nested": map[string]any{
					"type":       "object",
					"properties": nil,
				},
			},
		}},
	})
	if err != nil {
		t.Fatalf("Chat() error = %v", err)
	}
}

func TestOpenAICompatStreamCallbackUsesCanonicalToolName(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"role\":\"assistant\",\"tool_calls\":[{\"index\":0,\"id\":\"call-1\",\"type\":\"function\",\"function\":{\"name\":\"skill_x2E_ensure\",\"arguments\":\"\"}}]}}]}\n\n")
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"function\":{\"arguments\":\"{\\\"name\\\":\\\"checks\\\"}\"}}]}}]}\n\n")
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer server.Close()

	client, err := NewOpenAICompatClient(OpenAICompatConfig{
		BaseURL:      server.URL,
		DefaultModel: "gpt-test",
	})
	if err != nil {
		t.Fatalf("NewOpenAICompatClient() error = %v", err)
	}

	var started []string
	cb := &testStreamCallback{
		onToolCallStart: func(_ context.Context, _ string, toolName string) {
			started = append(started, toolName)
		},
	}
	resp, err := client.ChatStream(context.Background(), agent.ChatRequest{
		Model: "gpt-test",
		Tools: []agent.ToolDefinition{{
			Name: "skill.ensure",
		}},
	}, cb)
	if err != nil {
		t.Fatalf("ChatStream() error = %v", err)
	}
	if len(started) != 1 || started[0] != "skill.ensure" {
		t.Fatalf("started tool names = %#v", started)
	}
	if len(resp.ToolCalls) != 1 || resp.ToolCalls[0].Name != "skill.ensure" {
		t.Fatalf("resp.ToolCalls = %#v", resp.ToolCalls)
	}
}

func TestSanitizeToolNameAvoidsCollisions(t *testing.T) {
	t.Parallel()

	first := sanitizeToolName("fs.read")
	second := sanitizeToolName("fs_read")
	if first == second {
		t.Fatalf("sanitizeToolName collision: %q", first)
	}
	if first != "fs_x2E_read" {
		t.Fatalf("sanitizeToolName(fs.read) = %q", first)
	}
	if second != "fs_x5F_read" {
		t.Fatalf("sanitizeToolName(fs_read) = %q", second)
	}
}

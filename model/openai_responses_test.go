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

func TestConvertMessagesToInputUsesResponsesItems(t *testing.T) {
	t.Parallel()

	items := convertMessagesToInput([]contextengine.Message{
		{
			Role:    contextengine.RoleSystem,
			Content: "skip me",
		},
		{
			Role:    contextengine.RoleUser,
			Content: "plain text",
		},
		{
			Role: contextengine.RoleUser,
			ContentBlocks: []contextengine.ContentBlock{
				{Type: contextengine.ContentBlockText, Text: "look"},
				{Type: contextengine.ContentBlockImage, SourceURL: "https://example.com/cat.png"},
			},
		},
		{
			Role:    contextengine.RoleAssistant,
			Content: "calling a tool",
			ToolCalls: []contextengine.ToolCallRef{{
				Name:      "fs.read",
				Arguments: `{"path":"README.md"}`,
			}},
		},
		{
			Role:       contextengine.RoleTool,
			ToolCallID: "tool-call-1",
			Content:    `{"ok":true}`,
		},
	})

	if len(items) != 5 {
		t.Fatalf("len(items) = %d, want 5", len(items))
	}

	userText, ok := items[0].Content.([]responsesInputContentPart)
	if !ok || len(userText) != 1 {
		t.Fatalf("items[0].Content = %#v", items[0].Content)
	}
	if userText[0].Type != "input_text" || userText[0].Text != "plain text" {
		t.Fatalf("user text content = %#v", userText[0])
	}

	userVision, ok := items[1].Content.([]responsesInputContentPart)
	if !ok || len(userVision) != 2 {
		t.Fatalf("items[1].Content = %#v", items[1].Content)
	}
	if userVision[1].ImageURL != "https://example.com/cat.png" {
		t.Fatalf("user image content = %#v", userVision[1])
	}

	assistantText, ok := items[2].Content.([]responsesInputContentPart)
	if !ok || len(assistantText) != 1 || assistantText[0].Text != "calling a tool" {
		t.Fatalf("assistant text content = %#v", items[2].Content)
	}

	if items[3].Type != "function_call" {
		t.Fatalf("items[3].Type = %q, want function_call", items[3].Type)
	}
	if items[3].CallID == "" || items[3].ID == "" {
		t.Fatalf("assistant tool call ids = %#v", items[3])
	}
	if items[3].Name != "fs_x2E_read" {
		t.Fatalf("assistant tool call name = %q", items[3].Name)
	}
	if items[3].Arguments != `{"path":"README.md"}` {
		t.Fatalf("assistant tool call arguments = %q", items[3].Arguments)
	}

	if items[4].Type != "function_call_output" {
		t.Fatalf("items[4].Type = %q, want function_call_output", items[4].Type)
	}
	if items[4].CallID != "tool-call-1" || items[4].Output != `{"ok":true}` {
		t.Fatalf("tool output item = %#v", items[4])
	}
}

func TestOpenAIResponsesClientChatJSONResponse(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/responses" {
			t.Fatalf("r.URL.Path = %q", r.URL.Path)
		}
		if accept := r.Header.Get("Accept"); accept != "application/json" {
			t.Fatalf("Accept = %q, want application/json", accept)
		}

		var req map[string]any
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("Decode(request) error = %v", err)
		}
		if req["stream"] != false {
			t.Fatalf("req.stream = %#v, want false", req["stream"])
		}
		if req["instructions"] != "be concise" {
			t.Fatalf("req.instructions = %#v", req["instructions"])
		}
		reasoning, ok := req["reasoning"].(map[string]any)
		if !ok || reasoning["effort"] != "high" {
			t.Fatalf("req.reasoning = %#v, want effort=high", req["reasoning"])
		}

		input, ok := req["input"].([]any)
		if !ok || len(input) != 1 {
			t.Fatalf("req.input = %#v", req["input"])
		}
		msg := input[0].(map[string]any)
		content := msg["content"].([]any)
		part := content[0].(map[string]any)
		if part["type"] != "input_text" || part["text"] != "hello" {
			t.Fatalf("input content part = %#v", part)
		}

		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"status":"completed","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"hello from responses"}]}],"usage":{"input_tokens":7,"output_tokens":2,"total_tokens":9}}`)
	}))
	defer server.Close()

	client, err := NewOpenAIResponsesClient(OpenAIResponsesConfig{
		BaseURL:      server.URL + "/v1",
		DefaultModel: "gpt-test",
	})
	if err != nil {
		t.Fatalf("NewOpenAIResponsesClient() error = %v", err)
	}

	resp, err := client.Chat(context.Background(), agent.ChatRequest{
		SystemPrompt: "be concise",
		ThinkingMode: agent.ThinkingExtended,
		Messages: []contextengine.Message{{
			Role:    contextengine.RoleUser,
			Content: "hello",
		}},
	})
	if err != nil {
		t.Fatalf("Chat() error = %v", err)
	}
	if resp.Message.Content != "hello from responses" {
		t.Fatalf("resp.Message.Content = %q", resp.Message.Content)
	}
	if resp.Usage == nil || resp.Usage.PromptTokens != 7 || resp.Usage.CompletionTokens != 2 || resp.Usage.TotalTokens != 9 {
		t.Fatalf("resp.Usage = %#v", resp.Usage)
	}
}

func TestOpenAIResponsesClientChatMapsToolHistoryAndDefinitions(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]any
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("Decode(request) error = %v", err)
		}

		if req["tool_choice"] != "auto" {
			t.Fatalf("req.tool_choice = %#v, want auto", req["tool_choice"])
		}
		tools := req["tools"].([]any)
		tool := tools[0].(map[string]any)
		if tool["name"] != "fs_x2E_read" {
			t.Fatalf("tool name = %#v, want fs_x2E_read", tool["name"])
		}

		input := req["input"].([]any)
		if len(input) != 3 {
			t.Fatalf("len(req.input) = %d, want 3", len(input))
		}

		call := input[1].(map[string]any)
		if call["type"] != "function_call" || call["call_id"] != "call-1" {
			t.Fatalf("function_call item = %#v", call)
		}
		if call["name"] != "fs_x2E_read" {
			t.Fatalf("function_call name = %#v", call["name"])
		}

		callOutput := input[2].(map[string]any)
		if callOutput["type"] != "function_call_output" || callOutput["call_id"] != "call-1" {
			t.Fatalf("function_call_output item = %#v", callOutput)
		}
		if callOutput["output"] != "README contents" {
			t.Fatalf("function_call_output output = %#v", callOutput["output"])
		}

		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"status":"completed","output":[{"type":"function_call","call_id":"call-2","name":"fs_x2E_read","arguments":"{\"path\":\"README.md\"}"}]}`)
	}))
	defer server.Close()

	client, err := NewOpenAIResponsesClient(OpenAIResponsesConfig{
		BaseURL:      server.URL,
		DefaultModel: "gpt-test",
	})
	if err != nil {
		t.Fatalf("NewOpenAIResponsesClient() error = %v", err)
	}

	resp, err := client.Chat(context.Background(), agent.ChatRequest{
		Model: "gpt-test",
		Messages: []contextengine.Message{
			{
				Role:    contextengine.RoleUser,
				Content: "check the readme",
			},
			{
				Role: contextengine.RoleAssistant,
				ToolCalls: []contextengine.ToolCallRef{{
					ID:        "call-1",
					Name:      "fs.read",
					Arguments: `{"path":"README.md"}`,
				}},
			},
			{
				Role:       contextengine.RoleTool,
				ToolCallID: "call-1",
				Content:    "README contents",
			},
		},
		Tools: []agent.ToolDefinition{{
			Name: "fs.read",
		}},
	})
	if err != nil {
		t.Fatalf("Chat() error = %v", err)
	}
	if len(resp.ToolCalls) != 1 {
		t.Fatalf("len(resp.ToolCalls) = %d, want 1", len(resp.ToolCalls))
	}
	if resp.ToolCalls[0].Name != "fs.read" {
		t.Fatalf("resp.ToolCalls[0].Name = %q, want fs.read", resp.ToolCalls[0].Name)
	}
	if resp.ToolCalls[0].Input["path"] != "README.md" {
		t.Fatalf("resp.ToolCalls[0].Input = %#v", resp.ToolCalls[0].Input)
	}
	if resp.Message.Content != "" {
		t.Fatalf("resp.Message.Content = %q, want empty when tool calls are present", resp.Message.Content)
	}
}

func TestOpenAIResponsesClientChatStreamTextResponse(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if accept := r.Header.Get("Accept"); accept != "text/event-stream" {
			t.Fatalf("Accept = %q, want text/event-stream", accept)
		}

		var req map[string]any
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("Decode(request) error = %v", err)
		}
		if req["stream"] != true {
			t.Fatalf("req.stream = %#v, want true", req["stream"])
		}

		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "event: response.output_text.delta\n")
		fmt.Fprint(w, "data: {\"output_index\":0,\"delta\":\"hello \"}\n\n")
		fmt.Fprint(w, "event: response.output_text.delta\n")
		fmt.Fprint(w, "data: {\"output_index\":0,\"delta\":\"world\"}\n\n")
		fmt.Fprint(w, "event: response.completed\n")
		fmt.Fprint(w, "data: {\"response\":{\"status\":\"completed\",\"output\":[{\"type\":\"message\",\"content\":[{\"type\":\"output_text\",\"text\":\"hello world\"}]}],\"usage\":{\"input_tokens\":11,\"output_tokens\":3,\"total_tokens\":14}}}\n\n")
	}))
	defer server.Close()

	client, err := NewOpenAIResponsesClient(OpenAIResponsesConfig{
		BaseURL:      server.URL,
		DefaultModel: "gpt-test",
	})
	if err != nil {
		t.Fatalf("NewOpenAIResponsesClient() error = %v", err)
	}

	var deltas []string
	completed := false
	cb := &testStreamCallback{
		onTextDelta: func(_ context.Context, delta string) {
			deltas = append(deltas, delta)
		},
		onComplete: func(context.Context) {
			completed = true
		},
	}

	resp, err := client.ChatStream(context.Background(), agent.ChatRequest{
		Model: "gpt-test",
		Messages: []contextengine.Message{{
			Role:    contextengine.RoleUser,
			Content: "hello",
		}},
	}, cb)
	if err != nil {
		t.Fatalf("ChatStream() error = %v", err)
	}
	if len(deltas) != 2 || deltas[0] != "hello " || deltas[1] != "world" {
		t.Fatalf("text deltas = %#v", deltas)
	}
	if !completed {
		t.Fatal("expected completion callback")
	}
	if resp.Message.Content != "hello world" {
		t.Fatalf("resp.Message.Content = %q", resp.Message.Content)
	}
	if resp.Usage == nil || resp.Usage.PromptTokens != 11 || resp.Usage.CompletionTokens != 3 || resp.Usage.TotalTokens != 14 {
		t.Fatalf("resp.Usage = %#v", resp.Usage)
	}
}

func TestOpenAIResponsesClientChatStreamToolCallUsesCanonicalName(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "event: response.output_item.added\n")
		fmt.Fprint(w, "data: {\"output_index\":0,\"item\":{\"type\":\"function_call\",\"call_id\":\"call-1\",\"name\":\"skill_x2E_ensure\"}}\n\n")
		fmt.Fprint(w, "event: response.function_call_arguments.delta\n")
		fmt.Fprint(w, "data: {\"output_index\":0,\"delta\":\"{\\\"name\\\":\\\"checks\\\"}\"}\n\n")
		fmt.Fprint(w, "event: response.completed\n")
		fmt.Fprint(w, "data: {\"response\":{\"status\":\"completed\",\"output\":[{\"type\":\"function_call\",\"call_id\":\"call-1\",\"name\":\"skill_x2E_ensure\",\"arguments\":\"{\\\"name\\\":\\\"checks\\\"}\"}]}}\n\n")
	}))
	defer server.Close()

	client, err := NewOpenAIResponsesClient(OpenAIResponsesConfig{
		BaseURL:      server.URL,
		DefaultModel: "gpt-test",
	})
	if err != nil {
		t.Fatalf("NewOpenAIResponsesClient() error = %v", err)
	}

	var started []string
	var argDeltas []string
	cb := &testStreamCallback{
		onToolCallStart: func(_ context.Context, _ string, toolName string) {
			started = append(started, toolName)
		},
		onToolCallDelta: func(_ context.Context, _ string, delta string) {
			argDeltas = append(argDeltas, delta)
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
	if len(argDeltas) != 1 || argDeltas[0] != `{"name":"checks"}` {
		t.Fatalf("tool arg deltas = %#v", argDeltas)
	}
	if len(resp.ToolCalls) != 1 || resp.ToolCalls[0].Name != "skill.ensure" {
		t.Fatalf("resp.ToolCalls = %#v", resp.ToolCalls)
	}
	if resp.ToolCalls[0].Input["name"] != "checks" {
		t.Fatalf("resp.ToolCalls[0].Input = %#v", resp.ToolCalls[0].Input)
	}
}

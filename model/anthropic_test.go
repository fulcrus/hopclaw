package model

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/fulcrus/hopclaw/agent"
	"github.com/fulcrus/hopclaw/contextengine"
)

// ---------------------------------------------------------------------------
// Constructor tests
// ---------------------------------------------------------------------------

func TestNewAnthropicClientRequiresAPIKey(t *testing.T) {
	t.Parallel()

	_, err := NewAnthropicClient(AnthropicConfig{})
	if err == nil {
		t.Fatal("expected error when api_key is empty")
	}
	if !strings.Contains(err.Error(), "api_key is required") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestNewAnthropicClientDefaultBaseURL(t *testing.T) {
	t.Parallel()

	client, err := NewAnthropicClient(AnthropicConfig{APIKey: "test-key"})
	if err != nil {
		t.Fatalf("NewAnthropicClient() error = %v", err)
	}
	if client.baseURL != "https://api.anthropic.com" {
		t.Fatalf("baseURL = %q, want https://api.anthropic.com", client.baseURL)
	}
}

func TestNewAnthropicClientCustomBaseURL(t *testing.T) {
	t.Parallel()

	client, err := NewAnthropicClient(AnthropicConfig{
		APIKey:  "test-key",
		BaseURL: "https://custom.api.com/",
	})
	if err != nil {
		t.Fatalf("NewAnthropicClient() error = %v", err)
	}
	if client.baseURL != "https://custom.api.com" {
		t.Fatalf("baseURL = %q, want https://custom.api.com (trailing slash stripped)", client.baseURL)
	}
}

// ---------------------------------------------------------------------------
// Chat tests
// ---------------------------------------------------------------------------

func TestAnthropicClientChatTextResponse(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/v1/messages" {
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
		if r.Header.Get("x-api-key") != "test-key" {
			t.Fatalf("unexpected x-api-key: %q", r.Header.Get("x-api-key"))
		}
		if r.Header.Get("anthropic-version") != anthropicAPIVersion {
			t.Fatalf("unexpected anthropic-version: %q", r.Header.Get("anthropic-version"))
		}

		var req anthropicRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if req.Model != "claude-test" {
			t.Fatalf("req.Model = %q", req.Model)
		}
		if req.System != "be helpful" {
			t.Fatalf("req.System = %q", req.System)
		}
		if req.CacheControl == nil || req.CacheControl.Type != "ephemeral" {
			t.Fatalf("req.CacheControl = %#v, want top-level ephemeral cache control", req.CacheControl)
		}
		if req.Temperature != 0.42 {
			t.Fatalf("req.Temperature = %v, want 0.42", req.Temperature)
		}
		if req.Thinking == nil || req.Thinking.Type != "enabled" || req.Thinking.BudgetTokens < 1024 {
			t.Fatalf("req.Thinking = %#v, want enabled thinking config", req.Thinking)
		}

		resp := anthropicResponse{
			ID:   "msg_test",
			Type: "message",
			Role: "assistant",
			Content: []anthropicResponseBlock{
				{Type: "text", Text: "hello from anthropic"},
			},
			StopReason: "end_turn",
			Usage: &anthropicUsage{
				InputTokens:              10,
				OutputTokens:             5,
				CacheCreationInputTokens: 4,
				CacheReadInputTokens:     6,
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client, err := NewAnthropicClient(AnthropicConfig{
		BaseURL: server.URL,
		APIKey:  "test-key",
	})
	if err != nil {
		t.Fatalf("NewAnthropicClient() error = %v", err)
	}
	client.cachePrompts = true

	resp, err := client.Chat(context.Background(), agent.ChatRequest{
		Model:        "claude-test",
		SystemPrompt: "be helpful",
		Temperature:  0.42,
		ThinkingMode: agent.ThinkingExtended,
		Messages: []contextengine.Message{
			{Role: contextengine.RoleUser, Content: "hello"},
		},
	})
	if err != nil {
		t.Fatalf("Chat() error = %v", err)
	}
	if resp.Message.Content != "hello from anthropic" {
		t.Fatalf("resp.Message.Content = %q", resp.Message.Content)
	}
	if resp.Message.Role != contextengine.RoleAssistant {
		t.Fatalf("resp.Message.Role = %q", resp.Message.Role)
	}
	if resp.Usage == nil {
		t.Fatal("resp.Usage is nil")
	}
	if resp.Usage.PromptTokens != 20 || resp.Usage.CompletionTokens != 5 || resp.Usage.TotalTokens != 25 {
		t.Fatalf("resp.Usage = %+v", resp.Usage)
	}
	if resp.Usage.CacheCreationInputTokens != 4 || resp.Usage.CacheReadInputTokens != 6 {
		t.Fatalf("resp.Usage cache fields = %+v", resp.Usage)
	}
}

func TestAnthropicClientChatToolCall(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req anthropicRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		// Verify tool names are sanitized on the wire.
		if len(req.Tools) != 1 {
			t.Fatalf("len(req.Tools) = %d, want 1", len(req.Tools))
		}
		if req.Tools[0].Name != "fs_x2E_read" {
			t.Fatalf("req.Tools[0].Name = %q, want fs_x2E_read", req.Tools[0].Name)
		}
		if req.Tools[0].CacheControl == nil || req.Tools[0].CacheControl.Type != "ephemeral" {
			t.Fatalf("req.Tools[0].CacheControl = %#v, want ephemeral", req.Tools[0].CacheControl)
		}

		resp := anthropicResponse{
			ID:   "msg_tool",
			Role: "assistant",
			Content: []anthropicResponseBlock{
				{
					Type:  "tool_use",
					ID:    "call-1",
					Name:  "fs_x2E_read",
					Input: map[string]any{"path": "README.md"},
				},
			},
			StopReason: "tool_use",
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client, err := NewAnthropicClient(AnthropicConfig{
		BaseURL: server.URL,
		APIKey:  "test-key",
	})
	if err != nil {
		t.Fatalf("NewAnthropicClient() error = %v", err)
	}
	client.cachePrompts = true

	resp, err := client.Chat(context.Background(), agent.ChatRequest{
		Model: "claude-test",
		Tools: []agent.ToolDefinition{{
			Name:        "fs.read",
			Description: "Read a file",
		}},
		Messages: []contextengine.Message{
			{Role: contextengine.RoleUser, Content: "read README"},
		},
	})
	if err != nil {
		t.Fatalf("Chat() error = %v", err)
	}
	if len(resp.ToolCalls) != 1 {
		t.Fatalf("len(resp.ToolCalls) = %d", len(resp.ToolCalls))
	}
	// Tool name should be restored to the original.
	if resp.ToolCalls[0].Name != "fs.read" {
		t.Fatalf("resp.ToolCalls[0].Name = %q, want fs.read", resp.ToolCalls[0].Name)
	}
	if resp.ToolCalls[0].Input["path"] != "README.md" {
		t.Fatalf("resp.ToolCalls[0].Input = %v", resp.ToolCalls[0].Input)
	}
}

func TestAnthropicClientChatSplitsStableSystemPrefixForCaching(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req anthropicRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}

		blocks, ok := req.System.([]any)
		if !ok {
			t.Fatalf("req.System type = %T, want []any after JSON decode", req.System)
		}
		if len(blocks) != 2 {
			t.Fatalf("len(system blocks) = %d, want 2", len(blocks))
		}

		first, ok := blocks[0].(map[string]any)
		if !ok {
			t.Fatalf("system[0] type = %T, want map[string]any", blocks[0])
		}
		if got := first["text"]; got != "Stable rules\n\n<project_rules>\nStay precise.\n</project_rules>" {
			t.Fatalf("system[0].text = %#v", got)
		}
		cache, ok := first["cache_control"].(map[string]any)
		if !ok || cache["type"] != "ephemeral" {
			t.Fatalf("system[0].cache_control = %#v", first["cache_control"])
		}

		second, ok := blocks[1].(map[string]any)
		if !ok {
			t.Fatalf("system[1] type = %T, want map[string]any", blocks[1])
		}
		if got := second["text"]; got != "<session_state>\n- status: running\n</session_state>\n\n## Tool Usage Guidelines\n\nPrefer built-in tools." {
			t.Fatalf("system[1].text = %#v", got)
		}
		if _, ok := second["cache_control"]; ok {
			t.Fatalf("system[1].cache_control present = %#v, want absent", second["cache_control"])
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(anthropicResponse{
			ID:      "msg_cache_system",
			Role:    "assistant",
			Content: []anthropicResponseBlock{{Type: "text", Text: "ok"}},
		})
	}))
	defer server.Close()

	client, err := NewAnthropicClient(AnthropicConfig{
		BaseURL: server.URL,
		APIKey:  "test-key",
	})
	if err != nil {
		t.Fatalf("NewAnthropicClient() error = %v", err)
	}
	client.cachePrompts = true

	_, err = client.Chat(context.Background(), agent.ChatRequest{
		Model:        "claude-test",
		SystemPrompt: "Stable rules\n\n<project_rules>\nStay precise.\n</project_rules>\n\n<session_state>\n- status: running\n</session_state>\n\n## Tool Usage Guidelines\n\nPrefer built-in tools.",
		Messages: []contextengine.Message{
			{Role: contextengine.RoleUser, Content: "hi"},
		},
	})
	if err != nil {
		t.Fatalf("Chat() error = %v", err)
	}
}

func TestAnthropicClientChatCompatibleProviderDoesNotSendPromptCaching(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req anthropicRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if req.CacheControl != nil {
			t.Fatalf("req.CacheControl = %#v, want nil for compatible providers", req.CacheControl)
		}
		if req.System != "be helpful" {
			t.Fatalf("req.System = %#v, want raw string system prompt", req.System)
		}
		if len(req.Tools) != 1 {
			t.Fatalf("len(req.Tools) = %d, want 1", len(req.Tools))
		}
		if req.Tools[0].CacheControl != nil {
			t.Fatalf("req.Tools[0].CacheControl = %#v, want nil", req.Tools[0].CacheControl)
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(anthropicResponse{
			ID:      "msg_no_cache",
			Role:    "assistant",
			Content: []anthropicResponseBlock{{Type: "text", Text: "ok"}},
		})
	}))
	defer server.Close()

	client, err := NewAnthropicClient(AnthropicConfig{
		BaseURL: server.URL,
		APIKey:  "test-key",
	})
	if err != nil {
		t.Fatalf("NewAnthropicClient() error = %v", err)
	}
	client.cachePrompts = false

	_, err = client.Chat(context.Background(), agent.ChatRequest{
		Model:        "claude-test",
		SystemPrompt: "be helpful",
		Tools: []agent.ToolDefinition{{
			Name: "fs.read",
		}},
		Messages: []contextengine.Message{
			{Role: contextengine.RoleUser, Content: "hi"},
		},
	})
	if err != nil {
		t.Fatalf("Chat() error = %v", err)
	}
}

func TestFromAnthropicResponseFallsBackToDesanitizedToolName(t *testing.T) {
	t.Parallel()

	resp, err := fromAnthropicResponse(anthropicResponse{
		Content: []anthropicResponseBlock{{
			Type:  "tool_use",
			ID:    "call-1",
			Name:  "fs_x2E_read",
			Input: map[string]any{"path": "README.md"},
		}},
	}, map[string]string{})
	if err != nil {
		t.Fatalf("fromAnthropicResponse() error = %v", err)
	}
	if len(resp.ToolCalls) != 1 {
		t.Fatalf("len(resp.ToolCalls) = %d", len(resp.ToolCalls))
	}
	if resp.ToolCalls[0].Name != "fs.read" {
		t.Fatalf("resp.ToolCalls[0].Name = %q, want fs.read", resp.ToolCalls[0].Name)
	}
}

func TestShouldSendAnthropicBearerAuthForCompatibleProviders(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		baseURL string
		want    bool
	}{
		{name: "minimax", baseURL: "https://api.minimax.io/anthropic", want: true},
		{name: "hunyuan", baseURL: "https://api.hunyuan.cloud.tencent.com/anthropic", want: true},
		{name: "xiaomi", baseURL: "https://api.xiaomimimo.com/anthropic", want: true},
		{name: "anthropic", baseURL: "https://api.anthropic.com", want: false},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := shouldSendAnthropicBearerAuth(tt.baseURL, nil); got != tt.want {
				t.Fatalf("shouldSendAnthropicBearerAuth(%q) = %v, want %v", tt.baseURL, got, tt.want)
			}
		})
	}
}

func TestShouldSendAnthropicBearerAuthHonorsCaseInsensitiveExplicitHeader(t *testing.T) {
	t.Parallel()

	if shouldSendAnthropicBearerAuth("https://api.minimax.io/anthropic", map[string]string{"authorization": "Bearer custom"}) {
		t.Fatal("explicit lowercase authorization header should disable generated bearer auth")
	}
}

func TestShouldSendKimiCodingUserAgent(t *testing.T) {
	t.Parallel()
	if !shouldSendKimiCodingUserAgent("https://api.kimi.com/coding/", nil) {
		t.Fatal("expected kimi coding base URL to enable default User-Agent")
	}
	if shouldSendKimiCodingUserAgent("https://api.kimi.com/coding/", map[string]string{"User-Agent": "custom"}) {
		t.Fatal("explicit User-Agent header should disable default kimi coding User-Agent")
	}
}

func TestShouldSendKimiCodingUserAgentHonorsCaseInsensitiveExplicitHeader(t *testing.T) {
	t.Parallel()

	if shouldSendKimiCodingUserAgent("https://api.kimi.com/coding/", map[string]string{"user-agent": "custom"}) {
		t.Fatal("explicit lowercase user-agent header should disable generated kimi coding User-Agent")
	}
}

func TestAnthropicClientChatModelRequired(t *testing.T) {
	t.Parallel()

	client, err := NewAnthropicClient(AnthropicConfig{APIKey: "test-key"})
	if err != nil {
		t.Fatalf("NewAnthropicClient() error = %v", err)
	}

	_, err = client.Chat(context.Background(), agent.ChatRequest{})
	if err == nil {
		t.Fatal("expected error when model is empty")
	}
	if !strings.Contains(err.Error(), "model is required") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestAnthropicClientChatAPIError(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"type":"error","error":{"type":"authentication_error","message":"invalid api key"}}`))
	}))
	defer server.Close()

	client, err := NewAnthropicClient(AnthropicConfig{
		BaseURL: server.URL,
		APIKey:  "bad-key",
	})
	if err != nil {
		t.Fatalf("NewAnthropicClient() error = %v", err)
	}

	_, err = client.Chat(context.Background(), agent.ChatRequest{
		Model:    "claude-test",
		Messages: []contextengine.Message{{Role: contextengine.RoleUser, Content: "hi"}},
	})
	if err == nil {
		t.Fatal("expected error on 401 response")
	}
	if !strings.Contains(err.Error(), "authentication_error") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestAnthropicClientChatRetriesTransientHTTPFailure(t *testing.T) {
	t.Parallel()

	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			w.Write([]byte(`{"type":"error","error":{"type":"overloaded_error","message":"temporarily unavailable"}}`))
			return
		}

		resp := anthropicResponse{
			ID:   "msg_retry",
			Type: "message",
			Role: "assistant",
			Content: []anthropicResponseBlock{
				{Type: "text", Text: "recovered"},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client, err := NewAnthropicClient(AnthropicConfig{
		BaseURL: server.URL,
		APIKey:  "test-key",
	})
	if err != nil {
		t.Fatalf("NewAnthropicClient() error = %v", err)
	}

	resp, err := client.Chat(context.Background(), agent.ChatRequest{
		Model:    "claude-test",
		Messages: []contextengine.Message{{Role: contextengine.RoleUser, Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("Chat() error = %v", err)
	}
	if attempts != 2 {
		t.Fatalf("attempts = %d, want 2", attempts)
	}
	if resp.Message.Content != "recovered" {
		t.Fatalf("resp.Message.Content = %q, want recovered", resp.Message.Content)
	}
}

func TestAnthropicClientChatCustomHeaders(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Custom") != "test-value" {
			t.Fatalf("missing custom header, got %q", r.Header.Get("X-Custom"))
		}
		resp := anthropicResponse{
			ID:      "msg_hdr",
			Role:    "assistant",
			Content: []anthropicResponseBlock{{Type: "text", Text: "ok"}},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client, err := NewAnthropicClient(AnthropicConfig{
		BaseURL: server.URL,
		APIKey:  "test-key",
		Headers: map[string]string{"X-Custom": "test-value"},
	})
	if err != nil {
		t.Fatalf("NewAnthropicClient() error = %v", err)
	}

	_, err = client.Chat(context.Background(), agent.ChatRequest{
		Model:    "claude-test",
		Messages: []contextengine.Message{{Role: contextengine.RoleUser, Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("Chat() error = %v", err)
	}
}

func TestAnthropicClientChatCompatibleProviderPreservesExplicitLowercaseAuthAndUserAgent(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer custom-auth" {
			t.Fatalf("Authorization = %q, want Bearer custom-auth", got)
		}
		if got := r.Header.Get("User-Agent"); got != "custom-agent/1.0" {
			t.Fatalf("User-Agent = %q, want custom-agent/1.0", got)
		}
		if got := r.Header.Get("x-api-key"); got != "test-key" {
			t.Fatalf("x-api-key = %q, want test-key", got)
		}
		if got := r.Header.Get("anthropic-version"); got != anthropicAPIVersion {
			t.Fatalf("anthropic-version = %q, want %q", got, anthropicAPIVersion)
		}

		resp := anthropicResponse{
			ID:      "msg_hdr_preserve",
			Role:    "assistant",
			Content: []anthropicResponseBlock{{Type: "text", Text: "ok"}},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client, err := NewAnthropicClient(AnthropicConfig{
		BaseURL: server.URL,
		APIKey:  "test-key",
		Headers: map[string]string{
			"authorization": "Bearer custom-auth",
			"user-agent":    "custom-agent/1.0",
		},
	})
	if err != nil {
		t.Fatalf("NewAnthropicClient() error = %v", err)
	}
	client.baseURL = server.URL + "/api.minimax.io/anthropic/api.kimi.com/coding"

	_, err = client.Chat(context.Background(), agent.ChatRequest{
		Model:    "claude-test",
		Messages: []contextengine.Message{{Role: contextengine.RoleUser, Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("Chat() error = %v", err)
	}
}

// ---------------------------------------------------------------------------
// Streaming tests
// ---------------------------------------------------------------------------

func TestAnthropicClientChatStreamText(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Accept") != "text/event-stream" {
			t.Fatalf("expected Accept: text/event-stream, got %q", r.Header.Get("Accept"))
		}

		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintln(w, `event: message_start`)
		fmt.Fprintln(w, `data: {"type":"message_start","message":{"usage":{"input_tokens":8,"output_tokens":0,"cache_creation_input_tokens":5,"cache_read_input_tokens":13}}}`)
		fmt.Fprintln(w)
		fmt.Fprintln(w, `event: content_block_start`)
		fmt.Fprintln(w, `data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`)
		fmt.Fprintln(w)
		fmt.Fprintln(w, `event: content_block_delta`)
		fmt.Fprintln(w, `data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"hello "}}`)
		fmt.Fprintln(w)
		fmt.Fprintln(w, `event: content_block_delta`)
		fmt.Fprintln(w, `data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"stream"}}`)
		fmt.Fprintln(w)
		fmt.Fprintln(w, `event: message_delta`)
		fmt.Fprintln(w, `data: {"type":"message_delta","usage":{"output_tokens":3}}`)
		fmt.Fprintln(w)
		fmt.Fprintln(w, `event: message_stop`)
		fmt.Fprintln(w, `data: {"type":"message_stop"}`)
	}))
	defer server.Close()

	client, err := NewAnthropicClient(AnthropicConfig{
		BaseURL: server.URL,
		APIKey:  "test-key",
	})
	if err != nil {
		t.Fatalf("NewAnthropicClient() error = %v", err)
	}

	resp, err := client.ChatStream(context.Background(), agent.ChatRequest{
		Model:    "claude-test",
		Messages: []contextengine.Message{{Role: contextengine.RoleUser, Content: "hi"}},
	}, nil)
	if err != nil {
		t.Fatalf("ChatStream() error = %v", err)
	}
	if resp.Message.Content != "hello stream" {
		t.Fatalf("resp.Message.Content = %q, want 'hello stream'", resp.Message.Content)
	}
	if resp.Usage == nil {
		t.Fatal("resp.Usage is nil")
	}
	if resp.Usage.PromptTokens != 26 {
		t.Fatalf("resp.Usage.PromptTokens = %d, want 26", resp.Usage.PromptTokens)
	}
	if resp.Usage.CompletionTokens != 3 {
		t.Fatalf("resp.Usage.CompletionTokens = %d, want 3", resp.Usage.CompletionTokens)
	}
	if resp.Usage.CacheCreationInputTokens != 5 || resp.Usage.CacheReadInputTokens != 13 {
		t.Fatalf("resp.Usage cache fields = %+v", resp.Usage)
	}
}

func TestAnthropicClientChatStreamToolCallUsesCanonicalName(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintln(w, `event: content_block_start`)
		fmt.Fprintln(w, `data: {"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"call-1","name":"fs_x2E_read"}}`)
		fmt.Fprintln(w)
		fmt.Fprintln(w, `event: content_block_delta`)
		fmt.Fprintln(w, `data: {"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"{\"path\":\"main.go\"}"}}`)
		fmt.Fprintln(w)
		fmt.Fprintln(w, `event: message_stop`)
		fmt.Fprintln(w, `data: {"type":"message_stop"}`)
	}))
	defer server.Close()

	client, err := NewAnthropicClient(AnthropicConfig{
		BaseURL: server.URL,
		APIKey:  "test-key",
	})
	if err != nil {
		t.Fatalf("NewAnthropicClient() error = %v", err)
	}

	var started []string
	cb := &testStreamCallback{
		onToolCallStart: func(_ context.Context, _, toolName string) {
			started = append(started, toolName)
		},
	}
	resp, err := client.ChatStream(context.Background(), agent.ChatRequest{
		Model: "claude-test",
		Tools: []agent.ToolDefinition{{Name: "fs.read"}},
		Messages: []contextengine.Message{{
			Role:    contextengine.RoleUser,
			Content: "read main.go",
		}},
	}, cb)
	if err != nil {
		t.Fatalf("ChatStream() error = %v", err)
	}
	if len(started) != 1 || started[0] != "fs.read" {
		t.Fatalf("started tool names = %#v", started)
	}
	if len(resp.ToolCalls) != 1 || resp.ToolCalls[0].Name != "fs.read" {
		t.Fatalf("resp.ToolCalls = %#v", resp.ToolCalls)
	}
}

// ---------------------------------------------------------------------------
// Error decoding
// ---------------------------------------------------------------------------

func TestDecodeAnthropicError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		body       string
		status     int
		wantSubstr string
	}{
		{
			name:       "structured error",
			body:       `{"type":"error","error":{"type":"invalid_request_error","message":"bad model"}}`,
			status:     400,
			wantSubstr: "bad model",
		},
		{
			name:       "plain text error",
			body:       `gateway timeout`,
			status:     504,
			wantSubstr: "gateway timeout",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := decodeAnthropicError(strings.NewReader(tt.body), tt.status)
			if err == nil {
				t.Fatal("expected error")
			}
			if !strings.Contains(err.Error(), tt.wantSubstr) {
				t.Fatalf("error = %v, want substring %q", err, tt.wantSubstr)
			}
		})
	}
}

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

func TestNewGoogleClientRequiresAPIKey(t *testing.T) {
	t.Parallel()

	_, err := NewGoogleClient(GoogleConfig{})
	if err == nil {
		t.Fatal("expected error when api_key is empty")
	}
	if !strings.Contains(err.Error(), "api_key is required") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestNewGoogleClientDefaultBaseURL(t *testing.T) {
	t.Parallel()

	client, err := NewGoogleClient(GoogleConfig{APIKey: "test-key"})
	if err != nil {
		t.Fatalf("NewGoogleClient() error = %v", err)
	}
	if client.baseURL != "https://generativelanguage.googleapis.com" {
		t.Fatalf("baseURL = %q", client.baseURL)
	}
}

// ---------------------------------------------------------------------------
// Chat tests
// ---------------------------------------------------------------------------

func TestGoogleClientChatTextResponse(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("expected POST, got %s", r.Method)
		}
		if !strings.Contains(r.URL.Path, "gemini-test:generateContent") {
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
		if got := r.Header.Get("x-goog-api-key"); got != "test-key" {
			t.Fatalf("unexpected x-goog-api-key header: %q", got)
		}

		var req googleRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if req.SystemInstruction == nil || req.SystemInstruction.Parts[0].Text != "be helpful" {
			t.Fatalf("system instruction = %+v", req.SystemInstruction)
		}
		if req.GenerationConfig == nil {
			t.Fatal("generation config is nil")
		}
		if req.GenerationConfig.Temperature != 0.33 {
			t.Fatalf("temperature = %v, want 0.33", req.GenerationConfig.Temperature)
		}

		resp := googleResponse{
			Candidates: []googleCandidate{
				{
					Content: googleContent{
						Role: "model",
						Parts: []googlePart{
							{Text: "hello from gemini"},
						},
					},
					FinishReason: "STOP",
				},
			},
			UsageMetadata: &googleUsageMetadata{
				PromptTokenCount:     12,
				CandidatesTokenCount: 4,
				TotalTokenCount:      16,
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client, err := NewGoogleClient(GoogleConfig{
		BaseURL: server.URL,
		APIKey:  "test-key",
	})
	if err != nil {
		t.Fatalf("NewGoogleClient() error = %v", err)
	}

	resp, err := client.Chat(context.Background(), agent.ChatRequest{
		Model:        "gemini-test",
		SystemPrompt: "be helpful",
		Temperature:  0.33,
		Messages: []contextengine.Message{
			{Role: contextengine.RoleUser, Content: "hello"},
		},
	})
	if err != nil {
		t.Fatalf("Chat() error = %v", err)
	}
	if resp.Message.Content != "hello from gemini" {
		t.Fatalf("resp.Message.Content = %q", resp.Message.Content)
	}
	if resp.Usage == nil {
		t.Fatal("resp.Usage is nil")
	}
	if resp.Usage.PromptTokens != 12 || resp.Usage.CompletionTokens != 4 || resp.Usage.TotalTokens != 16 {
		t.Fatalf("resp.Usage = %+v", resp.Usage)
	}
}

func TestGoogleClientChatRequestHookReceivesMetadata(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("X-Hook-Meta"); got != "google-generative-ai|models.generate_content|gemini-test|false|1" {
			t.Fatalf("X-Hook-Meta = %q", got)
		}

		resp := googleResponse{
			Candidates: []googleCandidate{{
				Content: googleContent{
					Role:  "model",
					Parts: []googlePart{{Text: "hooked"}},
				},
			}},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client, err := NewGoogleClient(GoogleConfig{
		BaseURL: server.URL,
		APIKey:  "test-key",
		RequestHooks: []ProviderRequestHook{{
			BeforeRequest: func(_ context.Context, meta ProviderRequestMetadata, req *http.Request) error {
				req.Header.Set("X-Hook-Meta", fmt.Sprintf("%s|%s|%s|%t|%d", meta.API, meta.Operation, meta.Model, meta.Streaming, meta.Attempt))
				return nil
			},
		}},
	})
	if err != nil {
		t.Fatalf("NewGoogleClient() error = %v", err)
	}

	resp, err := client.Chat(context.Background(), agent.ChatRequest{
		Model:    "gemini-test",
		Messages: []contextengine.Message{{Role: contextengine.RoleUser, Content: "hello"}},
	})
	if err != nil {
		t.Fatalf("Chat() error = %v", err)
	}
	if resp.Message.Content != "hooked" {
		t.Fatalf("resp.Message.Content = %q, want hooked", resp.Message.Content)
	}
}

func TestGoogleClientChatToolCall(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req googleRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if len(req.Tools) == 0 || len(req.Tools[0].FunctionDeclarations) == 0 {
			t.Fatalf("no tools in request")
		}
		if req.Tools[0].FunctionDeclarations[0].Name != "fs_x2E_read" {
			t.Fatalf("tool name = %q, want fs_x2E_read", req.Tools[0].FunctionDeclarations[0].Name)
		}

		resp := googleResponse{
			Candidates: []googleCandidate{
				{
					Content: googleContent{
						Role: "model",
						Parts: []googlePart{
							{
								FunctionCall: &googleFunctionCall{
									Name: "fs_x2E_read",
									Args: map[string]any{"path": "main.go"},
								},
							},
						},
					},
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client, err := NewGoogleClient(GoogleConfig{
		BaseURL: server.URL,
		APIKey:  "test-key",
	})
	if err != nil {
		t.Fatalf("NewGoogleClient() error = %v", err)
	}

	resp, err := client.Chat(context.Background(), agent.ChatRequest{
		Model: "gemini-test",
		Tools: []agent.ToolDefinition{{
			Name:        "fs.read",
			Description: "Read a file",
		}},
		Messages: []contextengine.Message{
			{Role: contextengine.RoleUser, Content: "read main.go"},
		},
	})
	if err != nil {
		t.Fatalf("Chat() error = %v", err)
	}
	if len(resp.ToolCalls) != 1 {
		t.Fatalf("len(resp.ToolCalls) = %d", len(resp.ToolCalls))
	}
	if resp.ToolCalls[0].Name != "fs.read" {
		t.Fatalf("resp.ToolCalls[0].Name = %q, want fs.read", resp.ToolCalls[0].Name)
	}
	if resp.ToolCalls[0].Input["path"] != "main.go" {
		t.Fatalf("resp.ToolCalls[0].Input = %v", resp.ToolCalls[0].Input)
	}
}

func TestGoogleClientChatRetriesTransientHTTPFailure(t *testing.T) {
	t.Parallel()

	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts == 1 {
			w.WriteHeader(http.StatusBadGateway)
			w.Write([]byte(`{"error":{"code":502,"status":"UNAVAILABLE","message":"upstream unavailable"}}`))
			return
		}

		resp := googleResponse{
			Candidates: []googleCandidate{{
				Content: googleContent{
					Role:  "model",
					Parts: []googlePart{{Text: "recovered"}},
				},
			}},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client, err := NewGoogleClient(GoogleConfig{
		BaseURL: server.URL,
		APIKey:  "test-key",
	})
	if err != nil {
		t.Fatalf("NewGoogleClient() error = %v", err)
	}

	resp, err := client.Chat(context.Background(), agent.ChatRequest{
		Model:    "gemini-test",
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

func TestGoogleClientChatStreamTextResponse(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "gemini-test:streamGenerateContent") {
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
		if got := r.URL.Query().Get("alt"); got != "sse" {
			t.Fatalf("alt = %q, want sse", got)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"candidates\":[{\"content\":{\"role\":\"model\",\"parts\":[{\"text\":\"hello \"}]}}]}\n\n")
		fmt.Fprint(w, "data: {\"candidates\":[{\"content\":{\"role\":\"model\",\"parts\":[{\"text\":\"world\"}]}}],\"usageMetadata\":{\"promptTokenCount\":7,\"candidatesTokenCount\":2,\"totalTokenCount\":9}}\n\n")
	}))
	defer server.Close()

	client, err := NewGoogleClient(GoogleConfig{
		BaseURL: server.URL,
		APIKey:  "test-key",
	})
	if err != nil {
		t.Fatalf("NewGoogleClient() error = %v", err)
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
		Model: "gemini-test",
		Messages: []contextengine.Message{{
			Role:    contextengine.RoleUser,
			Content: "hello",
		}},
	}, cb)
	if err != nil {
		t.Fatalf("ChatStream() error = %v", err)
	}
	if resp.Message.Content != "hello world" {
		t.Fatalf("resp.Message.Content = %q, want hello world", resp.Message.Content)
	}
	if len(deltas) != 2 || deltas[0] != "hello " || deltas[1] != "world" {
		t.Fatalf("stream deltas = %#v", deltas)
	}
	if resp.Usage == nil || resp.Usage.TotalTokens != 9 {
		t.Fatalf("resp.Usage = %#v", resp.Usage)
	}
	if !completed {
		t.Fatal("expected completion callback")
	}
}

func TestGoogleClientChatStreamToolCallUsesCanonicalNameAndStableID(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"candidates\":[{\"content\":{\"role\":\"model\",\"parts\":[{\"functionCall\":{\"name\":\"fs_x2E_read\",\"args\":{\"path\":\"main.go\"}}}]}}]}\n\n")
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer server.Close()

	client, err := NewGoogleClient(GoogleConfig{
		BaseURL: server.URL,
		APIKey:  "test-key",
	})
	if err != nil {
		t.Fatalf("NewGoogleClient() error = %v", err)
	}

	var startedIDs []string
	var startedNames []string
	var deltas []string
	cb := &testStreamCallback{
		onToolCallStart: func(_ context.Context, toolCallID, toolName string) {
			startedIDs = append(startedIDs, toolCallID)
			startedNames = append(startedNames, toolName)
		},
		onToolCallDelta: func(_ context.Context, _ string, delta string) {
			deltas = append(deltas, delta)
		},
	}

	resp, err := client.ChatStream(context.Background(), agent.ChatRequest{
		Model: "gemini-test",
		Tools: []agent.ToolDefinition{{Name: "fs.read"}},
		Messages: []contextengine.Message{{
			Role:    contextengine.RoleUser,
			Content: "read main.go",
		}},
	}, cb)
	if err != nil {
		t.Fatalf("ChatStream() error = %v", err)
	}
	if len(resp.ToolCalls) != 1 {
		t.Fatalf("len(resp.ToolCalls) = %d", len(resp.ToolCalls))
	}
	if resp.ToolCalls[0].ID != "google-call-1" {
		t.Fatalf("resp.ToolCalls[0].ID = %q, want google-call-1", resp.ToolCalls[0].ID)
	}
	if resp.ToolCalls[0].Name != "fs.read" {
		t.Fatalf("resp.ToolCalls[0].Name = %q, want fs.read", resp.ToolCalls[0].Name)
	}
	if len(startedIDs) != 1 || startedIDs[0] != "google-call-1" {
		t.Fatalf("startedIDs = %#v", startedIDs)
	}
	if len(startedNames) != 1 || startedNames[0] != "fs.read" {
		t.Fatalf("startedNames = %#v", startedNames)
	}
	if len(deltas) != 1 || deltas[0] != "{\"path\":\"main.go\"}" {
		t.Fatalf("tool deltas = %#v", deltas)
	}
}

func TestToGoogleContentsPreservesMalformedToolArguments(t *testing.T) {
	t.Parallel()

	wireToInternal := make(map[string]string)
	contents := toGoogleContents([]contextengine.Message{{
		Role: contextengine.RoleAssistant,
		ToolCalls: []contextengine.ToolCallRef{{
			ID:        "call-1",
			Name:      "fs.read",
			Arguments: "{\"path\":",
		}},
	}}, wireToInternal)
	if len(contents) != 1 || len(contents[0].Parts) != 1 || contents[0].Parts[0].FunctionCall == nil {
		t.Fatalf("unexpected contents = %#v", contents)
	}
	args := contents[0].Parts[0].FunctionCall.Args
	if args["_raw_arguments"] != "{\"path\":" {
		t.Fatalf("raw arguments = %#v", args["_raw_arguments"])
	}
	if args["_parse_error"] == "" {
		t.Fatalf("parse error marker missing from %#v", args)
	}
}

func TestFromGoogleResponseFallsBackToDesanitizedToolName(t *testing.T) {
	t.Parallel()

	resp, err := fromGoogleResponse(googleResponse{
		Candidates: []googleCandidate{{
			Content: googleContent{
				Role: "model",
				Parts: []googlePart{{
					FunctionCall: &googleFunctionCall{
						Name: "fs_x2E_read",
						Args: map[string]any{"path": "main.go"},
					},
				}},
			},
		}},
	}, map[string]string{})
	if err != nil {
		t.Fatalf("fromGoogleResponse() error = %v", err)
	}
	if len(resp.ToolCalls) != 1 {
		t.Fatalf("len(resp.ToolCalls) = %d", len(resp.ToolCalls))
	}
	if resp.ToolCalls[0].Name != "fs.read" {
		t.Fatalf("resp.ToolCalls[0].Name = %q, want fs.read", resp.ToolCalls[0].Name)
	}
}

func TestGoogleClientChatModelRequired(t *testing.T) {
	t.Parallel()

	client, err := NewGoogleClient(GoogleConfig{APIKey: "test-key"})
	if err != nil {
		t.Fatalf("NewGoogleClient() error = %v", err)
	}

	_, err = client.Chat(context.Background(), agent.ChatRequest{})
	if err == nil {
		t.Fatal("expected error when model is empty")
	}
}

func TestGoogleClientChatAPIError(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte(`{"error":{"message":"API key not valid","code":403,"status":"PERMISSION_DENIED"}}`))
	}))
	defer server.Close()

	client, err := NewGoogleClient(GoogleConfig{
		BaseURL: server.URL,
		APIKey:  "bad-key",
	})
	if err != nil {
		t.Fatalf("NewGoogleClient() error = %v", err)
	}

	_, err = client.Chat(context.Background(), agent.ChatRequest{
		Model:    "gemini-test",
		Messages: []contextengine.Message{{Role: contextengine.RoleUser, Content: "hi"}},
	})
	if err == nil {
		t.Fatal("expected error on 403 response")
	}
	if !strings.Contains(err.Error(), "API key not valid") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestGoogleClientChatEmptyCandidates(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := googleResponse{Candidates: nil}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client, err := NewGoogleClient(GoogleConfig{
		BaseURL: server.URL,
		APIKey:  "test-key",
	})
	if err != nil {
		t.Fatalf("NewGoogleClient() error = %v", err)
	}

	resp, err := client.Chat(context.Background(), agent.ChatRequest{
		Model:    "gemini-test",
		Messages: []contextengine.Message{{Role: contextengine.RoleUser, Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("Chat() error = %v", err)
	}
	if resp.Message.Content != "" {
		t.Fatalf("expected empty content, got %q", resp.Message.Content)
	}
}

// ---------------------------------------------------------------------------
// cleanGoogleSchema tests
// ---------------------------------------------------------------------------

func TestCleanGoogleSchema(t *testing.T) {
	t.Parallel()

	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"name": map[string]any{
				"type":      "string",
				"minLength": 1,
				"pattern":   "^[a-z]+$",
			},
		},
		"additionalProperties": false,
		"$ref":                 "#/defs/Foo",
	}

	cleaned := cleanGoogleSchema(schema)

	// Unsupported keys should be stripped.
	if _, ok := cleaned["additionalProperties"]; ok {
		t.Fatal("additionalProperties should be stripped")
	}
	if _, ok := cleaned["$ref"]; ok {
		t.Fatal("$ref should be stripped")
	}
	// Supported keys should remain.
	if _, ok := cleaned["type"]; !ok {
		t.Fatal("type should remain")
	}
	// Nested unsupported keys should be stripped recursively.
	props, _ := cleaned["properties"].(map[string]any)
	nameProps, _ := props["name"].(map[string]any)
	if _, ok := nameProps["minLength"]; ok {
		t.Fatal("nested minLength should be stripped")
	}
	if _, ok := nameProps["pattern"]; ok {
		t.Fatal("nested pattern should be stripped")
	}
	if _, ok := nameProps["type"]; !ok {
		t.Fatal("nested type should remain")
	}
}

func TestCleanGoogleSchemaNil(t *testing.T) {
	t.Parallel()

	if result := cleanGoogleSchema(nil); result != nil {
		t.Fatalf("cleanGoogleSchema(nil) = %v, want nil", result)
	}
}

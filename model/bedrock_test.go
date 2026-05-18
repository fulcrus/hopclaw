package model

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/fulcrus/hopclaw/agent"
	"github.com/fulcrus/hopclaw/contextengine"
	"github.com/fulcrus/hopclaw/internal/support/normalize"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

// ---------------------------------------------------------------------------
// Constructor tests
// ---------------------------------------------------------------------------

func TestNewBedrockClientRequiresRegion(t *testing.T) {
	// Uses t.Setenv — cannot be parallel.
	t.Setenv("AWS_REGION", "")
	t.Setenv("AWS_DEFAULT_REGION", "")

	_, err := NewBedrockClient(BedrockConfig{
		AccessKeyID: "AKID",
		SecretKey:   "secret",
	})
	if err == nil {
		t.Fatal("expected error when region is empty")
	}
	if !strings.Contains(err.Error(), "region is required") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestNewBedrockClientRequiresCredentials(t *testing.T) {
	// Uses t.Setenv — cannot be parallel.
	t.Setenv("AWS_ACCESS_KEY_ID", "")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "")

	_, err := NewBedrockClient(BedrockConfig{
		Region: "us-east-1",
	})
	if err == nil {
		t.Fatal("expected error when credentials are empty")
	}
	if !strings.Contains(err.Error(), "credentials are required") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestNewBedrockClientSuccess(t *testing.T) {
	t.Parallel()

	client, err := NewBedrockClient(BedrockConfig{
		Region:      "us-west-2",
		AccessKeyID: "AKID",
		SecretKey:   "secret",
	})
	if err != nil {
		t.Fatalf("NewBedrockClient() error = %v", err)
	}
	if client.region != "us-west-2" {
		t.Fatalf("region = %q", client.region)
	}
}

// ---------------------------------------------------------------------------
// Chat tests
// ---------------------------------------------------------------------------

func TestFromBedrockResponseText(t *testing.T) {
	t.Parallel()

	resp := bedrockResponse{
		Output: bedrockOutput{
			Message: bedrockMessage{
				Role: "assistant",
				Content: []bedrockContentBlock{
					{Text: "hello from bedrock"},
				},
			},
		},
		Usage: bedrockUsage{
			InputTokens:  15,
			OutputTokens: 6,
		},
	}

	result := fromBedrockResponse(resp, make(map[string]string))
	if result.Message.Content != "hello from bedrock" {
		t.Fatalf("result.Message.Content = %q", result.Message.Content)
	}
	if result.Usage == nil {
		t.Fatal("result.Usage is nil")
	}
	if result.Usage.PromptTokens != 15 || result.Usage.CompletionTokens != 6 || result.Usage.TotalTokens != 21 {
		t.Fatalf("result.Usage = %+v", result.Usage)
	}
}

func TestBedrockClientChatToolCallResponse(t *testing.T) {
	t.Parallel()

	wireToInternal := map[string]string{
		"fs_x2E_read": "fs.read",
	}

	resp := bedrockResponse{
		Output: bedrockOutput{
			Message: bedrockMessage{
				Role: "assistant",
				Content: []bedrockContentBlock{
					{
						ToolUse: &bedrockToolUse{
							ToolUseID: "call-1",
							Name:      "fs_x2E_read",
							Input:     map[string]any{"path": "README.md"},
						},
					},
				},
			},
		},
	}

	result := fromBedrockResponse(resp, wireToInternal)
	if len(result.ToolCalls) != 1 {
		t.Fatalf("len(result.ToolCalls) = %d", len(result.ToolCalls))
	}
	if result.ToolCalls[0].Name != "fs.read" {
		t.Fatalf("result.ToolCalls[0].Name = %q, want fs.read", result.ToolCalls[0].Name)
	}
	if result.ToolCalls[0].Input["path"] != "README.md" {
		t.Fatalf("result.ToolCalls[0].Input = %v", result.ToolCalls[0].Input)
	}
}

func TestFromBedrockResponseFallsBackToDesanitizedToolName(t *testing.T) {
	t.Parallel()

	result := fromBedrockResponse(bedrockResponse{
		Output: bedrockOutput{
			Message: bedrockMessage{
				Role: "assistant",
				Content: []bedrockContentBlock{{
					ToolUse: &bedrockToolUse{
						ToolUseID: "call-1",
						Name:      "fs_x2E_read",
						Input:     map[string]any{"path": "README.md"},
					},
				}},
			},
		},
	}, map[string]string{})
	if len(result.ToolCalls) != 1 {
		t.Fatalf("len(result.ToolCalls) = %d", len(result.ToolCalls))
	}
	if result.ToolCalls[0].Name != "fs.read" {
		t.Fatalf("result.ToolCalls[0].Name = %q, want fs.read", result.ToolCalls[0].Name)
	}
}

func TestBedrockClientChatSendsTemperature(t *testing.T) {
	t.Parallel()

	client := &BedrockClient{
		region:       "us-east-1",
		accessKeyID:  "AKID",
		secretKey:    "secret",
		defaultModel: "anthropic.claude-bedrock",
		httpClient: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			body, err := io.ReadAll(req.Body)
			if err != nil {
				t.Fatalf("read body: %v", err)
			}
			var payload bedrockRequest
			if err := json.Unmarshal(body, &payload); err != nil {
				t.Fatalf("decode body: %v", err)
			}
			if payload.InferenceConfig == nil {
				t.Fatal("inference config is nil")
			}
			if payload.InferenceConfig.Temperature != 0.27 {
				t.Fatalf("temperature = %v, want 0.27", payload.InferenceConfig.Temperature)
			}
			respBody := `{"output":{"message":{"role":"assistant","content":[{"text":"ok"}]}},"usage":{"inputTokens":1,"outputTokens":1},"stopReason":"end_turn"}`
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader(respBody)),
			}, nil
		})},
	}

	resp, err := client.Chat(context.Background(), agent.ChatRequest{
		Model:       "anthropic.claude-bedrock",
		Temperature: 0.27,
		Messages: []contextengine.Message{
			{Role: contextengine.RoleUser, Content: "hello"},
		},
	})
	if err != nil {
		t.Fatalf("Chat() error = %v", err)
	}
	if resp.Message.Content != "ok" {
		t.Fatalf("resp.Message.Content = %q", resp.Message.Content)
	}
}

// ---------------------------------------------------------------------------
// SigV4 signing tests
// ---------------------------------------------------------------------------

func TestDeriveSigningKey(t *testing.T) {
	t.Parallel()

	key := deriveSigningKey("wJalrXUtnFEMI", "20230101", "us-east-1", "bedrock-runtime")
	if len(key) == 0 {
		t.Fatal("deriveSigningKey returned empty key")
	}
	// The key should be deterministic.
	key2 := deriveSigningKey("wJalrXUtnFEMI", "20230101", "us-east-1", "bedrock-runtime")
	if string(key) != string(key2) {
		t.Fatal("deriveSigningKey is not deterministic")
	}
}

func TestSha256Hex(t *testing.T) {
	t.Parallel()

	got := sha256Hex([]byte("hello"))
	want := "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824"
	if got != want {
		t.Fatalf("sha256Hex(hello) = %q, want %q", got, want)
	}
}

func TestSignRequestSetsHeaders(t *testing.T) {
	t.Parallel()

	client := &BedrockClient{
		region:      "us-east-1",
		accessKeyID: "AKIAIOSFODNN7EXAMPLE",
		secretKey:   "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
	}

	req, err := http.NewRequest(http.MethodPost, "https://bedrock-runtime.us-east-1.amazonaws.com/model/test/converse", strings.NewReader("{}"))
	if err != nil {
		t.Fatalf("NewRequest error = %v", err)
	}
	req.Header.Set("Content-Type", "application/json")

	if err := client.signRequest(req, []byte("{}")); err != nil {
		t.Fatalf("signRequest error = %v", err)
	}

	// Verify SigV4 headers are set.
	if req.Header.Get("Authorization") == "" {
		t.Fatal("missing Authorization header")
	}
	if !strings.HasPrefix(req.Header.Get("Authorization"), awsSigV4Algo) {
		t.Fatalf("Authorization header does not start with %s: %q", awsSigV4Algo, req.Header.Get("Authorization"))
	}
	if req.Header.Get("X-Amz-Date") == "" {
		t.Fatal("missing X-Amz-Date header")
	}
	if req.Header.Get("X-Amz-Content-Sha256") == "" {
		t.Fatal("missing X-Amz-Content-Sha256 header")
	}
}

func TestSignRequestSessionToken(t *testing.T) {
	t.Parallel()

	client := &BedrockClient{
		region:       "us-east-1",
		accessKeyID:  "AKID",
		secretKey:    "secret",
		sessionToken: "my-session-token",
	}

	req, err := http.NewRequest(http.MethodPost, "https://bedrock-runtime.us-east-1.amazonaws.com/model/test/converse", strings.NewReader("{}"))
	if err != nil {
		t.Fatalf("NewRequest error = %v", err)
	}
	req.Header.Set("Content-Type", "application/json")

	if err := client.signRequest(req, []byte("{}")); err != nil {
		t.Fatalf("signRequest error = %v", err)
	}

	if req.Header.Get("X-Amz-Security-Token") != "my-session-token" {
		t.Fatalf("X-Amz-Security-Token = %q, want my-session-token", req.Header.Get("X-Amz-Security-Token"))
	}
}

// ---------------------------------------------------------------------------
// Message conversion tests
// ---------------------------------------------------------------------------

func TestToBedrockMessages(t *testing.T) {
	t.Parallel()

	wireToInternal := make(map[string]string)
	msgs := []contextengine.Message{
		{Role: contextengine.RoleUser, Content: "hello"},
		{Role: contextengine.RoleAssistant, Content: "hi there"},
		{Role: contextengine.RoleUser, Content: "follow up"},
	}

	result := toBedrockMessages(msgs, wireToInternal)
	if len(result) != 3 {
		t.Fatalf("len(result) = %d, want 3", len(result))
	}
	if result[0].Role != "user" || result[0].Content[0].Text != "hello" {
		t.Fatalf("result[0] = %+v", result[0])
	}
	if result[1].Role != "assistant" || result[1].Content[0].Text != "hi there" {
		t.Fatalf("result[1] = %+v", result[1])
	}
}

func TestToBedrockMessagesMergesConsecutiveRoles(t *testing.T) {
	t.Parallel()

	wireToInternal := make(map[string]string)
	// Two consecutive tool results should merge into one user message.
	msgs := []contextengine.Message{
		{Role: contextengine.RoleTool, Content: "result1", ToolCallID: "tc1", Name: "tool1"},
		{Role: contextengine.RoleTool, Content: "result2", ToolCallID: "tc2", Name: "tool2"},
	}

	result := toBedrockMessages(msgs, wireToInternal)
	if len(result) != 1 {
		t.Fatalf("len(result) = %d, want 1 (merged user message)", len(result))
	}
	if result[0].Role != "user" {
		t.Fatalf("result[0].Role = %q, want user", result[0].Role)
	}
	if len(result[0].Content) != 2 {
		t.Fatalf("len(result[0].Content) = %d, want 2", len(result[0].Content))
	}
}

// ---------------------------------------------------------------------------
// Helper tests
// ---------------------------------------------------------------------------

func TestFirstNonEmpty(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		values []string
		want   string
	}{
		{"first non-empty", []string{"", "b", "c"}, "b"},
		{"all empty", []string{"", " ", ""}, ""},
		{"first is set", []string{"a", "b"}, "a"},
		{"whitespace trimmed", []string{" hello "}, "hello"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := normalize.FirstNonEmpty(tt.values...)
			if got != tt.want {
				t.Fatalf("normalize.FirstNonEmpty(%v) = %q, want %q", tt.values, got, tt.want)
			}
		})
	}
}

func TestBedrockImageFormat(t *testing.T) {
	t.Parallel()

	tests := []struct {
		mediaType string
		want      string
	}{
		{"image/jpeg", "jpeg"},
		{"image/png", "png"},
		{"image/gif", "gif"},
		{"image/webp", "webp"},
		{"image/bmp", "bmp"},
		{"text/plain", "text/plain"},
	}

	for _, tt := range tests {
		t.Run(tt.mediaType, func(t *testing.T) {
			t.Parallel()
			got := bedrockImageFormat(tt.mediaType)
			if got != tt.want {
				t.Fatalf("bedrockImageFormat(%q) = %q, want %q", tt.mediaType, got, tt.want)
			}
		})
	}
}

func TestDecodeBedrockError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		body       string
		status     int
		wantSubstr string
	}{
		{
			name:       "structured",
			body:       `{"message":"Model not found"}`,
			status:     404,
			wantSubstr: "Model not found",
		},
		{
			name:       "plain text",
			body:       `service unavailable`,
			status:     503,
			wantSubstr: "service unavailable",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := decodeBedrockError(strings.NewReader(tt.body), tt.status)
			if err == nil {
				t.Fatal("expected error")
			}
			if !strings.Contains(err.Error(), tt.wantSubstr) {
				t.Fatalf("error = %v, want substring %q", err, tt.wantSubstr)
			}
		})
	}
}

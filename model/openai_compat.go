package model

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"

	"github.com/fulcrus/hopclaw/agent"
	"github.com/fulcrus/hopclaw/contextengine"
	"github.com/fulcrus/hopclaw/internal/jsonrepair"
	supportmaps "github.com/fulcrus/hopclaw/internal/support/maps"
)

type OpenAICompatConfig struct {
	BaseURL      string
	APIKey       string
	DefaultModel string
	Timeout      time.Duration
	Headers      map[string]string
	RequestHooks []ProviderRequestHook
}

type OpenAICompatClient struct {
	baseURL      string
	apiKey       string
	defaultModel string
	headers      map[string]string
	httpClient   *http.Client
	requestHooks []ProviderRequestHook
}

func NewOpenAICompatClient(cfg OpenAICompatConfig) (*OpenAICompatClient, error) {
	if strings.TrimSpace(cfg.BaseURL) == "" {
		return nil, fmt.Errorf("base url is required")
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 60 * time.Second
	}
	return &OpenAICompatClient{
		baseURL:      strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/"),
		apiKey:       strings.TrimSpace(cfg.APIKey),
		defaultModel: strings.TrimSpace(cfg.DefaultModel),
		headers:      cloneHeaders(cfg.Headers),
		httpClient:   newStreamingHTTPClient(cfg.Timeout),
		requestHooks: cloneProviderRequestHooks(cfg.RequestHooks),
	}, nil
}

func (c *OpenAICompatClient) Chat(ctx context.Context, req agent.ChatRequest) (*agent.ModelResponse, error) {
	payload, urlValue, wireToInternal, err := c.buildChatPayload(req)
	if err != nil {
		return nil, err
	}
	return executeProviderRequest(ctx, newProviderJSONRequestOptions(
		c.httpClient,
		ProviderRequestMetadata{
			API:       APIOpenAICompletions,
			Operation: "chat.completions",
			Method:    http.MethodPost,
			Model:     defaultString(req.Model, c.defaultModel),
			Endpoint:  urlValue,
			Streaming: true,
		},
		c.requestHooks,
		http.MethodPost,
		urlValue,
		payload,
		c.chatRequestHeaders(),
		decodeOpenAIError,
		func(body io.Reader) (*agent.ModelResponse, error) {
			return consumeSSEStream(ctx, body, wireToInternal, nil)
		},
	))
}

// ChatStream implements StreamingModelClient by streaming the response and
// invoking the callback as chunks arrive. The final accumulated ModelResponse
// is returned exactly as Chat would.
func (c *OpenAICompatClient) ChatStream(ctx context.Context, req agent.ChatRequest, cb agent.StreamCallback) (*agent.ModelResponse, error) {
	payload, urlValue, wireToInternal, err := c.buildChatPayload(req)
	if err != nil {
		if cb != nil {
			cb.OnError(ctx, err)
		}
		return nil, err
	}
	result, err := executeProviderRequest(ctx, newProviderJSONRequestOptions(
		c.httpClient,
		ProviderRequestMetadata{
			API:       APIOpenAICompletions,
			Operation: "chat.completions",
			Method:    http.MethodPost,
			Model:     defaultString(req.Model, c.defaultModel),
			Endpoint:  urlValue,
			Streaming: true,
		},
		c.requestHooks,
		http.MethodPost,
		urlValue,
		payload,
		c.chatRequestHeaders(),
		decodeOpenAIError,
		func(body io.Reader) (*agent.ModelResponse, error) {
			return consumeSSEStream(ctx, body, wireToInternal, cb)
		},
	))
	if err != nil && cb != nil {
		cb.OnError(ctx, err)
	}
	return result, err
}

func (c *OpenAICompatClient) buildChatPayload(req agent.ChatRequest) ([]byte, string, map[string]string, error) {
	body := openAIChatRequest{
		Model:       defaultString(req.Model, c.defaultModel),
		Messages:    make([]openAIMessage, 0, len(req.Messages)+1),
		MaxTokens:   req.Budget.ReservedOutput,
		Temperature: req.Temperature,
		Stream:      true,
	}
	if body.Model == "" {
		return nil, "", nil, fmt.Errorf("chat model is required")
	}

	if strings.TrimSpace(req.SystemPrompt) != "" {
		body.Messages = append(body.Messages, openAIMessage{
			Role:    string(contextengine.RoleSystem),
			Content: req.SystemPrompt,
		})
	}
	for _, msg := range req.Messages {
		body.Messages = append(body.Messages, toOpenAIMessage(msg))
	}
	// Build a bidirectional map for tool name sanitization.
	// Many providers (DeepSeek, etc.) require ^[a-zA-Z0-9_-]+$ — no dots.
	wireToInternal := make(map[string]string) // sanitized name → original name
	if len(req.Tools) > 0 {
		body.Tools = make([]openAITool, 0, len(req.Tools))
		for _, tool := range req.Tools {
			wireName := sanitizeToolName(tool.Name)
			wireToInternal[wireName] = tool.Name
			body.Tools = append(body.Tools, openAITool{
				Type: "function",
				Function: openAIFunction{
					Name:        wireName,
					Description: tool.Description,
					Parameters:  normalizeOpenAIToolSchema(tool.InputSchema),
				},
			})
		}
		body.ToolChoice = "auto"
	}

	payload, err := json.Marshal(body)
	if err != nil {
		return nil, "", nil, err
	}
	urlValue, err := buildChatURL(c.baseURL)
	if err != nil {
		return nil, "", nil, err
	}
	return payload, urlValue, wireToInternal, nil
}

func (c *OpenAICompatClient) chatRequestHeaders() map[string]string {
	return buildProviderJSONHeaders(providerJSONHeadersOptions{
		Base:        c.headers,
		Accept:      "text/event-stream",
		BearerToken: c.apiKey,
	})
}

func normalizeOpenAIToolSchema(schema map[string]any) map[string]any {
	if schema == nil {
		return nil
	}
	normalized, ok := normalizeOpenAISchemaValue(schema).(map[string]any)
	if !ok {
		return supportmaps.Clone(schema)
	}
	return normalized
}

func normalizeOpenAISchemaValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		normalized := make(map[string]any, len(typed)+1)
		for key, item := range typed {
			normalized[key] = normalizeOpenAISchemaValue(item)
		}
		if schemaLooksLikeObject(normalized) {
			if properties, ok := normalized["properties"]; !ok || properties == nil {
				normalized["properties"] = map[string]any{}
			}
		}
		return normalized
	case []any:
		out := make([]any, len(typed))
		for i, item := range typed {
			out[i] = normalizeOpenAISchemaValue(item)
		}
		return out
	case []map[string]any:
		out := make([]any, len(typed))
		for i, item := range typed {
			out[i] = normalizeOpenAISchemaValue(item)
		}
		return out
	case []string:
		return append([]string(nil), typed...)
	default:
		return value
	}
}

func schemaLooksLikeObject(schema map[string]any) bool {
	if schema == nil {
		return false
	}
	switch typed := schema["type"].(type) {
	case string:
		if strings.EqualFold(strings.TrimSpace(typed), "object") {
			return true
		}
	case []any:
		for _, candidate := range typed {
			if name, ok := candidate.(string); ok && strings.EqualFold(strings.TrimSpace(name), "object") {
				return true
			}
		}
	case []string:
		for _, candidate := range typed {
			if strings.EqualFold(strings.TrimSpace(candidate), "object") {
				return true
			}
		}
	}
	_, hasProperties := schema["properties"]
	_, hasRequired := schema["required"]
	return hasProperties || hasRequired
}

// consumeSSEStream reads an SSE stream of OpenAI chat completion chunks
// and accumulates them into a single ModelResponse.
// If cb is non-nil, streaming callbacks are invoked as chunks arrive.
func consumeSSEStream(ctx context.Context, body io.Reader, wireToInternal map[string]string, cb agent.StreamCallback) (*agent.ModelResponse, error) {
	scanner := newSSEScanner(body)

	var contentParts []string
	// toolCalls indexed by tool call index from the stream.
	type toolCallAcc struct {
		ID   string
		Name string
		Args strings.Builder
	}
	toolCalls := make(map[int]*toolCallAcc)
	role := ""
	var lastUsage *openAIStreamUsage

	for {
		event, ok, err := nextSSEEvent(scanner)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
		if !ok {
			break
		}
		data := strings.TrimSpace(event.Data)
		if data == "" {
			continue
		}
		if data == "[DONE]" {
			break
		}
		var chunk openAIStreamChunk
		if err := jsonrepair.DecodeJSONObjectCandidate(data, &chunk); err != nil {
			continue // skip malformed chunks
		}
		if chunk.Usage != nil {
			lastUsage = chunk.Usage
		}
		if len(chunk.Choices) == 0 {
			continue
		}
		delta := chunk.Choices[0].Delta
		if delta.Role != "" {
			role = delta.Role
		}
		if delta.Content != "" {
			contentParts = append(contentParts, delta.Content)
			if cb != nil {
				cb.OnTextDelta(ctx, delta.Content)
			}
		}
		for _, tc := range delta.ToolCalls {
			acc, ok := toolCalls[tc.Index]
			if !ok {
				acc = &toolCallAcc{}
				toolCalls[tc.Index] = acc
			}
			if tc.ID != "" {
				acc.ID = tc.ID
			}
			if tc.Function.Name != "" {
				acc.Name = tc.Function.Name
				if cb != nil {
					cb.OnToolCallStart(ctx, acc.ID, restoreToolName(tc.Function.Name, wireToInternal))
				}
			}
			if tc.Function.Arguments != "" {
				acc.Args.WriteString(tc.Function.Arguments)
				if cb != nil {
					cb.OnToolCallDelta(ctx, acc.ID, tc.Function.Arguments)
				}
			}
		}
	}
	if cb != nil {
		cb.OnComplete(ctx)
	}

	// Build the final message from accumulated deltas.
	content := strings.Join(contentParts, "")
	finalMsg := openAIMessage{
		Role:    defaultString(role, string(contextengine.RoleAssistant)),
		Content: content,
	}
	if len(toolCalls) > 0 {
		// Sort by index.
		sorted := make([]openAIToolCall, 0, len(toolCalls))
		for i := 0; i < len(toolCalls); i++ {
			acc, ok := toolCalls[i]
			if !ok {
				break
			}
			sorted = append(sorted, openAIToolCall{
				ID:   acc.ID,
				Type: "function",
				Function: openAIFunctionCall{
					Name:      acc.Name,
					Arguments: acc.Args.String(),
				},
			})
		}
		finalMsg.ToolCalls = sorted
	}

	result, err := toModelResponse(finalMsg, wireToInternal)
	if err != nil {
		return nil, err
	}
	if lastUsage != nil {
		result.Usage = &agent.ModelUsageInfo{
			PromptTokens:     lastUsage.PromptTokens,
			CompletionTokens: lastUsage.CompletionTokens,
			TotalTokens:      lastUsage.TotalTokens,
		}
	}
	return result, nil
}

// openAIStreamChunk represents a single SSE chunk from the streaming API.
type openAIStreamChunk struct {
	Choices []struct {
		Delta struct {
			Role      string                 `json:"role,omitempty"`
			Content   string                 `json:"content,omitempty"`
			ToolCalls []openAIStreamToolCall `json:"tool_calls,omitempty"`
		} `json:"delta"`
		FinishReason *string `json:"finish_reason"`
	} `json:"choices"`
	Usage *openAIStreamUsage `json:"usage,omitempty"`
}

// openAIStreamUsage captures token usage from the final streaming chunk.
type openAIStreamUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

type openAIStreamToolCall struct {
	Index    int    `json:"index"`
	ID       string `json:"id,omitempty"`
	Type     string `json:"type,omitempty"`
	Function struct {
		Name      string `json:"name,omitempty"`
		Arguments string `json:"arguments,omitempty"`
	} `json:"function"`
}

type openAIChatRequest struct {
	Model       string          `json:"model"`
	Messages    []openAIMessage `json:"messages"`
	Tools       []openAITool    `json:"tools,omitempty"`
	ToolChoice  string          `json:"tool_choice,omitempty"`
	MaxTokens   int             `json:"max_tokens,omitempty"`
	Temperature float64         `json:"temperature,omitempty"`
	Stream      bool            `json:"stream,omitempty"`
}

type openAIMessage struct {
	Role       string           `json:"role"`
	Content    any              `json:"content"`
	Name       string           `json:"name,omitempty"`
	ToolCallID string           `json:"tool_call_id,omitempty"`
	ToolCalls  []openAIToolCall `json:"tool_calls,omitempty"`
}

type openAITool struct {
	Type     string         `json:"type"`
	Function openAIFunction `json:"function"`
}

type openAIFunction struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	Parameters  map[string]any `json:"parameters,omitempty"`
}

type openAIToolCall struct {
	ID       string             `json:"id"`
	Type     string             `json:"type"`
	Function openAIFunctionCall `json:"function"`
}

type openAIFunctionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// openAIContentPart represents a single part in a multimodal content array.
// Used when messages contain images alongside text.
type openAIContentPart struct {
	Type     string          `json:"type"`
	Text     string          `json:"text,omitempty"`
	ImageURL *openAIImageURL `json:"image_url,omitempty"`
}

// openAIImageURL holds the URL payload for an image content part.
// The URL may be a data URI (data:image/jpeg;base64,...) or an HTTP URL.
type openAIImageURL struct {
	URL string `json:"url"`
}

type openAIErrorEnvelope struct {
	Error struct {
		Message string `json:"message"`
		Type    string `json:"type"`
		Code    any    `json:"code"`
	} `json:"error"`
}

func toOpenAIMessage(msg contextengine.Message) openAIMessage {
	out := openAIMessage{
		Role: string(msg.Role),
		Name: msg.Name,
	}

	// Build multimodal content array when image blocks are present.
	if msg.HasImageContent() {
		out.Content = toOpenAIContentParts(msg.ContentBlocks)
	} else {
		out.Content = msg.TextContent()
	}

	if out.Content == nil {
		out.Content = ""
	}
	if msg.ToolCallID != "" {
		out.ToolCallID = msg.ToolCallID
	}
	// Replay assistant tool_call messages with sanitized names.
	if len(msg.ToolCalls) > 0 {
		out.ToolCalls = make([]openAIToolCall, len(msg.ToolCalls))
		for i, tc := range msg.ToolCalls {
			out.ToolCalls[i] = openAIToolCall{
				ID:   tc.ID,
				Type: "function",
				Function: openAIFunctionCall{
					Name:      sanitizeToolName(tc.Name),
					Arguments: tc.Arguments,
				},
			}
		}
	}
	return out
}

// toOpenAIContentParts converts contextengine.ContentBlock slices into the
// OpenAI multimodal content part format. Image blocks are encoded as data URIs.
func toOpenAIContentParts(blocks []contextengine.ContentBlock) []openAIContentPart {
	parts := make([]openAIContentPart, 0, len(blocks))
	for _, block := range blocks {
		switch block.Type {
		case contextengine.ContentBlockText:
			if block.Text != "" {
				parts = append(parts, openAIContentPart{
					Type: "text",
					Text: block.Text,
				})
			}
		case contextengine.ContentBlockImage:
			url := block.SourceURL
			if url == "" && block.Data != "" {
				// Build a data URI from the base64 data and media type.
				url = "data:" + block.MediaType + ";base64," + block.Data
			}
			if url != "" {
				parts = append(parts, openAIContentPart{
					Type:     "image_url",
					ImageURL: &openAIImageURL{URL: url},
				})
			}
		}
	}
	return parts
}

func toModelResponse(message openAIMessage, wireToInternal map[string]string) (*agent.ModelResponse, error) {
	response := &agent.ModelResponse{
		Message: contextengine.Message{
			Role:       contextengine.MessageRole(defaultString(message.Role, string(contextengine.RoleAssistant))),
			Content:    contentString(message.Content),
			Name:       message.Name,
			ToolCallID: message.ToolCallID,
		},
	}
	if len(message.ToolCalls) > 0 {
		response.Message.Content = ""
		response.ToolCalls = make([]agent.ToolCall, 0, len(message.ToolCalls))
		for _, call := range message.ToolCalls {
			input, err := parseArguments(call.Function.Arguments)
			if err != nil {
				// Mark the tool call with the parse error so the agent loop
				// can return it as a tool error instead of crashing the run.
				input = map[string]any{"_parse_error": err.Error()}
			}
			name := restoreToolName(call.Function.Name, wireToInternal)
			response.ToolCalls = append(response.ToolCalls, agent.ToolCall{
				ID:    call.ID,
				Name:  name,
				Input: input,
			})
		}
	}
	return response, nil
}

func contentString(v any) string {
	switch typed := v.(type) {
	case nil:
		return ""
	case string:
		return typed
	case []any:
		var parts []string
		for _, item := range typed {
			object, ok := item.(map[string]any)
			if !ok {
				continue
			}
			if text, ok := object["text"].(string); ok {
				parts = append(parts, text)
			}
		}
		return strings.Join(parts, "")
	default:
		return ""
	}
}

func buildChatURL(base string) (string, error) {
	u, err := url.Parse(base)
	if err != nil {
		return "", err
	}
	u.Path = path.Join(u.Path, "/chat/completions")
	return u.String(), nil
}

func decodeOpenAIError(body io.Reader, status int) error {
	data, _ := io.ReadAll(body)
	var envelope openAIErrorEnvelope
	if err := json.Unmarshal(data, &envelope); err == nil && envelope.Error.Message != "" {
		return providerAPIError("openai-compatible", status, normalizeOpenAIErrorCode(envelope.Error), envelope.Error.Message)
	}
	return providerAPIError("openai-compatible", status, "", strings.TrimSpace(string(data)))
}

func normalizeOpenAIErrorCode(envelope struct {
	Message string `json:"message"`
	Type    string `json:"type"`
	Code    any    `json:"code"`
}) string {
	if code := providerErrorCode(envelope.Code); code != "" {
		return code
	}
	return strings.TrimSpace(envelope.Type)
}

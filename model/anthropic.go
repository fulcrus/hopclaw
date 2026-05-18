package model

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/fulcrus/hopclaw/agent"
	"github.com/fulcrus/hopclaw/contextengine"
	"github.com/fulcrus/hopclaw/internal/jsonrepair"
)

const anthropicAPIVersion = "2023-06-01"

type AnthropicConfig struct {
	BaseURL      string
	APIKey       string
	DefaultModel string
	Timeout      time.Duration
	Headers      map[string]string
	RequestHooks []ProviderRequestHook
}

type AnthropicClient struct {
	baseURL      string
	apiKey       string
	defaultModel string
	httpClient   *http.Client
	headers      map[string]string
	requestHooks []ProviderRequestHook
	cachePrompts bool
}

func NewAnthropicClient(cfg AnthropicConfig) (*AnthropicClient, error) {
	if strings.TrimSpace(cfg.APIKey) == "" {
		return nil, fmt.Errorf("anthropic: api_key is required")
	}
	baseURL := strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	if baseURL == "" {
		baseURL = "https://api.anthropic.com"
	}
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = 120 * time.Second
	}
	return &AnthropicClient{
		baseURL:      baseURL,
		apiKey:       strings.TrimSpace(cfg.APIKey),
		defaultModel: strings.TrimSpace(cfg.DefaultModel),
		httpClient:   newStreamingHTTPClient(timeout),
		headers:      cloneHeaders(cfg.Headers),
		requestHooks: cloneProviderRequestHooks(cfg.RequestHooks),
		cachePrompts: shouldUseAnthropicPromptCaching(baseURL),
	}, nil
}

func (c *AnthropicClient) Chat(ctx context.Context, req agent.ChatRequest) (*agent.ModelResponse, error) {
	payload, endpoint, wireToInternal, err := c.buildChatPayload(req, false)
	if err != nil {
		return nil, err
	}
	return executeProviderRequest(ctx, newProviderJSONRequestOptions(
		c.httpClient,
		ProviderRequestMetadata{
			API:       APIAnthropicMessages,
			Operation: "messages.create",
			Method:    http.MethodPost,
			Model:     defaultString(req.Model, c.defaultModel),
			Endpoint:  endpoint,
			Streaming: false,
		},
		c.requestHooks,
		http.MethodPost,
		endpoint,
		payload,
		c.chatRequestHeaders(false),
		decodeAnthropicError,
		func(body io.Reader) (*agent.ModelResponse, error) {
			var data anthropicResponse
			if err := json.NewDecoder(body).Decode(&data); err != nil {
				return nil, fmt.Errorf("anthropic: decode response: %w", err)
			}
			return fromAnthropicResponse(data, wireToInternal)
		},
	))
}

// --- Request types ---

type anthropicRequest struct {
	Model        string                 `json:"model"`
	MaxTokens    int                    `json:"max_tokens"`
	Temperature  float64                `json:"temperature,omitempty"`
	Stream       bool                   `json:"stream,omitempty"`
	CacheControl *anthropicCacheControl `json:"cache_control,omitempty"`
	Thinking     *anthropicThinking     `json:"thinking,omitempty"`
	System       any                    `json:"system,omitempty"`
	Messages     []anthropicMessage     `json:"messages"`
	Tools        []anthropicTool        `json:"tools,omitempty"`
}

type anthropicThinking struct {
	Type         string `json:"type"`
	BudgetTokens int    `json:"budget_tokens"`
}

type anthropicCacheControl struct {
	Type string `json:"type"`
}

type anthropicSystemBlock struct {
	Type         string                 `json:"type"`
	Text         string                 `json:"text"`
	CacheControl *anthropicCacheControl `json:"cache_control,omitempty"`
}

type anthropicMessage struct {
	Role    string `json:"role"`
	Content any    `json:"content"` // string or []anthropicContentBlock
}

type anthropicContentBlock struct {
	Type      string                `json:"type"`
	Text      string                `json:"text,omitempty"`
	ID        string                `json:"id,omitempty"`
	Name      string                `json:"name,omitempty"`
	Input     map[string]any        `json:"input,omitempty"`
	ToolUseID string                `json:"tool_use_id,omitempty"`
	Content   string                `json:"content,omitempty"`
	Source    *anthropicImageSource `json:"source,omitempty"`
}

// anthropicImageSource represents the source payload for an Anthropic image content block.
type anthropicImageSource struct {
	Type      string `json:"type"`
	MediaType string `json:"media_type"`
	Data      string `json:"data"`
}

type anthropicTool struct {
	Name         string                 `json:"name"`
	Description  string                 `json:"description,omitempty"`
	InputSchema  map[string]any         `json:"input_schema,omitempty"`
	CacheControl *anthropicCacheControl `json:"cache_control,omitempty"`
}

// --- Response types ---

type anthropicResponse struct {
	ID         string                   `json:"id"`
	Type       string                   `json:"type"`
	Role       string                   `json:"role"`
	Content    []anthropicResponseBlock `json:"content"`
	StopReason string                   `json:"stop_reason"`
	Usage      *anthropicUsage          `json:"usage,omitempty"`
}

type anthropicUsage struct {
	InputTokens              int `json:"input_tokens"`
	OutputTokens             int `json:"output_tokens"`
	CacheCreationInputTokens int `json:"cache_creation_input_tokens,omitempty"`
	CacheReadInputTokens     int `json:"cache_read_input_tokens,omitempty"`
}

type anthropicResponseBlock struct {
	Type  string         `json:"type"`
	Text  string         `json:"text,omitempty"`
	ID    string         `json:"id,omitempty"`
	Name  string         `json:"name,omitempty"`
	Input map[string]any `json:"input,omitempty"`
}

type anthropicErrorEnvelope struct {
	Type  string `json:"type"`
	Error struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"error"`
}

// --- Conversion helpers ---

func toAnthropicMessage(msg contextengine.Message, wireToInternal map[string]string) anthropicMessage {
	switch msg.Role {
	case contextengine.RoleAssistant:
		if len(msg.ToolCalls) > 0 {
			blocks := make([]anthropicContentBlock, 0, len(msg.ToolCalls)+1)
			if strings.TrimSpace(msg.Content) != "" {
				blocks = append(blocks, anthropicContentBlock{
					Type: "text",
					Text: msg.Content,
				})
			}
			for _, tc := range msg.ToolCalls {
				wireName := sanitizeToolName(tc.Name)
				wireToInternal[wireName] = tc.Name
				blocks = append(blocks, anthropicContentBlock{
					Type:  "tool_use",
					ID:    tc.ID,
					Name:  wireName,
					Input: argumentsOrParseError(tc.Arguments),
				})
			}
			return anthropicMessage{Role: "assistant", Content: blocks}
		}
		return anthropicMessage{Role: "assistant", Content: msg.Content}

	case contextengine.RoleTool:
		block := anthropicContentBlock{
			Type:      "tool_result",
			ToolUseID: msg.ToolCallID,
			Content:   msg.Content,
		}
		return anthropicMessage{Role: "user", Content: []anthropicContentBlock{block}}

	default:
		// User/system messages: check for multimodal content blocks (images).
		if msg.HasImageContent() {
			blocks := toAnthropicContentBlocks(msg.ContentBlocks)
			return anthropicMessage{Role: string(msg.Role), Content: blocks}
		}
		return anthropicMessage{Role: string(msg.Role), Content: msg.TextContent()}
	}
}

// toAnthropicContentBlocks converts contextengine.ContentBlock slices into
// the Anthropic API content block format, mapping image blocks to the
// Anthropic image source structure.
func toAnthropicContentBlocks(blocks []contextengine.ContentBlock) []anthropicContentBlock {
	out := make([]anthropicContentBlock, 0, len(blocks))
	for _, block := range blocks {
		switch block.Type {
		case contextengine.ContentBlockText:
			if block.Text != "" {
				out = append(out, anthropicContentBlock{
					Type: "text",
					Text: block.Text,
				})
			}
		case contextengine.ContentBlockImage:
			out = append(out, anthropicContentBlock{
				Type: "image",
				Source: &anthropicImageSource{
					Type:      "base64",
					MediaType: block.MediaType,
					Data:      block.Data,
				},
			})
		}
	}
	return out
}

func fromAnthropicResponse(resp anthropicResponse, wireToInternal map[string]string) (*agent.ModelResponse, error) {
	result := &agent.ModelResponse{
		Message: contextengine.Message{
			Role: contextengine.RoleAssistant,
		},
	}

	var textParts []string
	for _, block := range resp.Content {
		switch block.Type {
		case "text":
			textParts = append(textParts, block.Text)
		case "tool_use":
			result.ToolCalls = append(result.ToolCalls, agent.ToolCall{
				ID:    block.ID,
				Name:  restoreToolName(block.Name, wireToInternal),
				Input: block.Input,
			})
		}
	}
	result.Message.Content = strings.Join(textParts, "")
	if resp.Usage != nil {
		result.Usage = anthropicUsageToModelUsage(resp.Usage)
	}
	return result, nil
}

func decodeAnthropicError(body io.Reader, status int) error {
	data, _ := io.ReadAll(body)
	var envelope anthropicErrorEnvelope
	if err := json.Unmarshal(data, &envelope); err == nil && envelope.Error.Message != "" {
		return providerAPIError("anthropic", status, envelope.Error.Type, envelope.Error.Message)
	}
	return providerAPIError("anthropic", status, "", strings.TrimSpace(string(data)))
}

// --- Streaming SSE types ---

type anthropicSSEEvent struct {
	Type string `json:"type"`
	// Used by content_block_start
	Index        int                       `json:"index,omitempty"`
	ContentBlock *anthropicSSEContentBlock `json:"content_block,omitempty"`
	// Used by content_block_delta
	Delta *anthropicSSEDelta `json:"delta,omitempty"`
}

type anthropicSSEContentBlock struct {
	Type string `json:"type"`
	ID   string `json:"id,omitempty"`
	Name string `json:"name,omitempty"`
	Text string `json:"text,omitempty"`
}

type anthropicSSEDelta struct {
	Type        string `json:"type"`
	Text        string `json:"text,omitempty"`
	Thinking    string `json:"thinking,omitempty"`
	PartialJSON string `json:"partial_json,omitempty"`
}

// anthropicStreamUsage accumulates token usage from message_start and message_delta SSE events.
type anthropicStreamUsage struct {
	InputTokens              int
	OutputTokens             int
	CacheCreationInputTokens int
	CacheReadInputTokens     int
}

// ChatStream implements StreamingModelClient by sending a streaming request
// to the Anthropic API and invoking the callback as SSE events arrive.
func (c *AnthropicClient) ChatStream(ctx context.Context, req agent.ChatRequest, cb agent.StreamCallback) (*agent.ModelResponse, error) {
	payload, endpoint, wireToInternal, err := c.buildChatPayload(req, true)
	if err != nil {
		if cb != nil {
			cb.OnError(ctx, err)
		}
		return nil, err
	}
	result, err := executeProviderRequest(ctx, newProviderJSONRequestOptions(
		c.httpClient,
		ProviderRequestMetadata{
			API:       APIAnthropicMessages,
			Operation: "messages.create",
			Method:    http.MethodPost,
			Model:     defaultString(req.Model, c.defaultModel),
			Endpoint:  endpoint,
			Streaming: true,
		},
		c.requestHooks,
		http.MethodPost,
		endpoint,
		payload,
		c.chatRequestHeaders(true),
		decodeAnthropicError,
		func(body io.Reader) (*agent.ModelResponse, error) {
			return consumeAnthropicSSEStream(ctx, body, wireToInternal, cb)
		},
	))
	if err != nil && cb != nil {
		cb.OnError(ctx, err)
	}
	return result, err
}

// consumeAnthropicSSEStream reads Anthropic SSE events, invokes callbacks,
// and accumulates the final ModelResponse.
func consumeAnthropicSSEStream(ctx context.Context, body io.Reader, wireToInternal map[string]string, cb agent.StreamCallback) (*agent.ModelResponse, error) {
	scanner := newSSEScanner(body)

	// Track content blocks by index for accumulation.
	type blockAcc struct {
		Type     string
		ID       string
		Name     string
		Text     strings.Builder
		ArgsJSON strings.Builder
	}
	blocks := make(map[int]*blockAcc)

	var eventType string
	var streamUsage anthropicStreamUsage

	for {
		event, ok, err := nextSSEEvent(scanner)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
		if !ok {
			continue
		}
		eventType = event.Event
		data := event.Data

		switch eventType {
		case "content_block_start":
			var event anthropicSSEEvent
			if err := jsonrepair.DecodeJSONObjectCandidate(data, &event); err != nil {
				continue
			}
			if event.ContentBlock != nil {
				blocks[event.Index] = &blockAcc{
					Type: event.ContentBlock.Type,
					ID:   event.ContentBlock.ID,
					Name: event.ContentBlock.Name,
				}
				if event.ContentBlock.Type == "tool_use" && cb != nil {
					cb.OnToolCallStart(ctx, event.ContentBlock.ID, restoreToolName(event.ContentBlock.Name, wireToInternal))
				}
			}

		case "content_block_delta":
			var event anthropicSSEEvent
			if err := jsonrepair.DecodeJSONObjectCandidate(data, &event); err != nil {
				continue
			}
			if event.Delta == nil {
				continue
			}
			acc := blocks[event.Index]
			if acc == nil {
				acc = &blockAcc{}
				blocks[event.Index] = acc
			}

			switch event.Delta.Type {
			case "text_delta":
				acc.Text.WriteString(event.Delta.Text)
				if cb != nil && event.Delta.Text != "" {
					cb.OnTextDelta(ctx, event.Delta.Text)
				}
			case "thinking_delta":
				if cb != nil && event.Delta.Thinking != "" {
					cb.OnReasoningDelta(ctx, event.Delta.Thinking)
				}
			case "input_json_delta":
				acc.ArgsJSON.WriteString(event.Delta.PartialJSON)
				if cb != nil && event.Delta.PartialJSON != "" {
					cb.OnToolCallDelta(ctx, acc.ID, event.Delta.PartialJSON)
				}
			}

		case "message_start":
			var envelope struct {
				Message struct {
					Usage *anthropicUsage `json:"usage,omitempty"`
				} `json:"message"`
			}
			if err := jsonrepair.DecodeJSONObjectCandidate(data, &envelope); err == nil && envelope.Message.Usage != nil {
				streamUsage.InputTokens = envelope.Message.Usage.InputTokens
				streamUsage.OutputTokens = envelope.Message.Usage.OutputTokens
				streamUsage.CacheCreationInputTokens = envelope.Message.Usage.CacheCreationInputTokens
				streamUsage.CacheReadInputTokens = envelope.Message.Usage.CacheReadInputTokens
			}

		case "message_delta":
			var envelope struct {
				Usage *struct {
					OutputTokens int `json:"output_tokens"`
				} `json:"usage,omitempty"`
			}
			if err := jsonrepair.DecodeJSONObjectCandidate(data, &envelope); err == nil && envelope.Usage != nil {
				streamUsage.OutputTokens = envelope.Usage.OutputTokens
			}

		case "message_stop":
			if cb != nil {
				cb.OnComplete(ctx)
			}

		case "error":
			// Anthropic may send an error event on the stream.
			var errEvt struct {
				Error struct {
					Type    string `json:"type"`
					Message string `json:"message"`
				} `json:"error"`
			}
			if err := jsonrepair.DecodeJSONObjectCandidate(data, &errEvt); err == nil && errEvt.Error.Message != "" {
				return nil, fmt.Errorf("anthropic stream error: %s: %s", errEvt.Error.Type, errEvt.Error.Message)
			}
		}
	}

	// Build the final response from accumulated blocks.
	result := &agent.ModelResponse{
		Message: contextengine.Message{
			Role: contextengine.RoleAssistant,
		},
	}
	var textParts []string
	for i := 0; ; i++ {
		acc, ok := blocks[i]
		if !ok {
			break
		}
		switch acc.Type {
		case "text":
			textParts = append(textParts, acc.Text.String())
		case "tool_use":
			name := restoreToolName(acc.Name, wireToInternal)
			var input map[string]any
			raw := acc.ArgsJSON.String()
			if strings.TrimSpace(raw) != "" {
				if err := jsonrepair.DecodeJSONObjectCandidate(raw, &input); err != nil {
					input = map[string]any{
						"_parse_error":   fmt.Sprintf("malformed tool arguments: %v", err),
						"_raw_arguments": strings.TrimSpace(raw),
					}
				}
			}
			result.ToolCalls = append(result.ToolCalls, agent.ToolCall{
				ID:    acc.ID,
				Name:  name,
				Input: input,
			})
		}
	}
	result.Message.Content = strings.Join(textParts, "")
	if streamUsage.InputTokens > 0 || streamUsage.OutputTokens > 0 ||
		streamUsage.CacheCreationInputTokens > 0 || streamUsage.CacheReadInputTokens > 0 {
		result.Usage = anthropicUsageToModelUsage(&anthropicUsage{
			InputTokens:              streamUsage.InputTokens,
			OutputTokens:             streamUsage.OutputTokens,
			CacheCreationInputTokens: streamUsage.CacheCreationInputTokens,
			CacheReadInputTokens:     streamUsage.CacheReadInputTokens,
		})
	}
	return result, nil
}

func (c *AnthropicClient) buildChatPayload(req agent.ChatRequest, stream bool) ([]byte, string, map[string]string, error) {
	model := defaultString(req.Model, c.defaultModel)
	if model == "" {
		return nil, "", nil, fmt.Errorf("anthropic: model is required")
	}

	maxTokens := req.Budget.ReservedOutput
	if maxTokens <= 0 {
		maxTokens = 4096
	}

	body := anthropicRequest{
		Model:       model,
		MaxTokens:   maxTokens,
		Stream:      stream,
		Temperature: req.Temperature,
	}
	if thinking := anthropicThinkingForRequest(req.ThinkingMode, maxTokens); thinking != nil {
		body.Thinking = thinking
	}
	if strings.TrimSpace(req.SystemPrompt) != "" {
		body.System = buildAnthropicSystemPrompt(req.SystemPrompt, c.cachePrompts)
	}
	if c.cachePrompts {
		body.CacheControl = anthropicEphemeralCacheControl()
	}

	wireToInternal := make(map[string]string)
	for _, msg := range req.Messages {
		body.Messages = append(body.Messages, toAnthropicMessage(msg, wireToInternal))
	}

	if len(req.Tools) > 0 {
		tools := append([]agent.ToolDefinition(nil), req.Tools...)
		if c.cachePrompts {
			sort.SliceStable(tools, func(i, j int) bool {
				left := sanitizeToolName(strings.TrimSpace(tools[i].Name))
				right := sanitizeToolName(strings.TrimSpace(tools[j].Name))
				if left != right {
					return left < right
				}
				return strings.TrimSpace(tools[i].Description) < strings.TrimSpace(tools[j].Description)
			})
		}
		body.Tools = make([]anthropicTool, 0, len(tools))
		for _, tool := range tools {
			wireName := sanitizeToolName(tool.Name)
			wireToInternal[wireName] = tool.Name
			body.Tools = append(body.Tools, anthropicTool{
				Name:        wireName,
				Description: tool.Description,
				InputSchema: tool.InputSchema,
			})
		}
		if c.cachePrompts && len(body.Tools) > 0 {
			body.Tools[len(body.Tools)-1].CacheControl = anthropicEphemeralCacheControl()
		}
	}

	payload, err := json.Marshal(body)
	if err != nil {
		return nil, "", nil, err
	}
	return payload, c.baseURL + "/v1/messages", wireToInternal, nil
}

func anthropicThinkingForRequest(mode agent.ThinkingMode, maxTokens int) *anthropicThinking {
	if mode != agent.ThinkingExtended || maxTokens < 1024 {
		return nil
	}
	budgetTokens := maxTokens / 2
	if budgetTokens < 1024 {
		budgetTokens = 1024
	}
	if budgetTokens > maxTokens {
		budgetTokens = maxTokens
	}
	return &anthropicThinking{
		Type:         "enabled",
		BudgetTokens: budgetTokens,
	}
}

func (c *AnthropicClient) chatRequestHeaders(stream bool) map[string]string {
	fields := []providerHeaderField{
		{Key: "x-api-key", Value: c.apiKey},
		{Key: "anthropic-version", Value: anthropicAPIVersion},
	}
	accept := ""
	if stream {
		accept = "text/event-stream"
	}
	if shouldSendAnthropicBearerAuth(c.baseURL, c.headers) {
		fields = append(fields, providerHeaderField{
			Key:      "Authorization",
			Value:    "Bearer " + c.apiKey,
			IfAbsent: true,
		})
	}
	if shouldSendKimiCodingUserAgent(c.baseURL, c.headers) {
		fields = append(fields, providerHeaderField{
			Key:      "User-Agent",
			Value:    "claude-code/0.1.0",
			IfAbsent: true,
		})
	}
	return buildProviderJSONHeaders(providerJSONHeadersOptions{
		Base:   c.headers,
		Accept: accept,
		Fields: fields,
	})
}

func shouldSendAnthropicBearerAuth(baseURL string, headers map[string]string) bool {
	if headerValue(headers, "Authorization") != "" {
		return false
	}
	baseURL = strings.ToLower(strings.TrimSpace(baseURL))
	return strings.Contains(baseURL, "minimax") ||
		strings.Contains(baseURL, "hunyuan.cloud.tencent.com") ||
		strings.Contains(baseURL, "xiaomimimo.com")
}

func shouldSendKimiCodingUserAgent(baseURL string, headers map[string]string) bool {
	if headerValue(headers, "User-Agent") != "" {
		return false
	}
	baseURL = strings.ToLower(strings.TrimSpace(baseURL))
	return strings.Contains(baseURL, "api.kimi.com/coding")
}

func headerValue(headers map[string]string, key string) string {
	return newProviderHeaderMap(headers).Get(key)
}

func anthropicUsageToModelUsage(usage *anthropicUsage) *agent.ModelUsageInfo {
	if usage == nil {
		return nil
	}
	promptTokens := anthropicTotalInputTokens(usage)
	totalTokens := promptTokens + usage.OutputTokens
	return &agent.ModelUsageInfo{
		PromptTokens:             promptTokens,
		CompletionTokens:         usage.OutputTokens,
		TotalTokens:              totalTokens,
		CacheCreationInputTokens: usage.CacheCreationInputTokens,
		CacheReadInputTokens:     usage.CacheReadInputTokens,
	}
}

func anthropicTotalInputTokens(usage *anthropicUsage) int {
	if usage == nil {
		return 0
	}
	total := usage.InputTokens + usage.CacheCreationInputTokens + usage.CacheReadInputTokens
	if total < 0 {
		return 0
	}
	return total
}

func anthropicEphemeralCacheControl() *anthropicCacheControl {
	return &anthropicCacheControl{Type: "ephemeral"}
}

func buildAnthropicSystemPrompt(prompt string, cachePrompts bool) any {
	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		return ""
	}
	if !cachePrompts {
		return prompt
	}
	stablePrefix, dynamicSuffix, ok := splitAnthropicSystemPromptForCaching(prompt)
	if !ok {
		return prompt
	}
	blocks := []anthropicSystemBlock{{
		Type:         "text",
		Text:         stablePrefix,
		CacheControl: anthropicEphemeralCacheControl(),
	}}
	if suffix := strings.TrimSpace(dynamicSuffix); suffix != "" {
		blocks = append(blocks, anthropicSystemBlock{
			Type: "text",
			Text: suffix,
		})
	}
	return blocks
}

func splitAnthropicSystemPromptForCaching(prompt string) (string, string, bool) {
	markers := []string{
		"\n\nPinned facts: preserve these facts unless newer evidence explicitly contradicts them.",
		"\n\n<session_state>",
		"\n\n<recalled_context",
		"\n\n<interaction_advisory>",
		"\n\n<task_dependency_outcomes>",
		"\n\n<execution_plan>",
		"\n\n## Current Information Rule",
		"\n\n## Tool Selection",
		"\n\n## Response Guidelines",
		"\n\n## Tool Usage Guidelines",
		"\n\n## Evidence Rules",
		"\n\n<delegation_contract>",
		"\n\nTool groups for this turn:",
	}
	splitAt := -1
	for _, marker := range markers {
		idx := strings.Index(prompt, marker)
		if idx <= 0 {
			continue
		}
		if splitAt == -1 || idx < splitAt {
			splitAt = idx
		}
	}
	if splitAt <= 0 {
		return "", "", false
	}
	stablePrefix := strings.TrimSpace(prompt[:splitAt])
	dynamicSuffix := strings.TrimSpace(prompt[splitAt:])
	if stablePrefix == "" || dynamicSuffix == "" {
		return "", "", false
	}
	return stablePrefix, dynamicSuffix, true
}

func shouldUseAnthropicPromptCaching(baseURL string) bool {
	parsed, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil {
		return false
	}
	host := strings.ToLower(strings.TrimSpace(parsed.Hostname()))
	return host == "api.anthropic.com" || strings.HasSuffix(host, ".anthropic.com")
}

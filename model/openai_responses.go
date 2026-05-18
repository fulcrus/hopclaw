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
)

// ---------------------------------------------------------------------------
// Config & Client
// ---------------------------------------------------------------------------

// OpenAIResponsesConfig configures a client for the OpenAI Responses API.
type OpenAIResponsesConfig struct {
	BaseURL      string
	APIKey       string
	DefaultModel string
	Timeout      time.Duration
	Headers      map[string]string
	RequestHooks []ProviderRequestHook
}

// OpenAIResponsesClient implements ModelClient and StreamingModelClient
// for the OpenAI Responses API (/v1/responses).
type OpenAIResponsesClient struct {
	baseURL      string
	apiKey       string
	defaultModel string
	headers      map[string]string
	httpClient   *http.Client
	requestHooks []ProviderRequestHook
}

func NewOpenAIResponsesClient(cfg OpenAIResponsesConfig) (*OpenAIResponsesClient, error) {
	if strings.TrimSpace(cfg.BaseURL) == "" {
		return nil, fmt.Errorf("base url is required")
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 120 * time.Second
	}
	return &OpenAIResponsesClient{
		baseURL:      strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/"),
		apiKey:       strings.TrimSpace(cfg.APIKey),
		defaultModel: strings.TrimSpace(cfg.DefaultModel),
		headers:      cloneHeaders(cfg.Headers),
		httpClient:   newStreamingHTTPClient(cfg.Timeout),
		requestHooks: cloneProviderRequestHooks(cfg.RequestHooks),
	}, nil
}

// ---------------------------------------------------------------------------
// ModelClient / StreamingModelClient
// ---------------------------------------------------------------------------

func (c *OpenAIResponsesClient) Chat(ctx context.Context, req agent.ChatRequest) (*agent.ModelResponse, error) {
	payload, endpoint, wireToInternal, err := c.buildPayload(req, false)
	if err != nil {
		return nil, err
	}
	return executeProviderRequest(ctx, newProviderJSONRequestOptions(
		c.httpClient,
		ProviderRequestMetadata{
			API:       APIOpenAIResponses,
			Operation: "responses.create",
			Method:    http.MethodPost,
			Model:     defaultString(req.Model, c.defaultModel),
			Endpoint:  endpoint,
			Streaming: false,
		},
		c.requestHooks,
		http.MethodPost,
		endpoint,
		payload,
		c.requestHeaders(false),
		decodeOpenAIError,
		func(body io.Reader) (*agent.ModelResponse, error) {
			return consumeResponsesJSON(body, wireToInternal)
		},
	))
}

func (c *OpenAIResponsesClient) ChatStream(ctx context.Context, req agent.ChatRequest, cb agent.StreamCallback) (*agent.ModelResponse, error) {
	payload, endpoint, wireToInternal, err := c.buildPayload(req, true)
	if err != nil {
		if cb != nil {
			cb.OnError(ctx, err)
		}
		return nil, err
	}
	result, err := executeProviderRequest(ctx, newProviderJSONRequestOptions(
		c.httpClient,
		ProviderRequestMetadata{
			API:       APIOpenAIResponses,
			Operation: "responses.create",
			Method:    http.MethodPost,
			Model:     defaultString(req.Model, c.defaultModel),
			Endpoint:  endpoint,
			Streaming: true,
		},
		c.requestHooks,
		http.MethodPost,
		endpoint,
		payload,
		c.requestHeaders(true),
		decodeOpenAIError,
		func(body io.Reader) (*agent.ModelResponse, error) {
			return consumeResponsesSSEStream(ctx, body, wireToInternal, cb)
		},
	))
	if err != nil && cb != nil {
		cb.OnError(ctx, err)
	}
	return result, err
}

// ---------------------------------------------------------------------------
// Request building
// ---------------------------------------------------------------------------

func (c *OpenAIResponsesClient) buildPayload(req agent.ChatRequest, stream bool) ([]byte, string, map[string]string, error) {
	model := defaultString(req.Model, c.defaultModel)
	if model == "" {
		return nil, "", nil, fmt.Errorf("chat model is required")
	}

	body := responsesRequest{
		Model:  model,
		Stream: stream,
		Store:  false,
	}

	// System prompt → instructions field.
	if sp := strings.TrimSpace(req.SystemPrompt); sp != "" {
		body.Instructions = sp
	}

	if req.Budget.ReservedOutput > 0 {
		body.MaxOutputTokens = req.Budget.ReservedOutput
	}
	body.Temperature = req.Temperature
	if reasoning := responsesReasoningForThinkingMode(req.ThinkingMode); reasoning != nil {
		body.Reasoning = reasoning
	}

	// Convert messages to Responses API input items.
	body.Input = convertMessagesToInput(req.Messages)

	// Tools — same function definition format, slightly different wrapper.
	wireToInternal := make(map[string]string)
	if len(req.Tools) > 0 {
		body.Tools = make([]responsesTool, 0, len(req.Tools))
		for _, tool := range req.Tools {
			wireName := sanitizeToolName(tool.Name)
			wireToInternal[wireName] = tool.Name
			body.Tools = append(body.Tools, responsesTool{
				Type:        "function",
				Name:        wireName,
				Description: tool.Description,
				Parameters:  normalizeOpenAIToolSchema(tool.InputSchema),
			})
		}
		body.ToolChoice = "auto"
	}

	payload, err := json.Marshal(body)
	if err != nil {
		return nil, "", nil, err
	}
	endpoint, err := buildResponsesURL(c.baseURL)
	if err != nil {
		return nil, "", nil, err
	}
	return payload, endpoint, wireToInternal, nil
}

// convertMessagesToInput maps contextengine.Message slices to the Responses
// API input format. The key differences from chat completions:
//   - assistant tool calls become {"type": "function_call", ...} items
//   - tool results become {"type": "function_call_output", ...} items
//   - system messages are skipped (handled via instructions)
func convertMessagesToInput(msgs []contextengine.Message) []responsesInputItem {
	items := make([]responsesInputItem, 0, len(msgs))
	for _, msg := range msgs {
		switch msg.Role {
		case contextengine.RoleSystem:
			// System messages are handled as instructions; skip.
			continue

		case contextengine.RoleUser:
			item := responsesInputItem{Role: "user"}
			item.Content = messageToResponsesContentParts(msg)
			items = append(items, item)

		case contextengine.RoleAssistant:
			// If the assistant message has tool calls, emit them as
			// function_call items after an optional text message.
			if textContent := msg.TextContent(); textContent != "" {
				items = append(items, responsesInputItem{
					Role: "assistant",
					Content: []responsesInputContentPart{{
						Type: "input_text",
						Text: textContent,
					}},
				})
			}
			for index, tc := range msg.ToolCalls {
				callID := strings.TrimSpace(tc.ID)
				if callID == "" {
					callID = generatedToolCallID("openai-responses", index)
				}
				items = append(items, responsesInputItem{
					Type:      "function_call",
					ID:        callID,
					CallID:    callID,
					Name:      sanitizeToolName(tc.Name),
					Arguments: tc.Arguments,
				})
			}

		case contextengine.RoleTool:
			// Tool results → function_call_output items.
			items = append(items, responsesInputItem{
				Type:   "function_call_output",
				CallID: msg.ToolCallID,
				Output: msg.TextContent(),
			})
		}
	}
	return items
}

func messageToResponsesContentParts(msg contextengine.Message) any {
	if len(msg.ContentBlocks) > 0 {
		return toResponsesContentParts(msg.ContentBlocks)
	}
	if text := msg.TextContent(); text != "" {
		return []responsesInputContentPart{{
			Type: "input_text",
			Text: text,
		}}
	}
	return []responsesInputContentPart{}
}

// toResponsesContentParts converts multimodal content blocks for the
// Responses API input format.
func toResponsesContentParts(blocks []contextengine.ContentBlock) any {
	parts := make([]responsesInputContentPart, 0, len(blocks))
	for _, block := range blocks {
		switch block.Type {
		case contextengine.ContentBlockText:
			if block.Text != "" {
				parts = append(parts, responsesInputContentPart{
					Type: "input_text",
					Text: block.Text,
				})
			}
		case contextengine.ContentBlockImage:
			url := block.SourceURL
			if url == "" && block.Data != "" {
				url = "data:" + block.MediaType + ";base64," + block.Data
			}
			if url != "" {
				parts = append(parts, responsesInputContentPart{
					Type:     "input_image",
					ImageURL: url,
				})
			}
		}
	}
	return parts
}

func (c *OpenAIResponsesClient) requestHeaders(stream bool) map[string]string {
	accept := "application/json"
	if stream {
		accept = "text/event-stream"
	}
	return buildProviderJSONHeaders(providerJSONHeadersOptions{
		Base:        c.headers,
		Accept:      accept,
		BearerToken: c.apiKey,
	})
}

func buildResponsesURL(base string) (string, error) {
	u, err := url.Parse(base)
	if err != nil {
		return "", err
	}
	u.Path = path.Join(u.Path, "/responses")
	return u.String(), nil
}

// ---------------------------------------------------------------------------
// SSE stream consumer — Responses API format
// ---------------------------------------------------------------------------

func consumeResponsesJSON(body io.Reader, wireToInternal map[string]string) (*agent.ModelResponse, error) {
	var response responsesResponse
	if err := json.NewDecoder(body).Decode(&response); err != nil {
		return nil, err
	}
	if err := responsesError(response.Status, response.Error); err != nil {
		return nil, err
	}
	return buildResponsesModelResponse(response.Output, response.Usage, wireToInternal), nil
}

// consumeResponsesSSEStream reads the SSE stream from the Responses API and
// accumulates the output into a ModelResponse. The Responses API uses named
// events (event: + data:) rather than bare data: lines.
func consumeResponsesSSEStream(
	ctx context.Context,
	body io.Reader,
	wireToInternal map[string]string,
	cb agent.StreamCallback,
) (*agent.ModelResponse, error) {
	scanner := newSSEScanner(body)

	var contentParts []string

	// Track function calls by output_index.
	type funcCallAcc struct {
		ID                string
		Name              string
		Args              strings.Builder
		Started           bool
		DeliveredArgBytes int
	}
	funcCalls := make(map[int]*funcCallAcc)

	var completed *responsesResponse

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

		switch event.Event {
		case "response.output_text.delta":
			var delta responsesTextDelta
			if err := jsonrepair.DecodeJSONObjectCandidate(event.Data, &delta); err != nil {
				continue
			}
			if delta.Delta != "" {
				contentParts = append(contentParts, delta.Delta)
				if cb != nil {
					cb.OnTextDelta(ctx, delta.Delta)
				}
			}

		case "response.function_call_arguments.delta":
			var delta responsesFuncArgDelta
			if err := jsonrepair.DecodeJSONObjectCandidate(event.Data, &delta); err != nil {
				continue
			}
			acc, ok := funcCalls[delta.OutputIndex]
			if !ok {
				acc = &funcCallAcc{}
				funcCalls[delta.OutputIndex] = acc
			}
			acc.Args.WriteString(delta.Delta)
			if cb != nil && acc.Started && acc.ID != "" && delta.Delta != "" {
				cb.OnToolCallDelta(ctx, acc.ID, delta.Delta)
				acc.DeliveredArgBytes += len(delta.Delta)
			}

		case "response.output_item.added", "response.output_item.done":
			var item responsesOutputItemEvent
			if err := jsonrepair.DecodeJSONObjectCandidate(event.Data, &item); err != nil {
				continue
			}
			if item.Item.Type == "function_call" {
				acc, ok := funcCalls[item.OutputIndex]
				if !ok {
					acc = &funcCallAcc{}
					funcCalls[item.OutputIndex] = acc
				}
				acc.ID = defaultString(item.Item.CallID, acc.ID)
				if acc.ID == "" {
					acc.ID = defaultString(item.Item.ID, generatedToolCallID("openai-responses", item.OutputIndex))
				}
				acc.Name = defaultString(item.Item.Name, acc.Name)
				if cb != nil && !acc.Started {
					cb.OnToolCallStart(ctx, acc.ID, restoreToolName(acc.Name, wireToInternal))
					acc.Started = true
				}
				if cb != nil && acc.Started && acc.DeliveredArgBytes < acc.Args.Len() {
					args := acc.Args.String()
					pending := args[acc.DeliveredArgBytes:]
					if pending != "" {
						cb.OnToolCallDelta(ctx, acc.ID, pending)
						acc.DeliveredArgBytes = len(args)
					}
				}
			}

		case "response.completed":
			var completedEvent responsesCompletedEvent
			if err := jsonrepair.DecodeJSONObjectCandidate(event.Data, &completedEvent); err != nil {
				continue
			}
			if err := responsesError(completedEvent.Response.Status, completedEvent.Response.Error); err != nil {
				return nil, err
			}
			completedCopy := completedEvent.Response
			completed = &completedCopy

		case "response.failed":
			var failed responsesCompletedEvent
			if err := jsonrepair.DecodeJSONObjectCandidate(event.Data, &failed); err == nil {
				if err := responsesError(failed.Response.Status, failed.Response.Error); err != nil {
					return nil, err
				}
			}
			return nil, fmt.Errorf("openai-responses stream failed")

		case "error":
			var streamErr responsesStreamError
			if err := jsonrepair.DecodeJSONObjectCandidate(event.Data, &streamErr); err == nil {
				return nil, responsesStreamErrorf(streamErr.Code, streamErr.Message)
			}
			return nil, fmt.Errorf("openai-responses stream error: %s", strings.TrimSpace(event.Data))
		}
	}
	if cb != nil {
		cb.OnComplete(ctx)
	}

	if completed != nil {
		result := buildResponsesModelResponse(completed.Output, completed.Usage, wireToInternal)
		if strings.TrimSpace(result.Message.Content) == "" && len(result.ToolCalls) == 0 && len(contentParts) > 0 {
			result.Message.Content = strings.Join(contentParts, "")
		}
		return result, nil
	}

	// Build final ModelResponse from streamed deltas when the stream ends
	// without a terminal response.completed payload.
	result := &agent.ModelResponse{
		Message: contextengine.Message{
			Role:    contextengine.RoleAssistant,
			Content: strings.Join(contentParts, ""),
		},
	}
	// Collect function calls sorted by output_index.
	if len(funcCalls) > 0 {
		calls := make([]agent.ToolCall, 0, len(funcCalls))
		for i := 0; i < len(funcCalls)+maxFuncCallGap; i++ {
			acc, ok := funcCalls[i]
			if !ok {
				if len(calls) >= len(funcCalls) {
					break
				}
				continue
			}
			toolCallID := defaultString(acc.ID, generatedToolCallID("openai-responses", i))
			calls = append(calls, agent.ToolCall{
				ID:    toolCallID,
				Name:  restoreToolName(acc.Name, wireToInternal),
				Input: argumentsOrParseError(acc.Args.String()),
			})
		}
		result.ToolCalls = calls
		// When there are tool calls, clear content to match Chat Completions behavior.
		if len(calls) > 0 {
			result.Message.Content = ""
		}
	}

	return result, nil
}

func buildResponsesModelResponse(output []responsesOutputItem, usage *responsesUsage, wireToInternal map[string]string) *agent.ModelResponse {
	result := &agent.ModelResponse{
		Message: contextengine.Message{
			Role: contextengine.RoleAssistant,
		},
	}

	contentParts := make([]string, 0, len(output))
	toolCalls := make([]agent.ToolCall, 0, len(output))

	for index, item := range output {
		switch item.Type {
		case "message":
			for _, part := range item.Content {
				if part.Type == "output_text" && part.Text != "" {
					contentParts = append(contentParts, part.Text)
				}
			}
		case "function_call":
			toolCallID := defaultString(item.CallID, item.ID)
			if toolCallID == "" {
				toolCallID = generatedToolCallID("openai-responses", index)
			}
			toolCalls = append(toolCalls, agent.ToolCall{
				ID:    toolCallID,
				Name:  restoreToolName(item.Name, wireToInternal),
				Input: argumentsOrParseError(item.Arguments),
			})
		}
	}

	result.Message.Content = strings.Join(contentParts, "")
	result.ToolCalls = toolCalls
	if len(toolCalls) > 0 {
		result.Message.Content = ""
	}
	if usage != nil {
		result.Usage = &agent.ModelUsageInfo{
			PromptTokens:     usage.InputTokens,
			CompletionTokens: usage.OutputTokens,
			TotalTokens:      usage.TotalTokens,
		}
	}
	return result
}

func responsesError(status string, errInfo *responsesErrorInfo) error {
	if errInfo == nil {
		if strings.EqualFold(strings.TrimSpace(status), "failed") {
			return fmt.Errorf("openai-responses request failed")
		}
		return nil
	}
	return responsesStreamErrorf(errInfo.Code, errInfo.Message)
}

func responsesStreamErrorf(code, message string) error {
	message = strings.TrimSpace(message)
	if message == "" {
		message = "request failed"
	}
	code = strings.TrimSpace(code)
	if code == "" {
		return fmt.Errorf("openai-responses stream error: %s", message)
	}
	return fmt.Errorf("openai-responses stream error (%s): %s", code, message)
}

// maxFuncCallGap is the maximum gap in output_index to tolerate when
// collecting function calls (reasoning items may precede them).
const maxFuncCallGap = 5

// ---------------------------------------------------------------------------
// Wire types — Responses API
// ---------------------------------------------------------------------------

type responsesRequest struct {
	Model           string               `json:"model"`
	Input           []responsesInputItem `json:"input"`
	Instructions    string               `json:"instructions,omitempty"`
	Tools           []responsesTool      `json:"tools,omitempty"`
	ToolChoice      string               `json:"tool_choice,omitempty"`
	Reasoning       *responsesReasoning  `json:"reasoning,omitempty"`
	MaxOutputTokens int                  `json:"max_output_tokens,omitempty"`
	Temperature     float64              `json:"temperature,omitempty"`
	Stream          bool                 `json:"stream"`
	Store           bool                 `json:"store"`
}

type responsesReasoning struct {
	Effort string `json:"effort,omitempty"`
}

func responsesReasoningForThinkingMode(mode agent.ThinkingMode) *responsesReasoning {
	switch mode {
	case agent.ThinkingExtended:
		return &responsesReasoning{Effort: "high"}
	case agent.ThinkingRegular:
		return &responsesReasoning{Effort: "low"}
	default:
		return nil
	}
}

// responsesInputItem is a union type — the fields used depend on the item type.
// For role-based items (user/assistant): Role + Content.
// For function_call items: Type + ID + CallID + Name + Arguments.
// For function_call_output items: Type + CallID + Output.
type responsesInputItem struct {
	// Role-based items (user, assistant, developer)
	Role    string `json:"role,omitempty"`
	Content any    `json:"content,omitempty"`

	// Function call items
	Type      string `json:"type,omitempty"`
	ID        string `json:"id,omitempty"`
	CallID    string `json:"call_id,omitempty"`
	Name      string `json:"name,omitempty"`
	Arguments string `json:"arguments,omitempty"`

	// Function call output items
	Output string `json:"output,omitempty"`
}

type responsesInputContentPart struct {
	Type     string `json:"type"`
	Text     string `json:"text,omitempty"`
	ImageURL string `json:"image_url,omitempty"`
}

type responsesTool struct {
	Type        string         `json:"type"`
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	Parameters  map[string]any `json:"parameters,omitempty"`
}

// Streaming event data types.

type responsesTextDelta struct {
	Delta       string `json:"delta"`
	OutputIndex int    `json:"output_index"`
}

type responsesFuncArgDelta struct {
	Delta       string `json:"delta"`
	OutputIndex int    `json:"output_index"`
}

type responsesOutputItemEvent struct {
	OutputIndex int                 `json:"output_index"`
	Item        responsesOutputItem `json:"item"`
}

type responsesOutputItem struct {
	Type      string                       `json:"type"`
	ID        string                       `json:"id,omitempty"`
	CallID    string                       `json:"call_id,omitempty"`
	Name      string                       `json:"name,omitempty"`
	Role      string                       `json:"role,omitempty"`
	Arguments string                       `json:"arguments,omitempty"`
	Status    string                       `json:"status,omitempty"`
	Content   []responsesOutputContentPart `json:"content,omitempty"`
}

type responsesOutputContentPart struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

type responsesCompletedEvent struct {
	Response responsesResponse `json:"response"`
}

type responsesResponse struct {
	Status string                `json:"status,omitempty"`
	Error  *responsesErrorInfo   `json:"error,omitempty"`
	Output []responsesOutputItem `json:"output,omitempty"`
	Usage  *responsesUsage       `json:"usage,omitempty"`
}

type responsesErrorInfo struct {
	Code    string `json:"code,omitempty"`
	Message string `json:"message,omitempty"`
}

type responsesStreamError struct {
	Code    string `json:"code,omitempty"`
	Message string `json:"message,omitempty"`
}

type responsesUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
	TotalTokens  int `json:"total_tokens"`
}

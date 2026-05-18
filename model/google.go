package model

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/fulcrus/hopclaw/agent"
	"github.com/fulcrus/hopclaw/contextengine"
)

type GoogleConfig struct {
	BaseURL      string
	APIKey       string
	DefaultModel string
	Timeout      time.Duration
	Headers      map[string]string
	RequestHooks []ProviderRequestHook
}

type GoogleClient struct {
	baseURL      string
	apiKey       string
	defaultModel string
	httpClient   *http.Client
	headers      map[string]string
	requestHooks []ProviderRequestHook
}

func NewGoogleClient(cfg GoogleConfig) (*GoogleClient, error) {
	if strings.TrimSpace(cfg.APIKey) == "" {
		return nil, fmt.Errorf("google: api_key is required")
	}
	baseURL := strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	if baseURL == "" {
		baseURL = "https://generativelanguage.googleapis.com"
	}
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = 120 * time.Second
	}
	return &GoogleClient{
		baseURL:      baseURL,
		apiKey:       strings.TrimSpace(cfg.APIKey),
		defaultModel: strings.TrimSpace(cfg.DefaultModel),
		httpClient:   newStreamingHTTPClient(timeout),
		headers:      cloneHeaders(cfg.Headers),
		requestHooks: cloneProviderRequestHooks(cfg.RequestHooks),
	}, nil
}

func (c *GoogleClient) Chat(ctx context.Context, req agent.ChatRequest) (*agent.ModelResponse, error) {
	model, payload, wireToInternal, err := c.buildChatPayload(req)
	if err != nil {
		return nil, err
	}

	endpoint := fmt.Sprintf("%s/v1beta/models/%s:generateContent", c.baseURL, model)
	return executeProviderRequest(ctx, newProviderJSONRequestOptions(
		c.httpClient,
		ProviderRequestMetadata{
			API:       APIGoogleGenerativeAI,
			Operation: "models.generate_content",
			Method:    http.MethodPost,
			Model:     model,
			Endpoint:  endpoint,
			Streaming: false,
		},
		c.requestHooks,
		http.MethodPost,
		endpoint,
		payload,
		c.chatRequestHeaders(""),
		decodeGoogleError,
		func(body io.Reader) (*agent.ModelResponse, error) {
			var data googleResponse
			if err := json.NewDecoder(body).Decode(&data); err != nil {
				return nil, fmt.Errorf("google: decode response: %w", err)
			}
			return fromGoogleResponse(data, wireToInternal)
		},
	))
}

func (c *GoogleClient) ChatStream(ctx context.Context, req agent.ChatRequest, cb agent.StreamCallback) (*agent.ModelResponse, error) {
	model, payload, wireToInternal, err := c.buildChatPayload(req)
	if err != nil {
		if cb != nil {
			cb.OnError(ctx, err)
		}
		return nil, err
	}

	endpoint := fmt.Sprintf("%s/v1beta/models/%s:streamGenerateContent?alt=sse", c.baseURL, model)
	result, err := executeProviderRequest(ctx, newProviderJSONRequestOptions(
		c.httpClient,
		ProviderRequestMetadata{
			API:       APIGoogleGenerativeAI,
			Operation: "models.generate_content",
			Method:    http.MethodPost,
			Model:     model,
			Endpoint:  endpoint,
			Streaming: true,
		},
		c.requestHooks,
		http.MethodPost,
		endpoint,
		payload,
		c.chatRequestHeaders("text/event-stream"),
		decodeGoogleError,
		func(body io.Reader) (*agent.ModelResponse, error) {
			return consumeGoogleSSEStream(ctx, body, wireToInternal, cb)
		},
	))
	if err != nil && cb != nil {
		cb.OnError(ctx, err)
	}
	return result, err
}

// --- Request types ---

type googleRequest struct {
	Contents          []googleContent  `json:"contents"`
	SystemInstruction *googleContent   `json:"systemInstruction,omitempty"`
	Tools             []googleToolDef  `json:"tools,omitempty"`
	GenerationConfig  *googleGenConfig `json:"generationConfig,omitempty"`
}

type googleContent struct {
	Role  string       `json:"role,omitempty"`
	Parts []googlePart `json:"parts"`
}

type googlePart struct {
	Text             string              `json:"text,omitempty"`
	InlineData       *googleInlineData   `json:"inlineData,omitempty"`
	FunctionCall     *googleFunctionCall `json:"functionCall,omitempty"`
	FunctionResponse *googleFunctionResp `json:"functionResponse,omitempty"`
}

// googleInlineData represents inline binary data for Gemini vision requests.
type googleInlineData struct {
	MIMEType string `json:"mimeType"` // Google Gemini API uses camelCase
	Data     string `json:"data"`
}

type googleFunctionCall struct {
	Name string         `json:"name"`
	Args map[string]any `json:"args,omitempty"`
}

type googleFunctionResp struct {
	Name     string         `json:"name"`
	Response map[string]any `json:"response"`
}

type googleToolDef struct {
	FunctionDeclarations []googleFunctionDecl `json:"functionDeclarations"`
}

type googleFunctionDecl struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	Parameters  map[string]any `json:"parameters,omitempty"`
}

type googleGenConfig struct {
	Temperature     float64 `json:"temperature,omitempty"`
	MaxOutputTokens int     `json:"maxOutputTokens,omitempty"`
}

// --- Response types ---

type googleResponse struct {
	Candidates    []googleCandidate    `json:"candidates"`
	UsageMetadata *googleUsageMetadata `json:"usageMetadata,omitempty"`
}

type googleCandidate struct {
	Content      googleContent `json:"content"`
	FinishReason string        `json:"finishReason"`
}

// googleUsageMetadata captures token usage from the Google Generative AI API.
type googleUsageMetadata struct {
	PromptTokenCount     int `json:"promptTokenCount"`
	CandidatesTokenCount int `json:"candidatesTokenCount"`
	TotalTokenCount      int `json:"totalTokenCount"`
}

// --- Conversion helpers ---

func toGoogleContents(msgs []contextengine.Message, wireToInternal map[string]string) []googleContent {
	var contents []googleContent
	for _, msg := range msgs {
		switch msg.Role {
		case contextengine.RoleAssistant:
			parts := make([]googlePart, 0)
			if strings.TrimSpace(msg.Content) != "" {
				parts = append(parts, googlePart{Text: msg.Content})
			}
			for _, tc := range msg.ToolCalls {
				wireName := sanitizeToolName(tc.Name)
				wireToInternal[wireName] = tc.Name
				parts = append(parts, googlePart{
					FunctionCall: &googleFunctionCall{Name: wireName, Args: argumentsOrParseError(tc.Arguments)},
				})
			}
			if len(parts) > 0 {
				contents = mergeGoogleContent(contents, googleContent{Role: "model", Parts: parts})
			}

		case contextengine.RoleTool:
			name := msg.Name
			if name == "" {
				name = "unknown"
			}
			wireName := sanitizeToolName(name)
			wireToInternal[wireName] = name
			respData := map[string]any{"result": msg.Content}
			parts := []googlePart{{
				FunctionResponse: &googleFunctionResp{Name: wireName, Response: respData},
			}}
			contents = mergeGoogleContent(contents, googleContent{Role: "user", Parts: parts})

		default: // user, system
			var parts []googlePart
			if msg.HasImageContent() {
				parts = toGoogleImageParts(msg.ContentBlocks)
			} else {
				parts = []googlePart{{Text: msg.TextContent()}}
			}
			contents = mergeGoogleContent(contents, googleContent{Role: "user", Parts: parts})
		}
	}
	return contents
}

// mergeGoogleContent ensures Gemini's strict alternating user/model turn requirement.
// If the last content has the same role, merge parts into it.
func mergeGoogleContent(contents []googleContent, next googleContent) []googleContent {
	if len(contents) > 0 && contents[len(contents)-1].Role == next.Role {
		contents[len(contents)-1].Parts = append(contents[len(contents)-1].Parts, next.Parts...)
		return contents
	}
	return append(contents, next)
}

// toGoogleImageParts converts contextengine.ContentBlock slices into Google
// Gemini parts with inline data for images and text parts for text.
func toGoogleImageParts(blocks []contextengine.ContentBlock) []googlePart {
	parts := make([]googlePart, 0, len(blocks))
	for _, block := range blocks {
		switch block.Type {
		case contextengine.ContentBlockText:
			if block.Text != "" {
				parts = append(parts, googlePart{Text: block.Text})
			}
		case contextengine.ContentBlockImage:
			if block.Data != "" {
				parts = append(parts, googlePart{
					InlineData: &googleInlineData{
						MIMEType: block.MediaType,
						Data:     block.Data,
					},
				})
			}
		}
	}
	return parts
}

func (c *GoogleClient) buildChatPayload(req agent.ChatRequest) (string, []byte, map[string]string, error) {
	model := defaultString(req.Model, c.defaultModel)
	if model == "" {
		return "", nil, nil, fmt.Errorf("google: model is required")
	}

	body := googleRequest{}
	if strings.TrimSpace(req.SystemPrompt) != "" {
		body.SystemInstruction = &googleContent{
			Parts: []googlePart{{Text: req.SystemPrompt}},
		}
	}

	wireToInternal := make(map[string]string)
	body.Contents = toGoogleContents(req.Messages, wireToInternal)

	if len(req.Tools) > 0 {
		decls := make([]googleFunctionDecl, 0, len(req.Tools))
		for _, tool := range req.Tools {
			wireName := sanitizeToolName(tool.Name)
			wireToInternal[wireName] = tool.Name
			decls = append(decls, googleFunctionDecl{
				Name:        wireName,
				Description: tool.Description,
				Parameters:  cleanGoogleSchema(tool.InputSchema),
			})
		}
		body.Tools = []googleToolDef{{FunctionDeclarations: decls}}
	}

	maxTokens := req.Budget.ReservedOutput
	if maxTokens <= 0 {
		maxTokens = 4096
	}
	body.GenerationConfig = &googleGenConfig{
		Temperature:     req.Temperature,
		MaxOutputTokens: maxTokens,
	}

	payload, err := json.Marshal(body)
	if err != nil {
		return "", nil, nil, err
	}
	return model, payload, wireToInternal, nil
}

func fromGoogleResponse(resp googleResponse, wireToInternal map[string]string) (*agent.ModelResponse, error) {
	if len(resp.Candidates) == 0 {
		return &agent.ModelResponse{}, nil
	}
	candidate := resp.Candidates[0]

	result := &agent.ModelResponse{
		Message: contextengine.Message{
			Role: contextengine.RoleAssistant,
		},
	}

	var textParts []string
	toolCallIndex := 0
	for _, part := range candidate.Content.Parts {
		if part.Text != "" {
			textParts = append(textParts, part.Text)
		}
		if part.FunctionCall != nil {
			name := restoreToolName(part.FunctionCall.Name, wireToInternal)
			result.ToolCalls = append(result.ToolCalls, agent.ToolCall{
				ID:    generatedToolCallID("google", toolCallIndex),
				Name:  name,
				Input: part.FunctionCall.Args,
			})
			toolCallIndex++
		}
	}
	result.Message.Content = strings.Join(textParts, "")
	if resp.UsageMetadata != nil {
		result.Usage = &agent.ModelUsageInfo{
			PromptTokens:     resp.UsageMetadata.PromptTokenCount,
			CompletionTokens: resp.UsageMetadata.CandidatesTokenCount,
			TotalTokens:      resp.UsageMetadata.TotalTokenCount,
		}
	}
	return result, nil
}

func consumeGoogleSSEStream(ctx context.Context, body io.Reader, wireToInternal map[string]string, cb agent.StreamCallback) (*agent.ModelResponse, error) {
	scanner := newSSEScanner(body)

	result := &agent.ModelResponse{
		Message: contextengine.Message{Role: contextengine.RoleAssistant},
	}
	var content strings.Builder
	toolCallIndex := 0

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
		payload := event.Data
		if payload == "" || payload == "[DONE]" {
			continue
		}
		chunks, err := decodeGoogleStreamChunk(payload)
		if err != nil {
			return nil, err
		}
		for _, chunk := range chunks {
			if chunk.UsageMetadata != nil {
				result.Usage = &agent.ModelUsageInfo{
					PromptTokens:     chunk.UsageMetadata.PromptTokenCount,
					CompletionTokens: chunk.UsageMetadata.CandidatesTokenCount,
					TotalTokens:      chunk.UsageMetadata.TotalTokenCount,
				}
			}
			for _, candidate := range chunk.Candidates {
				for _, part := range candidate.Content.Parts {
					if part.Text != "" {
						content.WriteString(part.Text)
						if cb != nil {
							cb.OnTextDelta(ctx, part.Text)
						}
					}
					if part.FunctionCall != nil {
						name := restoreToolName(part.FunctionCall.Name, wireToInternal)
						toolCall := agent.ToolCall{
							ID:    generatedToolCallID("google", toolCallIndex),
							Name:  name,
							Input: part.FunctionCall.Args,
						}
						toolCallIndex++
						result.ToolCalls = append(result.ToolCalls, toolCall)
						if cb != nil {
							cb.OnToolCallStart(ctx, toolCall.ID, toolCall.Name)
							cb.OnToolCallDelta(ctx, toolCall.ID, marshalToolCallInput(toolCall.Input))
						}
					}
				}
			}
		}
	}

	result.Message.Content = content.String()
	if cb != nil {
		cb.OnComplete(ctx)
	}
	return result, nil
}

func decodeGoogleStreamChunk(payload string) ([]googleResponse, error) {
	payload = strings.TrimSpace(payload)
	if payload == "" {
		return nil, nil
	}
	var single googleResponse
	if err := json.Unmarshal([]byte(payload), &single); err == nil {
		return []googleResponse{single}, nil
	}
	var batch []googleResponse
	if err := json.Unmarshal([]byte(payload), &batch); err == nil {
		return batch, nil
	}
	return nil, fmt.Errorf("google: decode stream chunk: %s", payload)
}

// cleanGoogleSchema strips JSON Schema fields that Google Generative AI doesn't support.
func cleanGoogleSchema(schema map[string]any) map[string]any {
	if schema == nil {
		return nil
	}
	unsupported := []string{
		"patternProperties", "additionalProperties", "$ref", "$defs",
		"minLength", "maxLength", "pattern", "format",
		"minItems", "maxItems", "minProperties", "maxProperties",
	}
	cleaned := make(map[string]any, len(schema))
	for k, v := range schema {
		skip := false
		for _, u := range unsupported {
			if k == u {
				skip = true
				break
			}
		}
		if skip {
			continue
		}
		if nested, ok := v.(map[string]any); ok {
			cleaned[k] = cleanGoogleSchema(nested)
		} else {
			cleaned[k] = v
		}
	}
	return cleaned
}

func decodeGoogleError(body io.Reader, status int) error {
	data, _ := io.ReadAll(body)
	var envelope struct {
		Error struct {
			Message string `json:"message"`
			Code    int    `json:"code"`
			Status  string `json:"status"`
		} `json:"error"`
	}
	if err := json.Unmarshal(data, &envelope); err == nil && envelope.Error.Message != "" {
		code := strings.TrimSpace(envelope.Error.Status)
		if code == "" && envelope.Error.Code != 0 {
			code = fmt.Sprintf("%d", envelope.Error.Code)
		}
		return providerAPIError("google", status, code, envelope.Error.Message)
	}
	return providerAPIError("google", status, "", strings.TrimSpace(string(data)))
}

func (c *GoogleClient) chatRequestHeaders(accept string) map[string]string {
	return buildProviderJSONHeaders(providerJSONHeadersOptions{
		Base:   c.headers,
		Accept: accept,
		Fields: []providerHeaderField{{
			Key:   "x-goog-api-key",
			Value: c.apiKey,
		}},
	})
}

package model

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/fulcrus/hopclaw/agent"
	"github.com/fulcrus/hopclaw/contextengine"
	"github.com/fulcrus/hopclaw/internal/support/normalize"
)

const (
	bedrockService = "bedrock-runtime"
	awsSigV4Algo   = "AWS4-HMAC-SHA256"
	awsTimeFormat  = "20060102T150405Z"
	awsDateFormat  = "20060102"
)

// BedrockConfig holds configuration for the AWS Bedrock Converse client.
type BedrockConfig struct {
	Region       string
	AccessKeyID  string
	SecretKey    string
	SessionToken string
	DefaultModel string
	Timeout      time.Duration
	Headers      map[string]string
}

// BedrockClient implements agent.ModelClient using the AWS Bedrock Converse API.
type BedrockClient struct {
	region       string
	accessKeyID  string
	secretKey    string
	sessionToken string
	defaultModel string
	httpClient   *http.Client
	headers      map[string]string
}

// NewBedrockClient creates a new BedrockClient. Credentials are resolved from
// the config first, then falling back to the standard AWS environment variables
// (AWS_ACCESS_KEY_ID, AWS_SECRET_ACCESS_KEY, AWS_SESSION_TOKEN, AWS_REGION).
func NewBedrockClient(cfg BedrockConfig) (*BedrockClient, error) {
	region := normalize.FirstNonEmpty(cfg.Region, os.Getenv("AWS_REGION"), os.Getenv("AWS_DEFAULT_REGION"))
	if region == "" {
		return nil, fmt.Errorf("bedrock: region is required (set BedrockConfig.Region or AWS_REGION)")
	}
	accessKeyID := normalize.FirstNonEmpty(cfg.AccessKeyID, os.Getenv("AWS_ACCESS_KEY_ID"))
	secretKey := normalize.FirstNonEmpty(cfg.SecretKey, os.Getenv("AWS_SECRET_ACCESS_KEY"))
	if accessKeyID == "" || secretKey == "" {
		return nil, fmt.Errorf("bedrock: AWS credentials are required (set config or AWS_ACCESS_KEY_ID / AWS_SECRET_ACCESS_KEY)")
	}
	sessionToken := normalize.FirstNonEmpty(cfg.SessionToken, os.Getenv("AWS_SESSION_TOKEN"))

	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = 120 * time.Second
	}

	return &BedrockClient{
		region:       region,
		accessKeyID:  accessKeyID,
		secretKey:    secretKey,
		sessionToken: sessionToken,
		defaultModel: strings.TrimSpace(cfg.DefaultModel),
		httpClient:   newStreamingHTTPClient(timeout),
		headers:      cloneHeaders(cfg.Headers),
	}, nil
}

// Chat sends a Converse request to AWS Bedrock and returns the model response.
func (c *BedrockClient) Chat(ctx context.Context, req agent.ChatRequest) (*agent.ModelResponse, error) {
	model := defaultString(req.Model, c.defaultModel)
	if model == "" {
		return nil, fmt.Errorf("bedrock: model is required")
	}

	body := bedrockRequest{}

	// System prompt.
	if strings.TrimSpace(req.SystemPrompt) != "" {
		body.System = []bedrockTextBlock{{Text: req.SystemPrompt}}
	}

	// Convert messages.
	wireToInternal := make(map[string]string)
	body.Messages = toBedrockMessages(req.Messages, wireToInternal)

	// Inference config.
	maxTokens := req.Budget.ReservedOutput
	if maxTokens <= 0 {
		maxTokens = 4096
	}
	body.InferenceConfig = &bedrockInferenceConfig{
		MaxTokens:   maxTokens,
		Temperature: req.Temperature,
	}

	// Tools.
	if len(req.Tools) > 0 {
		tools := make([]bedrockToolDef, 0, len(req.Tools))
		for _, tool := range req.Tools {
			wireName := sanitizeToolName(tool.Name)
			wireToInternal[wireName] = tool.Name
			tools = append(tools, bedrockToolDef{
				ToolSpec: bedrockToolSpec{
					Name:        wireName,
					Description: tool.Description,
					InputSchema: bedrockInputSchema{JSON: tool.InputSchema},
				},
			})
		}
		body.ToolConfig = &bedrockToolConfig{Tools: tools}
	}

	payload, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("bedrock: marshal request: %w", err)
	}

	endpoint := fmt.Sprintf("https://%s.%s.amazonaws.com/model/%s/converse",
		bedrockService, c.region, model)

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("bedrock: create request: %w", err)
	}
	for key, value := range c.headers {
		httpReq.Header.Set(key, value)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	if err := c.signRequest(httpReq, payload); err != nil {
		return nil, fmt.Errorf("bedrock: sign request: %w", err)
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("bedrock: send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return nil, decodeBedrockError(resp.Body, resp.StatusCode)
	}

	var data bedrockResponse
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil, fmt.Errorf("bedrock: decode response: %w", err)
	}
	return fromBedrockResponse(data, wireToInternal), nil
}

func (c *BedrockClient) ChatStream(ctx context.Context, req agent.ChatRequest, cb agent.StreamCallback) (*agent.ModelResponse, error) {
	return streamModelResponseFallback(ctx, streamFallbackModelFunc(func(ctx context.Context, req agent.ChatRequest) (*agent.ModelResponse, error) {
		return c.Chat(ctx, req)
	}), req, cb)
}

// --- Request types ---

type bedrockRequest struct {
	Messages        []bedrockMessage        `json:"messages"`
	System          []bedrockTextBlock      `json:"system,omitempty"`
	InferenceConfig *bedrockInferenceConfig `json:"inferenceConfig,omitempty"`
	ToolConfig      *bedrockToolConfig      `json:"toolConfig,omitempty"`
}

type bedrockMessage struct {
	Role    string                `json:"role"`
	Content []bedrockContentBlock `json:"content"`
}

type bedrockContentBlock struct {
	Text       string             `json:"text,omitempty"`
	Image      *bedrockImageBlock `json:"image,omitempty"`
	ToolUse    *bedrockToolUse    `json:"toolUse,omitempty"`
	ToolResult *bedrockToolResult `json:"toolResult,omitempty"`
}

// bedrockImageBlock represents an image in the Bedrock Converse API format.
type bedrockImageBlock struct {
	Format string             `json:"format"`
	Source bedrockImageSource `json:"source"`
}

// bedrockImageSource holds the bytes payload for a Bedrock image block.
type bedrockImageSource struct {
	Bytes string `json:"bytes"` // base64-encoded image data
}

type bedrockTextBlock struct {
	Text string `json:"text"`
}

type bedrockToolUse struct {
	ToolUseID string         `json:"toolUseId"`
	Name      string         `json:"name"`
	Input     map[string]any `json:"input"`
}

type bedrockToolResult struct {
	ToolUseID string             `json:"toolUseId"`
	Content   []bedrockTextBlock `json:"content"`
}

type bedrockInferenceConfig struct {
	MaxTokens   int     `json:"maxTokens"`
	Temperature float64 `json:"temperature,omitempty"`
}

type bedrockToolConfig struct {
	Tools []bedrockToolDef `json:"tools"`
}

type bedrockToolDef struct {
	ToolSpec bedrockToolSpec `json:"toolSpec"`
}

type bedrockToolSpec struct {
	Name        string             `json:"name"`
	Description string             `json:"description,omitempty"`
	InputSchema bedrockInputSchema `json:"inputSchema"`
}

type bedrockInputSchema struct {
	JSON map[string]any `json:"json"`
}

// --- Response types ---

type bedrockResponse struct {
	Output     bedrockOutput `json:"output"`
	Usage      bedrockUsage  `json:"usage"`
	StopReason string        `json:"stopReason"`
}

type bedrockOutput struct {
	Message bedrockMessage `json:"message"`
}

type bedrockUsage struct {
	InputTokens  int `json:"inputTokens"`
	OutputTokens int `json:"outputTokens"`
}

// --- Conversion helpers ---

// toBedrockMessages converts internal messages to the Bedrock Converse format.
// Bedrock expects strict user/assistant alternation; consecutive same-role
// messages are merged, and tool results are wrapped in user messages.
func toBedrockMessages(msgs []contextengine.Message, wireToInternal map[string]string) []bedrockMessage {
	var out []bedrockMessage
	for _, msg := range msgs {
		switch msg.Role {
		case contextengine.RoleAssistant:
			blocks := make([]bedrockContentBlock, 0, len(msg.ToolCalls)+1)
			if strings.TrimSpace(msg.Content) != "" {
				blocks = append(blocks, bedrockContentBlock{Text: msg.Content})
			}
			for _, tc := range msg.ToolCalls {
				wireName := sanitizeToolName(tc.Name)
				wireToInternal[wireName] = tc.Name
				blocks = append(blocks, bedrockContentBlock{
					ToolUse: &bedrockToolUse{
						ToolUseID: tc.ID,
						Name:      wireName,
						Input:     argumentsOrParseError(tc.Arguments),
					},
				})
			}
			if len(blocks) > 0 {
				out = mergeBedrockMessage(out, bedrockMessage{Role: "assistant", Content: blocks})
			}

		case contextengine.RoleTool:
			block := bedrockContentBlock{
				ToolResult: &bedrockToolResult{
					ToolUseID: msg.ToolCallID,
					Content:   []bedrockTextBlock{{Text: msg.Content}},
				},
			}
			out = mergeBedrockMessage(out, bedrockMessage{Role: "user", Content: []bedrockContentBlock{block}})

		case contextengine.RoleSystem:
			// System messages in the conversation are sent as user text.
			block := bedrockContentBlock{Text: msg.TextContent()}
			out = mergeBedrockMessage(out, bedrockMessage{Role: "user", Content: []bedrockContentBlock{block}})

		default: // user
			var blocks []bedrockContentBlock
			if msg.HasImageContent() {
				blocks = toBedrockContentBlocks(msg.ContentBlocks)
			} else {
				blocks = []bedrockContentBlock{{Text: msg.TextContent()}}
			}
			out = mergeBedrockMessage(out, bedrockMessage{Role: "user", Content: blocks})
		}
	}
	return out
}

// toBedrockContentBlocks converts contextengine.ContentBlock slices into
// the Bedrock Converse API content block format.
func toBedrockContentBlocks(blocks []contextengine.ContentBlock) []bedrockContentBlock {
	out := make([]bedrockContentBlock, 0, len(blocks))
	for _, block := range blocks {
		switch block.Type {
		case contextengine.ContentBlockText:
			if block.Text != "" {
				out = append(out, bedrockContentBlock{Text: block.Text})
			}
		case contextengine.ContentBlockImage:
			if block.Data != "" {
				out = append(out, bedrockContentBlock{
					Image: &bedrockImageBlock{
						Format: bedrockImageFormat(block.MediaType),
						Source: bedrockImageSource{Bytes: block.Data},
					},
				})
			}
		}
	}
	return out
}

// bedrockImageFormat extracts the image format from a MIME type string.
// Bedrock expects "jpeg", "png", "gif", or "webp" rather than the full MIME type.
func bedrockImageFormat(mediaType string) string {
	switch mediaType {
	case "image/jpeg":
		return "jpeg"
	case "image/png":
		return "png"
	case "image/gif":
		return "gif"
	case "image/webp":
		return "webp"
	default:
		// Best-effort: strip the "image/" prefix if present.
		if strings.HasPrefix(mediaType, "image/") {
			return strings.TrimPrefix(mediaType, "image/")
		}
		return mediaType
	}
}

// mergeBedrockMessage appends a message, merging content blocks into the last
// message if it has the same role (Bedrock requires strict alternation).
func mergeBedrockMessage(msgs []bedrockMessage, next bedrockMessage) []bedrockMessage {
	if len(msgs) > 0 && msgs[len(msgs)-1].Role == next.Role {
		msgs[len(msgs)-1].Content = append(msgs[len(msgs)-1].Content, next.Content...)
		return msgs
	}
	return append(msgs, next)
}

func fromBedrockResponse(resp bedrockResponse, wireToInternal map[string]string) *agent.ModelResponse {
	result := &agent.ModelResponse{
		Message: contextengine.Message{
			Role: contextengine.RoleAssistant,
		},
	}

	var textParts []string
	for _, block := range resp.Output.Message.Content {
		if block.Text != "" {
			textParts = append(textParts, block.Text)
		}
		if block.ToolUse != nil {
			result.ToolCalls = append(result.ToolCalls, agent.ToolCall{
				ID:    block.ToolUse.ToolUseID,
				Name:  restoreToolName(block.ToolUse.Name, wireToInternal),
				Input: block.ToolUse.Input,
			})
		}
	}
	result.Message.Content = strings.Join(textParts, "")
	if resp.Usage.InputTokens > 0 || resp.Usage.OutputTokens > 0 {
		total := resp.Usage.InputTokens + resp.Usage.OutputTokens
		result.Usage = &agent.ModelUsageInfo{
			PromptTokens:     resp.Usage.InputTokens,
			CompletionTokens: resp.Usage.OutputTokens,
			TotalTokens:      total,
		}
	}
	return result
}

func decodeBedrockError(body io.Reader, status int) error {
	data, _ := io.ReadAll(body)
	var envelope struct {
		Message string `json:"message"`
	}
	if err := json.Unmarshal(data, &envelope); err == nil && envelope.Message != "" {
		return providerAPIError("bedrock", status, "", envelope.Message)
	}
	return providerAPIError("bedrock", status, "", strings.TrimSpace(string(data)))
}

// --- AWS Signature V4 ---

// signRequest applies AWS Signature V4 to the given HTTP request.
func (c *BedrockClient) signRequest(req *http.Request, payload []byte) error {
	now := time.Now().UTC()
	datestamp := now.Format(awsDateFormat)
	amzDate := now.Format(awsTimeFormat)

	req.Header.Set("X-Amz-Date", amzDate)
	if c.sessionToken != "" {
		req.Header.Set("X-Amz-Security-Token", c.sessionToken)
	}

	host := req.URL.Host
	req.Header.Set("Host", host)

	payloadHash := sha256Hex(payload)
	req.Header.Set("X-Amz-Content-Sha256", payloadHash)

	// 1. Canonical request.
	signedHeaders, canonicalHeaders := buildCanonicalHeaders(req)
	canonicalURI := req.URL.Path
	if canonicalURI == "" {
		canonicalURI = "/"
	}
	canonicalQueryString := req.URL.RawQuery

	canonicalRequest := strings.Join([]string{
		req.Method,
		canonicalURI,
		canonicalQueryString,
		canonicalHeaders,
		signedHeaders,
		payloadHash,
	}, "\n")

	// 2. String to sign.
	credentialScope := fmt.Sprintf("%s/%s/%s/aws4_request", datestamp, c.region, bedrockService)
	stringToSign := strings.Join([]string{
		awsSigV4Algo,
		amzDate,
		credentialScope,
		sha256Hex([]byte(canonicalRequest)),
	}, "\n")

	// 3. Signing key.
	signingKey := deriveSigningKey(c.secretKey, datestamp, c.region, bedrockService)

	// 4. Signature.
	signature := hex.EncodeToString(hmacSHA256(signingKey, []byte(stringToSign)))

	// 5. Authorization header.
	authHeader := fmt.Sprintf("%s Credential=%s/%s, SignedHeaders=%s, Signature=%s",
		awsSigV4Algo, c.accessKeyID, credentialScope, signedHeaders, signature)
	req.Header.Set("Authorization", authHeader)

	return nil
}

// buildCanonicalHeaders returns the signed-headers list and the canonical
// headers string as defined by AWS Signature V4.
func buildCanonicalHeaders(req *http.Request) (signedHeaders, canonicalHeaders string) {
	type headerEntry struct {
		key   string
		value string
	}
	var entries []headerEntry
	for key, values := range req.Header {
		lk := strings.ToLower(key)
		entries = append(entries, headerEntry{
			key:   lk,
			value: strings.TrimSpace(values[0]),
		})
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].key < entries[j].key
	})

	var headerKeys []string
	var canonicalBuf strings.Builder
	for _, e := range entries {
		headerKeys = append(headerKeys, e.key)
		canonicalBuf.WriteString(e.key)
		canonicalBuf.WriteByte(':')
		canonicalBuf.WriteString(e.value)
		canonicalBuf.WriteByte('\n')
	}
	signedHeaders = strings.Join(headerKeys, ";")
	canonicalHeaders = canonicalBuf.String()
	return
}

// deriveSigningKey derives the signing key for AWS Signature V4.
//
//	kDate    = HMAC-SHA256("AWS4" + secret, datestamp)
//	kRegion  = HMAC-SHA256(kDate, region)
//	kService = HMAC-SHA256(kRegion, service)
//	kSigning = HMAC-SHA256(kService, "aws4_request")
func deriveSigningKey(secret, datestamp, region, service string) []byte {
	kDate := hmacSHA256([]byte("AWS4"+secret), []byte(datestamp))
	kRegion := hmacSHA256(kDate, []byte(region))
	kService := hmacSHA256(kRegion, []byte(service))
	kSigning := hmacSHA256(kService, []byte("aws4_request"))
	return kSigning
}

func hmacSHA256(key, data []byte) []byte {
	mac := hmac.New(sha256.New, key)
	mac.Write(data)
	return mac.Sum(nil)
}

func sha256Hex(data []byte) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}

package model

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/fulcrus/hopclaw/agent"
)

type streamFallbackModelFunc func(context.Context, agent.ChatRequest) (*agent.ModelResponse, error)

type ProviderErrorClass string

const (
	ProviderErrorClassUnknown        ProviderErrorClass = ""
	ProviderErrorClassRateLimit      ProviderErrorClass = "rate_limit"
	ProviderErrorClassAuth           ProviderErrorClass = "auth"
	ProviderErrorClassInvalidRequest ProviderErrorClass = "invalid_request"
	ProviderErrorClassTransient      ProviderErrorClass = "transient"
)

// ProviderRequestMetadata describes one outbound provider HTTP request attempt.
type ProviderRequestMetadata struct {
	API       ProviderAPI
	Operation string
	Method    string
	Model     string
	Endpoint  string
	Streaming bool
	Attempt   int
}

// ProviderRequestHook exposes a narrow request lifecycle contract for
// instrumenting or adjusting provider HTTP requests without forking each
// provider client implementation.
type ProviderRequestHook struct {
	BeforeRequest func(context.Context, ProviderRequestMetadata, *http.Request) error
	AfterResponse func(context.Context, ProviderRequestMetadata, *http.Response, error)
}

type ProviderAPIError struct {
	Provider   string
	StatusCode int
	Code       string
	Message    string
	Class      ProviderErrorClass
	Retryable  bool
}

func (e *ProviderAPIError) Error() string {
	if e == nil {
		return ""
	}
	codeSuffix := ""
	if code := strings.TrimSpace(e.Code); code != "" {
		codeSuffix = "/" + code
	}
	return fmt.Sprintf("%s API error (%d%s): %s", strings.TrimSpace(e.Provider), e.StatusCode, codeSuffix, strings.TrimSpace(e.Message))
}

const (
	defaultStreamingDialTimeout         = 30 * time.Second
	defaultStreamingKeepAlive           = 30 * time.Second
	defaultStreamingTLSHandshakeTimeout = 10 * time.Second
	defaultStreamingIdleConnTimeout     = 90 * time.Second
	defaultStreamingExpectContinue      = 1 * time.Second

	defaultSSEScannerInitialBuffer = 64 * 1024
	defaultSSEScannerMaxBuffer     = 1024 * 1024

	defaultProviderRetryMaxAttempts = 3
	defaultProviderRetryBaseDelay   = 1 * time.Second
)

func (fn streamFallbackModelFunc) Chat(ctx context.Context, req agent.ChatRequest) (*agent.ModelResponse, error) {
	return fn(ctx, req)
}

func providerAPIError(provider string, status int, code, message string) error {
	class := classifyProviderAPIError(status, code, message)
	return &ProviderAPIError{
		Provider:   strings.TrimSpace(provider),
		StatusCode: status,
		Code:       strings.TrimSpace(code),
		Message:    normalizedProviderErrorMessage(status, message),
		Class:      class,
		Retryable:  class == ProviderErrorClassRateLimit || class == ProviderErrorClassTransient,
	}
}

func normalizedProviderErrorMessage(status int, message string) string {
	message = strings.TrimSpace(message)
	if message != "" {
		return message
	}
	if text := strings.TrimSpace(http.StatusText(status)); text != "" {
		return text
	}
	return "request failed"
}

func classifyProviderAPIError(status int, code, message string) ProviderErrorClass {
	lowerCode := strings.ToLower(strings.TrimSpace(code))
	lowerMessage := strings.ToLower(strings.TrimSpace(message))
	switch {
	case status == http.StatusTooManyRequests,
		containsProviderErrorSignal(lowerCode, "rate_limit", "resource_exhausted", "quota"),
		containsProviderErrorSignal(lowerMessage, "rate limit", "too many requests", "quota exceeded", "resource exhausted"):
		return ProviderErrorClassRateLimit
	case status == http.StatusUnauthorized,
		status == http.StatusForbidden,
		containsProviderErrorSignal(lowerCode, "auth", "unauthorized", "permission_denied", "forbidden", "invalid_api_key"),
		containsProviderErrorSignal(lowerMessage, "unauthorized", "forbidden", "invalid api key", "authentication failed", "permission denied"):
		return ProviderErrorClassAuth
	case status == http.StatusRequestTimeout,
		status == http.StatusConflict,
		status == http.StatusTooEarly,
		status == http.StatusInternalServerError,
		status == http.StatusBadGateway,
		status == http.StatusServiceUnavailable,
		status == http.StatusGatewayTimeout:
		return ProviderErrorClassTransient
	case status >= 400 && status < 500:
		return ProviderErrorClassInvalidRequest
	default:
		return ProviderErrorClassUnknown
	}
}

func containsProviderErrorSignal(text string, signals ...string) bool {
	if strings.TrimSpace(text) == "" {
		return false
	}
	for _, signal := range signals {
		if strings.Contains(text, signal) {
			return true
		}
	}
	return false
}

func providerErrorCode(value any) string {
	switch typed := value.(type) {
	case nil:
		return ""
	case string:
		return strings.TrimSpace(typed)
	default:
		rendered := strings.TrimSpace(fmt.Sprint(typed))
		if rendered == "<nil>" {
			return ""
		}
		return rendered
	}
}

func asProviderAPIError(err error) (*ProviderAPIError, bool) {
	if err == nil {
		return nil, false
	}
	var target *ProviderAPIError
	if !errors.As(err, &target) || target == nil {
		return nil, false
	}
	return target, true
}

func isProviderKeyRotatable(err error) bool {
	typed, ok := asProviderAPIError(err)
	if !ok {
		return false
	}
	return typed.Class == ProviderErrorClassRateLimit || typed.Class == ProviderErrorClassAuth
}

func isProviderRateLimitError(err error) bool {
	typed, ok := asProviderAPIError(err)
	if !ok {
		return false
	}
	return typed.Class == ProviderErrorClassRateLimit
}

func defaultString(v, fallback string) string {
	if strings.TrimSpace(v) != "" {
		return strings.TrimSpace(v)
	}
	return strings.TrimSpace(fallback)
}

func restoreToolName(name string, wireToInternal map[string]string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	if original, ok := wireToInternal[name]; ok && strings.TrimSpace(original) != "" {
		return strings.TrimSpace(original)
	}
	if strings.Contains(name, "_x") {
		return desanitizeToolName(name)
	}
	return name
}

func cloneHeaders(in map[string]string) map[string]string {
	if in == nil {
		return nil
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func cloneProviderRequestHooks(in []ProviderRequestHook) []ProviderRequestHook {
	if len(in) == 0 {
		return nil
	}
	out := make([]ProviderRequestHook, len(in))
	copy(out, in)
	return out
}

type providerHeaderField struct {
	Key      string
	Value    string
	IfAbsent bool
}

type providerJSONHeadersOptions struct {
	Base        map[string]string
	Accept      string
	BearerToken string
	Fields      []providerHeaderField
}

type providerHeaderMap struct {
	values map[string]string
}

func newProviderHeaderMap(base map[string]string) providerHeaderMap {
	if len(base) == 0 {
		return providerHeaderMap{values: make(map[string]string)}
	}
	keys := make([]string, 0, len(base))
	for key := range base {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		left := canonicalProviderHeaderKey(keys[i])
		right := canonicalProviderHeaderKey(keys[j])
		if left == right {
			return keys[i] < keys[j]
		}
		return left < right
	})

	headers := providerHeaderMap{values: make(map[string]string, len(base)+4)}
	for _, key := range keys {
		headers.Set(key, base[key])
	}
	return headers
}

func canonicalProviderHeaderKey(key string) string {
	return http.CanonicalHeaderKey(strings.TrimSpace(key))
}

func (h *providerHeaderMap) Set(key, value string) {
	key = canonicalProviderHeaderKey(key)
	if key == "" {
		return
	}
	if h.values == nil {
		h.values = make(map[string]string)
	}
	h.values[key] = value
}

func (h *providerHeaderMap) SetIfAbsent(key, value string) {
	if h.Get(key) != "" {
		return
	}
	h.Set(key, value)
}

func (h providerHeaderMap) Get(key string) string {
	if len(h.values) == 0 {
		return ""
	}
	return strings.TrimSpace(h.values[canonicalProviderHeaderKey(key)])
}

func (h providerHeaderMap) Map() map[string]string {
	return cloneHeaders(h.values)
}

func buildProviderJSONHeaders(opts providerJSONHeadersOptions) map[string]string {
	headers := newProviderHeaderMap(opts.Base)
	headers.Set("Content-Type", "application/json")
	if accept := strings.TrimSpace(opts.Accept); accept != "" {
		headers.Set("Accept", accept)
	}
	if token := strings.TrimSpace(opts.BearerToken); token != "" {
		headers.Set("Authorization", "Bearer "+token)
	}
	for _, field := range opts.Fields {
		if field.IfAbsent {
			headers.SetIfAbsent(field.Key, field.Value)
			continue
		}
		headers.Set(field.Key, field.Value)
	}
	return headers.Map()
}

func newStreamingHTTPClient(timeout time.Duration) *http.Client {
	if timeout < 0 {
		timeout = 0
	}
	return &http.Client{
		Transport: &http.Transport{
			DialContext: (&net.Dialer{
				Timeout:   defaultStreamingDialTimeout,
				KeepAlive: defaultStreamingKeepAlive,
			}).DialContext,
			TLSHandshakeTimeout:   defaultStreamingTLSHandshakeTimeout,
			ResponseHeaderTimeout: timeout,
			IdleConnTimeout:       defaultStreamingIdleConnTimeout,
			ExpectContinueTimeout: defaultStreamingExpectContinue,
		},
	}
}

type providerRequestOptions struct {
	Client            *http.Client
	NewRequest        func(context.Context) (*http.Request, error)
	DecodeError       func(io.Reader, int) error
	Consume           func(io.Reader) (*agent.ModelResponse, error)
	Metadata          ProviderRequestMetadata
	Hooks             []ProviderRequestHook
	MaxAttempts       int
	RetryBaseDelay    time.Duration
	RetryOnStatusCode func(int) bool
	RetryOnError      func(error) bool
}

type providerTransportProfile struct {
	MaxAttempts       int
	RetryBaseDelay    time.Duration
	RetryOnStatusCode func(int) bool
	RetryOnError      func(error) bool
}

func defaultProviderTransportProfile() providerTransportProfile {
	return providerTransportProfile{
		MaxAttempts:       defaultProviderRetryMaxAttempts,
		RetryBaseDelay:    defaultProviderRetryBaseDelay,
		RetryOnStatusCode: isRetryableProviderStatus,
		RetryOnError:      isRetryableProviderTransportError,
	}
}

func withProviderTransportProfile(opts providerRequestOptions, profile providerTransportProfile) providerRequestOptions {
	if opts.MaxAttempts <= 0 {
		opts.MaxAttempts = profile.MaxAttempts
	}
	if opts.RetryBaseDelay <= 0 {
		opts.RetryBaseDelay = profile.RetryBaseDelay
	}
	if opts.RetryOnStatusCode == nil {
		opts.RetryOnStatusCode = profile.RetryOnStatusCode
	}
	if opts.RetryOnError == nil {
		opts.RetryOnError = profile.RetryOnError
	}
	return opts
}

func newProviderRequestOptions(
	client *http.Client,
	newRequest func(context.Context) (*http.Request, error),
	decodeError func(io.Reader, int) error,
	consume func(io.Reader) (*agent.ModelResponse, error),
) providerRequestOptions {
	return withProviderTransportProfile(providerRequestOptions{
		Client:      client,
		NewRequest:  newRequest,
		DecodeError: decodeError,
		Consume:     consume,
	}, defaultProviderTransportProfile())
}

func newProviderJSONRequestOptions(
	client *http.Client,
	metadata ProviderRequestMetadata,
	hooks []ProviderRequestHook,
	method, endpoint string,
	payload []byte,
	headers map[string]string,
	decodeError func(io.Reader, int) error,
	consume func(io.Reader) (*agent.ModelResponse, error),
) providerRequestOptions {
	opts := newProviderRequestOptions(
		client,
		func(ctx context.Context) (*http.Request, error) {
			return buildProviderJSONRequest(ctx, method, endpoint, payload, headers)
		},
		decodeError,
		consume,
	)
	opts.Metadata = metadata
	opts.Hooks = cloneProviderRequestHooks(hooks)
	return opts
}

func isRetryableProviderStatus(status int) bool {
	return status >= http.StatusInternalServerError
}

func isRetryableProviderTransportError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		return true
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return true
	}
	lower := strings.ToLower(strings.TrimSpace(err.Error()))
	return strings.Contains(lower, "connection reset") ||
		strings.Contains(lower, "broken pipe") ||
		strings.Contains(lower, "unexpected eof")
}

type sseEvent struct {
	Event string
	Data  string
}

func executeProviderRequest(ctx context.Context, opts providerRequestOptions) (*agent.ModelResponse, error) {
	if opts.Client == nil {
		return nil, errors.New("provider http client is required")
	}
	if opts.NewRequest == nil {
		return nil, errors.New("provider request factory is required")
	}
	if opts.DecodeError == nil {
		return nil, errors.New("provider error decoder is required")
	}
	if opts.Consume == nil {
		return nil, errors.New("provider response consumer is required")
	}
	if opts.MaxAttempts <= 0 {
		opts.MaxAttempts = 1
	}
	if opts.RetryBaseDelay <= 0 {
		opts.RetryBaseDelay = time.Second
	}

	var lastErr error
	for attempt := 0; attempt < opts.MaxAttempts; attempt++ {
		if attempt > 0 {
			if err := waitProviderRetry(ctx, attempt, opts.RetryBaseDelay); err != nil {
				return nil, err
			}
		}

		req, err := opts.NewRequest(ctx)
		if err != nil {
			return nil, err
		}
		meta := opts.Metadata
		meta.Attempt = attempt + 1
		if err := callProviderBeforeRequestHooks(ctx, opts.Hooks, meta, req); err != nil {
			return nil, err
		}

		resp, err := opts.Client.Do(req)
		if err != nil {
			lastErr = err
			callProviderAfterResponseHooks(ctx, opts.Hooks, meta, nil, err)
			if attempt+1 < opts.MaxAttempts && opts.RetryOnError != nil && opts.RetryOnError(err) {
				continue
			}
			return nil, err
		}

		if resp.StatusCode >= 400 {
			err = opts.DecodeError(resp.Body, resp.StatusCode)
			resp.Body.Close()
			lastErr = err
			callProviderAfterResponseHooks(ctx, opts.Hooks, meta, resp, err)
			if attempt+1 < opts.MaxAttempts && opts.RetryOnStatusCode != nil && opts.RetryOnStatusCode(resp.StatusCode) {
				continue
			}
			return nil, err
		}

		result, err := opts.Consume(resp.Body)
		resp.Body.Close()
		callProviderAfterResponseHooks(ctx, opts.Hooks, meta, resp, err)
		if err != nil {
			lastErr = err
			if attempt+1 < opts.MaxAttempts && opts.RetryOnError != nil && opts.RetryOnError(err) {
				continue
			}
		}
		return result, err
	}

	if lastErr == nil {
		lastErr = errors.New("provider request failed")
	}
	return nil, lastErr
}

func callProviderBeforeRequestHooks(ctx context.Context, hooks []ProviderRequestHook, meta ProviderRequestMetadata, req *http.Request) error {
	for _, hook := range hooks {
		if hook.BeforeRequest == nil {
			continue
		}
		if err := hook.BeforeRequest(ctx, meta, req); err != nil {
			return err
		}
	}
	return nil
}

func callProviderAfterResponseHooks(ctx context.Context, hooks []ProviderRequestHook, meta ProviderRequestMetadata, resp *http.Response, err error) {
	for _, hook := range hooks {
		if hook.AfterResponse == nil {
			continue
		}
		hook.AfterResponse(ctx, meta, resp, err)
	}
}

func waitProviderRetry(ctx context.Context, attempt int, baseDelay time.Duration) error {
	if attempt <= 0 {
		return nil
	}
	delay := time.Duration(attempt) * baseDelay
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func buildProviderJSONRequest(ctx context.Context, method, endpoint string, payload []byte, headers map[string]string) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, method, endpoint, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	return req, nil
}

func newSSEScanner(body io.Reader) *bufio.Scanner {
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 0, defaultSSEScannerInitialBuffer), defaultSSEScannerMaxBuffer)
	return scanner
}

func nextSSEEvent(scanner *bufio.Scanner) (sseEvent, bool, error) {
	var (
		eventType string
		dataLines []string
	)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			if eventType == "" && len(dataLines) == 0 {
				continue
			}
			return sseEvent{
				Event: strings.TrimSpace(eventType),
				Data:  strings.Join(dataLines, "\n"),
			}, true, nil
		}
		switch {
		case strings.HasPrefix(line, "event:"):
			eventType = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
		case strings.HasPrefix(line, "data:"):
			data := strings.TrimPrefix(line, "data:")
			data = strings.TrimPrefix(data, " ")
			dataLines = append(dataLines, data)
		}
	}
	if err := scanner.Err(); err != nil {
		return sseEvent{}, false, err
	}
	if eventType != "" || len(dataLines) > 0 {
		return sseEvent{
			Event: strings.TrimSpace(eventType),
			Data:  strings.Join(dataLines, "\n"),
		}, true, nil
	}
	return sseEvent{}, false, io.EOF
}

// sanitizeToolName converts internal tool names into a provider-safe,
// collision-free wire representation that only uses [A-Za-z0-9_-].
// Characters outside [A-Za-z0-9-] are escaped as _xHH_ byte sequences.
// desanitizeToolName reverses the _xHH_ encoding applied by sanitizeToolName.
// "skill_x2E_ensure" -> "skill.ensure"
func desanitizeToolName(name string) string {
	var builder strings.Builder
	builder.Grow(len(name))
	i := 0
	for i < len(name) {
		if i+4 < len(name) && name[i] == '_' && name[i+1] == 'x' && name[i+4] == '_' {
			hexStr := name[i+2 : i+4]
			var ch byte
			if _, err := fmt.Sscanf(hexStr, "%02X", &ch); err == nil {
				builder.WriteByte(ch)
				i += 5
				continue
			}
			if _, err := fmt.Sscanf(hexStr, "%02x", &ch); err == nil {
				builder.WriteByte(ch)
				i += 5
				continue
			}
		}
		builder.WriteByte(name[i])
		i++
	}
	return builder.String()
}

func sanitizeToolName(name string) string {
	var builder strings.Builder
	builder.Grow(len(name) + 8)
	for i := 0; i < len(name); i++ {
		ch := name[i]
		if (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || (ch >= '0' && ch <= '9') || ch == '-' {
			builder.WriteByte(ch)
			continue
		}
		builder.WriteString(fmt.Sprintf("_x%02X_", ch))
	}
	return builder.String()
}

func streamModelResponseFallback(ctx context.Context, client agent.ModelClient, req agent.ChatRequest, cb agent.StreamCallback) (*agent.ModelResponse, error) {
	resp, err := client.Chat(ctx, req)
	if err != nil {
		if cb != nil {
			cb.OnError(ctx, err)
		}
		return nil, err
	}
	if cb == nil {
		return resp, nil
	}
	if resp != nil && strings.TrimSpace(resp.Message.Content) != "" {
		cb.OnTextDelta(ctx, resp.Message.Content)
	}
	if resp != nil {
		for index, toolCall := range resp.ToolCalls {
			toolCallID := strings.TrimSpace(toolCall.ID)
			if toolCallID == "" {
				toolCallID = fmt.Sprintf("tool-call-%d", index)
			}
			cb.OnToolCallStart(ctx, toolCallID, strings.TrimSpace(toolCall.Name))
			if payload := marshalToolCallInput(toolCall.Input); payload != "" {
				cb.OnToolCallDelta(ctx, toolCallID, payload)
			}
		}
	}
	cb.OnComplete(ctx)
	return resp, nil
}

func marshalToolCallInput(input map[string]any) string {
	if len(input) == 0 {
		return "{}"
	}
	data, err := json.Marshal(input)
	if err != nil {
		return "{}"
	}
	return string(data)
}

func parseArguments(raw string) (map[string]any, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	var args map[string]any
	if err := json.Unmarshal([]byte(raw), &args); err != nil {
		return nil, fmt.Errorf("malformed tool arguments: %w", err)
	}
	return args, nil
}

func argumentsOrParseError(raw string) map[string]any {
	args, err := parseArguments(raw)
	if err == nil {
		return args
	}
	return map[string]any{
		"_parse_error":   err.Error(),
		"_raw_arguments": strings.TrimSpace(raw),
	}
}

func generatedToolCallID(provider string, index int) string {
	provider = strings.TrimSpace(provider)
	if provider == "" {
		provider = "tool"
	}
	return fmt.Sprintf("%s-call-%d", provider, index+1)
}

package model

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/fulcrus/hopclaw/agent"
	"github.com/fulcrus/hopclaw/contextengine"
)

func assertStreamingSafeHTTPClient(t *testing.T, client *http.Client, want time.Duration) {
	t.Helper()
	if client == nil {
		t.Fatal("expected http client")
	}
	if client.Timeout != 0 {
		t.Fatalf("client.Timeout = %s, want 0 for streaming-safe client", client.Timeout)
	}
	transport, ok := client.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("client.Transport = %T, want *http.Transport", client.Transport)
	}
	if transport.ResponseHeaderTimeout != want {
		t.Fatalf("ResponseHeaderTimeout = %s, want %s", transport.ResponseHeaderTimeout, want)
	}
}

func TestNewAnthropicClientUsesStreamingSafeTimeouts(t *testing.T) {
	t.Parallel()

	client, err := NewAnthropicClient(AnthropicConfig{
		APIKey:  "test-key",
		Timeout: 45 * time.Second,
	})
	if err != nil {
		t.Fatalf("NewAnthropicClient() error = %v", err)
	}
	assertStreamingSafeHTTPClient(t, client.httpClient, 45*time.Second)
}

func TestNewGoogleClientUsesStreamingSafeTimeouts(t *testing.T) {
	t.Parallel()

	client, err := NewGoogleClient(GoogleConfig{
		APIKey:  "test-key",
		Timeout: 45 * time.Second,
	})
	if err != nil {
		t.Fatalf("NewGoogleClient() error = %v", err)
	}
	assertStreamingSafeHTTPClient(t, client.httpClient, 45*time.Second)
}

func TestNewBedrockClientUsesStreamingSafeTimeouts(t *testing.T) {
	t.Parallel()

	client, err := NewBedrockClient(BedrockConfig{
		Region:      "us-west-2",
		AccessKeyID: "AKID",
		SecretKey:   "secret",
		Timeout:     45 * time.Second,
	})
	if err != nil {
		t.Fatalf("NewBedrockClient() error = %v", err)
	}
	assertStreamingSafeHTTPClient(t, client.httpClient, 45*time.Second)
}

func TestNewOpenAICompatClientUsesDefaultTimeoutWhenZero(t *testing.T) {
	t.Parallel()

	client, err := NewOpenAICompatClient(OpenAICompatConfig{
		BaseURL: "https://api.example.com/v1",
		Timeout: 0,
	})
	if err != nil {
		t.Fatalf("NewOpenAICompatClient() error = %v", err)
	}
	assertStreamingSafeHTTPClient(t, client.httpClient, 60*time.Second)
}

func TestToAnthropicMessagePreservesMalformedToolArguments(t *testing.T) {
	t.Parallel()

	wireToInternal := make(map[string]string)
	msg := toAnthropicMessage(contextengine.Message{
		Role: contextengine.RoleAssistant,
		ToolCalls: []contextengine.ToolCallRef{{
			ID:        "call-1",
			Name:      "fs.read",
			Arguments: "{\"path\":",
		}},
	}, wireToInternal)
	blocks, ok := msg.Content.([]anthropicContentBlock)
	if !ok {
		t.Fatalf("msg.Content type = %T, want []anthropicContentBlock", msg.Content)
	}
	if len(blocks) != 1 || blocks[0].Input["_parse_error"] == "" {
		t.Fatalf("tool input = %#v", msg.Content)
	}
}

func TestToBedrockMessagesPreservesMalformedToolArguments(t *testing.T) {
	t.Parallel()

	wireToInternal := make(map[string]string)
	msgs := toBedrockMessages([]contextengine.Message{{
		Role: contextengine.RoleAssistant,
		ToolCalls: []contextengine.ToolCallRef{{
			ID:        "call-1",
			Name:      "fs.read",
			Arguments: "{\"path\":",
		}},
	}}, wireToInternal)
	if len(msgs) != 1 || len(msgs[0].Content) != 1 || msgs[0].Content[0].ToolUse == nil {
		t.Fatalf("messages = %#v", msgs)
	}
	if msgs[0].Content[0].ToolUse.Input["_parse_error"] == "" {
		t.Fatalf("tool input = %#v", msgs[0].Content[0].ToolUse.Input)
	}
}

func TestProviderAPIErrorClassification(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		err       error
		wantClass ProviderErrorClass
		rotatable bool
		rateLimit bool
	}{
		{
			name:      "rate limit",
			err:       providerAPIError("openai-compatible", 429, "rate_limit_exceeded", "too many requests"),
			wantClass: ProviderErrorClassRateLimit,
			rotatable: true,
			rateLimit: true,
		},
		{
			name:      "auth",
			err:       providerAPIError("anthropic", 401, "invalid_api_key", "unauthorized"),
			wantClass: ProviderErrorClassAuth,
			rotatable: true,
		},
		{
			name:      "invalid request",
			err:       providerAPIError("google", 400, "INVALID_ARGUMENT", "bad request"),
			wantClass: ProviderErrorClassInvalidRequest,
		},
		{
			name:      "transient",
			err:       providerAPIError("bedrock", 503, "", "service unavailable"),
			wantClass: ProviderErrorClassTransient,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			typed, ok := asProviderAPIError(tt.err)
			if !ok {
				t.Fatalf("expected ProviderAPIError, got %T", tt.err)
			}
			if typed.Class != tt.wantClass {
				t.Fatalf("Class = %q, want %q", typed.Class, tt.wantClass)
			}
			if got := isProviderKeyRotatable(tt.err); got != tt.rotatable {
				t.Fatalf("isProviderKeyRotatable() = %v, want %v", got, tt.rotatable)
			}
			if got := isProviderRateLimitError(tt.err); got != tt.rateLimit {
				t.Fatalf("isProviderRateLimitError() = %v, want %v", got, tt.rateLimit)
			}
		})
	}
}

func TestBuildProviderJSONHeadersOverridesCaseInsensitiveDefaults(t *testing.T) {
	t.Parallel()

	headers := buildProviderJSONHeaders(providerJSONHeadersOptions{
		Base: map[string]string{
			"content-type":  "text/plain",
			"authorization": "Bearer stale",
			"ACCEPT":        "application/xml",
			"x-custom":      "keep-me",
		},
		Accept:      "text/event-stream",
		BearerToken: "fresh-token",
	})

	if got := headers["Content-Type"]; got != "application/json" {
		t.Fatalf("Content-Type = %q, want application/json", got)
	}
	if got := headers["Accept"]; got != "text/event-stream" {
		t.Fatalf("Accept = %q, want text/event-stream", got)
	}
	if got := headers["Authorization"]; got != "Bearer fresh-token" {
		t.Fatalf("Authorization = %q, want Bearer fresh-token", got)
	}
	if got := headers["X-Custom"]; got != "keep-me" {
		t.Fatalf("X-Custom = %q, want keep-me", got)
	}
	if _, ok := headers["content-type"]; ok {
		t.Fatalf("expected canonicalized headers, got raw content-type key in %#v", headers)
	}
	if _, ok := headers["authorization"]; ok {
		t.Fatalf("expected canonicalized headers, got raw authorization key in %#v", headers)
	}
}

func TestBuildProviderJSONHeadersSetIfAbsentPreservesExplicitHeaders(t *testing.T) {
	t.Parallel()

	headers := buildProviderJSONHeaders(providerJSONHeadersOptions{
		Base: map[string]string{
			"authorization": "Bearer custom",
			"user-agent":    "custom-agent/1.0",
		},
		Fields: []providerHeaderField{
			{Key: "Authorization", Value: "Bearer generated", IfAbsent: true},
			{Key: "User-Agent", Value: "generated-agent/1.0", IfAbsent: true},
			{Key: "x-api-key", Value: "test-key"},
		},
	})

	if got := headers["Authorization"]; got != "Bearer custom" {
		t.Fatalf("Authorization = %q, want Bearer custom", got)
	}
	if got := headers["User-Agent"]; got != "custom-agent/1.0" {
		t.Fatalf("User-Agent = %q, want custom-agent/1.0", got)
	}
	if got := headers["X-Api-Key"]; got != "test-key" {
		t.Fatalf("X-Api-Key = %q, want test-key", got)
	}
	if got := headers["Content-Type"]; got != "application/json" {
		t.Fatalf("Content-Type = %q, want application/json", got)
	}
}

func TestDefaultProviderTransportProfileUsesSharedRetryPolicy(t *testing.T) {
	t.Parallel()

	profile := defaultProviderTransportProfile()
	if profile.MaxAttempts != defaultProviderRetryMaxAttempts {
		t.Fatalf("MaxAttempts = %d, want %d", profile.MaxAttempts, defaultProviderRetryMaxAttempts)
	}
	if profile.RetryBaseDelay != defaultProviderRetryBaseDelay {
		t.Fatalf("RetryBaseDelay = %s, want %s", profile.RetryBaseDelay, defaultProviderRetryBaseDelay)
	}
	if !profile.RetryOnStatusCode(http.StatusBadGateway) {
		t.Fatal("expected 502 to be retryable")
	}
	if profile.RetryOnStatusCode(http.StatusTooManyRequests) {
		t.Fatal("expected 429 to remain non-retryable in the shared HTTP status policy")
	}
	if !profile.RetryOnError(io.ErrUnexpectedEOF) {
		t.Fatal("expected unexpected EOF to be retryable")
	}
}

func TestWithProviderTransportProfilePreservesExplicitOverrides(t *testing.T) {
	t.Parallel()

	customStatus := func(status int) bool { return status == http.StatusTooManyRequests }
	customError := func(err error) bool { return err != nil && err.Error() == "custom" }

	opts := withProviderTransportProfile(providerRequestOptions{
		MaxAttempts:       7,
		RetryBaseDelay:    250 * time.Millisecond,
		RetryOnStatusCode: customStatus,
		RetryOnError:      customError,
	}, defaultProviderTransportProfile())

	if opts.MaxAttempts != 7 {
		t.Fatalf("MaxAttempts = %d, want 7", opts.MaxAttempts)
	}
	if opts.RetryBaseDelay != 250*time.Millisecond {
		t.Fatalf("RetryBaseDelay = %s, want 250ms", opts.RetryBaseDelay)
	}
	if !opts.RetryOnStatusCode(http.StatusTooManyRequests) || opts.RetryOnStatusCode(http.StatusBadGateway) {
		t.Fatal("expected explicit RetryOnStatusCode to be preserved")
	}
	if !opts.RetryOnError(errors.New("custom")) || opts.RetryOnError(io.EOF) {
		t.Fatal("expected explicit RetryOnError to be preserved")
	}
}

func TestNewProviderRequestOptionsAppliesSharedTransportProfile(t *testing.T) {
	t.Parallel()

	opts := newProviderRequestOptions(
		&http.Client{},
		func(context.Context) (*http.Request, error) { return nil, nil },
		func(io.Reader, int) error { return nil },
		func(io.Reader) (*agent.ModelResponse, error) { return nil, nil },
	)

	if opts.MaxAttempts != defaultProviderRetryMaxAttempts {
		t.Fatalf("MaxAttempts = %d, want %d", opts.MaxAttempts, defaultProviderRetryMaxAttempts)
	}
	if opts.RetryBaseDelay != defaultProviderRetryBaseDelay {
		t.Fatalf("RetryBaseDelay = %s, want %s", opts.RetryBaseDelay, defaultProviderRetryBaseDelay)
	}
	if opts.RetryOnStatusCode == nil || !opts.RetryOnStatusCode(http.StatusServiceUnavailable) {
		t.Fatal("expected RetryOnStatusCode to be initialized")
	}
	if opts.RetryOnError == nil || !opts.RetryOnError(io.EOF) {
		t.Fatal("expected RetryOnError to be initialized")
	}
}

func TestIsRetryableProviderTransportError(t *testing.T) {
	t.Parallel()

	if !isRetryableProviderTransportError(io.EOF) {
		t.Fatal("expected EOF to be retryable")
	}
	if !isRetryableProviderTransportError(errors.New("write tcp: broken pipe")) {
		t.Fatal("expected broken pipe to be retryable")
	}
	if isRetryableProviderTransportError(context.DeadlineExceeded) {
		t.Fatal("expected context deadline exceeded to stay non-retryable")
	}
}

func TestExecuteProviderRequestInvokesHooksAcrossRetries(t *testing.T) {
	t.Parallel()

	attempts := 0
	beforeAttempts := make([]int, 0, 2)
	afterAttempts := make([]string, 0, 2)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if got := r.Header.Get("X-Hook-Attempt"); got != strconv.Itoa(attempts) {
			t.Fatalf("X-Hook-Attempt = %q, want %d", got, attempts)
		}
		if attempts == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte("retry later"))
			return
		}
		_, _ = w.Write([]byte("ok"))
	}))
	defer server.Close()

	opts := newProviderJSONRequestOptions(
		server.Client(),
		ProviderRequestMetadata{
			API:       APIOpenAICompletions,
			Operation: "test.request",
			Method:    http.MethodPost,
			Model:     "gpt-test",
			Endpoint:  server.URL,
		},
		[]ProviderRequestHook{{
			BeforeRequest: func(_ context.Context, meta ProviderRequestMetadata, req *http.Request) error {
				beforeAttempts = append(beforeAttempts, meta.Attempt)
				req.Header.Set("X-Hook-Attempt", strconv.Itoa(meta.Attempt))
				return nil
			},
			AfterResponse: func(_ context.Context, meta ProviderRequestMetadata, resp *http.Response, err error) {
				status := 0
				if resp != nil {
					status = resp.StatusCode
				}
				afterAttempts = append(afterAttempts, fmt.Sprintf("%d:%d:%v", meta.Attempt, status, err != nil))
			},
		}},
		http.MethodPost,
		server.URL,
		[]byte(`{}`),
		map[string]string{"Content-Type": "application/json"},
		func(body io.Reader, status int) error {
			data, _ := io.ReadAll(body)
			return providerAPIError("test", status, "", string(data))
		},
		func(body io.Reader) (*agent.ModelResponse, error) {
			data, err := io.ReadAll(body)
			if err != nil {
				return nil, err
			}
			return &agent.ModelResponse{Message: contextengine.Message{Content: string(data)}}, nil
		},
	)
	opts.MaxAttempts = 2
	opts.RetryBaseDelay = time.Millisecond

	resp, err := executeProviderRequest(context.Background(), opts)
	if err != nil {
		t.Fatalf("executeProviderRequest() error = %v", err)
	}
	if resp.Message.Content != "ok" {
		t.Fatalf("resp.Message.Content = %q, want ok", resp.Message.Content)
	}
	if attempts != 2 {
		t.Fatalf("attempts = %d, want 2", attempts)
	}
	if got := fmt.Sprint(beforeAttempts); got != "[1 2]" {
		t.Fatalf("before attempts = %s, want [1 2]", got)
	}
	if got := fmt.Sprint(afterAttempts); got != "[1:503:true 2:200:false]" {
		t.Fatalf("after attempts = %s", got)
	}
}

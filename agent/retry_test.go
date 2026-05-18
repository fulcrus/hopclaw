package agent

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/fulcrus/hopclaw/contextengine"
	"github.com/fulcrus/hopclaw/eventbus"
	"github.com/fulcrus/hopclaw/modelrouter"
)

func TestRetryDelayExponentialGrowth(t *testing.T) {
	t.Parallel()

	cfg := RetryConfig{
		MinDelay: 100 * time.Millisecond,
		MaxDelay: 10 * time.Second,
		Jitter:   0, // No jitter for deterministic test.
	}

	d1 := retryDelay(1, cfg)
	d2 := retryDelay(2, cfg)
	d3 := retryDelay(3, cfg)

	if d2 <= d1 {
		t.Fatalf("delay should grow: d1=%v, d2=%v", d1, d2)
	}
	if d3 <= d2 {
		t.Fatalf("delay should grow: d2=%v, d3=%v", d2, d3)
	}
}

func TestRetryDelayRespectsMax(t *testing.T) {
	t.Parallel()

	cfg := RetryConfig{
		MinDelay: 1 * time.Second,
		MaxDelay: 5 * time.Second,
		Jitter:   0,
	}

	// Very high attempt to ensure we hit the cap.
	d := retryDelay(20, cfg)
	if d > cfg.MaxDelay {
		t.Fatalf("delay %v exceeds max %v", d, cfg.MaxDelay)
	}
}

func TestRetryDelayRespectsMin(t *testing.T) {
	t.Parallel()

	cfg := RetryConfig{
		MinDelay: 500 * time.Millisecond,
		MaxDelay: 30 * time.Second,
		Jitter:   0,
	}

	d := retryDelay(1, cfg)
	if d < cfg.MinDelay {
		t.Fatalf("delay %v is less than min %v", d, cfg.MinDelay)
	}
}

func TestRetryDelayWithJitter(t *testing.T) {
	t.Parallel()

	cfg := RetryConfig{
		MinDelay: 100 * time.Millisecond,
		MaxDelay: 10 * time.Second,
		Jitter:   0.5,
	}

	// Run many times and ensure delay is always within bounds.
	for i := 0; i < 100; i++ {
		d := retryDelay(1, cfg)
		if d < cfg.MinDelay {
			t.Fatalf("delay %v < min %v", d, cfg.MinDelay)
		}
		if d > cfg.MaxDelay {
			t.Fatalf("delay %v > max %v", d, cfg.MaxDelay)
		}
	}
}

func TestStatusErrorImplementsError(t *testing.T) {
	t.Parallel()

	err := &StatusError{Status: 429, Message: "rate limited"}
	var e error = err
	if e.Error() != "status 429: rate limited" {
		t.Fatalf("Error() = %q", e.Error())
	}
}

func TestExtractErrorInfoStatusError(t *testing.T) {
	t.Parallel()

	err := &StatusError{Status: 503, Message: "service unavailable"}
	status, msg := extractErrorInfo(err)
	if status != 503 {
		t.Fatalf("status = %d, want 503", status)
	}
	if msg != "service unavailable" {
		t.Fatalf("msg = %q", msg)
	}
}

func TestExtractErrorInfoNil(t *testing.T) {
	t.Parallel()

	status, msg := extractErrorInfo(nil)
	if status != 0 || msg != "" {
		t.Fatalf("extractErrorInfo(nil) = (%d, %q), want (0, \"\")", status, msg)
	}
}

func TestExtractErrorInfoGenericError(t *testing.T) {
	t.Parallel()

	err := errors.New("some generic error")
	status, msg := extractErrorInfo(err)
	if status != 0 {
		t.Fatalf("status = %d, want 0", status)
	}
	if msg != "some generic error" {
		t.Fatalf("msg = %q", msg)
	}
}

func TestIsThinkingRelated(t *testing.T) {
	t.Parallel()

	if !isThinkingRelated(modelrouter.FailureTimeout) {
		t.Fatal("expected FailureTimeout to be thinking related")
	}
	if !isThinkingRelated(modelrouter.FailureOverloaded) {
		t.Fatal("expected FailureOverloaded to be thinking related")
	}
	if isThinkingRelated(modelrouter.FailureRateLimit) {
		t.Fatal("expected FailureRateLimit to NOT be thinking related")
	}
	if isThinkingRelated(modelrouter.FailureAuth) {
		t.Fatal("expected FailureAuth to NOT be thinking related")
	}
}

func TestReasonToFailureClass(t *testing.T) {
	t.Parallel()

	tests := []struct {
		reason modelrouter.FailureReason
		want   modelrouter.FailureClass
	}{
		{modelrouter.FailureRateLimit, modelrouter.FailureRateLimited},
		{modelrouter.FailureBilling, modelrouter.FailureQuota},
		{modelrouter.FailureOverloaded, modelrouter.FailureUnavailable},
		{modelrouter.FailureTimeout, modelrouter.FailureServer},
		{modelrouter.FailureAuth, modelrouter.FailureClient},
	}

	for _, tt := range tests {
		got := reasonToFailureClass(tt.reason)
		if got != tt.want {
			t.Fatalf("reasonToFailureClass(%q) = %q, want %q", tt.reason, got, tt.want)
		}
	}
}

func TestActualProviderForModelHonorsFallbackPrefixWithSlash(t *testing.T) {
	t.Parallel()

	got := actualProviderForModel("demo/copilot/gpt-4o", "demo/copilot")
	if got != "demo/copilot" {
		t.Fatalf("actualProviderForModel() = %q, want demo/copilot", got)
	}
}

func TestRetryModelCallFailoverToAlternateModel(t *testing.T) {
	t.Parallel()

	component := &AgentComponent{
		config: AgentConfig{
			Retry: RetryConfig{
				MaxAttempts: 2,
				MinDelay:    time.Millisecond,
				MaxDelay:    5 * time.Millisecond,
				Jitter:      0,
			},
		},
		bus: eventbus.NewInMemoryBus(),
		router: modelrouter.NewInMemoryRouter([]modelrouter.ModelProfile{
			{
				ID:              "model-a",
				Provider:        "openai",
				Priority:        100,
				ContextWindow:   128000,
				MaxOutputTokens: 4000,
				Enabled:         true,
				Supports: map[modelrouter.Capability]bool{
					modelrouter.CapabilityChat: true,
				},
			},
			{
				ID:              "fallback/model-b",
				Provider:        "fallback",
				Priority:        50,
				ContextWindow:   128000,
				MaxOutputTokens: 4000,
				Enabled:         true,
				Supports: map[modelrouter.Capability]bool{
					modelrouter.CapabilityChat: true,
				},
			},
		}, time.Minute),
	}
	run := &Run{ID: "run-1"}
	session := &Session{ID: "sess-1"}
	req := &ChatRequest{
		Model: "model-a",
	}
	callCount := 0
	resp, err := component.retryModelCall(context.Background(), run, session, req.Model, req, func() (*ModelResponse, error) {
		callCount++
		if req.Model == "model-a" {
			return nil, &StatusError{Status: 429, Message: "rate limit"}
		}
		return &ModelResponse{Message: contextengine.Message{Role: contextengine.RoleAssistant, Content: "ok"}}, nil
	})
	if err != nil {
		t.Fatalf("retryModelCall() error = %v", err)
	}
	if resp == nil || resp.Message.Content != "ok" {
		t.Fatalf("unexpected response = %#v", resp)
	}
	if callCount != 2 {
		t.Fatalf("callCount = %d, want 2", callCount)
	}
	if req.Model != "fallback/model-b" {
		t.Fatalf("req.Model = %q, want fallback/model-b", req.Model)
	}
	if run.Model != "fallback/model-b" {
		t.Fatalf("run.Model = %q, want fallback/model-b", run.Model)
	}
	if session.Model != "fallback/model-b" {
		t.Fatalf("session.Model = %q, want fallback/model-b", session.Model)
	}

	events := component.bus.(*eventbus.InMemoryBus).Snapshot()
	failoverCount := 0
	for _, event := range events {
		if event.Type != eventbus.EventModelFailover {
			continue
		}
		failoverCount++
		payload, ok := event.ModelFailoverPayload()
		if !ok {
			t.Fatal("ModelFailoverPayload() ok = false")
		}
		if payload.FromModel != "model-a" || payload.ToModel != "fallback/model-b" {
			t.Fatalf("payload = %#v", payload)
		}
	}
	if failoverCount != 1 {
		t.Fatalf("failoverCount = %d, want 1", failoverCount)
	}
}

func TestRetryModelCallThinkingDegradesBeforeFailover(t *testing.T) {
	t.Parallel()

	component := &AgentComponent{
		config: AgentConfig{
			Retry: RetryConfig{
				MaxAttempts: 2,
				MinDelay:    time.Millisecond,
				MaxDelay:    5 * time.Millisecond,
				Jitter:      0,
			},
		},
		router: modelrouter.NewInMemoryRouter([]modelrouter.ModelProfile{
			{
				ID:              "model-a",
				Provider:        "openai",
				Priority:        100,
				ContextWindow:   128000,
				MaxOutputTokens: 4000,
				Enabled:         true,
				Supports: map[modelrouter.Capability]bool{
					modelrouter.CapabilityChat: true,
				},
			},
			{
				ID:              "fallback/model-b",
				Provider:        "fallback",
				Priority:        50,
				ContextWindow:   128000,
				MaxOutputTokens: 4000,
				Enabled:         true,
				Supports: map[modelrouter.Capability]bool{
					modelrouter.CapabilityChat: true,
				},
			},
		}, time.Minute),
	}
	run := &Run{ID: "run-2"}
	session := &Session{ID: "sess-2"}
	req := &ChatRequest{
		Model:        "model-a",
		ThinkingMode: ThinkingExtended,
	}
	callCount := 0
	resp, err := component.retryModelCall(context.Background(), run, session, req.Model, req, func() (*ModelResponse, error) {
		callCount++
		if req.ThinkingMode == ThinkingExtended {
			return nil, &StatusError{Status: 503, Message: "server overloaded"}
		}
		if req.Model != "model-a" {
			t.Fatalf("model switched to %q, want original model-a after thinking degradation", req.Model)
		}
		return &ModelResponse{Message: contextengine.Message{Role: contextengine.RoleAssistant, Content: "ok after degrade"}}, nil
	})
	if err != nil {
		t.Fatalf("retryModelCall() error = %v", err)
	}
	if resp == nil || resp.Message.Content != "ok after degrade" {
		t.Fatalf("unexpected response = %#v", resp)
	}
	if callCount != 2 {
		t.Fatalf("callCount = %d, want 2", callCount)
	}
	if req.ThinkingMode != ThinkingRegular {
		t.Fatalf("ThinkingMode = %q, want %q", req.ThinkingMode, ThinkingRegular)
	}
	if req.Model != "model-a" {
		t.Fatalf("req.Model = %q, want model-a", req.Model)
	}
}

func TestEffectiveRetryDelayHonorsRouterCooldown(t *testing.T) {
	t.Parallel()

	cfg := RetryConfig{
		MaxAttempts: 4,
		MinDelay:    500 * time.Millisecond,
		MaxDelay:    20 * time.Second,
		Jitter:      0,
	}
	policy := retryPolicy{
		AllowRetry:          true,
		AllowFailover:       true,
		AlignToRouterWindow: true,
		MinDelay:            2 * time.Second,
	}

	delay := effectiveRetryDelay(1, cfg, policy, 5*time.Second)
	if delay != 5*time.Second {
		t.Fatalf("effectiveRetryDelay() = %v, want 5s", delay)
	}
}

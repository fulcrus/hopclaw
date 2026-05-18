package model

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/fulcrus/hopclaw/agent"
	"github.com/fulcrus/hopclaw/contextengine"
)

// ---------------------------------------------------------------------------
// KeyPool tests
// ---------------------------------------------------------------------------

func TestKeyPoolRoundRobin(t *testing.T) {
	t.Parallel()

	pool, err := NewKeyPool([]string{"key-a", "key-b", "key-c"})
	if err != nil {
		t.Fatalf("NewKeyPool() error = %v", err)
	}

	seen := make([]string, 6)
	for i := 0; i < 6; i++ {
		key, ok := pool.Next()
		if !ok {
			t.Fatalf("Next() returned false at iteration %d", i)
		}
		seen[i] = key
	}

	// Should cycle: a, b, c, a, b, c.
	want := []string{"key-a", "key-b", "key-c", "key-a", "key-b", "key-c"}
	for i, w := range want {
		if seen[i] != w {
			t.Fatalf("iteration %d: got %q, want %q", i, seen[i], w)
		}
	}
}

func TestKeyPoolSkipsCooledDown(t *testing.T) {
	t.Parallel()

	pool, err := NewKeyPool([]string{"key-a", "key-b", "key-c"})
	if err != nil {
		t.Fatalf("NewKeyPool() error = %v", err)
	}

	// Cool down key-a.
	pool.ReportFailure("key-a", true)

	// Should skip key-a.
	key, ok := pool.Next()
	if !ok || key != "key-b" {
		t.Fatalf("Next() = %q, %v; want key-b, true", key, ok)
	}
	key, ok = pool.Next()
	if !ok || key != "key-c" {
		t.Fatalf("Next() = %q, %v; want key-c, true", key, ok)
	}
	// Back to key-b (skipping key-a again).
	key, ok = pool.Next()
	if !ok || key != "key-b" {
		t.Fatalf("Next() = %q, %v; want key-b, true", key, ok)
	}
}

func TestKeyPoolAllCooledDown(t *testing.T) {
	t.Parallel()

	pool, err := NewKeyPool([]string{"key-a", "key-b"})
	if err != nil {
		t.Fatalf("NewKeyPool() error = %v", err)
	}

	pool.ReportFailure("key-a", true)
	pool.ReportFailure("key-b", true)

	_, ok := pool.Next()
	if ok {
		t.Fatal("Next() should return false when all keys are cooled down")
	}
}

func TestKeyPoolSuccessClears(t *testing.T) {
	t.Parallel()

	pool, err := NewKeyPool([]string{"key-a", "key-b"})
	if err != nil {
		t.Fatalf("NewKeyPool() error = %v", err)
	}

	pool.ReportFailure("key-a", true)
	if pool.AvailableCount() != 1 {
		t.Fatalf("AvailableCount() = %d, want 1", pool.AvailableCount())
	}

	pool.ReportSuccess("key-a")
	if pool.AvailableCount() != 2 {
		t.Fatalf("AvailableCount() = %d after success, want 2", pool.AvailableCount())
	}
}

func TestKeyPoolLen(t *testing.T) {
	t.Parallel()

	pool, err := NewKeyPool([]string{"a", "b", "c"})
	if err != nil {
		t.Fatalf("NewKeyPool() error = %v", err)
	}

	if pool.Len() != 3 {
		t.Fatalf("Len() = %d, want 3", pool.Len())
	}
}

func TestKeyPoolEmptyReturnsError(t *testing.T) {
	t.Parallel()

	_, err := NewKeyPool(nil)
	if err == nil {
		t.Fatal("NewKeyPool(nil) should return error")
	}
}

// ---------------------------------------------------------------------------
// KeyRotatingClient tests
// ---------------------------------------------------------------------------

type keyPoolStubClient struct {
	key  string
	err  error
	resp *agent.ModelResponse
}

func (c *keyPoolStubClient) Chat(_ context.Context, _ agent.ChatRequest) (*agent.ModelResponse, error) {
	if c.err != nil {
		return nil, c.err
	}
	return c.resp, nil
}

func TestKeyRotatingClientSuccess(t *testing.T) {
	t.Parallel()

	client, err := NewKeyRotatingClient(
		[]string{"key-a", "key-b"},
		func(key string) (agent.ModelClient, error) {
			return &keyPoolStubClient{
				key: key,
				resp: &agent.ModelResponse{
					Message: contextengine.Message{
						Role:    contextengine.RoleAssistant,
						Content: "hello from " + key,
					},
				},
			}, nil
		},
	)
	if err != nil {
		t.Fatalf("NewKeyRotatingClient() error = %v", err)
	}

	resp, err := client.Chat(context.Background(), agent.ChatRequest{Model: "test"})
	if err != nil {
		t.Fatalf("Chat() error = %v", err)
	}
	if resp.Message.Content != "hello from key-a" {
		t.Fatalf("Chat() content = %q, want 'hello from key-a'", resp.Message.Content)
	}
}

func TestKeyRotatingClientRotatesOnRateLimit(t *testing.T) {
	t.Parallel()

	callCount := 0
	var mu sync.Mutex

	client, err := NewKeyRotatingClient(
		[]string{"key-a", "key-b"},
		func(key string) (agent.ModelClient, error) {
			return &keyPoolStubClient{
				key: key,
				resp: &agent.ModelResponse{
					Message: contextengine.Message{
						Role:    contextengine.RoleAssistant,
						Content: "hello from " + key,
					},
				},
				err: func() error {
					mu.Lock()
					defer mu.Unlock()
					callCount++
					// First call fails with rate limit.
					if callCount == 1 && key == "key-a" {
						return providerAPIError("openai-compatible", 429, "rate_limit_exceeded", "rate limit exceeded")
					}
					return nil
				}(),
			}, nil
		},
	)
	if err != nil {
		t.Fatalf("NewKeyRotatingClient() error = %v", err)
	}

	resp, err := client.Chat(context.Background(), agent.ChatRequest{Model: "test"})
	if err != nil {
		t.Fatalf("Chat() error = %v", err)
	}
	// Should have rotated to key-b after key-a's rate limit.
	if !strings.Contains(resp.Message.Content, "key-b") {
		t.Fatalf("expected response from key-b, got %q", resp.Message.Content)
	}
}

func TestKeyRotatingClientAllKeysFail(t *testing.T) {
	t.Parallel()

	client, err := NewKeyRotatingClient(
		[]string{"key-a", "key-b"},
		func(key string) (agent.ModelClient, error) {
			return &keyPoolStubClient{
				key: key,
				err: providerAPIError("openai-compatible", 429, "rate_limit_exceeded", fmt.Sprintf("rate limit exceeded for %s", key)),
			}, nil
		},
	)
	if err != nil {
		t.Fatalf("NewKeyRotatingClient() error = %v", err)
	}

	_, err = client.Chat(context.Background(), agent.ChatRequest{Model: "test"})
	if err == nil {
		t.Fatal("Chat() should fail when all keys are rate limited")
	}
	if !strings.Contains(err.Error(), "429") {
		t.Fatalf("expected rate limit error, got %q", err.Error())
	}
}

func TestKeyRotatingClientNonRotatableError(t *testing.T) {
	t.Parallel()

	client, err := NewKeyRotatingClient(
		[]string{"key-a", "key-b"},
		func(key string) (agent.ModelClient, error) {
			return &keyPoolStubClient{
				key: key,
				err: providerAPIError("openai-compatible", 400, "invalid_request", "invalid request"),
			}, nil
		},
	)
	if err != nil {
		t.Fatalf("NewKeyRotatingClient() error = %v", err)
	}

	_, err = client.Chat(context.Background(), agent.ChatRequest{Model: "test"})
	if err == nil {
		t.Fatal("Chat() should fail on non-rotatable error")
	}
	// Should not have tried key-b — only key-a was used.
	if strings.Contains(err.Error(), "key-b") {
		t.Fatalf("should not have rotated to key-b, got %q", err.Error())
	}
}

// ---------------------------------------------------------------------------
// Error detection tests
// ---------------------------------------------------------------------------

func TestIsKeyRotatable(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"429", providerAPIError("openai-compatible", 429, "rate_limit_exceeded", "too many requests"), true},
		{"401", providerAPIError("openai-compatible", 401, "invalid_api_key", "unauthorized"), true},
		{"400 format", providerAPIError("openai-compatible", 400, "invalid_request", "bad request"), false},
		{"500 server", providerAPIError("openai-compatible", 500, "server_error", "internal server error"), false},
		{"timeout", fmt.Errorf("context deadline exceeded"), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := isKeyRotatable(tt.err)
			if got != tt.want {
				t.Fatalf("isKeyRotatable(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}

func TestIsRateLimitError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"429", providerAPIError("openai-compatible", 429, "rate_limit_exceeded", "too many requests"), true},
		{"resource exhausted", providerAPIError("google", 429, "RESOURCE_EXHAUSTED", "quota exceeded"), true},
		{"401", providerAPIError("openai-compatible", 401, "invalid_api_key", "unauthorized"), false},
		{"timeout", fmt.Errorf("context deadline exceeded"), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := isRateLimitError(tt.err)
			if got != tt.want {
				t.Fatalf("isRateLimitError(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Concurrent safety
// ---------------------------------------------------------------------------

func TestKeyPoolConcurrentAccess(t *testing.T) {
	t.Parallel()

	pool, err := NewKeyPool([]string{"a", "b", "c"})
	if err != nil {
		t.Fatalf("NewKeyPool() error = %v", err)
	}

	var wg sync.WaitGroup
	const goroutines = 50
	const iterations = 100

	wg.Add(goroutines)
	for g := 0; g < goroutines; g++ {
		go func() {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				key, ok := pool.Next()
				if !ok {
					continue
				}
				if i%3 == 0 {
					pool.ReportFailure(key, true)
				} else {
					pool.ReportSuccess(key)
				}
			}
		}()
	}
	wg.Wait()
}

func TestKeyPoolCooldownExpires(t *testing.T) {
	t.Parallel()

	pool, err := NewKeyPool([]string{"key-a"})
	if err != nil {
		t.Fatalf("NewKeyPool() error = %v", err)
	}

	// Manually set a very short cooldown by accessing internals.
	pool.mu.Lock()
	pool.keys[0].cooldownUntil = time.Now().Add(10 * time.Millisecond)
	pool.mu.Unlock()

	// Should be unavailable immediately.
	_, ok := pool.Next()
	if ok {
		t.Fatal("key should be in cooldown")
	}

	// Wait for cooldown to expire.
	time.Sleep(20 * time.Millisecond)

	key, ok := pool.Next()
	if !ok || key != "key-a" {
		t.Fatalf("Next() = %q, %v after cooldown; want key-a, true", key, ok)
	}
}

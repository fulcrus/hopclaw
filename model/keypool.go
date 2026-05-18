package model

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/fulcrus/hopclaw/agent"
	"github.com/fulcrus/hopclaw/logging"
)

var log = logging.WithSubsystem("model")

// ---------------------------------------------------------------------------
// Key pool constants
// ---------------------------------------------------------------------------

const (
	keyPoolRateLimitCooldown = 30 * time.Second // cooldown after rate limit
	keyPoolAuthCooldown      = 2 * time.Minute  // cooldown after auth failure
	maxKeyRotationAttempts   = 10               // max keys to try per call
)

// ---------------------------------------------------------------------------
// KeyPool
// ---------------------------------------------------------------------------

// KeyPool manages a pool of API keys with round-robin rotation and
// per-key cooldown. It is safe for concurrent use.
type KeyPool struct {
	mu      sync.Mutex
	keys    []keyEntry
	current int
}

type keyEntry struct {
	key           string
	cooldownUntil time.Time
	failureCount  int
}

// NewKeyPool creates a key pool from the given keys.
func NewKeyPool(keys []string) (*KeyPool, error) {
	if len(keys) == 0 {
		return nil, fmt.Errorf("key pool requires at least one key")
	}
	entries := make([]keyEntry, len(keys))
	for i, k := range keys {
		entries[i] = keyEntry{key: k}
	}
	return &KeyPool{keys: entries}, nil
}

// Next returns the next available key, skipping keys in cooldown.
// Returns ("", false) if all keys are in cooldown.
func (kp *KeyPool) Next() (string, bool) {
	kp.mu.Lock()
	defer kp.mu.Unlock()

	now := time.Now()
	n := len(kp.keys)
	for i := 0; i < n; i++ {
		idx := (kp.current + i) % n
		entry := &kp.keys[idx]
		if !entry.cooldownUntil.IsZero() && entry.cooldownUntil.After(now) {
			continue
		}
		// Clear expired cooldown.
		entry.cooldownUntil = time.Time{}
		kp.current = (idx + 1) % n
		return entry.key, true
	}
	return "", false
}

// ReportFailure marks a key as failed. Rate-limit failures get a short
// cooldown; auth failures get a longer one.
func (kp *KeyPool) ReportFailure(key string, isRateLimit bool) {
	kp.mu.Lock()
	defer kp.mu.Unlock()

	for i := range kp.keys {
		if kp.keys[i].key == key {
			kp.keys[i].failureCount++
			if isRateLimit {
				kp.keys[i].cooldownUntil = time.Now().Add(keyPoolRateLimitCooldown)
			} else {
				kp.keys[i].cooldownUntil = time.Now().Add(keyPoolAuthCooldown)
			}
			return
		}
	}
}

// ReportSuccess clears cooldown and failure count for a key.
func (kp *KeyPool) ReportSuccess(key string) {
	kp.mu.Lock()
	defer kp.mu.Unlock()

	for i := range kp.keys {
		if kp.keys[i].key == key {
			kp.keys[i].cooldownUntil = time.Time{}
			kp.keys[i].failureCount = 0
			return
		}
	}
}

// Len returns the total number of keys.
func (kp *KeyPool) Len() int {
	kp.mu.Lock()
	defer kp.mu.Unlock()
	return len(kp.keys)
}

// AvailableCount returns the number of keys not in cooldown.
func (kp *KeyPool) AvailableCount() int {
	kp.mu.Lock()
	defer kp.mu.Unlock()

	now := time.Now()
	count := 0
	for _, entry := range kp.keys {
		if entry.cooldownUntil.IsZero() || !entry.cooldownUntil.After(now) {
			count++
		}
	}
	return count
}

// ---------------------------------------------------------------------------
// KeyRotatingClient
// ---------------------------------------------------------------------------

// ClientFactory creates a ModelClient for the given API key.
type ClientFactory func(key string) (agent.ModelClient, error)

// KeyRotatingClient wraps multiple API keys for the same provider.
// On rate-limit or auth errors, it automatically rotates to the next key.
type KeyRotatingClient struct {
	pool    *KeyPool
	factory ClientFactory
	mu      sync.Mutex
	clients map[string]agent.ModelClient
}

// NewKeyRotatingClient creates a client that rotates among the given API keys.
func NewKeyRotatingClient(keys []string, factory ClientFactory) (*KeyRotatingClient, error) {
	pool, err := NewKeyPool(keys)
	if err != nil {
		return nil, err
	}
	// Pre-validate by creating the first client.
	first, err := factory(keys[0])
	if err != nil {
		return nil, err
	}
	return &KeyRotatingClient{
		pool:    pool,
		factory: factory,
		clients: map[string]agent.ModelClient{keys[0]: first},
	}, nil
}

func (c *KeyRotatingClient) getClient(key string) (agent.ModelClient, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if client, ok := c.clients[key]; ok {
		return client, nil
	}
	client, err := c.factory(key)
	if err != nil {
		return nil, err
	}
	c.clients[key] = client
	return client, nil
}

// Chat implements agent.ModelClient with key rotation.
func (c *KeyRotatingClient) Chat(ctx context.Context, req agent.ChatRequest) (*agent.ModelResponse, error) {
	return c.doWithRotation(ctx, func(client agent.ModelClient) (*agent.ModelResponse, error) {
		return client.Chat(ctx, req)
	})
}

// ChatStream implements agent.StreamingModelClient with key rotation.
func (c *KeyRotatingClient) ChatStream(ctx context.Context, req agent.ChatRequest, cb agent.StreamCallback) (*agent.ModelResponse, error) {
	return c.doWithRotation(ctx, func(client agent.ModelClient) (*agent.ModelResponse, error) {
		if sc, ok := client.(agent.StreamingModelClient); ok {
			return sc.ChatStream(ctx, req, cb)
		}
		return streamModelResponseFallback(ctx, client, req, cb)
	})
}

func (c *KeyRotatingClient) doWithRotation(ctx context.Context, fn func(agent.ModelClient) (*agent.ModelResponse, error)) (*agent.ModelResponse, error) {
	attempts := c.pool.Len()
	if attempts > maxKeyRotationAttempts {
		attempts = maxKeyRotationAttempts
	}

	var lastErr error
	for i := 0; i < attempts; i++ {
		key, ok := c.pool.Next()
		if !ok {
			if lastErr != nil {
				return nil, lastErr
			}
			return nil, fmt.Errorf("all API keys are in cooldown")
		}

		client, err := c.getClient(key)
		if err != nil {
			return nil, err
		}

		resp, err := fn(client)
		if err == nil {
			c.pool.ReportSuccess(key)
			return resp, nil
		}

		lastErr = err

		// Only rotate on rate-limit or auth errors.
		if isKeyRotatable(err) {
			isRL := isRateLimitError(err)
			c.pool.ReportFailure(key, isRL)
			log.Warn("api key rotated",
				"attempt", i+1,
				"is_rate_limit", isRL,
				"error", err.Error(),
			)
			continue
		}

		// Non-rotatable error — return immediately.
		return nil, err
	}

	return nil, lastErr
}

// isKeyRotatable returns true if the error warrants trying a different API key.
func isKeyRotatable(err error) bool {
	return isProviderKeyRotatable(err)
}

// isRateLimitError returns true specifically for rate limit (vs auth) errors.
func isRateLimitError(err error) bool {
	return isProviderRateLimitError(err)
}

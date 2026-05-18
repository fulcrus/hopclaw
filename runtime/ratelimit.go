package runtime

import (
	"sync"
	"time"
)

// ---------------------------------------------------------------------------
// Per-session submit rate limiter (token bucket)
// ---------------------------------------------------------------------------

const (
	rateLimitCleanupInterval = 5 * time.Minute
	rateLimitBucketExpiry    = 10 * time.Minute
)

// SessionRateLimiter implements a per-session-key token bucket rate limiter.
type SessionRateLimiter struct {
	mu      sync.Mutex // guards buckets
	buckets map[string]*bucket
	rate    float64 // tokens per second
	burst   int
	stop    chan struct{}
	clock   Clock
}

type bucket struct {
	tokens   float64
	lastTime time.Time
}

// NewSessionRateLimiter creates a new rate limiter. requestsPerMinute controls
// the sustained rate, burstSize controls the maximum burst.
func NewSessionRateLimiter(requestsPerMinute int, burstSize int) *SessionRateLimiter {
	return newSessionRateLimiterWithClock(requestsPerMinute, burstSize, nil)
}

func newSessionRateLimiterWithClock(requestsPerMinute int, burstSize int, clock Clock) *SessionRateLimiter {
	if requestsPerMinute <= 0 {
		return nil
	}
	if burstSize <= 0 {
		burstSize = 1
	}
	if clock == nil {
		clock = defaultRuntimeClock
	}
	rl := &SessionRateLimiter{
		buckets: make(map[string]*bucket),
		rate:    float64(requestsPerMinute) / 60.0,
		burst:   burstSize,
		stop:    make(chan struct{}),
		clock:   clock,
	}
	go rl.cleanup()
	return rl
}

// Allow checks whether the session identified by key is allowed to proceed.
func (rl *SessionRateLimiter) Allow(key string) bool {
	if rl == nil {
		return true
	}

	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := rl.limiterClock().Now()
	b, ok := rl.buckets[key]
	if !ok {
		b = &bucket{
			tokens:   float64(rl.burst) - 1,
			lastTime: now,
		}
		rl.buckets[key] = b
		return true
	}

	// Refill tokens based on elapsed time.
	elapsed := now.Sub(b.lastTime).Seconds()
	b.tokens += elapsed * rl.rate
	if b.tokens > float64(rl.burst) {
		b.tokens = float64(rl.burst)
	}
	b.lastTime = now

	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

// Stop terminates the background cleanup goroutine.
func (rl *SessionRateLimiter) Stop() {
	if rl == nil {
		return
	}
	select {
	case <-rl.stop:
	default:
		close(rl.stop)
	}
}

// cleanup periodically removes expired buckets.
func (rl *SessionRateLimiter) cleanup() {
	ticker := rl.limiterClock().NewTicker(rateLimitCleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-rl.stop:
			return
		case now := <-ticker.C():
			rl.mu.Lock()
			for key, b := range rl.buckets {
				if now.Sub(b.lastTime) > rateLimitBucketExpiry {
					delete(rl.buckets, key)
				}
			}
			rl.mu.Unlock()
		}
	}
}

func (rl *SessionRateLimiter) limiterClock() Clock {
	if rl != nil && rl.clock != nil {
		return rl.clock
	}
	return defaultRuntimeClock
}

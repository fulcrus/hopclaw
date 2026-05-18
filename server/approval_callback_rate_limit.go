package server

import (
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

const (
	defaultApprovalCallbackRequestsPerSecond = 5.0
	defaultApprovalCallbackBurst             = 10
	approvalCallbackBucketExpiry            = 10 * time.Minute
)

type ApprovalCallbackRateLimitConfig struct {
	RequestsPerSecond float64
	BurstSize         int
}

type approvalCallbackRateLimiter struct {
	mu      sync.Mutex
	buckets map[string]*approvalCallbackBucket
	rate    float64
	burst   int
}

type approvalCallbackBucket struct {
	tokens   float64
	lastFill time.Time
}

func newApprovalCallbackRateLimiter(cfg ApprovalCallbackRateLimitConfig) *approvalCallbackRateLimiter {
	if cfg.RequestsPerSecond <= 0 {
		cfg.RequestsPerSecond = defaultApprovalCallbackRequestsPerSecond
	}
	if cfg.BurstSize <= 0 {
		cfg.BurstSize = defaultApprovalCallbackBurst
	}
	return &approvalCallbackRateLimiter{
		buckets: make(map[string]*approvalCallbackBucket),
		rate:    cfg.RequestsPerSecond,
		burst:   cfg.BurstSize,
	}
}

func (rl *approvalCallbackRateLimiter) allow(key string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	for bucketKey, bucket := range rl.buckets {
		if now.Sub(bucket.lastFill) > approvalCallbackBucketExpiry {
			delete(rl.buckets, bucketKey)
		}
	}
	if strings.TrimSpace(key) == "" {
		key = "unknown"
	}
	bucket, ok := rl.buckets[key]
	if !ok {
		bucket = &approvalCallbackBucket{
			tokens:   float64(rl.burst),
			lastFill: now,
		}
		rl.buckets[key] = bucket
	}

	elapsed := now.Sub(bucket.lastFill).Seconds()
	bucket.tokens += elapsed * rl.rate
	if bucket.tokens > float64(rl.burst) {
		bucket.tokens = float64(rl.burst)
	}
	bucket.lastFill = now

	if bucket.tokens < 1 {
		return false
	}
	bucket.tokens--
	return true
}

func approvalCallbackClientIP(r *http.Request) string {
	if r == nil {
		return "unknown"
	}
	host, _, err := net.SplitHostPort(strings.TrimSpace(r.RemoteAddr))
	if err == nil && host != "" {
		return host
	}
	return strings.TrimSpace(r.RemoteAddr)
}

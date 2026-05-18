package runtime

import (
	"testing"
	"time"
)

func TestSessionRateLimiterAllow(t *testing.T) {
	t.Parallel()

	rl := NewSessionRateLimiter(60, 3) // 1/sec, burst 3
	defer rl.Stop()

	// First 3 should be allowed (burst).
	for i := 0; i < 3; i++ {
		if !rl.Allow("sess-1") {
			t.Fatalf("request %d should be allowed", i)
		}
	}

	// 4th should be denied.
	if rl.Allow("sess-1") {
		t.Fatal("request 4 should be denied")
	}
}

func TestSessionRateLimiterBurst(t *testing.T) {
	t.Parallel()

	rl := NewSessionRateLimiter(60, 5) // 1/sec, burst 5
	defer rl.Stop()

	// Exhaust burst for session A.
	for i := 0; i < 5; i++ {
		if !rl.Allow("a") {
			t.Fatalf("session a request %d should be allowed", i)
		}
	}
	if rl.Allow("a") {
		t.Fatal("session a should be rate limited")
	}

	// Session B is independent.
	if !rl.Allow("b") {
		t.Fatal("session b should be allowed")
	}
}

func TestSessionRateLimiterRefill(t *testing.T) {
	t.Parallel()

	clock := newManualClock(time.Date(2026, 4, 13, 11, 0, 0, 0, time.UTC))
	rl := newSessionRateLimiterWithClock(600, 1, clock) // 10/sec, burst 1
	defer rl.Stop()

	if !rl.Allow("sess") {
		t.Fatal("first request should be allowed")
	}
	if rl.Allow("sess") {
		t.Fatal("second request should be denied")
	}

	clock.Advance(250 * time.Millisecond)

	if !rl.Allow("sess") {
		t.Fatal("request after refill should be allowed")
	}
}

func TestSessionRateLimiterNil(t *testing.T) {
	t.Parallel()

	// nil limiter should always allow.
	var rl *SessionRateLimiter
	if !rl.Allow("any") {
		t.Fatal("nil limiter should allow")
	}
}

func TestSessionRateLimiterZeroRate(t *testing.T) {
	t.Parallel()

	// Zero rate returns nil (disabled).
	rl := NewSessionRateLimiter(0, 5)
	if rl != nil {
		t.Fatal("zero rate should return nil")
	}
}

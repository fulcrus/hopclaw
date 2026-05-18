package channels

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"time"
)

// Rate limit retry constants.
const (
	rateLimitMaxRetries   = 3
	rateLimitDefaultDelay = 1 * time.Second
	rateLimitMaxDelay     = 60 * time.Second
)

// RateLimitError is returned when an API responds with HTTP 429.
type RateLimitError struct {
	RetryAfter time.Duration
	StatusCode int
	Body       string
}

func (e *RateLimitError) Error() string {
	return fmt.Sprintf("rate limited (HTTP %d), retry after %s", e.StatusCode, e.RetryAfter)
}

// CheckRateLimit inspects an HTTP response for rate limit signals (HTTP 429).
// If the response is rate-limited, it returns a RateLimitError with the
// parsed Retry-After duration. Otherwise it returns nil.
func CheckRateLimit(resp *http.Response) *RateLimitError {
	if resp == nil || resp.StatusCode != http.StatusTooManyRequests {
		return nil
	}
	delay := rateLimitDefaultDelay

	// Try Retry-After header (seconds or HTTP-date).
	if ra := resp.Header.Get("Retry-After"); ra != "" {
		if secs, err := strconv.ParseFloat(ra, 64); err == nil && secs > 0 {
			delay = time.Duration(secs * float64(time.Second))
		}
	}
	// Discord uses X-RateLimit-Reset-After (seconds, float).
	if ra := resp.Header.Get("X-RateLimit-Reset-After"); ra != "" {
		if secs, err := strconv.ParseFloat(ra, 64); err == nil && secs > 0 {
			delay = time.Duration(secs * float64(time.Second))
		}
	}

	if delay > rateLimitMaxDelay {
		delay = rateLimitMaxDelay
	}
	return &RateLimitError{
		RetryAfter: delay,
		StatusCode: resp.StatusCode,
	}
}

// RateLimitMaxRetries returns the maximum number of rate-limit retries.
func RateLimitMaxRetries() int { return rateLimitMaxRetries }

// WaitForRateLimit sleeps for the duration specified in the RateLimitError,
// respecting context cancellation. Returns an error if the context is cancelled.
func WaitForRateLimit(ctx context.Context, err *RateLimitError) error {
	if err == nil || err.RetryAfter <= 0 {
		return nil
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(err.RetryAfter):
		return nil
	}
}

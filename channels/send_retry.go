package channels

import (
	"context"
	"errors"
	"fmt"
	"time"
)

const (
	defaultSendRetryAttempts    = 3
	defaultSendRetryBaseBackoff = 200 * time.Millisecond
	defaultSendRetryMaxBackoff  = 2 * time.Second
)

// SendError annotates adapter delivery failures with retry semantics so the
// outer bridge can avoid blindly retrying permanent errors.
type SendError struct {
	Retryable  bool
	StatusCode int
	Cause      error
}

func (e *SendError) Error() string {
	if e == nil {
		return ""
	}
	if e.Cause != nil {
		return e.Cause.Error()
	}
	if e.StatusCode > 0 {
		return fmt.Sprintf("send failed (HTTP %d)", e.StatusCode)
	}
	return "send failed"
}

func (e *SendError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

type SendRetryPolicy struct {
	MaxAttempts int
	BaseBackoff time.Duration
	MaxBackoff  time.Duration
}

func MarkSendError(err error, retryable bool, statusCode int) error {
	if err == nil {
		return nil
	}
	var sendErr *SendError
	if errors.As(err, &sendErr) {
		if statusCode > 0 {
			sendErr.StatusCode = statusCode
		}
		sendErr.Retryable = retryable
		return sendErr
	}
	return &SendError{
		Retryable:  retryable,
		StatusCode: statusCode,
		Cause:      err,
	}
}

func ShouldRetrySend(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	var sendErr *SendError
	if errors.As(err, &sendErr) {
		return sendErr.Retryable
	}
	var rateLimited *RateLimitError
	if errors.As(err, &rateLimited) {
		return true
	}
	return true
}

func RetrySend(ctx context.Context, policy SendRetryPolicy, send func(context.Context) error) error {
	if send == nil {
		return nil
	}
	attempts := policy.MaxAttempts
	if attempts <= 0 {
		attempts = defaultSendRetryAttempts
	}
	var lastErr error
	for attempt := 1; attempt <= attempts; attempt++ {
		lastErr = send(ctx)
		if lastErr == nil {
			return nil
		}
		if !ShouldRetrySend(lastErr) {
			return lastErr
		}
		if attempt == attempts {
			return MarkSendError(lastErr, false, sendErrorStatusCode(lastErr))
		}
		if err := waitSendRetry(ctx, retryBackoffDelay(policy, attempt, lastErr)); err != nil {
			return err
		}
	}
	return lastErr
}

func retryBackoffDelay(policy SendRetryPolicy, attempt int, err error) time.Duration {
	var rateLimited *RateLimitError
	if errors.As(err, &rateLimited) && rateLimited.RetryAfter > 0 {
		return rateLimited.RetryAfter
	}
	base := policy.BaseBackoff
	if base <= 0 {
		base = defaultSendRetryBaseBackoff
	}
	maxBackoff := policy.MaxBackoff
	if maxBackoff <= 0 {
		maxBackoff = defaultSendRetryMaxBackoff
	}
	delay := base
	for i := 1; i < attempt; i++ {
		if delay >= maxBackoff {
			return maxBackoff
		}
		delay *= 2
	}
	if delay > maxBackoff {
		return maxBackoff
	}
	return delay
}

func waitSendRetry(ctx context.Context, delay time.Duration) error {
	if delay <= 0 {
		delay = time.Millisecond
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func sendErrorStatusCode(err error) int {
	var sendErr *SendError
	if errors.As(err, &sendErr) {
		return sendErr.StatusCode
	}
	var rateLimited *RateLimitError
	if errors.As(err, &rateLimited) {
		return rateLimited.StatusCode
	}
	return 0
}

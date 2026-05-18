package channels

import (
	"context"
	"errors"
	"testing"
)

func TestRetrySendRetriesTransientErrors(t *testing.T) {
	t.Parallel()

	attempts := 0
	err := RetrySend(context.Background(), SendRetryPolicy{MaxAttempts: 3}, func(context.Context) error {
		attempts++
		if attempts < 3 {
			return MarkSendError(errors.New("temporary"), true, 502)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("RetrySend() error = %v", err)
	}
	if attempts != 3 {
		t.Fatalf("attempts = %d, want 3", attempts)
	}
}

func TestRetrySendStopsOnPermanentError(t *testing.T) {
	t.Parallel()

	attempts := 0
	want := MarkSendError(errors.New("bad request"), false, 400)
	err := RetrySend(context.Background(), SendRetryPolicy{MaxAttempts: 3}, func(context.Context) error {
		attempts++
		return want
	})
	if err == nil {
		t.Fatal("RetrySend() error = nil, want permanent failure")
	}
	if attempts != 1 {
		t.Fatalf("attempts = %d, want 1", attempts)
	}
	var sendErr *SendError
	if !errors.As(err, &sendErr) || sendErr.Retryable {
		t.Fatalf("error = %#v, want permanent SendError", err)
	}
}

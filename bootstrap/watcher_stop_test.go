package bootstrap

import (
	"context"
	"testing"
	"time"
)

func TestCancelAndWaitBlocksUntilDone(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	stop := cancelAndWait(cancel, done)

	returned := make(chan struct{})
	go func() {
		stop()
		close(returned)
	}()

	select {
	case <-returned:
		t.Fatal("stop returned before done closed")
	case <-time.After(25 * time.Millisecond):
	}

	select {
	case <-ctx.Done():
	default:
		t.Fatal("expected cancel to be called immediately")
	}

	close(done)

	select {
	case <-returned:
	case <-time.After(time.Second):
		t.Fatal("stop did not return after done closed")
	}
}

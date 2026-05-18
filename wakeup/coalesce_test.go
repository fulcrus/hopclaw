package wakeup

import (
	"sync"
	"testing"
	"time"
)

func TestCoalescer_MergesByPriority(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	var results []WakeRequest

	c := NewCoalescer(func(req WakeRequest) {
		mu.Lock()
		results = append(results, req)
		mu.Unlock()
	})

	c.Submit(WakeRequest{SessionKey: "s1", Message: "low", Priority: PriorityRetry})
	c.Submit(WakeRequest{SessionKey: "s1", Message: "high", Priority: PriorityAction})
	c.Submit(WakeRequest{SessionKey: "s1", Message: "mid", Priority: PriorityDefault})

	// Wait for coalesce window to expire.
	time.Sleep(coalesceWindow + 50*time.Millisecond)

	mu.Lock()
	defer mu.Unlock()

	if len(results) != 1 {
		t.Fatalf("expected 1 coalesced result, got %d", len(results))
	}
	if results[0].Message != "high" {
		t.Fatalf("expected highest priority message 'high', got %q", results[0].Message)
	}
	if results[0].Priority != PriorityAction {
		t.Fatalf("expected PriorityAction, got %d", results[0].Priority)
	}
}

func TestCoalescer_SeparateSessions(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	var results []WakeRequest

	c := NewCoalescer(func(req WakeRequest) {
		mu.Lock()
		results = append(results, req)
		mu.Unlock()
	})

	c.Submit(WakeRequest{SessionKey: "s1", Message: "a", Priority: PriorityDefault})
	c.Submit(WakeRequest{SessionKey: "s2", Message: "b", Priority: PriorityDefault})

	time.Sleep(coalesceWindow + 50*time.Millisecond)

	mu.Lock()
	defer mu.Unlock()

	if len(results) != 2 {
		t.Fatalf("expected 2 results for separate sessions, got %d", len(results))
	}
}

func TestCoalescer_PendingCount(t *testing.T) {
	t.Parallel()

	c := NewCoalescer(func(req WakeRequest) {})

	if c.PendingCount() != 0 {
		t.Fatal("expected 0 pending initially")
	}

	c.Submit(WakeRequest{SessionKey: "s1", Message: "a", Priority: PriorityDefault})
	c.Submit(WakeRequest{SessionKey: "s2", Message: "b", Priority: PriorityDefault})

	if c.PendingCount() != 2 {
		t.Fatalf("expected 2 pending, got %d", c.PendingCount())
	}

	time.Sleep(coalesceWindow + 50*time.Millisecond)

	if c.PendingCount() != 0 {
		t.Fatalf("expected 0 pending after flush, got %d", c.PendingCount())
	}
}

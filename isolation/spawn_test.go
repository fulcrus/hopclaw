package isolation

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

// fakeSubmit returns a SubmitFunc that records calls and completes immediately.
func fakeSubmit(result string) SubmitFunc {
	return func(_ context.Context, sessionKey, _ string) (string, error) {
		return result + ":" + sessionKey, nil
	}
}

// fakeSubmitErr returns a SubmitFunc that always fails.
func fakeSubmitErr(msg string) SubmitFunc {
	return func(_ context.Context, _, _ string) (string, error) {
		return "", fmt.Errorf("%s", msg)
	}
}

// slowSubmit returns a SubmitFunc that blocks for the given duration.
func slowSubmit(d time.Duration, result string) SubmitFunc {
	return func(ctx context.Context, sessionKey, _ string) (string, error) {
		select {
		case <-time.After(d):
			return result + ":" + sessionKey, nil
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}
}

// ---------------------------------------------------------------------------
// tests
// ---------------------------------------------------------------------------

func TestSpawnCreatesChildSession(t *testing.T) {
	spawner := NewSpawner(fakeSubmit("ok"))

	child, err := spawner.Spawn(context.Background(), SpawnRequest{
		ParentSessionID: "parent-1",
		AgentName:       "researcher",
		Message:         "find papers on AI safety",
	})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}

	if child.ID == "" {
		t.Error("child ID is empty")
	}
	if child.ParentID != "parent-1" {
		t.Errorf("ParentID = %q, want %q", child.ParentID, "parent-1")
	}
	if child.AgentName != "researcher" {
		t.Errorf("AgentName = %q, want %q", child.AgentName, "researcher")
	}
	if child.Status != childStatusRunning {
		t.Errorf("Status = %q, want %q", child.Status, childStatusRunning)
	}
	if child.SessionKey == "" {
		t.Error("SessionKey is empty")
	}
	if child.CreatedAt.IsZero() {
		t.Error("CreatedAt is zero")
	}
}

func TestSpawnRequiresFields(t *testing.T) {
	spawner := NewSpawner(fakeSubmit("ok"))

	tests := []struct {
		name string
		req  SpawnRequest
	}{
		{"missing parent", SpawnRequest{AgentName: "a", Message: "m"}},
		{"missing agent", SpawnRequest{ParentSessionID: "p", Message: "m"}},
		{"missing message", SpawnRequest{ParentSessionID: "p", AgentName: "a"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := spawner.Spawn(context.Background(), tt.req)
			if err == nil {
				t.Error("expected error")
			}
		})
	}
}

func TestSpawnSetsWorkspaceID(t *testing.T) {
	spawner := NewSpawner(fakeSubmit("ok"))

	ws := &Workspace{ID: "ws-123"}
	child, err := spawner.Spawn(context.Background(), SpawnRequest{
		ParentSessionID: "parent-1",
		AgentName:       "coder",
		Message:         "write tests",
		Workspace:       ws,
	})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	if child.WorkspaceID != "ws-123" {
		t.Errorf("WorkspaceID = %q, want %q", child.WorkspaceID, "ws-123")
	}
}

func TestYieldReturnsCompletedChildren(t *testing.T) {
	spawner := NewSpawner(fakeSubmit("done"))

	_, err := spawner.Spawn(context.Background(), SpawnRequest{
		ParentSessionID: "parent-1",
		AgentName:       "worker",
		Message:         "do work",
	})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}

	// Wait briefly for the goroutine to complete.
	time.Sleep(50 * time.Millisecond)

	completed, err := spawner.Yield("parent-1")
	if err != nil {
		t.Fatalf("Yield: %v", err)
	}
	if len(completed) != 1 {
		t.Fatalf("Yield returned %d children, want 1", len(completed))
	}
	if completed[0].Status != childStatusCompleted {
		t.Errorf("Status = %q, want %q", completed[0].Status, childStatusCompleted)
	}
	if completed[0].Result == "" {
		t.Error("Result is empty")
	}
	if completed[0].CompletedAt.IsZero() {
		t.Error("CompletedAt is zero")
	}

	// Yield again should return empty since we already harvested.
	again, err := spawner.Yield("parent-1")
	if err != nil {
		t.Fatalf("Yield again: %v", err)
	}
	if len(again) != 0 {
		t.Errorf("Yield again returned %d, want 0", len(again))
	}
}

func TestYieldRequiresParentID(t *testing.T) {
	spawner := NewSpawner(fakeSubmit("ok"))
	_, err := spawner.Yield("")
	if err == nil {
		t.Error("expected error for empty parent ID")
	}
}

func TestYieldWithFailedChild(t *testing.T) {
	spawner := NewSpawner(fakeSubmitErr("submit failed"))

	_, err := spawner.Spawn(context.Background(), SpawnRequest{
		ParentSessionID: "parent-1",
		AgentName:       "worker",
		Message:         "do work",
	})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}

	time.Sleep(50 * time.Millisecond)

	completed, err := spawner.Yield("parent-1")
	if err != nil {
		t.Fatalf("Yield: %v", err)
	}
	if len(completed) != 1 {
		t.Fatalf("Yield returned %d children, want 1", len(completed))
	}
	if completed[0].Status != childStatusFailed {
		t.Errorf("Status = %q, want %q", completed[0].Status, childStatusFailed)
	}
	if completed[0].Result != "submit failed" {
		t.Errorf("Result = %q, want %q", completed[0].Result, "submit failed")
	}
}

func TestListChildrenFiltersByParent(t *testing.T) {
	spawner := NewSpawner(fakeSubmit("ok"))

	_, err := spawner.Spawn(context.Background(), SpawnRequest{
		ParentSessionID: "parent-1",
		AgentName:       "worker-a",
		Message:         "task a",
	})
	if err != nil {
		t.Fatalf("Spawn a: %v", err)
	}
	_, err = spawner.Spawn(context.Background(), SpawnRequest{
		ParentSessionID: "parent-2",
		AgentName:       "worker-b",
		Message:         "task b",
	})
	if err != nil {
		t.Fatalf("Spawn b: %v", err)
	}

	children := spawner.ListChildren("parent-1")
	if len(children) != 1 {
		t.Fatalf("ListChildren(parent-1) = %d, want 1", len(children))
	}
	if children[0].ParentID != "parent-1" {
		t.Errorf("ParentID = %q, want %q", children[0].ParentID, "parent-1")
	}
}

func TestMultipleChildrenPerParent(t *testing.T) {
	spawner := NewSpawner(fakeSubmit("ok"))

	const childCount = 3
	for i := range childCount {
		_, err := spawner.Spawn(context.Background(), SpawnRequest{
			ParentSessionID: "parent-multi",
			AgentName:       fmt.Sprintf("worker-%d", i),
			Message:         fmt.Sprintf("task %d", i),
		})
		if err != nil {
			t.Fatalf("Spawn %d: %v", i, err)
		}
	}

	children := spawner.ListChildren("parent-multi")
	if len(children) != childCount {
		t.Errorf("ListChildren = %d, want %d", len(children), childCount)
	}
}

func TestWaitAllCompletesSuccessfully(t *testing.T) {
	spawner := NewSpawner(slowSubmit(20*time.Millisecond, "result"))

	_, err := spawner.Spawn(context.Background(), SpawnRequest{
		ParentSessionID: "parent-wait",
		AgentName:       "slow-worker",
		Message:         "slow task",
	})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	results, err := spawner.WaitAll(ctx, "parent-wait")
	if err != nil {
		t.Fatalf("WaitAll: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("WaitAll returned %d, want 1", len(results))
	}
	if results[0].Status != childStatusCompleted {
		t.Errorf("Status = %q, want %q", results[0].Status, childStatusCompleted)
	}
}

func TestSendMessageRejectsCompletedChild(t *testing.T) {
	spawner := NewSpawner(fakeSubmit("done"))

	child, err := spawner.Spawn(context.Background(), SpawnRequest{
		ParentSessionID: "parent-send",
		AgentName:       "worker",
		Message:         "initial",
	})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}

	time.Sleep(50 * time.Millisecond)

	if err := spawner.SendMessage(context.Background(), "parent-send", child.ID, "follow up"); err == nil {
		t.Fatal("expected SendMessage to reject completed child")
	}
}

func TestSendMessageRegistersIntentBeforeAsyncSubmit(t *testing.T) {
	var submitted atomic.Int32
	release := make(chan struct{})
	spawner := NewSpawner(func(_ context.Context, _, _ string) (string, error) {
		submitted.Add(1)
		<-release
		return "ok", nil
	})

	child, err := spawner.Spawn(context.Background(), SpawnRequest{
		ParentSessionID: "parent-pending",
		AgentName:       "worker",
		Message:         "initial",
	})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}

	children := spawner.ListChildren("parent-pending")
	if len(children) != 1 || children[0].pendingMessages != 0 {
		t.Fatalf("expected spawned child with no pending messages, got %#v", children)
	}

	if err := spawner.SendMessage(context.Background(), "parent-pending", child.ID, "follow up"); err != nil {
		t.Fatalf("SendMessage() error = %v", err)
	}

	children = spawner.ListChildren("parent-pending")
	if len(children) != 1 || children[0].pendingMessages != 1 {
		t.Fatalf("expected one pending message after SendMessage, got %#v", children)
	}

	close(release)
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		children = spawner.ListChildren("parent-pending")
		if len(children) == 1 && children[0].pendingMessages == 0 {
			if submitted.Load() == 0 {
				t.Fatal("expected submit function to be called")
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("pending message counter did not clear")
}

func TestWaitAllContextCancellation(t *testing.T) {
	spawner := NewSpawner(slowSubmit(5*time.Second, "never"))

	_, err := spawner.Spawn(context.Background(), SpawnRequest{
		ParentSessionID: "parent-cancel",
		AgentName:       "blocked-worker",
		Message:         "will be cancelled",
	})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	_, err = spawner.WaitAll(ctx, "parent-cancel")
	if err == nil {
		t.Fatal("expected context cancellation error")
	}
}

func TestWaitAllRequiresParentID(t *testing.T) {
	spawner := NewSpawner(fakeSubmit("ok"))
	_, err := spawner.WaitAll(context.Background(), "")
	if err == nil {
		t.Error("expected error for empty parent ID")
	}
}

func TestSpawnerConcurrentAccess(t *testing.T) {
	spawner := NewSpawner(fakeSubmit("ok"))

	var wg sync.WaitGroup
	const goroutines = 10

	for i := range goroutines {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			_, err := spawner.Spawn(context.Background(), SpawnRequest{
				ParentSessionID: "parent-concurrent",
				AgentName:       fmt.Sprintf("worker-%d", idx),
				Message:         fmt.Sprintf("task %d", idx),
			})
			if err != nil {
				t.Errorf("Spawn %d: %v", idx, err)
			}
		}(i)
	}
	wg.Wait()

	children := spawner.ListChildren("parent-concurrent")
	if len(children) != goroutines {
		t.Errorf("ListChildren = %d, want %d", len(children), goroutines)
	}
}

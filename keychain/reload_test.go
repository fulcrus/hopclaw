package keychain

import (
	"context"
	"os"
	"sync"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// Start / Stop lifecycle
// ---------------------------------------------------------------------------

func TestWatcherStartStop(t *testing.T) {
	w := NewWatcher(50*time.Millisecond, []string{"env:TEST_WATCHER_LIFECYCLE"})

	t.Setenv("TEST_WATCHER_LIFECYCLE", "initial")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := w.Start(ctx); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	if !w.IsRunning() {
		t.Fatal("IsRunning() = false after Start, want true")
	}

	w.Stop()

	// Give the goroutine a moment to exit.
	time.Sleep(20 * time.Millisecond)

	if w.IsRunning() {
		t.Fatal("IsRunning() = true after Stop, want false")
	}
}

func TestWatcherStartTwice(t *testing.T) {
	w := NewWatcher(50*time.Millisecond, nil)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := w.Start(ctx); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	defer w.Stop()

	if err := w.Start(ctx); err == nil {
		t.Fatal("Start() expected error for double start")
	}
}

func TestWatcherStopWhenNotRunning(t *testing.T) {
	w := NewWatcher(50*time.Millisecond, nil)

	// Should not panic.
	w.Stop()
}

func TestWatcherContextCancel(t *testing.T) {
	w := NewWatcher(50*time.Millisecond, []string{"env:TEST_WATCHER_CTX"})

	t.Setenv("TEST_WATCHER_CTX", "val")

	ctx, cancel := context.WithCancel(context.Background())

	if err := w.Start(ctx); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	cancel()

	// Wait for the goroutine to notice cancellation.
	time.Sleep(100 * time.Millisecond)

	if w.IsRunning() {
		t.Fatal("IsRunning() = true after context cancel, want false")
	}
}

// ---------------------------------------------------------------------------
// Subscribe receives events
// ---------------------------------------------------------------------------

func TestWatcherSubscribeReceivesEvents(t *testing.T) {
	envKey := "TEST_WATCHER_SUB_" + t.Name()
	t.Setenv(envKey, "value-a")

	w := NewWatcher(30*time.Millisecond, []string{"env:" + envKey})

	var mu sync.Mutex
	var received []ChangeEvent

	w.Subscribe(func(event ChangeEvent) {
		mu.Lock()
		received = append(received, event)
		mu.Unlock()
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := w.Start(ctx); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	defer w.Stop()

	// Change the env var.
	os.Setenv(envKey, "value-b")
	t.Cleanup(func() { os.Unsetenv(envKey) })

	// Wait for at least one poll cycle.
	time.Sleep(100 * time.Millisecond)

	mu.Lock()
	count := len(received)
	mu.Unlock()

	if count == 0 {
		t.Fatal("Subscribe handler was not called after env var change")
	}

	mu.Lock()
	event := received[0]
	mu.Unlock()

	if event.Key != "env:"+envKey {
		t.Fatalf("event.Key = %q, want %q", event.Key, "env:"+envKey)
	}
	if event.Source != "env" {
		t.Fatalf("event.Source = %q, want %q", event.Source, "env")
	}
	if event.ChangedAt.IsZero() {
		t.Fatal("event.ChangedAt is zero")
	}
}

func TestWatcherSubscribeNilHandler(t *testing.T) {
	w := NewWatcher(50*time.Millisecond, nil)

	// Should not panic.
	w.Subscribe(nil)

	w.mu.RLock()
	count := len(w.subscribers)
	w.mu.RUnlock()

	if count != 0 {
		t.Fatalf("subscriber count = %d after nil Subscribe, want 0", count)
	}
}

// ---------------------------------------------------------------------------
// Check detects env var changes
// ---------------------------------------------------------------------------

func TestWatcherCheckDetectsChange(t *testing.T) {
	envKey := "TEST_WATCHER_CHECK_" + t.Name()
	t.Setenv(envKey, "before")

	w := NewWatcher(time.Minute, []string{"env:" + envKey})

	// Take initial snapshot.
	w.takeSnapshot()

	// No change yet.
	events := w.Check()
	if len(events) != 0 {
		t.Fatalf("Check() returned %d events before change, want 0", len(events))
	}

	// Change the value.
	os.Setenv(envKey, "after")
	t.Cleanup(func() { os.Unsetenv(envKey) })

	events = w.Check()
	if len(events) != 1 {
		t.Fatalf("Check() returned %d events, want 1", len(events))
	}
	if events[0].Key != "env:"+envKey {
		t.Fatalf("event.Key = %q, want %q", events[0].Key, "env:"+envKey)
	}
	if events[0].Source != "env" {
		t.Fatalf("event.Source = %q, want %q", events[0].Source, "env")
	}
}

func TestWatcherCheckNoChangeNoEvents(t *testing.T) {
	envKey := "TEST_WATCHER_NOCHANGE_" + t.Name()
	t.Setenv(envKey, "stable")

	w := NewWatcher(time.Minute, []string{"env:" + envKey})

	// Take initial snapshot.
	w.takeSnapshot()

	events := w.Check()
	if len(events) != 0 {
		t.Fatalf("Check() returned %d events, want 0", len(events))
	}
}

// ---------------------------------------------------------------------------
// Snapshot returns current state
// ---------------------------------------------------------------------------

func TestWatcherSnapshot(t *testing.T) {
	envKey := "TEST_WATCHER_SNAP_" + t.Name()
	t.Setenv(envKey, "snap-value")

	w := NewWatcher(time.Minute, []string{"env:" + envKey})

	// Before snapshot is taken, map should be empty.
	snap := w.Snapshot()
	if len(snap) != 0 {
		t.Fatalf("Snapshot() len = %d before Start, want 0", len(snap))
	}

	// Take snapshot.
	w.takeSnapshot()

	snap = w.Snapshot()
	if len(snap) != 1 {
		t.Fatalf("Snapshot() len = %d, want 1", len(snap))
	}

	hash, ok := snap["env:"+envKey]
	if !ok {
		t.Fatalf("Snapshot() missing key %q", "env:"+envKey)
	}
	if hash == "" {
		t.Fatal("Snapshot() hash is empty")
	}

	// Verify it's a SHA-256 hex string (64 chars).
	if len(hash) != 64 {
		t.Fatalf("Snapshot() hash len = %d, want 64 (SHA-256 hex)", len(hash))
	}
}

func TestWatcherSnapshotIsCopy(t *testing.T) {
	envKey := "TEST_WATCHER_SNAPCOPY_" + t.Name()
	t.Setenv(envKey, "copy-value")

	w := NewWatcher(time.Minute, []string{"env:" + envKey})
	w.takeSnapshot()

	snap := w.Snapshot()
	// Mutate the returned map.
	snap["injected"] = "malicious"

	// Original should be unaffected.
	snap2 := w.Snapshot()
	if _, ok := snap2["injected"]; ok {
		t.Fatal("Snapshot() returned a reference to internal state, want a copy")
	}
}

// ---------------------------------------------------------------------------
// sourceForKey
// ---------------------------------------------------------------------------

func TestSourceForKey(t *testing.T) {
	tests := []struct {
		key  string
		want string
	}{
		{"keychain:my-secret", "keychain"},
		{"env:MY_VAR", "env"},
		{"literal-value", "file"},
		{"", "file"},
	}
	for _, tt := range tests {
		got := sourceForKey(tt.key)
		if got != tt.want {
			t.Errorf("sourceForKey(%q) = %q, want %q", tt.key, got, tt.want)
		}
	}
}

// ---------------------------------------------------------------------------
// hashSecretValue
// ---------------------------------------------------------------------------

func TestHashSecretValue(t *testing.T) {
	h1 := hashSecretValue("hello")
	h2 := hashSecretValue("hello")
	h3 := hashSecretValue("world")

	if h1 != h2 {
		t.Fatal("hashSecretValue() not deterministic")
	}
	if h1 == h3 {
		t.Fatal("hashSecretValue() same hash for different inputs")
	}
	if len(h1) != 64 {
		t.Fatalf("hashSecretValue() len = %d, want 64", len(h1))
	}
}

// ---------------------------------------------------------------------------
// NewWatcher defaults
// ---------------------------------------------------------------------------

func TestNewWatcherDefaultInterval(t *testing.T) {
	w := NewWatcher(0, nil)
	if w.interval != defaultWatchInterval {
		t.Fatalf("interval = %v, want %v", w.interval, defaultWatchInterval)
	}
}

func TestNewWatcherNegativeInterval(t *testing.T) {
	w := NewWatcher(-time.Second, nil)
	if w.interval != defaultWatchInterval {
		t.Fatalf("interval = %v, want %v", w.interval, defaultWatchInterval)
	}
}

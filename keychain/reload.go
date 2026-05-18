package keychain

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"sync"
	"time"
)

// ---------------------------------------------------------------------------
// Constants
// ---------------------------------------------------------------------------

const (
	// defaultWatchInterval is the default polling interval for the Watcher.
	defaultWatchInterval = 60 * time.Second
)

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

// ChangeHandler is called when a watched secret changes.
type ChangeHandler func(event ChangeEvent)

// ChangeEvent describes a detected change to a watched secret.
type ChangeEvent struct {
	Key       string    `json:"key"`
	Source    string    `json:"source"` // "keychain", "env", "file"
	ChangedAt time.Time `json:"changed_at"`
}

// Watcher monitors keychain and environment changes, notifying subscribers
// when secrets change. This enables hot-reload of credentials without restart.
type Watcher struct {
	mu          sync.RWMutex
	interval    time.Duration
	subscribers []ChangeHandler
	snapshots   map[string]string // key -> hash of value
	stopCh      chan struct{}
	running     bool
	keys        []string // keys to watch
}

// ---------------------------------------------------------------------------
// Constructor
// ---------------------------------------------------------------------------

// NewWatcher creates a Watcher that polls for changes at the given interval.
// Keys should use the same prefix format as ResolveSecret (e.g. "env:VAR",
// "keychain:key").
func NewWatcher(interval time.Duration, keys []string) *Watcher {
	if interval <= 0 {
		interval = defaultWatchInterval
	}
	keyCopy := make([]string, len(keys))
	copy(keyCopy, keys)

	return &Watcher{
		interval:    interval,
		subscribers: nil,
		snapshots:   make(map[string]string),
		keys:        keyCopy,
	}
}

// ---------------------------------------------------------------------------
// Public methods
// ---------------------------------------------------------------------------

// Subscribe registers a change handler that is called when a watched secret
// changes. Must be called before Start.
func (w *Watcher) Subscribe(handler ChangeHandler) {
	if handler == nil {
		return
	}
	w.mu.Lock()
	w.subscribers = append(w.subscribers, handler)
	w.mu.Unlock()
}

// Start begins polling for changes in a background goroutine. It takes an
// initial snapshot before returning. The polling stops when ctx is cancelled
// or Stop is called.
func (w *Watcher) Start(ctx context.Context) error {
	w.mu.Lock()
	if w.running {
		w.mu.Unlock()
		return fmt.Errorf("watcher: already running")
	}
	w.running = true
	w.stopCh = make(chan struct{})
	w.mu.Unlock()

	// Take initial snapshot.
	w.takeSnapshot()

	go w.poll(ctx)
	return nil
}

// Stop stops the polling goroutine.
func (w *Watcher) Stop() {
	w.mu.Lock()
	defer w.mu.Unlock()

	if !w.running {
		return
	}
	w.running = false
	close(w.stopCh)
}

// Check performs a one-shot comparison of current secret values against
// the last snapshot and returns any detected changes. It does NOT notify
// subscribers; callers who want notification should use Start instead.
func (w *Watcher) Check() []ChangeEvent {
	w.mu.RLock()
	oldSnaps := make(map[string]string, len(w.snapshots))
	for k, v := range w.snapshots {
		oldSnaps[k] = v
	}
	w.mu.RUnlock()

	now := time.Now()
	var events []ChangeEvent

	for _, key := range w.keys {
		hash := hashSecretValue(resolveValue(key))
		if old, ok := oldSnaps[key]; ok && old != hash {
			events = append(events, ChangeEvent{
				Key:       key,
				Source:    sourceForKey(key),
				ChangedAt: now,
			})
		}
	}

	// Update snapshot with current values.
	w.mu.Lock()
	for _, key := range w.keys {
		w.snapshots[key] = hashSecretValue(resolveValue(key))
	}
	w.mu.Unlock()

	return events
}

// Snapshot returns a copy of the current key-to-hash snapshot.
func (w *Watcher) Snapshot() map[string]string {
	w.mu.RLock()
	defer w.mu.RUnlock()

	out := make(map[string]string, len(w.snapshots))
	for k, v := range w.snapshots {
		out[k] = v
	}
	return out
}

// ---------------------------------------------------------------------------
// Internal methods
// ---------------------------------------------------------------------------

func (w *Watcher) poll(ctx context.Context) {
	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			w.mu.Lock()
			w.running = false
			w.mu.Unlock()
			return
		case <-w.stopCh:
			return
		case <-ticker.C:
			events := w.detect()
			if len(events) > 0 {
				w.notify(events)
			}
		}
	}
}

// takeSnapshot captures the initial hash for each watched key.
func (w *Watcher) takeSnapshot() {
	w.mu.Lock()
	defer w.mu.Unlock()

	for _, key := range w.keys {
		w.snapshots[key] = hashSecretValue(resolveValue(key))
	}
}

// detect compares current values with the snapshot and returns changes.
func (w *Watcher) detect() []ChangeEvent {
	now := time.Now()
	var events []ChangeEvent

	w.mu.Lock()
	defer w.mu.Unlock()

	for _, key := range w.keys {
		hash := hashSecretValue(resolveValue(key))
		if old, ok := w.snapshots[key]; ok && old != hash {
			events = append(events, ChangeEvent{
				Key:       key,
				Source:    sourceForKey(key),
				ChangedAt: now,
			})
		}
		w.snapshots[key] = hash
	}

	return events
}

// notify calls all subscribers with the given events.
func (w *Watcher) notify(events []ChangeEvent) {
	w.mu.RLock()
	subs := make([]ChangeHandler, len(w.subscribers))
	copy(subs, w.subscribers)
	w.mu.RUnlock()

	for _, event := range events {
		for _, handler := range subs {
			handler(event)
		}
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// resolveValue resolves a key to its current value using the same prefix
// conventions as ResolveSecret. On error, returns empty string so the hash
// changes if the value disappears.
func resolveValue(key string) string {
	val, err := ResolveSecret(key)
	if err != nil {
		return ""
	}
	return val
}

// hashSecretValue computes a SHA-256 hex digest of the given value.
func hashSecretValue(value string) string {
	h := sha256.Sum256([]byte(value))
	return hex.EncodeToString(h[:])
}

// sourceForKey infers the source type from the key prefix.
func sourceForKey(key string) string {
	switch {
	case len(key) > len(keychainPrefix) && key[:len(keychainPrefix)] == keychainPrefix:
		return "keychain"
	case len(key) > len(envPrefix) && key[:len(envPrefix)] == envPrefix:
		return "env"
	default:
		return "file"
	}
}

// IsRunning reports whether the watcher is currently polling.
func (w *Watcher) IsRunning() bool {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.running
}

// SetEnvForTest is a test helper that sets an environment variable. It is
// only used by tests in this package and does NOT call t.Setenv because the
// watcher tests manipulate env vars outside the test goroutine.
func SetEnvForTest(key, value string) {
	os.Setenv(key, value)
}

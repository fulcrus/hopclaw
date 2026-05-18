package config

import (
	"context"
	"crypto/sha256"
	"errors"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/fulcrus/hopclaw/logging"
)

var log = logging.WithSubsystem("config")

// ReloadCallback is invoked when config changes.
// Receives old and new config. Return error to reject the reload.
type ReloadCallback func(old, new Config) error

// ReloadCallbackV2 receives the old config, new config, and the detected changes.
type ReloadCallbackV2 func(old, new Config, changes ChangeSet) error

// Watcher monitors a config file for changes and triggers callbacks.
type Watcher struct {
	path        string
	interval    time.Duration
	callbacks   []ReloadCallback
	callbacksV2 []ReloadCallbackV2
	debounce    time.Duration

	mu       sync.RWMutex
	current  Config
	lastHash [32]byte
}

// NewWatcher creates a config watcher.
func NewWatcher(path string, initial Config, interval time.Duration) *Watcher {
	if interval <= 0 {
		interval = 5 * time.Second
	}
	hash := hashFile(path)
	return &Watcher{
		path:     path,
		interval: interval,
		debounce: 100 * time.Millisecond,
		current:  initial,
		lastHash: hash,
	}
}

// OnReload registers a callback for config changes.
func (w *Watcher) OnReload(cb ReloadCallback) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.callbacks = append(w.callbacks, cb)
}

// OnReloadV2 registers a callback that also receives the change set.
func (w *Watcher) OnReloadV2(cb ReloadCallbackV2) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.callbacksV2 = append(w.callbacksV2, cb)
}

// Current returns the current config.
func (w *Watcher) Current() Config {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.current
}

// Run starts watching for config changes. Blocks until ctx is cancelled.
func (w *Watcher) Run(ctx context.Context) {
	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()

	var pendingCheck atomic.Bool

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			// Debounce: if a check was recently triggered, skip.
			if pendingCheck.CompareAndSwap(false, true) {
				time.AfterFunc(w.debounce, func() {
					_, _ = w.reload(false)
					pendingCheck.Store(false)
				})
			}
		}
	}
}

// Reload forces an immediate config reload attempt.
// It returns nil when the file has not changed or when the reload succeeds.
func (w *Watcher) Reload() error {
	_, err := w.reload(true)
	return err
}

func (w *Watcher) reload(force bool) (bool, error) {
	w.mu.Lock()
	newHash := hashFile(w.path)
	changed := newHash != w.lastHash
	if !changed && !force {
		w.mu.Unlock()
		return false, nil
	}
	oldCfg := w.current
	cbs := append([]ReloadCallback(nil), w.callbacks...)
	cbsV2 := append([]ReloadCallbackV2(nil), w.callbacksV2...)
	w.mu.Unlock()

	newCfg, err := Load(w.path)
	if err != nil {
		log.Warn("config reload: failed to parse", "path", w.path, "error", err)
		return false, err
	}

	changes := Diff(oldCfg, newCfg)
	if !changes.HasChanges() {
		// Hash changed but config is semantically identical.
		w.mu.Lock()
		w.lastHash = newHash
		w.mu.Unlock()
		return false, nil
	}

	if changes.Fatal {
		err := errors.New("config contains non-reloadable changes, restart required")
		log.Warn("config reload: contains non-reloadable changes, restart required",
			"sections", changes.Sections())
		return false, err
	}

	// Run v1 callbacks.
	for _, cb := range cbs {
		if err := cb(oldCfg, newCfg); err != nil {
			log.Warn("config reload: callback rejected", "error", err)
			return false, err
		}
	}

	// Run v2 callbacks.
	for _, cb := range cbsV2 {
		if err := cb(oldCfg, newCfg, changes); err != nil {
			log.Warn("config reload: v2 callback rejected", "error", err)
			return false, err
		}
	}

	w.mu.Lock()
	w.current = newCfg
	w.lastHash = newHash
	w.mu.Unlock()

	log.Info("config reloaded", "path", w.path, "changes", changes.String())
	return true, nil
}

func hashFile(path string) [32]byte {
	data, err := os.ReadFile(path)
	if err != nil {
		return [32]byte{}
	}
	return sha256.Sum256(data)
}

package channels

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// ---------------------------------------------------------------------------
// Thread Binding — Bind Agent sessions to channel threads/topics
// ---------------------------------------------------------------------------

// ThreadBinding manages channel thread/topic to Agent session key mappings.
type ThreadBinding struct {
	mu       sync.RWMutex
	bindings map[string]string // "channel:thread_id" → session_key
	path     string
	loaded   bool
}

// NewThreadBinding creates a new thread binding manager.
func NewThreadBinding() *ThreadBinding {
	return &ThreadBinding{bindings: make(map[string]string)}
}

func NewPersistentThreadBinding(path string) *ThreadBinding {
	return &ThreadBinding{
		bindings: make(map[string]string),
		path:     strings.TrimSpace(path),
	}
}

// Bind associates a thread/topic with a session key.
func (tb *ThreadBinding) Bind(channel, threadID, sessionKey string) {
	tb.mu.Lock()
	defer tb.mu.Unlock()
	tb.loadLocked()
	key := fmt.Sprintf("%s:%s", channel, threadID)
	tb.bindings[key] = sessionKey
	tb.persistLocked()
}

// Resolve looks up the session key for a thread/topic.
func (tb *ThreadBinding) Resolve(channel, threadID string) (string, bool) {
	tb.mu.Lock()
	defer tb.mu.Unlock()
	tb.loadLocked()
	key := fmt.Sprintf("%s:%s", channel, threadID)
	sk, ok := tb.bindings[key]
	return sk, ok
}

// Unbind removes a thread/topic binding.
func (tb *ThreadBinding) Unbind(channel, threadID string) {
	tb.mu.Lock()
	defer tb.mu.Unlock()
	tb.loadLocked()
	key := fmt.Sprintf("%s:%s", channel, threadID)
	delete(tb.bindings, key)
	tb.persistLocked()
}

// ListByChannel returns all bindings for a given channel.
func (tb *ThreadBinding) ListByChannel(channel string) map[string]string {
	tb.mu.Lock()
	defer tb.mu.Unlock()
	tb.loadLocked()

	prefix := channel + ":"
	result := make(map[string]string)
	for k, v := range tb.bindings {
		if len(k) > len(prefix) && k[:len(prefix)] == prefix {
			threadID := k[len(prefix):]
			result[threadID] = v
		}
	}
	return result
}

func (tb *ThreadBinding) List() map[string]string {
	tb.mu.Lock()
	defer tb.mu.Unlock()
	tb.loadLocked()
	result := make(map[string]string, len(tb.bindings))
	for key, value := range tb.bindings {
		result[key] = value
	}
	return result
}

func (tb *ThreadBinding) loadLocked() {
	if tb == nil || tb.loaded || tb.path == "" {
		tb.loaded = true
		return
	}
	tb.loaded = true
	data, err := os.ReadFile(tb.path)
	if err != nil {
		return
	}
	var raw map[string]string
	if err := json.Unmarshal(data, &raw); err != nil {
		return
	}
	for key, value := range raw {
		if strings.TrimSpace(key) == "" || strings.TrimSpace(value) == "" {
			continue
		}
		tb.bindings[key] = value
	}
}

func (tb *ThreadBinding) persistLocked() {
	if tb == nil || tb.path == "" {
		return
	}
	_ = os.MkdirAll(filepath.Dir(tb.path), 0o755)
	data, err := json.Marshal(tb.bindings)
	if err != nil {
		return
	}
	_ = os.WriteFile(tb.path, data, 0o644)
}

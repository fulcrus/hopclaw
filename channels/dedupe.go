package channels

import (
	"encoding/json"
	stdlog "log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type MessageDeduper struct {
	path string
	ttl  time.Duration

	mu             sync.Mutex
	memory         map[string]time.Time
	loaded         bool
	lastErr        error
	errText        string
	dirty          bool
	persisted      bool
	flushScheduled bool
	persistDelay   time.Duration
}

func NewMessageDeduper(path string, ttl time.Duration) *MessageDeduper {
	if ttl <= 0 {
		ttl = 24 * time.Hour
	}
	return &MessageDeduper{
		path:         strings.TrimSpace(path),
		ttl:          ttl,
		memory:       make(map[string]time.Time),
		persistDelay: 250 * time.Millisecond,
	}
}

func (d *MessageDeduper) Seen(key string) bool {
	if d == nil {
		return false
	}
	key = strings.TrimSpace(key)
	if key == "" {
		return false
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	d.loadLocked()
	now := time.Now().UTC()
	changed := d.pruneLocked(now)
	if _, ok := d.memory[key]; ok {
		if changed || d.dirty {
			d.persistStateChangeLocked()
		}
		return true
	}
	d.memory[key] = now
	d.dirty = true
	d.persistStateChangeLocked()
	return false
}

func (d *MessageDeduper) LastError() error {
	if d == nil {
		return nil
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.lastErr
}

func (d *MessageDeduper) loadLocked() {
	if d.loaded || d.path == "" {
		d.loaded = true
		return
	}
	d.loaded = true
	data, err := os.ReadFile(d.path)
	if err != nil {
		if !os.IsNotExist(err) {
			d.recordErrLocked(err)
		}
		return
	}
	var raw map[string]int64
	if err := json.Unmarshal(data, &raw); err != nil {
		d.recordErrLocked(err)
		return
	}
	for key, unix := range raw {
		if unix > 0 {
			d.memory[key] = time.Unix(unix, 0).UTC()
		}
	}
	d.clearErrLocked()
}

func (d *MessageDeduper) pruneLocked(now time.Time) bool {
	changed := false
	if d.ttl <= 0 {
		return false
	}
	for key, seenAt := range d.memory {
		if now.Sub(seenAt) >= d.ttl {
			delete(d.memory, key)
			changed = true
		}
	}
	return changed
}

func (d *MessageDeduper) persistLocked() {
	if d.path == "" {
		return
	}
	if err := os.MkdirAll(filepath.Dir(d.path), 0o755); err != nil {
		d.recordErrLocked(err)
		return
	}
	payload := make(map[string]int64, len(d.memory))
	for key, seenAt := range d.memory {
		payload[key] = seenAt.Unix()
	}
	data, err := json.Marshal(payload)
	if err != nil {
		d.recordErrLocked(err)
		return
	}
	tmpPath := d.path + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0o644); err != nil {
		d.recordErrLocked(err)
		return
	}
	if err := os.Rename(tmpPath, d.path); err != nil {
		_ = os.Remove(tmpPath)
		d.recordErrLocked(err)
		return
	}
	d.clearErrLocked()
}

func (d *MessageDeduper) persistStateChangeLocked() {
	if d == nil {
		return
	}
	if d.path == "" {
		d.persisted = true
		d.dirty = false
		return
	}
	if !d.persisted {
		d.persistLocked()
		if d.lastErr == nil {
			d.persisted = true
			d.dirty = false
		}
		return
	}
	d.schedulePersistLocked()
}

func (d *MessageDeduper) schedulePersistLocked() {
	if d == nil || d.path == "" || d.flushScheduled {
		return
	}
	delay := d.persistDelay
	if delay <= 0 {
		delay = time.Millisecond
	}
	d.flushScheduled = true
	go d.flushAfterDelay(delay)
}

func (d *MessageDeduper) flushAfterDelay(delay time.Duration) {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	<-timer.C

	d.mu.Lock()
	defer d.mu.Unlock()
	d.flushScheduled = false
	if !d.persisted || !d.dirty {
		return
	}
	d.persistLocked()
	if d.lastErr == nil {
		d.dirty = false
		return
	}
	if d.dirty {
		d.schedulePersistLocked()
	}
}

func (d *MessageDeduper) recordErrLocked(err error) {
	if d == nil || err == nil {
		return
	}
	msg := strings.TrimSpace(err.Error())
	if msg != "" && msg != d.errText {
		stdlog.Printf("channels dedupe persistence error for %s: %v", d.path, err)
	}
	d.lastErr = err
	d.errText = msg
}

func (d *MessageDeduper) clearErrLocked() {
	if d == nil {
		return
	}
	d.lastErr = nil
	d.errText = ""
}

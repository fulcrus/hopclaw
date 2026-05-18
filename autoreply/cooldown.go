package autoreply

import (
	"sync"
	"time"
)

// ---------------------------------------------------------------------------
// Constants
// ---------------------------------------------------------------------------

const (
	// cooldownCleanupInterval is how often expired entries are reaped.
	cooldownCleanupInterval = 10 * time.Minute
	// cooldownDefaultTTL is the maximum age before an entry is eligible for
	// cleanup regardless of the rule's configured cooldown.
	cooldownDefaultTTL = time.Hour
)

// ---------------------------------------------------------------------------
// CooldownTracker
// ---------------------------------------------------------------------------

// cooldownKey uniquely identifies a rule+session pair without risk of
// string-concatenation collisions (e.g. ruleID="a:b" + sessionKey="c"
// vs. ruleID="a" + sessionKey="b:c").
type cooldownKey struct {
	ruleID     string
	sessionKey string
}

// CooldownTracker records per-session, per-rule fire times and enforces
// minimum intervals between repeated auto-replies.
type CooldownTracker struct {
	mu      sync.Mutex                 // guards entries and counts
	entries map[cooldownKey]time.Time  // last fired time per rule+session
	counts  map[cooldownKey]*hourCount // fires in current hour window

	startOnce sync.Once
	stopOnce  sync.Once
	done      chan struct{}
}

// hourCount tracks how many times a rule fired within a rolling hour.
type hourCount struct {
	fires     int
	windowEnd time.Time // fires reset after this time
}

// NewCooldownTracker returns a new tracker. Call Start to begin the background
// cleanup goroutine.
func NewCooldownTracker() *CooldownTracker {
	return &CooldownTracker{
		entries: make(map[cooldownKey]time.Time),
		counts:  make(map[cooldownKey]*hourCount),
		done:    make(chan struct{}),
	}
}

// Start launches a background goroutine that periodically purges expired
// cooldown entries. It is safe to call multiple times; only the first call
// starts the goroutine.
func (t *CooldownTracker) Start() {
	t.startOnce.Do(func() {
		go func() {
			ticker := time.NewTicker(cooldownCleanupInterval)
			defer ticker.Stop()
			for {
				select {
				case <-ticker.C:
					t.cleanup()
				case <-t.done:
					return
				}
			}
		}()
	})
}

// Stop terminates the background cleanup goroutine. It is safe to call
// multiple times.
func (t *CooldownTracker) Stop() {
	t.stopOnce.Do(func() {
		close(t.done)
	})
}

// IsOnCooldown returns true if the given rule+session pair has been fired
// within the specified cooldown duration.
func (t *CooldownTracker) IsOnCooldown(ruleID, sessionKey string, cooldown time.Duration) bool {
	if cooldown <= 0 {
		return false
	}

	key := cooldownKey{ruleID: ruleID, sessionKey: sessionKey}

	t.mu.Lock()
	defer t.mu.Unlock()

	last, ok := t.entries[key]
	if !ok {
		return false
	}
	return time.Since(last) < cooldown
}

// ExceedsMaxPerHour returns true if the rule+session pair has already been
// fired maxPerHour times within the current hour window.
func (t *CooldownTracker) ExceedsMaxPerHour(ruleID, sessionKey string, maxPerHour int) bool {
	if maxPerHour <= 0 {
		return false
	}

	key := cooldownKey{ruleID: ruleID, sessionKey: sessionKey}
	now := time.Now()

	t.mu.Lock()
	defer t.mu.Unlock()

	hc, ok := t.counts[key]
	if !ok || now.After(hc.windowEnd) {
		return false // no record or window expired
	}
	return hc.fires >= maxPerHour
}

// RecordFire records that a rule fired for the given session.
func (t *CooldownTracker) RecordFire(ruleID, sessionKey string) {
	key := cooldownKey{ruleID: ruleID, sessionKey: sessionKey}
	now := time.Now()

	t.mu.Lock()
	defer t.mu.Unlock()

	t.entries[key] = now

	hc, ok := t.counts[key]
	if !ok || now.After(hc.windowEnd) {
		t.counts[key] = &hourCount{fires: 1, windowEnd: now.Add(time.Hour)}
	} else {
		hc.fires++
	}
}

// cleanup removes entries older than cooldownDefaultTTL and expired hour
// windows.
func (t *CooldownTracker) cleanup() {
	t.mu.Lock()
	defer t.mu.Unlock()

	now := time.Now()
	cutoff := now.Add(-cooldownDefaultTTL)
	for k, v := range t.entries {
		if v.Before(cutoff) {
			delete(t.entries, k)
		}
	}
	for k, hc := range t.counts {
		if now.After(hc.windowEnd) {
			delete(t.counts, k)
		}
	}
}

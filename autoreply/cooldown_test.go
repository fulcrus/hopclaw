package autoreply

import (
	"testing"
	"time"
)

func TestCooldownTrackerNotOnCooldownByDefault(t *testing.T) {
	t.Parallel()

	tracker := NewCooldownTracker()
	if tracker.IsOnCooldown("rule-1", "session-1", time.Minute) {
		t.Fatal("expected no cooldown for unfired rule")
	}
}

func TestCooldownTrackerRecordAndCheck(t *testing.T) {
	t.Parallel()

	tracker := NewCooldownTracker()
	tracker.RecordFire("rule-1", "session-1")

	if !tracker.IsOnCooldown("rule-1", "session-1", time.Minute) {
		t.Fatal("expected cooldown to be active after RecordFire")
	}
}

func TestCooldownTrackerDifferentKeysIndependent(t *testing.T) {
	t.Parallel()

	tracker := NewCooldownTracker()
	tracker.RecordFire("rule-1", "session-1")

	if tracker.IsOnCooldown("rule-1", "session-2", time.Minute) {
		t.Fatal("different session should not be on cooldown")
	}
	if tracker.IsOnCooldown("rule-2", "session-1", time.Minute) {
		t.Fatal("different rule should not be on cooldown")
	}
}

func TestCooldownTrackerZeroCooldownNeverBlocks(t *testing.T) {
	t.Parallel()

	tracker := NewCooldownTracker()
	tracker.RecordFire("rule-1", "session-1")

	if tracker.IsOnCooldown("rule-1", "session-1", 0) {
		t.Fatal("zero cooldown should never be on cooldown")
	}
}

func TestCooldownTrackerNegativeCooldownNeverBlocks(t *testing.T) {
	t.Parallel()

	tracker := NewCooldownTracker()
	tracker.RecordFire("rule-1", "session-1")

	if tracker.IsOnCooldown("rule-1", "session-1", -time.Minute) {
		t.Fatal("negative cooldown should never be on cooldown")
	}
}

func TestCooldownTrackerCleanup(t *testing.T) {
	t.Parallel()

	tracker := NewCooldownTracker()

	// Manually insert an old entry.
	key := cooldownKey{ruleID: "rule-old", sessionKey: "session-old"}
	tracker.mu.Lock()
	tracker.entries[key] = time.Now().Add(-2 * cooldownDefaultTTL)
	tracker.mu.Unlock()

	tracker.cleanup()

	tracker.mu.Lock()
	_, exists := tracker.entries[key]
	tracker.mu.Unlock()

	if exists {
		t.Fatal("cleanup should have removed expired entry")
	}
}

func TestCooldownTrackerCleanupPreservesRecent(t *testing.T) {
	t.Parallel()

	tracker := NewCooldownTracker()
	tracker.RecordFire("rule-recent", "session-recent")

	tracker.cleanup()

	if !tracker.IsOnCooldown("rule-recent", "session-recent", time.Minute) {
		t.Fatal("cleanup should not remove recent entries")
	}
}

func TestCooldownTrackerMaxPerHour(t *testing.T) {
	t.Parallel()

	tracker := NewCooldownTracker()

	// No fires yet — should not exceed.
	if tracker.ExceedsMaxPerHour("rule-1", "sess-1", 3) {
		t.Fatal("expected false before any fires")
	}

	// Fire twice — still under limit of 3.
	tracker.RecordFire("rule-1", "sess-1")
	tracker.RecordFire("rule-1", "sess-1")
	if tracker.ExceedsMaxPerHour("rule-1", "sess-1", 3) {
		t.Fatal("expected false after 2 fires with limit 3")
	}

	// Fire third time — now at limit.
	tracker.RecordFire("rule-1", "sess-1")
	if !tracker.ExceedsMaxPerHour("rule-1", "sess-1", 3) {
		t.Fatal("expected true after 3 fires with limit 3")
	}

	// Different session should be independent.
	if tracker.ExceedsMaxPerHour("rule-1", "sess-2", 3) {
		t.Fatal("different session should not be affected")
	}
}

func TestCooldownTrackerMaxPerHourZeroOrNegativeNeverBlocks(t *testing.T) {
	t.Parallel()

	tracker := NewCooldownTracker()
	tracker.RecordFire("rule-1", "sess-1")

	if tracker.ExceedsMaxPerHour("rule-1", "sess-1", 0) {
		t.Fatal("zero max should never block")
	}
	if tracker.ExceedsMaxPerHour("rule-1", "sess-1", -1) {
		t.Fatal("negative max should never block")
	}
}

func TestCooldownTrackerStopIdempotent(t *testing.T) {
	t.Parallel()

	tracker := NewCooldownTracker()
	tracker.Start()
	tracker.Stop()
	tracker.Stop() // should not panic
}

func TestCooldownTrackerStartIdempotent(t *testing.T) {
	t.Parallel()

	tracker := NewCooldownTracker()
	tracker.Start()
	tracker.Start() // should not spawn duplicate goroutines
	tracker.Stop()
}

func TestCooldownTrackerCleanupPrunesExpiredHourCounts(t *testing.T) {
	t.Parallel()

	tracker := NewCooldownTracker()

	// Manually insert an expired hour count.
	key := cooldownKey{ruleID: "rule-old", sessionKey: "session-old"}
	tracker.mu.Lock()
	tracker.counts[key] = &hourCount{fires: 5, windowEnd: time.Now().Add(-time.Hour)}
	tracker.mu.Unlock()

	tracker.cleanup()

	tracker.mu.Lock()
	_, exists := tracker.counts[key]
	tracker.mu.Unlock()

	if exists {
		t.Fatal("cleanup should have removed expired hour count")
	}
}

package runtime

import (
	goruntime "runtime"
	"sync"
	"testing"
	"time"

	"github.com/fulcrus/hopclaw/agent"
	"github.com/fulcrus/hopclaw/eventbus"
)

type manualClock struct {
	mu      sync.Mutex
	cond    *sync.Cond
	now     time.Time
	tickers map[*manualTicker]struct{}
	timers  map[*manualTimer]struct{}
}

func newManualClock(start time.Time) *manualClock {
	clock := &manualClock{
		now:     start,
		tickers: make(map[*manualTicker]struct{}),
		timers:  make(map[*manualTimer]struct{}),
	}
	clock.cond = sync.NewCond(&clock.mu)
	return clock
}

func (c *manualClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *manualClock) Since(t time.Time) time.Duration {
	return c.Now().Sub(t)
}

func (c *manualClock) NewTicker(d time.Duration) Ticker {
	if d <= 0 {
		panic("non-positive interval for NewTicker")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	ticker := &manualTicker{
		clock:    c,
		interval: d,
		next:     c.now.Add(d),
		ch:       make(chan time.Time, 16),
	}
	c.tickers[ticker] = struct{}{}
	c.cond.Broadcast()
	return ticker
}

func (c *manualClock) NewTimer(d time.Duration) Timer {
	c.mu.Lock()
	defer c.mu.Unlock()
	timer := &manualTimer{
		clock:    c,
		deadline: c.now.Add(d),
		ch:       make(chan time.Time, 1),
	}
	c.timers[timer] = struct{}{}
	c.fireLocked()
	c.cond.Broadcast()
	return timer
}

func (c *manualClock) Advance(d time.Duration) {
	c.mu.Lock()
	c.now = c.now.Add(d)
	c.fireLocked()
	c.mu.Unlock()
}

func (c *manualClock) WaitForTickers(count int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for len(c.tickers) < count {
		c.cond.Wait()
	}
}

func (c *manualClock) WaitForTimers(count int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for len(c.timers) < count {
		c.cond.Wait()
	}
}

func (c *manualClock) fireLocked() {
	for ticker := range c.tickers {
		ticker.fireLocked(c.now)
	}
	for timer := range c.timers {
		if timer.fireLocked(c.now) {
			delete(c.timers, timer)
		}
	}
}

type manualTicker struct {
	clock    *manualClock
	interval time.Duration
	next     time.Time
	ch       chan time.Time
	stopped  bool
}

func (t *manualTicker) C() <-chan time.Time { return t.ch }

func (t *manualTicker) Stop() {
	if t == nil || t.clock == nil {
		return
	}
	t.clock.mu.Lock()
	defer t.clock.mu.Unlock()
	if t.stopped {
		return
	}
	t.stopped = true
	delete(t.clock.tickers, t)
}

func (t *manualTicker) fireLocked(now time.Time) {
	if t == nil || t.stopped {
		return
	}
	for !t.next.After(now) {
		select {
		case t.ch <- t.next:
		default:
		}
		t.next = t.next.Add(t.interval)
	}
}

type manualTimer struct {
	clock    *manualClock
	deadline time.Time
	ch       chan time.Time
	stopped  bool
	fired    bool
}

func (t *manualTimer) C() <-chan time.Time { return t.ch }

func (t *manualTimer) Stop() bool {
	if t == nil || t.clock == nil {
		return false
	}
	t.clock.mu.Lock()
	defer t.clock.mu.Unlock()
	active := !t.stopped && !t.fired
	t.stopped = true
	delete(t.clock.timers, t)
	return active
}

func (t *manualTimer) fireLocked(now time.Time) bool {
	if t == nil || t.stopped || t.fired || now.Before(t.deadline) {
		return false
	}
	t.fired = true
	select {
	case t.ch <- t.deadline:
	default:
	}
	return true
}

func TestEventSnapshotCacheExpiresWithInjectedClock(t *testing.T) {
	t.Parallel()

	clock := newManualClock(time.Date(2026, 4, 13, 8, 0, 0, 0, time.UTC))
	reader := &countingReplayReader{events: []eventbus.Event{{ID: "evt-1", Type: eventbus.EventRunCompleted}}}
	svc := NewService(nil, agent.NewInMemorySessionStore(), agent.NewInMemoryRunStore(), nil, eventbus.NewInMemoryBus(), nil).
		WithClock(clock).
		WithEventReader(reader)

	svc.EventSnapshot()
	svc.EventSnapshot()
	if reader.calls != 1 {
		t.Fatalf("ReplayContext() calls before TTL expiry = %d, want 1", reader.calls)
	}

	clock.Advance(eventReplayCacheTTL + time.Millisecond)
	svc.EventSnapshot()
	if reader.calls != 2 {
		t.Fatalf("ReplayContext() calls after TTL expiry = %d, want 2", reader.calls)
	}
}

func TestSessionRateLimiterRefillWithInjectedClock(t *testing.T) {
	t.Parallel()

	clock := newManualClock(time.Date(2026, 4, 13, 9, 0, 0, 0, time.UTC))
	rl := newSessionRateLimiterWithClock(600, 1, clock) // 10/sec, burst 1
	defer rl.Stop()

	if !rl.Allow("sess") {
		t.Fatal("first request should be allowed")
	}
	if rl.Allow("sess") {
		t.Fatal("second request should be denied")
	}

	clock.Advance(250 * time.Millisecond)
	if !rl.Allow("sess") {
		t.Fatal("request after logical refill should be allowed")
	}
}

func TestSessionRateLimiterCleanupUsesInjectedClock(t *testing.T) {
	t.Parallel()

	clock := newManualClock(time.Date(2026, 4, 13, 9, 30, 0, 0, time.UTC))
	rl := newSessionRateLimiterWithClock(60, 1, clock)
	defer rl.Stop()

	if !rl.Allow("sess") {
		t.Fatal("Allow() = false, want true")
	}
	clock.WaitForTickers(1)
	clock.Advance(rateLimitBucketExpiry + rateLimitCleanupInterval + time.Second)

	for i := 0; i < 1000; i++ {
		rl.mu.Lock()
		_, exists := rl.buckets["sess"]
		rl.mu.Unlock()
		if !exists {
			return
		}
		goruntime.Gosched()
	}
	t.Fatal("expected expired bucket to be pruned by cleanup ticker")
}

func TestManualClockTimerFiresOnAdvance(t *testing.T) {
	t.Parallel()

	clock := newManualClock(time.Date(2026, 4, 13, 10, 0, 0, 0, time.UTC))
	timer := clock.NewTimer(time.Second)
	clock.WaitForTimers(1)

	select {
	case <-timer.C():
		t.Fatal("timer fired before clock advance")
	default:
	}

	clock.Advance(time.Second)

	select {
	case <-timer.C():
	default:
		t.Fatal("timer did not fire after clock advance")
	}
}

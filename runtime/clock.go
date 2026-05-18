package runtime

import "time"

// Clock provides a swappable time source for runtime hot paths and tests.
type Clock interface {
	Now() time.Time
	Since(time.Time) time.Duration
	NewTicker(time.Duration) Ticker
	NewTimer(time.Duration) Timer
}

// Ticker is the minimal ticker contract used by runtime loops.
type Ticker interface {
	C() <-chan time.Time
	Stop()
}

// Timer is the minimal timer contract used by timeout paths.
type Timer interface {
	C() <-chan time.Time
	Stop() bool
}

// Scheduler provides a swappable async execution hook for runtime dispatch.
type Scheduler interface {
	Go(func())
}

var (
	defaultRuntimeClock     Clock     = systemClock{}
	defaultRuntimeScheduler Scheduler = systemScheduler{}
)

type systemClock struct{}

func (systemClock) Now() time.Time { return time.Now() }

func (systemClock) Since(t time.Time) time.Duration { return time.Since(t) }

func (systemClock) NewTicker(d time.Duration) Ticker {
	return systemTicker{ticker: time.NewTicker(d)}
}

func (systemClock) NewTimer(d time.Duration) Timer {
	return systemTimer{timer: time.NewTimer(d)}
}

type systemTicker struct {
	ticker *time.Ticker
}

func (t systemTicker) C() <-chan time.Time { return t.ticker.C }

func (t systemTicker) Stop() { t.ticker.Stop() }

type systemTimer struct {
	timer *time.Timer
}

func (t systemTimer) C() <-chan time.Time { return t.timer.C }

func (t systemTimer) Stop() bool { return t.timer.Stop() }

type systemScheduler struct{}

func (systemScheduler) Go(fn func()) { go fn() }

// WithClock overrides the runtime clock used by background loops, replay cache
// TTL checks, and one-shot polling.
func (s *Service) WithClock(clock Clock) *Service {
	if s == nil {
		return nil
	}
	s.clock = clock
	return s
}

// WithScheduler overrides async dispatch execution for runtime tests.
func (s *Service) WithScheduler(scheduler Scheduler) *Service {
	if s == nil {
		return nil
	}
	s.scheduler = scheduler
	return s
}

func (s *Service) runtimeClock() Clock {
	if s != nil && s.clock != nil {
		return s.clock
	}
	return defaultRuntimeClock
}

func (s *Service) runtimeScheduler() Scheduler {
	if s != nil && s.scheduler != nil {
		return s.scheduler
	}
	return defaultRuntimeScheduler
}

func (s *Service) nowUTC() time.Time {
	return s.runtimeClock().Now().UTC()
}

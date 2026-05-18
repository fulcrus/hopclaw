package logging

import (
	"context"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"
)

// ---------------------------------------------------------------------------
// Constants
// ---------------------------------------------------------------------------

const (
	defaultSamplingInitialN    = 10
	defaultSamplingThereafterN = 100
	defaultSamplingIntervalSec = 60
)

// ---------------------------------------------------------------------------
// SamplingConfig
// ---------------------------------------------------------------------------

// SamplingConfig controls log entry sampling to rate-limit repetitive entries.
type SamplingConfig struct {
	Enabled     bool `json:"enabled" yaml:"enabled"`
	InitialN    int  `json:"initial_n" yaml:"initial_n"`       // log first N entries per interval
	ThereafterN int  `json:"thereafter_n" yaml:"thereafter_n"` // then log every Nth entry
	IntervalSec int  `json:"interval_sec" yaml:"interval_sec"` // reset counter interval in seconds
}

// ---------------------------------------------------------------------------
// Sampling handler
// ---------------------------------------------------------------------------

// samplingHandler wraps a slog.Handler and rate-limits repetitive log entries.
// The first InitialN occurrences of each (level, message) pair within an
// interval pass through; after that only every ThereafterN-th entry passes.
// Counters reset every IntervalSec seconds.
type samplingHandler struct {
	inner       slog.Handler
	initialN    int
	thereafterN int
	interval    time.Duration

	mu       sync.Mutex // guards counters and resetAt
	counters map[samplingKey]*atomic.Int64
	resetAt  time.Time
}

// samplingKey is the composite key for tracking log entry counts.
type samplingKey struct {
	level   slog.Level
	message string
}

// Compile-time interface check.
var _ slog.Handler = (*samplingHandler)(nil)

// newSamplingHandler creates a new sampling handler wrapping inner. It applies
// defaults for any zero-value config fields.
func newSamplingHandler(inner slog.Handler, cfg SamplingConfig) *samplingHandler {
	initialN := cfg.InitialN
	if initialN <= 0 {
		initialN = defaultSamplingInitialN
	}
	thereafterN := cfg.ThereafterN
	if thereafterN <= 0 {
		thereafterN = defaultSamplingThereafterN
	}
	intervalSec := cfg.IntervalSec
	if intervalSec <= 0 {
		intervalSec = defaultSamplingIntervalSec
	}

	return &samplingHandler{
		inner:       inner,
		initialN:    initialN,
		thereafterN: thereafterN,
		interval:    time.Duration(intervalSec) * time.Second,
		counters:    make(map[samplingKey]*atomic.Int64),
		resetAt:     time.Now().Add(time.Duration(intervalSec) * time.Second),
	}
}

// Enabled delegates to the inner handler.
func (h *samplingHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.inner.Enabled(ctx, level)
}

// Handle checks the sampling counters and either passes or drops the record.
func (h *samplingHandler) Handle(ctx context.Context, r slog.Record) error {
	if !h.shouldLog(r.Level, r.Message) {
		return nil
	}
	return h.inner.Handle(ctx, r)
}

// WithAttrs returns a new samplingHandler wrapping the inner handler with attrs.
func (h *samplingHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &samplingHandler{
		inner:       h.inner.WithAttrs(attrs),
		initialN:    h.initialN,
		thereafterN: h.thereafterN,
		interval:    h.interval,
		counters:    h.counters,
		resetAt:     h.resetAt,
	}
}

// WithGroup returns a new samplingHandler with the given group on the inner handler.
func (h *samplingHandler) WithGroup(name string) slog.Handler {
	return &samplingHandler{
		inner:       h.inner.WithGroup(name),
		initialN:    h.initialN,
		thereafterN: h.thereafterN,
		interval:    h.interval,
		counters:    h.counters,
		resetAt:     h.resetAt,
	}
}

// shouldLog returns true if the entry should be emitted based on sampling rules.
func (h *samplingHandler) shouldLog(level slog.Level, message string) bool {
	h.mu.Lock()
	now := time.Now()
	if now.After(h.resetAt) {
		h.counters = make(map[samplingKey]*atomic.Int64)
		h.resetAt = now.Add(h.interval)
	}

	key := samplingKey{level: level, message: message}
	counter, ok := h.counters[key]
	if !ok {
		counter = &atomic.Int64{}
		h.counters[key] = counter
	}
	h.mu.Unlock()

	n := counter.Add(1)
	if n <= int64(h.initialN) {
		return true
	}
	return (n-int64(h.initialN))%int64(h.thereafterN) == 0
}

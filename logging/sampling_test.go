package logging

import (
	"bytes"
	"log/slog"
	"strings"
	"sync/atomic"
	"testing"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// setupSamplingBuffer creates a handler chain with sampling enabled and returns
// the output buffer. Uses JSON format for easy inspection.
func setupSamplingBuffer(t *testing.T, cfg SamplingConfig) *bytes.Buffer {
	t.Helper()

	var buf bytes.Buffer
	opts := &slog.HandlerOptions{Level: slog.LevelDebug}
	inner := slog.NewJSONHandler(&buf, opts)
	handler := newSamplingHandler(inner, cfg)
	redact := newRedactHandler(handler, defaultRedactKeys)
	levelH := newSubsystemLevelHandler(redact, &globalLevel, &subsystemLevel)

	globalLevel.Set(slog.LevelDebug)
	subsystemLevel.reset()

	slog.SetDefault(slog.New(levelH))
	return &buf
}

// ---------------------------------------------------------------------------
// Tests - Initial N
// ---------------------------------------------------------------------------

func TestSamplingInitialN(t *testing.T) {
	const initialN = 3
	buf := setupSamplingBuffer(t, SamplingConfig{
		Enabled:     true,
		InitialN:    initialN,
		ThereafterN: 1000, // effectively never log after initial
		IntervalSec: 60,
	})

	const totalMessages = 10
	for i := 0; i < totalMessages; i++ {
		slog.Info("repeated message")
	}

	out := buf.String()
	count := strings.Count(out, "repeated message")
	if count != initialN {
		t.Fatalf("expected %d messages to pass, got %d\nout: %s", initialN, count, out)
	}
}

// ---------------------------------------------------------------------------
// Tests - Thereafter N
// ---------------------------------------------------------------------------

func TestSamplingThereafter(t *testing.T) {
	const (
		initialN    = 2
		thereafterN = 5
	)
	buf := setupSamplingBuffer(t, SamplingConfig{
		Enabled:     true,
		InitialN:    initialN,
		ThereafterN: thereafterN,
		IntervalSec: 60,
	})

	// Send initialN + thereafterN messages. We expect:
	// - initialN pass unconditionally
	// - Then every thereafterN-th passes. After initialN, messages are numbered
	//   1..thereafterN relative to the threshold. The thereafterN-th (i.e. #5)
	//   should pass.
	const totalMessages = initialN + thereafterN
	for i := 0; i < totalMessages; i++ {
		slog.Info("sampled msg")
	}

	out := buf.String()
	count := strings.Count(out, "sampled msg")
	// initialN (2) + 1 thereafter (the 5th after initial) = 3
	const expectedCount = initialN + 1
	if count != expectedCount {
		t.Fatalf("expected %d messages, got %d\nout: %s", expectedCount, count, out)
	}
}

// ---------------------------------------------------------------------------
// Tests - Interval reset
// ---------------------------------------------------------------------------

func TestSamplingIntervalReset(t *testing.T) {
	const initialN = 2
	var buf bytes.Buffer
	opts := &slog.HandlerOptions{Level: slog.LevelDebug}
	inner := slog.NewJSONHandler(&buf, opts)
	sh := newSamplingHandler(inner, SamplingConfig{
		Enabled:     true,
		InitialN:    initialN,
		ThereafterN: 1000,
		IntervalSec: 1,
	})
	redact := newRedactHandler(sh, defaultRedactKeys)
	levelH := newSubsystemLevelHandler(redact, &globalLevel, &subsystemLevel)
	globalLevel.Set(slog.LevelDebug)
	subsystemLevel.reset()
	slog.SetDefault(slog.New(levelH))

	// Emit initialN messages — all should pass.
	const firstBatch = 5
	for i := 0; i < firstBatch; i++ {
		slog.Info("reset msg")
	}

	count1 := strings.Count(buf.String(), "reset msg")
	if count1 != initialN {
		t.Fatalf("first batch: expected %d messages, got %d", initialN, count1)
	}

	// Force a counter reset by moving resetAt to the past.
	sh.mu.Lock()
	sh.counters = make(map[samplingKey]*atomic.Int64)
	sh.mu.Unlock()

	// Emit another batch — counters are reset so initialN should pass again.
	const secondBatch = 5
	for i := 0; i < secondBatch; i++ {
		slog.Info("reset msg")
	}

	count2 := strings.Count(buf.String(), "reset msg")
	expectedTotal := initialN + initialN
	if count2 != expectedTotal {
		t.Fatalf("after reset: expected %d total messages, got %d", expectedTotal, count2)
	}
}

// ---------------------------------------------------------------------------
// Tests - Different messages are tracked independently
// ---------------------------------------------------------------------------

func TestSamplingDifferentMessages(t *testing.T) {
	const initialN = 1
	buf := setupSamplingBuffer(t, SamplingConfig{
		Enabled:     true,
		InitialN:    initialN,
		ThereafterN: 1000,
		IntervalSec: 60,
	})

	const messagesPerType = 5
	for i := 0; i < messagesPerType; i++ {
		slog.Info("message A")
		slog.Info("message B")
	}

	out := buf.String()
	countA := strings.Count(out, "message A")
	countB := strings.Count(out, "message B")
	if countA != initialN {
		t.Fatalf("message A: expected %d, got %d", initialN, countA)
	}
	if countB != initialN {
		t.Fatalf("message B: expected %d, got %d", initialN, countB)
	}
}

// ---------------------------------------------------------------------------
// Tests - Sampling defaults
// ---------------------------------------------------------------------------

func TestSamplingDefaults(t *testing.T) {
	sh := newSamplingHandler(slog.Default().Handler(), SamplingConfig{Enabled: true})
	if sh.initialN != defaultSamplingInitialN {
		t.Fatalf("initialN = %d, want %d", sh.initialN, defaultSamplingInitialN)
	}
	if sh.thereafterN != defaultSamplingThereafterN {
		t.Fatalf("thereafterN = %d, want %d", sh.thereafterN, defaultSamplingThereafterN)
	}
	expectedInterval := defaultSamplingIntervalSec
	if int(sh.interval.Seconds()) != expectedInterval {
		t.Fatalf("interval = %v, want %ds", sh.interval, expectedInterval)
	}
}

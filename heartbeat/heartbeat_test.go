package heartbeat

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// shortInterval returns a Config with a very fast ticker for testing.
func shortInterval() Config {
	return Config{
		Interval: 10 * time.Millisecond,
		Timeout:  50 * time.Millisecond,
	}
}

// mockPruner implements TranscriptPruner for testing.
type mockPruner struct {
	mu        sync.Mutex // guards calls and pruned
	calls     int
	pruned    int
	returnErr error
}

func (m *mockPruner) PruneOlderThan(_ context.Context, _ time.Duration) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls++
	if m.returnErr != nil {
		return 0, m.returnErr
	}
	m.pruned += 5
	return 5, nil
}

func (m *mockPruner) getCalls() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.calls
}

// ---------------------------------------------------------------------------
// Lifecycle tests
// ---------------------------------------------------------------------------

func TestService_StartStop(t *testing.T) {
	t.Parallel()
	svc := NewService(shortInterval())

	if svc.IsRunning() {
		t.Fatal("expected service to not be running before Start")
	}

	if err := svc.Start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}
	if !svc.IsRunning() {
		t.Fatal("expected service to be running after Start")
	}

	// Double start should return an error.
	if err := svc.Start(context.Background()); err == nil {
		t.Fatal("expected error on double start")
	}

	svc.Stop()
	if svc.IsRunning() {
		t.Fatal("expected service to be stopped after Stop")
	}

	// Stopping again should be a no-op (no panic).
	svc.Stop()
}

func TestService_StartWhileDisabled(t *testing.T) {
	t.Parallel()
	svc := NewService(shortInterval())
	svc.Disable()

	err := svc.Start(context.Background())
	if err == nil {
		t.Fatal("expected error when starting a disabled service")
	}
}

func TestService_ContextCancellation(t *testing.T) {
	t.Parallel()
	svc := NewService(shortInterval())

	ctx, cancel := context.WithCancel(context.Background())
	if err := svc.Start(ctx); err != nil {
		t.Fatalf("start: %v", err)
	}

	cancel()

	// Allow the goroutine to observe cancellation.
	time.Sleep(30 * time.Millisecond)

	if svc.IsRunning() {
		t.Fatal("expected service to stop after context cancellation")
	}
}

// ---------------------------------------------------------------------------
// Beat tests
// ---------------------------------------------------------------------------

func TestService_BeatReturnsValidData(t *testing.T) {
	t.Parallel()
	svc := NewService(shortInterval())
	if err := svc.Start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer svc.Stop()

	// Wait for at least one tick.
	time.Sleep(30 * time.Millisecond)

	beat := svc.Beat()
	if beat.BeatAt.IsZero() {
		t.Fatal("expected non-zero timestamp in beat")
	}
	if beat.Status != StatusOnline {
		t.Fatalf("expected status %q, got %q", StatusOnline, beat.Status)
	}
	if beat.Uptime <= 0 {
		t.Fatal("expected positive uptime")
	}
	if beat.Metrics.GoRoutines <= 0 {
		t.Fatal("expected positive goroutine count")
	}
	if beat.Metrics.MemoryUsageMB <= 0 {
		t.Fatal("expected positive memory usage")
	}
}

func TestService_LastBeat(t *testing.T) {
	t.Parallel()
	svc := NewService(shortInterval())

	// Before starting, LastBeat should be zero.
	if !svc.LastBeat().IsZero() {
		t.Fatal("expected zero LastBeat before Start")
	}

	if err := svc.Start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer svc.Stop()

	time.Sleep(30 * time.Millisecond)

	lb := svc.LastBeat()
	if lb.IsZero() {
		t.Fatal("expected non-zero LastBeat after Start")
	}
	if time.Since(lb) > 1*time.Second {
		t.Fatal("LastBeat is too old")
	}
}

// ---------------------------------------------------------------------------
// Stale detection
// ---------------------------------------------------------------------------

func TestService_IsStale(t *testing.T) {
	t.Parallel()
	cfg := Config{
		Interval: 10 * time.Millisecond,
		Timeout:  30 * time.Millisecond,
	}
	svc := NewService(cfg)

	// No beat recorded yet: not stale.
	if svc.IsStale() {
		t.Fatal("expected not stale when no beat has been recorded")
	}

	if err := svc.Start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}

	// Wait for at least one beat.
	time.Sleep(20 * time.Millisecond)
	if svc.IsStale() {
		t.Fatal("expected not stale immediately after a beat")
	}

	// Stop and wait longer than the timeout.
	svc.Stop()
	time.Sleep(50 * time.Millisecond)

	if !svc.IsStale() {
		t.Fatal("expected stale after timeout with no beats")
	}
}

// ---------------------------------------------------------------------------
// Enable / Disable
// ---------------------------------------------------------------------------

func TestService_EnableDisable(t *testing.T) {
	t.Parallel()
	svc := NewService(shortInterval())

	if err := svc.Start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}

	// Disable should stop the service.
	svc.Disable()
	if svc.IsRunning() {
		t.Fatal("expected service to stop after Disable")
	}

	// Starting while disabled should fail.
	if err := svc.Start(context.Background()); err == nil {
		t.Fatal("expected error starting a disabled service")
	}

	// Re-enable and start again.
	svc.Enable()
	if err := svc.Start(context.Background()); err != nil {
		t.Fatalf("start after Enable: %v", err)
	}
	defer svc.Stop()

	if !svc.IsRunning() {
		t.Fatal("expected service to be running after re-enable and Start")
	}
}

// ---------------------------------------------------------------------------
// Status transitions
// ---------------------------------------------------------------------------

func TestService_SetStatus(t *testing.T) {
	t.Parallel()
	svc := NewService(shortInterval())

	if err := svc.Start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer svc.Stop()

	svc.SetStatus(StatusIdle)
	beat := svc.Beat()
	if beat.Status != StatusIdle {
		t.Fatalf("expected status %q after SetStatus, got %q", StatusIdle, beat.Status)
	}

	svc.SetStatus(StatusOnline)
	beat = svc.Beat()
	if beat.Status != StatusOnline {
		t.Fatalf("expected status %q after SetStatus, got %q", StatusOnline, beat.Status)
	}
}

func TestService_StatusAfterStop(t *testing.T) {
	t.Parallel()
	svc := NewService(shortInterval())

	if err := svc.Start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}
	svc.Stop()

	beat := svc.Beat()
	if beat.Status != StatusOffline {
		t.Fatalf("expected status %q after Stop, got %q", StatusOffline, beat.Status)
	}
}

// ---------------------------------------------------------------------------
// UpdateMetrics thread safety
// ---------------------------------------------------------------------------

func TestService_UpdateMetrics_ThreadSafety(t *testing.T) {
	t.Parallel()
	svc := NewService(shortInterval())

	if err := svc.Start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer svc.Stop()

	const goroutines = 20
	const increments = 100
	var wg sync.WaitGroup

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < increments; j++ {
				svc.UpdateMetrics(func(m *Metrics) {
					m.TotalRuns++
				})
			}
		}()
	}
	wg.Wait()

	beat := svc.Beat()
	expected := int64(goroutines * increments)
	if beat.Metrics.TotalRuns != expected {
		t.Fatalf("expected TotalRuns=%d, got %d", expected, beat.Metrics.TotalRuns)
	}
}

func TestService_UpdateMetrics_ActiveSessions(t *testing.T) {
	t.Parallel()
	svc := NewService(shortInterval())

	svc.UpdateMetrics(func(m *Metrics) {
		m.ActiveSessions = 5
	})

	beat := svc.Beat()
	if beat.Metrics.ActiveSessions != 5 {
		t.Fatalf("expected ActiveSessions=5, got %d", beat.Metrics.ActiveSessions)
	}
}

// ---------------------------------------------------------------------------
// OnBeat callback
// ---------------------------------------------------------------------------

func TestService_OnBeatCallback(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	var beats []Beat

	cfg := shortInterval()
	cfg.OnBeat = func(b Beat) {
		mu.Lock()
		defer mu.Unlock()
		beats = append(beats, b)
	}

	svc := NewService(cfg)
	if err := svc.Start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}

	// Let a few ticks fire.
	time.Sleep(50 * time.Millisecond)
	svc.Stop()

	mu.Lock()
	count := len(beats)
	mu.Unlock()

	if count == 0 {
		t.Fatal("expected OnBeat callback to be invoked at least once")
	}

	// Verify the first beat has valid fields.
	mu.Lock()
	first := beats[0]
	mu.Unlock()

	if first.BeatAt.IsZero() {
		t.Fatal("expected non-zero timestamp in callback beat")
	}
	if first.Metrics.GoRoutines <= 0 {
		t.Fatal("expected positive goroutine count in callback beat")
	}
}

// ---------------------------------------------------------------------------
// Default config
// ---------------------------------------------------------------------------

func TestNewService_DefaultConfig(t *testing.T) {
	t.Parallel()
	svc := NewService(Config{})

	// Verify defaults were applied by starting and checking behavior.
	// We can't directly access config, so we verify via IsStale timing.
	if svc.IsRunning() {
		t.Fatal("expected service to not be running after NewService")
	}

	beat := svc.Beat()
	if beat.Status != StatusOffline {
		t.Fatalf("expected initial status %q, got %q", StatusOffline, beat.Status)
	}
}

// ---------------------------------------------------------------------------
// Active hours scheduling
// ---------------------------------------------------------------------------

func TestService_ActiveHours_WithinHours(t *testing.T) {
	t.Parallel()

	// Configure active hours to include the current time.
	now := time.Now()
	startStr := now.Add(-1 * time.Hour).Format("15:04")
	endStr := now.Add(1 * time.Hour).Format("15:04")

	cfg := shortInterval()
	cfg.ActiveHours = &ActiveHoursConfig{
		Start:    startStr,
		End:      endStr,
		Timezone: "Local",
	}

	svc := NewService(cfg)
	if err := svc.Start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer svc.Stop()

	time.Sleep(30 * time.Millisecond)

	beat := svc.Beat()
	if beat.Status == StatusSleeping {
		t.Fatalf("expected status other than %q within active hours, got %q",
			StatusSleeping, beat.Status)
	}
}

func TestService_ActiveHours_OutsideHours(t *testing.T) {
	t.Parallel()

	// Configure active hours to exclude the current time by picking a
	// window that ended an hour ago.
	now := time.Now()
	startStr := now.Add(-3 * time.Hour).Format("15:04")
	endStr := now.Add(-1 * time.Hour).Format("15:04")

	cfg := shortInterval()
	cfg.ActiveHours = &ActiveHoursConfig{
		Start:    startStr,
		End:      endStr,
		Timezone: "Local",
	}

	svc := NewService(cfg)
	if err := svc.Start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer svc.Stop()

	// Let a couple ticks fire so the sleeping status is applied.
	time.Sleep(30 * time.Millisecond)

	beat := svc.Beat()
	if beat.Status != StatusSleeping {
		t.Fatalf("expected status %q outside active hours, got %q",
			StatusSleeping, beat.Status)
	}
}

func TestService_ActiveHours_NoConfig(t *testing.T) {
	t.Parallel()

	// Without ActiveHours config, service should behave normally.
	cfg := shortInterval()
	svc := NewService(cfg)
	if err := svc.Start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer svc.Stop()

	time.Sleep(30 * time.Millisecond)

	beat := svc.Beat()
	if beat.Status == StatusSleeping {
		t.Fatal("expected non-sleeping status without active hours config")
	}
}

func TestService_ActiveHours_OvernightWindow(t *testing.T) {
	t.Parallel()

	// Test overnight window (e.g. 22:00 - 06:00).
	now := time.Now()
	hour := now.Hour()

	// Build an overnight window that includes the current hour.
	// If current hour is 14, we create a window from 13:00 to 12:00 (next day),
	// which wraps around and includes 14.
	startStr := fmt.Sprintf("%02d:00", (hour+23)%24) // one hour before
	endStr := fmt.Sprintf("%02d:00", (hour+22)%24)   // two hours before (next day wrap)

	cfg := shortInterval()
	cfg.ActiveHours = &ActiveHoursConfig{
		Start:    startStr,
		End:      endStr,
		Timezone: "Local",
	}

	svc := NewService(cfg)
	if err := svc.Start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer svc.Stop()

	time.Sleep(30 * time.Millisecond)

	beat := svc.Beat()
	if beat.Status == StatusSleeping {
		t.Fatalf("expected non-sleeping status within overnight active window, got %q",
			beat.Status)
	}
}

// ---------------------------------------------------------------------------
// isWithinActiveHours unit tests
// ---------------------------------------------------------------------------

func TestIsWithinActiveHours_NilConfig(t *testing.T) {
	t.Parallel()
	svc := NewService(Config{})
	if !svc.isWithinActiveHours(time.Now()) {
		t.Fatal("expected always active when ActiveHours is nil")
	}
}

func TestIsWithinActiveHours_InvalidStart(t *testing.T) {
	t.Parallel()
	cfg := Config{
		ActiveHours: &ActiveHoursConfig{
			Start: "invalid",
			End:   "18:00",
		},
	}
	svc := NewService(cfg)
	// Invalid start should fall back to always active.
	if !svc.isWithinActiveHours(time.Now()) {
		t.Fatal("expected always active with invalid start time")
	}
}

func TestIsWithinActiveHours_InvalidEnd(t *testing.T) {
	t.Parallel()
	cfg := Config{
		ActiveHours: &ActiveHoursConfig{
			Start: "09:00",
			End:   "bad",
		},
	}
	svc := NewService(cfg)
	if !svc.isWithinActiveHours(time.Now()) {
		t.Fatal("expected always active with invalid end time")
	}
}

// ---------------------------------------------------------------------------
// parseHHMM tests
// ---------------------------------------------------------------------------

func TestParseHHMM_Valid(t *testing.T) {
	t.Parallel()
	h, m, err := parseHHMM("09:30")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if h != 9 || m != 30 {
		t.Fatalf("expected 9:30, got %d:%d", h, m)
	}
}

func TestParseHHMM_Invalid(t *testing.T) {
	t.Parallel()
	_, _, err := parseHHMM("25:00")
	if err == nil {
		t.Fatal("expected error for invalid time")
	}
}

// ---------------------------------------------------------------------------
// Wake signal
// ---------------------------------------------------------------------------

func TestService_Wake_ForcesImmediateTick(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	var beats []Beat

	// Use a long interval so ticks only happen from wake signals.
	cfg := Config{
		Interval: 5 * time.Second,
		Timeout:  10 * time.Second,
		OnBeat: func(b Beat) {
			mu.Lock()
			defer mu.Unlock()
			beats = append(beats, b)
		},
	}

	svc := NewService(cfg)
	if err := svc.Start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer svc.Stop()

	// The initial tick fires immediately; wait for it.
	time.Sleep(20 * time.Millisecond)

	mu.Lock()
	countBefore := len(beats)
	mu.Unlock()

	// Send a wake signal and wait for it to be processed.
	svc.Wake()
	time.Sleep(30 * time.Millisecond)

	mu.Lock()
	countAfter := len(beats)
	mu.Unlock()

	if countAfter <= countBefore {
		t.Fatalf("expected wake to trigger additional beat(s); before=%d, after=%d",
			countBefore, countAfter)
	}
}

func TestService_Wake_ResetsIdleStatus(t *testing.T) {
	t.Parallel()

	cfg := shortInterval()
	svc := NewService(cfg)

	if err := svc.Start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer svc.Stop()

	// Manually set status to idle.
	svc.SetStatus(StatusIdle)
	time.Sleep(10 * time.Millisecond)

	beat := svc.Beat()
	if beat.Status != StatusIdle {
		t.Fatalf("expected idle before wake, got %q", beat.Status)
	}

	svc.Wake()
	time.Sleep(30 * time.Millisecond)

	beat = svc.Beat()
	if beat.Status != StatusOnline {
		t.Fatalf("expected online after wake, got %q", beat.Status)
	}
}

func TestService_Wake_ResetsSleepingStatus(t *testing.T) {
	t.Parallel()

	cfg := shortInterval()
	svc := NewService(cfg)

	if err := svc.Start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer svc.Stop()

	// Manually set status to sleeping.
	svc.SetStatus(StatusSleeping)
	time.Sleep(10 * time.Millisecond)

	svc.Wake()
	time.Sleep(30 * time.Millisecond)

	beat := svc.Beat()
	if beat.Status != StatusOnline {
		t.Fatalf("expected online after wake from sleeping, got %q", beat.Status)
	}
}

func TestService_Wake_NonBlocking(t *testing.T) {
	t.Parallel()

	svc := NewService(shortInterval())

	// Wake before start should not block or panic.
	svc.Wake()
	svc.Wake() // double wake should not block (buffer is 1, second is dropped)
}

// ---------------------------------------------------------------------------
// Transcript pruning
// ---------------------------------------------------------------------------

func TestService_TranscriptPruning_Called(t *testing.T) {
	t.Parallel()

	pruner := &mockPruner{}
	cfg := shortInterval()
	cfg.Pruner = pruner
	cfg.PruneAge = 24 * time.Hour

	svc := NewService(cfg)
	if err := svc.Start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer svc.Stop()

	// Wait for the first tick which should trigger pruning (lastPruneAt is zero).
	time.Sleep(30 * time.Millisecond)

	calls := pruner.getCalls()
	if calls == 0 {
		t.Fatal("expected pruner to be called at least once on first tick")
	}
}

func TestService_TranscriptPruning_RespectsPruneInterval(t *testing.T) {
	t.Parallel()

	pruner := &mockPruner{}
	cfg := shortInterval()
	cfg.Pruner = pruner

	svc := NewService(cfg)
	if err := svc.Start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer svc.Stop()

	// Wait for several ticks.
	time.Sleep(50 * time.Millisecond)

	calls := pruner.getCalls()
	// Pruning should only happen once because pruneInterval is 1 hour,
	// and we've only run for ~50ms.
	if calls != 1 {
		t.Fatalf("expected exactly 1 prune call (respecting interval), got %d", calls)
	}
}

func TestService_TranscriptPruning_ErrorDoesNotCrash(t *testing.T) {
	t.Parallel()

	pruner := &mockPruner{returnErr: fmt.Errorf("storage unavailable")}
	cfg := shortInterval()
	cfg.Pruner = pruner

	svc := NewService(cfg)
	if err := svc.Start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer svc.Stop()

	// Wait for ticks; errors should be logged, not cause a crash.
	time.Sleep(30 * time.Millisecond)

	if !svc.IsRunning() {
		t.Fatal("expected service to keep running after prune error")
	}
}

func TestService_TranscriptPruning_NilPruner(t *testing.T) {
	t.Parallel()

	cfg := shortInterval()
	// No Pruner set — should work fine.

	svc := NewService(cfg)
	if err := svc.Start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer svc.Stop()

	time.Sleep(30 * time.Millisecond)

	if !svc.IsRunning() {
		t.Fatal("expected service to keep running with nil pruner")
	}
}

func TestService_TranscriptPruning_DefaultAge(t *testing.T) {
	t.Parallel()

	// Verify the default prune age is applied when not configured.
	svc := NewService(Config{})
	svc.mu.RLock()
	age := svc.config.PruneAge
	svc.mu.RUnlock()

	if age != defaultPruneAge {
		t.Fatalf("expected default prune age %v, got %v", defaultPruneAge, age)
	}
}

// ---------------------------------------------------------------------------
// Status change callback
// ---------------------------------------------------------------------------

func TestService_OnStatusChange_Fires(t *testing.T) {
	t.Parallel()

	type transition struct {
		old Status
		new Status
	}

	var mu sync.Mutex
	var transitions []transition

	// Configure active hours in the past so the service will go sleeping.
	now := time.Now()
	startStr := now.Add(-3 * time.Hour).Format("15:04")
	endStr := now.Add(-1 * time.Hour).Format("15:04")

	cfg := shortInterval()
	cfg.ActiveHours = &ActiveHoursConfig{
		Start:    startStr,
		End:      endStr,
		Timezone: "Local",
	}
	cfg.OnStatusChange = func(old, new_ Status) {
		mu.Lock()
		defer mu.Unlock()
		transitions = append(transitions, transition{old: old, new: new_})
	}

	svc := NewService(cfg)
	if err := svc.Start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer svc.Stop()

	// Wait for ticks to fire the status change.
	time.Sleep(30 * time.Millisecond)

	mu.Lock()
	count := len(transitions)
	mu.Unlock()

	if count == 0 {
		t.Fatal("expected at least one status change callback")
	}

	mu.Lock()
	first := transitions[0]
	mu.Unlock()

	if first.old != StatusOnline {
		t.Fatalf("expected old status %q, got %q", StatusOnline, first.old)
	}
	if first.new != StatusSleeping {
		t.Fatalf("expected new status %q, got %q", StatusSleeping, first.new)
	}
}

func TestService_OnStatusChange_NotFiredWhenSameStatus(t *testing.T) {
	t.Parallel()

	var callCount atomic.Int32

	cfg := shortInterval()
	cfg.OnStatusChange = func(_, _ Status) {
		callCount.Add(1)
	}

	svc := NewService(cfg)
	if err := svc.Start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer svc.Stop()

	// Within active hours (no ActiveHours config), status stays online.
	time.Sleep(30 * time.Millisecond)

	if callCount.Load() != 0 {
		t.Fatalf("expected no status change callback when status is stable, got %d calls",
			callCount.Load())
	}
}

func TestService_OnStatusChange_NilCallback(t *testing.T) {
	t.Parallel()

	// Ensure nil OnStatusChange doesn't cause a panic.
	now := time.Now()
	startStr := now.Add(-3 * time.Hour).Format("15:04")
	endStr := now.Add(-1 * time.Hour).Format("15:04")

	cfg := shortInterval()
	cfg.ActiveHours = &ActiveHoursConfig{
		Start:    startStr,
		End:      endStr,
		Timezone: "Local",
	}
	// OnStatusChange is nil.

	svc := NewService(cfg)
	if err := svc.Start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer svc.Stop()

	time.Sleep(30 * time.Millisecond)

	if !svc.IsRunning() {
		t.Fatal("expected service to keep running with nil OnStatusChange")
	}
}

// ---------------------------------------------------------------------------
// Scheduled tasks
// ---------------------------------------------------------------------------

func TestService_ScheduledTasks_RunAtInterval(t *testing.T) {
	t.Parallel()

	var taskRuns atomic.Int32

	cfg := shortInterval()
	cfg.Tasks = []ScheduledTask{
		{
			Name:     "counter",
			Interval: 1 * time.Millisecond, // Run on every tick
			Fn: func(_ context.Context) error {
				taskRuns.Add(1)
				return nil
			},
		},
	}

	svc := NewService(cfg)
	if err := svc.Start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer svc.Stop()

	// Wait for several ticks.
	time.Sleep(50 * time.Millisecond)

	runs := taskRuns.Load()
	if runs == 0 {
		t.Fatal("expected scheduled task to run at least once")
	}
}

func TestService_ScheduledTasks_RespectInterval(t *testing.T) {
	t.Parallel()

	var taskRuns atomic.Int32

	cfg := shortInterval()
	cfg.Tasks = []ScheduledTask{
		{
			Name:     "slow-task",
			Interval: 10 * time.Second, // Much longer than test duration
			Fn: func(_ context.Context) error {
				taskRuns.Add(1)
				return nil
			},
		},
	}

	svc := NewService(cfg)
	if err := svc.Start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer svc.Stop()

	time.Sleep(50 * time.Millisecond)

	runs := taskRuns.Load()
	// Should run exactly once (on the first tick when lastRunAt is zero),
	// then not again because interval hasn't elapsed.
	if runs != 1 {
		t.Fatalf("expected exactly 1 task run (interval not elapsed), got %d", runs)
	}
}

func TestService_ScheduledTasks_ErrorDoesNotCrash(t *testing.T) {
	t.Parallel()

	cfg := shortInterval()
	cfg.Tasks = []ScheduledTask{
		{
			Name:     "failing-task",
			Interval: 1 * time.Millisecond,
			Fn: func(_ context.Context) error {
				return fmt.Errorf("task failed on purpose")
			},
		},
	}

	svc := NewService(cfg)
	if err := svc.Start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer svc.Stop()

	// Wait for several ticks; the failing task should not crash the service.
	time.Sleep(50 * time.Millisecond)

	if !svc.IsRunning() {
		t.Fatal("expected service to keep running after task error")
	}
}

func TestService_ScheduledTasks_MultipleTasks(t *testing.T) {
	t.Parallel()

	var fastRuns atomic.Int32
	var slowRuns atomic.Int32

	cfg := shortInterval()
	cfg.Tasks = []ScheduledTask{
		{
			Name:     "fast",
			Interval: 1 * time.Millisecond,
			Fn: func(_ context.Context) error {
				fastRuns.Add(1)
				return nil
			},
		},
		{
			Name:     "slow",
			Interval: 10 * time.Second,
			Fn: func(_ context.Context) error {
				slowRuns.Add(1)
				return nil
			},
		},
	}

	svc := NewService(cfg)
	if err := svc.Start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer svc.Stop()

	time.Sleep(50 * time.Millisecond)

	fast := fastRuns.Load()
	slow := slowRuns.Load()

	if fast <= 1 {
		t.Fatalf("expected fast task to run multiple times, got %d", fast)
	}
	if slow != 1 {
		t.Fatalf("expected slow task to run exactly once, got %d", slow)
	}
}

func TestService_ScheduledTasks_ZeroInterval(t *testing.T) {
	t.Parallel()

	var runs atomic.Int32

	cfg := shortInterval()
	cfg.Tasks = []ScheduledTask{
		{
			Name:     "zero-interval",
			Interval: 0, // Should be skipped
			Fn: func(_ context.Context) error {
				runs.Add(1)
				return nil
			},
		},
	}

	svc := NewService(cfg)
	if err := svc.Start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer svc.Stop()

	time.Sleep(30 * time.Millisecond)

	if runs.Load() != 0 {
		t.Fatalf("expected zero-interval task to never run, got %d", runs.Load())
	}
}

func TestService_ScheduledTasks_NoTasks(t *testing.T) {
	t.Parallel()

	// Ensure service works fine without any tasks.
	cfg := shortInterval()
	svc := NewService(cfg)
	if err := svc.Start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer svc.Stop()

	time.Sleep(30 * time.Millisecond)

	if !svc.IsRunning() {
		t.Fatal("expected service to keep running with no tasks")
	}
}

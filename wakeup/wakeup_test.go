package wakeup

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

type submitRecord struct {
	Channel    string
	SessionKey string
	Message    string
}

type mockSubmitter struct {
	mu      sync.Mutex
	records []submitRecord
	err     error
}

func (m *mockSubmitter) submit(_ context.Context, channel, sessionKey, message string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.records = append(m.records, submitRecord{
		Channel:    channel,
		SessionKey: sessionKey,
		Message:    message,
	})
	return m.err
}

func (m *mockSubmitter) calls() []submitRecord {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]submitRecord, len(m.records))
	copy(out, m.records)
	return out
}

func baseTrigger(id string) Trigger {
	return Trigger{
		ID:         id,
		Name:       "test-" + id,
		Schedule:   "every 1h",
		Channel:    "slack",
		SessionKey: "session-1",
		Message:    "hello from " + id,
		Enabled:    true,
		CreatedAt:  time.Now().UTC(),
	}
}

func newTestService(submit any) *Service {
	switch typed := submit.(type) {
	case SubmitFunc:
		return NewService(NewStore(""), typed)
	case func(context.Context, Trigger) (*ExecutionResult, error):
		return NewService(NewStore(""), typed)
	case func(context.Context, string, string, string) error:
		return NewService(NewStore(""), func(ctx context.Context, trigger Trigger) (*ExecutionResult, error) {
			return nil, typed(ctx, trigger.Channel, trigger.SessionKey, trigger.Message)
		})
	default:
		panic("unsupported wakeup test submitter")
	}
}

// ---------------------------------------------------------------------------
// CRUD tests
// ---------------------------------------------------------------------------

func TestAdd_And_Get(t *testing.T) {
	t.Parallel()
	mock := &mockSubmitter{}
	svc := newTestService(mock.submit)

	trigger := baseTrigger("t-1")
	if err := svc.Add(trigger); err != nil {
		t.Fatalf("add: %v", err)
	}

	got, err := svc.Get("t-1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Name != "test-t-1" {
		t.Fatalf("expected name %q, got %q", "test-t-1", got.Name)
	}
	if got.NextRunAt.IsZero() {
		t.Fatal("expected NextRunAt to be computed on Add")
	}
}

func TestAdd_DuplicateID(t *testing.T) {
	t.Parallel()
	svc := newTestService(func(_ context.Context, _, _, _ string) error { return nil })

	trigger := baseTrigger("t-1")
	if err := svc.Add(trigger); err != nil {
		t.Fatalf("add: %v", err)
	}
	err := svc.Add(trigger)
	if err == nil {
		t.Fatal("expected error for duplicate ID")
	}
	if !errors.Is(err, ErrDuplicateID) {
		t.Fatalf("expected ErrDuplicateID, got %v", err)
	}
}

func TestRemove(t *testing.T) {
	t.Parallel()
	svc := newTestService(func(_ context.Context, _, _, _ string) error { return nil })

	if err := svc.Add(baseTrigger("t-1")); err != nil {
		t.Fatalf("add: %v", err)
	}
	if err := svc.Remove("t-1"); err != nil {
		t.Fatalf("remove: %v", err)
	}
	_, err := svc.Get("t-1")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound after remove, got %v", err)
	}
}

func TestRemove_NotFound(t *testing.T) {
	t.Parallel()
	svc := newTestService(func(_ context.Context, _, _, _ string) error { return nil })

	err := svc.Remove("nonexistent")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestUpdate(t *testing.T) {
	t.Parallel()
	svc := newTestService(func(_ context.Context, _, _, _ string) error { return nil })

	if err := svc.Add(baseTrigger("t-1")); err != nil {
		t.Fatalf("add: %v", err)
	}
	if err := svc.Update("t-1", func(t *Trigger) {
		t.Message = "updated message"
	}); err != nil {
		t.Fatalf("update: %v", err)
	}

	got, _ := svc.Get("t-1")
	if got.Message != "updated message" {
		t.Fatalf("expected message %q, got %q", "updated message", got.Message)
	}
}

func TestUpdate_NotFound(t *testing.T) {
	t.Parallel()
	svc := newTestService(func(_ context.Context, _, _, _ string) error { return nil })

	err := svc.Update("nonexistent", func(_ *Trigger) {})
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestList(t *testing.T) {
	t.Parallel()
	svc := newTestService(func(_ context.Context, _, _, _ string) error { return nil })

	if err := svc.Add(baseTrigger("t-b")); err != nil {
		t.Fatalf("add: %v", err)
	}
	if err := svc.Add(baseTrigger("t-a")); err != nil {
		t.Fatalf("add: %v", err)
	}

	list := svc.List()
	if len(list) != 2 {
		t.Fatalf("expected 2 triggers, got %d", len(list))
	}
	// Should be sorted by ID.
	if list[0].ID != "t-a" || list[1].ID != "t-b" {
		t.Fatalf("expected sorted order [t-a, t-b], got [%s, %s]", list[0].ID, list[1].ID)
	}
}

func TestGet_ReturnsCopy(t *testing.T) {
	t.Parallel()
	svc := newTestService(func(_ context.Context, _, _, _ string) error { return nil })

	if err := svc.Add(baseTrigger("t-1")); err != nil {
		t.Fatalf("add: %v", err)
	}
	got, _ := svc.Get("t-1")
	got.Name = "mutated"

	got2, _ := svc.Get("t-1")
	if got2.Name != "test-t-1" {
		t.Fatal("Get returned a reference instead of a copy")
	}
}

// ---------------------------------------------------------------------------
// Enable / Disable tests
// ---------------------------------------------------------------------------

func TestEnableDisable(t *testing.T) {
	t.Parallel()
	svc := newTestService(func(_ context.Context, _, _, _ string) error { return nil })

	trigger := baseTrigger("t-1")
	trigger.Enabled = true
	if err := svc.Add(trigger); err != nil {
		t.Fatalf("add: %v", err)
	}

	// Disable.
	if err := svc.Disable("t-1"); err != nil {
		t.Fatalf("disable: %v", err)
	}
	got, _ := svc.Get("t-1")
	if got.Enabled {
		t.Fatal("expected trigger to be disabled")
	}
	if !got.NextRunAt.IsZero() {
		t.Fatal("expected NextRunAt to be cleared on disable")
	}

	// Enable.
	if err := svc.Enable("t-1"); err != nil {
		t.Fatalf("enable: %v", err)
	}
	got, _ = svc.Get("t-1")
	if !got.Enabled {
		t.Fatal("expected trigger to be enabled")
	}
	if got.NextRunAt.IsZero() {
		t.Fatal("expected NextRunAt to be recomputed on enable")
	}
}

func TestEnable_NotFound(t *testing.T) {
	t.Parallel()
	svc := newTestService(func(_ context.Context, _, _, _ string) error { return nil })

	err := svc.Enable("nonexistent")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestDisable_NotFound(t *testing.T) {
	t.Parallel()
	svc := newTestService(func(_ context.Context, _, _, _ string) error { return nil })

	err := svc.Disable("nonexistent")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// Trigger firing tests
// ---------------------------------------------------------------------------

func TestFireDueTriggers(t *testing.T) {
	t.Parallel()
	mock := &mockSubmitter{}
	svc := newTestService(mock.submit)

	trigger := baseTrigger("t-fire")
	trigger.Enabled = true
	if err := svc.Add(trigger); err != nil {
		t.Fatalf("add: %v", err)
	}

	// Set NextRunAt to the past so it fires.
	if err := svc.Update("t-fire", func(tr *Trigger) {
		tr.NextRunAt = time.Now().UTC().Add(-1 * time.Minute)
	}); err != nil {
		t.Fatalf("update: %v", err)
	}

	svc.fireDueTriggers(context.Background())

	calls := mock.calls()
	if len(calls) != 1 {
		t.Fatalf("expected 1 submit call, got %d", len(calls))
	}
	if calls[0].Channel != "slack" {
		t.Fatalf("expected channel %q, got %q", "slack", calls[0].Channel)
	}
	if calls[0].SessionKey != "session-1" {
		t.Fatalf("expected session key %q, got %q", "session-1", calls[0].SessionKey)
	}
	if calls[0].Message != "hello from t-fire" {
		t.Fatalf("expected message %q, got %q", "hello from t-fire", calls[0].Message)
	}

	// Verify LastRunAt was updated.
	got, _ := svc.Get("t-fire")
	if got.LastRunAt.IsZero() {
		t.Fatal("expected LastRunAt to be set after firing")
	}
	if got.NextRunAt.IsZero() {
		t.Fatal("expected NextRunAt to be recomputed after firing")
	}
}

func TestFireDueTriggers_SkipsDisabled(t *testing.T) {
	t.Parallel()
	mock := &mockSubmitter{}
	svc := newTestService(mock.submit)

	trigger := baseTrigger("t-disabled")
	trigger.Enabled = false
	if err := svc.Add(trigger); err != nil {
		t.Fatalf("add: %v", err)
	}

	// Force NextRunAt to the past.
	if err := svc.Update("t-disabled", func(tr *Trigger) {
		tr.NextRunAt = time.Now().UTC().Add(-1 * time.Minute)
	}); err != nil {
		t.Fatalf("update: %v", err)
	}

	svc.fireDueTriggers(context.Background())

	if len(mock.calls()) != 0 {
		t.Fatal("disabled trigger should not have been fired")
	}
}

func TestFireDueTriggers_SkipsFuture(t *testing.T) {
	t.Parallel()
	mock := &mockSubmitter{}
	svc := newTestService(mock.submit)

	trigger := baseTrigger("t-future")
	trigger.Enabled = true
	if err := svc.Add(trigger); err != nil {
		t.Fatalf("add: %v", err)
	}

	// NextRunAt is already in the future from Add.
	svc.fireDueTriggers(context.Background())

	if len(mock.calls()) != 0 {
		t.Fatal("future trigger should not have been fired")
	}
}

func TestFireDueTriggers_PersistsExecutionResult(t *testing.T) {
	t.Parallel()

	svc := newTestService(SubmitFunc(func(_ context.Context, trigger Trigger) (*ExecutionResult, error) {
		if trigger.ID != "t-execution" {
			t.Fatalf("trigger.ID = %q", trigger.ID)
		}
		return &ExecutionResult{
			RunID:               "run-wakeup-1",
			Summary:             "morning brief sent",
			VerificationStatus:  "warning",
			VerificationSummary: "calendar data was partially unavailable",
		}, nil
	}))

	trigger := baseTrigger("t-execution")
	if err := svc.Add(trigger); err != nil {
		t.Fatalf("add: %v", err)
	}
	if err := svc.Update("t-execution", func(tr *Trigger) {
		tr.NextRunAt = time.Now().UTC().Add(-1 * time.Minute)
	}); err != nil {
		t.Fatalf("update: %v", err)
	}

	svc.fireDueTriggers(context.Background())

	got, err := svc.Get("t-execution")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.LastStatus != "triggered" {
		t.Fatalf("LastStatus = %q, want %q", got.LastStatus, "triggered")
	}
	if got.LastRunID != "run-wakeup-1" {
		t.Fatalf("LastRunID = %q", got.LastRunID)
	}
	if got.LastSummary != "morning brief sent" {
		t.Fatalf("LastSummary = %q", got.LastSummary)
	}
	if got.LastVerificationStatus != "warning" {
		t.Fatalf("LastVerificationStatus = %q", got.LastVerificationStatus)
	}
	if got.LastVerificationSummary != "calendar data was partially unavailable" {
		t.Fatalf("LastVerificationSummary = %q", got.LastVerificationSummary)
	}
}

// ---------------------------------------------------------------------------
// Timezone handling tests
// ---------------------------------------------------------------------------

func TestAdd_WithTimezone(t *testing.T) {
	t.Parallel()
	svc := newTestService(func(_ context.Context, _, _, _ string) error { return nil })

	trigger := baseTrigger("t-tz")
	trigger.Schedule = "0 9 * * *"
	trigger.Timezone = "America/New_York"
	if err := svc.Add(trigger); err != nil {
		t.Fatalf("add: %v", err)
	}

	got, _ := svc.Get("t-tz")
	if got.NextRunAt.IsZero() {
		t.Fatal("expected NextRunAt to be computed with timezone")
	}

	// Verify the next run is at 09:00 in New York.
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatalf("load location: %v", err)
	}
	inNY := got.NextRunAt.In(loc)
	if inNY.Hour() != 9 {
		t.Fatalf("expected 09:00 in New York, got %v", inNY)
	}
}

func TestAdd_InvalidTimezone(t *testing.T) {
	t.Parallel()
	svc := newTestService(func(_ context.Context, _, _, _ string) error { return nil })

	trigger := baseTrigger("t-bad-tz")
	trigger.Schedule = "0 9 * * *"
	trigger.Timezone = "Invalid/Timezone"
	err := svc.Add(trigger)
	if err == nil {
		t.Fatal("expected error for invalid timezone")
	}
}

// ---------------------------------------------------------------------------
// NextRunAt computation tests
// ---------------------------------------------------------------------------

func TestNextRunAt_CronSchedule(t *testing.T) {
	t.Parallel()
	svc := newTestService(func(_ context.Context, _, _, _ string) error { return nil })

	trigger := baseTrigger("t-cron")
	trigger.Schedule = "0 9 * * *"
	trigger.Timezone = "UTC"
	if err := svc.Add(trigger); err != nil {
		t.Fatalf("add: %v", err)
	}

	got, _ := svc.Get("t-cron")
	if got.NextRunAt.IsZero() {
		t.Fatal("expected NextRunAt to be computed")
	}
	// NextRunAt should be at 09:00 UTC on some future day.
	if got.NextRunAt.Minute() != 0 || got.NextRunAt.Hour() != 9 {
		t.Fatalf("expected 09:00 UTC, got %v", got.NextRunAt)
	}
}

func TestNextRunAt_EverySchedule(t *testing.T) {
	t.Parallel()
	svc := newTestService(func(_ context.Context, _, _, _ string) error { return nil })

	trigger := baseTrigger("t-every")
	trigger.Schedule = "every 30m"
	before := time.Now().UTC()
	if err := svc.Add(trigger); err != nil {
		t.Fatalf("add: %v", err)
	}

	got, _ := svc.Get("t-every")
	if got.NextRunAt.IsZero() {
		t.Fatal("expected NextRunAt to be computed for every schedule")
	}
	if got.NextRunAt.Before(before) {
		t.Fatalf("expected NextRunAt to be in the future, got %v", got.NextRunAt)
	}
}

func TestAdd_InvalidSchedule(t *testing.T) {
	t.Parallel()
	svc := newTestService(func(_ context.Context, _, _, _ string) error { return nil })

	trigger := baseTrigger("t-bad")
	trigger.Schedule = "not-a-valid-schedule"
	err := svc.Add(trigger)
	if err == nil {
		t.Fatal("expected error for invalid schedule")
	}
}

// ---------------------------------------------------------------------------
// Service lifecycle tests
// ---------------------------------------------------------------------------

func TestStartStop(t *testing.T) {
	t.Parallel()
	svc := newTestService(func(_ context.Context, _, _, _ string) error { return nil })

	ctx := context.Background()
	if err := svc.Start(ctx); err != nil {
		t.Fatalf("start: %v", err)
	}
	if !svc.IsRunning() {
		t.Fatal("expected service to be running")
	}

	// Double start should fail.
	if err := svc.Start(ctx); !errors.Is(err, ErrAlreadyRunning) {
		t.Fatalf("expected ErrAlreadyRunning, got %v", err)
	}

	svc.Stop()
	if svc.IsRunning() {
		t.Fatal("expected service to be stopped")
	}

	// Double stop is a no-op.
	svc.Stop()
}

// ---------------------------------------------------------------------------
// Concurrency test
// ---------------------------------------------------------------------------

func TestService_ConcurrentAccess(t *testing.T) {
	t.Parallel()
	svc := newTestService(func(_ context.Context, _, _, _ string) error { return nil })

	// Pre-populate.
	if err := svc.Add(baseTrigger("t-0")); err != nil {
		t.Fatalf("add: %v", err)
	}

	var wg sync.WaitGroup
	const goroutines = 20

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			_ = svc.Add(baseTrigger("t-concurrent-" + time.Now().Format("150405.000000000")))
			_, _ = svc.Get("t-0")
			_ = svc.List()
			_ = svc.Update("t-0", func(tr *Trigger) {
				tr.Message = "updated"
			})
		}(i)
	}
	wg.Wait()
}

// ---------------------------------------------------------------------------
// Parse schedule tests
// ---------------------------------------------------------------------------

func TestParseSchedule_Every(t *testing.T) {
	t.Parallel()
	sched, err := parseSchedule("every 30m", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sched.Kind != "every" {
		t.Fatalf("expected kind %q, got %q", "every", sched.Kind)
	}
	if sched.Every != "30m" {
		t.Fatalf("expected every %q, got %q", "30m", sched.Every)
	}
}

func TestParseSchedule_Cron(t *testing.T) {
	t.Parallel()
	sched, err := parseSchedule("0 9 * * *", "UTC")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sched.Kind != "cron" {
		t.Fatalf("expected kind %q, got %q", "cron", sched.Kind)
	}
	if sched.Expression != "0 9 * * *" {
		t.Fatalf("expected expression %q, got %q", "0 9 * * *", sched.Expression)
	}
	if sched.Timezone != "UTC" {
		t.Fatalf("expected timezone %q, got %q", "UTC", sched.Timezone)
	}
}

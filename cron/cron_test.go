package cron

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/fulcrus/hopclaw/automation"
	runtimesvc "github.com/fulcrus/hopclaw/runtime"
	verifyrt "github.com/fulcrus/hopclaw/runtime/verify"
)

// ---------------------------------------------------------------------------
// Schedule computation tests
// ---------------------------------------------------------------------------

func TestNextRunTime_At_Future(t *testing.T) {
	t.Parallel()
	future := time.Now().UTC().Add(1 * time.Hour)
	sched := Schedule{
		Kind: ScheduleKindAt,
		At:   future.Format(time.RFC3339),
	}
	next, err := NextRunTime(sched, time.Now().UTC())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if next.IsZero() {
		t.Fatal("expected non-zero next run time for future at schedule")
	}
	// Allow 1-second tolerance for formatting precision.
	if diff := next.Sub(future); diff < -time.Second || diff > time.Second {
		t.Fatalf("expected next run near %v, got %v", future, next)
	}
}

func TestNextRunTime_At_Past(t *testing.T) {
	t.Parallel()
	past := time.Now().UTC().Add(-1 * time.Hour)
	sched := Schedule{
		Kind: ScheduleKindAt,
		At:   past.Format(time.RFC3339),
	}
	next, err := NextRunTime(sched, time.Now().UTC())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !next.IsZero() {
		t.Fatalf("expected zero time for past at schedule, got %v", next)
	}
}

func TestNextRunTime_At_MissingField(t *testing.T) {
	t.Parallel()
	sched := Schedule{Kind: ScheduleKindAt}
	_, err := NextRunTime(sched, time.Now().UTC())
	if err == nil {
		t.Fatal("expected error for missing at field")
	}
	if !errors.Is(err, ErrInvalidSchedule) {
		t.Fatalf("expected ErrInvalidSchedule, got %v", err)
	}
}

func TestNextRunTime_Every(t *testing.T) {
	t.Parallel()
	now := time.Date(2024, 1, 1, 10, 0, 0, 0, time.UTC)
	sched := Schedule{
		Kind:  ScheduleKindEvery,
		Every: "1h",
	}
	next, err := NextRunTime(sched, now)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expected := now.Add(1 * time.Hour)
	if !next.Equal(expected) {
		t.Fatalf("expected %v, got %v", expected, next)
	}
}

func TestNextRunTime_Every_Anchored(t *testing.T) {
	t.Parallel()
	anchor := time.Date(2024, 1, 1, 9, 0, 0, 0, time.UTC)
	now := time.Date(2024, 1, 1, 10, 30, 0, 0, time.UTC)
	sched := Schedule{
		Kind:  ScheduleKindEvery,
		Every: "1h",
	}
	next, err := NextRunTimeAnchored(sched, now, anchor)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Anchor at 09:00, every 1h: 09, 10, 11. After 10:30 => 11:00.
	expected := time.Date(2024, 1, 1, 11, 0, 0, 0, time.UTC)
	if !next.Equal(expected) {
		t.Fatalf("expected %v, got %v", expected, next)
	}
}

func TestNextRunTime_Every_NegativeDuration(t *testing.T) {
	t.Parallel()
	sched := Schedule{Kind: ScheduleKindEvery, Every: "-1h"}
	_, err := NextRunTime(sched, time.Now().UTC())
	if err == nil {
		t.Fatal("expected error for negative duration")
	}
	if !errors.Is(err, ErrInvalidSchedule) {
		t.Fatalf("expected ErrInvalidSchedule, got %v", err)
	}
}

func TestNextRunTime_Cron(t *testing.T) {
	t.Parallel()
	// Explicitly set timezone to UTC so the test is deterministic regardless
	// of the host's local timezone.
	ref := time.Date(2024, 1, 1, 8, 30, 0, 0, time.UTC)
	sched := Schedule{
		Kind:       ScheduleKindCron,
		Expression: "0 9 * * *", // every day at 09:00
		Timezone:   "UTC",
	}
	next, err := NextRunTime(sched, ref)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expected := time.Date(2024, 1, 1, 9, 0, 0, 0, time.UTC)
	if !next.Equal(expected) {
		t.Fatalf("expected %v, got %v", expected, next)
	}
}

func TestNextRunTime_Cron_Timezone(t *testing.T) {
	t.Parallel()
	ref := time.Date(2024, 1, 1, 0, 30, 0, 0, time.UTC)
	sched := Schedule{
		Kind:       ScheduleKindCron,
		Expression: "0 9 * * *",
		Timezone:   "America/New_York",
	}
	next, err := NextRunTime(sched, ref)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if next.IsZero() {
		t.Fatal("expected non-zero next run time")
	}
	// 09:00 EST = 14:00 UTC (EST is UTC-5)
	loc, _ := time.LoadLocation("America/New_York")
	inNY := next.In(loc)
	if inNY.Hour() != 9 {
		t.Fatalf("expected 09:00 in New York, got %v", inNY)
	}
}

func TestNextRunTime_Cron_InvalidExpression(t *testing.T) {
	t.Parallel()
	sched := Schedule{Kind: ScheduleKindCron, Expression: "not a cron expr"}
	_, err := NextRunTime(sched, time.Now().UTC())
	if err == nil {
		t.Fatal("expected error for invalid cron expression")
	}
}

func TestNextRunTime_UnknownKind(t *testing.T) {
	t.Parallel()
	sched := Schedule{Kind: "unknown"}
	_, err := NextRunTime(sched, time.Now().UTC())
	if err == nil {
		t.Fatal("expected error for unknown kind")
	}
	if !errors.Is(err, ErrInvalidSchedule) {
		t.Fatalf("expected ErrInvalidSchedule, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// Store CRUD tests
// ---------------------------------------------------------------------------

func testStore(t *testing.T) (*Store, string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "cron-jobs.json")
	store, err := Load(path)
	if err != nil {
		t.Fatalf("load store: %v", err)
	}
	return store, path
}

func TestStore_EmptyOnCreation(t *testing.T) {
	t.Parallel()
	store, _ := testStore(t)
	if len(store.List()) != 0 {
		t.Fatal("expected empty store")
	}
}

func TestStore_AddAndGet(t *testing.T) {
	t.Parallel()
	store, _ := testStore(t)
	job := Job{
		ID:   "job-1",
		Name: "test job",
	}
	if err := store.Add(job); err != nil {
		t.Fatalf("add: %v", err)
	}
	got, err := store.Get("job-1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Name != "test job" {
		t.Fatalf("expected name %q, got %q", "test job", got.Name)
	}
}

func TestStore_AddDuplicate(t *testing.T) {
	t.Parallel()
	store, _ := testStore(t)
	job := Job{ID: "job-1"}
	if err := store.Add(job); err != nil {
		t.Fatalf("add: %v", err)
	}
	err := store.Add(job)
	if err == nil {
		t.Fatal("expected error for duplicate")
	}
	if !errors.Is(err, ErrDuplicateID) {
		t.Fatalf("expected ErrDuplicateID, got %v", err)
	}
}

func TestStore_Update(t *testing.T) {
	t.Parallel()
	store, _ := testStore(t)
	if err := store.Add(Job{ID: "job-1", Name: "original"}); err != nil {
		t.Fatalf("add: %v", err)
	}
	if err := store.Update("job-1", func(j *Job) {
		j.Name = "updated"
	}); err != nil {
		t.Fatalf("update: %v", err)
	}
	got, _ := store.Get("job-1")
	if got.Name != "updated" {
		t.Fatalf("expected name %q, got %q", "updated", got.Name)
	}
}

func TestStore_UpdateNotFound(t *testing.T) {
	t.Parallel()
	store, _ := testStore(t)
	err := store.Update("nonexistent", func(_ *Job) {})
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestStore_Remove(t *testing.T) {
	t.Parallel()
	store, _ := testStore(t)
	if err := store.Add(Job{ID: "job-1"}); err != nil {
		t.Fatalf("add: %v", err)
	}
	if err := store.Remove("job-1"); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if len(store.List()) != 0 {
		t.Fatal("expected empty list after remove")
	}
}

func TestStore_RemoveNotFound(t *testing.T) {
	t.Parallel()
	store, _ := testStore(t)
	err := store.Remove("nonexistent")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestStore_SaveAndReload(t *testing.T) {
	t.Parallel()
	store, path := testStore(t)
	now := time.Now().UTC().Truncate(time.Second)
	if err := store.Add(Job{
		ID:        "job-1",
		Name:      "persistent",
		Enabled:   true,
		CreatedAt: now,
		UpdatedAt: now,
		Schedule:  Schedule{Kind: ScheduleKindEvery, Every: "5m"},
		Payload:   Payload{Content: "hello"},
	}); err != nil {
		t.Fatalf("add: %v", err)
	}
	if err := store.Save(); err != nil {
		t.Fatalf("save: %v", err)
	}

	// Reload from disk.
	store2, err := Load(path)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	jobs := store2.List()
	if len(jobs) != 1 {
		t.Fatalf("expected 1 job, got %d", len(jobs))
	}
	if jobs[0].Name != "persistent" {
		t.Fatalf("expected name %q, got %q", "persistent", jobs[0].Name)
	}
	if jobs[0].Payload.Content != "hello" {
		t.Fatalf("expected content %q, got %q", "hello", jobs[0].Payload.Content)
	}
}

func TestStore_ListReturnsCopy(t *testing.T) {
	t.Parallel()
	store, _ := testStore(t)
	if err := store.Add(Job{ID: "job-1", Name: "original"}); err != nil {
		t.Fatalf("add: %v", err)
	}
	list := store.List()
	list[0].Name = "mutated"
	got, _ := store.Get("job-1")
	if got.Name != "original" {
		t.Fatal("List returned a reference instead of a copy")
	}
}

func TestStore_GetReturnsCopy(t *testing.T) {
	t.Parallel()
	store, _ := testStore(t)
	if err := store.Add(Job{ID: "job-1", Name: "original"}); err != nil {
		t.Fatalf("add: %v", err)
	}
	got, _ := store.Get("job-1")
	got.Name = "mutated"
	got2, _ := store.Get("job-1")
	if got2.Name != "original" {
		t.Fatal("Get returned a reference instead of a copy")
	}
}

func TestStore_SaveCreatesDirectory(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "nested", "deep", "cron-jobs.json")
	store, err := Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if err := store.Add(Job{ID: "job-1"}); err != nil {
		t.Fatalf("add: %v", err)
	}
	if err := store.Save(); err != nil {
		t.Fatalf("save: %v", err)
	}
	// Verify file exists.
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("stat: %v", err)
	}
}

func TestStore_SaveAtomicJSON(t *testing.T) {
	t.Parallel()
	store, path := testStore(t)
	if err := store.Add(Job{ID: "job-1", Name: "atomic"}); err != nil {
		t.Fatalf("add: %v", err)
	}
	if err := store.Save(); err != nil {
		t.Fatalf("save: %v", err)
	}

	// Read raw file and ensure it's valid JSON.
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	var sf StoreFile
	if err := json.Unmarshal(data, &sf); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if sf.Version != storeFileVersion {
		t.Fatalf("expected version %d, got %d", storeFileVersion, sf.Version)
	}
	if len(sf.Jobs) != 1 {
		t.Fatalf("expected 1 job, got %d", len(sf.Jobs))
	}
}

// ---------------------------------------------------------------------------
// Service lifecycle tests
// ---------------------------------------------------------------------------

type mockSubmitter struct {
	mu           sync.Mutex
	requests     []automation.SubmitRequest
	result       *runtimesvc.RunResult
	getResults   []*runtimesvc.RunResult
	verification *verifyrt.RunVerification
	verifyErr    error
	err          error
	getErr       error
	getCalls     int
}

func (m *mockSubmitter) Submit(_ context.Context, req automation.SubmitRequest) (*runtimesvc.RunResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.requests = append(m.requests, req)
	return m.result, m.err
}

func (m *mockSubmitter) GetRunResult(_ context.Context, runID string) (*runtimesvc.RunResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.getCalls++
	if m.getErr != nil {
		return nil, m.getErr
	}
	if len(m.getResults) > 0 {
		result := m.getResults[0]
		if len(m.getResults) > 1 {
			m.getResults = m.getResults[1:]
		}
		if result != nil && result.RunID == "" {
			result.RunID = runID
		}
		return result, nil
	}
	result := m.result
	if result != nil && result.RunID == "" {
		result.RunID = runID
	}
	return result, nil
}

func (m *mockSubmitter) Requests() []automation.SubmitRequest {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]automation.SubmitRequest, len(m.requests))
	copy(out, m.requests)
	return out
}

func (m *mockSubmitter) GetRunCalls() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.getCalls
}

func (m *mockSubmitter) GetRunVerification(_ context.Context, runID string) (*verifyrt.RunVerification, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.verifyErr != nil {
		return nil, m.verifyErr
	}
	if m.verification == nil {
		return nil, nil
	}
	copyVerification := *m.verification
	if copyVerification.RunID == "" {
		copyVerification.RunID = runID
	}
	if len(copyVerification.Checks) > 0 {
		copyVerification.Checks = append([]verifyrt.Check(nil), copyVerification.Checks...)
	}
	return &copyVerification, nil
}

type mockDeliverer struct {
	mu    sync.Mutex
	calls []deliverCall
	err   error
}

type deliverCall struct {
	Channel string
	Target  string
	Content string
}

func (m *mockDeliverer) DeliverMessage(_ context.Context, target automation.DeliveryTarget, content string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls = append(m.calls, deliverCall{Channel: target.Channel, Target: target.Target, Content: content})
	return m.err
}

func TestService_StartStop(t *testing.T) {
	t.Parallel()
	store, _ := testStore(t)
	submitter := &mockSubmitter{result: &runtimesvc.RunResult{RunID: "r1", Status: "completed"}}
	svc := NewService(store, submitter, nil)

	ctx := context.Background()
	if err := svc.Start(ctx); err != nil {
		t.Fatalf("start: %v", err)
	}
	if !svc.IsRunning() {
		t.Fatal("expected service to be running")
	}

	// Starting again should fail.
	if err := svc.Start(ctx); err == nil {
		t.Fatal("expected error on double start")
	}

	_ = svc.Stop()
	if svc.IsRunning() {
		t.Fatal("expected service to be stopped")
	}
}

func TestService_ExecuteDueJobs(t *testing.T) {
	t.Parallel()
	store, _ := testStore(t)
	now := time.Now().UTC()

	if err := store.Add(Job{
		ID:        "due-job",
		Name:      "due",
		Enabled:   true,
		Schedule:  Schedule{Kind: ScheduleKindEvery, Every: "1h"},
		Payload:   Payload{Content: "check status"},
		NextRunAt: now.Add(-1 * time.Minute), // already past
		CreatedAt: now.Add(-2 * time.Hour),
		UpdatedAt: now,
	}); err != nil {
		t.Fatalf("add: %v", err)
	}

	submitter := &mockSubmitter{result: &runtimesvc.RunResult{RunID: "r1", Status: "completed"}}
	svc := NewService(store, submitter, nil)

	// Execute due jobs directly (don't rely on timer).
	svc.fireDueJobs(context.Background())

	// Wait briefly for the synchronous execution to complete.
	time.Sleep(100 * time.Millisecond)

	reqs := submitter.Requests()
	if len(reqs) != 1 {
		t.Fatalf("expected 1 submit, got %d", len(reqs))
	}
	if reqs[0].Content != "check status" {
		t.Fatalf("expected content %q, got %q", "check status", reqs[0].Content)
	}

	// Verify job state was updated.
	job, err := store.Get("due-job")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if job.LastStatus != RunStatusOK {
		t.Fatalf("expected last status %q, got %q", RunStatusOK, job.LastStatus)
	}
	if job.LastRunAt.IsZero() {
		t.Fatal("expected LastRunAt to be set")
	}
	if job.LastResult == nil {
		t.Fatal("expected LastResult to be populated")
	}
	if job.LastResult.Status != "ok" || job.LastResult.RunID != "r1" {
		t.Fatalf("LastResult = %#v", job.LastResult)
	}
}

func TestService_SkipsDisabledJobs(t *testing.T) {
	t.Parallel()
	store, _ := testStore(t)
	now := time.Now().UTC()

	if err := store.Add(Job{
		ID:        "disabled-job",
		Enabled:   false,
		Schedule:  Schedule{Kind: ScheduleKindEvery, Every: "1h"},
		Payload:   Payload{Content: "should not run"},
		NextRunAt: now.Add(-1 * time.Minute),
		CreatedAt: now,
		UpdatedAt: now,
	}); err != nil {
		t.Fatalf("add: %v", err)
	}

	submitter := &mockSubmitter{result: &runtimesvc.RunResult{RunID: "r1", Status: "completed"}}
	svc := NewService(store, submitter, nil)
	svc.fireDueJobs(context.Background())

	time.Sleep(50 * time.Millisecond)
	if len(submitter.Requests()) != 0 {
		t.Fatal("disabled job should not have been submitted")
	}
}

func TestService_SkipsFutureJobs(t *testing.T) {
	t.Parallel()
	store, _ := testStore(t)
	now := time.Now().UTC()

	if err := store.Add(Job{
		ID:        "future-job",
		Enabled:   true,
		Schedule:  Schedule{Kind: ScheduleKindEvery, Every: "1h"},
		Payload:   Payload{Content: "not yet"},
		NextRunAt: now.Add(1 * time.Hour),
		CreatedAt: now,
		UpdatedAt: now,
	}); err != nil {
		t.Fatalf("add: %v", err)
	}

	submitter := &mockSubmitter{result: &runtimesvc.RunResult{RunID: "r1", Status: "completed"}}
	svc := NewService(store, submitter, nil)
	svc.fireDueJobs(context.Background())

	time.Sleep(50 * time.Millisecond)
	if len(submitter.Requests()) != 0 {
		t.Fatal("future job should not have been submitted")
	}
}

func TestService_AtJobDisabledAfterRun(t *testing.T) {
	t.Parallel()
	store, _ := testStore(t)
	now := time.Now().UTC()

	if err := store.Add(Job{
		ID:        "oneshot",
		Name:      "one-shot",
		Enabled:   true,
		Schedule:  Schedule{Kind: ScheduleKindAt, At: now.Add(-1 * time.Minute).Format(time.RFC3339)},
		Payload:   Payload{Content: "run once"},
		NextRunAt: now.Add(-1 * time.Minute),
		CreatedAt: now.Add(-2 * time.Minute),
		UpdatedAt: now,
	}); err != nil {
		t.Fatalf("add: %v", err)
	}

	submitter := &mockSubmitter{result: &runtimesvc.RunResult{RunID: "r1", Status: "completed"}}
	svc := NewService(store, submitter, nil)
	svc.fireDueJobs(context.Background())

	time.Sleep(100 * time.Millisecond)

	job, err := store.Get("oneshot")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if job.Enabled {
		t.Fatal("one-shot at job should be disabled after execution")
	}
}

func TestService_TriggerJob(t *testing.T) {
	t.Parallel()
	store, _ := testStore(t)
	now := time.Now().UTC()

	if err := store.Add(Job{
		ID:        "trigger-me",
		Name:      "manual",
		Enabled:   true,
		Schedule:  Schedule{Kind: ScheduleKindEvery, Every: "1h"},
		Payload:   Payload{Content: "manual trigger"},
		NextRunAt: now.Add(1 * time.Hour),
		CreatedAt: now,
		UpdatedAt: now,
	}); err != nil {
		t.Fatalf("add: %v", err)
	}

	submitter := &mockSubmitter{result: &runtimesvc.RunResult{RunID: "r1", Status: "completed"}}
	svc := NewService(store, submitter, nil)

	if err := svc.TriggerJob(context.Background(), "trigger-me"); err != nil {
		t.Fatalf("trigger: %v", err)
	}

	// Wait for async execution.
	time.Sleep(200 * time.Millisecond)

	reqs := submitter.Requests()
	if len(reqs) != 1 {
		t.Fatalf("expected 1 submit, got %d", len(reqs))
	}
	if reqs[0].Content != "manual trigger" {
		t.Fatalf("expected content %q, got %q", "manual trigger", reqs[0].Content)
	}
}

func TestService_TriggerJobNotFound(t *testing.T) {
	t.Parallel()
	store, _ := testStore(t)
	submitter := &mockSubmitter{result: &runtimesvc.RunResult{RunID: "r1", Status: "completed"}}
	svc := NewService(store, submitter, nil)

	err := svc.TriggerJob(context.Background(), "nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent job")
	}
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestService_SubmitError(t *testing.T) {
	t.Parallel()
	store, _ := testStore(t)
	now := time.Now().UTC()

	if err := store.Add(Job{
		ID:        "fail-job",
		Enabled:   true,
		Schedule:  Schedule{Kind: ScheduleKindEvery, Every: "1h"},
		Payload:   Payload{Content: "will fail"},
		NextRunAt: now.Add(-1 * time.Minute),
		CreatedAt: now,
		UpdatedAt: now,
	}); err != nil {
		t.Fatalf("add: %v", err)
	}

	submitter := &mockSubmitter{
		result: &runtimesvc.RunResult{},
		err:    errors.New("connection refused"),
	}
	svc := NewService(store, submitter, nil)
	svc.fireDueJobs(context.Background())

	time.Sleep(100 * time.Millisecond)

	job, err := store.Get("fail-job")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if job.LastStatus != RunStatusError {
		t.Fatalf("expected last status %q, got %q", RunStatusError, job.LastStatus)
	}
	if job.LastError == "" {
		t.Fatal("expected non-empty last error")
	}
}

func TestService_SessionKeyDefault(t *testing.T) {
	t.Parallel()
	store, _ := testStore(t)
	now := time.Now().UTC()

	if err := store.Add(Job{
		ID:        "no-session-key",
		Enabled:   true,
		Schedule:  Schedule{Kind: ScheduleKindEvery, Every: "1h"},
		Payload:   Payload{Content: "default key"},
		NextRunAt: now.Add(-1 * time.Minute),
		CreatedAt: now,
		UpdatedAt: now,
	}); err != nil {
		t.Fatalf("add: %v", err)
	}

	submitter := &mockSubmitter{result: &runtimesvc.RunResult{RunID: "r1", Status: "completed"}}
	svc := NewService(store, submitter, nil)
	svc.fireDueJobs(context.Background())

	time.Sleep(100 * time.Millisecond)

	reqs := submitter.Requests()
	if len(reqs) != 1 {
		t.Fatalf("expected 1 submit, got %d", len(reqs))
	}
	if reqs[0].SessionKey != "cron:no-session-key" {
		t.Fatalf("expected default session key %q, got %q", "cron:no-session-key", reqs[0].SessionKey)
	}
}

func TestService_CustomSessionKey(t *testing.T) {
	t.Parallel()
	store, _ := testStore(t)
	now := time.Now().UTC()

	if err := store.Add(Job{
		ID:         "custom-key",
		Enabled:    true,
		SessionKey: "my-session",
		Schedule:   Schedule{Kind: ScheduleKindEvery, Every: "1h"},
		Payload:    Payload{Content: "custom key"},
		NextRunAt:  now.Add(-1 * time.Minute),
		CreatedAt:  now,
		UpdatedAt:  now,
	}); err != nil {
		t.Fatalf("add: %v", err)
	}

	submitter := &mockSubmitter{result: &runtimesvc.RunResult{RunID: "r1", Status: "completed"}}
	svc := NewService(store, submitter, nil)
	svc.fireDueJobs(context.Background())

	time.Sleep(100 * time.Millisecond)

	reqs := submitter.Requests()
	if len(reqs) != 1 {
		t.Fatalf("expected 1 submit, got %d", len(reqs))
	}
	if reqs[0].SessionKey != "my-session" {
		t.Fatalf("expected session key %q, got %q", "my-session", reqs[0].SessionKey)
	}
}

func TestExecutor_RunPropagatesAutomationContext(t *testing.T) {
	t.Parallel()

	submitter := &mockSubmitter{result: &runtimesvc.RunResult{RunID: "r-scope", Status: "completed"}}
	exec := &executor{runner: automation.NewRunner(submitter, executionTimeout, pollInterval)}

	result := exec.run(context.Background(), &Job{
		ID:         "job-scope",
		Name:       "daily sync",
		SessionKey: "ops:daily",
		Payload:    Payload{Content: "sync the daily report"},
	})
	status, errMsg := outcomeFromResult(result)
	if status != RunStatusOK {
		t.Fatalf("status = %q, want %q (err: %s)", status, RunStatusOK, errMsg)
	}

	reqs := submitter.Requests()
	if len(reqs) != 1 {
		t.Fatalf("expected 1 submit, got %d", len(reqs))
	}
	if reqs[0].SessionKey != "ops:daily" {
		t.Fatalf("SessionKey = %q", reqs[0].SessionKey)
	}
	if reqs[0].AutomationID != "job-scope" {
		t.Fatalf("AutomationID = %q, want %q", reqs[0].AutomationID, "job-scope")
	}
	if reqs[0].Metadata["automation_kind"] != "cron" || reqs[0].Metadata["automation_id"] != "job-scope" || reqs[0].Metadata["automation_name"] != "daily sync" {
		t.Fatalf("metadata = %+v", reqs[0].Metadata)
	}
}

// ---------------------------------------------------------------------------
// Executor delivery tests
// ---------------------------------------------------------------------------

func TestExecutor_Delivery(t *testing.T) {
	t.Parallel()
	submitter := &mockSubmitter{result: &runtimesvc.RunResult{RunID: "r1", Status: "completed"}}
	deliverer := &mockDeliverer{}
	exec := &executor{runner: automation.NewRunner(submitter, executionTimeout, pollInterval), channels: deliverer}

	job := &Job{
		ID:      "deliver-job",
		Name:    "deliver",
		Payload: Payload{Content: "test"},
		Delivery: &Delivery{
			Channel: "slack",
			Target:  "C123",
		},
	}

	result := exec.run(context.Background(), job)
	status, errMsg := outcomeFromResult(result)
	if status != RunStatusOK {
		t.Fatalf("expected status %q, got %q (err: %s)", RunStatusOK, status, errMsg)
	}

	// Deliver result.
	err := exec.deliver(context.Background(), job, "result content")
	if err != nil {
		t.Fatalf("deliver: %v", err)
	}

	deliverer.mu.Lock()
	defer deliverer.mu.Unlock()
	if len(deliverer.calls) != 1 {
		t.Fatalf("expected 1 delivery call, got %d", len(deliverer.calls))
	}
	if deliverer.calls[0].Channel != "slack" {
		t.Fatalf("expected channel %q, got %q", "slack", deliverer.calls[0].Channel)
	}
	if deliverer.calls[0].Target != "C123" {
		t.Fatalf("expected target %q, got %q", "C123", deliverer.calls[0].Target)
	}
}

func TestExecutor_RunPollsAsyncStatus(t *testing.T) {
	t.Parallel()
	submitter := &mockSubmitter{
		result:     &runtimesvc.RunResult{RunID: "r1", Status: "queued"},
		getResults: []*runtimesvc.RunResult{{Status: "completed", Output: "report ready"}},
	}
	exec := &executor{runner: automation.NewRunner(submitter, executionTimeout, pollInterval)}

	result := exec.run(context.Background(), &Job{
		ID:      "async-job",
		Payload: Payload{Content: "work"},
	})
	status, errMsg := outcomeFromResult(result)
	if status != RunStatusOK {
		t.Fatalf("status = %q, want %q (err: %s)", status, RunStatusOK, errMsg)
	}
	if errMsg != "" {
		t.Fatalf("errMsg = %q, want empty", errMsg)
	}
	if result.Output != "report ready" {
		t.Fatalf("Output = %q, want %q", result.Output, "report ready")
	}
	if submitter.GetRunCalls() != 1 {
		t.Fatalf("GetRunCalls = %d, want 1", submitter.GetRunCalls())
	}
}

func TestService_DeliversRunOutputAndArtifacts(t *testing.T) {
	t.Parallel()
	store, _ := testStore(t)
	now := time.Now().UTC()

	if err := store.Add(Job{
		ID:       "deliver-output",
		Name:     "deliver output",
		Enabled:  true,
		Schedule: Schedule{Kind: ScheduleKindEvery, Every: "1h"},
		Payload:  Payload{Content: "monitor"},
		Delivery: &Delivery{
			Channel: "slack",
			Target:  "C123",
		},
		NextRunAt: now.Add(-1 * time.Minute),
		CreatedAt: now.Add(-2 * time.Hour),
		UpdatedAt: now,
	}); err != nil {
		t.Fatalf("add: %v", err)
	}

	submitter := &mockSubmitter{result: &runtimesvc.RunResult{
		RunID:        "r1",
		Status:       "completed",
		Output:       "AAPL crossed 200",
		Deliverables: []runtimesvc.DeliverableRef{{URI: "artifact://chart-1"}},
	}}
	deliverer := &mockDeliverer{}
	svc := NewService(store, submitter, deliverer)

	svc.fireDueJobs(context.Background())
	time.Sleep(100 * time.Millisecond)

	deliverer.mu.Lock()
	defer deliverer.mu.Unlock()
	if len(deliverer.calls) != 1 {
		t.Fatalf("expected 1 delivery call, got %d", len(deliverer.calls))
	}
	want := "AAPL crossed 200\n\nArtifacts:\n- artifact://chart-1"
	if deliverer.calls[0].Content != want {
		t.Fatalf("Content = %q, want %q", deliverer.calls[0].Content, want)
	}
	job, err := store.Get("deliver-output")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if job.Notifications.TotalCount != 1 || job.Notifications.TodayCount != 1 {
		t.Fatalf("Notifications = %+v", job.Notifications)
	}
	if job.Notifications.LastStatus != "delivered" {
		t.Fatalf("LastStatus = %q", job.Notifications.LastStatus)
	}
}

func TestService_DeliversVerificationWarningWithSuccessfulResult(t *testing.T) {
	t.Parallel()
	store, _ := testStore(t)
	now := time.Now().UTC()

	if err := store.Add(Job{
		ID:        "deliver-warning",
		Name:      "deliver warning",
		Enabled:   true,
		Schedule:  Schedule{Kind: ScheduleKindEvery, Every: "1h"},
		Payload:   Payload{Content: "monitor"},
		Delivery:  &Delivery{Channel: "slack", Target: "C123"},
		NextRunAt: now.Add(-1 * time.Minute),
		CreatedAt: now.Add(-2 * time.Hour),
		UpdatedAt: now,
	}); err != nil {
		t.Fatalf("add: %v", err)
	}

	submitter := &mockSubmitter{
		result: &runtimesvc.RunResult{RunID: "r-warning", Status: "completed", Output: "Market summary ready"},
		verification: &verifyrt.RunVerification{
			Status:  verifyrt.StatusWarning,
			Summary: "verification finished with 1 advisory warning",
		},
	}
	deliverer := &mockDeliverer{}
	svc := NewService(store, submitter, deliverer)

	svc.fireDueJobs(context.Background())
	time.Sleep(100 * time.Millisecond)

	deliverer.mu.Lock()
	defer deliverer.mu.Unlock()
	if len(deliverer.calls) != 1 {
		t.Fatalf("expected 1 delivery call, got %d", len(deliverer.calls))
	}
	if !strings.Contains(deliverer.calls[0].Content, "Verification warning: verification finished with 1 advisory warning") {
		t.Fatalf("Content = %q", deliverer.calls[0].Content)
	}
	if !strings.Contains(deliverer.calls[0].Content, "Market summary ready") {
		t.Fatalf("Content = %q", deliverer.calls[0].Content)
	}
}

func TestService_DoesNotDeliverSuccessWhenVerificationFails(t *testing.T) {
	t.Parallel()
	store, _ := testStore(t)
	now := time.Now().UTC()

	if err := store.Add(Job{
		ID:        "deliver-verification-fail",
		Name:      "deliver verification fail",
		Enabled:   true,
		Schedule:  Schedule{Kind: ScheduleKindEvery, Every: "1h"},
		Payload:   Payload{Content: "monitor"},
		Delivery:  &Delivery{Channel: "slack", Target: "C123"},
		NextRunAt: now.Add(-1 * time.Minute),
		CreatedAt: now.Add(-2 * time.Hour),
		UpdatedAt: now,
	}); err != nil {
		t.Fatalf("add: %v", err)
	}

	submitter := &mockSubmitter{
		result: &runtimesvc.RunResult{RunID: "r-fail", Status: "completed", Output: "AAPL crossed 200"},
		verification: &verifyrt.RunVerification{
			Status:  verifyrt.StatusFailed,
			Summary: "verification failed: artifact chart is missing",
		},
	}
	deliverer := &mockDeliverer{}
	svc := NewService(store, submitter, deliverer)

	svc.fireDueJobs(context.Background())
	time.Sleep(100 * time.Millisecond)

	job, err := store.Get("deliver-verification-fail")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if job.LastStatus != RunStatusError {
		t.Fatalf("LastStatus = %q, want %q", job.LastStatus, RunStatusError)
	}
	if !strings.Contains(job.LastError, "verification failed") {
		t.Fatalf("LastError = %q", job.LastError)
	}

	deliverer.mu.Lock()
	defer deliverer.mu.Unlock()
	if len(deliverer.calls) != 1 {
		t.Fatalf("expected 1 delivery call, got %d", len(deliverer.calls))
	}
	if strings.Contains(deliverer.calls[0].Content, "AAPL crossed 200") {
		t.Fatalf("unexpected success content = %q", deliverer.calls[0].Content)
	}
	if !strings.Contains(deliverer.calls[0].Content, `failed verification`) {
		t.Fatalf("Content = %q", deliverer.calls[0].Content)
	}
}

func TestExecutor_NoDeliveryWithoutConfig(t *testing.T) {
	t.Parallel()
	exec := &executor{runner: automation.NewRunner(&mockSubmitter{}, executionTimeout, pollInterval), channels: &mockDeliverer{}}
	job := &Job{ID: "no-delivery"}
	// No delivery config => no-op.
	err := exec.deliver(context.Background(), job, "content")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// Store thread-safety test
// ---------------------------------------------------------------------------

func TestStore_ConcurrentAccess(t *testing.T) {
	t.Parallel()
	store, _ := testStore(t)

	var wg sync.WaitGroup
	const goroutines = 10

	// Concurrent adds.
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			id := "job-" + time.Now().Format("150405.000000000") + "-" + string(rune('a'+idx))
			_ = store.Add(Job{ID: id, Name: "concurrent"})
		}(i)
	}
	wg.Wait()

	// Concurrent reads.
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = store.List()
		}()
	}
	wg.Wait()
}

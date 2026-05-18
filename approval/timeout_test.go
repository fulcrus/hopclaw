package approval

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// stubStore implements Store for timeout tests
// ---------------------------------------------------------------------------

type stubStore struct {
	mu      sync.Mutex
	tickets []*Ticket
	listErr error
}

func newStubStore(tickets ...*Ticket) *stubStore {
	return &stubStore{tickets: tickets}
}

func (s *stubStore) Create(_ context.Context, ticket Ticket) (*Ticket, error) {
	return &ticket, nil
}

func (s *stubStore) Get(_ context.Context, ticketID string) (*Ticket, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, t := range s.tickets {
		if t.ID == ticketID {
			return t, nil
		}
	}
	return nil, nil
}

func (s *stubStore) GetByRun(_ context.Context, _ string) (*Ticket, error) {
	return nil, nil
}

func (s *stubStore) GetByExternal(_ context.Context, _, _ string) (*Ticket, error) {
	return nil, nil
}

func (s *stubStore) List(_ context.Context, filter ListFilter) ([]*Ticket, error) {
	filter = filter.Normalize()
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.listErr != nil {
		return nil, s.listErr
	}
	var out []*Ticket
	for _, t := range s.tickets {
		if filter.Status == "" || t.Status == filter.Status {
			out = append(out, t)
		}
	}
	if filter.Offset > 0 {
		if filter.Offset >= len(out) {
			return nil, nil
		}
		out = out[filter.Offset:]
	}
	if filter.Limit > 0 && len(out) > filter.Limit {
		out = out[:filter.Limit]
	}
	return out, nil
}

func (s *stubStore) Resolve(_ context.Context, ticketID string, res Resolution) (*Ticket, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, t := range s.tickets {
		if t.ID == ticketID {
			t.Status = res.Status
			return t, nil
		}
	}
	return nil, nil
}

func (s *stubStore) UpsertExternalRef(_ context.Context, ticketID string, ref ExternalReference) (*Ticket, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, t := range s.tickets {
		if t.ID != ticketID {
			continue
		}
		nextRefs, _, err := UpsertExternalReferences(t.External, ref)
		if err != nil {
			return nil, err
		}
		t.External = nextRefs
		return t, nil
	}
	return nil, nil
}

// ---------------------------------------------------------------------------
// NewTimeoutService
// ---------------------------------------------------------------------------

func TestNewTimeoutServiceDefaultInterval(t *testing.T) {
	t.Parallel()
	svc := NewTimeoutService(TimeoutConfig{
		ApprovalTimeout: 5 * time.Minute,
	}, newStubStore(), func(_ context.Context, _ string) error { return nil }, nil)
	if svc == nil {
		t.Fatal("NewTimeoutService returned nil")
	}
	if svc.config.CheckInterval != defaultCheckInterval {
		t.Fatalf("CheckInterval = %v, want %v", svc.config.CheckInterval, defaultCheckInterval)
	}
}

func TestNewTimeoutServiceCustomInterval(t *testing.T) {
	t.Parallel()
	custom := 10 * time.Second
	svc := NewTimeoutService(TimeoutConfig{
		ApprovalTimeout: 5 * time.Minute,
		CheckInterval:   custom,
	}, newStubStore(), func(_ context.Context, _ string) error { return nil }, nil)
	if svc.config.CheckInterval != custom {
		t.Fatalf("CheckInterval = %v, want %v", svc.config.CheckInterval, custom)
	}
}

// ---------------------------------------------------------------------------
// sweep — auto-cancel expired tickets
// ---------------------------------------------------------------------------

func TestSweepCancelsExpiredTicket(t *testing.T) {
	t.Parallel()

	expired := &Ticket{
		ID:        "ticket-expired",
		Status:    StatusPending,
		CreatedAt: time.Now().UTC().Add(-10 * time.Minute),
	}
	store := newStubStore(expired)

	var resolvedID string
	var mu sync.Mutex
	resolve := func(_ context.Context, id string) error {
		mu.Lock()
		resolvedID = id
		mu.Unlock()
		return nil
	}

	svc := NewTimeoutService(TimeoutConfig{
		ApprovalTimeout: 5 * time.Minute,
		CheckInterval:   time.Millisecond,
	}, store, resolve, nil)

	svc.sweep(context.Background())

	mu.Lock()
	defer mu.Unlock()
	if resolvedID != "ticket-expired" {
		t.Fatalf("resolved ID = %q, want ticket-expired", resolvedID)
	}
}

func TestSweepDoesNotCancelFreshTicket(t *testing.T) {
	t.Parallel()

	fresh := &Ticket{
		ID:        "ticket-fresh",
		Status:    StatusPending,
		CreatedAt: time.Now().UTC(),
	}
	store := newStubStore(fresh)

	resolved := false
	resolve := func(_ context.Context, _ string) error {
		resolved = true
		return nil
	}

	svc := NewTimeoutService(TimeoutConfig{
		ApprovalTimeout: 5 * time.Minute,
		CheckInterval:   time.Millisecond,
	}, store, resolve, nil)

	svc.sweep(context.Background())

	if resolved {
		t.Fatal("fresh ticket should not be auto-cancelled")
	}
}

// ---------------------------------------------------------------------------
// sweep — grace period warning
// ---------------------------------------------------------------------------

func TestSweepSendsGraceWarning(t *testing.T) {
	t.Parallel()

	// Ticket is 4 minutes old, timeout is 5 minutes, grace is 2 minutes.
	// graceThreshold = 5m - 2m = 3m. Age (4m) >= graceThreshold (3m) -> warn.
	nearTimeout := &Ticket{
		ID:        "ticket-grace",
		Status:    StatusPending,
		CreatedAt: time.Now().UTC().Add(-4 * time.Minute),
	}
	store := newStubStore(nearTimeout)

	var notifiedTicketID string
	var notifiedRemaining time.Duration
	var mu sync.Mutex
	notify := func(_ context.Context, ticket *Ticket, remaining time.Duration) {
		mu.Lock()
		notifiedTicketID = ticket.ID
		notifiedRemaining = remaining
		mu.Unlock()
	}

	svc := NewTimeoutService(TimeoutConfig{
		ApprovalTimeout: 5 * time.Minute,
		GracePeriod:     2 * time.Minute,
		CheckInterval:   time.Millisecond,
	}, store, func(_ context.Context, _ string) error { return nil }, notify)

	svc.sweep(context.Background())

	mu.Lock()
	defer mu.Unlock()
	if notifiedTicketID != "ticket-grace" {
		t.Fatalf("notified ticket ID = %q, want ticket-grace", notifiedTicketID)
	}
	if notifiedRemaining <= 0 {
		t.Fatalf("remaining = %v, expected positive", notifiedRemaining)
	}
}

func TestSweepDoesNotDuplicateGraceWarning(t *testing.T) {
	t.Parallel()

	nearTimeout := &Ticket{
		ID:        "ticket-no-dup",
		Status:    StatusPending,
		CreatedAt: time.Now().UTC().Add(-4 * time.Minute),
	}
	store := newStubStore(nearTimeout)

	notifyCount := 0
	var mu sync.Mutex
	notify := func(_ context.Context, _ *Ticket, _ time.Duration) {
		mu.Lock()
		notifyCount++
		mu.Unlock()
	}

	svc := NewTimeoutService(TimeoutConfig{
		ApprovalTimeout: 5 * time.Minute,
		GracePeriod:     2 * time.Minute,
		CheckInterval:   time.Millisecond,
	}, store, func(_ context.Context, _ string) error { return nil }, notify)

	svc.sweep(context.Background())
	svc.sweep(context.Background())
	svc.sweep(context.Background())

	mu.Lock()
	defer mu.Unlock()
	if notifyCount != 1 {
		t.Fatalf("notifyCount = %d, want 1 (no duplicates)", notifyCount)
	}
}

func TestSweepNoWarningIfNoNotifyFunc(t *testing.T) {
	t.Parallel()

	nearTimeout := &Ticket{
		ID:        "ticket-no-notify",
		Status:    StatusPending,
		CreatedAt: time.Now().UTC().Add(-4 * time.Minute),
	}
	store := newStubStore(nearTimeout)

	svc := NewTimeoutService(TimeoutConfig{
		ApprovalTimeout: 5 * time.Minute,
		GracePeriod:     2 * time.Minute,
		CheckInterval:   time.Millisecond,
	}, store, func(_ context.Context, _ string) error { return nil }, nil)

	// Should not panic when notify is nil.
	svc.sweep(context.Background())
}

// ---------------------------------------------------------------------------
// sweep — prune notified map
// ---------------------------------------------------------------------------

func TestSweepPrunesNotifiedMapForResolvedTickets(t *testing.T) {
	t.Parallel()

	ticket := &Ticket{
		ID:        "ticket-prune",
		Status:    StatusPending,
		CreatedAt: time.Now().UTC().Add(-4 * time.Minute),
	}
	store := newStubStore(ticket)

	svc := NewTimeoutService(TimeoutConfig{
		ApprovalTimeout: 5 * time.Minute,
		GracePeriod:     2 * time.Minute,
		CheckInterval:   time.Millisecond,
	}, store, func(_ context.Context, _ string) error { return nil },
		func(_ context.Context, _ *Ticket, _ time.Duration) {})

	// First sweep: ticket gets notified.
	svc.sweep(context.Background())

	svc.mu.Lock()
	if _, ok := svc.notified["ticket-prune"]; !ok {
		svc.mu.Unlock()
		t.Fatal("expected ticket-prune in notified map after first sweep")
	}
	svc.mu.Unlock()

	// Simulate ticket resolution by removing it from the store.
	store.mu.Lock()
	store.tickets = nil
	store.mu.Unlock()

	// Second sweep: notified entry should be pruned.
	svc.sweep(context.Background())

	svc.mu.Lock()
	defer svc.mu.Unlock()
	if _, ok := svc.notified["ticket-prune"]; ok {
		t.Fatal("expected ticket-prune to be pruned from notified map")
	}
}

func TestSweepReportsListFailureAndRecovery(t *testing.T) {
	t.Parallel()

	store := newStubStore()
	store.listErr = errors.New("list unavailable")

	var (
		listFailureErr error
		recoveredCount int
		mu             sync.Mutex
	)
	svc := NewTimeoutService(TimeoutConfig{
		ApprovalTimeout: 5 * time.Minute,
		CheckInterval:   time.Millisecond,
	}, store, func(_ context.Context, _ string) error { return nil }, nil).WithFailureHooks(TimeoutFailureHooks{
		OnListFailure: func(err error) {
			mu.Lock()
			listFailureErr = err
			mu.Unlock()
		},
		OnListRecovered: func() {
			mu.Lock()
			recoveredCount++
			mu.Unlock()
		},
	})

	svc.sweep(context.Background())

	mu.Lock()
	if listFailureErr == nil || listFailureErr.Error() != "list unavailable" {
		mu.Unlock()
		t.Fatalf("OnListFailure error = %v", listFailureErr)
	}
	mu.Unlock()

	store.mu.Lock()
	store.listErr = nil
	store.mu.Unlock()

	svc.sweep(context.Background())

	mu.Lock()
	defer mu.Unlock()
	if recoveredCount != 1 {
		t.Fatalf("OnListRecovered count = %d, want 1", recoveredCount)
	}
}

func TestSweepReportsResolveFailureAttempts(t *testing.T) {
	t.Parallel()

	expired := &Ticket{
		ID:        "ticket-retry",
		Status:    StatusPending,
		CreatedAt: time.Now().UTC().Add(-10 * time.Minute),
	}
	store := newStubStore(expired)

	var (
		attempts []int
		ticketID string
		mu       sync.Mutex
	)
	svc := NewTimeoutService(TimeoutConfig{
		ApprovalTimeout: 5 * time.Minute,
		CheckInterval:   time.Millisecond,
	}, store, func(_ context.Context, _ string) error {
		return errors.New("resolve failed")
	}, nil).WithFailureHooks(TimeoutFailureHooks{
		OnResolveFailure: func(ticket *Ticket, attempt int, err error) {
			mu.Lock()
			attempts = append(attempts, attempt)
			if ticket != nil {
				ticketID = ticket.ID
			}
			mu.Unlock()
		},
	})

	svc.sweep(context.Background())
	svc.sweep(context.Background())

	mu.Lock()
	defer mu.Unlock()
	if ticketID != "ticket-retry" {
		t.Fatalf("OnResolveFailure ticket ID = %q, want ticket-retry", ticketID)
	}
	if !reflect.DeepEqual(attempts, []int{1, 2}) {
		t.Fatalf("OnResolveFailure attempts = %#v, want []int{1, 2}", attempts)
	}
}

func TestSweepForceResolvesAfterRepeatedFailures(t *testing.T) {
	t.Parallel()

	expired := &Ticket{
		ID:        "ticket-force",
		Status:    StatusPending,
		CreatedAt: time.Now().UTC().Add(-10 * time.Minute),
	}
	store := newStubStore(expired)

	var (
		resolveCalls  int
		forceCalls    int
		recoveredIDs  []string
		recordedFails []int
		mu            sync.Mutex
	)
	svc := NewTimeoutService(TimeoutConfig{
		ApprovalTimeout: 5 * time.Minute,
		CheckInterval:   time.Millisecond,
	}, store, func(_ context.Context, _ string) error {
		mu.Lock()
		resolveCalls++
		mu.Unlock()
		return errors.New("resolve failed")
	}, nil).WithFailureHooks(TimeoutFailureHooks{
		OnResolveFailure: func(_ *Ticket, attempt int, _ error) {
			mu.Lock()
			recordedFails = append(recordedFails, attempt)
			mu.Unlock()
		},
		OnResolveRecovered: func(ticketID string) {
			mu.Lock()
			recoveredIDs = append(recoveredIDs, ticketID)
			mu.Unlock()
		},
	}).WithForceResolve(2, func(ctx context.Context, ticket *Ticket) error {
		mu.Lock()
		forceCalls++
		mu.Unlock()
		_, err := store.Resolve(ctx, ticket.ID, Resolution{
			Status:     StatusCancelled,
			ResolvedBy: "test",
			Note:       "forced cancel",
		})
		return err
	})

	svc.sweep(context.Background())
	svc.sweep(context.Background())

	mu.Lock()
	defer mu.Unlock()
	if resolveCalls != 2 {
		t.Fatalf("resolve calls = %d, want 2", resolveCalls)
	}
	if forceCalls != 1 {
		t.Fatalf("force calls = %d, want 1", forceCalls)
	}
	if !reflect.DeepEqual(recordedFails, []int{1}) {
		t.Fatalf("OnResolveFailure attempts = %#v, want []int{1}", recordedFails)
	}
	if !reflect.DeepEqual(recoveredIDs, []string{"ticket-force"}) {
		t.Fatalf("OnResolveRecovered IDs = %#v, want []string{\"ticket-force\"}", recoveredIDs)
	}
	if expired.Status != StatusCancelled {
		t.Fatalf("ticket status = %q, want %q", expired.Status, StatusCancelled)
	}
}

// ---------------------------------------------------------------------------
// Start / Stop
// ---------------------------------------------------------------------------

func TestStartAndStop(t *testing.T) {
	t.Parallel()

	store := newStubStore()
	svc := NewTimeoutService(TimeoutConfig{
		ApprovalTimeout: 5 * time.Minute,
		CheckInterval:   time.Millisecond,
	}, store, func(_ context.Context, _ string) error { return nil }, nil)

	svc.Start(context.Background())
	// Give it a moment to run at least one tick.
	time.Sleep(10 * time.Millisecond)
	svc.Stop()
	// Should not panic on double stop.
	svc.Stop()
}

func TestStopNilService(t *testing.T) {
	t.Parallel()
	var svc *TimeoutService
	// Should not panic.
	svc.Stop()
}

func TestStartContextCancel(t *testing.T) {
	t.Parallel()

	store := newStubStore()
	svc := NewTimeoutService(TimeoutConfig{
		ApprovalTimeout: 5 * time.Minute,
		CheckInterval:   time.Millisecond,
	}, store, func(_ context.Context, _ string) error { return nil }, nil)

	ctx, cancel := context.WithCancel(context.Background())
	svc.Start(ctx)
	cancel()
	// Give the goroutine time to notice cancellation.
	time.Sleep(10 * time.Millisecond)
}

func TestStartIsIdempotent(t *testing.T) {
	t.Parallel()

	store := newStubStore()
	svc := NewTimeoutService(TimeoutConfig{
		ApprovalTimeout: 5 * time.Minute,
		CheckInterval:   time.Hour,
	}, store, func(_ context.Context, _ string) error { return nil }, nil)

	ctx := context.Background()
	svc.Start(ctx)

	svc.mu.Lock()
	firstCancel := svc.cancel
	svc.mu.Unlock()
	if firstCancel == nil {
		t.Fatal("expected cancel func after first Start")
	}

	svc.Start(ctx)

	svc.mu.Lock()
	secondCancel := svc.cancel
	svc.mu.Unlock()
	if secondCancel == nil {
		t.Fatal("expected cancel func after second Start")
	}
	if reflect.ValueOf(firstCancel).Pointer() != reflect.ValueOf(secondCancel).Pointer() {
		t.Fatal("expected second Start to be a no-op")
	}

	svc.Stop()

	svc.mu.Lock()
	defer svc.mu.Unlock()
	if svc.cancel != nil {
		t.Fatal("expected Stop to clear cancel func")
	}
}

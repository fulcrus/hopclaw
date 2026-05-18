package approval

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/fulcrus/hopclaw/logging"
)

var log = logging.WithSubsystem("approval")

// ---------------------------------------------------------------------------
// Defaults
// ---------------------------------------------------------------------------

const (
	defaultCheckInterval = 30 * time.Second
	defaultForceAttempts = 3
)

// ---------------------------------------------------------------------------
// Callback types
// ---------------------------------------------------------------------------

// ResolveFunc is called to cancel a timed-out approval ticket.
type ResolveFunc func(ctx context.Context, ticketID string) error

// NotifyFunc is called to warn that a ticket is approaching its timeout.
type NotifyFunc func(ctx context.Context, ticket *Ticket, remaining time.Duration)

// ForceResolveFunc is called after repeated timeout-resolution failures to
// perform a last-resort cancellation path for the stuck ticket.
type ForceResolveFunc func(ctx context.Context, ticket *Ticket) error

// TimeoutFailureHooks receives timeout-sweep failures so callers can project
// degraded approval-timeout state beyond local logs.
type TimeoutFailureHooks struct {
	OnListFailure      func(err error)
	OnListRecovered    func()
	OnResolveFailure   func(ticket *Ticket, attempts int, err error)
	OnResolveRecovered func(ticketID string)
}

// ---------------------------------------------------------------------------
// TimeoutConfig
// ---------------------------------------------------------------------------

// TimeoutConfig controls the approval timeout sweep behaviour.
type TimeoutConfig struct {
	ApprovalTimeout time.Duration // when to auto-cancel a pending ticket
	GracePeriod     time.Duration // how long before timeout to send a warning
	CheckInterval   time.Duration // sweep frequency (default 30s)
}

// ---------------------------------------------------------------------------
// TimeoutService
// ---------------------------------------------------------------------------

// TimeoutService periodically sweeps pending approval tickets and
// auto-cancels those that exceed the configured timeout.
type TimeoutService struct {
	config  TimeoutConfig
	store   Store
	resolve ResolveFunc
	notify  NotifyFunc // optional, may be nil

	mu               sync.Mutex          // guards sweep state
	notified         map[string]struct{} // ticket IDs that already received a grace warning
	resolveFailures  map[string]int
	forceResolve     ForceResolveFunc
	maxForceAttempts int
	failureHooks     TimeoutFailureHooks
	listFailed       bool

	cancel context.CancelFunc
}

// NewTimeoutService creates a new TimeoutService. The notify callback is
// optional and may be nil if grace-period warnings are not needed.
func NewTimeoutService(cfg TimeoutConfig, store Store, resolve ResolveFunc, notify NotifyFunc) *TimeoutService {
	if cfg.CheckInterval <= 0 {
		cfg.CheckInterval = defaultCheckInterval
	}
	return &TimeoutService{
		config:          cfg,
		store:           store,
		resolve:         resolve,
		notify:          notify,
		notified:        make(map[string]struct{}),
		resolveFailures: make(map[string]int),
	}
}

// WithFailureHooks installs callbacks for approval-timeout sweep failures and
// recoveries.
func (s *TimeoutService) WithFailureHooks(hooks TimeoutFailureHooks) *TimeoutService {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	s.failureHooks = hooks
	s.mu.Unlock()
	return s
}

// WithForceResolve installs a bounded last-resort timeout cancellation path.
// When maxAttempts is zero or negative, a conservative default is used.
func (s *TimeoutService) WithForceResolve(maxAttempts int, force ForceResolveFunc) *TimeoutService {
	if s == nil {
		return nil
	}
	if force != nil && maxAttempts <= 0 {
		maxAttempts = defaultForceAttempts
	}
	s.mu.Lock()
	s.forceResolve = force
	s.maxForceAttempts = maxAttempts
	s.mu.Unlock()
	return s
}

// Start launches the background sweep loop. It is safe to call Start only
// once; subsequent calls without a Stop in between are a no-op.
func (s *TimeoutService) Start(ctx context.Context) {
	if s == nil {
		return
	}
	s.mu.Lock()
	if s.cancel != nil {
		s.mu.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(ctx)
	s.cancel = cancel
	s.mu.Unlock()

	go func() {
		ticker := time.NewTicker(s.config.CheckInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				s.sweep(ctx)
			}
		}
	}()
}

// Stop cancels the background sweep loop.
func (s *TimeoutService) Stop() {
	if s == nil {
		return
	}
	s.mu.Lock()
	cancel := s.cancel
	s.cancel = nil
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

// sweep lists all pending tickets and either auto-cancels or warns
// depending on each ticket's age relative to the configured thresholds.
func (s *TimeoutService) sweep(ctx context.Context) {
	tickets, err := s.store.List(ctx, ListFilter{Status: StatusPending})
	if err != nil {
		s.recordListFailure(err)
		log.Warn("approval timeout sweep failed to list tickets", "error", err)
		return
	}
	s.clearListFailure()

	now := time.Now().UTC()
	graceThreshold := s.config.ApprovalTimeout - s.config.GracePeriod

	// Collect IDs of tickets still pending so we can prune the notified map.
	pendingIDs := make(map[string]struct{}, len(tickets))
	for _, ticket := range tickets {
		pendingIDs[ticket.ID] = struct{}{}
	}

	for _, ticket := range tickets {
		age := now.Sub(ticket.CreatedAt)

		if age >= s.config.ApprovalTimeout {
			if err := s.resolve(ctx, ticket.ID); err != nil {
				attempts, finalErr := s.handleResolveFailure(ctx, ticket, err)
				log.Warn("approval timeout resolve failed",
					"ticket_id", ticket.ID,
					"attempts", attempts,
					"error", finalErr,
				)
			} else {
				s.clearResolveFailure(ticket.ID)
			}
			continue
		}

		if age >= graceThreshold && s.notify != nil {
			s.mu.Lock()
			_, alreadyNotified := s.notified[ticket.ID]
			if !alreadyNotified {
				s.notified[ticket.ID] = struct{}{}
			}
			s.mu.Unlock()

			if !alreadyNotified {
				remaining := s.config.ApprovalTimeout - age
				s.notify(ctx, ticket, remaining)
			}
		}
	}

	// Prune notified entries for tickets that are no longer pending.
	s.mu.Lock()
	for id := range s.notified {
		if _, still := pendingIDs[id]; !still {
			delete(s.notified, id)
		}
	}
	recovered := make([]string, 0)
	for id := range s.resolveFailures {
		if _, still := pendingIDs[id]; still {
			continue
		}
		delete(s.resolveFailures, id)
		recovered = append(recovered, id)
	}
	hooks := s.failureHooks
	s.mu.Unlock()
	if hooks.OnResolveRecovered != nil {
		for _, id := range recovered {
			hooks.OnResolveRecovered(id)
		}
	}
}

func (s *TimeoutService) handleResolveFailure(ctx context.Context, ticket *Ticket, err error) (int, error) {
	if ticket == nil {
		s.mu.Lock()
		hooks := s.failureHooks
		s.mu.Unlock()
		if hooks.OnResolveFailure != nil {
			hooks.OnResolveFailure(nil, 1, err)
		}
		return 1, err
	}

	attempts, hooks, force, maxAttempts := s.nextResolveFailureForTicket(ticket.ID)
	finalErr := err
	if force != nil && maxAttempts > 0 && attempts >= maxAttempts {
		if forceErr := force(ctx, ticket); forceErr == nil {
			s.clearResolveFailure(ticket.ID)
			return attempts, nil
		} else {
			finalErr = fmt.Errorf("resolve timed-out approval: %w; force-cancel fallback failed: %w", err, forceErr)
		}
	}
	if hooks.OnResolveFailure != nil {
		hooks.OnResolveFailure(ticket, attempts, finalErr)
	}
	return attempts, finalErr
}

func (s *TimeoutService) nextResolveFailureForTicket(ticketID string) (int, TimeoutFailureHooks, ForceResolveFunc, int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.resolveFailures[ticketID]++
	return s.resolveFailures[ticketID], s.failureHooks, s.forceResolve, s.maxForceAttempts
}

func (s *TimeoutService) clearResolveFailure(ticketID string) {
	if s == nil {
		return
	}
	ticketID = strings.TrimSpace(ticketID)
	if ticketID == "" {
		return
	}
	s.mu.Lock()
	_, hadFailure := s.resolveFailures[ticketID]
	delete(s.resolveFailures, ticketID)
	hooks := s.failureHooks
	s.mu.Unlock()
	if hadFailure && hooks.OnResolveRecovered != nil {
		hooks.OnResolveRecovered(ticketID)
	}
}

func (s *TimeoutService) recordListFailure(err error) {
	if s == nil || err == nil {
		return
	}
	s.mu.Lock()
	s.listFailed = true
	hooks := s.failureHooks
	s.mu.Unlock()
	if hooks.OnListFailure != nil {
		hooks.OnListFailure(err)
	}
}

func (s *TimeoutService) clearListFailure() {
	if s == nil {
		return
	}
	s.mu.Lock()
	hadFailure := s.listFailed
	s.listFailed = false
	hooks := s.failureHooks
	s.mu.Unlock()
	if hadFailure && hooks.OnListRecovered != nil {
		hooks.OnListRecovered()
	}
}

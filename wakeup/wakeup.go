// Package wakeup extends the cron system with time-based triggers that wake
// the agent to perform scheduled actions such as morning briefings or daily
// summaries.
package wakeup

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/fulcrus/hopclaw/cron"
	"github.com/fulcrus/hopclaw/logging"
)

var log = logging.WithSubsystem("wakeup")

// ---------------------------------------------------------------------------
// Constants
// ---------------------------------------------------------------------------

const (
	// tickInterval is how often the service checks for due triggers.
	tickInterval = 15 * time.Second
)

// ---------------------------------------------------------------------------
// Sentinel errors
// ---------------------------------------------------------------------------

var (
	ErrNotFound       = errors.New("not found")
	ErrDuplicateID    = errors.New("duplicate trigger id")
	ErrNotRunning     = errors.New("service is not running")
	ErrAlreadyRunning = errors.New("service is already running")
)

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

// ExecutionResult captures the runtime-facing outcome of a wakeup trigger.
type ExecutionResult struct {
	RunID               string
	Summary             string
	VerificationStatus  string
	VerificationSummary string
}

// SubmitFunc is called when a wakeup trigger fires.
type SubmitFunc func(ctx context.Context, trigger Trigger) (*ExecutionResult, error)

// Trigger defines a scheduled wakeup event.
type Trigger struct {
	ID                      string            `json:"id"`
	Name                    string            `json:"name"`
	Schedule                string            `json:"schedule"`
	Channel                 string            `json:"channel"`
	SessionKey              string            `json:"session_key"`
	Message                 string            `json:"message"`
	Model                   string            `json:"model,omitempty"`
	AutomationID            string            `json:"automation_id,omitempty"`
	Enabled                 bool              `json:"enabled"`
	Timezone                string            `json:"timezone,omitempty"`
	Metadata                map[string]string `json:"metadata,omitempty"`
	CreatedAt               time.Time         `json:"created_at"`
	LastRunAt               time.Time         `json:"last_run_at,omitempty"`
	LastRunID               string            `json:"last_run_id,omitempty"`
	LastStatus              string            `json:"last_status,omitempty"`
	LastError               string            `json:"last_error,omitempty"`
	LastSummary             string            `json:"last_summary,omitempty"`
	LastVerificationStatus  string            `json:"last_verification_status,omitempty"`
	LastVerificationSummary string            `json:"last_verification_summary,omitempty"`
	NextRunAt               time.Time         `json:"next_run_at,omitempty"`
}

// ---------------------------------------------------------------------------
// Service
// ---------------------------------------------------------------------------

// Service manages wakeup triggers and fires them at their scheduled times.
type Service struct {
	mu      sync.RWMutex
	store   *Store
	stopCh  chan struct{}
	running bool
	submit  SubmitFunc
}

// NewService creates a wakeup Service. The submit function is called whenever
// a trigger fires. Call Start to begin the ticker loop.
func NewService(store *Store, submit SubmitFunc) *Service {
	if store == nil {
		store = NewStore("")
	}
	return &Service{
		store:  store,
		submit: submit,
	}
}

// ---------------------------------------------------------------------------
// Lifecycle
// ---------------------------------------------------------------------------

// Start begins the ticker loop in a background goroutine. Returns an error
// if the service is already running.
func (s *Service) Start(ctx context.Context) error {
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return ErrAlreadyRunning
	}
	s.stopCh = make(chan struct{})
	s.running = true
	s.mu.Unlock()

	if err := s.seedNextRunTimes(); err != nil {
		return err
	}

	go s.loop(ctx)
	go s.fireDueTriggers(ctx)

	log.Info("wakeup service started", "trigger_count", s.triggerCount())
	return nil
}

// Stop gracefully shuts down the ticker loop.
func (s *Service) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.running {
		return
	}
	close(s.stopCh)
	s.running = false

	log.Info("wakeup service stopped")
}

// IsRunning reports whether the ticker loop is active.
func (s *Service) IsRunning() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.running
}

// ---------------------------------------------------------------------------
// CRUD
// ---------------------------------------------------------------------------

// Add registers a new trigger and computes its NextRunAt. Returns
// ErrDuplicateID if a trigger with the same ID already exists.
func (s *Service) Add(trigger Trigger) error {
	if trigger.CreatedAt.IsZero() {
		trigger.CreatedAt = time.Now().UTC()
	}
	if trigger.Enabled {
		next, err := s.computeNextRun(trigger.Schedule, trigger.Timezone, time.Now().UTC())
		if err != nil {
			return fmt.Errorf("wakeup: compute next run for trigger %s: %w", trigger.ID, err)
		}
		trigger.NextRunAt = next
	} else {
		trigger.NextRunAt = time.Time{}
	}

	if err := s.store.Add(trigger); err != nil {
		return err
	}
	return s.store.Save()
}

// Remove deletes the trigger with the given ID.
func (s *Service) Remove(id string) error {
	if err := s.store.Remove(id); err != nil {
		return err
	}
	return s.store.Save()
}

// Update applies fn to the trigger with the given ID. The caller can mutate
// any field of the trigger inside fn.
func (s *Service) Update(id string, fn func(*Trigger)) error {
	now := time.Now().UTC()
	var nextErr error
	if err := s.store.Update(id, func(t *Trigger) {
		prevSchedule := t.Schedule
		prevTimezone := t.Timezone
		prevEnabled := t.Enabled
		fn(t)
		needsRecompute := t.Schedule != prevSchedule || t.Timezone != prevTimezone || t.Enabled != prevEnabled
		if !needsRecompute {
			return
		}
		if t.Enabled {
			next, err := s.computeNextRun(t.Schedule, t.Timezone, now)
			if err != nil {
				nextErr = err
				return
			}
			if err == nil {
				t.NextRunAt = next
			}
		} else {
			t.NextRunAt = time.Time{}
		}
	}); err != nil {
		return err
	}
	if nextErr != nil {
		return fmt.Errorf("wakeup: trigger %s: %w", id, nextErr)
	}
	return s.store.Save()
}

// Get returns a copy of the trigger with the given ID.
func (s *Service) Get(id string) (*Trigger, error) {
	return s.store.Get(id)
}

// List returns a copy of all triggers sorted by ID.
func (s *Service) List() []Trigger {
	out := s.store.List()
	sort.Slice(out, func(i, j int) bool {
		return out[i].ID < out[j].ID
	})
	return out
}

// Enable enables the trigger and recomputes its NextRunAt.
func (s *Service) Enable(id string) error {
	now := time.Now().UTC()
	var nextErr error
	if err := s.store.Update(id, func(t *Trigger) {
		t.Enabled = true
		next, err := s.computeNextRunLocked(t.Schedule, t.Timezone, now)
		if err != nil {
			nextErr = err
			return
		}
		if err == nil {
			t.NextRunAt = next
		}
	}); err != nil {
		return err
	}
	if nextErr != nil {
		return fmt.Errorf("wakeup: trigger %s: %w", id, nextErr)
	}
	return s.store.Save()
}

// Disable disables the trigger and clears its NextRunAt.
func (s *Service) Disable(id string) error {
	if err := s.store.Update(id, func(t *Trigger) {
		t.Enabled = false
		t.NextRunAt = time.Time{}
	}); err != nil {
		return err
	}
	return s.store.Save()
}

// ---------------------------------------------------------------------------
// Ticker loop
// ---------------------------------------------------------------------------

func (s *Service) loop(ctx context.Context) {
	ticker := time.NewTicker(tickInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-s.stopCh:
			return
		case <-ticker.C:
			s.fireDueTriggers(ctx)
		}
	}
}

// fireDueTriggers checks all enabled triggers and fires those whose
// NextRunAt is at or before now.
func (s *Service) fireDueTriggers(ctx context.Context) {
	now := time.Now().UTC()
	items := s.store.List()
	var due []*Trigger
	for i := range items {
		t := items[i]
		if !t.Enabled {
			continue
		}
		if t.NextRunAt.IsZero() {
			continue
		}
		if t.NextRunAt.After(now) {
			continue
		}
		cp := t
		due = append(due, &cp)
	}

	for _, t := range due {
		s.fireTrigger(ctx, t, now)
	}
}

func (s *Service) fireTrigger(ctx context.Context, t *Trigger, now time.Time) {
	log.Info("wakeup trigger fired",
		"trigger_id", t.ID,
		"trigger_name", t.Name,
		"channel", t.Channel,
	)

	status := "triggered"
	errText := ""
	var err error
	var execution *ExecutionResult
	if s.submit == nil {
		status = "error"
		errText = "wakeup submitter is not configured"
	} else {
		execution, err = s.submit(ctx, *t)
		if err != nil {
			status = "error"
			errText = err.Error()
			log.Error("wakeup: submit failed",
				"trigger_id", t.ID,
				"error", err,
			)
		}
	}

	// Update LastRunAt and NextRunAt.
	next, err := s.computeNextRun(t.Schedule, t.Timezone, now)
	if err != nil {
		log.Warn("wakeup: compute next run after fire",
			"trigger_id", t.ID,
			"error", err,
		)
	}

	_ = s.store.Update(t.ID, func(current *Trigger) {
		current.LastRunAt = now
		if execution != nil {
			current.LastRunID = execution.RunID
			current.LastSummary = execution.Summary
			current.LastVerificationStatus = execution.VerificationStatus
			current.LastVerificationSummary = execution.VerificationSummary
		}
		current.LastStatus = status
		current.LastError = errText
		current.NextRunAt = next
	})
	_ = s.store.Save()
}

// ---------------------------------------------------------------------------
// Schedule parsing
// ---------------------------------------------------------------------------

// computeNextRun delegates to the cron package's NextRunTime to compute the
// next occurrence. The schedule string is interpreted as either a cron
// expression (5-field) or an "every" duration (e.g. "every 1h").
func (s *Service) computeNextRun(schedule, timezone string, after time.Time) (time.Time, error) {
	return s.computeNextRunLocked(schedule, timezone, after)
}

func (s *Service) computeNextRunLocked(schedule, timezone string, after time.Time) (time.Time, error) {
	cronSchedule, err := parseSchedule(schedule, timezone)
	if err != nil {
		return time.Time{}, err
	}
	return cron.NextRunTime(cronSchedule, after)
}

// parseSchedule converts a wakeup schedule string into a cron.Schedule.
// Supported formats:
//   - "every <duration>" (e.g. "every 1h", "every 30m")
//   - Standard 5-field cron expression (e.g. "0 9 * * *")
func parseSchedule(schedule, timezone string) (cron.Schedule, error) {
	if len(schedule) > 6 && schedule[:6] == "every " {
		duration := schedule[6:]
		return cron.Schedule{
			Kind:     "every",
			Every:    duration,
			Timezone: timezone,
		}, nil
	}

	// Treat as cron expression.
	return cron.Schedule{
		Kind:       "cron",
		Expression: schedule,
		Timezone:   timezone,
	}, nil
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func (s *Service) triggerCount() int {
	return len(s.store.List())
}

func (s *Service) seedNextRunTimes() error {
	now := time.Now().UTC()
	changed := false
	for _, trigger := range s.store.List() {
		if !trigger.Enabled || !trigger.NextRunAt.IsZero() {
			continue
		}
		next, err := s.computeNextRun(trigger.Schedule, trigger.Timezone, now)
		if err != nil {
			return fmt.Errorf("wakeup: seed trigger %s: %w", trigger.ID, err)
		}
		if next.IsZero() {
			continue
		}
		if err := s.store.Update(trigger.ID, func(t *Trigger) {
			t.NextRunAt = next
		}); err != nil {
			return err
		}
		changed = true
	}
	if changed {
		return s.store.Save()
	}
	return nil
}

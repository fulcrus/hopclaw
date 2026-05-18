package cron

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/fulcrus/hopclaw/automation"
	"github.com/fulcrus/hopclaw/logging"
	"github.com/fulcrus/hopclaw/resultmodel"
)

// ---------------------------------------------------------------------------
// Constants
// ---------------------------------------------------------------------------

const (
	// maxTimerDuration caps how far ahead the timer can sleep. When no jobs
	// are due within this window the loop wakes periodically to recheck.
	maxTimerDuration = 24 * time.Hour

	// minTimerDelay prevents the loop from spinning on extremely short
	// intervals caused by clock jitter or past-due jobs.
	minTimerDelay = 1 * time.Millisecond
)

// ---------------------------------------------------------------------------
// Service
// ---------------------------------------------------------------------------

// Service is the lifecycle owner of the cron scheduling system. It maintains
// a timer-based tick loop that fires jobs at their scheduled times and exposes
// methods the gateway uses for CRUD, manual triggering, and status queries.
type Service struct {
	store    *Store
	executor *executor

	executionTimeout time.Duration
	pollInterval     time.Duration

	mu      sync.Mutex // guards running, cancel, rearmCh
	running bool
	cancel  context.CancelFunc
	rearmCh chan struct{}
}

// NewService creates a cron Service. Pass nil for channels if delivery is not
// needed. Call Start to begin the scheduling loop.
func NewService(store *Store, runtime RuntimeSubmitter, channels ChannelDeliverer, opts ...Option) *Service {
	var verifier RuntimeVerifier
	if typed, ok := runtime.(RuntimeVerifier); ok {
		verifier = typed
	}
	svc := &Service{
		store:            store,
		executionTimeout: executionTimeout,
		pollInterval:     pollInterval,
		rearmCh:          make(chan struct{}, 1),
	}
	for _, opt := range opts {
		if opt != nil {
			opt(svc)
		}
	}
	svc.executor = &executor{
		runner:   automation.NewRunner(runtime, svc.executionTimeout, svc.pollInterval),
		verifier: verifier,
		channels: channels,
	}
	return svc
}

// ---------------------------------------------------------------------------
// Lifecycle
// ---------------------------------------------------------------------------

// Start begins the timer-based tick loop in a background goroutine. It seeds
// NextRunAt for any enabled jobs that do not yet have one, then launches the
// loop. Returns an error if the service is already running.
func (s *Service) Start(ctx context.Context) error {
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return fmt.Errorf("cron service is already running")
	}

	loopCtx, cancel := context.WithCancel(ctx)
	s.cancel = cancel
	s.running = true
	s.mu.Unlock()

	// Seed NextRunAt for enabled jobs that don't have one yet.
	s.seedNextRunTimes()

	// Run jobs that were due while the service was stopped.
	s.catchUpMissedJobs(loopCtx)

	go s.loop(loopCtx)

	log.Info("cron service started", "job_count", len(s.store.List()))
	return nil
}

// Stop gracefully shuts down the tick loop.
func (s *Service) Stop() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.running {
		return nil
	}
	s.cancel()
	s.running = false

	log.Info("cron service stopped")
	return nil
}

// Store returns the underlying job store for use by gateway handlers.
func (s *Service) Store() *Store {
	return s.store
}

// IsRunning reports whether the scheduling loop is active.
func (s *Service) IsRunning() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.running
}

// Rearm signals the tick loop to re-evaluate the next fire time. Call this
// after any job CRUD operation so the timer adjusts immediately.
func (s *Service) Rearm() {
	select {
	case s.rearmCh <- struct{}{}:
	default:
		// A rearm signal is already pending; no need to queue another.
	}
}

// TriggerJob manually executes the job with the given ID, bypassing its
// schedule. The job's LastRunAt, LastStatus, and LastError are updated.
func (s *Service) TriggerJob(ctx context.Context, id string) error {
	job, err := s.store.Get(id)
	if err != nil {
		return fmt.Errorf("trigger job: %w", err)
	}

	log.Info("cron job manually triggered", "job_id", id, "job_name", job.Name)
	go s.executeAndUpdate(ctx, job)
	return nil
}

// ---------------------------------------------------------------------------
// Startup catchup
// ---------------------------------------------------------------------------

const (
	// catchUpStagger is the delay between consecutive catchup job executions.
	catchUpStagger = 5 * time.Second

	// maxCatchUpJobs caps the number of missed jobs processed on startup.
	maxCatchUpJobs = 5
)

// catchUpMissedJobs runs jobs that were due while the service was stopped,
// staggering execution to avoid thundering herd.
func (s *Service) catchUpMissedJobs(ctx context.Context) {
	now := time.Now().UTC()
	jobs := s.store.List()

	var missed []Job
	for _, job := range jobs {
		if !job.Enabled || job.NextRunAt.IsZero() {
			continue
		}
		if job.NextRunAt.Before(now) {
			missed = append(missed, job)
		}
	}

	if len(missed) == 0 {
		return
	}

	if len(missed) > maxCatchUpJobs {
		missed = missed[:maxCatchUpJobs]
	}

	log.Info("cron: catching up missed jobs", "count", len(missed))

	go func() {
		for i, job := range missed {
			if ctx.Err() != nil {
				return
			}
			if i > 0 {
				select {
				case <-time.After(catchUpStagger):
				case <-ctx.Done():
					return
				}
			}
			j := job // capture
			s.executeAndUpdate(ctx, &j)
		}
	}()
}

// ---------------------------------------------------------------------------
// Seed
// ---------------------------------------------------------------------------

// seedNextRunTimes computes an initial NextRunAt for every enabled job that
// does not already have one (e.g. after a cold start).
func (s *Service) seedNextRunTimes() {
	now := time.Now().UTC()
	changed := false

	for _, job := range s.store.List() {
		if !job.Enabled || !job.NextRunAt.IsZero() {
			continue
		}
		anchor := job.LastRunAt
		if anchor.IsZero() {
			anchor = job.CreatedAt
		}
		next, err := NextRunTimeAnchored(job.Schedule, now, anchor)
		if err != nil {
			log.Warn("cron: compute initial next run",
				"job_id", job.ID,
				"error", err,
			)
			continue
		}
		if next.IsZero() {
			continue
		}
		logging.LogIfErr(context.Background(), s.store.Update(job.ID, func(j *Job) {
			j.NextRunAt = next
			j.UpdatedAt = now
		}), "update cron job failed")
		changed = true
	}

	if changed {
		logging.LogIfErr(context.Background(), s.store.Save(), "save cron store failed")
	}
}

// ---------------------------------------------------------------------------
// Tick loop
// ---------------------------------------------------------------------------

func (s *Service) loop(ctx context.Context) {
	timer := time.NewTimer(s.nextDelay())
	defer timer.Stop()

	for {
		select {
		case <-ctx.Done():
			return

		case <-s.rearmCh:
			// Job list changed — drain and reset the timer.
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			timer.Reset(s.nextDelay())

		case <-timer.C:
			s.fireDueJobs(ctx)
			timer.Reset(s.nextDelay())
		}
	}
}

// fireDueJobs finds and executes all jobs whose NextRunAt is at or before now.
func (s *Service) fireDueJobs(ctx context.Context) {
	now := time.Now().UTC()
	jobs := s.store.List()

	for i := range jobs {
		job := &jobs[i]
		if !job.Enabled {
			continue
		}
		if job.NextRunAt.IsZero() {
			continue
		}
		if job.NextRunAt.After(now) {
			continue
		}
		// Skip jobs in backoff period.
		if !job.BackoffUntil.IsZero() && now.Before(job.BackoffUntil) {
			continue
		}
		s.executeAndUpdate(ctx, job)
	}
}

// executeAndUpdate runs a single job through the executor and persists the
// resulting state (LastRunAt, LastStatus, LastError, NextRunAt) to the store.
func (s *Service) executeAndUpdate(ctx context.Context, job *Job) {
	now := time.Now().UTC()
	runResult := s.executor.run(ctx, job)
	status, errMsg := outcomeFromResult(runResult)

	// Attempt delivery if configured and the run succeeded.
	if status == RunStatusOK && job.Delivery != nil {
		if deliverErr := s.deliverNotification(ctx, job, deliveryContent(job, runResult)); deliverErr != nil {
			log.Warn("cron: delivery failed",
				"job_id", job.ID,
				"channel", job.Delivery.Channel,
				"error", deliverErr,
			)
		}
	}
	if status == RunStatusError && job.Delivery != nil && verificationFailed(runResult.Verification) {
		if deliverErr := s.deliverNotification(ctx, job, verificationFailureContent(job, runResult)); deliverErr != nil {
			log.Warn("cron: verification failure delivery failed",
				"job_id", job.ID,
				"channel", job.Delivery.Channel,
				"error", deliverErr,
			)
		}
	}

	// Compute the next run time using the anchored variant so "every"
	// schedules stay aligned to the original cadence.
	anchor := job.CreatedAt
	nextRun, schedErr := NextRunTimeAnchored(job.Schedule, now, anchor)
	if schedErr != nil {
		log.Warn("cron: compute next run",
			"job_id", job.ID,
			"error", schedErr,
		)
	}

	// Track consecutive errors for resilience.
	var consecutiveErrors int
	if status == RunStatusError {
		consecutiveErrors = job.ConsecutiveErrors + 1
	}

	disableAfterRun := job.Schedule.Kind == ScheduleKindAt
	autoDisable := consecutiveErrors >= maxConsecutiveErrors

	backoffUntil := time.Time{}
	if status == RunStatusError && !autoDisable {
		backoffUntil = now.Add(computeBackoff(consecutiveErrors))
	}

	logging.LogIfErr(ctx, s.store.Update(job.ID, func(j *Job) {
		canonical := resultmodel.CloneAutomationResult(&runResult)
		j.LastRunAt = now
		j.LastRunID = strings.TrimSpace(runResult.RunID)
		j.LastStatus = status
		j.LastError = errMsg
		j.LastSummary = strings.TrimSpace(runResult.Normalized().Summary)
		j.LastVerificationStatus = verificationStatus(runResult.Verification)
		j.LastVerificationSummary = verificationSummary(runResult.Verification)
		j.LastResult = canonical
		j.NextRunAt = nextRun
		j.UpdatedAt = now

		if status == RunStatusOK {
			j.ConsecutiveErrors = 0
			j.BackoffUntil = time.Time{}
		} else {
			j.ConsecutiveErrors = consecutiveErrors
			j.BackoffUntil = backoffUntil
		}

		if disableAfterRun || autoDisable {
			j.Enabled = false
			j.NextRunAt = time.Time{}
		}
	}), "update cron job failed")

	// Deliver failure alert when threshold is reached.
	if consecutiveErrors == failureAlertThreshold && job.Delivery != nil {
		alertMsg := fmt.Sprintf("[cron:%s] job %q has failed %d times consecutively. Last error: %s",
			job.ID, job.Name, consecutiveErrors, errMsg)
		if alertErr := s.deliverNotification(ctx, job, alertMsg); alertErr != nil {
			log.Warn("cron: failure alert delivery failed",
				"job_id", job.ID,
				"error", alertErr,
			)
		}
	}

	if autoDisable {
		log.Warn("cron: job auto-disabled after consecutive failures",
			"job_id", job.ID,
			"job_name", job.Name,
			"consecutive_errors", consecutiveErrors,
		)
		// Deliver auto-disable notification if possible.
		if job.Delivery != nil {
			disableMsg := fmt.Sprintf("[cron:%s] job %q auto-disabled after %d consecutive failures",
				job.ID, job.Name, consecutiveErrors)
			_ = s.deliverNotification(ctx, job, disableMsg)
		}
	}

	if saveErr := s.store.Save(); saveErr != nil {
		log.Error("cron: save store after job execution",
			"job_id", job.ID,
			"error", saveErr,
		)
	}

	log.Info("cron job executed",
		"job_id", job.ID,
		"job_name", job.Name,
		"status", status,
		"consecutive_errors", consecutiveErrors,
		"next_run_at", nextRun,
	)
}

func (s *Service) deliverNotification(ctx context.Context, job *Job, content string) error {
	if s == nil || job == nil || job.Delivery == nil || strings.TrimSpace(content) == "" {
		return nil
	}
	attemptAt := time.Now().UTC()
	err := s.executor.deliver(ctx, job, content)
	if recordErr := s.recordNotificationAttempt(job.ID, attemptAt, err); recordErr != nil {
		log.Warn("cron: record notification stats failed",
			"job_id", job.ID,
			"error", recordErr,
		)
	}
	return err
}

func (s *Service) recordNotificationAttempt(jobID string, attemptAt time.Time, deliverErr error) error {
	if s == nil || s.store == nil || strings.TrimSpace(jobID) == "" {
		return nil
	}
	if err := s.store.Update(jobID, func(j *Job) {
		j.Notifications = automation.RecordNotification(j.Notifications, attemptAt, deliverErr == nil, errorText(deliverErr))
		j.UpdatedAt = time.Now().UTC()
	}); err != nil {
		return err
	}
	return s.store.Save()
}

func errorText(err error) string {
	if err == nil {
		return ""
	}
	return strings.TrimSpace(err.Error())
}

// nextDelay computes the duration until the next job fires. If no enabled job
// has a future NextRunAt, it returns maxTimerDuration so the loop wakes
// periodically to recheck.
func (s *Service) nextDelay() time.Duration {
	now := time.Now().UTC()
	jobs := s.store.List()

	var earliest time.Time
	for _, job := range jobs {
		if !job.Enabled {
			continue
		}
		if job.NextRunAt.IsZero() {
			continue
		}
		if earliest.IsZero() || job.NextRunAt.Before(earliest) {
			earliest = job.NextRunAt
		}
	}

	if earliest.IsZero() {
		return maxTimerDuration
	}

	delay := earliest.Sub(now)
	if delay <= 0 {
		return minTimerDelay
	}
	if delay > maxTimerDuration {
		return maxTimerDuration
	}
	return delay
}

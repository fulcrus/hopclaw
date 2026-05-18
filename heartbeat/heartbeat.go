package heartbeat

import (
	"context"
	"fmt"
	"runtime"
	"sync"
	"time"

	"github.com/fulcrus/hopclaw/logging"
)

var log = logging.WithSubsystem("heartbeat")

// ---------------------------------------------------------------------------
// Constants
// ---------------------------------------------------------------------------

const (
	// defaultInterval is how often the background ticker fires a heartbeat.
	defaultInterval = 30 * time.Second

	// defaultTimeout is the duration after which the service is considered
	// stale if no beat has been recorded.
	defaultTimeout = 2 * time.Minute

	// idleThreshold is the duration of inactivity (no runs) after which the
	// service status transitions to idle.
	idleThreshold = 5 * time.Minute

	// bytesPerMB converts bytes to megabytes for memory reporting.
	bytesPerMB = 1024 * 1024

	// defaultPruneAge is the default max age for transcript pruning.
	defaultPruneAge = 7 * 24 * time.Hour

	// pruneInterval is how often transcript pruning is attempted.
	pruneInterval = 1 * time.Hour

	// wakeChSize is the buffer size for the wake signal channel.
	wakeChSize = 1
)

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

// Status represents the operational state of the heartbeat service.
type Status string

const (
	StatusOnline   Status = "online"
	StatusIdle     Status = "idle"
	StatusOffline  Status = "offline"
	StatusStale    Status = "stale"
	StatusSleeping Status = "sleeping"
)

// ActiveHoursConfig defines a time window when the service should be active.
type ActiveHoursConfig struct {
	Start    string `json:"start" yaml:"start"`       // HH:MM format, e.g. "09:00"
	End      string `json:"end" yaml:"end"`           // HH:MM format, e.g. "18:00"
	Timezone string `json:"timezone" yaml:"timezone"` // IANA timezone, default "Local"
}

// TranscriptPruner prunes transcript entries older than a given age.
type TranscriptPruner interface {
	PruneOlderThan(ctx context.Context, age time.Duration) (int, error)
}

// BeatListener is a callback invoked on each heartbeat.
type BeatListener func(beat Beat)

// ScheduledTask defines a recurring task executed by the heartbeat service.
type ScheduledTask struct {
	Name     string
	Interval time.Duration
	Fn       func(ctx context.Context) error
}

// Config holds the configuration for the heartbeat Service.
type Config struct {
	Interval       time.Duration          `json:"interval" yaml:"interval"`
	Timeout        time.Duration          `json:"timeout" yaml:"timeout"`
	OnBeat         func(beat Beat)        `json:"-" yaml:"-"`
	ActiveHours    *ActiveHoursConfig     `json:"active_hours,omitempty" yaml:"active_hours,omitempty"`
	Pruner         TranscriptPruner       `json:"-" yaml:"-"`
	PruneAge       time.Duration          `json:"prune_age" yaml:"prune_age"`
	OnStatusChange func(old, new_ Status) `json:"-" yaml:"-"`
	Tasks          []ScheduledTask        `json:"-" yaml:"-"`
}

// Beat represents a single heartbeat snapshot.
type Beat struct {
	BeatAt  time.Time     `json:"beat_at"`
	Status  Status        `json:"status"`
	Uptime  time.Duration `json:"uptime"`
	Metrics Metrics       `json:"metrics"`
}

// Metrics holds runtime telemetry captured on each beat.
type Metrics struct {
	ActiveSessions int     `json:"active_sessions"`
	TotalRuns      int64   `json:"total_runs"`
	MemoryUsageMB  float64 `json:"memory_usage_mb"`
	GoRoutines     int     `json:"go_routines"`
	CPUPercent     float64 `json:"cpu_percent,omitempty"`
}

// ---------------------------------------------------------------------------
// Service
// ---------------------------------------------------------------------------

// Service manages a background heartbeat ticker that periodically collects
// runtime metrics and broadcasts beat events.
type Service struct {
	mu          sync.RWMutex // guards all mutable fields below
	config      Config
	startedAt   time.Time
	lastBeat    time.Time
	status      Status
	metrics     Metrics
	enabled     bool
	stopCh      chan struct{}
	wakeCh      chan struct{}
	running     bool
	lastPruneAt time.Time
	taskLastRun []time.Time // parallel to config.Tasks; tracks last execution
}

// NewService creates a heartbeat Service with the given configuration.
// Zero-value durations in cfg are replaced with defaults.
func NewService(cfg Config) *Service {
	if cfg.Interval <= 0 {
		cfg.Interval = defaultInterval
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = defaultTimeout
	}
	if cfg.PruneAge <= 0 {
		cfg.PruneAge = defaultPruneAge
	}
	return &Service{
		config:      cfg,
		status:      StatusOffline,
		enabled:     true,
		wakeCh:      make(chan struct{}, wakeChSize),
		taskLastRun: make([]time.Time, len(cfg.Tasks)),
	}
}

// ---------------------------------------------------------------------------
// Lifecycle
// ---------------------------------------------------------------------------

// Start begins the background heartbeat ticker. It returns an error if the
// service is already running or has been disabled.
func (s *Service) Start(ctx context.Context) error {
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return fmt.Errorf("heartbeat service is already running")
	}
	if !s.enabled {
		s.mu.Unlock()
		return fmt.Errorf("heartbeat service is disabled")
	}

	s.stopCh = make(chan struct{})
	s.running = true
	s.status = StatusOnline
	s.startedAt = time.Now()

	// Capture under lock so the goroutine never races on s.stopCh or
	// s.config with a subsequent Start/Disable call.
	stopCh := s.stopCh
	wakeCh := s.wakeCh
	interval := s.config.Interval
	timeout := s.config.Timeout
	s.mu.Unlock()

	go s.loop(ctx, stopCh, wakeCh, interval)

	log.Info("heartbeat service started",
		"interval", interval,
		"timeout", timeout,
	)
	return nil
}

// Stop gracefully shuts down the heartbeat ticker.
func (s *Service) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.running {
		return
	}
	close(s.stopCh)
	s.running = false
	s.status = StatusOffline

	log.Info("heartbeat service stopped")
}

// IsRunning reports whether the heartbeat ticker is active.
func (s *Service) IsRunning() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.running
}

// ---------------------------------------------------------------------------
// Enable / Disable
// ---------------------------------------------------------------------------

// Enable marks the service as eligible to start.
func (s *Service) Enable() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.enabled = true
}

// Disable marks the service as ineligible to start and stops it if running.
func (s *Service) Disable() {
	s.mu.Lock()
	if s.running {
		close(s.stopCh)
		s.running = false
		s.status = StatusOffline
	}
	s.enabled = false
	s.mu.Unlock()
}

// ---------------------------------------------------------------------------
// Wake
// ---------------------------------------------------------------------------

// Wake sends a non-blocking wake signal that forces an immediate tick and
// resets the idle timer.
func (s *Service) Wake() {
	select {
	case s.wakeCh <- struct{}{}:
	default:
		// Channel is full; a wake is already pending.
	}
}

// ---------------------------------------------------------------------------
// Accessors
// ---------------------------------------------------------------------------

// Beat returns the latest heartbeat snapshot.
func (s *Service) Beat() Beat {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var uptime time.Duration
	if !s.startedAt.IsZero() {
		uptime = time.Since(s.startedAt)
	}

	return Beat{
		BeatAt:  s.lastBeat,
		Status:  s.status,
		Uptime:  uptime,
		Metrics: s.metrics,
	}
}

// LastBeat returns the timestamp of the most recent heartbeat.
func (s *Service) LastBeat() time.Time {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.lastBeat
}

// IsStale reports whether the last beat is older than the configured timeout.
func (s *Service) IsStale() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.lastBeat.IsZero() {
		return false
	}
	return time.Since(s.lastBeat) > s.config.Timeout
}

// SetStatus manually overrides the service status.
func (s *Service) SetStatus(status Status) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.status = status
}

// UpdateMetrics applies fn to the current metrics under the write lock,
// allowing callers to update counters in a thread-safe way.
func (s *Service) UpdateMetrics(fn func(*Metrics)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	fn(&s.metrics)
}

// ---------------------------------------------------------------------------
// Active hours
// ---------------------------------------------------------------------------

// isWithinActiveHours reports whether the current time falls inside the
// configured active-hours window. If no active hours are configured, it
// returns true (always active).
func (s *Service) isWithinActiveHours(now time.Time) bool {
	ah := s.config.ActiveHours
	if ah == nil {
		return true
	}

	loc := time.Local
	if ah.Timezone != "" {
		parsed, err := time.LoadLocation(ah.Timezone)
		if err != nil {
			log.Warn("heartbeat: invalid timezone, falling back to local",
				"timezone", ah.Timezone, "error", err)
		} else {
			loc = parsed
		}
	}

	localNow := now.In(loc)

	startH, startM, err := parseHHMM(ah.Start)
	if err != nil {
		log.Warn("heartbeat: invalid active hours start, treating as always active",
			"start", ah.Start, "error", err)
		return true
	}
	endH, endM, err := parseHHMM(ah.End)
	if err != nil {
		log.Warn("heartbeat: invalid active hours end, treating as always active",
			"end", ah.End, "error", err)
		return true
	}

	y, mo, d := localNow.Date()
	startTime := time.Date(y, mo, d, startH, startM, 0, 0, loc)
	endTime := time.Date(y, mo, d, endH, endM, 0, 0, loc)

	if endTime.Before(startTime) || endTime.Equal(startTime) {
		// Overnight window: e.g. 22:00 – 06:00
		return !localNow.Before(startTime) || localNow.Before(endTime)
	}
	return !localNow.Before(startTime) && localNow.Before(endTime)
}

// parseHHMM parses a "HH:MM" string into hour and minute integers.
func parseHHMM(s string) (int, int, error) {
	t, err := time.Parse("15:04", s)
	if err != nil {
		return 0, 0, fmt.Errorf("invalid HH:MM %q: %w", s, err)
	}
	return t.Hour(), t.Minute(), nil
}

// ---------------------------------------------------------------------------
// Tick loop
// ---------------------------------------------------------------------------

func (s *Service) loop(ctx context.Context, stopCh <-chan struct{}, wakeCh <-chan struct{}, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	// Fire an immediate first beat so callers don't have to wait a full
	// interval before seeing data.
	s.tick(ctx)

	for {
		select {
		case <-ctx.Done():
			s.Stop()
			return
		case <-stopCh:
			return
		case <-wakeCh:
			s.handleWake(ctx)
		case <-ticker.C:
			s.tick(ctx)
		}
	}
}

// handleWake processes a wake signal: resets the start time (idle timer) and
// forces an immediate tick.
func (s *Service) handleWake(ctx context.Context) {
	s.mu.Lock()
	s.startedAt = time.Now()
	if s.status == StatusIdle || s.status == StatusSleeping {
		s.status = StatusOnline
	}
	s.mu.Unlock()

	s.tick(ctx)
}

// tick collects runtime metrics, determines the current status, records the
// beat, and invokes the OnBeat callback if configured.
func (s *Service) tick(ctx context.Context) {
	now := time.Now()

	// Check active hours before collecting metrics.
	if !s.isWithinActiveHours(now) {
		s.mu.Lock()
		oldStatus := s.status
		s.status = StatusSleeping
		s.lastBeat = now
		onStatusChange := s.config.OnStatusChange
		onBeat := s.config.OnBeat

		var uptime time.Duration
		if !s.startedAt.IsZero() {
			uptime = now.Sub(s.startedAt)
		}
		beat := Beat{
			BeatAt:  now,
			Status:  s.status,
			Uptime:  uptime,
			Metrics: s.metrics,
		}
		s.mu.Unlock()

		if onStatusChange != nil && oldStatus != StatusSleeping {
			onStatusChange(oldStatus, StatusSleeping)
		}
		if onBeat != nil {
			onBeat(beat)
		}
		return
	}

	// Collect runtime stats outside the lock.
	goroutines := runtime.NumGoroutine()

	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)
	memMB := float64(mem.Alloc) / float64(bytesPerMB)

	s.mu.Lock()

	oldStatus := s.status

	// If we were sleeping and are now within active hours, go online.
	if s.status == StatusSleeping {
		s.status = StatusOnline
	}

	s.metrics.GoRoutines = goroutines
	s.metrics.MemoryUsageMB = memMB

	// Determine status: if there have been no runs for idleThreshold and the
	// service is online, transition to idle.
	if s.status == StatusOnline && s.metrics.TotalRuns == 0 && !s.startedAt.IsZero() &&
		now.Sub(s.startedAt) > idleThreshold {
		s.status = StatusIdle
	}

	s.lastBeat = now

	var uptime time.Duration
	if !s.startedAt.IsZero() {
		uptime = now.Sub(s.startedAt)
	}

	beat := Beat{
		BeatAt:  now,
		Status:  s.status,
		Uptime:  uptime,
		Metrics: s.metrics,
	}

	newStatus := s.status
	onBeat := s.config.OnBeat
	onStatusChange := s.config.OnStatusChange
	pruner := s.config.Pruner
	pruneAge := s.config.PruneAge
	lastPrune := s.lastPruneAt
	tasks := s.config.Tasks
	taskLastRun := make([]time.Time, len(s.taskLastRun))
	copy(taskLastRun, s.taskLastRun)
	s.mu.Unlock()

	// Fire status change callback if status has changed.
	if onStatusChange != nil && oldStatus != newStatus {
		onStatusChange(oldStatus, newStatus)
	}

	if onBeat != nil {
		onBeat(beat)
	}

	// Transcript pruning: run at most once per pruneInterval.
	if pruner != nil && (lastPrune.IsZero() || now.Sub(lastPrune) >= pruneInterval) {
		pruned, err := pruner.PruneOlderThan(ctx, pruneAge)
		if err != nil {
			log.Warn("heartbeat: transcript prune failed", "error", err)
		} else if pruned > 0 {
			log.Info("heartbeat: pruned transcripts", "count", pruned)
		}
		s.mu.Lock()
		s.lastPruneAt = now
		s.mu.Unlock()
	}

	// Scheduled tasks.
	for i, task := range tasks {
		if task.Interval <= 0 {
			continue
		}
		if taskLastRun[i].IsZero() || now.Sub(taskLastRun[i]) >= task.Interval {
			if err := task.Fn(ctx); err != nil {
				log.Warn("heartbeat: scheduled task failed",
					"task", task.Name, "error", err)
			}
			s.mu.Lock()
			s.taskLastRun[i] = now
			s.mu.Unlock()
		}
	}
}

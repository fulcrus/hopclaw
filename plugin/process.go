package plugin

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"
	"time"
)

// ---------------------------------------------------------------------------
// Process manager constants
// ---------------------------------------------------------------------------

const (
	// initialBackoff is the first restart delay after a crash.
	initialBackoff = 2 * time.Second

	// maxBackoff caps the exponential restart delay.
	maxBackoff = 5 * time.Minute

	// backoffMultiplier doubles the delay on each consecutive failure.
	backoffMultiplier = 2

	// defaultMaxRestarts is the default restart limit (0 = unlimited).
	defaultMaxRestarts = 0

	// healthyRunDuration resets the backoff counter when a process stays
	// alive for at least this long.
	healthyRunDuration = 2 * time.Minute
)

// ---------------------------------------------------------------------------
// ProcessConfig
// ---------------------------------------------------------------------------

// ProcessConfig describes how to spawn and supervise a plugin process.
type ProcessConfig struct {
	// Name is a human-readable identifier for the process.
	Name string

	// Command is the executable path.
	Command string

	// Args are command-line arguments.
	Args []string

	// Env is extra environment variables (key=value).
	Env map[string]string

	// WorkDir is the working directory.
	WorkDir string

	// MaxRestarts limits restart attempts (0 = unlimited).
	MaxRestarts int

	// OnReady is called once the process is spawned and the caller has
	// completed its setup (handshake, etc.). It is NOT managed by the
	// ProcessManager — see ProcessHandle.
	OnReady func()

	// OnExit is called whenever the process exits (before restart decision).
	OnExit func(err error)
}

// ---------------------------------------------------------------------------
// ProcessHandle
// ---------------------------------------------------------------------------

// ProcessStatus represents the lifecycle state of a managed process.
type ProcessStatus string

const (
	ProcessStatusRunning  ProcessStatus = "running"
	ProcessStatusStopped  ProcessStatus = "stopped"
	ProcessStatusBackoff  ProcessStatus = "backoff"
	ProcessStatusFailed   ProcessStatus = "failed"
	ProcessStatusStarting ProcessStatus = "starting"
)

// ProcessHandle holds runtime state for a managed process.
type ProcessHandle struct {
	Name        string
	Status      ProcessStatus
	Restarts    int
	LastError   string
	StartedAt   time.Time
	BackoffNext time.Duration
}

// ---------------------------------------------------------------------------
// ProcessManager
// ---------------------------------------------------------------------------

// SpawnFunc is called by the ProcessManager to start a process. It should
// block until the process exits and return any error. The context is
// cancelled when Stop is called.
type SpawnFunc func(ctx context.Context) error

// ProcessManager supervises plugin processes with automatic restart and
// exponential backoff.
type ProcessManager struct {
	mu       sync.Mutex
	handles  map[string]*processEntry
	wg       sync.WaitGroup
	stopping bool
}

type processEntry struct {
	cfg       ProcessConfig
	spawn     SpawnFunc
	handle    ProcessHandle
	cancel    context.CancelFunc
	backoff   time.Duration
	restarts  int
	startedAt time.Time
}

// NewProcessManager creates an empty process manager.
func NewProcessManager() *ProcessManager {
	return &ProcessManager{
		handles: make(map[string]*processEntry),
	}
}

// Supervise registers a process and starts supervising it. The spawnFn
// should block until the process exits.
func (pm *ProcessManager) Supervise(cfg ProcessConfig, spawnFn SpawnFunc) error {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	if pm.stopping {
		return fmt.Errorf("process manager is shutting down")
	}
	if _, exists := pm.handles[cfg.Name]; exists {
		return fmt.Errorf("process %q already supervised", cfg.Name)
	}

	entry := &processEntry{
		cfg:     cfg,
		spawn:   spawnFn,
		backoff: initialBackoff,
		handle: ProcessHandle{
			Name:   cfg.Name,
			Status: ProcessStatusStarting,
		},
	}
	pm.handles[cfg.Name] = entry

	pm.wg.Add(1)
	go pm.superviseLoop(entry)

	return nil
}

// Stop signals all managed processes to shut down and waits for them.
func (pm *ProcessManager) Stop(ctx context.Context) {
	pm.mu.Lock()
	pm.stopping = true
	for _, entry := range pm.handles {
		if entry.cancel != nil {
			entry.cancel()
		}
	}
	pm.mu.Unlock()

	done := make(chan struct{})
	go func() {
		pm.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-ctx.Done():
		log.Warn("process manager: shutdown timed out")
	}
}

// Handles returns a snapshot of all process handles.
func (pm *ProcessManager) Handles() []ProcessHandle {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	out := make([]ProcessHandle, 0, len(pm.handles))
	for _, entry := range pm.handles {
		out = append(out, entry.handle)
	}
	return out
}

// Handle returns the handle for a specific process.
func (pm *ProcessManager) Handle(name string) (ProcessHandle, bool) {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	entry, ok := pm.handles[name]
	if !ok {
		return ProcessHandle{}, false
	}
	return entry.handle, true
}

// Remove stops and removes a supervised process.
func (pm *ProcessManager) Remove(name string) {
	pm.mu.Lock()
	entry, ok := pm.handles[name]
	if !ok {
		pm.mu.Unlock()
		return
	}
	if entry.cancel != nil {
		entry.cancel()
	}
	delete(pm.handles, name)
	pm.mu.Unlock()
}

// ---------------------------------------------------------------------------
// Supervision loop
// ---------------------------------------------------------------------------

func (pm *ProcessManager) superviseLoop(entry *processEntry) {
	defer pm.wg.Done()

	// stopCtx is cancelled only when Stop() is called, used for backoff waits.
	stopCtx, stopCancel := context.WithCancel(context.Background())
	defer stopCancel()

	pm.mu.Lock()
	oldCancel := entry.cancel
	entry.cancel = stopCancel
	pm.mu.Unlock()
	if oldCancel != nil {
		oldCancel()
	}

	for {
		pm.mu.Lock()
		if pm.stopping {
			entry.handle.Status = ProcessStatusStopped
			pm.mu.Unlock()
			return
		}

		// Create a child context for this spawn cycle.
		spawnCtx, spawnCancel := context.WithCancel(stopCtx)
		entry.startedAt = time.Now()
		entry.handle.Status = ProcessStatusRunning
		entry.handle.StartedAt = entry.startedAt
		pm.mu.Unlock()

		err := entry.spawn(spawnCtx)
		spawnCancel()

		pm.mu.Lock()
		if pm.stopping {
			entry.handle.Status = ProcessStatusStopped
			pm.mu.Unlock()
			return
		}

		if entry.cfg.OnExit != nil {
			entry.cfg.OnExit(err)
		}

		// Check if the process ran long enough to be considered healthy.
		runDuration := time.Since(entry.startedAt)
		if runDuration >= healthyRunDuration {
			entry.backoff = initialBackoff
			entry.restarts = 0
		}

		entry.restarts++
		entry.handle.Restarts = entry.restarts
		if err != nil {
			entry.handle.LastError = err.Error()
		}

		// Check restart limit.
		maxRestarts := entry.cfg.MaxRestarts
		if maxRestarts == 0 {
			maxRestarts = defaultMaxRestarts
		}
		if maxRestarts > 0 && entry.restarts >= maxRestarts {
			entry.handle.Status = ProcessStatusFailed
			log.Error("process exceeded max restarts", "name", entry.cfg.Name, "restarts", entry.restarts)
			pm.mu.Unlock()
			return
		}

		// Backoff before restart.
		delay := entry.backoff
		entry.handle.Status = ProcessStatusBackoff
		entry.handle.BackoffNext = delay
		entry.backoff *= backoffMultiplier
		if entry.backoff > maxBackoff {
			entry.backoff = maxBackoff
		}
		pm.mu.Unlock()

		log.Warn("process exited, restarting after backoff", "name", entry.cfg.Name, "delay", delay, "error", err)

		select {
		case <-time.After(delay):
		case <-stopCtx.Done():
			pm.mu.Lock()
			entry.handle.Status = ProcessStatusStopped
			pm.mu.Unlock()
			return
		}
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// ResolveCommand resolves a plugin command relative to its plugin directory.
// If the command is already absolute, it is returned as-is.
func ResolveCommand(pluginDir, command string) string {
	if filepath.IsAbs(command) {
		return command
	}
	return filepath.Join(pluginDir, command)
}

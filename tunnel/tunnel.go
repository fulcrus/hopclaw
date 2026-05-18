package tunnel

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"sync"
	"time"
)

// ---------------------------------------------------------------------------
// Constants
// ---------------------------------------------------------------------------

const (
	defaultSSHPort       = 22
	defaultLocalPort     = 0 // auto-assign
	defaultRetryInterval = 5 * time.Second
	defaultMaxRetries    = 10
	sshConnectTimeout    = 30 // seconds
)

// ---------------------------------------------------------------------------
// Sentinel errors
// ---------------------------------------------------------------------------

var (
	ErrSSHNotAvailable = errors.New("ssh client not available")
	ErrTunnelRunning   = errors.New("tunnel is already running")
	ErrTunnelStopped   = errors.New("tunnel is not running")
)

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

// Config defines an SSH tunnel forwarding configuration.
type Config struct {
	// SSH connection
	Host    string `json:"host" yaml:"host"`
	Port    int    `json:"port,omitempty" yaml:"port"`
	User    string `json:"user,omitempty" yaml:"user"`
	KeyFile string `json:"key_file,omitempty" yaml:"key_file"`

	// Port forwarding: LocalPort -> RemoteHost:RemotePort
	LocalPort  int    `json:"local_port,omitempty" yaml:"local_port"`
	RemoteHost string `json:"remote_host" yaml:"remote_host"`
	RemotePort int    `json:"remote_port" yaml:"remote_port"`

	// Behavior
	AutoReconnect bool `json:"auto_reconnect,omitempty" yaml:"auto_reconnect"`
	MaxRetries    int  `json:"max_retries,omitempty" yaml:"max_retries"`
}

// Status represents the current tunnel status.
type Status struct {
	Running    bool      `json:"running"`
	LocalPort  int       `json:"local_port"`
	RemoteAddr string    `json:"remote_addr"`
	StartedAt  time.Time `json:"started_at,omitempty"`
	Retries    int       `json:"retries"`
	Error      string    `json:"error,omitempty"`
}

// Tunnel manages a single SSH port-forwarding session.
type Tunnel struct {
	mu      sync.Mutex // guards cmd, cancel, status
	config  Config
	cmd     *exec.Cmd
	cancel  context.CancelFunc
	status  Status
	sshPath string
}

// ---------------------------------------------------------------------------
// Constructor
// ---------------------------------------------------------------------------

// New validates the config and locates the ssh binary, returning a ready
// Tunnel instance that has not yet started.
func New(cfg Config) (*Tunnel, error) {
	if cfg.Host == "" {
		return nil, fmt.Errorf("tunnel: host is required")
	}
	if cfg.RemoteHost == "" {
		return nil, fmt.Errorf("tunnel: remote host is required")
	}
	if cfg.RemotePort <= 0 {
		return nil, fmt.Errorf("tunnel: remote port must be positive")
	}

	// Apply defaults.
	if cfg.Port <= 0 {
		cfg.Port = defaultSSHPort
	}
	if cfg.LocalPort < 0 {
		cfg.LocalPort = defaultLocalPort
	}
	if cfg.MaxRetries <= 0 {
		cfg.MaxRetries = defaultMaxRetries
	}

	sshPath, err := exec.LookPath("ssh")
	if err != nil {
		return nil, fmt.Errorf("tunnel: %w", ErrSSHNotAvailable)
	}

	return &Tunnel{
		config:  cfg,
		sshPath: sshPath,
		status: Status{
			LocalPort:  cfg.LocalPort,
			RemoteAddr: fmt.Sprintf("%s:%d", cfg.RemoteHost, cfg.RemotePort),
		},
	}, nil
}

// ---------------------------------------------------------------------------
// Public methods
// ---------------------------------------------------------------------------

// Start launches the SSH tunnel subprocess. If AutoReconnect is enabled a
// background goroutine will restart the process on failure up to MaxRetries.
func (t *Tunnel) Start(ctx context.Context) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.status.Running {
		return ErrTunnelRunning
	}

	return t.startLocked(ctx)
}

// Stop terminates the running SSH process.
func (t *Tunnel) Stop() error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if !t.status.Running {
		return ErrTunnelStopped
	}

	return t.stopLocked()
}

// GetStatus returns a snapshot of the current tunnel status.
func (t *Tunnel) GetStatus() Status {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.status
}

// IsRunning reports whether the tunnel subprocess is active.
func (t *Tunnel) IsRunning() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.status.Running
}

// Wait blocks until the tunnel process exits and returns its error.
func (t *Tunnel) Wait() error {
	t.mu.Lock()
	cmd := t.cmd
	t.mu.Unlock()

	if cmd == nil {
		return ErrTunnelStopped
	}
	return cmd.Wait()
}

// ---------------------------------------------------------------------------
// Argument builder (exported for testing)
// ---------------------------------------------------------------------------

// buildArgs constructs the ssh command-line arguments from the current config.
func (t *Tunnel) buildArgs() []string {
	cfg := t.config

	localPort := strconv.Itoa(cfg.LocalPort)
	forward := localPort + ":" + cfg.RemoteHost + ":" + strconv.Itoa(cfg.RemotePort)

	args := []string{
		"-N",
		"-L", forward,
	}

	if cfg.User != "" {
		args = append(args, "-l", cfg.User)
	}

	args = append(args, "-p", strconv.Itoa(cfg.Port))

	if cfg.KeyFile != "" {
		args = append(args, "-i", cfg.KeyFile)
	}

	args = append(args,
		"-o", "StrictHostKeyChecking=no",
		"-o", "ConnectTimeout="+strconv.Itoa(sshConnectTimeout),
	)

	args = append(args, cfg.Host)

	return args
}

// ---------------------------------------------------------------------------
// Internal helpers
// ---------------------------------------------------------------------------

// startLocked launches the SSH process. Caller must hold t.mu.
func (t *Tunnel) startLocked(ctx context.Context) error {
	tunnelCtx, cancel := context.WithCancel(ctx)
	t.cancel = cancel

	args := t.buildArgs()
	cmd := exec.CommandContext(tunnelCtx, t.sshPath, args...)
	t.cmd = cmd

	if err := cmd.Start(); err != nil {
		cancel()
		return fmt.Errorf("tunnel: failed to start ssh: %w", err)
	}

	t.status.Running = true
	t.status.StartedAt = time.Now().UTC()
	t.status.Error = ""

	// Monitor the process in the background.
	go t.monitor(tunnelCtx)

	return nil
}

// stopLocked kills the SSH process. Caller must hold t.mu.
func (t *Tunnel) stopLocked() error {
	if t.cancel != nil {
		t.cancel()
	}
	if t.cmd != nil && t.cmd.Process != nil {
		if err := t.cmd.Process.Kill(); err != nil {
			return fmt.Errorf("tunnel: failed to kill ssh process: %w", err)
		}
	}
	t.status.Running = false
	return nil
}

// monitor waits for the SSH process to exit and optionally reconnects.
func (t *Tunnel) monitor(ctx context.Context) {
	err := t.cmd.Wait()

	t.mu.Lock()
	t.status.Running = false
	if err != nil {
		t.status.Error = err.Error()
	}
	shouldReconnect := t.config.AutoReconnect && t.status.Retries < t.config.MaxRetries
	t.mu.Unlock()

	if !shouldReconnect {
		return
	}

	// Context may already be cancelled (Stop was called).
	select {
	case <-ctx.Done():
		return
	case <-time.After(defaultRetryInterval):
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	// Re-check after sleeping — Stop may have been called.
	select {
	case <-ctx.Done():
		return
	default:
	}

	t.status.Retries++
	if err := t.startLocked(ctx); err != nil {
		t.status.Error = err.Error()
	}
}

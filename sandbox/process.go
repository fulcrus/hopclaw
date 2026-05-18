package sandbox

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/fulcrus/hopclaw/logging"
)

var log = logging.WithSubsystem("sandbox")

// ---------------------------------------------------------------------------
// Defaults
// ---------------------------------------------------------------------------

const (
	processDefaultTimeout     = 30       // seconds
	processDefaultMaxOutput   = 1 << 20  // 1 MiB
	processDefaultMaxFileSize = 10 << 20 // 10 MiB
	processDefaultMaxProcs    = 64
)

// ---------------------------------------------------------------------------
// Sentinel errors
// ---------------------------------------------------------------------------

var (
	ErrProcessEmptyCommand = errors.New("command must not be empty")
)

// ---------------------------------------------------------------------------
// ProcessConfig
// ---------------------------------------------------------------------------

// ProcessConfig controls process-level sandbox behavior. Zero-value fields
// are replaced with defaults in NewProcessRunner.
type ProcessConfig struct {
	WorkDir     string            `json:"work_dir" yaml:"work_dir"`           // base temp dir (default: os.TempDir())
	Timeout     int               `json:"timeout" yaml:"timeout"`             // seconds, default 30
	MaxOutput   int               `json:"max_output" yaml:"max_output"`       // bytes, default 1 MiB
	Env         map[string]string `json:"env,omitempty" yaml:"env"`           // allowed env vars
	AllowNet    bool              `json:"allow_net" yaml:"allow_net"`         // whether to allow network (default false)
	MaxFileSize int64             `json:"max_file_size" yaml:"max_file_size"` // max file size in bytes (ulimit -f)
	MaxProcs    int               `json:"max_procs" yaml:"max_procs"`         // max child processes (ulimit -u)
}

// applyDefaults fills zero-value fields with sensible defaults.
func (c *ProcessConfig) applyDefaults() {
	if c.WorkDir == "" {
		c.WorkDir = os.TempDir()
	}
	if c.Timeout <= 0 {
		c.Timeout = processDefaultTimeout
	}
	if c.MaxOutput <= 0 {
		c.MaxOutput = processDefaultMaxOutput
	}
	if c.MaxFileSize <= 0 {
		c.MaxFileSize = processDefaultMaxFileSize
	}
	if c.MaxProcs <= 0 {
		c.MaxProcs = processDefaultMaxProcs
	}
}

// ---------------------------------------------------------------------------
// ProcessRunner
// ---------------------------------------------------------------------------

// ProcessRunner executes commands in a restricted subprocess without Docker.
// It uses OS-level restrictions: temp work dirs, env sanitization, timeouts,
// output limits, and resource bounds via ulimit (Unix) or job objects (Windows).
type ProcessRunner struct {
	config ProcessConfig
}

// NewProcessRunner creates a ProcessRunner with the given configuration.
// Zero-value config fields are replaced with sensible defaults.
func NewProcessRunner(cfg ProcessConfig) *ProcessRunner {
	cfg.applyDefaults()
	return &ProcessRunner{
		config: cfg,
	}
}

// Exec runs a command inside a process-level sandbox and returns the captured
// output. It creates an isolated temp directory for each execution, sanitizes
// the environment, sets resource limits on supported platforms, and enforces
// a timeout via context.
func (r *ProcessRunner) Exec(ctx context.Context, req ExecRequest) (*ExecResult, error) {
	if len(req.Command) == 0 {
		return nil, ErrProcessEmptyCommand
	}

	// Determine timeout: per-request overrides config.
	timeout := time.Duration(r.config.Timeout) * time.Second
	if req.Timeout > 0 {
		timeout = time.Duration(req.Timeout) * time.Second
	}
	execCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// Create an isolated temp directory for this execution.
	execDir, err := os.MkdirTemp(r.config.WorkDir, "sandbox-*")
	if err != nil {
		return nil, fmt.Errorf("failed to create temp dir: %w", err)
	}
	defer os.RemoveAll(execDir)

	// Resolve command path.
	cmdPath, err := resolveCommand(req.Command[0])
	if err != nil {
		return nil, fmt.Errorf("failed to resolve command %q: %w", req.Command[0], err)
	}

	cmd := exec.CommandContext(execCtx, cmdPath, req.Command[1:]...)
	cmd.Dir = execDir

	// Build sanitized environment.
	cmd.Env = buildProcessEnv(r.config.Env, req.Env)

	// Apply OS-level resource restrictions.
	cmd.SysProcAttr = buildSysProcAttr(r.config)

	if req.Stdin != "" {
		cmd.Stdin = strings.NewReader(req.Stdin)
	}

	// Capture stdout/stderr with output limiting.
	var stdout, stderr bytes.Buffer
	stdoutWriter := &limitedWriter{buf: &stdout, limit: r.config.MaxOutput}
	stderrWriter := &limitedWriter{buf: &stderr, limit: r.config.MaxOutput}
	cmd.Stdout = stdoutWriter
	cmd.Stderr = stderrWriter

	start := time.Now()
	runErr := cmd.Run()
	duration := time.Since(start)

	result := &ExecResult{
		Stdout:    stdout.String(),
		Stderr:    stderr.String(),
		Duration:  duration,
		Truncated: stdoutWriter.Truncated() || stderrWriter.Truncated(),
	}

	// Check for timeout.
	if execCtx.Err() != nil && errors.Is(execCtx.Err(), context.DeadlineExceeded) {
		result.TimedOut = true
		result.ExitCode = -1
		return result, nil
	}

	if runErr != nil {
		var exitErr *exec.ExitError
		if errors.As(runErr, &exitErr) {
			result.ExitCode = exitErr.ExitCode()
			return result, nil
		}
		return nil, fmt.Errorf("process execution failed: %w", runErr)
	}

	result.ExitCode = 0
	return result, nil
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// resolveCommand resolves a command name to its absolute path. If the name
// already contains a path separator it is cleaned and returned as-is.
func resolveCommand(name string) (string, error) {
	if filepath.Base(name) != name {
		return filepath.Clean(name), nil
	}
	return exec.LookPath(name)
}

// buildProcessEnv constructs a minimal environment for the sandboxed process.
// It merges config-level and request-level env vars (request wins on conflict),
// sanitizes dangerous keys, and adds a safe PATH.
func buildProcessEnv(configEnv, reqEnv map[string]string) []string {
	merged := make(map[string]string, len(configEnv)+len(reqEnv))
	for k, v := range configEnv {
		merged[k] = v
	}
	for k, v := range reqEnv {
		merged[k] = v
	}

	sanitized := SanitizeEnv(merged)

	// Build the env slice. Always provide a minimal PATH so basic commands
	// are discoverable.
	var env []string
	pathSet := false
	for k, v := range sanitized {
		env = append(env, k+"="+v)
		if strings.ToUpper(k) == "PATH" {
			pathSet = true
		}
	}
	if !pathSet {
		env = append(env, "PATH=/usr/local/bin:/usr/bin:/bin")
	}
	return env
}

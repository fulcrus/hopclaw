package sandbox

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// ---------------------------------------------------------------------------
// Sentinel errors
// ---------------------------------------------------------------------------

var (
	ErrDockerNotAvailable = errors.New("docker CLI is not available")
	ErrEmptyCommand       = errors.New("command must not be empty")
	ErrImageNotAllowed    = errors.New("image is not in the allowed list")
)

// ---------------------------------------------------------------------------
// Output limits
// ---------------------------------------------------------------------------

const (
	// maxOutputBytes caps stdout and stderr capture to prevent memory
	// exhaustion from chatty processes inside the sandbox.
	maxOutputBytes = 1 << 20 // 1 MiB
)

// ---------------------------------------------------------------------------
// Runner
// ---------------------------------------------------------------------------

// Runner executes untrusted commands inside ephemeral Docker containers.
type Runner struct {
	config     Config
	dockerPath string // absolute path to docker binary
}

// NewRunner creates a Runner after verifying that the Docker CLI is reachable.
// Returns an error if Docker is not installed or not responding.
func NewRunner(cfg Config) (*Runner, error) {
	cfg.applyDefaults()

	dockerPath, err := exec.LookPath("docker")
	if err != nil {
		return nil, fmt.Errorf("%w: %s", ErrDockerNotAvailable, err)
	}

	// Quick health-check: `docker version` must succeed.
	out, err := exec.Command(dockerPath, "version", "--format", "{{.Client.Version}}").CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("%w: docker version failed: %s", ErrDockerNotAvailable, strings.TrimSpace(string(out)))
	}

	return &Runner{
		config:     cfg,
		dockerPath: dockerPath,
	}, nil
}

// IsAvailable returns true when the Docker CLI is accessible.
func (r *Runner) IsAvailable() bool {
	if r == nil || r.dockerPath == "" {
		return false
	}
	err := exec.Command(r.dockerPath, "version", "--format", "{{.Client.Version}}").Run()
	return err == nil
}

// Exec runs a command inside a sandboxed Docker container and returns the
// captured output. The context controls overall cancellation; an additional
// per-request timeout is layered on top (from req.Timeout or cfg.Timeout).
func (r *Runner) Exec(ctx context.Context, req ExecRequest) (*ExecResult, error) {
	if r == nil {
		return nil, ErrDockerNotAvailable
	}
	if err := r.validate(req); err != nil {
		return nil, err
	}

	timeout := time.Duration(r.config.Timeout) * time.Second
	if req.Timeout > 0 {
		timeout = time.Duration(req.Timeout) * time.Second
	}
	execCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	dockerArgs := BuildDockerArgs(r.config, req)
	cmd := exec.CommandContext(execCtx, r.dockerPath, dockerArgs...)

	if req.Stdin != "" {
		cmd.Stdin = strings.NewReader(req.Stdin)
	}

	var stdout, stderr bytes.Buffer
	stdoutWriter := &limitedWriter{buf: &stdout, limit: maxOutputBytes}
	stderrWriter := &limitedWriter{buf: &stderr, limit: maxOutputBytes}
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
		return nil, fmt.Errorf("docker run failed: %w", runErr)
	}

	result.ExitCode = 0
	return result, nil
}

// PullImage pulls a container image so that subsequent Exec calls do not
// incur the download latency.
func (r *Runner) PullImage(ctx context.Context, image string) error {
	if r == nil {
		return ErrDockerNotAvailable
	}
	image = strings.TrimSpace(image)
	if image == "" {
		return fmt.Errorf("image name is required")
	}
	if err := ValidateImage(image, r.config.AllowedImages); err != nil {
		return fmt.Errorf("%w: %s", ErrImageNotAllowed, err)
	}

	cmd := exec.CommandContext(ctx, r.dockerPath, "pull", image)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker pull %s failed: %s", image, strings.TrimSpace(string(out)))
	}
	return nil
}

// validate checks that the ExecRequest is well-formed and the image is allowed.
func (r *Runner) validate(req ExecRequest) error {
	if len(req.Command) == 0 {
		return ErrEmptyCommand
	}
	image := strings.TrimSpace(req.Image)
	if image == "" {
		image = r.config.Image
	}
	if err := ValidateImage(image, r.config.AllowedImages); err != nil {
		return fmt.Errorf("%w: %s", ErrImageNotAllowed, err)
	}
	return nil
}

// ---------------------------------------------------------------------------
// limitedWriter
// ---------------------------------------------------------------------------

// limitedWriter wraps a bytes.Buffer and silently discards writes once the
// limit is reached. This prevents a runaway process from exhausting memory.
type limitedWriter struct {
	buf       *bytes.Buffer
	limit     int
	truncated bool
}

func (w *limitedWriter) Write(p []byte) (int, error) {
	remaining := w.limit - w.buf.Len()
	if remaining <= 0 {
		if len(p) > 0 {
			w.truncated = true
		}
		return len(p), nil // discard
	}
	toWrite := p
	if len(p) > remaining {
		toWrite = p[:remaining]
		w.truncated = true
	}
	if _, err := w.buf.Write(toWrite); err != nil {
		return 0, err
	}
	return len(p), nil
}

func (w *limitedWriter) Truncated() bool {
	if w == nil {
		return false
	}
	return w.truncated
}

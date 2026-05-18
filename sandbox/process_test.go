package sandbox

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// Basic execution
// ---------------------------------------------------------------------------

func TestProcessExec_EchoCommand(t *testing.T) {
	r := NewProcessRunner(ProcessConfig{})
	result, err := r.Exec(context.Background(), ExecRequest{
		Command: []string{"echo", "hello sandbox"},
	})
	if err != nil {
		t.Fatalf("exec failed: %v", err)
	}
	if result.ExitCode != 0 {
		t.Fatalf("expected exit code 0, got %d (stderr: %s)", result.ExitCode, result.Stderr)
	}
	if !strings.Contains(result.Stdout, "hello sandbox") {
		t.Fatalf("expected stdout to contain 'hello sandbox', got %q", result.Stdout)
	}
	if result.TimedOut {
		t.Fatal("should not have timed out")
	}
	if result.Duration <= 0 {
		t.Fatal("expected positive duration")
	}
}

func TestProcessExec_MultiWordOutput(t *testing.T) {
	r := NewProcessRunner(ProcessConfig{})
	result, err := r.Exec(context.Background(), ExecRequest{
		Command: []string{"sh", "-c", "echo line1; echo line2; echo line3"},
	})
	if err != nil {
		t.Fatalf("exec failed: %v", err)
	}
	if result.ExitCode != 0 {
		t.Fatalf("expected exit code 0, got %d", result.ExitCode)
	}
	lines := strings.Split(strings.TrimSpace(result.Stdout), "\n")
	if len(lines) != 3 {
		t.Fatalf("expected 3 lines, got %d: %q", len(lines), result.Stdout)
	}
}

// ---------------------------------------------------------------------------
// Empty command
// ---------------------------------------------------------------------------

func TestProcessExec_EmptyCommand(t *testing.T) {
	r := NewProcessRunner(ProcessConfig{})
	_, err := r.Exec(context.Background(), ExecRequest{
		Command: nil,
	})
	if err != ErrProcessEmptyCommand {
		t.Fatalf("expected ErrProcessEmptyCommand, got: %v", err)
	}
}

func TestProcessExec_EmptyCommandSlice(t *testing.T) {
	r := NewProcessRunner(ProcessConfig{})
	_, err := r.Exec(context.Background(), ExecRequest{
		Command: []string{},
	})
	if err != ErrProcessEmptyCommand {
		t.Fatalf("expected ErrProcessEmptyCommand, got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Timeout enforcement
// ---------------------------------------------------------------------------

func TestProcessExec_Timeout(t *testing.T) {
	const timeoutSeconds = 1

	r := NewProcessRunner(ProcessConfig{
		Timeout: timeoutSeconds,
	})

	start := time.Now()
	result, err := r.Exec(context.Background(), ExecRequest{
		Command: []string{"sleep", "30"},
	})
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("exec should not return error on timeout: %v", err)
	}
	if !result.TimedOut {
		t.Fatal("expected timed out to be true")
	}
	if result.ExitCode != -1 {
		t.Fatalf("expected exit code -1 on timeout, got %d", result.ExitCode)
	}

	const maxExpectedDuration = 10 * time.Second
	if elapsed > maxExpectedDuration {
		t.Fatalf("timeout enforcement too slow: took %v", elapsed)
	}
}

func TestProcessExec_RequestTimeoutOverride(t *testing.T) {
	r := NewProcessRunner(ProcessConfig{
		Timeout: 30, // config timeout is generous
	})

	const requestTimeout = 1
	start := time.Now()
	result, err := r.Exec(context.Background(), ExecRequest{
		Command: []string{"sleep", "30"},
		Timeout: requestTimeout,
	})
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("exec should not return error on timeout: %v", err)
	}
	if !result.TimedOut {
		t.Fatal("expected timed out to be true")
	}

	const maxExpectedDuration = 10 * time.Second
	if elapsed > maxExpectedDuration {
		t.Fatalf("request timeout override not effective: took %v", elapsed)
	}
}

// ---------------------------------------------------------------------------
// Output limiting
// ---------------------------------------------------------------------------

func TestProcessExec_OutputLimiting(t *testing.T) {
	const maxOutput = 32 // very small limit for testing

	r := NewProcessRunner(ProcessConfig{
		MaxOutput: maxOutput,
	})

	// Generate output larger than the limit.
	result, err := r.Exec(context.Background(), ExecRequest{
		Command: []string{"sh", "-c", "dd if=/dev/zero bs=128 count=1 2>/dev/null | tr '\\0' 'A'"},
	})
	if err != nil {
		t.Fatalf("exec failed: %v", err)
	}
	if len(result.Stdout) > maxOutput {
		t.Fatalf("expected stdout to be at most %d bytes, got %d", maxOutput, len(result.Stdout))
	}
	if !result.Truncated {
		t.Fatal("expected truncated output to be reported")
	}
}

// ---------------------------------------------------------------------------
// Environment sanitization
// ---------------------------------------------------------------------------

func TestProcessExec_EnvSanitization(t *testing.T) {
	r := NewProcessRunner(ProcessConfig{})

	result, err := r.Exec(context.Background(), ExecRequest{
		Command: []string{"sh", "-c", "echo MY_VAR=$MY_VAR; echo AWS_KEY=$AWS_KEY"},
		Env: map[string]string{
			"MY_VAR":  "safe_value",
			"AWS_KEY": "should_be_stripped",
		},
	})
	if err != nil {
		t.Fatalf("exec failed: %v", err)
	}
	if result.ExitCode != 0 {
		t.Fatalf("expected exit code 0, got %d (stderr: %s)", result.ExitCode, result.Stderr)
	}
	if !strings.Contains(result.Stdout, "MY_VAR=safe_value") {
		t.Fatalf("expected MY_VAR=safe_value in output, got %q", result.Stdout)
	}
	if strings.Contains(result.Stdout, "should_be_stripped") {
		t.Fatalf("expected AWS_KEY to be sanitized, but found it in output: %q", result.Stdout)
	}
}

func TestProcessExec_ConfigEnvMerge(t *testing.T) {
	r := NewProcessRunner(ProcessConfig{
		Env: map[string]string{
			"CONFIG_VAR": "from_config",
		},
	})

	result, err := r.Exec(context.Background(), ExecRequest{
		Command: []string{"sh", "-c", "echo CONFIG_VAR=$CONFIG_VAR; echo REQ_VAR=$REQ_VAR"},
		Env: map[string]string{
			"REQ_VAR": "from_request",
		},
	})
	if err != nil {
		t.Fatalf("exec failed: %v", err)
	}
	if !strings.Contains(result.Stdout, "CONFIG_VAR=from_config") {
		t.Fatalf("expected CONFIG_VAR=from_config, got %q", result.Stdout)
	}
	if !strings.Contains(result.Stdout, "REQ_VAR=from_request") {
		t.Fatalf("expected REQ_VAR=from_request, got %q", result.Stdout)
	}
}

// ---------------------------------------------------------------------------
// Temp dir creation and cleanup
// ---------------------------------------------------------------------------

func TestProcessExec_TempDirCleanup(t *testing.T) {
	baseDir := t.TempDir()

	// Resolve symlinks so the comparison works on macOS where /var ->
	// /private/var.
	resolvedBase, err := filepath.EvalSymlinks(baseDir)
	if err != nil {
		t.Fatalf("failed to resolve base dir symlinks: %v", err)
	}

	r := NewProcessRunner(ProcessConfig{
		WorkDir: baseDir,
	})

	// Run a command that writes the working directory to stdout.
	result, err := r.Exec(context.Background(), ExecRequest{
		Command: []string{"pwd"},
	})
	if err != nil {
		t.Fatalf("exec failed: %v", err)
	}

	execDir := strings.TrimSpace(result.Stdout)
	if !strings.HasPrefix(execDir, resolvedBase) {
		t.Fatalf("expected exec dir under %q, got %q", resolvedBase, execDir)
	}

	// The temp dir should have been cleaned up after Exec returned.
	// Resolve the path from pwd output back through the original base to
	// locate the actual directory on disk.
	relativeSuffix := strings.TrimPrefix(execDir, resolvedBase)
	actualDir := filepath.Join(baseDir, relativeSuffix)
	if _, err := os.Stat(actualDir); !os.IsNotExist(err) {
		t.Fatalf("expected temp dir %q to be removed after exec, but it still exists", actualDir)
	}
}

func TestProcessExec_TempDirIsolation(t *testing.T) {
	baseDir := t.TempDir()
	r := NewProcessRunner(ProcessConfig{
		WorkDir: baseDir,
	})

	// Create a file in the base dir.
	sentinel := filepath.Join(baseDir, "sentinel.txt")
	if err := os.WriteFile(sentinel, []byte("secret"), 0o600); err != nil {
		t.Fatalf("failed to write sentinel file: %v", err)
	}

	// The sandboxed process should be working in a subdirectory, not the
	// base dir itself, so it should not see the sentinel file.
	result, err := r.Exec(context.Background(), ExecRequest{
		Command: []string{"ls"},
	})
	if err != nil {
		t.Fatalf("exec failed: %v", err)
	}
	if strings.Contains(result.Stdout, "sentinel.txt") {
		t.Fatal("sandboxed process should not see files in the base work dir")
	}
}

// ---------------------------------------------------------------------------
// Non-zero exit code
// ---------------------------------------------------------------------------

func TestProcessExec_NonZeroExitCode(t *testing.T) {
	r := NewProcessRunner(ProcessConfig{})
	result, err := r.Exec(context.Background(), ExecRequest{
		Command: []string{"sh", "-c", "exit 42"},
	})
	if err != nil {
		t.Fatalf("exec should not return error for non-zero exit: %v", err)
	}
	if result.ExitCode != 42 {
		t.Fatalf("expected exit code 42, got %d", result.ExitCode)
	}
}

// ---------------------------------------------------------------------------
// Stderr capture
// ---------------------------------------------------------------------------

func TestProcessExec_StderrCapture(t *testing.T) {
	r := NewProcessRunner(ProcessConfig{})
	result, err := r.Exec(context.Background(), ExecRequest{
		Command: []string{"sh", "-c", "echo error_output >&2"},
	})
	if err != nil {
		t.Fatalf("exec failed: %v", err)
	}
	if !strings.Contains(result.Stderr, "error_output") {
		t.Fatalf("expected stderr to contain 'error_output', got %q", result.Stderr)
	}
}

// ---------------------------------------------------------------------------
// Stdin forwarding
// ---------------------------------------------------------------------------

func TestProcessExec_StdinForwarding(t *testing.T) {
	r := NewProcessRunner(ProcessConfig{})
	result, err := r.Exec(context.Background(), ExecRequest{
		Command: []string{"cat"},
		Stdin:   "piped content",
	})
	if err != nil {
		t.Fatalf("exec failed: %v", err)
	}
	if result.ExitCode != 0 {
		t.Fatalf("expected exit code 0, got %d", result.ExitCode)
	}
	if !strings.Contains(result.Stdout, "piped content") {
		t.Fatalf("expected stdout to contain 'piped content', got %q", result.Stdout)
	}
}

// ---------------------------------------------------------------------------
// Context cancellation
// ---------------------------------------------------------------------------

func TestProcessExec_ContextCancellation(t *testing.T) {
	r := NewProcessRunner(ProcessConfig{
		Timeout: 30,
	})

	ctx, cancel := context.WithCancel(context.Background())

	// Cancel the context after a short delay.
	go func() {
		time.Sleep(500 * time.Millisecond)
		cancel()
	}()

	result, err := r.Exec(ctx, ExecRequest{
		Command: []string{"sleep", "30"},
	})
	// Context cancellation may surface as an error or a timed-out result,
	// depending on timing. Either is acceptable.
	if err != nil {
		return // context cancellation surfaced as error — acceptable
	}
	if result.ExitCode == 0 {
		t.Fatal("expected non-zero exit or timeout after context cancellation")
	}
}

// ---------------------------------------------------------------------------
// Config defaults
// ---------------------------------------------------------------------------

func TestProcessConfigApplyDefaults(t *testing.T) {
	var cfg ProcessConfig
	cfg.applyDefaults()

	if cfg.WorkDir == "" {
		t.Error("expected non-empty default work dir")
	}
	if cfg.Timeout != processDefaultTimeout {
		t.Errorf("expected default timeout %d, got %d", processDefaultTimeout, cfg.Timeout)
	}
	if cfg.MaxOutput != processDefaultMaxOutput {
		t.Errorf("expected default max output %d, got %d", processDefaultMaxOutput, cfg.MaxOutput)
	}
	if cfg.MaxFileSize != processDefaultMaxFileSize {
		t.Errorf("expected default max file size %d, got %d", processDefaultMaxFileSize, cfg.MaxFileSize)
	}
	if cfg.MaxProcs != processDefaultMaxProcs {
		t.Errorf("expected default max procs %d, got %d", processDefaultMaxProcs, cfg.MaxProcs)
	}
}

func TestProcessConfigApplyDefaults_PreservesExisting(t *testing.T) {
	cfg := ProcessConfig{
		WorkDir:     "/custom",
		Timeout:     60,
		MaxOutput:   2048,
		MaxFileSize: 5 << 20,
		MaxProcs:    32,
	}
	cfg.applyDefaults()

	if cfg.WorkDir != "/custom" {
		t.Errorf("expected preserved work dir, got %q", cfg.WorkDir)
	}
	if cfg.Timeout != 60 {
		t.Errorf("expected preserved timeout, got %d", cfg.Timeout)
	}
	if cfg.MaxOutput != 2048 {
		t.Errorf("expected preserved max output, got %d", cfg.MaxOutput)
	}
	if cfg.MaxFileSize != 5<<20 {
		t.Errorf("expected preserved max file size, got %d", cfg.MaxFileSize)
	}
	if cfg.MaxProcs != 32 {
		t.Errorf("expected preserved max procs, got %d", cfg.MaxProcs)
	}
}

// ---------------------------------------------------------------------------
// Invalid work dir
// ---------------------------------------------------------------------------

func TestProcessExec_InvalidWorkDir(t *testing.T) {
	r := NewProcessRunner(ProcessConfig{
		WorkDir: "/nonexistent/path/that/does/not/exist",
	})

	_, err := r.Exec(context.Background(), ExecRequest{
		Command: []string{"echo", "hello"},
	})
	if err == nil {
		t.Fatal("expected error for invalid work dir")
	}
	if !strings.Contains(err.Error(), "failed to create temp dir") {
		t.Fatalf("expected 'failed to create temp dir' error, got: %v", err)
	}
}

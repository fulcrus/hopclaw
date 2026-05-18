package sandbox

import (
	"bytes"
	"context"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// IsAvailable
// ---------------------------------------------------------------------------

func TestIsAvailable_NilRunner(t *testing.T) {
	var r *Runner
	if r.IsAvailable() {
		t.Fatal("nil runner should not be available")
	}
}

func TestIsAvailable_EmptyDockerPath(t *testing.T) {
	r := &Runner{config: Config{}, dockerPath: ""}
	if r.IsAvailable() {
		t.Fatal("runner with empty docker path should not be available")
	}
}

// ---------------------------------------------------------------------------
// Validation — empty command
// ---------------------------------------------------------------------------

func TestExec_EmptyCommand(t *testing.T) {
	r := &Runner{config: Config{}, dockerPath: "/usr/bin/docker"}
	r.config.applyDefaults()

	_, err := r.Exec(context.Background(), ExecRequest{
		Command: nil,
	})
	if err == nil {
		t.Fatal("expected error for empty command")
	}
	if err != ErrEmptyCommand {
		t.Fatalf("expected ErrEmptyCommand, got: %v", err)
	}
}

func TestExec_EmptyCommandSlice(t *testing.T) {
	r := &Runner{config: Config{}, dockerPath: "/usr/bin/docker"}
	r.config.applyDefaults()

	_, err := r.Exec(context.Background(), ExecRequest{
		Command: []string{},
	})
	if err == nil {
		t.Fatal("expected error for empty command slice")
	}
	if err != ErrEmptyCommand {
		t.Fatalf("expected ErrEmptyCommand, got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Validation — disallowed image
// ---------------------------------------------------------------------------

func TestExec_DisallowedImage(t *testing.T) {
	r := &Runner{
		config: Config{
			AllowedImages: []string{"python:3.12-slim", "node:20-slim"},
		},
		dockerPath: "/usr/bin/docker",
	}
	r.config.applyDefaults()

	_, err := r.Exec(context.Background(), ExecRequest{
		Image:   "ubuntu:latest",
		Command: []string{"echo", "hello"},
	})
	if err == nil {
		t.Fatal("expected error for disallowed image")
	}
	if !strings.Contains(err.Error(), "not in the allowed list") {
		t.Fatalf("expected 'not in the allowed list' error, got: %v", err)
	}
}

func TestExec_AllowedImage(t *testing.T) {
	// This test only validates that the allowed image passes validation.
	// It does not run Docker.
	r := &Runner{
		config: Config{
			AllowedImages: []string{"python:3.12-slim", "node:20-slim"},
		},
		dockerPath: "/nonexistent/docker",
	}
	r.config.applyDefaults()

	// The validate step should pass; execution will fail because dockerPath
	// does not exist, but we only care about the validation path here.
	err := r.validate(ExecRequest{
		Image:   "python:3.12-slim",
		Command: []string{"python", "-c", "print('hi')"},
	})
	if err != nil {
		t.Fatalf("expected allowed image to pass validation, got: %v", err)
	}
}

func TestExec_EmptyAllowedList(t *testing.T) {
	r := &Runner{
		config:     Config{},
		dockerPath: "/nonexistent/docker",
	}
	r.config.applyDefaults()

	err := r.validate(ExecRequest{
		Image:   "anything:latest",
		Command: []string{"echo", "hi"},
	})
	if err != nil {
		t.Fatalf("empty allowed list should permit any image, got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Exec on nil runner
// ---------------------------------------------------------------------------

func TestExec_NilRunner(t *testing.T) {
	var r *Runner
	_, err := r.Exec(context.Background(), ExecRequest{
		Command: []string{"echo", "hello"},
	})
	if err != ErrDockerNotAvailable {
		t.Fatalf("expected ErrDockerNotAvailable, got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// PullImage — validation
// ---------------------------------------------------------------------------

func TestPullImage_NilRunner(t *testing.T) {
	var r *Runner
	err := r.PullImage(context.Background(), "python:3.12-slim")
	if err != ErrDockerNotAvailable {
		t.Fatalf("expected ErrDockerNotAvailable, got: %v", err)
	}
}

func TestPullImage_EmptyImage(t *testing.T) {
	r := &Runner{config: Config{}, dockerPath: "/usr/bin/docker"}
	r.config.applyDefaults()

	err := r.PullImage(context.Background(), "")
	if err == nil {
		t.Fatal("expected error for empty image name")
	}
}

func TestPullImage_DisallowedImage(t *testing.T) {
	r := &Runner{
		config: Config{
			AllowedImages: []string{"python:3.12-slim"},
		},
		dockerPath: "/usr/bin/docker",
	}
	r.config.applyDefaults()

	err := r.PullImage(context.Background(), "malicious:latest")
	if err == nil {
		t.Fatal("expected error for disallowed image")
	}
	if !strings.Contains(err.Error(), "not in the allowed list") {
		t.Fatalf("expected 'not in the allowed list' error, got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Config defaults
// ---------------------------------------------------------------------------

func TestConfigApplyDefaults(t *testing.T) {
	var cfg Config
	cfg.applyDefaults()

	if cfg.Image != defaultImage {
		t.Errorf("expected default image %q, got %q", defaultImage, cfg.Image)
	}
	if cfg.MemoryLimit != defaultMemoryLimit {
		t.Errorf("expected default memory limit %q, got %q", defaultMemoryLimit, cfg.MemoryLimit)
	}
	if cfg.CPULimit != defaultCPULimit {
		t.Errorf("expected default CPU limit %q, got %q", defaultCPULimit, cfg.CPULimit)
	}
	if cfg.Timeout != defaultTimeout {
		t.Errorf("expected default timeout %d, got %d", defaultTimeout, cfg.Timeout)
	}
	if cfg.NetworkMode != defaultNetworkMode {
		t.Errorf("expected default network mode %q, got %q", defaultNetworkMode, cfg.NetworkMode)
	}
	if cfg.WorkDir != defaultWorkDir {
		t.Errorf("expected default work dir %q, got %q", defaultWorkDir, cfg.WorkDir)
	}
}

func TestConfigApplyDefaults_PreservesExisting(t *testing.T) {
	cfg := Config{
		Image:       "node:20-slim",
		MemoryLimit: "512m",
		CPULimit:    "2.0",
		Timeout:     60,
		NetworkMode: "bridge",
		WorkDir:     "/app",
	}
	cfg.applyDefaults()

	if cfg.Image != "node:20-slim" {
		t.Errorf("expected preserved image, got %q", cfg.Image)
	}
	if cfg.MemoryLimit != "512m" {
		t.Errorf("expected preserved memory limit, got %q", cfg.MemoryLimit)
	}
	if cfg.CPULimit != "2.0" {
		t.Errorf("expected preserved CPU limit, got %q", cfg.CPULimit)
	}
	if cfg.Timeout != 60 {
		t.Errorf("expected preserved timeout, got %d", cfg.Timeout)
	}
	if cfg.NetworkMode != "bridge" {
		t.Errorf("expected preserved network mode, got %q", cfg.NetworkMode)
	}
	if cfg.WorkDir != "/app" {
		t.Errorf("expected preserved work dir, got %q", cfg.WorkDir)
	}
}

// ---------------------------------------------------------------------------
// limitedWriter
// ---------------------------------------------------------------------------

func TestLimitedWriter(t *testing.T) {
	const limit = 10

	t.Run("within limit", func(t *testing.T) {
		var buf bytes.Buffer
		w := &limitedWriter{buf: &buf, limit: limit}
		n, err := w.Write([]byte("hello"))
		if err != nil {
			t.Fatal(err)
		}
		if n != 5 {
			t.Fatalf("expected 5 bytes written, got %d", n)
		}
		if buf.String() != "hello" {
			t.Fatalf("expected 'hello', got %q", buf.String())
		}
		if w.Truncated() {
			t.Fatal("writer should not report truncation within limit")
		}
	})

	t.Run("exceeds limit", func(t *testing.T) {
		var buf bytes.Buffer
		w := &limitedWriter{buf: &buf, limit: limit}
		// Write 7 bytes, then 7 more (only 3 should be kept from second write).
		w.Write([]byte("1234567"))
		n, err := w.Write([]byte("abcdefg"))
		if err != nil {
			t.Fatal(err)
		}
		if n != 7 {
			t.Fatalf("expected reported 7 bytes, got %d", n)
		}
		if buf.String() != "1234567abc" {
			t.Fatalf("expected '1234567abc', got %q", buf.String())
		}
		if !w.Truncated() {
			t.Fatal("writer should report truncation when limit is exceeded")
		}
	})

	t.Run("at limit", func(t *testing.T) {
		var buf bytes.Buffer
		w := &limitedWriter{buf: &buf, limit: limit}
		w.Write([]byte("1234567890"))
		n, err := w.Write([]byte("more"))
		if err != nil {
			t.Fatal(err)
		}
		if n != 4 {
			t.Fatalf("expected reported 4 bytes, got %d", n)
		}
		if buf.String() != "1234567890" {
			t.Fatalf("expected '1234567890', got %q", buf.String())
		}
		if !w.Truncated() {
			t.Fatal("writer should report truncation after writes beyond the limit")
		}
	})
}

// ---------------------------------------------------------------------------
// Integration tests — require Docker to be installed
// ---------------------------------------------------------------------------

func dockerAvailable() bool {
	_, err := exec.LookPath("docker")
	if err != nil {
		return false
	}
	return exec.Command("docker", "version").Run() == nil
}

func TestIntegration_ExecEchoCommand(t *testing.T) {
	if !dockerAvailable() {
		t.Skip("docker is not available")
	}

	r, err := NewRunner(Config{
		Image:   "alpine:latest",
		Timeout: 30,
	})
	if err != nil {
		t.Fatalf("failed to create runner: %v", err)
	}

	result, err := r.Exec(context.Background(), ExecRequest{
		Image:   "alpine:latest",
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
}

func TestIntegration_ExecWithStdin(t *testing.T) {
	if !dockerAvailable() {
		t.Skip("docker is not available")
	}

	r, err := NewRunner(Config{
		Image:   "alpine:latest",
		Timeout: 30,
	})
	if err != nil {
		t.Fatalf("failed to create runner: %v", err)
	}

	result, err := r.Exec(context.Background(), ExecRequest{
		Image:   "alpine:latest",
		Command: []string{"cat"},
		Stdin:   "piped input",
	})
	if err != nil {
		t.Fatalf("exec failed: %v", err)
	}
	if result.ExitCode != 0 {
		t.Fatalf("expected exit code 0, got %d", result.ExitCode)
	}
	if !strings.Contains(result.Stdout, "piped input") {
		t.Fatalf("expected stdout to contain 'piped input', got %q", result.Stdout)
	}
}

func TestIntegration_ExecNonZeroExit(t *testing.T) {
	if !dockerAvailable() {
		t.Skip("docker is not available")
	}

	r, err := NewRunner(Config{
		Image:   "alpine:latest",
		Timeout: 30,
	})
	if err != nil {
		t.Fatalf("failed to create runner: %v", err)
	}

	result, err := r.Exec(context.Background(), ExecRequest{
		Image:   "alpine:latest",
		Command: []string{"sh", "-c", "exit 42"},
	})
	if err != nil {
		t.Fatalf("exec should not return error for non-zero exit: %v", err)
	}
	if result.ExitCode != 42 {
		t.Fatalf("expected exit code 42, got %d", result.ExitCode)
	}
}

func TestIntegration_ExecTimeout(t *testing.T) {
	if !dockerAvailable() {
		t.Skip("docker is not available")
	}

	const timeoutSeconds = 3

	r, err := NewRunner(Config{
		Image:   "alpine:latest",
		Timeout: timeoutSeconds,
	})
	if err != nil {
		t.Fatalf("failed to create runner: %v", err)
	}

	start := time.Now()
	result, err := r.Exec(context.Background(), ExecRequest{
		Image:   "alpine:latest",
		Command: []string{"sleep", "60"},
		Timeout: timeoutSeconds,
	})
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("exec should not return error on timeout: %v", err)
	}
	if !result.TimedOut {
		t.Fatal("expected timed out to be true")
	}
	// Allow generous slack for container startup/teardown.
	const maxExpectedDuration = 30 * time.Second
	if elapsed > maxExpectedDuration {
		t.Fatalf("timeout enforcement too slow: took %v", elapsed)
	}
}

func TestIntegration_ExecWithEnv(t *testing.T) {
	if !dockerAvailable() {
		t.Skip("docker is not available")
	}

	r, err := NewRunner(Config{
		Image:   "alpine:latest",
		Timeout: 30,
	})
	if err != nil {
		t.Fatalf("failed to create runner: %v", err)
	}

	result, err := r.Exec(context.Background(), ExecRequest{
		Image:   "alpine:latest",
		Command: []string{"sh", "-c", "echo $MY_VAR"},
		Env: map[string]string{
			"MY_VAR": "sandbox_value",
		},
	})
	if err != nil {
		t.Fatalf("exec failed: %v", err)
	}
	if result.ExitCode != 0 {
		t.Fatalf("expected exit code 0, got %d", result.ExitCode)
	}
	if !strings.Contains(result.Stdout, "sandbox_value") {
		t.Fatalf("expected stdout to contain 'sandbox_value', got %q", result.Stdout)
	}
}

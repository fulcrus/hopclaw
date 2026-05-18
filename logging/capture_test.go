package logging

import (
	"bytes"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// Constants
// ---------------------------------------------------------------------------

const (
	captureWaitTimeout = 2 * time.Second
	captureWaitTick    = 10 * time.Millisecond
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// setupCaptureLogger creates a JSON logger writing to a buffer for capture tests.
func setupCaptureLogger(t *testing.T) (*bytes.Buffer, *slog.Logger) {
	t.Helper()
	var buf bytes.Buffer
	opts := &slog.HandlerOptions{Level: slog.LevelDebug}
	handler := slog.NewJSONHandler(&buf, opts)
	logger := slog.New(handler)
	return &buf, logger
}

// waitForContent waits until the buffer contains the expected string or times out.
func waitForContent(t *testing.T, buf *bytes.Buffer, expected string) {
	t.Helper()
	deadline := time.Now().Add(captureWaitTimeout)
	for time.Now().Before(deadline) {
		if strings.Contains(buf.String(), expected) {
			return
		}
		time.Sleep(captureWaitTick)
	}
	t.Fatalf("timed out waiting for %q in output:\n%s", expected, buf.String())
}

// ---------------------------------------------------------------------------
// Tests - Stdout capture
// ---------------------------------------------------------------------------

func TestConsoleCaptureStdout(t *testing.T) {
	buf, logger := setupCaptureLogger(t)

	cc := NewConsoleCapture(logger)
	if err := cc.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	// Write to os.Stdout which is now redirected.
	fmt.Fprintln(os.Stdout, "captured stdout line")

	if err := cc.Stop(); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}

	waitForContent(t, buf, "captured stdout line")

	out := buf.String()
	if !strings.Contains(out, streamStdout) {
		t.Fatalf("output should contain stream=stdout: %s", out)
	}
}

// ---------------------------------------------------------------------------
// Tests - Stderr capture
// ---------------------------------------------------------------------------

func TestConsoleCaptureStderr(t *testing.T) {
	buf, logger := setupCaptureLogger(t)

	cc := NewConsoleCapture(logger)
	if err := cc.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	// Write to os.Stderr which is now redirected.
	fmt.Fprintln(os.Stderr, "captured stderr line")

	if err := cc.Stop(); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}

	waitForContent(t, buf, "captured stderr line")

	out := buf.String()
	if !strings.Contains(out, streamStderr) {
		t.Fatalf("output should contain stream=stderr: %s", out)
	}
}

// ---------------------------------------------------------------------------
// Tests - Start/Stop lifecycle
// ---------------------------------------------------------------------------

func TestConsoleCaptureStartStop(t *testing.T) {
	_, logger := setupCaptureLogger(t)

	cc := NewConsoleCapture(logger)

	// Stop before start should error.
	if err := cc.Stop(); err == nil {
		t.Fatal("Stop() before Start() should return error")
	}

	// Start should succeed.
	if err := cc.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	// Double start should error.
	if err := cc.Start(); err == nil {
		t.Fatal("double Start() should return error")
	}

	// Stop should succeed.
	if err := cc.Stop(); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}

	// Verify stdout/stderr are restored.
	// Writing after stop should not panic and should go to original stdout.
	fmt.Fprintln(os.Stdout, "after stop stdout")
	fmt.Fprintln(os.Stderr, "after stop stderr")
}

func TestConsoleCaptureRestoresFileDescriptors(t *testing.T) {
	_, logger := setupCaptureLogger(t)

	origStdout := os.Stdout
	origStderr := os.Stderr

	cc := NewConsoleCapture(logger)
	if err := cc.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	// During capture, os.Stdout/os.Stderr should be different.
	if os.Stdout == origStdout {
		t.Fatal("os.Stdout should be redirected during capture")
	}
	if os.Stderr == origStderr {
		t.Fatal("os.Stderr should be redirected during capture")
	}

	if err := cc.Stop(); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}

	// After stop, os.Stdout/os.Stderr should be restored.
	if os.Stdout != origStdout {
		t.Fatal("os.Stdout should be restored after stop")
	}
	if os.Stderr != origStderr {
		t.Fatal("os.Stderr should be restored after stop")
	}
}

// ---------------------------------------------------------------------------
// Tests - Subsystem tagging
// ---------------------------------------------------------------------------

func TestConsoleCaptureSubsystem(t *testing.T) {
	buf, logger := setupCaptureLogger(t)

	cc := NewConsoleCapture(logger)
	if err := cc.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	fmt.Fprintln(os.Stdout, "subsystem check")

	if err := cc.Stop(); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}

	waitForContent(t, buf, "subsystem check")

	out := buf.String()
	if !strings.Contains(out, captureSubsystem) {
		t.Fatalf("output should contain subsystem=%s: %s", captureSubsystem, out)
	}
}

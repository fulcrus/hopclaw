package logging

import (
	"bufio"
	"fmt"
	"io"
	"log/slog"
	"os"
	"sync"
)

// ---------------------------------------------------------------------------
// Constants
// ---------------------------------------------------------------------------

const (
	captureSubsystem = "console"
	captureBufSize   = 64 * 1024 // 64 KiB scanner buffer

	streamStdout = "stdout"
	streamStderr = "stderr"
)

// ---------------------------------------------------------------------------
// ConsoleCapture
// ---------------------------------------------------------------------------

// ConsoleCapture captures stdout and stderr output and writes each line as a
// structured log entry. This is useful for capturing output from subprocesses
// or legacy code that writes directly to os.Stdout/os.Stderr.
type ConsoleCapture struct {
	logger *slog.Logger

	mu         sync.Mutex // guards started, origStdout, origStderr, done
	started    bool
	origStdout *os.File
	origStderr *os.File
	stdoutR    *os.File
	stdoutW    *os.File
	stderrR    *os.File
	stderrW    *os.File
	done       chan struct{}
}

// NewConsoleCapture creates a new ConsoleCapture that writes captured output
// as structured log entries using the given logger.
func NewConsoleCapture(logger *slog.Logger) *ConsoleCapture {
	return &ConsoleCapture{
		logger: logger.With("subsystem", captureSubsystem),
	}
}

// Start redirects os.Stdout and os.Stderr to internal pipes and begins
// reading from them. Each line is emitted as a structured log entry.
// Returns an error if capture is already started or if pipe creation fails.
func (c *ConsoleCapture) Start() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.started {
		return fmt.Errorf("console capture already started")
	}

	stdoutR, stdoutW, err := os.Pipe()
	if err != nil {
		return fmt.Errorf("failed to create stdout pipe: %w", err)
	}

	stderrR, stderrW, err := os.Pipe()
	if err != nil {
		_ = stdoutR.Close()
		_ = stdoutW.Close()
		return fmt.Errorf("failed to create stderr pipe: %w", err)
	}

	c.origStdout = os.Stdout
	c.origStderr = os.Stderr
	c.stdoutR = stdoutR
	c.stdoutW = stdoutW
	c.stderrR = stderrR
	c.stderrW = stderrW
	c.done = make(chan struct{})

	os.Stdout = stdoutW
	os.Stderr = stderrW

	var wg sync.WaitGroup
	wg.Add(2) //nolint:mnd // two goroutines for stdout and stderr

	go c.readPipe(&wg, stdoutR, streamStdout)
	go c.readPipe(&wg, stderrR, streamStderr)

	go func() {
		wg.Wait()
		close(c.done)
	}()

	c.started = true
	return nil
}

// Stop restores the original stdout and stderr and waits for the capture
// goroutines to finish draining. Returns an error if capture is not started.
func (c *ConsoleCapture) Stop() error {
	c.mu.Lock()
	if !c.started {
		c.mu.Unlock()
		return fmt.Errorf("console capture not started")
	}

	// Restore original file descriptors first.
	os.Stdout = c.origStdout
	os.Stderr = c.origStderr

	// Close write ends so readers see EOF.
	_ = c.stdoutW.Close()
	_ = c.stderrW.Close()

	done := c.done
	c.started = false
	c.mu.Unlock()

	// Wait for reader goroutines to drain.
	<-done

	return nil
}

// readPipe scans lines from r and emits each as a log entry tagged with the
// given stream name.
func (c *ConsoleCapture) readPipe(wg *sync.WaitGroup, r io.Reader, stream string) {
	defer wg.Done()

	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, captureBufSize), captureBufSize)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}
		if stream == streamStderr {
			c.logger.Warn(line, "stream", stream)
		} else {
			c.logger.Info(line, "stream", stream)
		}
	}
}

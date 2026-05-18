package toolruntime

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/fulcrus/hopclaw/agent"
)

// ---------------------------------------------------------------------------
// proc.start / proc.list tests
// ---------------------------------------------------------------------------

func TestProcStartAndList(t *testing.T) {
	t.Parallel()

	builtins := NewBuiltins(BuiltinsConfig{Root: t.TempDir()})
	ctx := context.Background()
	run := &agent.Run{ID: "run-proc"}
	sess := &agent.Session{ID: "sess-proc"}

	// Start a short-lived process.
	results, err := builtins.ExecuteBatch(ctx, run, sess, []agent.ToolCall{{
		ID:   "call-proc-start",
		Name: "proc.start",
		Input: map[string]any{
			"command": "sh",
			"args":    []any{"-c", "echo hello && sleep 0.5"},
		},
	}})
	if err != nil {
		t.Fatalf("proc.start error = %v", err)
	}

	var startPayload struct {
		ID      string `json:"id"`
		Command string `json:"command"`
		Status  string `json:"status"`
	}
	if err := json.Unmarshal([]byte(results[0].Content), &startPayload); err != nil {
		t.Fatalf("Unmarshal error = %v", err)
	}
	if startPayload.ID == "" {
		t.Fatal("process ID should not be empty")
	}
	if startPayload.Command != "sh" {
		t.Fatalf("command = %q, want sh", startPayload.Command)
	}

	procID := startPayload.ID

	// List processes.
	results, err = builtins.ExecuteBatch(ctx, run, sess, []agent.ToolCall{{
		ID:    "call-proc-list",
		Name:  "proc.list",
		Input: map[string]any{},
	}})
	if err != nil {
		t.Fatalf("proc.list error = %v", err)
	}

	var listPayload struct {
		Processes []struct {
			ID string `json:"id"`
		} `json:"processes"`
		Count int `json:"count"`
	}
	json.Unmarshal([]byte(results[0].Content), &listPayload)
	if listPayload.Count < 1 {
		t.Fatalf("count = %d, want >= 1", listPayload.Count)
	}

	found := false
	for _, p := range listPayload.Processes {
		if p.ID == procID {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("started process %q not found in list", procID)
	}

	// Clean up: stop the process.
	builtins.ExecuteBatch(ctx, run, sess, []agent.ToolCall{{
		ID:   "call-proc-stop",
		Name: "proc.stop",
		Input: map[string]any{
			"id": procID,
		},
	}})
}

// ---------------------------------------------------------------------------
// proc.logs tests
// ---------------------------------------------------------------------------

func TestProcLogs(t *testing.T) {
	t.Parallel()

	builtins := NewBuiltins(BuiltinsConfig{Root: t.TempDir()})
	ctx := context.Background()
	run := &agent.Run{ID: "run-proc"}
	sess := &agent.Session{ID: "sess-proc"}

	// Start a process that writes to stdout.
	results, err := builtins.ExecuteBatch(ctx, run, sess, []agent.ToolCall{{
		ID:   "call-proc-start-logs",
		Name: "proc.start",
		Input: map[string]any{
			"command": "sh",
			"args":    []any{"-c", "echo proc-output"},
		},
	}})
	if err != nil {
		t.Fatalf("proc.start error = %v", err)
	}
	var startPayload struct {
		ID string `json:"id"`
	}
	json.Unmarshal([]byte(results[0].Content), &startPayload)
	procID := startPayload.ID

	// Wait a moment for the process to complete.
	time.Sleep(200 * time.Millisecond)

	// Get logs.
	results, err = builtins.ExecuteBatch(ctx, run, sess, []agent.ToolCall{{
		ID:   "call-proc-logs",
		Name: "proc.logs",
		Input: map[string]any{
			"id": procID,
		},
	}})
	if err != nil {
		t.Fatalf("proc.logs error = %v", err)
	}

	var logsPayload struct {
		Stdout string `json:"stdout"`
	}
	json.Unmarshal([]byte(results[0].Content), &logsPayload)
	if logsPayload.Stdout == "" {
		t.Fatal("stdout should contain output")
	}
}

func TestProcLogsWhileRunning(t *testing.T) {
	t.Parallel()

	builtins := NewBuiltins(BuiltinsConfig{Root: t.TempDir()})
	ctx := context.Background()
	run := &agent.Run{ID: "run-proc"}
	sess := &agent.Session{ID: "sess-proc"}

	results, err := builtins.ExecuteBatch(ctx, run, sess, []agent.ToolCall{{
		ID:   "call-proc-start-running-logs",
		Name: "proc.start",
		Input: map[string]any{
			"command": "sh",
			"args": []any{"-c",
				"printf 'tick-0\\n'; sleep 0.2; printf 'tick-1\\n'; sleep 0.2",
			},
		},
	}})
	if err != nil {
		t.Fatalf("proc.start error = %v", err)
	}
	var startPayload struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal([]byte(results[0].Content), &startPayload); err != nil {
		t.Fatalf("proc.start unmarshal: %v", err)
	}
	if startPayload.ID == "" {
		t.Fatal("process ID should not be empty")
	}

	time.Sleep(100 * time.Millisecond)

	results, err = builtins.ExecuteBatch(ctx, run, sess, []agent.ToolCall{{
		ID:   "call-proc-logs-running",
		Name: "proc.logs",
		Input: map[string]any{
			"id":     startPayload.ID,
			"stream": "stdout",
		},
	}})
	if err != nil {
		t.Fatalf("proc.logs error = %v", err)
	}

	var logsPayload struct {
		Stdout string `json:"stdout"`
	}
	if err := json.Unmarshal([]byte(results[0].Content), &logsPayload); err != nil {
		t.Fatalf("proc.logs unmarshal: %v", err)
	}
	if !strings.Contains(logsPayload.Stdout, "tick-0") {
		t.Fatalf("stdout = %q, want tick-0 while process is still running", logsPayload.Stdout)
	}

	results, err = builtins.ExecuteBatch(ctx, run, sess, []agent.ToolCall{{
		ID:   "call-proc-wait-running-logs",
		Name: "proc.wait",
		Input: map[string]any{
			"id":              startPayload.ID,
			"timeout_seconds": 2,
		},
	}})
	if err != nil {
		t.Fatalf("proc.wait error = %v", err)
	}

	var waitPayload struct {
		Stdout   string `json:"stdout"`
		TimedOut bool   `json:"timed_out"`
	}
	if err := json.Unmarshal([]byte(results[0].Content), &waitPayload); err != nil {
		t.Fatalf("proc.wait unmarshal: %v", err)
	}
	if waitPayload.TimedOut {
		t.Fatal("timed_out should be false")
	}
	if !strings.Contains(waitPayload.Stdout, "tick-1") {
		t.Fatalf("stdout = %q, want tick-1 after wait completes", waitPayload.Stdout)
	}
}

// ---------------------------------------------------------------------------
// proc.stop tests
// ---------------------------------------------------------------------------

func TestProcStop(t *testing.T) {
	t.Parallel()

	builtins := NewBuiltins(BuiltinsConfig{Root: t.TempDir()})
	ctx := context.Background()
	run := &agent.Run{ID: "run-proc"}
	sess := &agent.Session{ID: "sess-proc"}

	// Start a long-running process.
	results, err := builtins.ExecuteBatch(ctx, run, sess, []agent.ToolCall{{
		ID:   "call-proc-start-stop",
		Name: "proc.start",
		Input: map[string]any{
			"command": "sh",
			"args":    []any{"-c", "sleep 60"},
		},
	}})
	if err != nil {
		t.Fatalf("proc.start error = %v", err)
	}
	var startPayload struct {
		ID string `json:"id"`
	}
	json.Unmarshal([]byte(results[0].Content), &startPayload)
	procID := startPayload.ID

	// Stop the process.
	results, err = builtins.ExecuteBatch(ctx, run, sess, []agent.ToolCall{{
		ID:   "call-proc-stop",
		Name: "proc.stop",
		Input: map[string]any{
			"id": procID,
		},
	}})
	if err != nil {
		t.Fatalf("proc.stop error = %v", err)
	}

	var stopPayload struct {
		ID      string `json:"id"`
		Stopped bool   `json:"stopped"`
	}
	json.Unmarshal([]byte(results[0].Content), &stopPayload)
	if !stopPayload.Stopped {
		t.Fatal("stopped should be true")
	}
}

func TestProcStopNonExistent(t *testing.T) {
	t.Parallel()

	builtins := NewBuiltins(BuiltinsConfig{Root: t.TempDir()})
	_, err := builtins.ExecuteBatch(context.Background(), &agent.Run{ID: "run-proc"}, &agent.Session{ID: "sess-proc"}, []agent.ToolCall{{
		ID:   "call-proc-stop-miss",
		Name: "proc.stop",
		Input: map[string]any{
			"id": "nonexistent-proc-id",
		},
	}})
	if err == nil {
		t.Fatal("expected error when stopping nonexistent process")
	}
}

// ---------------------------------------------------------------------------
// proc.wait tests
// ---------------------------------------------------------------------------

func TestProcWait(t *testing.T) {
	t.Parallel()

	builtins := NewBuiltins(BuiltinsConfig{Root: t.TempDir()})
	ctx := context.Background()
	run := &agent.Run{ID: "run-proc"}
	sess := &agent.Session{ID: "sess-proc"}

	// Start a fast process.
	results, err := builtins.ExecuteBatch(ctx, run, sess, []agent.ToolCall{{
		ID:   "call-proc-start-wait",
		Name: "proc.start",
		Input: map[string]any{
			"command": "sh",
			"args":    []any{"-c", "echo done"},
		},
	}})
	if err != nil {
		t.Fatalf("proc.start error = %v", err)
	}
	var startPayload struct {
		ID string `json:"id"`
	}
	json.Unmarshal([]byte(results[0].Content), &startPayload)
	procID := startPayload.ID

	// Give the fast process a moment to finish before calling wait.
	time.Sleep(200 * time.Millisecond)

	// Wait for the process.
	results, err = builtins.ExecuteBatch(ctx, run, sess, []agent.ToolCall{{
		ID:   "call-proc-wait",
		Name: "proc.wait",
		Input: map[string]any{
			"id":              procID,
			"timeout_seconds": 5,
		},
	}})
	if err != nil {
		t.Fatalf("proc.wait error = %v", err)
	}

	var waitPayload struct {
		ID       string `json:"id"`
		ExitCode int    `json:"exit_code"`
		TimedOut bool   `json:"timed_out"`
	}
	json.Unmarshal([]byte(results[0].Content), &waitPayload)
	if waitPayload.TimedOut {
		t.Fatal("timed_out should be false for a completed process")
	}
	if waitPayload.ExitCode != 0 {
		t.Fatalf("exit_code = %d, want 0", waitPayload.ExitCode)
	}
}

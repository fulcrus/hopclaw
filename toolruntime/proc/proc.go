// Package proc implements proc.list, proc.start, proc.stop, proc.logs, and proc.wait
// tool handlers for the toolruntime registry.
package proc

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/fulcrus/hopclaw/agent"
	"github.com/fulcrus/hopclaw/contextengine"
	"github.com/fulcrus/hopclaw/skill"
)

// Runtime is the narrow interface that proc handlers need from *Builtins.
type Runtime interface {
	JSONResult(call agent.ToolCall, payload map[string]any) (contextengine.ToolResult, error)
	ResolvePath(input string) (string, error)
	RootAbs() string
}

// Handler is the tool handler signature, parameterized on our narrow Runtime interface.
type Handler func(ctx context.Context, rt Runtime, call agent.ToolCall) (contextengine.ToolResult, error)

// ToolDef pairs a tool manifest with a proc handler.
type ToolDef struct {
	Manifest skill.ToolManifest
	Handler  Handler
}

// managedProcess holds the state for a background process started by the agent.
type managedProcess struct {
	ID        string
	Command   string
	Args      []string
	Dir       string
	Cmd       *exec.Cmd
	Stdout    *lockedBuffer
	Stderr    *lockedBuffer
	StartedAt time.Time
	Done      chan struct{}
	stateMu   sync.RWMutex
	ExitCode  int
	Error     string
}

type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// registry is a package-level registry of managed background processes.
var registry = struct {
	mu    sync.Mutex
	procs map[string]*managedProcess
}{procs: make(map[string]*managedProcess)}

// ToolDefs returns all proc domain tool definitions.
func ToolDefs() []ToolDef {
	return []ToolDef{
		{
			Manifest: skill.ToolManifest{
				Name:            "proc.list",
				Description:     "List all managed background processes.",
				InputSchema:     procListInputSchema(),
				OutputSchema:    procListOutputSchema(),
				SideEffectClass: "read",
				Idempotent:      true,
				ExecutionKey:    "proc:list",
			},
			Handler: handleProcList,
		},
		{
			Manifest: skill.ToolManifest{
				Name:             "proc.start",
				Description:      "Start a command as a managed background process.",
				InputSchema:      procStartInputSchema(),
				OutputSchema:     procStartOutputSchema(),
				SideEffectClass:  "destructive",
				RequiresApproval: true,
				ExecutionKey:     "proc:start:{command}",
			},
			Handler: handleProcStart,
		},
		{
			Manifest: skill.ToolManifest{
				Name:             "proc.stop",
				Description:      "Stop a managed background process.",
				InputSchema:      procStopInputSchema(),
				OutputSchema:     procStopOutputSchema(),
				SideEffectClass:  "destructive",
				RequiresApproval: true,
				ExecutionKey:     "proc:stop:{id}",
			},
			Handler: handleProcStop,
		},
		{
			Manifest: skill.ToolManifest{
				Name:            "proc.logs",
				Description:     "Retrieve stdout and/or stderr output from a managed process.",
				InputSchema:     procLogsInputSchema(),
				OutputSchema:    procLogsOutputSchema(),
				SideEffectClass: "read",
				Idempotent:      true,
				ExecutionKey:    "proc:logs:{id}",
			},
			Handler: handleProcLogs,
		},
		{
			Manifest: skill.ToolManifest{
				Name:            "proc.wait",
				Description:     "Wait for a managed process to finish, with an optional timeout.",
				InputSchema:     procWaitInputSchema(),
				OutputSchema:    procWaitOutputSchema(),
				SideEffectClass: "read",
				ExecutionKey:    "proc:wait:{id}",
			},
			Handler: handleProcWait,
		},
	}
}

// ---------------------------------------------------------------------------
// Param helpers — duplicated locally to avoid importing toolruntime.
// ---------------------------------------------------------------------------

func stringFrom(value any) (string, error) {
	switch typed := value.(type) {
	case nil:
		return "", nil
	case string:
		return typed, nil
	default:
		return "", fmt.Errorf("expected string, got %T", value)
	}
}

func requiredString(input map[string]any, key string) (string, error) {
	value, err := stringFrom(input[key])
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(value) == "" {
		return "", fmt.Errorf("%s is required", key)
	}
	return value, nil
}

func stringSliceFrom(value any) ([]string, error) {
	switch typed := value.(type) {
	case nil:
		return nil, nil
	case []string:
		return append([]string(nil), typed...), nil
	case []any:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			s, ok := item.(string)
			if !ok {
				return nil, fmt.Errorf("expected string array element, got %T", item)
			}
			out = append(out, s)
		}
		return out, nil
	default:
		return nil, fmt.Errorf("expected string array, got %T", value)
	}
}

func intFrom(value any, fallback int) (int, error) {
	if value == nil {
		return fallback, nil
	}
	switch typed := value.(type) {
	case int:
		return typed, nil
	case int64:
		return int(typed), nil
	case float64:
		return int(typed), nil
	case string:
		if strings.TrimSpace(typed) == "" {
			return fallback, nil
		}
		var v int64
		_, err := fmt.Sscanf(typed, "%d", &v)
		if err != nil {
			return 0, err
		}
		return int(v), nil
	default:
		return 0, fmt.Errorf("expected integer, got %T", value)
	}
}

// ---------------------------------------------------------------------------
// Schema helpers — duplicated locally to avoid importing toolruntime.
// ---------------------------------------------------------------------------

func stringSchema(description string) map[string]any {
	schema := map[string]any{"type": "string"}
	if description != "" {
		schema["description"] = description
	}
	return schema
}

func integerSchema(description string) map[string]any {
	schema := map[string]any{"type": "integer"}
	if description != "" {
		schema["description"] = description
	}
	return schema
}

func booleanSchema(description string) map[string]any {
	schema := map[string]any{"type": "boolean"}
	if description != "" {
		schema["description"] = description
	}
	return schema
}

func objectSchema(properties map[string]any, required ...string) map[string]any {
	schema := map[string]any{
		"type":                 "object",
		"properties":           properties,
		"additionalProperties": false,
	}
	if len(required) > 0 {
		schema["required"] = required
	}
	return schema
}

func arraySchema(items map[string]any, description string) map[string]any {
	schema := map[string]any{
		"type":  "array",
		"items": items,
	}
	if description != "" {
		schema["description"] = description
	}
	return schema
}

// ---------------------------------------------------------------------------
// Input schemas
// ---------------------------------------------------------------------------

func procListInputSchema() map[string]any {
	return map[string]any{
		"type":                 "object",
		"properties":           map[string]any{},
		"additionalProperties": false,
	}
}

func procStartInputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"command": map[string]any{
				"type":        "string",
				"description": "The command to run.",
			},
			"args": map[string]any{
				"type":        "array",
				"items":       map[string]any{"type": "string"},
				"description": "Arguments for the command.",
			},
			"dir": map[string]any{
				"type":        "string",
				"description": "Working directory. Defaults to the workspace root.",
			},
		},
		"required":             []string{"command"},
		"additionalProperties": false,
	}
}

func procStopInputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"id": map[string]any{
				"type":        "string",
				"description": "The process ID returned by proc.start.",
			},
		},
		"required":             []string{"id"},
		"additionalProperties": false,
	}
}

func procLogsInputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"id": map[string]any{
				"type":        "string",
				"description": "The process ID returned by proc.start.",
			},
			"stream": map[string]any{
				"type":        "string",
				"description": "Which output stream: stdout, stderr, or both. Defaults to stdout.",
			},
		},
		"required":             []string{"id"},
		"additionalProperties": false,
	}
}

func procWaitInputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"id": map[string]any{
				"type":        "string",
				"description": "The process ID returned by proc.start.",
			},
			"timeout_seconds": map[string]any{
				"type":        "integer",
				"description": "Maximum seconds to wait. Defaults to 30.",
			},
		},
		"required":             []string{"id"},
		"additionalProperties": false,
	}
}

// ---------------------------------------------------------------------------
// Output schemas
// ---------------------------------------------------------------------------

func procListOutputSchema() map[string]any {
	entry := objectSchema(map[string]any{
		"id":         stringSchema("Process ID."),
		"command":    stringSchema("Command name."),
		"pid":        integerSchema("OS process ID."),
		"running":    booleanSchema("Whether the process is still running."),
		"started_at": stringSchema("Start time in RFC3339 format."),
	}, "id", "command", "running", "started_at")
	return objectSchema(map[string]any{
		"processes": arraySchema(entry, "Managed processes."),
		"count":     integerSchema("Number of managed processes."),
	}, "processes", "count")
}

func procStartOutputSchema() map[string]any {
	return objectSchema(map[string]any{
		"id":      stringSchema("Assigned process ID."),
		"command": stringSchema("Command that was started."),
		"pid":     integerSchema("OS process ID."),
	}, "id", "command", "pid")
}

func procStopOutputSchema() map[string]any {
	return objectSchema(map[string]any{
		"id":      stringSchema("Process ID."),
		"stopped": booleanSchema("Whether the process was successfully stopped."),
		"message": stringSchema("Human-readable result."),
	}, "id", "stopped", "message")
}

func procLogsOutputSchema() map[string]any {
	return objectSchema(map[string]any{
		"id":     stringSchema("Process ID."),
		"stdout": stringSchema("Captured stdout output."),
		"stderr": stringSchema("Captured stderr output."),
	}, "id")
}

func procWaitOutputSchema() map[string]any {
	return objectSchema(map[string]any{
		"id":        stringSchema("Process ID."),
		"exit_code": integerSchema("Process exit code."),
		"stdout":    stringSchema("Captured stdout output."),
		"stderr":    stringSchema("Captured stderr output."),
		"timed_out": booleanSchema("Whether the wait timed out."),
	}, "id", "timed_out")
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func generateProcID() string {
	buf := make([]byte, 4)
	rand.Read(buf)
	return hex.EncodeToString(buf)
}

func (mp *managedProcess) setCompletion(err error, exitCode int) {
	mp.stateMu.Lock()
	defer mp.stateMu.Unlock()
	mp.ExitCode = exitCode
	if err != nil {
		mp.Error = err.Error()
		return
	}
	mp.Error = ""
}

func (mp *managedProcess) exitCode() int {
	mp.stateMu.RLock()
	defer mp.stateMu.RUnlock()
	return mp.ExitCode
}

func procIsRunning(mp *managedProcess) bool {
	select {
	case <-mp.Done:
		return false
	default:
		return true
	}
}

func lookupProc(id string) (*managedProcess, error) {
	registry.mu.Lock()
	defer registry.mu.Unlock()
	mp, ok := registry.procs[id]
	if !ok {
		return nil, fmt.Errorf("process %q not found", id)
	}
	return mp, nil
}

// ---------------------------------------------------------------------------
// Handlers
// ---------------------------------------------------------------------------

func handleProcList(_ context.Context, rt Runtime, call agent.ToolCall) (contextengine.ToolResult, error) {
	registry.mu.Lock()
	defer registry.mu.Unlock()

	entries := make([]map[string]any, 0, len(registry.procs))
	for _, mp := range registry.procs {
		pid := 0
		if mp.Cmd != nil && mp.Cmd.Process != nil {
			pid = mp.Cmd.Process.Pid
		}
		entries = append(entries, map[string]any{
			"id":         mp.ID,
			"command":    mp.Command,
			"pid":        pid,
			"running":    procIsRunning(mp),
			"started_at": mp.StartedAt.Format(time.RFC3339),
		})
	}

	return rt.JSONResult(call, map[string]any{
		"processes": entries,
		"count":     len(entries),
	})
}

func handleProcStart(_ context.Context, rt Runtime, call agent.ToolCall) (contextengine.ToolResult, error) {
	command, err := requiredString(call.Input, "command")
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("proc.start: %w", err)
	}
	args, err := stringSliceFrom(call.Input["args"])
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("proc.start: %w", err)
	}
	dir, _ := stringFrom(call.Input["dir"])

	workDir := rt.RootAbs()
	if strings.TrimSpace(dir) != "" {
		workDir, err = rt.ResolvePath(dir)
		if err != nil {
			return contextengine.ToolResult{}, fmt.Errorf("proc.start: %w", err)
		}
	}

	id := generateProcID()

	cmd := exec.Command(command, args...)
	cmd.Dir = workDir
	// Ensure cmd.Wait() returns even when the process leaves grandchild
	// processes inheriting our stdout/stderr pipes (a typical pattern with
	// `sh -c '...'`). Without this, killing the immediate child can leave
	// Wait() blocked until the orphaned grandchild eventually exits.
	cmd.WaitDelay = 5 * time.Second

	stdoutBuf := &lockedBuffer{}
	stderrBuf := &lockedBuffer{}
	cmd.Stdout = stdoutBuf
	cmd.Stderr = stderrBuf

	if err := cmd.Start(); err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("proc.start: %w", err)
	}

	mp := &managedProcess{
		ID:        id,
		Command:   command,
		Args:      args,
		Dir:       workDir,
		Cmd:       cmd,
		Stdout:    stdoutBuf,
		Stderr:    stderrBuf,
		StartedAt: time.Now(),
		Done:      make(chan struct{}),
	}

	// Monitor the process in the background.
	go func() {
		defer close(mp.Done)
		err := cmd.Wait()
		exitCode := 0
		if cmd.ProcessState != nil {
			exitCode = cmd.ProcessState.ExitCode()
		}
		mp.setCompletion(err, exitCode)
	}()

	registry.mu.Lock()
	registry.procs[id] = mp
	registry.mu.Unlock()

	pid := 0
	if cmd.Process != nil {
		pid = cmd.Process.Pid
	}

	return rt.JSONResult(call, map[string]any{
		"id":      id,
		"command": command,
		"pid":     pid,
	})
}

func handleProcStop(_ context.Context, rt Runtime, call agent.ToolCall) (contextengine.ToolResult, error) {
	id, err := requiredString(call.Input, "id")
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("proc.stop: %w", err)
	}

	mp, err := lookupProc(id)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("proc.stop: %w", err)
	}

	if !procIsRunning(mp) {
		return rt.JSONResult(call, map[string]any{
			"id":      id,
			"stopped": false,
			"message": fmt.Sprintf("process %s has already exited", id),
		})
	}

	// Send SIGTERM first.
	if mp.Cmd.Process != nil {
		mp.Cmd.Process.Signal(syscall.SIGTERM)
	}

	// Wait up to 5 seconds for graceful shutdown.
	select {
	case <-mp.Done:
		return rt.JSONResult(call, map[string]any{
			"id":      id,
			"stopped": true,
			"message": fmt.Sprintf("process %s terminated gracefully", id),
		})
	case <-time.After(5 * time.Second):
	}

	// Force kill if still running.
	if procIsRunning(mp) && mp.Cmd.Process != nil {
		mp.Cmd.Process.Kill()
		// Wait for the kill to take effect.
		select {
		case <-mp.Done:
		case <-time.After(5 * time.Second):
		}
	}

	return rt.JSONResult(call, map[string]any{
		"id":      id,
		"stopped": !procIsRunning(mp),
		"message": fmt.Sprintf("process %s was forcefully killed", id),
	})
}

func handleProcLogs(_ context.Context, rt Runtime, call agent.ToolCall) (contextengine.ToolResult, error) {
	id, err := requiredString(call.Input, "id")
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("proc.logs: %w", err)
	}
	stream, _ := stringFrom(call.Input["stream"])
	if strings.TrimSpace(stream) == "" {
		stream = "stdout"
	}
	stream = strings.ToLower(stream)

	mp, err := lookupProc(id)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("proc.logs: %w", err)
	}

	result := map[string]any{
		"id": id,
	}

	switch stream {
	case "stdout":
		result["stdout"] = mp.Stdout.String()
	case "stderr":
		result["stderr"] = mp.Stderr.String()
	case "both":
		result["stdout"] = mp.Stdout.String()
		result["stderr"] = mp.Stderr.String()
	default:
		return contextengine.ToolResult{}, fmt.Errorf("proc.logs: unsupported stream %q (supported: stdout, stderr, both)", stream)
	}

	return rt.JSONResult(call, result)
}

func handleProcWait(_ context.Context, rt Runtime, call agent.ToolCall) (contextengine.ToolResult, error) {
	id, err := requiredString(call.Input, "id")
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("proc.wait: %w", err)
	}
	timeoutSec, err := intFrom(call.Input["timeout_seconds"], 30)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("proc.wait: %w", err)
	}
	if timeoutSec <= 0 {
		timeoutSec = 30
	}

	mp, err := lookupProc(id)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("proc.wait: %w", err)
	}

	timedOut := false
	select {
	case <-mp.Done:
		// Process finished.
	case <-time.After(time.Duration(timeoutSec) * time.Second):
		timedOut = true
	}

	result := map[string]any{
		"id":        id,
		"exit_code": mp.exitCode(),
		"stdout":    mp.Stdout.String(),
		"stderr":    mp.Stderr.String(),
		"timed_out": timedOut,
	}

	return rt.JSONResult(call, result)
}

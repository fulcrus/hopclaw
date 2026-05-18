// Package exec implements exec.run, exec.shell, exec.which, and exec.script
// tool handlers for the toolruntime registry.
package exec

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/fulcrus/hopclaw/agent"
	"github.com/fulcrus/hopclaw/contextengine"
	"github.com/fulcrus/hopclaw/skill"
)

// Runtime is the narrow interface that exec handlers need from *Builtins.
type Runtime interface {
	JSONResult(call agent.ToolCall, payload map[string]any) (contextengine.ToolResult, error)
	ResolvePath(input string) (string, error)
	DisplayPath(absPath string) string
	RootAbs() string
	DefaultExecTimeout() time.Duration
	EffectiveExecTimeout(requested time.Duration) time.Duration
	ValidateExecInvocation(command string, args []string) error
	ValidateShellInvocation(command string) error
	ExecOutputLimit() int
	NewExecCapture(limit int) (stdout, stderr interface{ String() string }, stdoutW, stderrW io.Writer)
}

// Handler is the tool handler signature, parameterized on our narrow Runtime interface.
type Handler func(ctx context.Context, rt Runtime, call agent.ToolCall) (contextengine.ToolResult, error)

// ToolDef pairs a tool manifest with an exec handler.
type ToolDef struct {
	Manifest skill.ToolManifest
	Handler  Handler
}

// ToolDefs returns the core exec tool definitions (exec.run, exec.shell, exec.which).
func ToolDefs(defaultExecTimeout time.Duration) []ToolDef {
	return []ToolDef{
		{
			Manifest: skill.ToolManifest{
				Name:             "exec.run",
				Description:      "Run a command inside the workspace root without invoking a shell.",
				InputSchema:      execRunInputSchema(),
				OutputSchema:     execRunOutputSchema(),
				SideEffectClass:  "destructive",
				Idempotent:       false,
				ExecutionKey:     "exec:{command}",
				RequiresApproval: true,
				Timeout:          defaultExecTimeout,
			},
			Handler: handleRun,
		},
		{
			Manifest: skill.ToolManifest{
				Name:             "exec.shell",
				Description:      "Run a shell command inside the workspace root.",
				InputSchema:      execShellInputSchema(),
				OutputSchema:     execShellOutputSchema(),
				SideEffectClass:  "destructive",
				Idempotent:       false,
				ExecutionKey:     "exec:shell",
				RequiresApproval: true,
				Timeout:          defaultExecTimeout,
			},
			Handler: handleShell,
		},
		{
			Manifest: skill.ToolManifest{
				Name:            "exec.which",
				Description:     "Locate an executable in the system PATH.",
				InputSchema:     execWhichInputSchema(),
				OutputSchema:    execWhichOutputSchema(),
				SideEffectClass: "read",
				Idempotent:      true,
				ExecutionKey:    "exec:which:{name}",
			},
			Handler: handleWhich,
		},
	}
}

// ExtraToolDefs returns supplemental exec tool definitions (exec.script).
func ExtraToolDefs(defaultExecTimeout time.Duration) []ToolDef {
	return []ToolDef{
		{
			Manifest: skill.ToolManifest{
				Name:             "exec.script",
				Description:      "Write a multi-line script to a temporary file and execute it with the specified interpreter.",
				InputSchema:      execScriptInputSchema(),
				OutputSchema:     execScriptOutputSchema(),
				SideEffectClass:  "destructive",
				RequiresApproval: true,
				ExecutionKey:     "exec:script",
				Timeout:          defaultExecTimeout,
			},
			Handler: handleScript,
		},
	}
}

// ---------------------------------------------------------------------------
// Input schemas
// ---------------------------------------------------------------------------

func execRunInputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"command": map[string]any{
				"type":        "string",
				"description": "Executable name or absolute path.",
			},
			"args": map[string]any{
				"type":        "array",
				"items":       map[string]any{"type": "string"},
				"description": "Optional command arguments.",
			},
			"dir": map[string]any{
				"type":        "string",
				"description": "Optional working directory relative to the workspace root.",
			},
			"stdin": map[string]any{
				"type":        "string",
				"description": "Optional stdin content passed to the process.",
			},
			"timeout_seconds": map[string]any{
				"type":        "number",
				"description": "Optional command timeout in seconds.",
			},
		},
		"required":             []string{"command"},
		"additionalProperties": false,
	}
}

func execShellInputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"command": map[string]any{
				"type":        "string",
				"description": "Shell command string to execute (passed to $SHELL -c).",
			},
			"dir": map[string]any{
				"type":        "string",
				"description": "Optional working directory relative to the workspace root.",
			},
			"stdin": map[string]any{
				"type":        "string",
				"description": "Optional stdin content passed to the shell.",
			},
			"timeout_seconds": map[string]any{
				"type":        "number",
				"description": "Optional command timeout in seconds.",
			},
		},
		"required":             []string{"command"},
		"additionalProperties": false,
	}
}

func execWhichInputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"name": map[string]any{
				"type":        "string",
				"description": "Name of the executable to locate.",
			},
		},
		"required":             []string{"name"},
		"additionalProperties": false,
	}
}

func execScriptInputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"script": map[string]any{
				"type":        "string",
				"description": "The script content to execute.",
			},
			"interpreter": map[string]any{
				"type":        "string",
				"description": "Path to the interpreter binary. Defaults to \"/bin/sh\".",
			},
			"dir": map[string]any{
				"type":        "string",
				"description": "Optional working directory relative to the workspace root.",
			},
			"timeout_seconds": map[string]any{
				"type":        "number",
				"description": "Optional execution timeout in seconds.",
			},
		},
		"required":             []string{"script"},
		"additionalProperties": false,
	}
}

// ---------------------------------------------------------------------------
// Output schemas — use plain map literals to avoid importing toolruntime.
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

func execRunOutputSchema() map[string]any {
	return objectSchema(map[string]any{
		"command": stringSchema("Executed command name."),
		"dir":     stringSchema("Working directory."),
		"stdout":  stringSchema("Captured stdout content."),
		"stderr":  stringSchema("Captured stderr content."),
		"content": stringSchema("Combined human-facing output."),
	}, "command", "dir", "stdout", "stderr", "content")
}

func execShellOutputSchema() map[string]any {
	return objectSchema(map[string]any{
		"command":   stringSchema("Shell command that was executed."),
		"dir":       stringSchema("Working directory."),
		"stdout":    stringSchema("Captured stdout content."),
		"stderr":    stringSchema("Captured stderr content."),
		"exit_code": integerSchema("Process exit code."),
	}, "command", "dir", "stdout", "stderr", "exit_code")
}

func execWhichOutputSchema() map[string]any {
	return objectSchema(map[string]any{
		"name":  stringSchema("Requested executable name."),
		"path":  stringSchema("Absolute path if found."),
		"found": map[string]any{"type": "boolean", "description": "Whether the executable was found."},
	}, "name", "path", "found")
}

func execScriptOutputSchema() map[string]any {
	return objectSchema(map[string]any{
		"script":      stringSchema("Script content that was executed."),
		"interpreter": stringSchema("Interpreter used."),
		"dir":         stringSchema("Working directory."),
		"stdout":      stringSchema("Captured stdout content."),
		"stderr":      stringSchema("Captured stderr content."),
		"exit_code":   integerSchema("Process exit code."),
	}, "script", "interpreter", "dir", "stdout", "stderr", "exit_code")
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

func timeoutFrom(value any, fallback time.Duration) (time.Duration, error) {
	if value == nil {
		return fallback, nil
	}
	switch typed := value.(type) {
	case float64:
		if typed <= 0 {
			return fallback, nil
		}
		return time.Duration(typed * float64(time.Second)), nil
	case int:
		if typed <= 0 {
			return fallback, nil
		}
		return time.Duration(typed) * time.Second, nil
	case int64:
		if typed <= 0 {
			return fallback, nil
		}
		return time.Duration(typed) * time.Second, nil
	case string:
		if strings.TrimSpace(typed) == "" {
			return fallback, nil
		}
		d, err := time.ParseDuration(typed)
		if err != nil {
			return 0, fmt.Errorf("invalid timeout: %w", err)
		}
		return d, nil
	default:
		return 0, fmt.Errorf("expected number or duration string for timeout, got %T", value)
	}
}

// ---------------------------------------------------------------------------
// Handlers
// ---------------------------------------------------------------------------

func handleRun(ctx context.Context, rt Runtime, call agent.ToolCall) (contextengine.ToolResult, error) {
	commandName, err := requiredString(call.Input, "command")
	if err != nil {
		return contextengine.ToolResult{}, err
	}
	args, err := stringSliceFrom(call.Input["args"])
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("invalid exec.run args: %w", err)
	}
	dirValue, err := stringFrom(call.Input["dir"])
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("invalid exec.run dir: %w", err)
	}
	workingDir := rt.RootAbs()
	if strings.TrimSpace(dirValue) != "" {
		workingDir, err = rt.ResolvePath(dirValue)
		if err != nil {
			return contextengine.ToolResult{}, err
		}
	}
	timeout, err := timeoutFrom(call.Input["timeout_seconds"], rt.DefaultExecTimeout())
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("invalid exec.run timeout_seconds: %w", err)
	}
	timeout = rt.EffectiveExecTimeout(timeout)
	if err := rt.ValidateExecInvocation(commandName, args); err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("exec.run: %w", err)
	}
	execCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	command := exec.CommandContext(execCtx, commandName, args...)
	command.Dir = workingDir
	if stdinValue, _ := stringFrom(call.Input["stdin"]); stdinValue != "" {
		command.Stdin = strings.NewReader(stdinValue)
	}
	stdoutBuf, stderrBuf, stdoutWriter, stderrWriter := rt.NewExecCapture(rt.ExecOutputLimit())
	command.Stdout = stdoutWriter
	command.Stderr = stderrWriter
	if err := command.Run(); err != nil {
		message := strings.TrimSpace(stderrBuf.String())
		if message == "" {
			message = err.Error()
		}
		return contextengine.ToolResult{}, fmt.Errorf("exec.run %q failed: %s", commandName, message)
	}

	content := strings.TrimSpace(stdoutBuf.String())
	if strings.TrimSpace(stderrBuf.String()) != "" {
		if content == "" {
			content = strings.TrimSpace(stderrBuf.String())
		} else {
			content = content + "\nstderr:\n" + strings.TrimSpace(stderrBuf.String())
		}
	}
	if content == "" {
		content = fmt.Sprintf("command %q completed successfully", commandName)
	}
	body, err := json.MarshalIndent(map[string]any{
		"command": commandName,
		"dir":     rt.DisplayPath(workingDir),
		"stdout":  strings.TrimSpace(stdoutBuf.String()),
		"stderr":  strings.TrimSpace(stderrBuf.String()),
		"content": content,
	}, "", "  ")
	if err != nil {
		return contextengine.ToolResult{}, err
	}
	return contextengine.ToolResult{
		ToolName:   call.Name,
		ToolCallID: call.ID,
		Content:    string(body),
	}, nil
}

func handleShell(ctx context.Context, rt Runtime, call agent.ToolCall) (contextengine.ToolResult, error) {
	commandStr, err := requiredString(call.Input, "command")
	if err != nil {
		return contextengine.ToolResult{}, err
	}
	dirValue, err := stringFrom(call.Input["dir"])
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("invalid exec.shell dir: %w", err)
	}
	workingDir := rt.RootAbs()
	if strings.TrimSpace(dirValue) != "" {
		workingDir, err = rt.ResolvePath(dirValue)
		if err != nil {
			return contextengine.ToolResult{}, err
		}
	}
	timeout, err := timeoutFrom(call.Input["timeout_seconds"], rt.DefaultExecTimeout())
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("invalid exec.shell timeout_seconds: %w", err)
	}
	timeout = rt.EffectiveExecTimeout(timeout)
	if err := rt.ValidateShellInvocation(commandStr); err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("exec.shell: %w", err)
	}
	execCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	shell := "/bin/sh"
	if envShell := os.Getenv("SHELL"); envShell != "" {
		shell = envShell
	}
	command := exec.CommandContext(execCtx, shell, "-c", commandStr)
	command.Dir = workingDir
	if stdinValue, _ := stringFrom(call.Input["stdin"]); stdinValue != "" {
		command.Stdin = strings.NewReader(stdinValue)
	}
	stdoutBuf, stderrBuf, stdoutWriter, stderrWriter := rt.NewExecCapture(rt.ExecOutputLimit())
	command.Stdout = stdoutWriter
	command.Stderr = stderrWriter

	exitCode := 0
	if err := command.Run(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			return contextengine.ToolResult{}, fmt.Errorf("exec.shell failed: %w", err)
		}
	}

	body, err := json.MarshalIndent(map[string]any{
		"command":   commandStr,
		"dir":       rt.DisplayPath(workingDir),
		"stdout":    strings.TrimSpace(stdoutBuf.String()),
		"stderr":    strings.TrimSpace(stderrBuf.String()),
		"exit_code": exitCode,
	}, "", "  ")
	if err != nil {
		return contextengine.ToolResult{}, err
	}
	return contextengine.ToolResult{
		ToolName:   call.Name,
		ToolCallID: call.ID,
		Content:    string(body),
	}, nil
}

func handleWhich(_ context.Context, _ Runtime, call agent.ToolCall) (contextengine.ToolResult, error) {
	name, err := requiredString(call.Input, "name")
	if err != nil {
		return contextengine.ToolResult{}, err
	}
	path, lookupErr := exec.LookPath(name)
	found := lookupErr == nil

	body, err := json.MarshalIndent(map[string]any{
		"name":  name,
		"path":  path,
		"found": found,
	}, "", "  ")
	if err != nil {
		return contextengine.ToolResult{}, err
	}
	return contextengine.ToolResult{
		ToolName:   call.Name,
		ToolCallID: call.ID,
		Content:    string(body),
	}, nil
}

func handleScript(ctx context.Context, rt Runtime, call agent.ToolCall) (contextengine.ToolResult, error) {
	scriptContent, err := requiredString(call.Input, "script")
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("exec.script: %w", err)
	}

	interpreter, _ := stringFrom(call.Input["interpreter"])
	if strings.TrimSpace(interpreter) == "" {
		interpreter = "/bin/sh"
	}

	dirValue, _ := stringFrom(call.Input["dir"])
	workingDir := rt.RootAbs()
	if strings.TrimSpace(dirValue) != "" {
		workingDir, err = rt.ResolvePath(dirValue)
		if err != nil {
			return contextengine.ToolResult{}, fmt.Errorf("exec.script: %w", err)
		}
	}

	timeout, err := timeoutFrom(call.Input["timeout_seconds"], rt.DefaultExecTimeout())
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("exec.script: invalid timeout_seconds: %w", err)
	}
	timeout = rt.EffectiveExecTimeout(timeout)
	if err := rt.ValidateExecInvocation(interpreter, nil); err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("exec.script: %w", err)
	}

	// Write script to a temporary file.
	tmpFile, err := os.CreateTemp("", "hopclaw-script-*")
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("exec.script: failed to create temp file: %w", err)
	}
	tmpPath := tmpFile.Name()
	defer os.Remove(tmpPath)

	if _, err := tmpFile.WriteString(scriptContent); err != nil {
		tmpFile.Close()
		return contextengine.ToolResult{}, fmt.Errorf("exec.script: failed to write script: %w", err)
	}
	tmpFile.Close()

	if err := os.Chmod(tmpPath, 0700); err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("exec.script: failed to chmod temp file: %w", err)
	}

	execCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(execCtx, interpreter, tmpPath)
	cmd.Dir = workingDir

	stdoutBuf, stderrBuf, stdoutWriter, stderrWriter := rt.NewExecCapture(rt.ExecOutputLimit())
	cmd.Stdout = stdoutWriter
	cmd.Stderr = stderrWriter

	exitCode := 0
	if err := cmd.Run(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			return contextengine.ToolResult{}, fmt.Errorf("exec.script: %w", err)
		}
	}

	return rt.JSONResult(call, map[string]any{
		"script":      scriptContent,
		"interpreter": interpreter,
		"dir":         rt.DisplayPath(workingDir),
		"stdout":      strings.TrimSpace(stdoutBuf.String()),
		"stderr":      strings.TrimSpace(stderrBuf.String()),
		"exit_code":   exitCode,
	})
}

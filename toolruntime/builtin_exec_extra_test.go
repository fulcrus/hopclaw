package toolruntime

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/fulcrus/hopclaw/agent"
)

// ---------------------------------------------------------------------------
// exec.script tests
// ---------------------------------------------------------------------------

func TestExecScriptBasic(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	builtins := NewBuiltins(BuiltinsConfig{Root: root})
	results, err := builtins.ExecuteBatch(context.Background(), &agent.Run{ID: "run-1"}, &agent.Session{ID: "sess-1"}, []agent.ToolCall{{
		ID:   "call-script",
		Name: "exec.script",
		Input: map[string]any{
			"script": "echo 'hello from script'",
		},
	}})
	if err != nil {
		t.Fatalf("exec.script error = %v", err)
	}

	var payload struct {
		Script      string `json:"script"`
		Interpreter string `json:"interpreter"`
		Stdout      string `json:"stdout"`
		Stderr      string `json:"stderr"`
		ExitCode    int    `json:"exit_code"`
	}
	if err := json.Unmarshal([]byte(results[0].Content), &payload); err != nil {
		t.Fatalf("Unmarshal error = %v", err)
	}
	if payload.Stdout != "hello from script" {
		t.Fatalf("stdout = %q, want 'hello from script'", payload.Stdout)
	}
	if payload.ExitCode != 0 {
		t.Fatalf("exit_code = %d, want 0", payload.ExitCode)
	}
	if payload.Interpreter != "/bin/sh" {
		t.Fatalf("interpreter = %q, want /bin/sh", payload.Interpreter)
	}
}

func TestExecScriptCustomInterpreter(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	builtins := NewBuiltins(BuiltinsConfig{Root: root})
	results, err := builtins.ExecuteBatch(context.Background(), &agent.Run{ID: "run-1"}, &agent.Session{ID: "sess-1"}, []agent.ToolCall{{
		ID:   "call-script-bash",
		Name: "exec.script",
		Input: map[string]any{
			"script":      "echo bash-ok",
			"interpreter": "/bin/sh",
		},
	}})
	if err != nil {
		t.Fatalf("exec.script error = %v", err)
	}

	var payload struct {
		Interpreter string `json:"interpreter"`
		Stdout      string `json:"stdout"`
	}
	json.Unmarshal([]byte(results[0].Content), &payload)
	if payload.Interpreter != "/bin/sh" {
		t.Fatalf("interpreter = %q", payload.Interpreter)
	}
	if payload.Stdout != "bash-ok" {
		t.Fatalf("stdout = %q", payload.Stdout)
	}
}

func TestExecScriptNonZeroExit(t *testing.T) {
	t.Parallel()

	builtins := NewBuiltins(BuiltinsConfig{Root: t.TempDir()})
	results, err := builtins.ExecuteBatch(context.Background(), &agent.Run{ID: "run-1"}, &agent.Session{ID: "sess-1"}, []agent.ToolCall{{
		ID:   "call-script-fail",
		Name: "exec.script",
		Input: map[string]any{
			"script": "exit 42",
		},
	}})
	if err != nil {
		t.Fatalf("exec.script error = %v", err)
	}

	var payload struct {
		ExitCode int `json:"exit_code"`
	}
	json.Unmarshal([]byte(results[0].Content), &payload)
	if payload.ExitCode != 42 {
		t.Fatalf("exit_code = %d, want 42", payload.ExitCode)
	}
}

func TestExecScriptWithStderr(t *testing.T) {
	t.Parallel()

	builtins := NewBuiltins(BuiltinsConfig{Root: t.TempDir()})
	results, err := builtins.ExecuteBatch(context.Background(), &agent.Run{ID: "run-1"}, &agent.Session{ID: "sess-1"}, []agent.ToolCall{{
		ID:   "call-script-stderr",
		Name: "exec.script",
		Input: map[string]any{
			"script": "echo error-msg >&2",
		},
	}})
	if err != nil {
		t.Fatalf("exec.script error = %v", err)
	}

	var payload struct {
		Stderr string `json:"stderr"`
	}
	json.Unmarshal([]byte(results[0].Content), &payload)
	if payload.Stderr != "error-msg" {
		t.Fatalf("stderr = %q, want 'error-msg'", payload.Stderr)
	}
}

func TestExecScriptMultiLine(t *testing.T) {
	t.Parallel()

	builtins := NewBuiltins(BuiltinsConfig{Root: t.TempDir()})
	script := `#!/bin/sh
x=5
y=10
echo $((x + y))
`
	results, err := builtins.ExecuteBatch(context.Background(), &agent.Run{ID: "run-1"}, &agent.Session{ID: "sess-1"}, []agent.ToolCall{{
		ID:   "call-script-multi",
		Name: "exec.script",
		Input: map[string]any{
			"script": script,
		},
	}})
	if err != nil {
		t.Fatalf("exec.script error = %v", err)
	}

	var payload struct {
		Stdout   string `json:"stdout"`
		ExitCode int    `json:"exit_code"`
	}
	json.Unmarshal([]byte(results[0].Content), &payload)
	if payload.Stdout != "15" {
		t.Fatalf("stdout = %q, want '15'", payload.Stdout)
	}
	if payload.ExitCode != 0 {
		t.Fatalf("exit_code = %d", payload.ExitCode)
	}
}

func TestExecScriptMissingScript(t *testing.T) {
	t.Parallel()

	builtins := NewBuiltins(BuiltinsConfig{Root: t.TempDir()})
	_, err := builtins.ExecuteBatch(context.Background(), &agent.Run{ID: "run-1"}, &agent.Session{ID: "sess-1"}, []agent.ToolCall{{
		ID:    "call-script-empty",
		Name:  "exec.script",
		Input: map[string]any{},
	}})
	if err == nil {
		t.Fatal("expected error when script is missing")
	}
}

func TestExecScriptWorkingDir(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	builtins := NewBuiltins(BuiltinsConfig{Root: root})
	results, err := builtins.ExecuteBatch(context.Background(), &agent.Run{ID: "run-1"}, &agent.Session{ID: "sess-1"}, []agent.ToolCall{{
		ID:   "call-script-dir",
		Name: "exec.script",
		Input: map[string]any{
			"script": "pwd",
		},
	}})
	if err != nil {
		t.Fatalf("exec.script error = %v", err)
	}

	var payload struct {
		Dir    string `json:"dir"`
		Stdout string `json:"stdout"`
	}
	json.Unmarshal([]byte(results[0].Content), &payload)
	if payload.Dir == "" {
		t.Fatal("dir should not be empty")
	}
}

package toolruntime

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/fulcrus/hopclaw/agent"
	"github.com/fulcrus/hopclaw/contextengine"
)

func TestBuiltinsWriteAndReadFile(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	builtins := NewBuiltins(BuiltinsConfig{
		Root:         root,
		MaxReadBytes: 1024,
	})

	if _, err := builtins.ExecuteBatch(context.Background(), &agent.Run{ID: "run-1"}, &agent.Session{ID: "sess-1"}, []agent.ToolCall{{
		ID:   "call-write",
		Name: "fs.write",
		Input: map[string]any{
			"path":    "notes/todo.txt",
			"content": "ship it",
		},
	}}); err != nil {
		t.Fatalf("ExecuteBatch(fs.write) error = %v", err)
	}

	results, err := builtins.ExecuteBatch(context.Background(), &agent.Run{ID: "run-1"}, &agent.Session{ID: "sess-1"}, []agent.ToolCall{{
		ID:   "call-read",
		Name: "fs.read",
		Input: map[string]any{
			"path": "notes/todo.txt",
		},
	}})
	if err != nil {
		t.Fatalf("ExecuteBatch(fs.read) error = %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("len(results) = %d", len(results))
	}
	var readPayload struct {
		Path    string `json:"path"`
		Content string `json:"content"`
	}
	if err := json.Unmarshal([]byte(results[0].Content), &readPayload); err != nil {
		t.Fatalf("json.Unmarshal(fs.read) error = %v", err)
	}
	if readPayload.Path != "notes/todo.txt" || readPayload.Content != "ship it" {
		t.Fatalf("fs.read payload = %#v", readPayload)
	}
	data, err := os.ReadFile(filepath.Join(root, "notes", "todo.txt"))
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if string(data) != "ship it" {
		t.Fatalf("file content = %q", string(data))
	}
	definitions := builtins.ToolDefinitions(nil)
	if len(definitions) < 200 {
		t.Fatalf("len(ToolDefinitions) = %d", len(definitions))
	}
	seen := make(map[string]struct{}, len(definitions))
	for _, definition := range definitions {
		seen[definition.Name] = struct{}{}
	}
	for _, name := range []string{"fs.diff", "fs.changes", "fs.revert", "net.serve"} {
		if _, ok := seen[name]; ok {
			t.Fatalf("unexpected runtime-only/disabled definition %q in bare builtins", name)
		}
	}
	if bound, ok := builtins.ResolveTool(nil, "fs.write"); !ok || bound.Manifest.SideEffectClass != "local_write" {
		t.Fatalf("ResolveTool(fs.write) = %#v, %v", bound, ok)
	}
	if _, ok := builtins.ResolveTool(nil, "fs.diff"); ok {
		t.Fatal("ResolveTool(fs.diff) should be hidden on bare builtins")
	}
	if _, ok := builtins.ResolveTool(nil, "net.serve"); ok {
		t.Fatal("ResolveTool(net.serve) should be hidden")
	}
}

func TestBuiltinsRejectPathEscape(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	builtins := NewBuiltins(BuiltinsConfig{Root: root})
	_, err := builtins.ExecuteBatch(context.Background(), &agent.Run{ID: "run-1"}, &agent.Session{ID: "sess-1"}, []agent.ToolCall{{
		ID:   "call-escape",
		Name: "fs.read",
		Input: map[string]any{
			"path": "../secret.txt",
		},
	}})
	if err == nil || !strings.Contains(err.Error(), "escapes builtin root") {
		t.Fatalf("ExecuteBatch(fs.read escape) error = %v", err)
	}
}

func TestBuiltinsRunCommand(t *testing.T) {
	t.Parallel()

	builtins := NewBuiltins(BuiltinsConfig{Root: t.TempDir()})
	results, err := builtins.ExecuteBatch(context.Background(), &agent.Run{ID: "run-1"}, &agent.Session{ID: "sess-1"}, []agent.ToolCall{{
		ID:   "call-exec",
		Name: "exec.run",
		Input: map[string]any{
			"command": "sh",
			"args":    []any{"-c", "printf builtin-ok"},
		},
	}})
	if err != nil {
		t.Fatalf("ExecuteBatch(exec.run) error = %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("len(results) = %d", len(results))
	}
	var payload struct {
		Command string `json:"command"`
		Stdout  string `json:"stdout"`
		Content string `json:"content"`
	}
	if err := json.Unmarshal([]byte(results[0].Content), &payload); err != nil {
		t.Fatalf("json.Unmarshal(exec.run) error = %v", err)
	}
	if payload.Command != "sh" || payload.Stdout != "builtin-ok" || payload.Content != "builtin-ok" {
		t.Fatalf("exec.run payload = %#v", payload)
	}
}

func TestBuiltinsListStatAndEdit(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "notes"), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "notes", "todo.txt"), []byte("hello world"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	builtins := NewBuiltins(BuiltinsConfig{
		Root:         root,
		MaxReadBytes: 1024,
	})

	listResults, err := builtins.ExecuteBatch(context.Background(), &agent.Run{ID: "run-1"}, &agent.Session{ID: "sess-1"}, []agent.ToolCall{{
		ID:   "call-list",
		Name: "fs.list",
		Input: map[string]any{
			"path": "notes",
		},
	}})
	if err != nil {
		t.Fatalf("ExecuteBatch(fs.list) error = %v", err)
	}
	var listPayload struct {
		Count   int `json:"count"`
		Entries []struct {
			Path string `json:"path"`
			Type string `json:"type"`
		} `json:"entries"`
	}
	if err := json.Unmarshal([]byte(listResults[0].Content), &listPayload); err != nil {
		t.Fatalf("json.Unmarshal(fs.list) error = %v", err)
	}
	if listPayload.Count != 1 || listPayload.Entries[0].Path != "notes/todo.txt" {
		t.Fatalf("fs.list payload = %#v", listPayload)
	}

	statResults, err := builtins.ExecuteBatch(context.Background(), &agent.Run{ID: "run-1"}, &agent.Session{ID: "sess-1"}, []agent.ToolCall{{
		ID:   "call-stat",
		Name: "fs.stat",
		Input: map[string]any{
			"path": "notes/todo.txt",
		},
	}})
	if err != nil {
		t.Fatalf("ExecuteBatch(fs.stat) error = %v", err)
	}
	var statPayload struct {
		Path string `json:"path"`
		Type string `json:"type"`
		Size int64  `json:"size"`
	}
	if err := json.Unmarshal([]byte(statResults[0].Content), &statPayload); err != nil {
		t.Fatalf("json.Unmarshal(fs.stat) error = %v", err)
	}
	if statPayload.Path != "notes/todo.txt" || statPayload.Type != "file" || statPayload.Size == 0 {
		t.Fatalf("fs.stat payload = %#v", statPayload)
	}

	if _, err := builtins.ExecuteBatch(context.Background(), &agent.Run{ID: "run-1"}, &agent.Session{ID: "sess-1"}, []agent.ToolCall{{
		ID:   "call-edit",
		Name: "fs.edit",
		Input: map[string]any{
			"path":     "notes/todo.txt",
			"old_text": "world",
			"new_text": "openclaw",
		},
	}}); err != nil {
		t.Fatalf("ExecuteBatch(fs.edit) error = %v", err)
	}
	data, err := os.ReadFile(filepath.Join(root, "notes", "todo.txt"))
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if string(data) != "hello openclaw" {
		t.Fatalf("edited file content = %q", string(data))
	}
}

func TestBuiltinsFindAndGrep(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "notes", "nested"), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "notes", "nested", "todo.txt"), []byte("alpha\nneedle here\nomega\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(todo.txt) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "notes", "nested", "other.md"), []byte("not this one\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(other.md) error = %v", err)
	}

	builtins := NewBuiltins(BuiltinsConfig{
		Root:         root,
		MaxReadBytes: 1024,
	})

	findResults, err := builtins.ExecuteBatch(context.Background(), &agent.Run{ID: "run-1"}, &agent.Session{ID: "sess-1"}, []agent.ToolCall{{
		ID:   "call-find",
		Name: "fs.find",
		Input: map[string]any{
			"path":      "notes",
			"pattern":   "*.txt",
			"glob":      true,
			"recursive": true,
		},
	}})
	if err != nil {
		t.Fatalf("ExecuteBatch(fs.find) error = %v", err)
	}
	var findPayload struct {
		Count   int `json:"count"`
		Matches []struct {
			Path string `json:"path"`
			Type string `json:"type"`
		} `json:"matches"`
	}
	if err := json.Unmarshal([]byte(findResults[0].Content), &findPayload); err != nil {
		t.Fatalf("json.Unmarshal(fs.find) error = %v", err)
	}
	if findPayload.Count != 1 || findPayload.Matches[0].Path != "notes/nested/todo.txt" {
		t.Fatalf("fs.find payload = %#v", findPayload)
	}

	grepResults, err := builtins.ExecuteBatch(context.Background(), &agent.Run{ID: "run-1"}, &agent.Session{ID: "sess-1"}, []agent.ToolCall{{
		ID:   "call-grep",
		Name: "fs.grep",
		Input: map[string]any{
			"path":      "notes",
			"pattern":   "needle",
			"recursive": true,
		},
	}})
	if err != nil {
		t.Fatalf("ExecuteBatch(fs.grep) error = %v", err)
	}
	var grepPayload struct {
		Count   int `json:"count"`
		Matches []struct {
			Path    string `json:"path"`
			Line    int    `json:"line"`
			Content string `json:"content"`
		} `json:"matches"`
	}
	if err := json.Unmarshal([]byte(grepResults[0].Content), &grepPayload); err != nil {
		t.Fatalf("json.Unmarshal(fs.grep) error = %v", err)
	}
	if grepPayload.Count != 1 || grepPayload.Matches[0].Path != "notes/nested/todo.txt" || grepPayload.Matches[0].Line != 2 {
		t.Fatalf("fs.grep payload = %#v", grepPayload)
	}
}

func TestBuiltinsTreeAndHash(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "notes", "nested"), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	payload := []byte("hash-me")
	if err := os.WriteFile(filepath.Join(root, "notes", "nested", "todo.txt"), payload, 0o644); err != nil {
		t.Fatalf("WriteFile(todo.txt) error = %v", err)
	}

	builtins := NewBuiltins(BuiltinsConfig{Root: root, MaxReadBytes: 1024})
	treeResults, err := builtins.ExecuteBatch(context.Background(), &agent.Run{ID: "run-1"}, &agent.Session{ID: "sess-1"}, []agent.ToolCall{{
		ID:   "call-tree",
		Name: "fs.tree",
		Input: map[string]any{
			"path":      "notes",
			"max_depth": 2,
		},
	}})
	if err != nil {
		t.Fatalf("ExecuteBatch(fs.tree) error = %v", err)
	}
	var treePayload struct {
		Count   int `json:"count"`
		Entries []struct {
			Path  string `json:"path"`
			Depth int    `json:"depth"`
		} `json:"entries"`
	}
	if err := json.Unmarshal([]byte(treeResults[0].Content), &treePayload); err != nil {
		t.Fatalf("json.Unmarshal(fs.tree) error = %v", err)
	}
	if treePayload.Count < 2 {
		t.Fatalf("fs.tree payload = %#v", treePayload)
	}
	foundFile := false
	for _, entry := range treePayload.Entries {
		if entry.Path == "notes/nested/todo.txt" && entry.Depth == 1 {
			foundFile = true
			break
		}
	}
	if !foundFile {
		t.Fatalf("fs.tree entries = %#v", treePayload.Entries)
	}

	hashResults, err := builtins.ExecuteBatch(context.Background(), &agent.Run{ID: "run-1"}, &agent.Session{ID: "sess-1"}, []agent.ToolCall{{
		ID:   "call-hash",
		Name: "fs.hash",
		Input: map[string]any{
			"path": "notes/nested/todo.txt",
		},
	}})
	if err != nil {
		t.Fatalf("ExecuteBatch(fs.hash) error = %v", err)
	}
	var hashPayload struct {
		Path      string `json:"path"`
		Algorithm string `json:"algorithm"`
		Hash      string `json:"hash"`
	}
	if err := json.Unmarshal([]byte(hashResults[0].Content), &hashPayload); err != nil {
		t.Fatalf("json.Unmarshal(fs.hash) error = %v", err)
	}
	expected := fmt.Sprintf("%x", sha256.Sum256(payload))
	if hashPayload.Path != "notes/nested/todo.txt" || hashPayload.Algorithm != "sha256" || hashPayload.Hash != expected {
		t.Fatalf("fs.hash payload = %#v, want hash %q", hashPayload, expected)
	}
}

func TestBuiltinsPatch(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "todo.txt"), []byte("before\n"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	builtins := NewBuiltins(BuiltinsConfig{Root: root})
	patch := `diff --git a/todo.txt b/todo.txt
index 0000000..1111111 100644
--- a/todo.txt
+++ b/todo.txt
@@ -1 +1 @@
-before
+after
`
	results, err := builtins.ExecuteBatch(context.Background(), &agent.Run{ID: "run-1"}, &agent.Session{ID: "sess-1"}, []agent.ToolCall{{
		ID:   "call-patch",
		Name: "fs.patch",
		Input: map[string]any{
			"patch": patch,
		},
	}})
	if err != nil {
		t.Fatalf("ExecuteBatch(fs.patch) error = %v", err)
	}
	var payload struct {
		AppliedFiles []string `json:"applied_files"`
		FileCount    int      `json:"file_count"`
		Reverse      bool     `json:"reverse"`
	}
	if err := json.Unmarshal([]byte(results[0].Content), &payload); err != nil {
		t.Fatalf("json.Unmarshal(fs.patch) error = %v", err)
	}
	if payload.FileCount != 1 || len(payload.AppliedFiles) != 1 || payload.AppliedFiles[0] != "todo.txt" || payload.Reverse {
		t.Fatalf("fs.patch payload = %#v", payload)
	}
	data, err := os.ReadFile(filepath.Join(root, "todo.txt"))
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if string(data) != "after\n" {
		t.Fatalf("patched file content = %q", string(data))
	}
}

func TestBuiltinsPatchCreatesFileWithoutGit(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	builtins := NewBuiltins(BuiltinsConfig{Root: root})
	patch := `--- /dev/null
+++ a/new.txt
@@ -0,0 +1 @@
+hello
`
	results, err := builtins.ExecuteBatch(context.Background(), &agent.Run{ID: "run-create"}, &agent.Session{ID: "sess-create"}, []agent.ToolCall{{
		ID:   "call-patch-create",
		Name: "fs.patch",
		Input: map[string]any{
			"patch": patch,
		},
	}})
	if err != nil {
		t.Fatalf("ExecuteBatch(fs.patch create) error = %v", err)
	}
	var payload struct {
		AppliedFiles []string `json:"applied_files"`
	}
	if err := json.Unmarshal([]byte(results[0].Content), &payload); err != nil {
		t.Fatalf("json.Unmarshal(fs.patch create) error = %v", err)
	}
	if len(payload.AppliedFiles) != 1 || payload.AppliedFiles[0] != "new.txt" {
		t.Fatalf("fs.patch create payload = %#v", payload)
	}
	data, err := os.ReadFile(filepath.Join(root, "new.txt"))
	if err != nil {
		t.Fatalf("ReadFile(new.txt) error = %v", err)
	}
	if string(data) != "hello\n" {
		t.Fatalf("created file content = %q", string(data))
	}
}

func TestBuiltinsExposeOutputSchemas(t *testing.T) {
	t.Parallel()

	builtins := NewBuiltins(BuiltinsConfig{Root: t.TempDir()})
	definitions := builtins.ToolDefinitions(nil)
	for _, definition := range definitions {
		bound, ok := builtins.ResolveTool(nil, definition.Name)
		if !ok {
			t.Fatalf("ResolveTool(%q) failed", definition.Name)
		}
		if len(bound.Manifest.InputSchema) == 0 {
			t.Fatalf("tool %q missing input schema", definition.Name)
		}
		if len(bound.Manifest.OutputSchema) == 0 {
			t.Fatalf("tool %q missing output schema", definition.Name)
		}
	}
}

func TestBuiltinsExecShellAndWhich(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	builtins := NewBuiltins(BuiltinsConfig{Root: root})
	ctx := context.Background()
	run := &agent.Run{ID: "run-1"}
	sess := &agent.Session{ID: "sess-1"}

	// exec.shell
	results, err := builtins.ExecuteBatch(ctx, run, sess, []agent.ToolCall{{
		ID: "call-shell", Name: "exec.shell",
		Input: map[string]any{"command": "echo hello && echo world"},
	}})
	if err != nil {
		t.Fatalf("exec.shell error: %v", err)
	}
	var shellResult struct {
		Stdout   string `json:"stdout"`
		ExitCode int    `json:"exit_code"`
	}
	json.Unmarshal([]byte(results[0].Content), &shellResult)
	if shellResult.Stdout != "hello\nworld" {
		t.Fatalf("exec.shell stdout = %q", shellResult.Stdout)
	}
	if shellResult.ExitCode != 0 {
		t.Fatalf("exec.shell exit_code = %d", shellResult.ExitCode)
	}

	// exec.shell with non-zero exit
	results, err = builtins.ExecuteBatch(ctx, run, sess, []agent.ToolCall{{
		ID: "call-shell-fail", Name: "exec.shell",
		Input: map[string]any{"command": "exit 42"},
	}})
	if err != nil {
		t.Fatalf("exec.shell error: %v", err)
	}
	json.Unmarshal([]byte(results[0].Content), &shellResult)
	if shellResult.ExitCode != 42 {
		t.Fatalf("exec.shell exit_code = %d, want 42", shellResult.ExitCode)
	}

	// exec.which — should find "ls"
	results, err = builtins.ExecuteBatch(ctx, run, sess, []agent.ToolCall{{
		ID: "call-which", Name: "exec.which",
		Input: map[string]any{"name": "ls"},
	}})
	if err != nil {
		t.Fatalf("exec.which error: %v", err)
	}
	var whichResult struct {
		Name  string `json:"name"`
		Path  string `json:"path"`
		Found bool   `json:"found"`
	}
	json.Unmarshal([]byte(results[0].Content), &whichResult)
	if !whichResult.Found || whichResult.Path == "" {
		t.Fatalf("exec.which result = %+v", whichResult)
	}

	// exec.which — nonexistent
	results, err = builtins.ExecuteBatch(ctx, run, sess, []agent.ToolCall{{
		ID: "call-which-miss", Name: "exec.which",
		Input: map[string]any{"name": "definitely-not-a-real-command-xyz"},
	}})
	if err != nil {
		t.Fatalf("exec.which error: %v", err)
	}
	json.Unmarshal([]byte(results[0].Content), &whichResult)
	if whichResult.Found {
		t.Fatalf("exec.which should not find nonexistent command")
	}
}

func TestBuiltinsDeleteMovecopymkdirAppend(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	builtins := NewBuiltins(BuiltinsConfig{Root: root})
	ctx := context.Background()
	run := &agent.Run{ID: "run-1"}
	sess := &agent.Session{ID: "sess-1"}

	exec := func(name string, input map[string]any) string {
		t.Helper()
		results, err := builtins.ExecuteBatch(ctx, run, sess, []agent.ToolCall{{
			ID: "call-" + name, Name: name, Input: input,
		}})
		if err != nil {
			t.Fatalf("%s error: %v", name, err)
		}
		return results[0].Content
	}

	// fs.mkdir
	exec("fs.mkdir", map[string]any{"path": "subdir/deep"})
	info, err := os.Stat(filepath.Join(root, "subdir", "deep"))
	if err != nil || !info.IsDir() {
		t.Fatalf("fs.mkdir did not create directory: %v", err)
	}

	// fs.write a file to work with
	exec("fs.write", map[string]any{"path": "subdir/deep/hello.txt", "content": "hello"})

	// fs.append
	content := exec("fs.append", map[string]any{"path": "subdir/deep/hello.txt", "content": " world"})
	var appendResult struct {
		BytesAppended int `json:"bytes_appended"`
	}
	json.Unmarshal([]byte(content), &appendResult)
	if appendResult.BytesAppended != 6 {
		t.Fatalf("fs.append bytes_appended = %d, want 6", appendResult.BytesAppended)
	}
	data, _ := os.ReadFile(filepath.Join(root, "subdir", "deep", "hello.txt"))
	if string(data) != "hello world" {
		t.Fatalf("fs.append result = %q", string(data))
	}

	// fs.copy
	copyResult := exec("fs.copy", map[string]any{"source": "subdir/deep/hello.txt", "destination": "subdir/copy.txt"})
	var copyPayload struct {
		BytesCopied int64 `json:"bytes_copied"`
	}
	json.Unmarshal([]byte(copyResult), &copyPayload)
	if copyPayload.BytesCopied != 11 {
		t.Fatalf("fs.copy bytes_copied = %d, want 11", copyPayload.BytesCopied)
	}
	data, _ = os.ReadFile(filepath.Join(root, "subdir", "copy.txt"))
	if string(data) != "hello world" {
		t.Fatalf("fs.copy content = %q", string(data))
	}

	// fs.move
	exec("fs.move", map[string]any{"source": "subdir/copy.txt", "destination": "moved.txt"})
	if _, err := os.Stat(filepath.Join(root, "subdir", "copy.txt")); !os.IsNotExist(err) {
		t.Fatal("fs.move: source still exists")
	}
	data, _ = os.ReadFile(filepath.Join(root, "moved.txt"))
	if string(data) != "hello world" {
		t.Fatalf("fs.move content = %q", string(data))
	}

	// fs.delete file
	exec("fs.delete", map[string]any{"path": "moved.txt"})
	if _, err := os.Stat(filepath.Join(root, "moved.txt")); !os.IsNotExist(err) {
		t.Fatal("fs.delete: file still exists")
	}

	// fs.delete recursive
	exec("fs.delete", map[string]any{"path": "subdir", "recursive": true})
	if _, err := os.Stat(filepath.Join(root, "subdir")); !os.IsNotExist(err) {
		t.Fatal("fs.delete recursive: dir still exists")
	}
}

// ---------------------------------------------------------------------------
// Concurrent ExecuteBatch tests
// ---------------------------------------------------------------------------

func TestExecuteBatchConcurrent(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	builtins := NewBuiltins(BuiltinsConfig{Root: root, MaxReadBytes: 1024})

	// Write 3 files first.
	for i := 0; i < 3; i++ {
		_, err := builtins.ExecuteBatch(context.Background(), &agent.Run{ID: "r1"}, &agent.Session{ID: "s1"}, []agent.ToolCall{{
			ID:    fmt.Sprintf("w%d", i),
			Name:  "fs.write",
			Input: map[string]any{"path": fmt.Sprintf("f%d.txt", i), "content": fmt.Sprintf("data%d", i)},
		}})
		if err != nil {
			t.Fatalf("setup write %d: %v", i, err)
		}
	}

	// Now read all 3 concurrently in one ExecuteBatch call.
	calls := make([]agent.ToolCall, 3)
	for i := 0; i < 3; i++ {
		calls[i] = agent.ToolCall{
			ID:    fmt.Sprintf("r%d", i),
			Name:  "fs.read",
			Input: map[string]any{"path": fmt.Sprintf("f%d.txt", i)},
		}
	}

	results, err := builtins.ExecuteBatch(context.Background(), &agent.Run{ID: "r1"}, &agent.Session{ID: "s1"}, calls)
	if err != nil {
		t.Fatalf("ExecuteBatch concurrent: %v", err)
	}
	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}

	// Verify results are in the correct order (matching input call order).
	for i, result := range results {
		var payload struct {
			Content string `json:"content"`
		}
		if err := json.Unmarshal([]byte(result.Content), &payload); err != nil {
			t.Fatalf("unmarshal result %d: %v", i, err)
		}
		expected := fmt.Sprintf("data%d", i)
		if payload.Content != expected {
			t.Fatalf("result[%d].content = %q, want %q", i, payload.Content, expected)
		}
	}
}

func TestExecuteBatchTimeout(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	builtins := NewBuiltins(BuiltinsConfig{Root: root, MaxReadBytes: 1024})

	// Register a slow handler that will exceed timeout.
	var called atomic.Int32
	builtins.handlers["test.slow"] = func(ctx context.Context, b *Builtins, call agent.ToolCall) (contextengine.ToolResult, error) {
		called.Add(1)
		select {
		case <-ctx.Done():
			return contextengine.ToolResult{
				ToolName:   call.Name,
				ToolCallID: call.ID,
				Content:    `{"error":"context cancelled"}`,
			}, nil
		case <-time.After(5 * time.Second):
			return contextengine.ToolResult{
				ToolName:   call.Name,
				ToolCallID: call.ID,
				Content:    `{"result":"done"}`,
			}, nil
		}
	}

	// Force a short timeout for this tool via manifest.
	builtins.tools["test.slow"] = builtins.tools["fs.read"] // copy a bound tool for structure
	// Override timeout by patching toolTimeout — the handler is registered, so it will be called.
	// Since test.slow has no manifest timeout and doesn't match any domain prefix, it gets defaultToolTimeout (30s).
	// Instead, we use a very short context to simulate timeout.
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	results, err := builtins.ExecuteBatch(ctx, &agent.Run{ID: "r1"}, &agent.Session{ID: "s1"}, []agent.ToolCall{{
		ID:   "slow1",
		Name: "test.slow",
	}})
	if err != nil {
		t.Fatalf("ExecuteBatch error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if !strings.Contains(results[0].Content, "error") {
		t.Fatalf("expected error in result, got %q", results[0].Content)
	}
	if called.Load() != 1 {
		t.Fatalf("handler called %d times, expected 1", called.Load())
	}
}

func TestExecuteBatchPreservesBatchSlotsOnPerCallFailure(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	builtins := NewBuiltins(BuiltinsConfig{Root: root, MaxReadBytes: 1024})
	if _, err := builtins.ExecuteBatch(context.Background(), &agent.Run{ID: "r1"}, &agent.Session{ID: "s1"}, []agent.ToolCall{{
		ID:    "write1",
		Name:  "fs.write",
		Input: map[string]any{"path": "ok.txt", "content": "hello"},
	}}); err != nil {
		t.Fatalf("setup write: %v", err)
	}

	results, err := builtins.ExecuteBatch(context.Background(), &agent.Run{ID: "r1"}, &agent.Session{ID: "s1"}, []agent.ToolCall{
		{ID: "bad", Name: "fs.read", Input: map[string]any{"path": "missing.txt"}},
		{ID: "good", Name: "fs.read", Input: map[string]any{"path": "ok.txt"}},
	})
	if err != nil {
		t.Fatalf("ExecuteBatch() error = %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("len(results) = %d, want 2", len(results))
	}
	if results[0].Error == nil || results[0].Error.Message == "" {
		t.Fatalf("expected error result in slot 0, got %#v", results[0])
	}
	if results[1].Error != nil {
		t.Fatalf("expected success result in slot 1, got %#v", results[1])
	}
}

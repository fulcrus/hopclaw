package toolruntime

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fulcrus/hopclaw/agent"
)

func TestEditShadowCaptureAndDiff(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	f := filepath.Join(dir, "hello.txt")
	os.WriteFile(f, []byte("line1\nline2\nline3\n"), 0o644)

	shadow := NewEditShadow()
	shadow.Capture(f)

	// Modify file after capture.
	os.WriteFile(f, []byte("line1\nchanged\nline3\n"), 0o644)

	diff := shadow.Diff(f, "hello.txt")
	if !strings.Contains(diff, "-line2") {
		t.Fatalf("diff missing deleted line:\n%s", diff)
	}
	if !strings.Contains(diff, "+changed") {
		t.Fatalf("diff missing added line:\n%s", diff)
	}
}

func TestEditShadowCaptureOnlyOnce(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	f := filepath.Join(dir, "once.txt")
	os.WriteFile(f, []byte("original\n"), 0o644)

	shadow := NewEditShadow()
	shadow.Capture(f)

	// Modify and capture again — second capture should be no-op.
	os.WriteFile(f, []byte("modified\n"), 0o644)
	shadow.Capture(f)

	// Diff should be against "original", not "modified".
	os.WriteFile(f, []byte("final\n"), 0o644)
	diff := shadow.Diff(f, "once.txt")
	if !strings.Contains(diff, "-original") {
		t.Fatalf("expected diff against original, not intermediate:\n%s", diff)
	}
}

func TestEditShadowCreatedFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	f := filepath.Join(dir, "new.txt")

	shadow := NewEditShadow()
	// Capture before file exists → marks as "created".
	shadow.Capture(f)

	os.WriteFile(f, []byte("hello\n"), 0o644)

	changes := shadow.Changes()
	if len(changes) != 1 || changes[0].Status != "created" {
		t.Fatalf("expected 1 created entry, got %+v", changes)
	}

	diff := shadow.Diff(f, "new.txt")
	if !strings.Contains(diff, "+hello") {
		t.Fatalf("diff for new file missing added content:\n%s", diff)
	}
}

func TestEditShadowDeletedFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	f := filepath.Join(dir, "bye.txt")
	os.WriteFile(f, []byte("data\n"), 0o644)

	shadow := NewEditShadow()
	shadow.Capture(f)

	os.Remove(f)

	changes := shadow.Changes()
	if len(changes) != 1 || changes[0].Status != "deleted" {
		t.Fatalf("expected 1 deleted entry, got %+v", changes)
	}

	diff := shadow.Diff(f, "bye.txt")
	if !strings.Contains(diff, "-data") {
		t.Fatalf("diff for deleted file missing removed content:\n%s", diff)
	}
}

func TestEditShadowRevertModified(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	f := filepath.Join(dir, "revert.txt")
	os.WriteFile(f, []byte("original\n"), 0o644)

	shadow := NewEditShadow()
	shadow.Capture(f)

	os.WriteFile(f, []byte("modified\n"), 0o644)

	msg, err := shadow.Revert(f)
	if err != nil {
		t.Fatalf("Revert error: %v", err)
	}
	if !strings.Contains(msg, "reverted") {
		t.Fatalf("unexpected revert message: %s", msg)
	}

	data, _ := os.ReadFile(f)
	if string(data) != "original\n" {
		t.Fatalf("file not reverted: %q", string(data))
	}

	if shadow.HasChanges() {
		t.Fatal("shadow should have no changes after revert")
	}
}

func TestEditShadowRevertCreated(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	f := filepath.Join(dir, "created.txt")

	shadow := NewEditShadow()
	shadow.Capture(f)

	os.WriteFile(f, []byte("new content\n"), 0o644)

	msg, err := shadow.Revert(f)
	if err != nil {
		t.Fatalf("Revert error: %v", err)
	}
	if !strings.Contains(msg, "deleted") {
		t.Fatalf("unexpected revert message: %s", msg)
	}

	if _, err := os.Stat(f); !os.IsNotExist(err) {
		t.Fatal("file should be deleted after revert of created file")
	}
}

func TestEditShadowChangesSort(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	files := []string{"c.txt", "a.txt", "b.txt"}
	shadow := NewEditShadow()
	for _, name := range files {
		f := filepath.Join(dir, name)
		os.WriteFile(f, []byte("x\n"), 0o644)
		shadow.Capture(f)
	}

	changes := shadow.Changes()
	if len(changes) != 3 {
		t.Fatalf("expected 3 changes, got %d", len(changes))
	}
	for i := 1; i < len(changes); i++ {
		if changes[i].Path < changes[i-1].Path {
			t.Fatalf("changes not sorted: %v", changes)
		}
	}
}

func TestEditShadowDiffUnmodified(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	f := filepath.Join(dir, "same.txt")
	os.WriteFile(f, []byte("no change\n"), 0o644)

	shadow := NewEditShadow()
	shadow.Capture(f)

	// Don't modify — diff should be empty.
	diff := shadow.Diff(f, "same.txt")
	if diff != "" {
		t.Fatalf("expected empty diff for unmodified file, got:\n%s", diff)
	}
}

func TestEditShadowDiffUntracked(t *testing.T) {
	t.Parallel()
	shadow := NewEditShadow()
	diff := shadow.Diff("/nonexistent", "nonexistent")
	if diff != "" {
		t.Fatalf("expected empty diff for untracked file, got:\n%s", diff)
	}
}

func TestEditShadowRevertUntracked(t *testing.T) {
	t.Parallel()
	shadow := NewEditShadow()
	_, err := shadow.Revert("/nonexistent")
	if err == nil {
		t.Fatal("expected error reverting untracked file")
	}
}

func TestShadowExecutorPublishesShadowTools(t *testing.T) {
	t.Parallel()

	builtins := NewBuiltins(BuiltinsConfig{Root: t.TempDir()})
	wrapped := WithEditShadow()(builtins)

	provider, ok := wrapped.(agent.ToolDefinitionProvider)
	if !ok {
		t.Fatal("wrapped executor does not provide tool definitions")
	}
	definitions := provider.ToolDefinitions(nil)
	seen := make(map[string]struct{}, len(definitions))
	for _, definition := range definitions {
		seen[definition.Name] = struct{}{}
	}
	for _, name := range []string{"fs.diff", "fs.changes", "fs.revert"} {
		if _, ok := seen[name]; !ok {
			t.Fatalf("missing shadow definition %q", name)
		}
	}

	resolver, ok := wrapped.(agent.ToolResolver)
	if !ok {
		t.Fatal("wrapped executor does not resolve tools")
	}
	if _, ok := resolver.ResolveTool(nil, "fs.diff"); !ok {
		t.Fatal("ResolveTool(fs.diff) = false")
	}
}

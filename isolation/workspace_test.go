package isolation

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestNewManager(t *testing.T) {
	base := filepath.Join(t.TempDir(), "workspaces")
	mgr, err := NewManager(base)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	if mgr.baseDir != base {
		t.Errorf("baseDir = %q, want %q", mgr.baseDir, base)
	}
	info, err := os.Stat(base)
	if err != nil {
		t.Fatalf("base dir not created: %v", err)
	}
	if !info.IsDir() {
		t.Fatalf("base path is not a directory")
	}
}

func TestCreateWorkspace(t *testing.T) {
	mgr, err := NewManager(t.TempDir())
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}

	ws, err := mgr.Create("research-agent")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	if ws.ID == "" {
		t.Error("workspace ID is empty")
	}
	if ws.AgentName != "research-agent" {
		t.Errorf("AgentName = %q, want %q", ws.AgentName, "research-agent")
	}
	if ws.CreatedAt.IsZero() {
		t.Error("CreatedAt is zero")
	}
	if ws.Env == nil {
		t.Error("Env map is nil, expected initialized map")
	}
}

func TestCreateRequiresAgentName(t *testing.T) {
	mgr, err := NewManager(t.TempDir())
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}

	_, err = mgr.Create("")
	if err == nil {
		t.Fatal("expected error for empty agent name")
	}
}

func TestWorkspaceDirectoryStructure(t *testing.T) {
	mgr, err := NewManager(t.TempDir())
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}

	ws, err := mgr.Create("coder")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	for _, sub := range []string{workSubdir, artifactsSubdir, logsSubdir} {
		dir := filepath.Join(ws.WorkDir, sub)
		info, statErr := os.Stat(dir)
		if statErr != nil {
			t.Errorf("subdirectory %s not created: %v", sub, statErr)
			continue
		}
		if !info.IsDir() {
			t.Errorf("%s is not a directory", sub)
		}
	}
}

func TestWorkspaceFileReadWrite(t *testing.T) {
	mgr, err := NewManager(t.TempDir())
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}

	ws, err := mgr.Create("writer")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	content := []byte("hello workspace")
	if err := ws.WriteFile("notes.txt", content); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	got, err := ws.ReadFile("notes.txt")
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(got) != string(content) {
		t.Errorf("ReadFile = %q, want %q", got, content)
	}
}

func TestWorkspaceFileReadWriteNestedPath(t *testing.T) {
	mgr, err := NewManager(t.TempDir())
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}

	ws, err := mgr.Create("nested-writer")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	content := []byte("deep file")
	if err := ws.WriteFile(filepath.Join("sub", "dir", "deep.txt"), content); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	got, err := ws.ReadFile(filepath.Join("sub", "dir", "deep.txt"))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(got) != string(content) {
		t.Errorf("ReadFile = %q, want %q", got, content)
	}
}

func TestWorkspaceRejectsUnsafePaths(t *testing.T) {
	mgr, err := NewManager(t.TempDir())
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}

	ws, err := mgr.Create("safe-writer")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	tests := []string{
		"",
		"../escape.txt",
		filepath.Join("..", "escape.txt"),
		"/tmp/escape.txt",
		filepath.Clean(filepath.Join("nested", "..", "..", "escape.txt")),
	}
	for _, name := range tests {
		t.Run(strings.ReplaceAll(name, string(filepath.Separator), "_"), func(t *testing.T) {
			if err := ws.WriteFile(name, []byte("blocked")); err == nil {
				t.Fatalf("WriteFile(%q) should reject unsafe path", name)
			}
			if _, err := ws.ReadFile(name); err == nil {
				t.Fatalf("ReadFile(%q) should reject unsafe path", name)
			}
		})
	}
}

func TestWorkspaceListFiles(t *testing.T) {
	mgr, err := NewManager(t.TempDir())
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}

	ws, err := mgr.Create("lister")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	// Write a few files (including a nested one).
	for _, name := range []string{"b.txt", "a.txt", filepath.Join("sub", "c.txt")} {
		if err := ws.WriteFile(name, []byte(name)); err != nil {
			t.Fatalf("WriteFile(%s): %v", name, err)
		}
	}

	files, err := ws.ListFiles()
	if err != nil {
		t.Fatalf("ListFiles: %v", err)
	}

	// Expect sorted alphabetically: a.txt, b.txt, sub/c.txt
	want := []string{"a.txt", "b.txt", filepath.Join("sub", "c.txt")}
	if len(files) != len(want) {
		t.Fatalf("ListFiles returned %d files, want %d: %v", len(files), len(want), files)
	}
	for i, f := range files {
		if f != want[i] {
			t.Errorf("files[%d] = %q, want %q", i, f, want[i])
		}
	}
}

func TestWorkspaceListFilesEmpty(t *testing.T) {
	mgr, err := NewManager(t.TempDir())
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}

	ws, err := mgr.Create("empty-lister")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	files, err := ws.ListFiles()
	if err != nil {
		t.Fatalf("ListFiles: %v", err)
	}
	if len(files) != 0 {
		t.Errorf("ListFiles returned %d files, want 0", len(files))
	}
}

func TestGetWorkspace(t *testing.T) {
	mgr, err := NewManager(t.TempDir())
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}

	ws, err := mgr.Create("getter")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, ok := mgr.Get(ws.ID)
	if !ok {
		t.Fatal("Get returned false for existing workspace")
	}
	if got.ID != ws.ID {
		t.Errorf("Get ID = %q, want %q", got.ID, ws.ID)
	}

	_, ok = mgr.Get("nonexistent")
	if ok {
		t.Error("Get returned true for nonexistent workspace")
	}
}

func TestCleanup(t *testing.T) {
	mgr, err := NewManager(t.TempDir())
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}

	ws, err := mgr.Create("cleaner")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	// Write a file so the directory is not empty.
	if err := ws.WriteFile("data.txt", []byte("data")); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	if err := mgr.Cleanup(ws.ID); err != nil {
		t.Fatalf("Cleanup: %v", err)
	}

	// Directory should be gone.
	if _, statErr := os.Stat(ws.WorkDir); !os.IsNotExist(statErr) {
		t.Errorf("workspace directory still exists after Cleanup")
	}

	// Manager should no longer know about it.
	if _, ok := mgr.Get(ws.ID); ok {
		t.Error("workspace still registered after Cleanup")
	}
}

func TestCleanupNotFound(t *testing.T) {
	mgr, err := NewManager(t.TempDir())
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}

	if err := mgr.Cleanup("nonexistent"); err == nil {
		t.Error("expected error for cleaning up nonexistent workspace")
	}
}

func TestCleanupByAgent(t *testing.T) {
	mgr, err := NewManager(t.TempDir())
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}

	ws1, err := mgr.Create("alpha")
	if err != nil {
		t.Fatalf("Create alpha-1: %v", err)
	}
	ws2, err := mgr.Create("alpha")
	if err != nil {
		t.Fatalf("Create alpha-2: %v", err)
	}
	wsBeta, err := mgr.Create("beta")
	if err != nil {
		t.Fatalf("Create beta: %v", err)
	}

	if err := mgr.CleanupByAgent("alpha"); err != nil {
		t.Fatalf("CleanupByAgent: %v", err)
	}

	// Both alpha workspaces should be gone.
	if _, ok := mgr.Get(ws1.ID); ok {
		t.Error("alpha workspace 1 still registered")
	}
	if _, ok := mgr.Get(ws2.ID); ok {
		t.Error("alpha workspace 2 still registered")
	}

	// Beta should still exist.
	if _, ok := mgr.Get(wsBeta.ID); !ok {
		t.Error("beta workspace was incorrectly removed")
	}
}

func TestCleanupStale(t *testing.T) {
	mgr, err := NewManager(t.TempDir())
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}

	// Create a workspace and backdate it.
	ws, err := mgr.Create("stale-agent")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	mgr.mu.Lock()
	ws.CreatedAt = time.Now().UTC().Add(-2 * time.Hour)
	mgr.mu.Unlock()

	// Create a fresh workspace.
	fresh, err := mgr.Create("fresh-agent")
	if err != nil {
		t.Fatalf("Create fresh: %v", err)
	}

	count, err := mgr.CleanupStale(1 * time.Hour)
	if err != nil {
		t.Fatalf("CleanupStale: %v", err)
	}
	if count != 1 {
		t.Errorf("CleanupStale removed %d, want 1", count)
	}

	// Stale workspace should be gone.
	if _, ok := mgr.Get(ws.ID); ok {
		t.Error("stale workspace still registered")
	}

	// Fresh workspace should remain.
	if _, ok := mgr.Get(fresh.ID); !ok {
		t.Error("fresh workspace was incorrectly removed")
	}
}

func TestListWorkspaces(t *testing.T) {
	mgr, err := NewManager(t.TempDir())
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}

	if got := mgr.List(); len(got) != 0 {
		t.Errorf("List returned %d workspaces, want 0", len(got))
	}

	_, err = mgr.Create("agent-a")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	_, err = mgr.Create("agent-b")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	all := mgr.List()
	if len(all) != 2 {
		t.Errorf("List returned %d workspaces, want 2", len(all))
	}
}

func TestListByAgent(t *testing.T) {
	mgr, err := NewManager(t.TempDir())
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}

	_, err = mgr.Create("target")
	if err != nil {
		t.Fatalf("Create target-1: %v", err)
	}
	_, err = mgr.Create("target")
	if err != nil {
		t.Fatalf("Create target-2: %v", err)
	}
	_, err = mgr.Create("other")
	if err != nil {
		t.Fatalf("Create other: %v", err)
	}

	got := mgr.ListByAgent("target")
	if len(got) != 2 {
		t.Errorf("ListByAgent(target) = %d, want 2", len(got))
	}
	for _, ws := range got {
		if ws.AgentName != "target" {
			t.Errorf("ListByAgent returned workspace with agent %q", ws.AgentName)
		}
	}

	got = mgr.ListByAgent("missing")
	if len(got) != 0 {
		t.Errorf("ListByAgent(missing) = %d, want 0", len(got))
	}
}

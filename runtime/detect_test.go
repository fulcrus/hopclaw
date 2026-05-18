package runtime

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// ---------------------------------------------------------------------------
// extractVersion
// ---------------------------------------------------------------------------

func TestExtractVersionSimple(t *testing.T) {
	t.Parallel()
	cases := []struct {
		input string
		want  string
	}{
		{"GNU bash, version 5.2.15(1)-release", "5.2.15(1)-release"},
		{"zsh 5.9 (x86_64-apple-darwin22.0)", "5.9"},
		{"fish, version 3.6.1", "3.6.1"},
		{"go version go1.21.3 darwin/arm64", ""},
		{"", ""},
		{"no version here", ""},
	}
	for _, tc := range cases {
		got := extractVersion(tc.input)
		if got != tc.want {
			t.Fatalf("extractVersion(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

func TestExtractVersionTrimsTrailingPunctuation(t *testing.T) {
	t.Parallel()
	got := extractVersion("version 1.2.3,")
	if got != "1.2.3" {
		t.Fatalf("extractVersion with trailing comma = %q, want 1.2.3", got)
	}
	got = extractVersion("version 1.2.3;")
	if got != "1.2.3" {
		t.Fatalf("extractVersion with trailing semicolon = %q, want 1.2.3", got)
	}
	got = extractVersion("version 1.2.3(extra)")
	// extractVersion trims trailing parens, so (extra) is stripped.
	if got != "1.2.3(extra" {
		t.Fatalf("extractVersion with parens = %q, want 1.2.3(extra", got)
	}
}

func TestExtractVersionRequiresDot(t *testing.T) {
	t.Parallel()
	got := extractVersion("version 42")
	if got != "" {
		t.Fatalf("extractVersion('version 42') = %q, want empty (no dot)", got)
	}
}

// ---------------------------------------------------------------------------
// detectWorkspace
// ---------------------------------------------------------------------------

func TestDetectWorkspaceWithGoMod(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module test\n"), 0644); err != nil {
		t.Fatalf("WriteFile error = %v", err)
	}

	wc := detectWorkspace(dir)
	if wc.Root != dir {
		t.Fatalf("Root = %q, want %q", wc.Root, dir)
	}
	if wc.ProjectType != "go" {
		t.Fatalf("ProjectType = %q, want go", wc.ProjectType)
	}
	found := false
	for _, m := range wc.Markers {
		if m == "go.mod" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("Markers = %v, expected go.mod", wc.Markers)
	}
}

func TestDetectWorkspaceWithPackageJSON(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "package.json"), []byte("{}"), 0644); err != nil {
		t.Fatalf("WriteFile error = %v", err)
	}

	wc := detectWorkspace(dir)
	if wc.ProjectType != "node" {
		t.Fatalf("ProjectType = %q, want node", wc.ProjectType)
	}
}

func TestDetectWorkspaceWithMultipleMarkers(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	// Create both go.mod and Makefile.
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module test\n"), 0644); err != nil {
		t.Fatalf("WriteFile error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "Makefile"), []byte("all:\n"), 0644); err != nil {
		t.Fatalf("WriteFile error = %v", err)
	}

	wc := detectWorkspace(dir)
	// ProjectType should be the first match (go.mod comes before Makefile in markers).
	if wc.ProjectType != "go" {
		t.Fatalf("ProjectType = %q, want go (first match)", wc.ProjectType)
	}
	if len(wc.Markers) < 2 {
		t.Fatalf("Markers = %v, expected at least 2", wc.Markers)
	}
}

func TestDetectWorkspaceNoMarkers(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	wc := detectWorkspace(dir)
	if wc.Root != dir {
		t.Fatalf("Root = %q, want %q", wc.Root, dir)
	}
	if wc.ProjectType != "" {
		t.Fatalf("ProjectType = %q, want empty", wc.ProjectType)
	}
	if len(wc.Markers) != 0 {
		t.Fatalf("Markers = %v, want empty", wc.Markers)
	}
}

// ---------------------------------------------------------------------------
// DetectContext
// ---------------------------------------------------------------------------

func TestDetectContextSetsGOOSAndGOARCH(t *testing.T) {
	t.Parallel()
	ctx := DetectContext("")
	if ctx.GOOS != runtime.GOOS {
		t.Fatalf("GOOS = %q, want %q", ctx.GOOS, runtime.GOOS)
	}
	if ctx.GOARCH != runtime.GOARCH {
		t.Fatalf("GOARCH = %q, want %q", ctx.GOARCH, runtime.GOARCH)
	}
}

func TestDetectContextEmptyWorkDir(t *testing.T) {
	t.Parallel()
	ctx := DetectContext("")
	// Git and Workspace should have zero values when workDir is empty.
	if ctx.Git.InRepo {
		t.Fatal("expected Git.InRepo=false with empty workDir")
	}
	if ctx.Workspace.Root != "" {
		t.Fatalf("Workspace.Root = %q, want empty", ctx.Workspace.Root)
	}
}

func TestDetectContextWithWorkDir(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	ctx := DetectContext(dir)
	if ctx.Workspace.Root != dir {
		t.Fatalf("Workspace.Root = %q, want %q", ctx.Workspace.Root, dir)
	}
}

// ---------------------------------------------------------------------------
// detectIDE — environment variable detection
// ---------------------------------------------------------------------------

func TestDetectIDEVSCodeTermProgram(t *testing.T) {
	// Cannot use t.Parallel() with t.Setenv.
	t.Setenv("TERM_PROGRAM", "vscode")
	t.Setenv("TERM_PROGRAM_VERSION", "1.85.0")
	t.Setenv("VSCODE_PID", "")
	t.Setenv("VSCODE_IPC_HOOK", "")
	t.Setenv("CURSOR_TRACE_ID", "")
	t.Setenv("TERMINAL_EMULATOR", "")

	ide := detectIDE()
	if ide.Name != "vscode" {
		t.Fatalf("Name = %q, want vscode", ide.Name)
	}
	if ide.Version != "1.85.0" {
		t.Fatalf("Version = %q, want 1.85.0", ide.Version)
	}
}

func TestDetectIDEVSCodePID(t *testing.T) {
	t.Setenv("TERM_PROGRAM", "")
	t.Setenv("VSCODE_PID", "12345")
	t.Setenv("VSCODE_IPC_HOOK", "")
	t.Setenv("CURSOR_TRACE_ID", "")
	t.Setenv("TERMINAL_EMULATOR", "")

	ide := detectIDE()
	if ide.Name != "vscode" {
		t.Fatalf("Name = %q, want vscode", ide.Name)
	}
}

func TestDetectIDEVSCodeIPCHook(t *testing.T) {
	t.Setenv("TERM_PROGRAM", "")
	t.Setenv("VSCODE_PID", "")
	t.Setenv("VSCODE_IPC_HOOK", "/tmp/hook")
	t.Setenv("CURSOR_TRACE_ID", "")
	t.Setenv("TERMINAL_EMULATOR", "")

	ide := detectIDE()
	if ide.Name != "vscode" {
		t.Fatalf("Name = %q, want vscode", ide.Name)
	}
}

func TestDetectIDECursor(t *testing.T) {
	t.Setenv("TERM_PROGRAM", "")
	t.Setenv("VSCODE_PID", "")
	t.Setenv("VSCODE_IPC_HOOK", "")
	t.Setenv("CURSOR_TRACE_ID", "abc-123")
	t.Setenv("TERMINAL_EMULATOR", "")

	ide := detectIDE()
	if ide.Name != "cursor" {
		t.Fatalf("Name = %q, want cursor", ide.Name)
	}
}

func TestDetectIDEJetBrains(t *testing.T) {
	t.Setenv("TERM_PROGRAM", "")
	t.Setenv("VSCODE_PID", "")
	t.Setenv("VSCODE_IPC_HOOK", "")
	t.Setenv("CURSOR_TRACE_ID", "")
	t.Setenv("TERMINAL_EMULATOR", "JetBrains-JediTerm")

	ide := detectIDE()
	if ide.Name != "jetbrains" {
		t.Fatalf("Name = %q, want jetbrains", ide.Name)
	}
}

func TestDetectIDENone(t *testing.T) {
	t.Setenv("TERM_PROGRAM", "")
	t.Setenv("VSCODE_PID", "")
	t.Setenv("VSCODE_IPC_HOOK", "")
	t.Setenv("CURSOR_TRACE_ID", "")
	t.Setenv("TERMINAL_EMULATOR", "")

	ide := detectIDE()
	if ide.Name != "" {
		t.Fatalf("Name = %q, want empty", ide.Name)
	}
}

// ---------------------------------------------------------------------------
// All project markers
// ---------------------------------------------------------------------------

func TestDetectWorkspaceAllMarkers(t *testing.T) {
	t.Parallel()

	markerTests := []struct {
		fileName    string
		projectType string
	}{
		{"go.mod", "go"},
		{"Cargo.toml", "rust"},
		{"package.json", "node"},
		{"pyproject.toml", "python"},
		{"requirements.txt", "python"},
		{"Gemfile", "ruby"},
		{"pom.xml", "java"},
		{"build.gradle", "java"},
		{"CMakeLists.txt", "cpp"},
		{"Makefile", "make"},
	}
	for _, tc := range markerTests {
		t.Run(tc.fileName, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, tc.fileName), []byte(""), 0644); err != nil {
				t.Fatalf("WriteFile error = %v", err)
			}
			wc := detectWorkspace(dir)
			if wc.ProjectType != tc.projectType {
				t.Fatalf("ProjectType = %q, want %q for %s", wc.ProjectType, tc.projectType, tc.fileName)
			}
		})
	}
}

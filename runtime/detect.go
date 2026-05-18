package runtime

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"github.com/fulcrus/hopclaw/skill"
)

// DetectContext probes the current environment and returns a populated RuntimeContext.
func DetectContext(workDir string) skill.RuntimeContext {
	ctx := skill.RuntimeContext{
		GOOS:   runtime.GOOS,
		GOARCH: runtime.GOARCH,
		Shell:  detectShell(),
		IDE:    detectIDE(),
	}
	if workDir != "" {
		ctx.Git = detectGit(workDir)
		ctx.Workspace = detectWorkspace(workDir)
	}
	return ctx
}

// ---------------------------------------------------------------------------
// Shell detection
// ---------------------------------------------------------------------------

func detectShell() skill.ShellContext {
	shellPath := os.Getenv("SHELL")
	if shellPath == "" {
		if runtime.GOOS == "windows" {
			// Check for PowerShell.
			if ps, err := exec.LookPath("pwsh"); err == nil {
				return skill.ShellContext{Name: "pwsh", Path: ps}
			}
			return skill.ShellContext{Name: "cmd", Path: `C:\Windows\system32\cmd.exe`}
		}
		return skill.ShellContext{}
	}
	name := filepath.Base(shellPath)
	sc := skill.ShellContext{Name: name, Path: shellPath}

	// Try to get version.
	switch name {
	case "bash":
		if out, err := exec.Command(shellPath, "--version").Output(); err == nil {
			if lines := strings.SplitN(string(out), "\n", 2); len(lines) > 0 {
				sc.Version = extractVersion(lines[0])
			}
		}
	case "zsh":
		if out, err := exec.Command(shellPath, "--version").Output(); err == nil {
			sc.Version = extractVersion(strings.TrimSpace(string(out)))
		}
	case "fish":
		if out, err := exec.Command(shellPath, "--version").Output(); err == nil {
			sc.Version = extractVersion(strings.TrimSpace(string(out)))
		}
	}
	return sc
}

func extractVersion(s string) string {
	// Find first token that looks like a version number.
	for _, part := range strings.Fields(s) {
		if len(part) > 0 && part[0] >= '0' && part[0] <= '9' && strings.Contains(part, ".") {
			return strings.TrimRight(part, ",;()")
		}
	}
	return ""
}

// ---------------------------------------------------------------------------
// Git detection
// ---------------------------------------------------------------------------

func detectGit(dir string) skill.GitContext {
	gc := skill.GitContext{}

	// Check if in a git repo.
	out, err := execInDir(dir, "git", "rev-parse", "--show-toplevel")
	if err != nil {
		return gc
	}
	gc.InRepo = true
	gc.Root = strings.TrimSpace(out)

	// Current branch.
	if out, err := execInDir(dir, "git", "rev-parse", "--abbrev-ref", "HEAD"); err == nil {
		gc.Branch = strings.TrimSpace(out)
	}

	// Remotes.
	if out, err := execInDir(dir, "git", "remote"); err == nil {
		for _, r := range strings.Fields(out) {
			if r != "" {
				gc.Remotes = append(gc.Remotes, r)
			}
		}
		sort.Strings(gc.Remotes)
	}

	// Remote URL for fingerprinting.
	if len(gc.Remotes) > 0 {
		remoteName := gc.Remotes[0] // prefer "origin" if present
		for _, r := range gc.Remotes {
			if r == "origin" {
				remoteName = r
				break
			}
		}
		if out, err := execInDir(dir, "git", "config", "--get",
			fmt.Sprintf("remote.%s.url", remoteName)); err == nil {
			gc.RemoteURL = normalizeGitRemoteURL(strings.TrimSpace(out))
		}
	}

	// Dirty state.
	if out, err := execInDir(dir, "git", "status", "--porcelain"); err == nil {
		gc.Dirty = strings.TrimSpace(out) != ""
	}

	return gc
}

func execInDir(dir, name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	return string(out), err
}

// normalizeGitRemoteURL normalizes git remote URLs to a canonical form.
// "git@github.com:user/repo.git" -> "github.com/user/repo"
// "https://github.com/user/repo.git" -> "github.com/user/repo"
func normalizeGitRemoteURL(rawURL string) string {
	u := rawURL
	// SSH style: git@host:user/repo.git
	if strings.Contains(u, "@") && strings.Contains(u, ":") {
		parts := strings.SplitN(u, "@", 2)
		if len(parts) == 2 {
			hostPath := strings.Replace(parts[1], ":", "/", 1)
			u = hostPath
		}
	}
	// HTTPS style
	u = strings.TrimPrefix(u, "https://")
	u = strings.TrimPrefix(u, "http://")
	u = strings.TrimSuffix(u, ".git")
	u = strings.TrimSuffix(u, "/")
	return u
}

// ---------------------------------------------------------------------------
// IDE detection
// ---------------------------------------------------------------------------

func detectIDE() skill.IDEContext {
	// VS Code / Cursor detection via environment variables.
	if term := os.Getenv("TERM_PROGRAM"); term != "" {
		switch strings.ToLower(term) {
		case "vscode":
			return skill.IDEContext{
				Name:    "vscode",
				Version: os.Getenv("TERM_PROGRAM_VERSION"),
			}
		}
	}
	if os.Getenv("VSCODE_PID") != "" || os.Getenv("VSCODE_IPC_HOOK") != "" {
		return skill.IDEContext{
			Name:    "vscode",
			Version: os.Getenv("TERM_PROGRAM_VERSION"),
		}
	}
	if os.Getenv("CURSOR_TRACE_ID") != "" {
		return skill.IDEContext{Name: "cursor"}
	}
	// JetBrains terminal.
	if os.Getenv("TERMINAL_EMULATOR") == "JetBrains-JediTerm" {
		return skill.IDEContext{Name: "jetbrains"}
	}
	return skill.IDEContext{}
}

// ---------------------------------------------------------------------------
// Workspace detection
// ---------------------------------------------------------------------------

var projectMarkers = []struct {
	File        string
	ProjectType string
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

func detectWorkspace(dir string) skill.WorkspaceContext {
	wc := skill.WorkspaceContext{Root: dir}
	for _, m := range projectMarkers {
		if _, err := os.Stat(filepath.Join(dir, m.File)); err == nil {
			wc.Markers = append(wc.Markers, m.File)
			if wc.ProjectType == "" {
				wc.ProjectType = m.ProjectType
			}
		}
	}
	sort.Strings(wc.Markers)
	return wc
}

package toolruntime

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/fulcrus/hopclaw/agent"
	"github.com/fulcrus/hopclaw/contextengine"
)

const gitUnavailableCode = "not_a_git_repository"

type gitRepoContext struct {
	RequestedDir string
	WorkingDir   string
	RepoRoot     string
}

type gitStatusEntry struct {
	Path           string `json:"path"`
	OriginalPath   string `json:"original_path,omitempty"`
	Code           string `json:"code"`
	IndexStatus    string `json:"index_status"`
	WorktreeStatus string `json:"worktree_status"`
	Untracked      bool   `json:"untracked"`
	Ignored        bool   `json:"ignored"`
}

type gitBranchStatus struct {
	Head     string `json:"head,omitempty"`
	Upstream string `json:"upstream,omitempty"`
	Ahead    int    `json:"ahead,omitempty"`
	Behind   int    `json:"behind,omitempty"`
	Detached bool   `json:"detached,omitempty"`
	Initial  bool   `json:"initial,omitempty"`
}

func (w *ws) resolveGitContext(dirValue string) (gitRepoContext, error) {
	workingDir, err := w.resolvePathWithOptions(dirValue, true)
	if err != nil {
		return gitRepoContext{}, err
	}
	repoRoot, ok, err := w.discoverGitRoot(workingDir)
	if err != nil {
		return gitRepoContext{}, err
	}
	if !ok {
		return gitRepoContext{
			RequestedDir: workingDir,
			WorkingDir:   workingDir,
		}, nil
	}
	return gitRepoContext{
		RequestedDir: workingDir,
		WorkingDir:   workingDir,
		RepoRoot:     repoRoot,
	}, nil
}

func (w *ws) resolveGitContextForPath(dirValue, pathValue string) (gitRepoContext, string, error) {
	resolvedPath, err := w.resolvePath(pathValue)
	if err != nil {
		return gitRepoContext{}, "", err
	}
	startDir := filepath.Dir(resolvedPath)
	if strings.TrimSpace(dirValue) != "" {
		startDir, err = w.resolvePathWithOptions(dirValue, true)
		if err != nil {
			return gitRepoContext{}, "", err
		}
	}
	repoRoot, ok, err := w.discoverGitRoot(startDir)
	if err != nil {
		return gitRepoContext{}, "", err
	}
	if !ok {
		repoRoot, ok, err = w.discoverGitRoot(filepath.Dir(resolvedPath))
		if err != nil {
			return gitRepoContext{}, "", err
		}
	}
	ctx := gitRepoContext{
		RequestedDir: startDir,
		WorkingDir:   startDir,
	}
	if ok {
		ctx.RepoRoot = repoRoot
		ctx.WorkingDir = repoRoot
	}
	return ctx, resolvedPath, nil
}

func (w *ws) discoverGitRoot(start string) (string, bool, error) {
	current, err := filepath.Abs(start)
	if err != nil {
		return "", false, err
	}
	current = filepath.Clean(current)
	for {
		info, err := os.Stat(filepath.Join(current, ".git"))
		if err == nil {
			if info.IsDir() || info.Mode().IsRegular() {
				return current, true, nil
			}
		} else if !os.IsNotExist(err) {
			return "", false, err
		}
		if current == w.rootAbs {
			return "", false, nil
		}
		parent := filepath.Dir(current)
		if parent == current {
			return "", false, nil
		}
		rel, relErr := filepath.Rel(w.rootAbs, parent)
		if relErr != nil {
			return "", false, relErr
		}
		if rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
			return "", false, nil
		}
		current = parent
	}
}

func (ctx gitRepoContext) Available() bool {
	return strings.TrimSpace(ctx.RepoRoot) != ""
}

func (w *ws) gitUnavailableResult(call agent.ToolCall, repoCtx gitRepoContext, extra map[string]any) (contextengine.ToolResult, error) {
	payload := map[string]any{
		"repo_available": false,
		"status":         gitUnavailableCode,
		"working_dir":    w.displayPath(repoCtx.WorkingDir),
		"repo_root":      "",
	}
	if repoCtx.RequestedDir != "" {
		payload["requested_dir"] = w.displayPath(repoCtx.RequestedDir)
	}
	for key, value := range extra {
		payload[key] = value
	}
	body, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return contextengine.ToolResult{}, err
	}
	return contextengine.ToolResult{
		ToolName:   call.Name,
		ToolCallID: call.ID,
		Content:    string(body),
	}, nil
}

func (w *ws) gitSuccessResult(call agent.ToolCall, repoCtx gitRepoContext, extra map[string]any) (contextengine.ToolResult, error) {
	payload := map[string]any{
		"repo_available": true,
		"status":         "ok",
		"working_dir":    w.displayPath(repoCtx.WorkingDir),
		"repo_root":      w.displayPath(repoCtx.RepoRoot),
	}
	if repoCtx.RequestedDir != "" {
		payload["requested_dir"] = w.displayPath(repoCtx.RequestedDir)
	}
	for key, value := range extra {
		payload[key] = value
	}
	body, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return contextengine.ToolResult{}, err
	}
	return contextengine.ToolResult{
		ToolName:   call.Name,
		ToolCallID: call.ID,
		Content:    string(body),
	}, nil
}

func runGit(ctx context.Context, workingDir string, timeout time.Duration, args ...string) (string, error) {
	execCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	command := exec.CommandContext(execCtx, "git", args...)
	command.Dir = workingDir
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	if err := command.Run(); err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			return "", fmt.Errorf("git executable is not installed")
		}
		message := strings.TrimSpace(stderr.String())
		if message == "" {
			message = err.Error()
		}
		return "", fmt.Errorf("%s", message)
	}
	return stdout.String(), nil
}

func parseGitStatusOutput(output string) (gitBranchStatus, []gitStatusEntry) {
	var branch gitBranchStatus
	lines := strings.Split(strings.TrimSpace(output), "\n")
	entries := make([]gitStatusEntry, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "## ") {
			branch = parseGitBranchStatus(strings.TrimPrefix(line, "## "))
			continue
		}
		if len(line) < 3 {
			continue
		}
		code := line[:2]
		rest := strings.TrimSpace(line[3:])
		entry := gitStatusEntry{
			Code:           code,
			IndexStatus:    string(code[0]),
			WorktreeStatus: string(code[1]),
		}
		switch {
		case code == "??":
			entry.Untracked = true
			entry.Path = rest
		case code == "!!":
			entry.Ignored = true
			entry.Path = rest
		default:
			if strings.Contains(rest, " -> ") {
				parts := strings.SplitN(rest, " -> ", 2)
				entry.OriginalPath = parts[0]
				entry.Path = parts[1]
			} else {
				entry.Path = rest
			}
		}
		entries = append(entries, entry)
	}
	return branch, entries
}

func parseGitBranchStatus(line string) gitBranchStatus {
	var branch gitBranchStatus
	statusPart := line
	if idx := strings.Index(line, " ["); idx >= 0 {
		statusPart = line[:idx]
		parseAheadBehind(line[idx+2:len(line)-1], &branch)
	}
	switch {
	case strings.Contains(statusPart, "No commits yet on "):
		branch.Initial = true
		branch.Head = strings.TrimSpace(strings.TrimPrefix(statusPart, "No commits yet on "))
	case strings.HasPrefix(statusPart, "HEAD (no branch)"):
		branch.Detached = true
	case strings.HasPrefix(statusPart, "HEAD detached at "):
		branch.Detached = true
		branch.Head = strings.TrimSpace(strings.TrimPrefix(statusPart, "HEAD detached at "))
	default:
		head, upstream, _ := strings.Cut(statusPart, "...")
		branch.Head = strings.TrimSpace(head)
		branch.Upstream = strings.TrimSpace(upstream)
	}
	return branch
}

func parseAheadBehind(raw string, branch *gitBranchStatus) {
	for _, part := range strings.Split(raw, ",") {
		part = strings.TrimSpace(part)
		switch {
		case strings.HasPrefix(part, "ahead "):
			if n, err := strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(part, "ahead "))); err == nil {
				branch.Ahead = n
			}
		case strings.HasPrefix(part, "behind "):
			if n, err := strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(part, "behind "))); err == nil {
				branch.Behind = n
			}
		}
	}
}

func compactNonEmptyLines(raw string) []string {
	lines := strings.Split(raw, "\n")
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		out = append(out, line)
	}
	return out
}

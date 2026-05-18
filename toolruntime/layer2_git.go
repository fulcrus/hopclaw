package toolruntime

import (
	"context"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/fulcrus/hopclaw/agent"
	"github.com/fulcrus/hopclaw/contextengine"
	"github.com/fulcrus/hopclaw/skill"
)

func init() {
	RegisterLayer2GroupToggle("git", "git")
}

func (r *Layer2Registry) registerGitGroup() {
	timeout := r.config.DefaultExecTimeout
	r.registerGroup("git", []string{"git"}, []layer2ToolDef{
		{manifest: skill.ToolManifest{
			Name: "git.status", Description: "Inspect git status for a repository within the workspace root.",
			InputSchema: gitStatusSchema(), OutputSchema: gitStatusOutputSchema(),
			SideEffectClass: "read", Idempotent: true, ExecutionKey: "git:{dir}", Timeout: timeout,
		}, execFn: gitStatusExec},
		{manifest: skill.ToolManifest{
			Name: "git.rev_parse", Description: "Resolve git revisions and repository metadata within the workspace root.",
			InputSchema: gitRevParseSchema(), OutputSchema: gitRevParseOutputSchema(),
			SideEffectClass: "read", Idempotent: true, ExecutionKey: "git:{dir}", Timeout: timeout,
		}, execFn: gitRevParseExec},
		{manifest: skill.ToolManifest{
			Name: "git.blame", Description: "Inspect line authorship for a file within the workspace root.",
			InputSchema: gitBlameSchema(), OutputSchema: gitBlameOutputSchema(),
			SideEffectClass: "read", Idempotent: true, ExecutionKey: "git:{dir}", Timeout: timeout,
		}, execFn: gitBlameExec},
		{manifest: skill.ToolManifest{
			Name: "git.log", Description: "Inspect recent git history for a repository within the workspace root.",
			InputSchema: gitLogSchema(), OutputSchema: gitLogOutputSchema(),
			SideEffectClass: "read", Idempotent: true, ExecutionKey: "git:{dir}", Timeout: timeout,
		}, execFn: gitLogExec},
		{manifest: skill.ToolManifest{
			Name: "git.diff", Description: "Inspect git diff for a repository within the workspace root.",
			InputSchema: gitDiffSchema(), OutputSchema: gitDiffOutputSchema(),
			SideEffectClass: "read", Idempotent: true, ExecutionKey: "git:{dir}", Timeout: timeout,
		}, execFn: gitDiffExec},
		{manifest: skill.ToolManifest{
			Name: "git.show", Description: "Inspect a git object or commit within the workspace root.",
			InputSchema: gitShowSchema(), OutputSchema: gitShowOutputSchema(),
			SideEffectClass: "read", Idempotent: true, ExecutionKey: "git:{dir}", Timeout: timeout,
		}, execFn: gitShowExec},
	})
}

// --- Git tool implementations ---

func gitStatusExec(ctx context.Context, w *ws, cfg BuiltinsConfig, call agent.ToolCall) (contextengine.ToolResult, error) {
	dirValue, _ := stringFrom(call.Input["dir"])
	repoCtx, err := w.resolveGitContext(dirValue)
	if err != nil {
		return contextengine.ToolResult{}, err
	}
	timeout, err := timeoutFrom(call.Input["timeout_seconds"], cfg.DefaultExecTimeout)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("invalid git.status timeout_seconds: %w", err)
	}
	if !repoCtx.Available() {
		return w.gitUnavailableResult(call, repoCtx, map[string]any{
			"repo_root": "", "branch": gitBranchStatus{}, "entries": []gitStatusEntry{}, "is_clean": true,
		})
	}
	content, err := runGit(ctx, repoCtx.WorkingDir, timeout, "status", "--short", "--branch")
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("git.status failed: %w", err)
	}
	branch, entries := parseGitStatusOutput(content)
	return w.gitSuccessResult(call, repoCtx, map[string]any{
		"branch": branch, "entries": entries, "is_clean": len(entries) == 0,
	})
}

func gitRevParseExec(ctx context.Context, w *ws, cfg BuiltinsConfig, call agent.ToolCall) (contextengine.ToolResult, error) {
	dirValue, _ := stringFrom(call.Input["dir"])
	repoCtx, err := w.resolveGitContext(dirValue)
	if err != nil {
		return contextengine.ToolResult{}, err
	}
	timeout, err := timeoutFrom(call.Input["timeout_seconds"], cfg.DefaultExecTimeout)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("invalid git.rev_parse timeout_seconds: %w", err)
	}
	rev, _ := stringFrom(call.Input["rev"])
	if strings.TrimSpace(rev) == "" {
		rev = "HEAD"
	}
	if !repoCtx.Available() {
		return w.gitUnavailableResult(call, repoCtx, map[string]any{
			"rev": rev, "full": "", "short": "", "show_toplevel": "", "is_inside_work_tree": false,
		})
	}
	full, err := runGit(ctx, repoCtx.WorkingDir, timeout, "rev-parse", "--verify", rev)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("git.rev_parse failed: %w", err)
	}
	short, err := runGit(ctx, repoCtx.WorkingDir, timeout, "rev-parse", "--short", rev)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("git.rev_parse short failed: %w", err)
	}
	topLevel, err := runGit(ctx, repoCtx.WorkingDir, timeout, "rev-parse", "--show-toplevel")
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("git.rev_parse top-level failed: %w", err)
	}
	inside, err := runGit(ctx, repoCtx.WorkingDir, timeout, "rev-parse", "--is-inside-work-tree")
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("git.rev_parse inside-work-tree failed: %w", err)
	}
	return w.gitSuccessResult(call, repoCtx, map[string]any{
		"rev": rev, "full": strings.TrimSpace(full), "short": strings.TrimSpace(short),
		"show_toplevel": strings.TrimSpace(topLevel), "is_inside_work_tree": strings.TrimSpace(inside) == "true",
	})
}

func gitBlameExec(ctx context.Context, w *ws, cfg BuiltinsConfig, call agent.ToolCall) (contextengine.ToolResult, error) {
	dirValue, _ := stringFrom(call.Input["dir"])
	pathValue, err := requiredString(call.Input, "path")
	if err != nil {
		return contextengine.ToolResult{}, err
	}
	repoCtx, resolvedPath, err := w.resolveGitContextForPath(dirValue, pathValue)
	if err != nil {
		return contextengine.ToolResult{}, err
	}
	startLine, err := intFrom(call.Input["start_line"], 1)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("invalid git.blame start_line: %w", err)
	}
	endLine, err := intFrom(call.Input["end_line"], startLine)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("invalid git.blame end_line: %w", err)
	}
	timeout, err := timeoutFrom(call.Input["timeout_seconds"], cfg.DefaultExecTimeout)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("invalid git.blame timeout_seconds: %w", err)
	}
	rev, _ := stringFrom(call.Input["rev"])
	if strings.TrimSpace(rev) == "" {
		rev = "HEAD"
	}
	if !repoCtx.Available() {
		return w.gitUnavailableResult(call, repoCtx, map[string]any{
			"path": w.displayPath(resolvedPath), "rev": rev, "start_line": startLine, "end_line": endLine, "entries": []any{}, "count": 0,
		})
	}
	relPath, err := filepath.Rel(repoCtx.WorkingDir, resolvedPath)
	if err != nil {
		return contextengine.ToolResult{}, err
	}
	output, err := runGit(ctx, repoCtx.WorkingDir, timeout, "blame", "--line-porcelain", fmt.Sprintf("-L%d,%d", startLine, endLine), rev, "--", filepath.ToSlash(relPath))
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("git.blame failed: %w", err)
	}
	type entry struct {
		Line       int    `json:"line"`
		Commit     string `json:"commit"`
		Author     string `json:"author"`
		AuthorMail string `json:"author_mail"`
		Summary    string `json:"summary"`
		Content    string `json:"content"`
		OrigLine   int    `json:"orig_line"`
		FinalLine  int    `json:"final_line"`
	}
	var entries []entry
	var current entry
	for _, line := range strings.Split(output, "\n") {
		switch {
		case line == "":
			continue
		case strings.HasPrefix(line, "\t"):
			current.Content = strings.TrimPrefix(line, "\t")
			current.Line = current.FinalLine
			entries = append(entries, current)
			current = entry{}
		case isBlameHeader(line):
			fields := strings.Fields(line)
			if len(fields) >= 3 {
				current.Commit = fields[0]
				current.OrigLine, _ = strconv.Atoi(fields[1])
				current.FinalLine, _ = strconv.Atoi(fields[2])
			}
		case strings.HasPrefix(line, "author "):
			current.Author = strings.TrimPrefix(line, "author ")
		case strings.HasPrefix(line, "author-mail "):
			current.AuthorMail = strings.Trim(strings.TrimPrefix(line, "author-mail "), "<>")
		case strings.HasPrefix(line, "summary "):
			current.Summary = strings.TrimPrefix(line, "summary ")
		}
	}
	return w.gitSuccessResult(call, repoCtx, map[string]any{
		"path": filepath.ToSlash(relPath), "rev": rev, "start_line": startLine, "end_line": endLine, "entries": entries, "count": len(entries),
	})
}

func gitLogExec(ctx context.Context, w *ws, cfg BuiltinsConfig, call agent.ToolCall) (contextengine.ToolResult, error) {
	dirValue, _ := stringFrom(call.Input["dir"])
	repoCtx, err := w.resolveGitContext(dirValue)
	if err != nil {
		return contextengine.ToolResult{}, err
	}
	limit, err := intFrom(call.Input["limit"], 20)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("invalid git.log limit: %w", err)
	}
	if limit <= 0 {
		limit = 20
	}
	timeout, err := timeoutFrom(call.Input["timeout_seconds"], cfg.DefaultExecTimeout)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("invalid git.log timeout_seconds: %w", err)
	}
	rev, _ := stringFrom(call.Input["rev"])
	args := []string{"log", fmt.Sprintf("--max-count=%d", limit), "--date=iso-strict", "--pretty=format:%H%x1f%an%x1f%ae%x1f%aI%x1f%s%x1e"}
	if strings.TrimSpace(rev) != "" {
		args = append(args, rev)
	}
	pathspecs, err := stringSliceFrom(call.Input["pathspecs"])
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("invalid git.log pathspecs: %w", err)
	}
	if len(pathspecs) > 0 {
		args = append(args, "--")
		args = append(args, pathspecs...)
	}
	if !repoCtx.Available() {
		return w.gitUnavailableResult(call, repoCtx, map[string]any{
			"rev": rev, "limit": limit, "pathspecs": pathspecs, "count": 0, "commits": []any{},
		})
	}
	output, err := runGit(ctx, repoCtx.WorkingDir, timeout, args...)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("git.log failed: %w", err)
	}
	type commit struct {
		Hash        string `json:"hash"`
		AuthorName  string `json:"author_name"`
		AuthorEmail string `json:"author_email"`
		AuthoredAt  string `json:"authored_at"`
		Subject     string `json:"subject"`
	}
	var commits []commit
	records := strings.Split(strings.TrimSuffix(output, "\x1e"), "\x1e")
	for _, record := range records {
		record = strings.TrimSpace(record)
		if record == "" {
			continue
		}
		fields := strings.Split(record, "\x1f")
		if len(fields) < 5 {
			continue
		}
		commits = append(commits, commit{Hash: fields[0], AuthorName: fields[1], AuthorEmail: fields[2], AuthoredAt: fields[3], Subject: fields[4]})
	}
	return w.gitSuccessResult(call, repoCtx, map[string]any{
		"rev": rev, "limit": limit, "pathspecs": pathspecs, "count": len(commits), "commits": commits,
	})
}

func gitDiffExec(ctx context.Context, w *ws, cfg BuiltinsConfig, call agent.ToolCall) (contextengine.ToolResult, error) {
	dirValue, _ := stringFrom(call.Input["dir"])
	repoCtx, err := w.resolveGitContext(dirValue)
	if err != nil {
		return contextengine.ToolResult{}, err
	}
	cached, _ := boolFrom(call.Input["cached"])
	timeout, err := timeoutFrom(call.Input["timeout_seconds"], cfg.DefaultExecTimeout)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("invalid git.diff timeout_seconds: %w", err)
	}
	args := []string{"diff", "--no-ext-diff", "--submodule=diff"}
	if cached {
		args = append(args, "--cached")
	}
	pathspecs, err := stringSliceFrom(call.Input["pathspecs"])
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("invalid git.diff pathspecs: %w", err)
	}
	if len(pathspecs) > 0 {
		args = append(args, "--")
		args = append(args, pathspecs...)
	}
	if !repoCtx.Available() {
		return w.gitUnavailableResult(call, repoCtx, map[string]any{
			"cached": cached, "pathspecs": pathspecs, "diff": "", "is_clean": true,
		})
	}
	content, err := runGit(ctx, repoCtx.WorkingDir, timeout, args...)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("git.diff failed: %w", err)
	}
	content = strings.TrimSpace(content)
	nameOnlyArgs := []string{"diff", "--no-ext-diff", "--name-only"}
	if cached {
		nameOnlyArgs = append(nameOnlyArgs, "--cached")
	}
	if len(pathspecs) > 0 {
		nameOnlyArgs = append(nameOnlyArgs, "--")
		nameOnlyArgs = append(nameOnlyArgs, pathspecs...)
	}
	nameOnly, err := runGit(ctx, repoCtx.WorkingDir, timeout, nameOnlyArgs...)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("git.diff names failed: %w", err)
	}
	changedFiles := compactNonEmptyLines(nameOnly)
	return w.gitSuccessResult(call, repoCtx, map[string]any{
		"cached": cached, "pathspecs": pathspecs, "diff": content, "changed_files": changedFiles, "is_clean": len(changedFiles) == 0 && content == "",
	})
}

func gitShowExec(ctx context.Context, w *ws, cfg BuiltinsConfig, call agent.ToolCall) (contextengine.ToolResult, error) {
	dirValue, _ := stringFrom(call.Input["dir"])
	repoCtx, err := w.resolveGitContext(dirValue)
	if err != nil {
		return contextengine.ToolResult{}, err
	}
	timeout, err := timeoutFrom(call.Input["timeout_seconds"], cfg.DefaultExecTimeout)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("invalid git.show timeout_seconds: %w", err)
	}
	rev, _ := stringFrom(call.Input["rev"])
	if strings.TrimSpace(rev) == "" {
		rev = "HEAD"
	}
	stat, _ := boolFrom(call.Input["stat"])
	patch, _ := boolFromDefault(call.Input["patch"], true)
	args := []string{"show", "--no-ext-diff", "--submodule=diff"}
	if stat {
		args = append(args, "--stat")
	}
	if patch {
		args = append(args, "-p")
	} else {
		args = append(args, "--no-patch")
	}
	args = append(args, rev)
	pathspecs, _ := stringSliceFrom(call.Input["pathspecs"])
	if len(pathspecs) > 0 {
		args = append(args, "--")
		args = append(args, pathspecs...)
	}
	if !repoCtx.Available() {
		return w.gitUnavailableResult(call, repoCtx, map[string]any{
			"rev": rev, "pathspecs": pathspecs, "patch": patch, "stat": stat, "content": "", "is_empty": true,
		})
	}
	content, err := runGit(ctx, repoCtx.WorkingDir, timeout, args...)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("git.show failed: %w", err)
	}
	content = strings.TrimSpace(content)
	return w.gitSuccessResult(call, repoCtx, map[string]any{
		"rev": rev, "pathspecs": pathspecs, "patch": patch, "stat": stat, "content": content, "is_empty": content == "",
	})
}

// --- Git schemas ---

func gitStatusSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"dir":             map[string]any{"type": "string", "description": "Optional git repository path relative to the configured workspace root. Defaults to root."},
			"timeout_seconds": map[string]any{"type": "number", "description": "Optional timeout in seconds."},
		},
		"additionalProperties": false,
	}
}

func gitRevParseSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"dir":             map[string]any{"type": "string", "description": "Optional git repository path relative to the configured workspace root. Defaults to root."},
			"rev":             map[string]any{"type": "string", "description": "Revision to resolve. Defaults to HEAD."},
			"timeout_seconds": map[string]any{"type": "number", "description": "Optional timeout in seconds."},
		},
		"additionalProperties": false,
	}
}

func gitBlameSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"dir":             map[string]any{"type": "string", "description": "Optional git repository path relative to the configured workspace root."},
			"path":            map[string]any{"type": "string", "description": "File path relative to the workspace root."},
			"start_line":      map[string]any{"type": "integer", "description": "Start line for blame range."},
			"end_line":        map[string]any{"type": "integer", "description": "End line for blame range."},
			"rev":             map[string]any{"type": "string", "description": "Revision to blame. Defaults to HEAD."},
			"timeout_seconds": map[string]any{"type": "number", "description": "Optional timeout in seconds."},
		},
		"required":             []string{"path"},
		"additionalProperties": false,
	}
}

func gitLogSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"dir":             map[string]any{"type": "string", "description": "Optional git repository path relative to the configured workspace root. Defaults to root."},
			"rev":             map[string]any{"type": "string", "description": "Optional revision or range to inspect."},
			"limit":           map[string]any{"type": "integer", "description": "Maximum number of commits to return."},
			"pathspecs":       map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Optional pathspec filters."},
			"timeout_seconds": map[string]any{"type": "number", "description": "Optional timeout in seconds."},
		},
		"additionalProperties": false,
	}
}

func gitDiffSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"dir":             map[string]any{"type": "string", "description": "Optional git repository path relative to the configured workspace root. Defaults to root."},
			"cached":          map[string]any{"type": "boolean", "description": "Show staged changes when true."},
			"pathspecs":       map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Optional pathspec filters."},
			"timeout_seconds": map[string]any{"type": "number", "description": "Optional timeout in seconds."},
		},
		"additionalProperties": false,
	}
}

func gitShowSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"dir":             map[string]any{"type": "string", "description": "Optional git repository path relative to the configured workspace root. Defaults to root."},
			"rev":             map[string]any{"type": "string", "description": "Revision, commit, or object to inspect. Defaults to HEAD."},
			"stat":            map[string]any{"type": "boolean", "description": "Include diffstat output."},
			"patch":           map[string]any{"type": "boolean", "description": "Include patch output. Defaults to true."},
			"pathspecs":       map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Optional pathspec filters."},
			"timeout_seconds": map[string]any{"type": "number", "description": "Optional timeout in seconds."},
		},
		"additionalProperties": false,
	}
}

// --- Git output schemas ---

func gitEnvelopeSchema(extra map[string]any, required ...string) map[string]any {
	properties := map[string]any{
		"repo_available": booleanSchema("Whether a Git repository was discovered for the requested directory."),
		"status":         stringSchema("Operation status such as ok or not_a_git_repository."),
		"working_dir":    stringSchema("Directory where the Git command executed or would execute."),
		"repo_root":      stringSchema("Repository root when available."),
		"requested_dir":  stringSchema("Directory originally requested by the caller."),
	}
	for key, value := range extra {
		properties[key] = value
	}
	baseRequired := []string{"repo_available", "status", "working_dir", "repo_root"}
	baseRequired = append(baseRequired, required...)
	return objectSchema(properties, baseRequired...)
}

func gitStatusOutputSchema() map[string]any {
	branch := objectSchema(map[string]any{
		"head": stringSchema("Current HEAD branch or detached reference."), "upstream": stringSchema("Configured upstream branch."),
		"ahead": integerSchema("Commits ahead of upstream."), "behind": integerSchema("Commits behind upstream."),
		"detached": booleanSchema("Whether HEAD is detached."), "initial": booleanSchema("Whether the repository has no commits yet."),
	})
	entry := objectSchema(map[string]any{
		"path": stringSchema("Changed path."), "original_path": stringSchema("Original path for renames."),
		"code": stringSchema("Raw porcelain status code."), "index_status": stringSchema("Index status code."),
		"worktree_status": stringSchema("Worktree status code."), "untracked": booleanSchema("Whether the file is untracked."),
		"ignored": booleanSchema("Whether the file is ignored."),
	}, "path", "code", "index_status", "worktree_status", "untracked", "ignored")
	return gitEnvelopeSchema(map[string]any{
		"branch": branch, "entries": arraySchema(entry, "Status entries."), "is_clean": booleanSchema("Whether the worktree is clean."),
	}, "branch", "entries", "is_clean")
}

func gitRevParseOutputSchema() map[string]any {
	return gitEnvelopeSchema(map[string]any{
		"rev": stringSchema("Requested revision."), "full": stringSchema("Full object ID."), "short": stringSchema("Short object ID."),
		"show_toplevel": stringSchema("Absolute repository root reported by Git."), "is_inside_work_tree": booleanSchema("Whether the directory is inside a work tree."),
	}, "rev", "full", "short", "show_toplevel", "is_inside_work_tree")
}

func gitBlameOutputSchema() map[string]any {
	entry := objectSchema(map[string]any{
		"line": integerSchema("1-based final line number."), "commit": stringSchema("Commit hash."),
		"author": stringSchema("Author name."), "author_mail": stringSchema("Author email."),
		"summary": stringSchema("Commit summary."), "content": stringSchema("Line content."),
		"orig_line": integerSchema("Original source line number."), "final_line": integerSchema("Final line number in the blamed file."),
	}, "line", "commit", "author", "author_mail", "summary", "content", "orig_line", "final_line")
	return gitEnvelopeSchema(map[string]any{
		"path": stringSchema("Path relative to repository root."), "rev": stringSchema("Requested revision."),
		"start_line": integerSchema("Start line requested."), "end_line": integerSchema("End line requested."),
		"count": integerSchema("Number of blamed lines returned."), "entries": arraySchema(entry, "Blame entries."),
	}, "path", "rev", "start_line", "end_line", "count", "entries")
}

func gitLogOutputSchema() map[string]any {
	commit := objectSchema(map[string]any{
		"hash": stringSchema("Commit hash."), "author_name": stringSchema("Author name."),
		"author_email": stringSchema("Author email."), "authored_at": stringSchema("Authored timestamp in ISO-8601."),
		"subject": stringSchema("Commit subject."),
	}, "hash", "author_name", "author_email", "authored_at", "subject")
	return gitEnvelopeSchema(map[string]any{
		"rev": stringSchema("Revision or range filter."), "limit": integerSchema("Maximum commit count requested."),
		"pathspecs": stringArraySchema("Optional pathspec filters."), "count": integerSchema("Number of commits returned."),
		"commits": arraySchema(commit, "Commit records."),
	}, "rev", "limit", "pathspecs", "count", "commits")
}

func gitDiffOutputSchema() map[string]any {
	return gitEnvelopeSchema(map[string]any{
		"cached": booleanSchema("Whether staged changes were requested."), "pathspecs": stringArraySchema("Optional pathspec filters."),
		"changed_files": stringArraySchema("Files changed in the diff."), "diff": stringSchema("Unified diff output."),
		"is_clean": booleanSchema("Whether the diff was empty."),
	}, "cached", "pathspecs", "changed_files", "diff", "is_clean")
}

func gitShowOutputSchema() map[string]any {
	return gitEnvelopeSchema(map[string]any{
		"rev": stringSchema("Revision or object requested."), "pathspecs": stringArraySchema("Optional pathspec filters."),
		"patch": booleanSchema("Whether patch output was requested."), "stat": booleanSchema("Whether diffstat output was requested."),
		"content": stringSchema("Rendered git show output."), "is_empty": booleanSchema("Whether the output was empty."),
	}, "rev", "pathspecs", "patch", "stat", "content", "is_empty")
}

package toolruntime

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/fulcrus/hopclaw/agent"
	"github.com/fulcrus/hopclaw/contextengine"
	"github.com/fulcrus/hopclaw/skill"
)

func init() {
	RegisterLayer2GroupToggle("git-write", "git")
}

func (r *Layer2Registry) registerGitWriteGroup() {
	timeout := r.config.DefaultExecTimeout
	r.registerGroup("git-write", []string{"git"}, []layer2ToolDef{
		{manifest: skill.ToolManifest{
			Name: "git.branch", Description: "Create, delete, or list branches in a git repository within the workspace root.",
			InputSchema: gitBranchSchema(), OutputSchema: gitBranchOutputSchema(),
			SideEffectClass: "local_write", Idempotent: false, ExecutionKey: "git:{dir}", Timeout: timeout,
		}, execFn: gitBranchExec},
		{manifest: skill.ToolManifest{
			Name: "git.commit", Description: "Commit staged changes in a git repository within the workspace root.",
			InputSchema: gitCommitSchema(), OutputSchema: gitCommitOutputSchema(),
			SideEffectClass: "local_write", RequiresApproval: true, Idempotent: false, ExecutionKey: "git:{dir}", Timeout: timeout,
		}, execFn: gitCommitExec},
		{manifest: skill.ToolManifest{
			Name: "git.stash", Description: "Stash or restore uncommitted changes in a git repository within the workspace root.",
			InputSchema: gitStashSchema(), OutputSchema: gitStashOutputSchema(),
			SideEffectClass: "local_write", Idempotent: false, ExecutionKey: "git:{dir}", Timeout: timeout,
		}, execFn: gitStashExec},
		{manifest: skill.ToolManifest{
			Name: "git.tag", Description: "Create, delete, or list tags in a git repository within the workspace root.",
			InputSchema: gitTagSchema(), OutputSchema: gitTagOutputSchema(),
			SideEffectClass: "local_write", Idempotent: false, ExecutionKey: "git:{dir}", Timeout: timeout,
		}, execFn: gitTagExec},
		{manifest: skill.ToolManifest{
			Name: "git.clone", Description: "Clone a git repository into the workspace root.",
			InputSchema: gitCloneSchema(), OutputSchema: gitCloneOutputSchema(),
			SideEffectClass: "local_write", RequiresApproval: true, Idempotent: false, Timeout: 120 * time.Second,
		}, execFn: gitCloneExec},
		{manifest: skill.ToolManifest{
			Name: "git.pull", Description: "Pull changes from a remote in a git repository within the workspace root.",
			InputSchema: gitPullSchema(), OutputSchema: gitPullOutputSchema(),
			SideEffectClass: "external_write", RequiresApproval: true, Idempotent: false, ExecutionKey: "git:{dir}", Timeout: timeout,
		}, execFn: gitPullExec},
		{manifest: skill.ToolManifest{
			Name: "git.push", Description: "Push changes to a remote in a git repository within the workspace root.",
			InputSchema: gitPushSchema(), OutputSchema: gitPushOutputSchema(),
			SideEffectClass: "external_write", RequiresApproval: true, Idempotent: false, ExecutionKey: "git:{dir}", Timeout: timeout,
		}, execFn: gitPushExec},
	})
}

// --- Git write tool implementations ---

func gitBranchExec(ctx context.Context, w *ws, cfg BuiltinsConfig, call agent.ToolCall) (contextengine.ToolResult, error) {
	dirValue, _ := stringFrom(call.Input["dir"])
	repoCtx, err := w.resolveGitContext(dirValue)
	if err != nil {
		return contextengine.ToolResult{}, err
	}
	timeout, err := timeoutFrom(call.Input["timeout_seconds"], cfg.DefaultExecTimeout)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("invalid git.branch timeout_seconds: %w", err)
	}
	name, _ := stringFrom(call.Input["name"])
	deleteBranch, _ := boolFrom(call.Input["delete"])
	listBranches, _ := boolFromDefault(call.Input["list"], strings.TrimSpace(name) == "")

	if !repoCtx.Available() {
		return w.gitUnavailableResult(call, repoCtx, map[string]any{
			"branches": []string{}, "message": "",
		})
	}

	name = strings.TrimSpace(name)

	if name != "" && deleteBranch {
		output, gitErr := runGit(ctx, repoCtx.WorkingDir, timeout, "branch", "-d", name)
		if gitErr != nil {
			return contextengine.ToolResult{}, fmt.Errorf("git.branch delete failed: %w", gitErr)
		}
		return w.gitSuccessResult(call, repoCtx, map[string]any{
			"message": strings.TrimSpace(output),
		})
	}

	if name != "" && !listBranches {
		output, gitErr := runGit(ctx, repoCtx.WorkingDir, timeout, "branch", name)
		if gitErr != nil {
			return contextengine.ToolResult{}, fmt.Errorf("git.branch create failed: %w", gitErr)
		}
		return w.gitSuccessResult(call, repoCtx, map[string]any{
			"message": strings.TrimSpace(output),
		})
	}

	// Default: list branches
	output, gitErr := runGit(ctx, repoCtx.WorkingDir, timeout, "branch", "--list")
	if gitErr != nil {
		return contextengine.ToolResult{}, fmt.Errorf("git.branch list failed: %w", gitErr)
	}
	branches := parseBranchList(output)
	return w.gitSuccessResult(call, repoCtx, map[string]any{
		"branches": branches,
	})
}

func gitCommitExec(ctx context.Context, w *ws, cfg BuiltinsConfig, call agent.ToolCall) (contextengine.ToolResult, error) {
	dirValue, _ := stringFrom(call.Input["dir"])
	repoCtx, err := w.resolveGitContext(dirValue)
	if err != nil {
		return contextengine.ToolResult{}, err
	}
	timeout, err := timeoutFrom(call.Input["timeout_seconds"], cfg.DefaultExecTimeout)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("invalid git.commit timeout_seconds: %w", err)
	}
	message, err := requiredString(call.Input, "message")
	if err != nil {
		return contextengine.ToolResult{}, err
	}
	addAll, _ := boolFrom(call.Input["add_all"])
	amend, _ := boolFrom(call.Input["amend"])

	if !repoCtx.Available() {
		return w.gitUnavailableResult(call, repoCtx, map[string]any{
			"hash": "", "message": "",
		})
	}

	if addAll {
		_, gitErr := runGit(ctx, repoCtx.WorkingDir, timeout, "add", "-A")
		if gitErr != nil {
			return contextengine.ToolResult{}, fmt.Errorf("git.commit add failed: %w", gitErr)
		}
	}

	args := []string{"commit", "-m", message}
	if amend {
		args = append(args, "--amend")
	}
	output, gitErr := runGit(ctx, repoCtx.WorkingDir, timeout, args...)
	if gitErr != nil {
		return contextengine.ToolResult{}, fmt.Errorf("git.commit failed: %w", gitErr)
	}

	// Retrieve the commit hash
	hash, hashErr := runGit(ctx, repoCtx.WorkingDir, timeout, "rev-parse", "--short", "HEAD")
	if hashErr != nil {
		hash = ""
	}

	return w.gitSuccessResult(call, repoCtx, map[string]any{
		"hash":    strings.TrimSpace(hash),
		"message": strings.TrimSpace(output),
	})
}

func gitStashExec(ctx context.Context, w *ws, cfg BuiltinsConfig, call agent.ToolCall) (contextengine.ToolResult, error) {
	dirValue, _ := stringFrom(call.Input["dir"])
	repoCtx, err := w.resolveGitContext(dirValue)
	if err != nil {
		return contextengine.ToolResult{}, err
	}
	timeout, err := timeoutFrom(call.Input["timeout_seconds"], cfg.DefaultExecTimeout)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("invalid git.stash timeout_seconds: %w", err)
	}
	action, _ := stringFrom(call.Input["action"])
	if strings.TrimSpace(action) == "" {
		action = "push"
	}
	stashMessage, _ := stringFrom(call.Input["message"])

	if !repoCtx.Available() {
		return w.gitUnavailableResult(call, repoCtx, map[string]any{
			"action": action, "output": "",
		})
	}

	var args []string
	switch action {
	case "push":
		args = []string{"stash", "push"}
		if strings.TrimSpace(stashMessage) != "" {
			args = append(args, "-m", stashMessage)
		}
	case "pop":
		args = []string{"stash", "pop"}
	case "list":
		args = []string{"stash", "list"}
	case "drop":
		args = []string{"stash", "drop"}
	default:
		return contextengine.ToolResult{}, fmt.Errorf("git.stash: unknown action %q, expected push, pop, list, or drop", action)
	}

	output, gitErr := runGit(ctx, repoCtx.WorkingDir, timeout, args...)
	if gitErr != nil {
		return contextengine.ToolResult{}, fmt.Errorf("git.stash %s failed: %w", action, gitErr)
	}

	return w.gitSuccessResult(call, repoCtx, map[string]any{
		"action": action,
		"output": strings.TrimSpace(output),
	})
}

func gitTagExec(ctx context.Context, w *ws, cfg BuiltinsConfig, call agent.ToolCall) (contextengine.ToolResult, error) {
	dirValue, _ := stringFrom(call.Input["dir"])
	repoCtx, err := w.resolveGitContext(dirValue)
	if err != nil {
		return contextengine.ToolResult{}, err
	}
	timeout, err := timeoutFrom(call.Input["timeout_seconds"], cfg.DefaultExecTimeout)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("invalid git.tag timeout_seconds: %w", err)
	}
	name, _ := stringFrom(call.Input["name"])
	tagMessage, _ := stringFrom(call.Input["message"])
	deleteTag, _ := boolFrom(call.Input["delete"])
	listTags, _ := boolFromDefault(call.Input["list"], strings.TrimSpace(name) == "")

	if !repoCtx.Available() {
		return w.gitUnavailableResult(call, repoCtx, map[string]any{
			"tags": []string{}, "message": "",
		})
	}

	name = strings.TrimSpace(name)

	if name != "" && deleteTag {
		output, gitErr := runGit(ctx, repoCtx.WorkingDir, timeout, "tag", "-d", name)
		if gitErr != nil {
			return contextengine.ToolResult{}, fmt.Errorf("git.tag delete failed: %w", gitErr)
		}
		return w.gitSuccessResult(call, repoCtx, map[string]any{
			"message": strings.TrimSpace(output),
		})
	}

	if name != "" && !listTags {
		args := []string{"tag"}
		if strings.TrimSpace(tagMessage) != "" {
			args = append(args, "-a", name, "-m", tagMessage)
		} else {
			args = append(args, name)
		}
		output, gitErr := runGit(ctx, repoCtx.WorkingDir, timeout, args...)
		if gitErr != nil {
			return contextengine.ToolResult{}, fmt.Errorf("git.tag create failed: %w", gitErr)
		}
		return w.gitSuccessResult(call, repoCtx, map[string]any{
			"message": strings.TrimSpace(output),
		})
	}

	// Default: list tags
	output, gitErr := runGit(ctx, repoCtx.WorkingDir, timeout, "tag", "--list")
	if gitErr != nil {
		return contextengine.ToolResult{}, fmt.Errorf("git.tag list failed: %w", gitErr)
	}
	tags := compactNonEmptyLines(output)
	return w.gitSuccessResult(call, repoCtx, map[string]any{
		"tags": tags,
	})
}

func gitCloneExec(ctx context.Context, w *ws, cfg BuiltinsConfig, call agent.ToolCall) (contextengine.ToolResult, error) {
	url, err := requiredString(call.Input, "url")
	if err != nil {
		return contextengine.ToolResult{}, err
	}
	timeout, err := timeoutFrom(call.Input["timeout_seconds"], 120*time.Second)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("invalid git.clone timeout_seconds: %w", err)
	}
	dirValue, _ := stringFrom(call.Input["dir"])
	branch, _ := stringFrom(call.Input["branch"])
	depth, err := intFrom(call.Input["depth"], 0)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("invalid git.clone depth: %w", err)
	}

	args := []string{"clone"}
	if strings.TrimSpace(branch) != "" {
		args = append(args, "--branch", branch)
	}
	if depth > 0 {
		args = append(args, "--depth", fmt.Sprintf("%d", depth))
	}
	args = append(args, url)

	// Resolve target directory
	targetDir := ""
	workingDir := w.rootAbs
	if strings.TrimSpace(dirValue) != "" {
		resolved, resolveErr := w.resolvePathWithOptions(dirValue, false)
		if resolveErr != nil {
			return contextengine.ToolResult{}, fmt.Errorf("git.clone dir: %w", resolveErr)
		}
		targetDir = resolved
		workingDir = filepath.Dir(resolved)
		args = append(args, filepath.Base(resolved))
	}

	_, gitErr := runGit(ctx, workingDir, timeout, args...)
	if gitErr != nil {
		return contextengine.ToolResult{}, fmt.Errorf("git.clone failed: %w", gitErr)
	}

	resultDir := targetDir
	if resultDir == "" {
		resultDir = workingDir
	}

	payload := map[string]any{
		"url":     url,
		"dir":     w.displayPath(resultDir),
		"branch":  branch,
		"message": fmt.Sprintf("Cloned %s successfully", url),
	}
	body, marshalErr := json.MarshalIndent(payload, "", "  ")
	if marshalErr != nil {
		return contextengine.ToolResult{}, marshalErr
	}
	return contextengine.ToolResult{
		ToolName:   call.Name,
		ToolCallID: call.ID,
		Content:    string(body),
	}, nil
}

func gitPullExec(ctx context.Context, w *ws, cfg BuiltinsConfig, call agent.ToolCall) (contextengine.ToolResult, error) {
	dirValue, _ := stringFrom(call.Input["dir"])
	repoCtx, err := w.resolveGitContext(dirValue)
	if err != nil {
		return contextengine.ToolResult{}, err
	}
	timeout, err := timeoutFrom(call.Input["timeout_seconds"], cfg.DefaultExecTimeout)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("invalid git.pull timeout_seconds: %w", err)
	}
	remote, _ := stringFrom(call.Input["remote"])
	if strings.TrimSpace(remote) == "" {
		remote = "origin"
	}
	branch, _ := stringFrom(call.Input["branch"])

	if !repoCtx.Available() {
		return w.gitUnavailableResult(call, repoCtx, map[string]any{
			"remote": remote, "output": "",
		})
	}

	args := []string{"pull", remote}
	if strings.TrimSpace(branch) != "" {
		args = append(args, branch)
	}

	output, gitErr := runGit(ctx, repoCtx.WorkingDir, timeout, args...)
	if gitErr != nil {
		return contextengine.ToolResult{}, fmt.Errorf("git.pull failed: %w", gitErr)
	}

	return w.gitSuccessResult(call, repoCtx, map[string]any{
		"remote": remote,
		"output": strings.TrimSpace(output),
	})
}

func gitPushExec(ctx context.Context, w *ws, cfg BuiltinsConfig, call agent.ToolCall) (contextengine.ToolResult, error) {
	dirValue, _ := stringFrom(call.Input["dir"])
	repoCtx, err := w.resolveGitContext(dirValue)
	if err != nil {
		return contextengine.ToolResult{}, err
	}
	timeout, err := timeoutFrom(call.Input["timeout_seconds"], cfg.DefaultExecTimeout)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("invalid git.push timeout_seconds: %w", err)
	}
	remote, _ := stringFrom(call.Input["remote"])
	if strings.TrimSpace(remote) == "" {
		remote = "origin"
	}
	branch, _ := stringFrom(call.Input["branch"])
	force, _ := boolFrom(call.Input["force"])

	if !repoCtx.Available() {
		return w.gitUnavailableResult(call, repoCtx, map[string]any{
			"remote": remote, "output": "",
		})
	}

	args := []string{"push"}
	if force {
		args = append(args, "--force")
	}
	args = append(args, remote)
	if strings.TrimSpace(branch) != "" {
		args = append(args, branch)
	}

	output, gitErr := runGit(ctx, repoCtx.WorkingDir, timeout, args...)
	if gitErr != nil {
		return contextengine.ToolResult{}, fmt.Errorf("git.push failed: %w", gitErr)
	}

	return w.gitSuccessResult(call, repoCtx, map[string]any{
		"remote": remote,
		"output": strings.TrimSpace(output),
	})
}

// --- helpers ---

func parseBranchList(output string) []string {
	lines := strings.Split(strings.TrimSpace(output), "\n")
	branches := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// Remove leading "* " for current branch
		line = strings.TrimPrefix(line, "* ")
		line = strings.TrimSpace(line)
		if line != "" {
			branches = append(branches, line)
		}
	}
	return branches
}

// --- Git write schemas ---

func gitBranchSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"dir":             map[string]any{"type": "string", "description": "Optional git repository path relative to the configured workspace root. Defaults to root."},
			"name":            map[string]any{"type": "string", "description": "Branch name to create or delete. Omit to list branches."},
			"delete":          map[string]any{"type": "boolean", "description": "Delete the named branch when true."},
			"list":            map[string]any{"type": "boolean", "description": "List branches. Defaults to true when name is omitted."},
			"timeout_seconds": map[string]any{"type": "number", "description": "Optional timeout in seconds."},
		},
		"additionalProperties": false,
	}
}

func gitCommitSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"dir":             map[string]any{"type": "string", "description": "Optional git repository path relative to the configured workspace root. Defaults to root."},
			"message":         map[string]any{"type": "string", "description": "Commit message."},
			"add_all":         map[string]any{"type": "boolean", "description": "Run git add -A before committing."},
			"amend":           map[string]any{"type": "boolean", "description": "Amend the previous commit."},
			"timeout_seconds": map[string]any{"type": "number", "description": "Optional timeout in seconds."},
		},
		"required":             []string{"message"},
		"additionalProperties": false,
	}
}

func gitStashSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"dir":             map[string]any{"type": "string", "description": "Optional git repository path relative to the configured workspace root. Defaults to root."},
			"action":          map[string]any{"type": "string", "description": "Stash action: push (default), pop, list, or drop."},
			"message":         map[string]any{"type": "string", "description": "Optional message for stash push."},
			"timeout_seconds": map[string]any{"type": "number", "description": "Optional timeout in seconds."},
		},
		"additionalProperties": false,
	}
}

func gitTagSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"dir":             map[string]any{"type": "string", "description": "Optional git repository path relative to the configured workspace root. Defaults to root."},
			"name":            map[string]any{"type": "string", "description": "Tag name to create or delete. Omit to list tags."},
			"message":         map[string]any{"type": "string", "description": "Message for an annotated tag."},
			"delete":          map[string]any{"type": "boolean", "description": "Delete the named tag when true."},
			"list":            map[string]any{"type": "boolean", "description": "List tags. Defaults to true when name is omitted."},
			"timeout_seconds": map[string]any{"type": "number", "description": "Optional timeout in seconds."},
		},
		"additionalProperties": false,
	}
}

func gitCloneSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"url":             map[string]any{"type": "string", "description": "Repository URL to clone."},
			"dir":             map[string]any{"type": "string", "description": "Optional target directory relative to the workspace root."},
			"branch":          map[string]any{"type": "string", "description": "Optional branch to checkout."},
			"depth":           map[string]any{"type": "integer", "description": "Optional shallow clone depth."},
			"timeout_seconds": map[string]any{"type": "number", "description": "Optional timeout in seconds. Defaults to 120."},
		},
		"required":             []string{"url"},
		"additionalProperties": false,
	}
}

func gitPullSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"dir":             map[string]any{"type": "string", "description": "Optional git repository path relative to the configured workspace root. Defaults to root."},
			"remote":          map[string]any{"type": "string", "description": "Remote name. Defaults to origin."},
			"branch":          map[string]any{"type": "string", "description": "Optional branch to pull."},
			"timeout_seconds": map[string]any{"type": "number", "description": "Optional timeout in seconds."},
		},
		"additionalProperties": false,
	}
}

func gitPushSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"dir":             map[string]any{"type": "string", "description": "Optional git repository path relative to the configured workspace root. Defaults to root."},
			"remote":          map[string]any{"type": "string", "description": "Remote name. Defaults to origin."},
			"branch":          map[string]any{"type": "string", "description": "Optional branch to push."},
			"force":           map[string]any{"type": "boolean", "description": "Force push when true."},
			"timeout_seconds": map[string]any{"type": "number", "description": "Optional timeout in seconds."},
		},
		"additionalProperties": false,
	}
}

// --- Git write output schemas ---

func gitBranchOutputSchema() map[string]any {
	return gitEnvelopeSchema(map[string]any{
		"branches": stringArraySchema("List of branch names."),
		"message":  stringSchema("Operation result message."),
	})
}

func gitCommitOutputSchema() map[string]any {
	return gitEnvelopeSchema(map[string]any{
		"hash":    stringSchema("Short commit hash."),
		"message": stringSchema("Commit output message."),
	}, "hash", "message")
}

func gitStashOutputSchema() map[string]any {
	return gitEnvelopeSchema(map[string]any{
		"action": stringSchema("Stash action performed."),
		"output": stringSchema("Stash command output."),
	}, "action", "output")
}

func gitTagOutputSchema() map[string]any {
	return gitEnvelopeSchema(map[string]any{
		"tags":    stringArraySchema("List of tag names."),
		"message": stringSchema("Operation result message."),
	})
}

func gitCloneOutputSchema() map[string]any {
	return objectSchema(map[string]any{
		"url":     stringSchema("Repository URL that was cloned."),
		"dir":     stringSchema("Target directory."),
		"branch":  stringSchema("Branch that was checked out."),
		"message": stringSchema("Clone result message."),
	}, "url", "dir", "message")
}

func gitPullOutputSchema() map[string]any {
	return gitEnvelopeSchema(map[string]any{
		"remote": stringSchema("Remote that was pulled from."),
		"output": stringSchema("Pull command output."),
	}, "remote", "output")
}

func gitPushOutputSchema() map[string]any {
	return gitEnvelopeSchema(map[string]any{
		"remote": stringSchema("Remote that was pushed to."),
		"output": stringSchema("Push command output."),
	}, "remote", "output")
}

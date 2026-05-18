---
name: git-advanced
description: Advanced git operations including rebase, cherry-pick, bisect, and stash management
homepage: https://git-scm.com/docs
user-invocable: true
command-dispatch: tool
command-tool: git-advanced.run
command-arg-mode: raw
metadata:
  openclaw:
    skillKey: dev.git-advanced
    emoji: "\U0001F500"
    requires:
      bins:
        - git
    always: false
---
# Git Advanced

Perform advanced git operations beyond basic add/commit/push workflows.

## Capabilities

- Rebase workflows: interactive rebase helpers, rebase onto
- Cherry-pick commits across branches
- Bisect to find the commit that introduced a bug
- Stash management: save, list, apply, drop
- Reflog recovery: find and restore lost commits
- Worktree management for parallel checkouts
- Submodule operations
- Patch creation and application
- Blame and log analysis

## Usage

### Rebase Operations

```bash
# Rebase current branch onto main
git rebase main

# Rebase last N commits (for squashing/reordering)
# NOTE: avoid -i in non-interactive environments; use fixup/squash directly
git rebase --onto main HEAD~3

# Abort a rebase in progress
git rebase --abort

# Continue after resolving conflicts
git rebase --continue

# Autosquash fixup commits
git commit --fixup=abc1234
git rebase --autosquash main
```

### Cherry-Pick

```bash
# Cherry-pick a single commit
git cherry-pick abc1234

# Cherry-pick a range of commits
git cherry-pick abc1234..def5678

# Cherry-pick without committing (stage only)
git cherry-pick --no-commit abc1234

# Cherry-pick from another remote
git fetch upstream
git cherry-pick upstream/main~3..upstream/main

# Abort a cherry-pick
git cherry-pick --abort
```

### Bisect (Find Bug-Introducing Commit)

```bash
# Start bisect
git bisect start

# Mark current commit as bad
git bisect bad

# Mark a known good commit
git bisect good v1.0.0

# After testing each commit, mark as good or bad
git bisect good   # or: git bisect bad

# Automated bisect with a test command
git bisect start HEAD v1.0.0
git bisect run go test ./pkg/...

# View bisect log
git bisect log

# End bisect and return to original branch
git bisect reset
```

### Stash Management

```bash
# Save changes to stash with a message
git stash push -m "work in progress on feature X"

# Stash including untracked files
git stash push -u -m "including new files"

# List all stashes
git stash list

# Show stash contents
git stash show -p stash@{0}

# Apply most recent stash (keep in stash list)
git stash apply

# Apply and remove from stash list
git stash pop

# Apply a specific stash
git stash apply stash@{2}

# Drop a specific stash
git stash drop stash@{0}

# Clear all stashes
git stash clear
```

### Reflog Recovery

```bash
# View reflog (recent HEAD movements)
git reflog --date=relative

# Recover a deleted branch
git reflog | grep "checkout: moving from deleted-branch"
git checkout -b recovered-branch abc1234

# Undo a hard reset
git reflog
git reset --hard HEAD@{2}

# Find lost commits
git fsck --lost-found
```

### Worktree Management

```bash
# Create a worktree for a branch
git worktree add ../project-hotfix hotfix/v1.2.1

# List worktrees
git worktree list

# Remove a worktree
git worktree remove ../project-hotfix

# Prune stale worktree info
git worktree prune
```

### Log and Analysis

```bash
# Commits by author in date range
git log --author="alice" --since="2024-01-01" --oneline

# Files changed in a commit
git diff-tree --no-commit-id --name-only -r abc1234

# Find when a line was introduced
git log -S "function_name" --oneline

# Blame with ignore whitespace
git blame -w path/to/file.go

# Shortlog (commit count by author)
git shortlog -sn --since="2024-01-01"

# Show merge base between branches
git merge-base main feature-branch
```

### Patch Operations

```bash
# Create a patch from last 3 commits
git format-patch -3

# Create a single patch file
git diff main..feature > feature.patch

# Apply a patch
git apply feature.patch

# Apply a patch with 3-way merge
git am --3way 0001-commit-message.patch
```

## Error Handling

- If rebase encounters conflicts, list the conflicting files with `git diff --name-only --diff-filter=U` and guide the user through resolution.
- If cherry-pick fails due to conflicts, suggest `--no-commit` to inspect changes before committing.
- If bisect gives unexpected results, review the bisect log and consider resetting.
- If stash apply fails due to conflicts, the stash is not dropped. Resolve conflicts, then drop manually.

## Security

- Before any destructive operation (reset --hard, force push, branch -D), confirm with the user and explain the consequences.
- When recovering with reflog, note that reflog entries expire (default 90 days).
- Do not force-push to shared branches (main, develop) without explicit confirmation.
- When creating patches, ensure they do not contain sensitive data (API keys, passwords in config files).

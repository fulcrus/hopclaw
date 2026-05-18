---
name: github-issues
description: Create, search, triage, and manage GitHub issues using existing runtime capabilities.
homepage: https://cli.github.com/manual/gh_issue
user-invocable: true
command-dispatch: tool
command-tool: github-issues.run
command-arg-mode: raw
metadata:
  openclaw:
    skillKey: dev.github-issues
    emoji: "\U0001F41B"
    primaryEnv: GITHUB_TOKEN
    requires:
      bins:
        - gh
      env:
        - GITHUB_TOKEN
    always: false
---
# GitHub Issues

Use existing runtime capabilities to search, triage, and manage GitHub issues. Prefer the dedicated `github-issues.run` tool when it is available in this turn.

Preferred approach:

- Use `github-issues.run` for issue discovery, filtering, reading, commenting, creation, and lifecycle changes.
- Reuse repository, issue number, labels, milestones, and assignee context already present in the conversation or workspace before asking the user for more detail.
- If the current tool list truly lacks the needed GitHub issue capability, use `skill.ensure` before inventing raw `gh issue` commands or custom API requests.

Working rules:

- Distinguish read-only triage from mutating actions such as creating, closing, reopening, labeling, assigning, or commenting.
- Confirm the target repository and the exact public-facing content before posting new issues or comments.
- Keep issue summaries grounded in the actual issue state, labels, assignees, and timestamps returned by the capability.
- Never expose `GITHUB_TOKEN`, private repository data, or sensitive issue content beyond what the user requested.
- Do not teach raw `gh issue` command recipes, shell pipelines, or ad hoc scripts when existing capabilities can complete the task.

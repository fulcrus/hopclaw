---
name: github-pr
description: Create, review, merge, and manage GitHub pull requests using existing runtime capabilities.
homepage: https://cli.github.com/manual/gh_pr
user-invocable: true
command-dispatch: tool
command-tool: github-pr.run
command-arg-mode: raw
metadata:
  openclaw:
    skillKey: dev.github-pr
    emoji: "\U0001F500"
    primaryEnv: GITHUB_TOKEN
    requires:
      bins:
        - gh
        - git
      env:
        - GITHUB_TOKEN
    always: false
---
# GitHub Pull Requests

Use existing runtime capabilities to inspect, review, and manage GitHub pull requests. Prefer the dedicated `github-pr.run` tool when it is available in this turn.

Preferred approach:

- Use `github-pr.run` for PR discovery, status inspection, review actions, merge preparation, and merge execution.
- Reuse repository, branch, PR number, reviewer, and check-status context already present in the conversation or workspace before asking for more identifiers.
- If the current tool list truly lacks the needed GitHub pull request capability, use `skill.ensure` before inventing raw `gh pr` commands or custom API requests.

Working rules:

- Distinguish read-only review work from mutating actions such as creating, commenting, approving, requesting changes, closing, or merging.
- Confirm merge strategy, branch target, reviewer intent, and any public-facing review text before applying irreversible PR actions.
- Treat check results, mergeability, and review state as current operational data that can change between checks.
- Never expose `GITHUB_TOKEN`, private repository metadata, or sensitive review content beyond the requested scope.
- Do not teach raw `gh pr` command recipes, shell pipelines, or ad hoc scripts when existing capabilities can complete the task.

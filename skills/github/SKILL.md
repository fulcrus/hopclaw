---
name: github
description: Work with GitHub repositories, issues, pull requests, and CI using existing runtime capabilities.
homepage: https://cli.github.com
user-invocable: true
command-dispatch: tool
command-tool: github.run
command-arg-mode: raw
metadata:
  openclaw:
    skillKey: dev.github
    emoji: "\U0001F419"
    primaryEnv: GITHUB_TOKEN
    requires:
      bins:
        - gh
      env:
        - GITHUB_TOKEN
    always: false
---
# GitHub

Use existing runtime capabilities to inspect and manage GitHub repositories. Prefer the dedicated `github.run` tool when it is available in this turn.

Preferred approach:

- Use `github.run` for repository lookup, issue and pull request workflows, release browsing, and CI or workflow inspection.
- Reuse owner, repository, branch, issue, PR, and workflow context already present in the conversation or current workspace before asking for more identifiers.
- If the current tool list truly lacks the needed GitHub capability, use `skill.ensure` before inventing raw `gh` commands or custom API requests.

Working rules:

- Separate read-only inspection from state-changing actions such as commenting, labeling, merging, closing, or dispatching workflows.
- Confirm the target repository and the exact mutation before changing public or team-visible state.
- Treat CI status, run logs, and release data as time-sensitive operational information and include context when the freshness matters.
- Never expose `GITHUB_TOKEN`, installation tokens, private repository metadata, or confidential logs in output.
- Do not teach raw `gh` command recipes, shell pipelines, or ad hoc scripts when existing capabilities can complete the task.

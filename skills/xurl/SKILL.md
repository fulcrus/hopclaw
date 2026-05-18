---
name: xurl
description: Search X content and manage posting workflows using existing runtime capabilities.
homepage: https://developer.x.com
user-invocable: true
command-dispatch: tool
command-tool: xurl.run
command-arg-mode: raw
metadata:
  openclaw:
    skillKey: comm.xurl
    emoji: "\U0001F426"
    primaryEnv: X_BEARER_TOKEN
    requires:
      env:
        - X_BEARER_TOKEN
    always: false
---
# X

Use existing runtime capabilities to search X content, inspect timelines, and handle posting workflows. Prefer the dedicated `xurl.run` tool when it is available in this turn.

Preferred approach:

- Use `xurl.run` for search, user lookup, timeline retrieval, and supported posting actions.
- Use existing context already available in the turn to resolve usernames, topics, time ranges, and whether the user wants read-only or posting behavior.
- If the current tool list truly lacks the needed X capability, use `skill.ensure` before inventing manual API requests.

Working rules:

- Confirm any outbound post or thread content before publishing it.
- Treat search results, metrics, and timelines as current snapshots that can change quickly and should be reported with time context when relevant.
- Distinguish read-only analysis from account-mutating actions so the user can verify intent.
- Never expose bearer tokens, OAuth secrets, or private account data in output.
- Do not teach raw REST calls, signature flows, or ad hoc scripts when existing capabilities can complete the task.

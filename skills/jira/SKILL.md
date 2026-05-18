---
name: jira
description: Search, create, and manage Jira work items using existing runtime capabilities.
homepage: https://developer.atlassian.com/cloud/jira/platform/rest/v3/
user-invocable: true
command-dispatch: tool
command-tool: jira.run
command-arg-mode: raw
metadata:
  openclaw:
    skillKey: enterprise.jira
    emoji: "\U0001F3AF"
    primaryEnv: JIRA_URL
    requires:
      env:
        - JIRA_URL
        - JIRA_EMAIL
        - JIRA_API_TOKEN
    always: false
---
# Jira

Use existing runtime capabilities to search, create, and update Jira issues, boards, or sprint data. Prefer the dedicated `jira.run` tool when it is available in this turn.

Preferred approach:

- Use `jira.run` for JQL search, issue retrieval, creation, comments, transitions, assignee changes, and other workflow updates.
- Use planning context already available in the turn to resolve project keys, issue types, sprint names, and status targets before mutating anything.
- If the current tool list truly lacks the needed Jira capability, use `skill.ensure` before inventing manual API requests.

Working rules:

- Confirm project, issue key, workflow transition, and assignee whenever a write could affect delivery tracking or team notifications.
- Preserve the user’s Jira schema, including custom fields and required issue metadata, rather than guessing unsupported values.
- Present JQL results in a human-friendly summary and call out when counts or statuses are snapshots that may change.
- Never expose Jira tokens, internal admin details, or unrelated ticket data in output.
- Do not teach raw REST calls, basic-auth shell flows, or ad hoc scripts when existing capabilities can complete the task.

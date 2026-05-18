---
name: trello
description: Manage Trello boards, lists, and cards using existing runtime capabilities.
homepage: https://developer.atlassian.com/cloud/trello
user-invocable: true
command-dispatch: tool
command-tool: trello.run
command-arg-mode: raw
metadata:
  openclaw:
    skillKey: prod.trello
    emoji: "\U0001F4CB"
    primaryEnv: TRELLO_API_KEY
    requires:
      env:
        - TRELLO_API_KEY
        - TRELLO_TOKEN
    always: false
---
# Trello

Use existing runtime capabilities to manage Trello boards, lists, and cards. Prefer the dedicated `trello.run` tool when it is available in this turn.

Preferred approach:

- Use `trello.run` for board discovery, list management, card creation, card movement, and comment workflows.
- Use current planning context already available in the turn to map tasks to the correct board, list, and card instead of guessing names.
- If the current tool list truly lacks the needed Trello capability, use `skill.ensure` before falling back to generic network calls.

Working rules:

- Confirm destination board, list, and card before writes that could reorganize work or notify teammates.
- Keep card titles, descriptions, labels, due dates, and comments aligned with the user’s existing workflow rather than inventing structure.
- Summarize high-impact changes before executing bulk moves, archival actions, or card creation across many lists.
- Never expose Trello secrets, workspace internals, or private board data beyond the requested scope.
- Do not teach raw REST calls, query-parameter auth, or ad hoc scripts when existing capabilities can complete the task.

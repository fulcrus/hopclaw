---
name: notion
description: Create, search, and manage Notion content using existing runtime capabilities.
homepage: https://developers.notion.com
user-invocable: true
command-dispatch: tool
command-tool: notion.run
command-arg-mode: raw
metadata:
  openclaw:
    skillKey: enterprise.notion
    emoji: "\U0001F4D3"
    primaryEnv: NOTION_API_KEY
    requires:
      env:
        - NOTION_API_KEY
    always: false
---
# Notion

Use existing runtime capabilities to search, create, and update Notion pages or databases. Prefer the dedicated `notion.run` tool when it is available in this turn.

Preferred approach:

- Use `notion.run` for workspace search, page retrieval, database queries, page creation, and property updates.
- Use existing document and task context already available in the turn to map user intent onto the right page, database, or parent object.
- If the current tool list truly lacks the needed Notion capability, use `skill.ensure` before dropping to generic HTTP tooling.

Working rules:

- Confirm the destination workspace object before creating or editing content when multiple matches are plausible.
- Preserve the user’s structure and schema: page titles, database property names, status fields, and parent relationships must match the workspace.
- Summarize the intended mutation before executing any write that could alter important notes, plans, or databases.
- Never expose integration secrets, internal IDs, or unrelated workspace data in output.
- Do not teach raw Notion API requests, shell snippets, or one-off scripts when existing capabilities can complete the task.

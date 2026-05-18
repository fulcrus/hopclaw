---
name: feishu-doc
description: Create, read, and update Feishu/Lark documents using existing runtime capabilities.
homepage: https://open.feishu.cn/document
user-invocable: true
command-dispatch: tool
command-tool: feishu.doc
command-arg-mode: raw
metadata:
  openclaw:
    skillKey: enterprise.feishu-doc
    emoji: "\U0001F4DD"
    primaryEnv: FEISHU_APP_ID
    requires:
      env:
        - FEISHU_APP_ID
        - FEISHU_APP_SECRET
    always: false
---
# Feishu Document

Use existing runtime capabilities to read and update Feishu (Lark) documents. Prefer the dedicated `feishu.doc` tool and any `feishu.doc.*` capabilities already available in this turn.

Preferred approach:

- Use `feishu.doc` or the more specific `feishu.doc.*` capability already exposed in the turn for document discovery, reading, block inspection, and writing.
- Reuse document URLs, doc tokens, folder context, and user-provided structure already present in the conversation instead of asking the user to restate them.
- If the current tool list truly lacks the needed Feishu document capability, use `skill.ensure` before inventing manual Open API requests.

Working rules:

- Separate read-only inspection from content-mutating actions so the user can verify intent before any write.
- Confirm titles, destination folders, target blocks, and overwrite behavior before editing shared documents.
- Preserve the existing document structure unless the user explicitly asked for a rewrite or reorganization.
- Never expose `FEISHU_APP_ID`, `FEISHU_APP_SECRET`, access tokens, or private document internals in output.
- Do not teach raw HTTP, tenant-token exchange flows, or ad hoc scripts when existing capabilities can complete the task.

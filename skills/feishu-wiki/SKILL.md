---
name: feishu-wiki
description: Browse and manage Feishu/Lark wiki spaces and pages using existing runtime capabilities.
homepage: https://open.feishu.cn/document
user-invocable: true
command-dispatch: tool
command-tool: feishu.wiki
command-arg-mode: raw
metadata:
  openclaw:
    skillKey: enterprise.feishu-wiki
    emoji: "\U0001F4DA"
    primaryEnv: FEISHU_APP_ID
    requires:
      env:
        - FEISHU_APP_ID
        - FEISHU_APP_SECRET
    always: false
---
# Feishu Wiki

Use existing runtime capabilities to browse and manage Feishu (Lark) wiki spaces and pages. Prefer the dedicated `feishu.wiki` tool and any related Feishu wiki capabilities already available in this turn.

Preferred approach:

- Use `feishu.wiki` for space discovery, node browsing, page lookup, and supported wiki mutations.
- Reuse space IDs, node tokens, page links, and parent-child structure already present in the conversation instead of guessing paths.
- If the current tool list truly lacks the needed Feishu wiki capability, use `skill.ensure` before inventing manual Open API requests.

Working rules:

- Distinguish read-only browsing from page creation or updates so the user can verify intent before shared knowledge is changed.
- Confirm the target wiki space, parent node, page title, and placement before writes.
- Keep navigation trees and page summaries aligned with the existing wiki hierarchy rather than flattening or reformatting it arbitrarily.
- Never expose `FEISHU_APP_ID`, `FEISHU_APP_SECRET`, access tokens, or private workspace structure beyond the requested scope.
- Do not teach raw HTTP, tenant-token exchange flows, or ad hoc scripts when existing capabilities can complete the task.

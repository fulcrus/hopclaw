---
name: feishu-rewrite
description: Rewrite or polish Feishu document content with controlled write-back
homepage: https://open.feishu.cn/
user-invocable: true
metadata:
  openclaw:
    skillKey: enterprise.feishu-rewrite
    emoji: "\U0001F58A"
    always: true
---
# Feishu Rewrite

Rewrite Feishu/Lark content using `feishu-suite` tools.

## Default Workflow

1. Resolve the user-provided Feishu URL when needed with `feishu.url.resolve`.
2. Read the current source content with `feishu.doc.read`.
3. Infer the requested rewrite target:
   - shorter
   - clearer
   - more formal
   - more persuasive
   - more structured
   - translated or localized
4. Produce the rewritten content in markdown.

## Safety Rule

Do not overwrite the original document unless the user clearly asked for in-place rewrite.

If the user asked to "rewrite this document" and the intent is clearly write-back:

- rewrite the content
- preserve the main meaning and core facts
- keep dates, names, IDs, and quoted facts stable unless the user asked to change them
- write back with `feishu.doc.write`

If the intent is not explicit, show the rewritten draft first.

## Good Rewrite Behavior

- keep the user's target audience in mind
- improve structure with headings and lists when useful
- remove repetition
- keep factual claims unchanged unless the user asks for substantive edits
- do not invent product, legal, medical, or financial claims

## Write-Back Format

When writing back:

- use markdown headings deliberately
- keep tables as markdown tables when the content is tabular
- verify the write result and tell the user whether readback passed

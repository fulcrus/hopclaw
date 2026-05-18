---
name: feishu-summarize
description: Summarize Feishu documents, wiki pages, and bitable data with optional write-back
homepage: https://open.feishu.cn/
user-invocable: true
metadata:
  openclaw:
    skillKey: enterprise.feishu-summarize
    emoji: "\U0001F4CB"
    always: true
---
# Feishu Summarize

Summarize Feishu/Lark content using `feishu-suite` product tools.

## Default Workflow

1. If the user provides a Feishu URL, resolve it first with `feishu.url.resolve`.
2. For docx content, read the source with `feishu.doc.read`.
3. For wiki nodes, resolve the node and then read the underlying object.
4. For bitable data, use `feishu.bitable.meta.get`, `feishu.bitable.field.list`, and `feishu.bitable.record.list`.
5. Produce a concise summary first, then expand only if the user asked for detail.

## Output Shape

Prefer this structure unless the user asked for another format:

- TL;DR
- Key points
- Risks or gaps
- Suggested next actions

## Write-Back Rule

Do not modify external Feishu content by default.

Only write a summary back with `feishu.doc.write` if the user explicitly asks to:

- save the summary into the original document
- create a summary document
- append a summary section

When writing back:

- tell the user what will be changed
- preserve the original title unless told otherwise
- use clear headings in the generated markdown

## Bitable Guidance

For bitable summaries:

- identify the important fields first
- summarize counts, trends, outliers, and missing values
- avoid dumping every row unless the user asked for raw output

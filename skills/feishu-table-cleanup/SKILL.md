---
name: feishu-table-cleanup
description: Clean up Feishu bitable records or markdown tables in Feishu docs
homepage: https://open.feishu.cn/
user-invocable: true
metadata:
  openclaw:
    skillKey: enterprise.feishu-table-cleanup
    emoji: "\U0001F9F9"
    always: true
---
# Feishu Table Cleanup

Clean and normalize tabular content in Feishu/Lark.

## Supported Targets

- Feishu bitable tables
- Markdown tables inside Feishu docx documents

## Bitable Workflow

1. Resolve the target with `feishu.url.resolve` or `feishu.bitable.meta.get`.
2. Inspect schema using `feishu.bitable.field.list`.
3. Read records with `feishu.bitable.record.list`.
4. Normalize rows using `feishu.bitable.record.update`.

Typical cleanup operations:

- trim whitespace
- normalize date/text formatting
- standardize enum values
- fill derived fields when the transformation is clear
- flag missing or inconsistent values

Do not silently change business meaning. If normalization rules are ambiguous, state the rule before applying it.

## Doc Table Workflow

1. Read the doc with `feishu.doc.read`.
2. Rewrite the markdown table into a cleaner structure.
3. Write back only if the user explicitly asked to save the cleanup result.

## Limits

Current `feishu-suite` support is strongest for:

- row normalization
- schema inspection
- record-level updates
- markdown table cleanup

If the task requires destructive row deletion, field creation, or field deletion, say that the current tool surface is limited and propose the closest safe action.

---
name: summarize
description: Summarize web pages, documents, and long text into concise overviews
homepage: https://github.com/openclaw/skills
user-invocable: true
command-dispatch: tool
command-tool: summarize.run
command-arg-mode: raw
metadata:
  openclaw:
    skillKey: util.summarize
    emoji: "\U0001F4CB"
    always: true
---
# Summarize

Summarize long content into concise, structured overviews.

## Capabilities

- Summarize web pages by URL
- Summarize pasted text or documents
- Generate bullet-point or paragraph summaries
- Extract key facts, dates, and action items
- Support for multiple languages

## Usage

When the user asks to summarize content, first fetch or read the content,
then produce a structured summary with:

1. **TL;DR** — One sentence summary
2. **Key Points** — 3-5 bullet points
3. **Details** — Expanded summary if needed
4. **Action Items** — If applicable

## Examples

- "Summarize this article: [URL]"
- "Give me a brief summary of this document"
- "What are the key points from this text?"

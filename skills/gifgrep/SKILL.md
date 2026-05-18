---
name: gifgrep
description: Search and retrieve GIFs using existing runtime capabilities.
homepage: https://developers.giphy.com
user-invocable: true
command-dispatch: tool
command-tool: gifgrep.run
command-arg-mode: raw
metadata:
  openclaw:
    skillKey: media.gifgrep
    emoji: "\U0001F39E\uFE0F"
    primaryEnv: GIPHY_API_KEY
    requires:
      env:
        - GIPHY_API_KEY
    always: false
---
# GIF Grep

Use existing runtime capabilities to search and retrieve GIFs. Prefer the dedicated `gifgrep.run` tool when it is available in this turn.

Preferred approach:

- Use `gifgrep.run` for search, trending, random, and translate-to-GIF requests.
- Use the current text or ranking capabilities in the turn to narrow, sort, or explain GIF choices when needed.
- If the current tool list truly lacks the needed capability, use `skill.ensure` before reaching for raw HTTP examples.

Working rules:

- Ask for search intent, tone, and rating constraints when they materially affect the result.
- Return concise result sets with the direct GIF URL and enough context for the user to choose.
- Default to safer content filtering unless the user explicitly asks otherwise.
- Never expose API keys or account secrets in output.
- Do not teach `curl`, `python3`, or manual JSON scraping when existing capabilities can complete the task.

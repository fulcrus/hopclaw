---
name: weather
description: Get current weather and forecast information with current-data capabilities.
user-invocable: true
metadata:
  openclaw:
    skillKey: util.weather
    emoji: "\u26C5"
    always: false
---
# Weather

Weather questions are time-sensitive. Always use current-data capabilities instead of answering from memory.

Preferred approach:

- If a dedicated weather capability is already available, use it.
- Otherwise prefer `search.web` or another current-data discovery capability to find a trustworthy public weather source for the requested city or region.
- Use `net.fetch` only after you already have a specific trustworthy public URL, or when the user explicitly provided the weather URL to query.
- If the current turn only exposes direct fetch but no trustworthy weather source has been identified yet, use `skill.ensure` once to recover a better current-data capability before falling back to ad hoc fetches.

Working rules:

- Ask for the location only when it is not already clear from the request or saved user context.
- Include the date, local time, and timezone when presenting forecasts.
- For forecasts, summarize the next relevant window instead of dumping raw provider output.
- Do not hard-code any single weather provider as a universal fallback when better discovery or dedicated weather capabilities are available.
- If no current-data capability is available, say clearly that you cannot verify live weather and that any answer would be unreliable.

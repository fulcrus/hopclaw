---
name: goplaces
description: Search places, retrieve place details, and get directions using existing runtime capabilities.
homepage: https://developers.google.com/maps/documentation/places
user-invocable: true
command-dispatch: tool
command-tool: goplaces.run
command-arg-mode: raw
metadata:
  openclaw:
    skillKey: util.goplaces
    emoji: "\U0001F4CD"
    primaryEnv: GOOGLE_PLACES_API_KEY
    requires:
      env:
        - GOOGLE_PLACES_API_KEY
    always: false
---
# GoPlaces

Use existing runtime capabilities to search places, retrieve business details, and compute directions. Prefer the dedicated `goplaces.run` tool when it is available in this turn.

Preferred approach:

- Use `goplaces.run` for text search, nearby search, place details, autocomplete, and route lookups.
- Use existing location context already present in the turn to disambiguate cities, neighborhoods, coordinates, or travel modes.
- If the current tool list truly lacks the needed places capability, use `skill.ensure` before falling back to generic web fetching.

Working rules:

- Confirm the geographic scope when the query is ambiguous, especially for common place names or chains with many branches.
- Call out that ratings, opening hours, traffic, and availability can change quickly and should be presented as current snapshots rather than stable facts.
- Minimize unnecessary detail when the user just wants the best candidates, but include address, distance, and distinguishing metadata when comparing options.
- Never expose API keys, billing details, or internal place identifiers unless the user explicitly needs them for a follow-up task.
- Do not teach raw Google Maps HTTP requests or shell snippets when existing capabilities can complete the task.

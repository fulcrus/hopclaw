---
name: spotify
description: Search music and control Spotify playback using existing runtime capabilities.
homepage: https://developer.spotify.com/documentation/web-api
user-invocable: true
command-dispatch: tool
command-tool: spotify.run
command-arg-mode: raw
metadata:
  openclaw:
    skillKey: media.spotify
    emoji: "\U0001F3B5"
    primaryEnv: SPOTIFY_CLIENT_ID
    requires:
      env:
        - SPOTIFY_CLIENT_ID
        - SPOTIFY_CLIENT_SECRET
    always: false
---
# Spotify

Use existing runtime capabilities to search music, inspect playback state, and manage Spotify actions. Prefer the dedicated `spotify.run` tool when it is available in this turn.

Preferred approach:

- Use `spotify.run` for search, browse, playlist management, playback inspection, and playback control when authorized tokens are available.
- Use existing conversation context already available in the turn to resolve ambiguous artist, track, album, or playlist names before acting.
- If the current tool list truly lacks the needed Spotify capability, use `skill.ensure` before attempting raw OAuth or HTTP flows.

Working rules:

- Separate browse-only actions from user-playback actions because playback often needs different authorization and an active device.
- Confirm queueing, playlist edits, or playback changes before they affect the user’s listening session.
- Treat “currently playing” and device state as live operational data that may change between checks.
- Never expose client secrets, refresh tokens, or account identifiers in output.
- Do not teach raw token exchanges, shell pipelines, or manual API calls when existing capabilities can complete the task.

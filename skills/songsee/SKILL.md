---
name: songsee
description: Identify songs and retrieve lyric-related results using existing runtime capabilities.
homepage: https://audd.io
user-invocable: true
command-dispatch: tool
command-tool: songsee.run
command-arg-mode: raw
metadata:
  openclaw:
    skillKey: audio.songsee
    emoji: "\U0001F3B5"
    primaryEnv: AUDD_API_KEY
    requires:
      env:
        - AUDD_API_KEY
    always: false
---
# SongSee

Use existing runtime capabilities to identify songs from audio, look up lyrics, and return music metadata. Prefer the dedicated `songsee.run` tool when it is available in this turn.

Preferred approach:

- Use `songsee.run` for audio-based song recognition, lyric lookup, and related music metadata retrieval.
- Use existing file or URL context already available in the turn to determine whether the source is an uploaded clip, a remote media link, or a text lyric query.
- If the current tool list truly lacks the needed recognition capability, use `skill.ensure` before attempting custom API calls.

Working rules:

- Be explicit about confidence and ambiguity when multiple songs or lyric matches are plausible.
- Tell the user when short, noisy, or low-quality audio may reduce recognition accuracy.
- Keep the answer focused on the user’s goal: identification, lyrics, artist metadata, or follow-up playback/search actions.
- Never expose API keys, raw provider payloads, or copyrighted content beyond what the task reasonably requires.
- Do not teach raw HTTP uploads, shell snippets, or ad hoc scripts when existing capabilities can complete the task.

---
name: openai-whisper-api
description: Transcribe and translate audio using existing runtime capabilities.
homepage: https://platform.openai.com/docs/guides/speech-to-text
user-invocable: true
command-dispatch: tool
command-tool: openai-whisper-api.run
command-arg-mode: raw
metadata:
  openclaw:
    skillKey: ai.openai-whisper-api
    emoji: "\U0001F3A4"
    primaryEnv: OPENAI_API_KEY
    requires:
      env:
        - OPENAI_API_KEY
    always: false
---
# OpenAI Whisper API

Use existing runtime capabilities to transcribe audio, translate speech to English, and return caption-friendly formats. Prefer the dedicated `openai-whisper-api.run` tool when it is available in this turn.

Preferred approach:

- Use `openai-whisper-api.run` for transcription, translation, format selection, and timestamp-aware outputs.
- Use existing file-handling capabilities already available in the turn to locate audio inputs and place resulting text artifacts where the user expects them.
- If the current tool list truly lacks the needed speech-to-text capability, use `skill.ensure` before dropping to raw provider requests.

Working rules:

- Clarify output format only when it materially changes the result, such as plain text versus JSON versus subtitle formats.
- Tell the user when file size, audio quality, or language hints may materially affect accuracy or latency.
- Treat transcriptions as generated interpretations of audio and preserve uncertainty when speech is unclear.
- Never expose API keys, signed upload details, or sensitive transcript content beyond the requested scope.
- Do not teach multipart HTTP requests, shell uploads, or ad hoc scripts when existing capabilities can complete the task.

---
name: sag
description: Text-to-speech using system TTS engines (say on macOS, espeak on Linux)
user-invocable: true
command-dispatch: tool
command-tool: sag.run
command-arg-mode: raw
metadata:
  openclaw:
    skillKey: audio.sag
    emoji: "\U0001F50A"
    requires:
      anyBins:
        - say
        - espeak
    always: false
---
# Sag (System TTS)

Text-to-speech using system TTS engines: `say` on macOS, `espeak` on Linux.

## Capabilities

- Convert text to speech using system TTS
- Control voice selection, rate, and volume
- Save speech to audio files
- List available voices

## Usage

### macOS (say)

```bash
# Speak text
say "Hello, world!"

# Speak with a specific voice
say -v Samantha "Hello, world!"

# Control rate (words per minute, default ~175)
say -r 200 "Speaking faster now"

# Save to audio file
say -o /tmp/speech.aiff "Text to save as audio"

# Save as specific format
say -o /tmp/speech.m4a --data-format=aac "Text to save"

# List all available voices
say -v '?'

# Speak from a file
say -f /tmp/script.txt
```

### Linux (espeak)

```bash
# Speak text
espeak "Hello, world!"

# Speak with a specific voice
espeak -v en+f3 "Hello with female voice"

# Control rate (80-450 words per minute, default 175)
espeak -s 200 "Speaking faster"

# Control volume (0-200, default 100)
espeak -a 150 "Speaking louder"

# Control pitch (0-99, default 50)
espeak -p 70 "Higher pitch"

# Save to WAV file
espeak -w /tmp/speech.wav "Text to save"

# List available voices
espeak --voices

# Speak from a file
espeak -f /tmp/script.txt
```

## Examples

- `say "Hello from HopClaw"`
- `say -v Alex -r 180 "Adjustable voice and rate"`
- `say -o /tmp/speech.aiff "Save to file"`
- `espeak "Hello from HopClaw"`
- `espeak -w /tmp/speech.wav "Save to WAV"`

## Error Handling

- On macOS, `say` is always available. On Linux, `espeak` may need to be installed.
- If no audio output device is available, use file output (`-o` / `-w`) instead.
- Long texts may take time to synthesize. Consider breaking into paragraphs.

## Security

- Be mindful of the environment when playing audio aloud.
- Saved audio files may contain sensitive content. Clean up temporary files.

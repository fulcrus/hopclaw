---
name: sherpa-onnx-tts
description: Offline text-to-speech with ONNX models via sherpa-onnx
homepage: https://github.com/k2-fsa/sherpa-onnx
user-invocable: true
command-dispatch: tool
command-tool: sherpa-onnx-tts.run
command-arg-mode: raw
metadata:
  openclaw:
    skillKey: audio.sherpa-onnx-tts
    emoji: "\U0001F5E3\uFE0F"
    requires:
      bins:
        - sherpa-onnx-offline-tts
    always: false
---
# Sherpa ONNX TTS

Offline text-to-speech with ONNX models using the sherpa-onnx toolkit.

## Capabilities

- Generate speech from text entirely offline
- Support for multiple voices and languages via ONNX models
- Save output as WAV audio files
- Control speech speed and speaker ID
- No internet connection or API keys required

## Usage

### Basic TTS

```bash
# Generate speech with VITS model
sherpa-onnx-offline-tts \
  --vits-model=/path/to/model.onnx \
  --vits-tokens=/path/to/tokens.txt \
  --output-filename=/tmp/output.wav \
  "Hello, this is offline text-to-speech."

# Generate with a specific speaker (multi-speaker model)
sherpa-onnx-offline-tts \
  --vits-model=/path/to/model.onnx \
  --vits-tokens=/path/to/tokens.txt \
  --sid=1 \
  --output-filename=/tmp/output.wav \
  "Different speaker voice."
```

### Speed Control

```bash
# Speak faster (speed > 1.0)
sherpa-onnx-offline-tts \
  --vits-model=/path/to/model.onnx \
  --vits-tokens=/path/to/tokens.txt \
  --speed=1.5 \
  --output-filename=/tmp/fast.wav \
  "Speaking at 1.5x speed."

# Speak slower (speed < 1.0)
sherpa-onnx-offline-tts \
  --vits-model=/path/to/model.onnx \
  --vits-tokens=/path/to/tokens.txt \
  --speed=0.7 \
  --output-filename=/tmp/slow.wav \
  "Speaking slowly and clearly."
```

### Model Setup

Popular models can be downloaded from the sherpa-onnx releases:

```bash
# Download a VITS English model (example)
wget https://github.com/k2-fsa/sherpa-onnx/releases/download/tts-models/vits-piper-en_US-lessac-medium.tar.bz2
tar xf vits-piper-en_US-lessac-medium.tar.bz2
```

## Examples

- `sherpa-onnx-offline-tts --vits-model=model.onnx --vits-tokens=tokens.txt --output-filename=/tmp/out.wav "Hello"`
- `sherpa-onnx-offline-tts --vits-model=model.onnx --vits-tokens=tokens.txt --speed=1.2 --output-filename=/tmp/out.wav "Fast speech"`

## Error Handling

- If the model file is not found, suggest downloading from the sherpa-onnx releases page.
- Ensure the tokens file matches the model version.
- Large texts may take significant time to synthesize offline. Consider splitting.
- WAV output can be large. Suggest converting to MP3 with ffmpeg if size is a concern.

## Security

- All processing is local. No data is sent to external servers.
- Model files can be large (50-200 MB). Verify disk space before downloading.

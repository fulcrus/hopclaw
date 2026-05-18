---
name: video-frames
description: Extract frames, create thumbnails, and inspect video files using ffmpeg
homepage: https://ffmpeg.org
user-invocable: true
command-dispatch: tool
command-tool: video-frames.run
command-arg-mode: raw
metadata:
  openclaw:
    skillKey: media.video-frames
    emoji: "\U0001F3AC"
    requires:
      bins:
        - ffmpeg
    always: false
---
# Video Frames

Extract frames, create thumbnails, and inspect video files using ffmpeg.

## Capabilities

- Extract individual frames from video at specific timestamps
- Extract frames at regular intervals (e.g., one per second)
- Create thumbnail grids from video
- Get video metadata and duration
- Convert between video formats
- Extract audio from video

## Usage

### Extracting Frames

```bash
# Extract a single frame at a specific timestamp
ffmpeg -ss 00:01:30 -i input.mp4 -frames:v 1 /tmp/frame.png

# Extract one frame per second
ffmpeg -i input.mp4 -vf "fps=1" /tmp/frames_%04d.png

# Extract one frame every 10 seconds
ffmpeg -i input.mp4 -vf "fps=1/10" /tmp/frames_%04d.png

# Extract first frame only
ffmpeg -i input.mp4 -frames:v 1 /tmp/first_frame.png
```

### Creating Thumbnails

```bash
# Create a thumbnail at 25% of video duration
ffmpeg -ss 00:00:30 -i input.mp4 -frames:v 1 -vf "scale=320:-1" /tmp/thumb.jpg

# Create a grid of thumbnails (4x4)
ffmpeg -i input.mp4 -vf "select=not(mod(n\,100)),scale=160:-1,tile=4x4" -frames:v 1 /tmp/grid.png
```

### Video Information

```bash
# Get video metadata
ffprobe -v quiet -print_format json -show_format -show_streams input.mp4

# Get duration only
ffprobe -v quiet -show_entries format=duration -of csv="p=0" input.mp4

# Get resolution
ffprobe -v quiet -select_streams v:0 -show_entries stream=width,height -of csv="p=0" input.mp4
```

### Audio Extraction

```bash
# Extract audio as MP3
ffmpeg -i input.mp4 -vn -acodec libmp3lame /tmp/audio.mp3

# Extract audio as WAV
ffmpeg -i input.mp4 -vn /tmp/audio.wav
```

## Examples

- `ffmpeg -ss 00:01:00 -i input.mp4 -frames:v 1 /tmp/frame.png`
- `ffprobe -v quiet -print_format json -show_format input.mp4`
- `ffmpeg -i input.mp4 -vf "fps=1" /tmp/frame_%04d.png`

## Error Handling

- If the input file does not exist, report the error and suggest checking the path.
- For unsupported codecs, suggest installing additional codec libraries.
- Large videos may take time to process. Use `-ss` before `-i` for faster seeking.
- Always use `/tmp/` for output files unless the user specifies otherwise.

---
name: camsnap
description: Capture webcam photos and short video clips
user-invocable: true
command-dispatch: tool
command-tool: camsnap.run
command-arg-mode: raw
metadata:
  openclaw:
    skillKey: iot.camsnap
    emoji: "\U0001F4F8"
    requires:
      anyBins:
        - ffmpeg
        - imagesnap
    always: false
---
# CamSnap

Capture webcam photos and short video clips using ffmpeg or imagesnap.

## Capabilities

- Capture a single photo from the webcam
- Record short video clips
- List available camera devices
- Capture with custom resolution and format

## Usage

### macOS with imagesnap

```bash
# Capture a photo (default camera)
imagesnap /tmp/webcam.jpg

# Capture with a warm-up delay (recommended for better exposure)
imagesnap -w 1.0 /tmp/webcam.jpg

# List available cameras
imagesnap -l

# Capture from a specific camera
imagesnap -d "FaceTime HD Camera" /tmp/webcam.jpg
```

### Cross-Platform with ffmpeg

```bash
# List available video devices (macOS)
ffmpeg -f avfoundation -list_devices true -i "" 2>&1 | grep -E "^\[AVFoundation"

# List available video devices (Linux)
v4l2-ctl --list-devices 2>/dev/null || ls /dev/video*

# Capture a single frame (macOS)
ffmpeg -f avfoundation -framerate 30 -i "0" -frames:v 1 /tmp/webcam.jpg

# Capture a single frame (Linux)
ffmpeg -f v4l2 -framerate 30 -i /dev/video0 -frames:v 1 /tmp/webcam.jpg

# Record a 5-second video clip (macOS)
ffmpeg -f avfoundation -framerate 30 -i "0" -t 5 /tmp/clip.mp4

# Record a 5-second video clip (Linux)
ffmpeg -f v4l2 -framerate 30 -i /dev/video0 -t 5 /tmp/clip.mp4
```

### Resolution Control

```bash
# Capture at specific resolution (macOS)
ffmpeg -f avfoundation -video_size 1280x720 -framerate 30 -i "0" -frames:v 1 /tmp/webcam_hd.jpg

# Capture at specific resolution (Linux)
ffmpeg -f v4l2 -video_size 1280x720 -framerate 30 -i /dev/video0 -frames:v 1 /tmp/webcam_hd.jpg
```

## Examples

- `imagesnap -w 1.0 /tmp/webcam.jpg`
- `ffmpeg -f avfoundation -framerate 30 -i "0" -frames:v 1 /tmp/webcam.jpg`
- `imagesnap -l`

## Error Handling

- If no camera is detected, list available devices first.
- On macOS, camera access requires permission. The user may see a system prompt.
- Adding a warm-up delay (`-w 1.0` for imagesnap) helps with exposure adjustment.
- Always save to `/tmp/` or a user-specified directory.

## Security

- Webcam capture may record sensitive environments. Always inform the user before capturing.
- Never capture without explicit user request.
- Delete temporary captures when no longer needed.

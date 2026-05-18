---
name: screenshot
description: Capture screenshots of the screen, windows, or specific regions
homepage: https://ss64.com/mac/screencapture.html
user-invocable: true
command-dispatch: tool
command-tool: screenshot.capture
command-arg-mode: raw
metadata:
  openclaw:
    skillKey: util.screenshot
    emoji: "\U0001F4F7"
    requires:
      anyBins:
        - screencapture
        - gnome-screenshot
        - scrot
        - import
    always: false
---
# Screenshot

Capture screenshots of the entire screen, specific windows, or selected regions.

## Capabilities

- Capture full screen screenshots
- Capture specific windows
- Capture selected regions
- Capture with a delay (timed screenshots)
- Save to file in PNG, JPEG, or other formats
- Copy screenshot to clipboard
- Capture specific display (multi-monitor setups)

## Platform Support

This skill uses OS-specific tools:

- **macOS**: `screencapture` (built-in)
- **Linux (GNOME)**: `gnome-screenshot`
- **Linux (generic)**: `scrot` or ImageMagick `import`

## Usage

### macOS (screencapture)

```bash
# Full screen to file
screencapture /tmp/screenshot.png

# Specific window (interactive selection)
screencapture -w /tmp/window.png

# Region selection (interactive)
screencapture -s /tmp/region.png

# Timed capture (5 second delay)
screencapture -T 5 /tmp/timed.png

# Capture to clipboard instead of file
screencapture -c

# Capture without shadow (windows)
screencapture -o -w /tmp/window-no-shadow.png

# Capture specific display (display 1)
screencapture -D 1 /tmp/display1.png

# Save as JPEG with quality
screencapture -t jpg /tmp/screenshot.jpg

# Silent mode (no camera sound)
screencapture -x /tmp/silent.png
```

### Linux (gnome-screenshot)

```bash
# Full screen
gnome-screenshot -f /tmp/screenshot.png

# Current window
gnome-screenshot -w -f /tmp/window.png

# Selected area
gnome-screenshot -a -f /tmp/region.png

# With delay (5 seconds)
gnome-screenshot -d 5 -f /tmp/timed.png

# Include pointer
gnome-screenshot -p -f /tmp/with-pointer.png
```

### Linux (scrot)

```bash
# Full screen
scrot /tmp/screenshot.png

# Selected window (click to select)
scrot -s /tmp/region.png

# With delay (5 seconds)
scrot -d 5 /tmp/timed.png

# Current focused window
scrot -u /tmp/focused.png

# With quality setting (1-100)
scrot -q 90 /tmp/high-quality.png
```

### Linux (ImageMagick import)

```bash
# Full screen
import -window root /tmp/screenshot.png

# Interactive region selection
import /tmp/region.png

# Specific window by ID
import -window 0x12345 /tmp/window.png
```

### Cross-Platform Strategy

Detect the platform and use the appropriate tool:

```bash
# Auto-detect and capture
if command -v screencapture >/dev/null 2>&1; then
    screencapture -x /tmp/screenshot.png
elif command -v gnome-screenshot >/dev/null 2>&1; then
    gnome-screenshot -f /tmp/screenshot.png
elif command -v scrot >/dev/null 2>&1; then
    scrot /tmp/screenshot.png
elif command -v import >/dev/null 2>&1; then
    import -window root /tmp/screenshot.png
else
    echo "No screenshot tool available"
    exit 1
fi
```

## Output Format

After capturing, report the result:

```
Screenshot saved: /tmp/screenshot.png
Size: 1920x1080
File size: 245 KB
Format: PNG
```

Use `file` and `identify` (ImageMagick) to get image metadata when available:

```bash
file /tmp/screenshot.png
identify /tmp/screenshot.png 2>/dev/null || true
```

## Error Handling

- If no screenshot tool is found, list available package managers and suggest installation commands.
- On headless Linux (no display), `DISPLAY` environment variable must be set. Inform the user.
- If the file path is not writable, suggest `/tmp/` as an alternative.
- If interactive selection is needed but not possible (e.g., in a headless session), fall back to full screen capture.

## Security

- Screenshots may capture sensitive information (passwords, personal data, credentials).
- Always save to `/tmp/` or a user-specified directory. Never save to shared or public directories.
- Inform the user what will be captured before taking the screenshot.
- Delete temporary screenshots when they are no longer needed.

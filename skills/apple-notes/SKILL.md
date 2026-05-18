---
name: apple-notes
description: Create, read, search, and organize notes in Apple Notes via osascript
homepage: https://support.apple.com/guide/notes
user-invocable: true
command-dispatch: tool
command-tool: apple-notes.run
command-arg-mode: raw
metadata:
  openclaw:
    skillKey: prod.apple-notes
    emoji: "\U0001F4DD"
    requires:
      bins:
        - osascript
    os:
      - darwin
    always: false
---
# Apple Notes

Create, read, search, and organize notes in Apple Notes using AppleScript.

## Capabilities

- Create new notes with title and body content
- Read and display existing notes
- Search notes by keyword
- Delete notes
- List all folders
- Move notes between folders
- List notes in a specific folder

## Platform

macOS only. Uses `osascript` to interact with the Notes application via AppleScript.

## Usage

Use AppleScript commands via `osascript` to manage Apple Notes. The Notes app does not need to be visible but must be available on the system.

### Creating Notes

```bash
osascript -e 'tell application "Notes" to make new note at folder "Notes" with properties {name:"Meeting Notes", body:"<h1>Meeting Notes</h1><p>Agenda items...</p>"}'
```

### Reading Notes

```bash
# List all notes with their names
osascript -e 'tell application "Notes" to get name of every note'

# Get body of a specific note
osascript -e 'tell application "Notes" to get body of note "Meeting Notes"'
```

### Searching Notes

```bash
# Search notes by keyword (returns matching note names)
osascript -e 'tell application "Notes"
  set matchingNotes to {}
  repeat with n in every note
    if body of n contains "keyword" then
      set end of matchingNotes to name of n
    end if
  end repeat
  return matchingNotes
end tell'
```

### Managing Folders

```bash
# List all folders
osascript -e 'tell application "Notes" to get name of every folder'

# List notes in a folder
osascript -e 'tell application "Notes" to get name of every note in folder "Work"'
```

## Examples

- `osascript -e 'tell application "Notes" to get name of every note'`
- `osascript -e 'tell application "Notes" to make new note at folder "Notes" with properties {name:"Quick Note", body:"Content here"}'`
- `osascript -e 'tell application "Notes" to get name of every folder'`

## Error Handling

- If Apple Notes is not available, inform the user that this skill requires macOS.
- Note names must be unique within a folder. If a duplicate is detected, suggest an alternative name.
- HTML formatting is supported in note bodies. Use `<h1>`, `<p>`, `<ul>` tags for rich content.

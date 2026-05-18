---
name: bear-notes
description: Create, search, and tag notes in Bear app via osascript and x-callback-url
homepage: https://bear.app
user-invocable: true
command-dispatch: tool
command-tool: bear-notes.run
command-arg-mode: raw
metadata:
  openclaw:
    skillKey: prod.bear-notes
    emoji: "\U0001F43B"
    requires:
      bins:
        - osascript
    os:
      - darwin
    always: false
---
# Bear Notes

Create, search, and tag notes in the Bear app using AppleScript and x-callback-url.

## Capabilities

- Create new notes with markdown content
- Search notes by keyword or tag
- Tag and untag notes
- Export notes as markdown
- List all tags
- Open specific notes in Bear
- Append or prepend content to existing notes

## Platform

macOS only. Requires the Bear app to be installed. Uses `osascript` with `open` command for x-callback-url schemes.

## Usage

Bear uses x-callback-url schemes for automation. Use `open` with the `bear://` URL scheme.

### Creating Notes

```bash
# Create a note with title and body
open "bear://x-callback-url/create?title=Meeting%20Notes&text=Agenda%20items%20here&tags=work,meetings"

# Create a note via AppleScript
osascript -e 'open location "bear://x-callback-url/create?title=Quick%20Note&text=Content%20here"'
```

### Searching Notes

```bash
# Search by keyword
open "bear://x-callback-url/search?term=project%20plan"

# Search by tag
open "bear://x-callback-url/search?tag=work"
```

### Managing Tags

```bash
# Add a tag to a note
open "bear://x-callback-url/add-tag?id=NOTE_ID&tags=important"

# List all tags (via AppleScript and Bear's SQLite database)
sqlite3 ~/Library/Group\ Containers/9K33E3U3T4.net.shinyfrog.bear/Application\ Data/database.sqlite \
  "SELECT ZTITLE FROM ZSFNOTETAG ORDER BY ZTITLE"
```

### Exporting Notes

```bash
# Open a note for viewing (can then be exported)
open "bear://x-callback-url/open-note?title=Meeting%20Notes"
```

## Examples

- `open "bear://x-callback-url/create?title=New%20Note&text=Content"`
- `open "bear://x-callback-url/search?term=keyword"`
- `open "bear://x-callback-url/search?tag=work"`

## Error Handling

- If Bear is not installed, inform the user and suggest installation from the Mac App Store.
- URL-encode all parameters in x-callback-url schemes.
- Note IDs are required for some operations. Search first to find the target note.

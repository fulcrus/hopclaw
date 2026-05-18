---
name: things-mac
description: Manage todos, projects, and areas in Things 3 via osascript and URL schemes
homepage: https://culturedcode.com/things
user-invocable: true
command-dispatch: tool
command-tool: things-mac.run
command-arg-mode: raw
metadata:
  openclaw:
    skillKey: prod.things-mac
    emoji: "\U00002728"
    requires:
      bins:
        - osascript
    os:
      - darwin
    always: false
---
# Things for Mac

Manage todos, projects, and areas in Things 3 using AppleScript and URL schemes.

## Capabilities

- Create and complete todos
- Manage projects and areas
- Set deadlines and reminders
- Add tags to todos and projects
- Create checklists within todos
- Move items between projects and areas
- Search and list todos by various criteria

## Platform

macOS only. Requires Things 3 to be installed. Uses AppleScript and `things:///` URL schemes.

## Usage

### Creating Todos

```bash
# Create a simple todo
osascript -e 'tell application "Things3" to make new to do with properties {name:"Buy groceries"}'

# Create a todo with deadline
osascript -e 'tell application "Things3" to make new to do with properties {name:"Submit report", due date:date "2024-12-31"}'

# Create a todo in a specific project
osascript -e 'tell application "Things3"
  set proj to project "Work"
  make new to do at proj with properties {name:"Review PR", tag names:"code"}
end tell'

# Create via URL scheme (with checklist)
open "things:///add?title=Pack%20for%20trip&checklist-items=Passport%0ACharger%0AClothes&deadline=2024-12-20"
```

### Completing Todos

```bash
# Complete a todo by name
osascript -e 'tell application "Things3" to set status of to do "Buy groceries" to completed'
```

### Listing Todos

```bash
# List all todos in Inbox
osascript -e 'tell application "Things3" to get name of every to do in list "Inbox"'

# List todos in Today
osascript -e 'tell application "Things3" to get name of every to do in list "Today"'

# List all projects
osascript -e 'tell application "Things3" to get name of every project'

# List all areas
osascript -e 'tell application "Things3" to get name of every area'
```

### Managing Tags

```bash
# Create a tagged todo
osascript -e 'tell application "Things3" to make new to do with properties {name:"Urgent task", tag names:"urgent,high-priority"}'
```

## Examples

- `osascript -e 'tell application "Things3" to make new to do with properties {name:"Task"}'`
- `osascript -e 'tell application "Things3" to get name of every to do in list "Today"'`
- `osascript -e 'tell application "Things3" to get name of every project'`
- `open "things:///add?title=New%20Task&when=today&tags=work"`

## Error Handling

- If Things 3 is not installed, inform the user and suggest installation from the Mac App Store.
- The app name for AppleScript is "Things3" (no space).
- URL scheme parameters must be URL-encoded.

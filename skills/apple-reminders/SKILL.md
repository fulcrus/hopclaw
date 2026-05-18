---
name: apple-reminders
description: Create, complete, and manage reminders in Apple Reminders via osascript
homepage: https://support.apple.com/guide/reminders
user-invocable: true
command-dispatch: tool
command-tool: apple-reminders.run
command-arg-mode: raw
metadata:
  openclaw:
    skillKey: prod.apple-reminders
    emoji: "\U00002705"
    requires:
      bins:
        - osascript
    os:
      - darwin
    always: false
---
# Apple Reminders

Create, complete, and manage reminders in Apple Reminders using AppleScript.

## Capabilities

- Create new reminders with title, notes, and due dates
- Complete and uncomplete reminders
- Delete reminders
- List reminders by list
- Set due dates and times
- List all reminder lists
- Move reminders between lists

## Platform

macOS only. Uses `osascript` to interact with the Reminders application via AppleScript.

## Usage

Use AppleScript commands via `osascript` to manage Apple Reminders.

### Creating Reminders

```bash
# Simple reminder
osascript -e 'tell application "Reminders" to make new reminder in list "Reminders" with properties {name:"Buy groceries"}'

# Reminder with due date
osascript -e 'tell application "Reminders" to make new reminder in list "Reminders" with properties {name:"Submit report", due date:date "2024-12-31 17:00:00"}'

# Reminder with notes
osascript -e 'tell application "Reminders" to make new reminder in list "Reminders" with properties {name:"Call dentist", body:"Schedule annual checkup"}'
```

### Listing Reminders

```bash
# List all reminder lists
osascript -e 'tell application "Reminders" to get name of every list'

# List incomplete reminders in a list
osascript -e 'tell application "Reminders" to get name of every reminder in list "Reminders" whose completed is false'
```

### Completing Reminders

```bash
# Mark a reminder as completed
osascript -e 'tell application "Reminders" to set completed of reminder "Buy groceries" in list "Reminders" to true'
```

### Deleting Reminders

```bash
# Delete a reminder
osascript -e 'tell application "Reminders" to delete reminder "Buy groceries" in list "Reminders"'
```

## Examples

- `osascript -e 'tell application "Reminders" to get name of every list'`
- `osascript -e 'tell application "Reminders" to make new reminder in list "Reminders" with properties {name:"Task"}'`
- `osascript -e 'tell application "Reminders" to get name of every reminder in list "Reminders" whose completed is false'`

## Error Handling

- If Reminders is not available, inform the user that this skill requires macOS.
- Date parsing can be tricky with AppleScript. Use ISO-style dates when possible.
- If a list does not exist, suggest creating it first or using the default "Reminders" list.

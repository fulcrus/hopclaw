---
name: imsg
description: Send and read iMessages on macOS via osascript
user-invocable: true
command-dispatch: tool
command-tool: imsg.run
command-arg-mode: raw
metadata:
  openclaw:
    skillKey: comm.imsg
    emoji: "\U0001F4AC"
    requires:
      anyBins:
        - osascript
    os:
      - darwin
    always: false
---
# iMessage

Send and read iMessages on macOS using AppleScript via osascript.

## Capabilities

- Send iMessages to contacts by phone number or email
- Read recent messages from conversations
- Search conversations by contact
- List recent conversations

## Platform

macOS only. Uses `osascript` to interact with the Messages application via AppleScript.

## Usage

### Sending Messages

```bash
# Send a message to a phone number
osascript -e 'tell application "Messages"
  set targetService to 1st account whose service type = iMessage
  set targetBuddy to participant "+1234567890" of targetService
  send "Hello from HopClaw!" to targetBuddy
end tell'

# Send a message to an email address
osascript -e 'tell application "Messages"
  set targetService to 1st account whose service type = iMessage
  set targetBuddy to participant "user@example.com" of targetService
  send "Hello!" to targetBuddy
end tell'
```

### Reading Messages

```bash
# Get recent chats
osascript -e 'tell application "Messages" to get name of every chat'

# Get messages from a specific chat
osascript -e 'tell application "Messages"
  set targetChat to chat 1
  get text of every message of targetChat
end tell'
```

## Examples

- `osascript -e 'tell application "Messages" to get name of every chat'`
- `osascript -e 'tell application "Messages" to send "Hello" to participant "+1234567890" of (1st account whose service type = iMessage)'`

## Error Handling

- If Messages is not available or iMessage is not signed in, inform the user.
- Phone numbers should include country code (e.g., +1 for US).
- Some operations require Messages to have existing conversations with the contact.

## Security

- Always confirm the recipient and message content before sending.
- Never send messages without explicit user approval.
- Be cautious with phone numbers and email addresses in output.

---
name: himalaya
description: Read, send, and manage email from the terminal via the himalaya CLI
homepage: https://github.com/sostrovsky/himalaya
user-invocable: true
command-dispatch: tool
command-tool: himalaya.run
command-arg-mode: raw
metadata:
  openclaw:
    skillKey: comm.himalaya
    emoji: "\U0001F4E7"
    requires:
      bins:
        - himalaya
    always: false
---
# Himalaya

Read, send, and manage email from the terminal using the himalaya CLI.

## Capabilities

- Read and list emails in any folder
- Send emails with subject, body, and attachments
- Reply to and forward emails
- Manage folders (list, create, delete)
- Search messages by keyword, sender, or date
- Move and copy messages between folders
- Mark messages as read/unread/flagged

## Configuration

Himalaya uses a configuration file at `~/.config/himalaya/config.toml` or `$XDG_CONFIG_HOME/himalaya/config.toml`. No environment variable is required if the config file is present.

## Usage

### Reading Email

```bash
# List emails in inbox
himalaya list

# List emails in a specific folder
himalaya list -f "Sent"

# Read a specific email by ID
himalaya read 42

# List with page size
himalaya list -s 20
```

### Sending Email

```bash
# Send an email (opens editor or reads from stdin)
himalaya write --to "user@example.com" --subject "Hello" --body "Message body"

# Reply to a message
himalaya reply 42

# Forward a message
himalaya forward 42
```

### Searching

```bash
# Search by keyword
himalaya search "project update"

# Search by sender
himalaya search "from:boss@company.com"
```

### Managing Folders

```bash
# List all folders
himalaya folders

# Move a message to a folder
himalaya move 42 "Archive"

# Copy a message to a folder
himalaya copy 42 "Important"
```

### Flags

```bash
# Mark as read
himalaya flag set 42 seen

# Mark as flagged
himalaya flag set 42 flagged

# Mark as unread
himalaya flag remove 42 seen
```

## Examples

- `himalaya list`
- `himalaya read 42`
- `himalaya write --to "user@example.com" --subject "Hello"`
- `himalaya search "meeting"`
- `himalaya folders`

## Error Handling

- If himalaya is not configured, guide the user to create a config file.
- If authentication fails, suggest checking credentials in the config.
- For IMAP connection errors, verify server hostname and port settings.

## Security

- Never display full email content unless the user explicitly requests it.
- Summarize email bodies rather than printing them in full.
- Do not expose email addresses or credentials in output.

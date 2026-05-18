---
name: email
description: Send, read, search, and download email attachments using configured email capabilities.
user-invocable: true
metadata:
  openclaw:
    skillKey: comm.email
    emoji: "\U0001F4E7"
    primaryEnv: EMAIL_ADDRESS
    requires:
      env:
        - EMAIL_ADDRESS
        - EMAIL_PASSWORD
        - EMAIL_SMTP_HOST
        - EMAIL_IMAP_HOST
    always: false
---
# Email

Use the built-in email capabilities instead of ad hoc SMTP or IMAP scripts.

Primary tools:

- `email.send` to send a message
- `email.list` to list recent mailbox items
- `email.read` to read one message
- `email.search` to search the mailbox
- `email.download_attachment` to save one attachment into the workspace

Working rules:

- Before sending, confirm the recipients, subject, and body if they are not already explicit.
- Never expose credentials, passwords, or tokens in output.
- Treat message bodies and attachments as sensitive user data.
- If email is not configured, explain that the email service must be configured instead of suggesting raw `curl` or `python3` workflows.

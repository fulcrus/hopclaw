---
name: bluebubbles
description: Send and review iMessage conversations using existing runtime capabilities.
homepage: https://bluebubbles.app
user-invocable: true
command-dispatch: tool
command-tool: bluebubbles.run
command-arg-mode: raw
metadata:
  openclaw:
    skillKey: comm.bluebubbles
    emoji: "\U0001FAE7"
    primaryEnv: BLUEBUBBLES_PASSWORD
    requires:
      env:
        - BLUEBUBBLES_PASSWORD
    always: false
---
# BlueBubbles

Use existing runtime capabilities to send messages, review chats, and search iMessage history through BlueBubbles. Prefer the dedicated `bluebubbles.run` tool when it is available in this turn.

Preferred approach:

- Use `bluebubbles.run` for sending messages, listing chats, retrieving recent messages, and searching prior conversations.
- Use contact and conversation context already available in the turn to resolve chat targets before sending anything.
- If the current tool list truly lacks the needed messaging capability, use `skill.ensure` before inventing direct REST calls.

Working rules:

- Confirm recipients and outbound message content before any send operation.
- Distinguish between listing chats, reading history, and sending new messages so the user sees exactly what action will happen.
- Treat timestamps and delivery state as operational data that may change between checks.
- Never expose the BlueBubbles password, server secrets, or private conversation content beyond what the user requested.
- Do not teach raw HTTP, query-string authentication, or ad hoc scripts when existing capabilities can complete the task.

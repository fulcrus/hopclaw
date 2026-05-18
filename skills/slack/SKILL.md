---
name: slack
description: Send messages, inspect channels, and work with Slack using existing runtime capabilities.
homepage: https://api.slack.com
user-invocable: true
command-dispatch: tool
command-tool: slack.send
command-arg-mode: raw
metadata:
  openclaw:
    skillKey: comm.slack
    emoji: "\U0001F4AC"
    primaryEnv: SLACK_TOKEN
    requires:
      env:
        - SLACK_TOKEN
    always: false
---
# Slack

Use existing runtime capabilities to send messages, inspect channels, and work with Slack conversation state. Prefer the dedicated `slack.send` tool and any related Slack capabilities already available in this turn.

Preferred approach:

- Use `slack.send` for outbound messages when the user wants to notify a channel, reply in a thread, or DM someone.
- Use existing runtime capabilities already available in the turn for channel lookup, history search, member discovery, reactions, or file-related Slack actions.
- Reuse workspace, channel, thread, and recipient context already present in the conversation instead of guessing identifiers.
- If the current tool list truly lacks the needed Slack capability, use `skill.ensure` before inventing raw API calls.

Working rules:

- Confirm the workspace, channel or user, thread target, and final message content before posting anything outbound.
- Distinguish clearly between read-only Slack work and actions that will notify people or mutate workspace state.
- Treat channel history, member lists, private threads, and attachments as sensitive workspace data.
- Never expose `SLACK_TOKEN`, app secrets, or internal workspace identifiers in output.
- Do not teach raw HTTP requests, token headers, or ad hoc scripts when existing capabilities can complete the task.

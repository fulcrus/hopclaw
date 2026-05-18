---
name: eightctl
description: Control Eight Sleep devices and review sleep data using existing runtime capabilities.
homepage: https://www.eightsleep.com
user-invocable: true
command-dispatch: tool
command-tool: eightctl.run
command-arg-mode: raw
metadata:
  openclaw:
    skillKey: iot.eightctl
    emoji: "\U0001F6CF\uFE0F"
    primaryEnv: EIGHT_SLEEP_EMAIL
    requires:
      env:
        - EIGHT_SLEEP_EMAIL
        - EIGHT_SLEEP_PASSWORD
    always: false
---
# Eight Sleep Control

Use existing runtime capabilities to control Eight Sleep pod settings and inspect sleep data. Prefer the dedicated `eightctl.run` tool when it is available in this turn.

Preferred approach:

- Use `eightctl.run` for authentication-backed temperature control, pod status retrieval, alarm settings, and sleep trend lookups.
- Use current user and device context already available in the turn to identify the right side of the bed, device, or date range.
- If the current tool list truly lacks the needed Eight Sleep capability, use `skill.ensure` before attempting custom network workflows.

Working rules:

- Confirm temperature or alarm changes before executing them because they directly affect physical device behavior.
- Treat sleep metrics and pod state as time-sensitive snapshots and include dates or timestamps when reporting them.
- Be conservative with health-adjacent interpretation: report recorded values clearly, but do not overstate medical meaning.
- Never expose account secrets, session tokens, or personal sleep data beyond what the user requested.
- Do not teach raw login flows, bearer-token extraction, or ad hoc scripts when existing capabilities can complete the task.

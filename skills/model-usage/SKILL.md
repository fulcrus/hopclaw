---
name: model-usage
description: Inspect token, cost, and call-activity summaries from the local HopClaw gateway.
user-invocable: true
metadata:
  openclaw:
    skillKey: ops.model-usage
    emoji: "\U0001F4CA"
    always: false
---
# Model Usage

Use the local HopClaw gateway or operator usage surface to answer questions about:

- token usage
- cost estimates
- per-model breakdowns
- recent call activity
- session or time-window summaries

Working rules:

- Prefer an existing operator or gateway usage surface if it is already exposed in this runtime.
- If you only have generic HTTP access, query the configured local gateway address rather than hardcoding `localhost:8080`.
- Default to a clear summary that states the time window, totals, and per-model breakdown.
- When filters are missing, choose a sensible default such as the current day or current session and state that choice.
- Do not teach the user to use `curl`, `wget`, or ad hoc scripts to inspect their own gateway when an existing capability can do it.

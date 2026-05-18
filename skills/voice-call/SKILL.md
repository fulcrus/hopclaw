---
name: voice-call
description: Place and inspect Twilio voice calls using existing runtime capabilities.
homepage: https://www.twilio.com/docs/voice
user-invocable: true
command-dispatch: tool
command-tool: voice-call.run
command-arg-mode: raw
metadata:
  openclaw:
    skillKey: comm.voice-call
    emoji: "\U0001F4DE"
    primaryEnv: TWILIO_ACCOUNT_SID
    requires:
      env:
        - TWILIO_ACCOUNT_SID
        - TWILIO_AUTH_TOKEN
        - TWILIO_PHONE_NUMBER
    always: false
---
# Voice Call

Use existing runtime capabilities to place or inspect Twilio voice calls. Prefer the dedicated `voice-call.run` tool when it is available in this turn.

Preferred approach:

- Use `voice-call.run` for outbound calls, status lookup, and TwiML-based call flows.
- Use supporting text capabilities already available in the turn to prepare short spoken scripts or call summaries.
- If the current tool list truly lacks the needed capability, use `skill.ensure` to recover it instead of hand-writing raw HTTP requests.

Working rules:

- Confirm the recipient number, caller ID, spoken content or TwiML, and any cost-sensitive action before placing a call.
- Normalize phone numbers to E.164 when the tool expects canonical values.
- Treat call status as point-in-time operational data and report timestamps when relevant.
- Never expose Twilio secrets, account identifiers, or signed request material.
- Do not teach `curl` or ad hoc scripts when existing capabilities can complete the task.

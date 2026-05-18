---
name: openhue
description: Control Philips Hue lights, rooms, and scenes using existing runtime capabilities.
homepage: https://developers.meethue.com
user-invocable: true
command-dispatch: tool
command-tool: openhue.run
command-arg-mode: raw
metadata:
  openclaw:
    skillKey: iot.openhue
    emoji: "\U0001F4A1"
    primaryEnv: HUE_APPLICATION_KEY
    requires:
      env:
        - HUE_APPLICATION_KEY
        - HUE_BRIDGE_IP
    always: false
---
# OpenHue

Use existing runtime capabilities to control Philips Hue lights, rooms, and scenes. Prefer the dedicated `openhue.run` tool when it is available in this turn.

Preferred approach:

- Use `openhue.run` for light discovery, on/off control, brightness, color, color temperature, and scene activation.
- Use current device or home-automation context already available in the turn to resolve room names, light labels, or scene names before sending commands.
- If the current tool list truly lacks the needed Hue capability, use `skill.ensure` before reaching for generic network workflows.

Working rules:

- Confirm the target bridge, room, light, or scene whenever the request could affect the wrong device.
- Treat light state as live operational data and report timestamps or refresh points when the user asks about current status.
- Preserve safe defaults for destructive or surprising actions such as turning off rooms, changing many lights at once, or overwriting a scene.
- Never expose Hue bridge secrets, local network details, or raw authentication material in output.
- Do not teach raw HTTP, `curl -k`, or ad hoc scripts when existing capabilities can complete the task.

# Examples

This folder contains copyable reference templates for the most practical
community extension paths in HopClaw.

Start here when you want a minimal working skeleton instead of a long design
document.

## Templates Included

- [`skill-template/`](./skill-template): a `SKILL.md`-based capability template
  for prompt-driven or local-command-assisted skills
- [`webhook-template/`](./webhook-template): a simple webhook integration
  template showing both inbound events to HopClaw and outbound callbacks from
  HopClaw
- [`stdio-channel-template/`](./stdio-channel-template): a JSON-RPC over
  stdio channel plugin template that can be written in any language
- [`hook-template/`](./hook-template): event-driven command and HTTP hook
  templates for alerts, audit relays, and lightweight policy checks
- [`enterprise-bridge-template/`](./enterprise-bridge-template): a minimal
  outer-system bridge for external AuthZ and audit export without patching core
- [`external-http-tool-template/`](./external-http-tool-template): an HTTP tool
  server template for wrapping a service as a HopClaw tool
- [`capability-host-template/`](./capability-host-template): a reference
  template for host-style action routing similar to browser and desktop helpers

## Which One Should I Copy?

- Copy `skill-template` when the agent already has the right builtin tools and
  you mostly need reusable instructions, guardrails, and local helper scripts.
- Copy `webhook-template` when your external system can send and receive HTTP
  requests and you want the fastest path to integration.
- Copy `stdio-channel-template` when you need a process-isolated messaging
  bridge in Python, Node, Rust, or another language.
- Copy `hook-template` when you want to react to runtime lifecycle events
  without changing the core runtime.
- Copy `enterprise-bridge-template` when you want HopClaw to stay business
  neutral while your policy or audit systems live outside the core binary.
- Copy `external-http-tool-template` when you want to expose a service as a
  tool without compiling anything into the core binary.
- Copy `capability-host-template` only if you are shaping a host-backed service
  and are comfortable wiring a Go-side adapter. It is a reference pattern, not
  a drop-in stable extension protocol yet.

## Related Docs

- [`../docs/development/plugin-sdk.md`](../docs/development/plugin-sdk.md)
- [`../VERSIONING.md`](../VERSIONING.md)

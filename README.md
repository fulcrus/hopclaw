# HopClaw

[简体中文](./README.zh-CN.md)

`hopclaw` is a local-first agent runtime with an operator console, channel
bridges, and an HTTP control API. Shipped in this tree today: agent loop,
run/session store and queue, tool execution, approvals, artifacts, audit and
event bus, ~23 channel adapters, skill loader, capability extension points
(plugin, MCP, bundle), automation surfaces (watch, cron, wakeup, knowledge
sources), and control-plane / governance-delivery / quality / eval-suite
surfaces that back the gateway console.

## Who HopClaw Fits Today

HopClaw is aimed at people and teams that need agents to survive real
operations:

- individual developers, researchers, and power users who want one local runtime for browser, desktop, files, commands, and inspectable results
- team-facing agents running through Slack, Feishu, Discord, Telegram, or webhook ingress that need approvals and audit
- internal services or schedulers that want to drive runs over HTTP instead of treating chat as the only control plane
- local operators who need browser and desktop automation without pushing host-specific logic into the core runtime
- users migrating existing `SKILL.md` or `.openclaw` assets into a stricter runtime surface

## Runtime Concepts

- `session_key` is the primary isolation key for conversations, workflows, and product entry points.
- `automation_id` is an optional label for an automation or workflow family.
- HopClaw is a single-instance runtime. There is no built-in multi-tenancy, organization, or role model. If you need those, wrap HopClaw from an outer system over the HTTP API and AuthZ decider contract.

## What HopClaw Ships Today

The tree currently contains:

- session and run stores with queue coordination
- context compaction with a sliding-window strategy
- OpenAI-compatible model integration with a model router
- built-in file, exec, net, text, and runtime tools
- built-in office file tools for documents, spreadsheets, presentations, ICS calendars, and CalDAV calendars
- Layer 2 tool groups for git, packages, containers, search, speech, media, media generation, vision, and email send
- approval flow with grant store and timeout sweeper for tool calls that require user confirmation
- artifact storage plus audit and event streams
- governance delivery adapters with a reliable dispatcher
- quality summary, release readiness, and eval-suite execution over HTTP and CLI
- release-channel aware update checks plus local bug-report bundles
- `SKILL.md` loading, binding, and skill discovery from local or remote catalogs
- generic skill recovery via `skill.ensure`, with approval-gated or automatic installation based on runtime policy
- plugin system that projects process-backed modules into the runtime catalog
- MCP client, server, bridge, and manager for extending tools via the Model Context Protocol
- capability bundles (currently `bundles/feishu-suite`)
- automation surfaces: `watch` sources, `cron` schedules, `wakeup` timers, automation-intent classification, durable facts, and knowledge sources
- HTTP endpoints for runs, approvals, artifacts, tools, events, quality, evals, governance delivery, knowledge, watch, cron, wakeup, automation, and the module catalog
- gateway operator console backed by the same HTTP surface, with English / 简体中文 / 繁體中文 / 日本語 catalogs
- capability registration plus host-backed `browser.*` tools through a managed or external browser helper
- host-backed `desktop.*` tools through a managed or external desktop helper
- device pairing and auth for external helpers

## Channels And Productivity Surface

HopClaw classifies built-in surfaces as:

- `supported`: part of the main product contract, with catalog visibility and primary-path docs
- `experimental`: implemented in-repo, with thinner onboarding depth, test depth, and operator guidance

### Channels

- supported messaging channels: Feishu, Slack, Discord, Telegram, WhatsApp, Signal, LINE, Microsoft Teams, IRC, Matrix, Mattermost, Nextcloud Talk, webhook
- supported personal or special channels: BlueBubbles, iMessage (legacy bridge), Nostr, Tlon, Twitch, Zalo Personal
- experimental channels: Google Chat, Synology Chat, Zalo
- local and integration surfaces: WebChat, stdio/plugin channels, and webhook-based bridges
- common channel operations: `channel.list`, `channel.status`, `channel.send`, `channel.edit`, `channel.delete`, `channel.react`, `channel.history`, `channel.action`

### Office and productivity

- office file tools: `document.*`, `spreadsheet.*`, `presentation.*`
- calendar tools: `calendar.create_ics`, `calendar.parse_ics`, `calendar.list_events`, `calendar.create_event`, `calendar.update_event`, `calendar.delete_event`
- email tools: `email.send`, `email.list`, `email.read`, `email.search`, `email.download_attachment`
- Feishu/Lark product bundle: docs, wiki, drive, bitable, and URL resolution through `bundles/feishu-suite`
- repo-local productivity skills: Notion, Jira, Trello, Apple Notes, Apple Reminders, Things, Bear Notes, Slack, email, and other workflow helpers in `skills/`

Some of these surfaces are built-in runtime tools, while others are skill or bundle integrations that depend on external credentials, CLIs, or service configuration.

## Requirements

- Go `1.26.1`

The module is pinned to the latest stable Go release:

```text
go 1.26.1
```

## Install

Tagged releases publish checksummed bundles for Linux, macOS, and Windows on `amd64` and `arm64`. Each archive contains the CLI and helper binaries for that platform plus `README`, `CHANGELOG`, `SECURITY`, `LICENSE`, and `NOTICE`.

Install the latest tagged release with the one-line installer:

```sh
curl -fsSL https://hopclaw.com/install.sh | sh
```

The installer uses `/usr/local/bin` when it is already writable. Otherwise it falls back to `~/.local/bin` so a normal first-time install does not require `sudo`.

On Windows PowerShell, install the latest tagged release with:

```powershell
irm https://hopclaw.com/install.ps1 | iex
```

For the fastest web-first path on macOS or Linux, install and jump straight into the local dashboard bootstrap:

```sh
curl -fsSL https://hopclaw.com/install.sh | HOPCLAW_INSTALL_RUN_ONBOARD=1 sh
```

This runs `hopclaw onboard --web-first`: it writes a minimal local config, starts the gateway, opens the dashboard, and leaves models/channels for the web console.

On Windows PowerShell, the equivalent web-first path is:

```powershell
$env:HOPCLAW_INSTALL_RUN_ONBOARD='1'; irm https://hopclaw.com/install.ps1 | iex
```

Useful installer overrides:

- `HOPCLAW_INSTALL_BINARY=all` installs the CLI, `openclaw` compatibility alias, and helper binaries from the same release bundle
- `HOPCLAW_INSTALL_BINARY=hopclaw-browserd` installs a helper binary instead of the main CLI
- `HOPCLAW_INSTALL_VERSION=2026.3.17` installs a specific tagged release
- `HOPCLAW_INSTALL_DIR=...` changes the destination directory
- `HOPCLAW_INSTALL_REPO=owner/repo` overrides the GitHub release source when mirroring builds
- `HOPCLAW_INSTALL_BASE_URL=https://mirror.example.com/releases` points the installer at a mirrored hosted release surface
- `HOPCLAW_INSTALL_RUN_ONBOARD=1` launches `hopclaw onboard --web-first` after the install completes

If you prefer building from source, install the native HopClaw command:

```sh
go install github.com/fulcrus/hopclaw/cmd/hopclaw@latest
```

Install the local compatibility alias if you still want to run it as `openclaw`:

```sh
go install github.com/fulcrus/hopclaw/cmd/openclaw@latest
```

Install the optional standalone browser helper:

```sh
go install github.com/fulcrus/hopclaw/cmd/hopclaw-browserd@latest
```

Install the optional standalone desktop helper:

```sh
go install github.com/fulcrus/hopclaw/cmd/hopclaw-desktopd@latest
```

Container images for tagged releases are published to GHCR.

Homebrew users can bootstrap from the in-repo `HEAD` formula:

```sh
brew tap fulcrus/hopclaw https://github.com/fulcrus/hopclaw
brew install --HEAD fulcrus/hopclaw
```

A dedicated bottle-backed tap can be layered on top of the same release artifacts later without changing the install surface.

## Quick Start

Fastest path on macOS or Linux:

```sh
curl -fsSL https://hopclaw.com/install.sh | HOPCLAW_INSTALL_RUN_ONBOARD=1 sh
```

That installs the latest release and opens the guided `hopclaw onboard` wizard.

Fastest path on Windows PowerShell:

```powershell
$env:HOPCLAW_INSTALL_RUN_ONBOARD='1'; irm https://hopclaw.com/install.ps1 | iex
```

That installs the latest release, adds the user-level install directory to `PATH`, and opens the same guided `hopclaw onboard` wizard.

If you prefer the manual path:

1. Copy the example config.

```sh
cp config.example.yaml local.yaml
```

2. Set the required environment variables.

```sh
export HOPCLAW_AUTH_TOKEN=change-me
export OPENAI_BASE_URL=https://api.openai.com/v1
export OPENAI_API_KEY=your-api-key
export OPENAI_MODEL=gpt-4.1-mini
```

You can also let HopClaw auto-detect other supported providers such as
`DEEPSEEK_API_KEY`, `DASHSCOPE_API_KEY`, `MOONSHOT_API_KEY`, `MINIMAX_API_KEY`,
`XIAOMI_API_KEY`, `QIANFAN_API_KEY`, `ZAI_API_KEY`, `VOLCENGINE_API_KEY`,
`HUNYUAN_API_KEY`, and `SILICONFLOW_API_KEY` via `hopclaw setup` or `hopclaw onboard`.

3. Start the gateway.

```sh
hopclaw serve --config ./local.yaml
```

4. Verify the process is up and inspect the current runtime surface.

```sh
curl http://127.0.0.1:16280/healthz
curl -H "Authorization: Bearer $HOPCLAW_AUTH_TOKEN" http://127.0.0.1:16280/runtime/tools
```

5. Open the local operator console.

```sh
hopclaw dashboard
```

Recommended runtime settings for local desktop usage:

```yaml
runtime:
  profile: desktop
  status_reminder_delay: 6s
  audit:
    enabled: true
skills:
  install_policy: ask
```

Recommended release-management settings:

```yaml
update:
  enabled: true
  check_on_start: true
  channel: stable
  manifest_url: https://hopclaw.com/releases/manifest.json
diagnostics:
  enabled: true
  bug_report_dir: ./.hopclaw/reports
  include_logs: true
  telemetry_enabled: false
  # telemetry_endpoint: https://telemetry.example.com/api/v1/ingest/events
  # telemetry_token: env:HOPCLAW_TELEMETRY_TOKEN
  crash_reports_enabled: false
```

Telemetry guidance:

- keep `telemetry_enabled: false` for strict enterprise deployments unless you explicitly want outbound product analytics
- if you enable outbound telemetry, point `telemetry_endpoint` at a collector you operate or explicitly trust
- use `telemetry_endpoint` plus `telemetry_token` to send anonymous install / active / plugin / skill events
- use `telemetry_collector_enabled` on a gateway when you want local collection inside your own boundary
- telemetry is best-effort and silent by default; only `diagnostics.telemetry_debug_log: true` emits debug failure logs
- treat telemetry as install and active-install metrics, not exact user counts

Skill installation policy:

- `ask`: when the agent calls `skill.ensure` or `skill.install`, HopClaw creates an approval and resumes after confirmation
- `auto`: skill installation proceeds automatically and the run continues without a pause
- `deny`: runtime skill installation is blocked; the agent must explain the missing capability instead

Bug reports:

- `hopclaw bug-report` writes a local redacted ZIP bundle for manual issue filing
- `hopclaw update --check` shows the latest visible release on the configured channel
- `hopclaw doctor` surfaces the last recorded update state

Feedback and support:

- use `hopclaw doctor` first when install, config, or startup behavior looks wrong
- attach the `hopclaw bug-report` bundle plus exact version, platform, and repro steps when filing a bug
- use the GitHub issue tracker for bugs, documentation gaps, and feature requests
- use [`SECURITY.md`](./SECURITY.md) instead of public issues for vulnerabilities

## Minimal API Workflow

List the tools visible to the runtime:

```sh
curl -s \
  -H "Authorization: Bearer $HOPCLAW_AUTH_TOKEN" \
  http://127.0.0.1:16280/runtime/tools
```

Create and enqueue a run:

```sh
curl -s \
  -X POST \
  -H "Authorization: Bearer $HOPCLAW_AUTH_TOKEN" \
  -H "Content-Type: application/json" \
  http://127.0.0.1:16280/runtime/runs \
  -d '{
    "session_key": "demo-session",
    "content": "List the files in the current workspace and summarize the project structure."
  }'
```

Fetch the run status:

```sh
curl -s \
  -H "Authorization: Bearer $HOPCLAW_AUTH_TOKEN" \
  http://127.0.0.1:16280/runtime/runs/<run-id>
```

If a run stops for approval, inspect pending tickets:

```sh
curl -s \
  -H "Authorization: Bearer $HOPCLAW_AUTH_TOKEN" \
  "http://127.0.0.1:16280/runtime/approvals?status=pending"
```

Resolve an approval:

```sh
curl -s \
  -X POST \
  -H "Authorization: Bearer $HOPCLAW_AUTH_TOKEN" \
  -H "Content-Type: application/json" \
  http://127.0.0.1:16280/runtime/approvals/<approval-id>/resolve \
  -d '{
    "status": "approved",
    "resolvedBy": "operator",
    "note": "approved"
  }'
```

## Runtime API Semantics

- `POST /runtime/runs` is asynchronous by default. With `execute=true` or no explicit value, the server enqueues background execution and returns `202 Accepted`.
- Use `execute=false` if you only want to create the run record without executing it; that path returns `201 Created`.
- `POST /runtime/runs/{id}/resume` enqueues background resume work and returns `202 Accepted`.
- Authentication can use either `Authorization: Bearer <token>` or `X-HopClaw-Token: <token>`.

## Runtime Profiles

- `runtime.profile: desktop` is the default. It keeps the local-first behavior and still requires approval for write-like tools.
- `runtime.profile: trusted_desktop` reduces approval friction on trusted personal machines, but it still does not bypass destructive-tool safeguards.
- `runtime.profile: production` tightens defaults. It changes the default exec capability mode to `allowlist`, defaults `net.allow_local` to `false`, denies destructive tools by policy, and requires `server.auth_token`, `runtime.audit.enabled: true`, and a durable store backend.
- `runtime.status_reminder_delay` controls when channel bridges send a “still processing” update if a run takes longer than expected.

## Web Surface

- `GET /` serves a bilingual local operator console.
- The console shows runtime status, capabilities, runs, approvals, sessions, and recent events.
- The full operator surface is also reachable through the HTTP APIs documented under [`docs/openapi/runtime-v1.yaml`](./docs/openapi/runtime-v1.yaml).

## Capability Hosts

- HopClaw can register capability hosts through the gateway.
- If you set `hosts.browser.enabled: true` without `base_url`, HopClaw now starts a local browser helper on demand and stops it again when idle.
- If you set `hosts.desktop.enabled: true` without `base_url`, HopClaw does the same for the local desktop helper.
- If you set `base_url`, HopClaw uses that external helper instead.
- When configured, the browser host appears under `GET /operator/capabilities` and in the local operator console.
- When configured, the runtime also exposes `browser.create_session`, `browser.navigate`, `browser.click`, `browser.type`, `browser.wait_for`, `browser.snapshot`, `browser.screenshot`, and `browser.list_tabs`.
- When configured, the runtime also exposes `desktop.create_session`, `desktop.open_app`, `desktop.focus_app`, `desktop.focus_window`, `desktop.list_apps`, `desktop.list_windows`, `desktop.type_text`, `desktop.hotkey`, `desktop.screenshot`, `desktop.capture_tree`, `desktop.clipboard_read`, and `desktop.clipboard_write`.

Recommended local browser helper config:

```yaml
hosts:
  browser:
    enabled: true
```

Advanced external browser helper quick start:

```sh
export HOPCLAW_BROWSER_TOKEN=change-me-browser
hopclaw-browserd -listen 127.0.0.1:9223 -auth-token "$HOPCLAW_BROWSER_TOKEN"
```

Or use the simplified launcher:

```sh
hopclaw devices launch browserd --gateway-url http://127.0.0.1:16280 --pairing-code <code> --device-id <device-id>
```

Then point `local.yaml` at that external helper:

```yaml
hosts:
  browser:
    enabled: true
    base_url: http://127.0.0.1:9223
    auth_token: ${HOPCLAW_BROWSER_TOKEN}
```

Recommended local desktop helper config:

```yaml
hosts:
  desktop:
    enabled: true
```

Advanced external desktop helper quick start:

```sh
export HOPCLAW_DESKTOP_TOKEN=change-me-desktop
hopclaw-desktopd -listen 127.0.0.1:9224 -auth-token "$HOPCLAW_DESKTOP_TOKEN"
```

Or use the simplified launcher:

```sh
hopclaw devices launch desktopd --gateway-url http://127.0.0.1:16280 --pairing-code <code> --device-id <device-id>
```

Then point `local.yaml` at that external helper:

```yaml
hosts:
  desktop:
    enabled: true
    base_url: http://127.0.0.1:9224
    auth_token: ${HOPCLAW_DESKTOP_TOKEN}
```

## Security Defaults

- The server binds to `127.0.0.1:16280` by default.
- You can protect the runtime API with `server.auth_token`.
- Network tools default to `allow_private: false`.
- Do not expose this service to an untrusted network without a token and an external access-control layer.
- Security-sensitive reports should go through [`SECURITY.md`](./SECURITY.md), not the public issue tracker.

## Repository Layout

- [`agent/`](./agent): agent loop, stores, queue coordination
- [`approval/`](./approval): approval tickets, grants, and timeout sweeper
- [`artifact/`](./artifact): artifact storage and preview
- [`audit/`](./audit): audit sinks and reliable delivery
- [`authz/`](./authz): AuthZ decider contract
- [`bootstrap/`](./bootstrap): application assembly
- [`bundles/`](./bundles): capability bundles (currently `feishu-suite`)
- [`capabilities/`](./capabilities): concrete capability adapters
- [`capability/`](./capability): capability manifests, health, and registry
- [`channels/`](./channels): channel adapters and bridge
- [`cmd/`](./cmd): installable entrypoints (`hopclaw`, `hopclaw-browserd`, `hopclaw-desktopd`, `hopclaw-gateway`, `openclaw`)
- [`config/`](./config): YAML config, validation, and product catalog
- [`contextengine/`](./contextengine): prompt and context preparation
- [`controlplane/`](./controlplane): control-plane status probes and shared types
- [`cron/`](./cron) / [`watch/`](./watch) / [`wakeup/`](./wakeup): automation schedules and watchers
- [`eventbus/`](./eventbus): event bus and typed payloads
- [`gateway/`](./gateway): operator endpoints and local console
- [`internal/browserd/`](./internal/browserd) / [`internal/desktopd/`](./internal/desktopd): standalone host implementations
- [`knowledge/`](./knowledge): knowledge sources
- [`mcp/`](./mcp): MCP client, server, bridge, and manager
- [`plugin/`](./plugin): plugin loader, installer, and manager
- [`policy/`](./policy): policy engine and grant wiring
- [`runtime/`](./runtime): service layer over the agent component
- [`server/`](./server): runtime HTTP API
- [`skill/`](./skill): skill loading, binding, and registry
- [`store/`](./store): persistence backends
- [`toolruntime/`](./toolruntime): built-in tools and Layer 2 capability groups

## Documentation

- [`docs/`](./docs): installation, configuration, channel and provider integration, runbooks, troubleshooting, and reference material
- [`docs/openapi/runtime-v1.yaml`](./docs/openapi/runtime-v1.yaml): OpenAPI 3.1 spec for the Runtime API
- [`examples/`](./examples): copyable templates for skills, hooks, webhook integrations, stdio channel plugins, external HTTP tools, and capability hosts
- [`VERSIONING.md`](./VERSIONING.md): release channels, compatibility axes, and deprecation policy
- [`CHANGELOG.md`](./CHANGELOG.md): versioned feature, fix, and release-engineering notes
- [`SECURITY.md`](./SECURITY.md): supported-version policy and private vulnerability reporting process

## License

HopClaw is licensed under the MIT License. See [`LICENSE`](./LICENSE) and [`NOTICE`](./NOTICE).

If you redistribute a modified version, keep the license and notice files and
preserve attribution. Recommended wording includes `Based on HopClaw`,
`Forked from HopClaw`, or `Modified from HopClaw`.

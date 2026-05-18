---
sidebar_position: 2
title: Plugin Development
---

# Plugin Development

HopClaw plugins are directory-based extensions discovered from manifest files such as `hopclaw.plugin.yaml`.

## What a plugin can contribute

The current manifest supports:

- model providers
- channel adapters
- external HTTP tools
- skill directories
- hook assets
- MCP servers
- agent presets
- CLI command declarations

## 1. Create the plugin directory

```bash
mkdir -p ./extensions/acme-plugin/skills
```

Create the manifest:

```yaml title="./extensions/acme-plugin/hopclaw.plugin.yaml"
name: acme-plugin
version: 0.1.0
description: Add Acme provider, a lookup tool, and a reusable agent preset.
author: Your Team

providers:
  acme:
    api: openai_compat
    base_url: https://api.acme.example/v1
    api_key: ${ACME_API_KEY}
    default_model: acme-chat
    env_vars:
      - ACME_API_KEY

tools:
  - name: acme.lookup
    description: Query the Acme internal lookup service.
    endpoint: http://127.0.0.1:8088/tool

skills_dir: skills

mcp_servers:
  repo:
    description: Repository MCP server
    name: repo
    command: uvx
    args:
      - mcp-server-git

agents:
  support:
    description: Triage support requests before escalation.
    system_prompt: |
      You are the Acme support triage agent.
    model: acme-chat
    tools:
      - acme.lookup
```

## 2. Keep plugin-owned paths inside the plugin root

The loader validates that:

- `skills_dir`
- `skills_dirs`
- `hooks_dir`

do **not** escape the plugin root and are **not** absolute paths.

Good:

```yaml
skills_dir: skills
hooks_dir: hooks
```

Bad:

```yaml
skills_dir: ../../shared-skills
```

## 3. Add a custom provider

Providers are declared by ID under `providers`.

```yaml
providers:
  acme:
    api: openai_compat
    base_url: https://api.acme.example/v1
    api_key: ${ACME_API_KEY}
    default_model: acme-chat
```

This lets the plugin contribute a provider catalog entry that the runtime can discover.

## 4. Add a tool

Tools are outbound HTTP contracts:

```yaml
tools:
  - name: acme.lookup
    description: Query Acme metadata by ticket or customer id.
    endpoint: http://127.0.0.1:8088/tool
    timeout: 15s
```

## 5. Add skills

Point the plugin manifest at a relative skill folder:

```yaml
skills_dir: skills
```

Then place `SKILL.md` files below that directory.

## 6. Add a channel adapter

Plugin channels currently support `webhook` and `stdio`.

Webhook example:

```yaml
channels:
  outbound-alerts:
    type: webhook
    callback_url: https://hooks.example.com/hopclaw
    secret: ${HOPCLAW_ALERT_SECRET}
```

`stdio` example:

```yaml
channels:
  ops-bridge:
    type: stdio
    command: ./bin/ops-bridge
    args:
      - --stdio
    work_dir: .
    capabilities:
      - interactive
      - delivery
```

## 7. Discovery locations

The current loader scans these paths:

- `./.hopclaw/plugins`
- `./.hopclaw/extensions`
- `./.openclaw/plugins`
- `./.openclaw/extensions`
- `./extensions`
- `~/.hopclaw/plugins`
- `~/.hopclaw/extensions`

For local development, `./extensions` is the simplest choice.

## 8. Test the plugin

Start HopClaw and inspect the plugin in the dashboard:

```bash
hopclaw serve
hopclaw dashboard --open
```

Then use:

- **Settings → Plugins** to verify discovery
- **Settings → Skills** to confirm skill readiness
- **Settings → Models** if the plugin adds providers

You can also keep the manifest minimal at first, then grow it one capability at a time.

## 9. Publish and share

For a team workflow:

- keep the plugin in a dedicated repo or `extensions/` folder
- version the manifest
- document required env vars
- include example payloads for custom tools
- add at least one demo skill or agent preset

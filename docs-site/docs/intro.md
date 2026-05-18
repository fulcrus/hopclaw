---
slug: /
sidebar_position: 1
title: Welcome to HopClaw
---

# Welcome to HopClaw

HopClaw is a **governed agent runtime** for individuals and teams that want real automation, not toy demos. It combines:

- a local or server-hosted gateway
- a web dashboard at `/dashboard/`
- approval-aware tool execution
- multi-model provider support
- channels, plugins, skills, and helper processes

## Why teams pick HopClaw

- **Governed by design** — approvals, auth, audit, and policy are first-class surfaces
- **Operator-friendly** — one dashboard for chat, runs, approvals, governance, automation, and settings
- **Extensible** — add skills, plugin manifests, MCP servers, hooks, and channel integrations
- **Practical** — YAML config, Go binary, Dockerfile, and CLI-first workflows
- **Multilingual product surface** — dashboard and runtime catalogs in English, `zh-CN`, and `ja-JP`

## Five-minute path

```bash
go install github.com/fulcrus/hopclaw/cmd/hopclaw@latest
hopclaw onboard
hopclaw serve
hopclaw dashboard --open
hopclaw status
```

If you already know your provider and just want a quick local config:

```bash
export OPENAI_API_KEY=your-key
hopclaw setup
hopclaw serve
```

Then open:

```text
http://127.0.0.1:16280/dashboard/
```

## What you can do on day one

- Run your first operator task from the **Assistant** workspace
- Inspect artifacts, verification, and receipts in **Runs**
- Review human gates in **Approvals**
- Add channels, models, skills, and plugins in **Settings**
- Create schedules, watches, and hooks in **Automation**

## Next docs

- [Installation](./getting-started/installation)
- [Quick Start](./getting-started/quick-start)
- [Configuration](./getting-started/configuration)
- [Your First Agent](./guides/your-first-agent)
- [Plugin Development](./guides/plugin-development)
- [Channel Integration](./guides/channel-integration)
- [Web Dashboard](./guides/web-dashboard)

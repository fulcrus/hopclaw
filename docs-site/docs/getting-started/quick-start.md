---
sidebar_position: 2
title: Quick Start
---

# Quick Start

This is the fastest path from zero to a working dashboard and your first operator task.

## 1. Create a minimal config

Use the repository example as a base:

```yaml title="config.yaml"
server:
  address: 127.0.0.1:16280

store:
  backend: jsonl
  path: ./.hopclaw/state

agent:
  system_prompt: |
    You are HopClaw, a capable assistant that completes tasks reliably.
  default_model: gpt-4.1-mini
  max_tool_rounds: 8

runtime:
  profile: desktop

models:
  openai_compat:
    base_url: https://api.openai.com/v1
    api_key: ${OPENAI_API_KEY}
    model: gpt-4.1-mini
    timeout: 60s

tools:
  builtins:
    enabled: true
    root: .
    default_exec_timeout: 30s
    max_read_bytes: 262144
```

Export your provider key:

```bash
export OPENAI_API_KEY=your-key
```

## 2. Start the server

```bash
hopclaw --config ./config.yaml serve
```

If you prefer the default config discovery path instead:

```bash
mkdir -p ~/.hopclaw
cp ./config.yaml ~/.hopclaw/config.yaml
hopclaw serve
```

## 3. Open the dashboard

Open the local dashboard:

```bash
hopclaw dashboard --open
```

Or go directly to:

```text
http://127.0.0.1:16280/dashboard/
```

## 4. Send your first task

In **Assistant**, send a concrete operator request such as:

```text
Check the current runtime status, summarize what is configured, and tell me the next missing setup step.
```

Good first prompts:

```text
Inspect the latest failed run and summarize the root cause.
```

```text
Review pending approvals and explain which ones are safe to grant.
```

## 5. Inspect the run

After the response is generated:

- open **Runs** to inspect the execution record
- open **Approvals** if a tool action requires confirmation
- open **Knowledge** to review new artifacts or memory entries

From the CLI you can also confirm the gateway is healthy:

```bash
hopclaw status
```

## 6. Optional: finish setup from the dashboard

Use **Settings** for:

- **Models** — validate providers and test a model
- **Channels** — connect Slack, Feishu/Lark, Discord, Telegram, and others
- **Security** — review approval and sandbox controls
- **Infrastructure** — verify Browser / Desktop helpers and pairing records

## What “working” looks like

You are done when:

- `hopclaw status` returns a healthy gateway
- the dashboard opens successfully
- you can send a task in **Assistant**
- a run record appears in **Runs**

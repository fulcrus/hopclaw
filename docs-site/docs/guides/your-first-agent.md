---
sidebar_position: 1
title: Your First Agent
---

# Your First Agent

This guide builds a simple agent workflow with:

- one skill directory
- one model provider
- one approval-aware runtime

## 1. Create a skill directory

Create a local skill root:

```bash
mkdir -p ./.hopclaw/skills/release-notes
```

Add `SKILL.md`:

```markdown title="./.hopclaw/skills/release-notes/SKILL.md"
---
name: release-notes
description: Draft concise release notes from a changelog or diff
---

# Release Notes

Turn raw engineering changes into a short, operator-friendly release summary.

## When to use

- preparing weekly product updates
- summarizing shipped changes
- rewriting raw technical notes into publishable bullets

## Required input

- a changelog section, diff summary, or bullet list of changes
```

## 2. Point HopClaw at your skill root

```yaml title="config.yaml"
skills:
  include_catalog: true
  auto_detect: true
  install_policy: ask
  dirs:
    - ./.hopclaw/skills
```

## 3. Configure a model provider

```yaml title="config.yaml"
models:
  openai_compat:
    base_url: https://api.openai.com/v1
    api_key: ${OPENAI_API_KEY}
    model: gpt-4.1-mini
```

## 4. Set the operator posture

Use approval-aware execution for early testing:

```yaml title="config.yaml"
tools:
  capabilities:
    exec:
      mode: approve
      timeout: 30s
      max_output: 1048576
```

## 5. Run the gateway

```bash
hopclaw --config ./config.yaml serve
hopclaw dashboard --open
```

## 6. Test the agent in the Assistant view

Try a concrete task:

```text
Use the release-notes skill to turn the latest changelog into five concise bullets for product stakeholders.
```

Or test from the CLI after the gateway is running:

```bash
hopclaw status
hopclaw skills list
```

## 7. Inspect the result

In the dashboard:

- **Assistant** shows the conversational flow
- **Runs** shows the canonical execution record
- **Approvals** shows gated actions
- **Knowledge** shows generated artifacts and memory

## 8. Make it production-friendly

Before promoting the workflow:

- switch the runtime profile if needed
- connect the channel where requests should arrive
- add governance hooks or automation
- validate the provider in **Settings → Models**

## Minimal success checklist

- your skill appears under `hopclaw skills list`
- the dashboard can execute a task using the configured model
- approvals behave as expected
- a run receipt appears in **Runs**

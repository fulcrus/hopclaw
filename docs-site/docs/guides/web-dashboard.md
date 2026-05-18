---
sidebar_position: 4
title: Web Dashboard
---

# Web Dashboard

HopClaw’s web dashboard is the fastest way to operate the runtime from one place.

## Open it

```bash
hopclaw serve
hopclaw dashboard --open
```

Direct URL:

```text
http://127.0.0.1:16280/dashboard/
```

## Overview

**Overview** is the operator home page. It surfaces:

- system status and uptime
- capability health
- channel connectivity
- setup readiness
- recent run and success signals

Use it first when the system “feels off”.

## Assistant

**Assistant** is the main working surface:

- send tasks
- reuse the current session
- inspect the active plan
- review artifacts, approvals, verification, and receipts

Good first prompt:

```text
Check the current runtime status, summarize what is configured, and tell me the next missing setup step.
```

## Runs

**Runs** gives you the canonical execution record:

- status and duration
- tool calls and blocks
- artifacts
- preflight and triage details
- task contract and governance snapshots
- verification and suggested next actions

## Approvals

**Approvals** is the human gate:

- inspect pending approval requests
- review tool, arguments, reason, and session
- approve or deny safely

This is where risky actions stay visible.

## Governance

**Governance** is the control-plane console:

- delivery health
- audit exploration
- policy and AuthZ status
- effective config and runtime facts
- governance adapters and audit sinks

Use it when you need explainability rather than just “it failed”.

## Knowledge

**Knowledge** bundles three operator surfaces:

- managed memory
- knowledge sources
- generated artifacts

Typical uses:

- store durable notes for later runs
- connect a repository or docs source
- inspect generated files from completed work

## Automation

**Automation** manages scheduled and event-driven execution:

- cron jobs
- wakeup triggers
- watch jobs
- webhooks
- agent presets

It is the right place when you are moving from ad-hoc usage to repeatable workflows.

## Settings

**Settings** is split into practical sub-areas:

- Models
- Channels
- Skills
- Plugins
- Browser
- Security
- Infrastructure
- Diagnostics
- Config

This is where you validate a provider, connect channels, test tools, inspect helper health, or edit full config sections.

## Recommended dashboard workflow

1. open **Overview**
2. validate **Models** and **Channels**
3. execute one real task in **Assistant**
4. inspect the receipt in **Runs**
5. review any gates in **Approvals**
6. convert a repeated task into **Automation**

That path gives new operators a productive “first success” in minutes.

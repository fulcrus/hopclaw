# Multi-Session Workflows

## TL;DR

- Session separation is already a first-class runtime concept in HopClaw.
- Use explicit `--session-key` values instead of letting unrelated work collapse into one default thread.
- Inspect active history with `hopclaw sessions list` and `hopclaw sessions get <id>`.

English is canonical in this file. 中文同步 follows after the English section.

## Start Separate Threads On Purpose

```bash
hopclaw message send --session-key sales-ops "Summarize today’s pipeline changes."
hopclaw message send --session-key infra-oncall "Review the latest incident notes."
hopclaw message send --session-key release-qa "Draft the next release checklist."
```

## Inspect Sessions

```bash
hopclaw sessions list
hopclaw sessions get <session-id>
```

If you already know the session key you want to keep using, just keep sending to it:

```bash
hopclaw message send --session-key infra-oncall "Continue from the previous incident review."
```

## Use Memory Deliberately

Session history and KV memory are different surfaces:

```bash
hopclaw memory set team.primary_region us-east-1
hopclaw memory get team.primary_region
hopclaw memory search primary_region
```

Use sessions for conversational context. Use memory for durable facts that should be looked up explicitly.

## Good Naming Pattern

- `team-purpose`
- `channel-thread`
- `customer-incident-<id>`
- `release-<version>`

Avoid anonymous session keys like `test2` once the thread matters operationally.

## 中文同步

### TL;DR

- HopClaw 已经有清晰的 session 概念
- 重要工作请显式传 `--session-key`
- 用 `hopclaw sessions list` / `get` 查看当前线程状态

### 常用命令

```bash
hopclaw message send --session-key infra-oncall "Review the latest incident notes."
hopclaw sessions list
hopclaw sessions get <session-id>
hopclaw memory set team.primary_region us-east-1
```

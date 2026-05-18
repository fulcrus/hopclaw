# First Conversation

## TL;DR

- Start the gateway with a working model provider
- Use `hopclaw message send` for the first end-to-end prompt
- Inspect the result with `hopclaw message list`, `hopclaw sessions`, and `hopclaw logs`

English is canonical in this file. 中文同步 follows after the English section.

## Prerequisites

Make sure the gateway is reachable and at least one provider is configured:

```bash
hopclaw health
hopclaw models list
```

If the gateway is not running yet:

```bash
hopclaw onboard --web-first
```

## Send Your First Prompt

This is the simplest CLI path because it submits a run, waits for completion, and prints the last assistant reply:

```bash
hopclaw message send --session-key demo "Summarize what HopClaw is in three bullets."
```

If you want to keep a separate channel-scoped session:

```bash
hopclaw message send --channel cli --session-key onboarding "List the next three setup tasks."
```

## Inspect The Conversation State

List recent runs:

```bash
hopclaw message list --limit 10
```

Inspect sessions:

```bash
hopclaw sessions list
```

Watch recent runtime events:

```bash
hopclaw logs list --limit 20
```

## If A Run Stops For Approval

Check pending approvals:

```bash
hopclaw approvals list
```

If you prefer the dashboard flow for the same conversation:

```bash
hopclaw dashboard --open
```

## 中文同步

### TL;DR

- 先确认 gateway 可用、模型已配置
- 首次对话最自然的 CLI 路径是 `hopclaw message send`
- 对话结束后用 `hopclaw message list`、`hopclaw sessions list`、`hopclaw logs list` 看状态

### 第一次对话建议

直接执行上文的 `hopclaw message send` 示例即可；它会提交 run、轮询状态，并输出最后一条 assistant 回复。

### 如果卡在审批

先执行：

```bash
hopclaw approvals list
```

也可以用：

```bash
hopclaw dashboard --open
```

在控制台里处理审批。


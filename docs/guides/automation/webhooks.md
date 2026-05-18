# Webhooks

## TL;DR

- The current operator CLI can create, list, inspect, test, and delete outbound webhooks.
- Use a signing secret when the receiver is not fully private and trusted.
- Validate webhook plumbing separately from channel adapters; they are related, but not the same surface.

English is canonical in this file. 中文同步 follows after the English section.

## Create A Webhook

```bash
hopclaw webhooks create \
  --url "https://example.com/hopclaw/webhook" \
  --events "run.completed,tool.executed" \
  --secret "replace-with-a-real-secret"
```

## Inspect And Test

```bash
hopclaw webhooks list
hopclaw webhooks info <id>
hopclaw webhooks test <id>
```

## Delete

```bash
hopclaw webhooks delete <id>
```

## Good Event Selections

- `run.completed` for downstream notifications and workflow chaining
- `tool.executed` for audit or observability sinks
- narrow event sets instead of “everything” when your receiver is expensive or rate-limited

## Operational Notes

- Webhooks are managed by the running gateway, not by static YAML only.
- If delivery tests fail, check receiver authentication, TLS, and any IP allowlist before you assume HopClaw is wrong.
- If you also use the `webhook` channel adapter, keep the two concepts separate:
  - `hopclaw webhooks ...` manages outbound gateway events
  - `channels.webhook` configures a message channel surface

## 中文同步

### TL;DR

- 当前 CLI 已支持 webhook 的创建、查看、测试和删除
- 非完全可信的接收端应始终配置 `--secret`
- `hopclaw webhooks` 和 `channels.webhook` 不是同一个东西

### 常用命令

```bash
hopclaw webhooks create --url "https://example.com/hopclaw/webhook" --events "run.completed" --secret "replace-me"
hopclaw webhooks list
hopclaw webhooks info <id>
hopclaw webhooks test <id>
```

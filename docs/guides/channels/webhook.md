# Webhook Channel

## TL;DR

- Config key: `channels.webhook`
- The channel uses named `instances`, each with its own `callback_url`
- `hopclaw webhooks` and `channels.webhook` are different surfaces

English is canonical in this file. 中文同步 follows after the English section.

## Minimal Config

```yaml
channels:
  webhook:
    enabled: true
    instances:
      alerts:
        callback_url: "https://hooks.example.com/hopclaw"
        secret: env:WEBHOOK_ALERTS_SECRET
```

## Validate

```bash
hopclaw config validate
hopclaw channels validate webhook
hopclaw channels test webhook --target alerts --message "HopClaw webhook smoke test"
hopclaw doctor connectivity
```

## Notes

- Use instance names such as `alerts`, `ops`, or `audit`, not generic names like `default2`.
- `callback_url` is the required per-instance destination.

## 中文同步

### TL;DR

- 配置键是 `channels.webhook`
- 该渠道通过 `instances` 管理多个回调目标
- `hopclaw webhooks` 管的是 gateway 事件投递，不是这个 message channel

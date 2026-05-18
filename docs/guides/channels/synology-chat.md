# Synology Chat

## TL;DR

- Config key: `channels.synology_chat`
- CLI/runtime channel name: `synology-chat`
- Required field: `webhook_url`; `bot_token` is recommended when verifying inbound webhooks

English is canonical in this file. 中文同步 follows after the English section.

## Minimal Config

```yaml
channels:
  synology_chat:
    enabled: true
    base_url: "https://nas.example.com"
    webhook_url: env:SYNOLOGY_CHAT_WEBHOOK_URL
    bot_token: env:SYNOLOGY_CHAT_BOT_TOKEN
```

## Validate

```bash
hopclaw config validate
hopclaw channels validate synology-chat
hopclaw doctor connectivity
```

## Notes

- The runtime adapter requires `webhook_url` to send outbound messages.
- `bot_token` matters when you want to verify inbound webhook authenticity.

## 中文同步

### TL;DR

- 配置键是 `channels.synology_chat`
- CLI 名称是 `synology-chat`
- 发送侧最关键的是 `webhook_url`

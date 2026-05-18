# Mattermost

## TL;DR

- Config key: `channels.mattermost`
- Required fields: `base_url` and `bot_token`
- Optional `websocket_url` helps when the gateway URL differs from the default websocket path

English is canonical in this file. 中文同步 follows after the English section.

## Minimal Config

```yaml
channels:
  mattermost:
    enabled: true
    base_url: "https://mattermost.example.com"
    bot_token: env:MATTERMOST_BOT_TOKEN
    websocket_url: "wss://mattermost.example.com/api/v4/websocket"
    group_policy: open
    reply_in_thread: enabled
```

## Validate

```bash
hopclaw config validate
hopclaw channels validate mattermost
hopclaw channels test mattermost --target <channel-id> --message "HopClaw Mattermost smoke test"
hopclaw doctor connectivity
```

## Notes

- If threads or replies look wrong, check `reply_in_thread` and the Mattermost target type you are sending to.
- Validate websocket reachability separately from bot-token validity.

## 中文同步

### TL;DR

- 配置键是 `channels.mattermost`
- 必填是 `base_url` 和 `bot_token`
- 如果 websocket 路径不标准，可显式写 `websocket_url`

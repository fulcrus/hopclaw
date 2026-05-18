# Telegram

## TL;DR

- Config key: `channels.telegram`
- Required credential: `bot_token`
- Validate with `hopclaw channels validate telegram`

English is canonical in this file. 中文同步 follows after the English section.

## Minimal Config

```yaml
channels:
  telegram:
    enabled: true
    bot_token: env:TELEGRAM_BOT_TOKEN
    dm_policy: open
    group_policy: allowlist
    require_mention: true
```

## Validate

```bash
hopclaw config validate
hopclaw channels validate telegram
hopclaw channels test telegram --message "HopClaw Telegram smoke test"
hopclaw doctor connectivity
```

## Notes

- The runtime expects the BotFather-issued token.
- In groups, `require_mention` is usually the difference between useful behavior and noise.

## 中文同步

### TL;DR

- 配置键是 `channels.telegram`
- 必填是 `bot_token`
- 群聊里通常建议开启 `require_mention`

# Discord

## TL;DR

- Config key: `channels.discord`
- Required credential: `bot_token`
- Validate with `hopclaw channels validate discord`

English is canonical in this file. 中文同步 follows after the English section.

## Minimal Config

```yaml
channels:
  discord:
    enabled: true
    bot_token: env:DISCORD_BOT_TOKEN
    dm_policy: open
    group_policy: open
    group_session_scope: group
```

## Validate

```bash
hopclaw config validate
hopclaw channels validate discord
hopclaw channels test discord --message "HopClaw Discord smoke test"
hopclaw doctor connectivity
```

## Notes

- Most Discord setup failures are invalid bot token or missing gateway intents on the bot side.
- If group messages are ignored, review `require_mention` and `group_policy`.

## 中文同步

### TL;DR

- 配置键是 `channels.discord`
- 只需 `bot_token`
- 用 `hopclaw channels validate discord` 校验

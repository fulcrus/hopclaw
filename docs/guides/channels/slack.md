# Slack

## TL;DR

- Config key: `channels.slack`
- Required credentials: `bot_token` and `app_token`
- Validate with `hopclaw channels validate slack`

English is canonical in this file. 中文同步 follows after the English section.

## Minimal Config

```yaml
channels:
  slack:
    enabled: true
    bot_token: env:SLACK_BOT_TOKEN
    app_token: env:SLACK_APP_TOKEN
    dm_policy: open
    group_policy: open
    group_session_scope: group_thread
    reply_in_thread: enabled
```

## Validate

```bash
hopclaw config validate
hopclaw channels validate slack
hopclaw channels test slack --message "HopClaw Slack smoke test"
hopclaw doctor connectivity
```

## Notes

- Slack uses the bot token plus the app-level Socket Mode token.
- If the adapter connects but does not answer in channels, check `require_mention` and `group_policy`.

## 中文同步

### TL;DR

- 配置键是 `channels.slack`
- 必填是 `bot_token` 和 `app_token`
- 用 `hopclaw channels validate slack` 校验

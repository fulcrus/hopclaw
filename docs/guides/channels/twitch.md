# Twitch

## TL;DR

- Config key: `channels.twitch`
- Required fields: `oauth_token` and `nick`
- Optional `channels` is a comma-separated join list

English is canonical in this file. 中文同步 follows after the English section.

## Minimal Config

```yaml
channels:
  twitch:
    enabled: true
    oauth_token: env:TWITCH_OAUTH_TOKEN
    nick: "hopclawbot"
    channels: "streamer1,streamer2"
```

## Validate

```bash
hopclaw config validate
hopclaw channels validate twitch
hopclaw doctor connectivity
```

## Notes

- This is IRC-style Twitch chat integration, so the same connectivity debugging discipline as IRC applies.
- Keep the channel join list explicit rather than assuming the bot will discover destinations automatically.

## 中文同步

### TL;DR

- 配置键是 `channels.twitch`
- 必填是 `oauth_token` 和 `nick`
- `channels` 是逗号分隔加入列表

# Matrix

## TL;DR

- Config key: `channels.matrix`
- Required fields: `homeserver`, `user_id`, and `access_token`
- Validate with `hopclaw channels validate matrix`

English is canonical in this file. 中文同步 follows after the English section.

## Minimal Config

```yaml
channels:
  matrix:
    enabled: true
    homeserver: "https://matrix.example.com"
    user_id: "@hopclaw:example.com"
    access_token: env:MATRIX_ACCESS_TOKEN
    group_policy: open
    require_mention: true
    reply_in_thread: enabled
```

## Validate

```bash
hopclaw config validate
hopclaw channels validate matrix
hopclaw channels test matrix --target '!roomid:example.com' --message "HopClaw Matrix smoke test"
hopclaw doctor connectivity
```

## Notes

- The runtime key is `homeserver`, while some Matrix docs elsewhere call the same thing `home_server`.
- Pass a room ID explicitly during test sends.

## 中文同步

### TL;DR

- 配置键是 `channels.matrix`
- 必填是 `homeserver`、`user_id`、`access_token`
- 建议测试时显式传房间 ID

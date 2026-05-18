# LINE

## TL;DR

- Config key: `channels.line`
- Required credentials: `channel_secret` and `channel_token`
- Validate with `hopclaw channels validate line`

English is canonical in this file. 中文同步 follows after the English section.

## Minimal Config

```yaml
channels:
  line:
    enabled: true
    channel_secret: env:LINE_CHANNEL_SECRET
    channel_token: env:LINE_CHANNEL_TOKEN
    dm_policy: open
```

## Validate

```bash
hopclaw config validate
hopclaw channels validate line
hopclaw doctor connectivity
```

## Notes

- If webhook verification is failing, check that the channel secret matches the LINE console value exactly.
- Keep LINE-specific target identifiers outside prompts and in operator config where possible.

## 中文同步

### TL;DR

- 配置键是 `channels.line`
- 必填是 `channel_secret` 和 `channel_token`
- webhook 校验异常时优先检查 channel secret

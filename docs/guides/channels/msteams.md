# Microsoft Teams

## TL;DR

- Config key: `channels.msteams`
- Required credentials: `app_id` and `password`
- Validate with `hopclaw channels validate msteams`

English is canonical in this file. 中文同步 follows after the English section.

## Minimal Config

```yaml
channels:
  msteams:
    enabled: true
    app_id: env:MSTEAMS_APP_ID
    password: env:MSTEAMS_APP_PASSWORD
    dm_policy: open
    group_policy: open
```

## Validate

```bash
hopclaw config validate
hopclaw channels validate msteams
hopclaw doctor connectivity
```

## Notes

- This adapter uses Bot Framework credentials.
- If auth succeeds but inbound messages do not arrive, re-check the Bot Framework webhook side before touching prompts.

## 中文同步

### TL;DR

- 配置键是 `channels.msteams`
- 必填是 `app_id` 和 `password`
- 这是 Bot Framework 风格接入

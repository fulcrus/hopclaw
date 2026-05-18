# Zalo Official Account

## TL;DR

- Config key: `channels.zalo`
- Required fields: `app_id`, `secret_key`, and `access_token`
- Validate with `hopclaw channels validate zalo`

English is canonical in this file. 中文同步 follows after the English section.

## Minimal Config

```yaml
channels:
  zalo:
    enabled: true
    app_id: env:ZALO_APP_ID
    secret_key: env:ZALO_SECRET_KEY
    access_token: env:ZALO_ACCESS_TOKEN
    refresh_token: env:ZALO_REFRESH_TOKEN
```

## Validate

```bash
hopclaw config validate
hopclaw channels validate zalo
hopclaw channels test zalo --target <user-id> --message "HopClaw Zalo smoke test"
hopclaw doctor connectivity
```

## Notes

- Outbound sends require the recipient user ID as target.
- If inbound verification fails, re-check the OA-side webhook secret/token alignment.

## 中文同步

### TL;DR

- 配置键是 `channels.zalo`
- 必填是 `app_id`、`secret_key`、`access_token`
- 测试外发时需要显式用户 ID

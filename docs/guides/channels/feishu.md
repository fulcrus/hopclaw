# Feishu / Lark

## TL;DR

- Config key: `channels.feishu`
- Minimum credentials: `app_id` and `app_secret`
- Supports single-account and multi-account layouts

English is canonical in this file. 中文同步 follows after the English section.

## Minimal Config

```yaml
channels:
  feishu:
    enabled: true
    app_id: env:FEISHU_APP_ID
    app_secret: env:FEISHU_APP_SECRET
    encrypt_key: env:FEISHU_ENCRYPT_KEY
    verification_token: env:FEISHU_VERIFICATION_TOKEN
    domain: feishu
    connection_mode: websocket
```

Multi-account example:

```yaml
channels:
  feishu:
    enabled: true
    default_account: corp-a
    accounts:
      corp-a:
        enabled: true
        app_id: env:FEISHU_APP_ID_A
        app_secret: env:FEISHU_APP_SECRET_A
      corp-b:
        enabled: true
        app_id: env:FEISHU_APP_ID_B
        app_secret: env:FEISHU_APP_SECRET_B
```

## Validate

```bash
hopclaw config validate
hopclaw channels validate feishu
hopclaw doctor connectivity
```

## Notes

- `connection_mode` can be `websocket` or `webhook`.
- If webhook verification fails, check `encrypt_key` and `verification_token` first.

## 中文同步

### TL;DR

- 配置键是 `channels.feishu`
- 最小凭据是 `app_id` 和 `app_secret`
- 当前支持多账号配置

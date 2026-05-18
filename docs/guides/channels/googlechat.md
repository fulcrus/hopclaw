# Google Chat

## TL;DR

- Config key: `channels.googlechat`
- You must provide either `webhook_url` or `service_account`
- Validate with `hopclaw channels validate googlechat`

English is canonical in this file. 中文同步 follows after the English section.

## Minimal Config

Webhook mode:

```yaml
channels:
  googlechat:
    enabled: true
    webhook_url: env:GOOGLECHAT_WEBHOOK_URL
    verification_key: env:GOOGLECHAT_VERIFICATION_KEY
```

Service-account mode:

```yaml
channels:
  googlechat:
    enabled: true
    service_account: "/absolute/path/to/googlechat-service-account.json"
    verification_key: env:GOOGLECHAT_VERIFICATION_KEY
```

## Validate

```bash
hopclaw config validate
hopclaw channels validate googlechat
hopclaw channels test googlechat --target spaces/AAA... --message "HopClaw Google Chat smoke test"
hopclaw doctor connectivity
```

## Notes

- `verification_key` protects inbound event validation.
- If you use service-account mode, pass the target space explicitly when you test outbound sends.

## 中文同步

### TL;DR

- 配置键是 `channels.googlechat`
- `webhook_url` 和 `service_account` 二选一
- `verification_key` 用于入站事件校验

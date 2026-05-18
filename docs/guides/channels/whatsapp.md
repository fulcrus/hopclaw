# WhatsApp

## TL;DR

- Config key: `channels.whatsapp`
- Minimum credentials: `phone_id` and `api_token`
- Validate with `hopclaw channels validate whatsapp`

English is canonical in this file. 中文同步 follows after the English section.

## Minimal Config

```yaml
channels:
  whatsapp:
    enabled: true
    phone_id: env:WHATSAPP_PHONE_ID
    api_token: env:WHATSAPP_API_TOKEN
    base_url: "https://graph.facebook.com"
    dm_policy: open
```

## Validate

```bash
hopclaw config validate
hopclaw channels validate whatsapp
hopclaw doctor connectivity
```

## Notes

- This adapter targets the WhatsApp Cloud API.
- If outbound delivery fails, verify both the token and the configured phone ID.

## 中文同步

### TL;DR

- 配置键是 `channels.whatsapp`
- 最小凭据是 `phone_id` 和 `api_token`
- 外发失败时优先核查 phone ID 和 token 是否匹配

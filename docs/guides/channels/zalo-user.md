# Zalo User

## TL;DR

- Config key: `channels.zalouser`
- Required fields in practice: `cookie` and `base_url`
- Optional `imei` helps when the backing user bridge requires device identity

English is canonical in this file. 中文同步 follows after the English section.

## Minimal Config

```yaml
channels:
  zalouser:
    enabled: true
    cookie: env:ZALO_USER_COOKIE
    imei: env:ZALO_USER_IMEI
    base_url: "https://chat.zalo.me"
```

## Validate

```bash
hopclaw config validate
hopclaw channels validate zalouser
hopclaw channels test zalouser --target <user-id> --message "HopClaw Zalo User smoke test"
hopclaw doctor connectivity
```

## Notes

- The setup catalog marks `base_url` as optional, but the adapter connect path currently requires it for real use.
- Treat this as a bridge-style integration, not the same thing as the official OA adapter.

## 中文同步

### TL;DR

- 配置键是 `channels.zalouser`
- 实际可运行时通常需要 `cookie` 和 `base_url`
- 这不是官方 OA adapter，而是用户侧 bridge 风格接入

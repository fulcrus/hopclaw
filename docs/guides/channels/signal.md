# Signal

## TL;DR

- Config key: `channels.signal`
- Minimum fields: `base_url` and `number`
- Optional `auth_token` is used when your signal bridge requires it

English is canonical in this file. 中文同步 follows after the English section.

## Minimal Config

```yaml
channels:
  signal:
    enabled: true
    base_url: "http://127.0.0.1:8080"
    number: "+8613800138000"
    auth_token: env:SIGNAL_AUTH_TOKEN
```

## Validate

```bash
hopclaw config validate
hopclaw channels validate signal
hopclaw doctor connectivity
```

## Notes

- This adapter expects a `signal-cli` style REST bridge.
- If validation fails immediately, check bridge reachability before credentials.

## 中文同步

### TL;DR

- 配置键是 `channels.signal`
- 最小字段是 `base_url` 和 `number`
- `auth_token` 取决于你的 bridge 是否启用认证

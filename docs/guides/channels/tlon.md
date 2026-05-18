# Tlon / Urbit

## TL;DR

- Config key: `channels.tlon`
- Required fields: `ship_url` and `ship_code`
- Validate with `hopclaw channels validate tlon`

English is canonical in this file. 中文同步 follows after the English section.

## Minimal Config

```yaml
channels:
  tlon:
    enabled: true
    ship_url: "http://localhost:8080"
    ship_code: env:TLON_SHIP_CODE
```

## Validate

```bash
hopclaw config validate
hopclaw channels validate tlon
hopclaw channels test tlon --target ~zod/general --message "HopClaw Tlon smoke test"
hopclaw doctor connectivity
```

## Notes

- Outbound `--target` values are expected in a ship/channel form such as `~ship/channel-name`.
- If auth fails early, re-check the ship code before touching anything else.

## 中文同步

### TL;DR

- 配置键是 `channels.tlon`
- 必填是 `ship_url` 和 `ship_code`
- 测试目标一般写成 `~ship/channel-name`

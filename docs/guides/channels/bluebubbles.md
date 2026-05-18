# BlueBubbles

## TL;DR

- Config key: `channels.bluebubbles`
- Required fields: `base_url` and `password`
- Validate with `hopclaw channels validate bluebubbles`

English is canonical in this file. 中文同步 follows after the English section.

## Minimal Config

```yaml
channels:
  bluebubbles:
    enabled: true
    base_url: "http://127.0.0.1:1234"
    password: env:BLUEBUBBLES_PASSWORD
```

## Validate

```bash
hopclaw config validate
hopclaw channels validate bluebubbles
hopclaw channels test bluebubbles --target <chat-guid> --message "HopClaw BlueBubbles smoke test"
hopclaw doctor connectivity
```

## Notes

- This is the dedicated BlueBubbles adapter, separate from the more generic `imessage` config key.
- Outbound tests usually require an explicit chat GUID target.

## 中文同步

### TL;DR

- 配置键是 `channels.bluebubbles`
- 必填是 `base_url` 和 `password`
- 发消息测试时通常要显式传 `chat-guid`

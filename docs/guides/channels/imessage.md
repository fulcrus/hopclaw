# iMessage / BlueBubbles Bridge

## TL;DR

- Config key: `channels.imessage`
- Minimum fields: `base_url`, `api_key`
- The shipped adapter requires bridge authentication; treat `api_key` as required

English is canonical in this file. 中文同步 follows after the English section.

## Minimal Config

```yaml
channels:
  imessage:
    enabled: true
    base_url: "http://127.0.0.1:1234"
    api_key: env:IMESSAGE_BRIDGE_API_KEY
```

## Validate

```bash
hopclaw config validate
hopclaw channels validate imessage
hopclaw channels test imessage --target <chat-guid> --message "HopClaw iMessage smoke test"
hopclaw doctor connectivity
```

## Notes

- The shipped config schema calls this adapter `imessage`, even though many deployments back it with BlueBubbles-style bridges.
- For direct message delivery tests, pass the chat GUID explicitly with `--target`.

## 中文同步

### TL;DR

- 配置键是 `channels.imessage`
- 最小字段是 `base_url` 和 `api_key`
- 当前内置适配器默认要求 bridge 鉴权，因此 `api_key` 视为必填

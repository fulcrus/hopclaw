# IRC

## TL;DR

- Config key: `channels.irc`
- Minimum fields: `server` and `nick`
- Optional fields: `password`, `use_tls`, `channels`

English is canonical in this file. 中文同步 follows after the English section.

## Minimal Config

```yaml
channels:
  irc:
    enabled: true
    server: "irc.libera.chat:6697"
    nick: "hopclaw-bot"
    password: env:IRC_PASSWORD
    use_tls: true
    channels: "#ops,#release"
```

## Validate

```bash
hopclaw config validate
hopclaw channels validate irc
hopclaw doctor connectivity
```

## Notes

- `channels` is a comma-separated list in the current config schema.
- Validate connectivity before you debug IRC-specific permissions or channel modes.

## 中文同步

### TL;DR

- 配置键是 `channels.irc`
- 最小字段是 `server` 和 `nick`
- `channels` 在当前 schema 里是逗号分隔字符串

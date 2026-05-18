# Nostr

## TL;DR

- Config key: `channels.nostr`
- Required fields: `private_key` and `relays`
- Validate with `hopclaw channels validate nostr`

English is canonical in this file. 中文同步 follows after the English section.

## Minimal Config

```yaml
channels:
  nostr:
    enabled: true
    private_key: env:NOSTR_PRIVATE_KEY
    relays:
      - "wss://relay.damus.io"
      - "wss://nos.lol"
```

## Validate

```bash
hopclaw config validate
hopclaw channels validate nostr
hopclaw doctor connectivity
```

## Notes

- The adapter requires at least one reachable relay.
- Direct-message flows are more constrained than public note publication, so validate your expected mode explicitly.

## 中文同步

### TL;DR

- 配置键是 `channels.nostr`
- 必填是 `private_key` 和 `relays`
- 至少要有一个真正可连通的 relay

# Nextcloud Talk

## TL;DR

- Config key: `channels.nextcloud_talk`
- CLI/runtime channel name: `nextcloud-talk`
- Required fields: `base_url`, `username`, and `password`

English is canonical in this file. 中文同步 follows after the English section.

## Minimal Config

```yaml
channels:
  nextcloud_talk:
    enabled: true
    base_url: "https://cloud.example.com"
    username: "hopclaw-bot"
    password: env:NEXTCLOUD_APP_PASSWORD
```

## Validate

```bash
hopclaw config validate
hopclaw channels validate nextcloud-talk
hopclaw channels test nextcloud-talk --target <room-token> --message "HopClaw Nextcloud Talk smoke test"
hopclaw doctor connectivity
```

## Notes

- The config key uses underscore style, but the runtime adapter name uses a hyphen.
- Test sends usually require an explicit conversation token as `--target`.

## 中文同步

### TL;DR

- 配置键是 `channels.nextcloud_talk`
- CLI 名称是 `nextcloud-talk`
- 必填是 `base_url`、`username`、`password`

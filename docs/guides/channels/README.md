# Channel Integration Overview

## TL;DR

- Every built-in channel lives under `channels.<channel-id>` in config.
- Use `hopclaw channels list`, `hopclaw channels status`, and `hopclaw channels validate <name>` after wiring a channel.
- Setup and onboarding currently support a subset of the shipped adapters; the runtime surface is broader than the setup wizard.
- Channel support levels are explicit: `supported` means product-facing and documented, `experimental` means implemented but not promised at the same depth.
- Common operator policy keys such as `dm_policy`, `group_policy`, `require_mention`, and `reply_in_thread` materially change who can trigger the agent and where replies appear.

English is canonical in this file. 中文同步 follows after the English section.

## What This Page Covers

This page is the operator-facing overview for the channel adapters already shipped in the repository. It helps you:

- choose the right built-in adapter
- understand whether setup or onboarding can configure it for you
- see the minimum fields each adapter needs
- validate a new channel with copy-pasteable CLI checks

## General Channel Pattern

Built-in channel blocks live under:

```yaml
channels:
  <channel-id>:
    enabled: true
    ...
```

After editing config, validate the result:

```bash
hopclaw config validate
hopclaw channels list
hopclaw channels status
hopclaw doctor connectivity
```

If the gateway is already running and you want to add a mutable channel from the operator surface instead of editing YAML by hand:

```bash
hopclaw channels add slack --interactive
```

## Support Levels

- `supported`: operator-facing path with catalog visibility, setup or onboarding depth where applicable, and documentation kept in the main product path
- `experimental`: implemented and callable, but onboarding depth, tests, and operational guidance are intentionally thinner

For channels, HopClaw currently reserves `core` for install/runtime/dashboard paths rather than adapter breadth. No built-in channel is tagged `core`.

## Supported Built-In Channels

The current product catalog and `channels/` tree ship the following adapters.

| Channel ID | Display name | Support level | Setup wizard | Onboarding | Minimum fields |
| --- | --- | --- | --- | --- | --- |
| `feishu` | Feishu / Lark | `supported` | Yes | Yes | `app_id`, `app_secret` |
| `slack` | Slack | `supported` | Yes | Yes | `bot_token`, `app_token` |
| `discord` | Discord | `supported` | Yes | Yes | `bot_token` |
| `telegram` | Telegram | `supported` | Yes | Yes | `bot_token` |
| `webhook` | Webhook | `supported` | No | No | `callback_url` and usually `secret` |
| `whatsapp` | WhatsApp | `supported` | Yes | Yes | `phone_id`, `api_token` |
| `signal` | Signal | `supported` | Yes | Yes | `base_url`, `number` |
| `imessage` | iMessage / BlueBubbles | `supported` | Yes | Yes | `base_url`, `api_key` |
| `line` | LINE | `supported` | Yes | Yes | `channel_secret`, `channel_token` |
| `msteams` | Microsoft Teams | `supported` | Yes | Yes | `app_id`, `password` |
| `googlechat` | Google Chat | `experimental` | No | No | `service_account` or `webhook_url` |
| `irc` | IRC | `supported` | Yes | Yes | `server`, `nick` |
| `matrix` | Matrix | `supported` | Yes | Yes | `homeserver`, `user_id`, `access_token` |
| `mattermost` | Mattermost | `supported` | Yes | Yes | `base_url`, `bot_token` |
| `nextcloud_talk` | Nextcloud Talk | `supported` | Yes | Yes | `base_url`, `username`, `password` |
| `nostr` | Nostr | `supported` | Yes | Yes | `private_key`, `relays` |
| `bluebubbles` | BlueBubbles | `supported` | Yes | Yes | `base_url`, `password` |
| `synology_chat` | Synology Chat | `experimental` | No | No | usually `base_url` and `bot_token` or `webhook_url` |
| `tlon` | Tlon / Urbit | `supported` | Yes | Yes | `ship_url`, `ship_code` |
| `twitch` | Twitch | `supported` | Yes | Yes | `oauth_token`, `nick` |
| `zalo` | Zalo | `experimental` | No | No | `app_id`, `secret_key`, `access_token` |
| `zalouser` | Zalo User | `supported` | Yes | Yes | `cookie` |

Notes:

- `setup` and `onboard` only expose channels marked as setup- or onboarding-supported in the product catalog.
- `supported` does not mean every channel has the same onboarding depth; it means the adapter is part of the main product contract. `experimental` channels remain available but should not be described as equal-depth product paths.
- The repository also contains local or plugin-facing channel surfaces such as `stdio` and plugin-declared webhook channels, but those are not configured as ordinary built-in YAML blocks.
- `stdio` channels are documented separately in [`stdio.md`](./stdio.md) because they come from plugin manifests and appear at runtime as `plugin:<channel-key>`.
- A `wechat` guide exists in this docs tree only as a status page. There is no first-party built-in WeChat adapter in the current shipped catalog.

## Per-Channel Guides

- [`slack.md`](./slack.md)
- [`discord.md`](./discord.md)
- [`telegram.md`](./telegram.md)
- [`feishu.md`](./feishu.md)
- [`wechat.md`](./wechat.md)
- [`webhook.md`](./webhook.md)
- [`whatsapp.md`](./whatsapp.md)
- [`signal.md`](./signal.md)
- [`imessage.md`](./imessage.md)
- [`stdio.md`](./stdio.md)
- [`line.md`](./line.md)
- [`msteams.md`](./msteams.md)
- [`googlechat.md`](./googlechat.md)
- [`irc.md`](./irc.md)
- [`matrix.md`](./matrix.md)
- [`mattermost.md`](./mattermost.md)
- [`nextcloud-talk.md`](./nextcloud-talk.md)
- [`nostr.md`](./nostr.md)
- [`bluebubbles.md`](./bluebubbles.md)
- [`synology-chat.md`](./synology-chat.md)
- [`tlon.md`](./tlon.md)
- [`twitch.md`](./twitch.md)
- [`zalo.md`](./zalo.md)
- [`zalo-user.md`](./zalo-user.md)

## Common Operator Policy Fields

Many messaging adapters share the same operator policy fields. These matter more than most first-time users expect.

| Key | What it controls | Typical values |
| --- | --- | --- |
| `dm_policy` | Whether direct messages can reach the runtime | `open`, `allowlist`, `pairing` |
| `group_policy` | Whether group chats can reach the runtime | `open`, `allowlist`, `disabled` |
| `require_mention` | Whether group messages must mention the bot | `true`, `false` |
| `group_session_scope` | How group chat traffic is partitioned into sessions | `group`, `group_sender`, `group_thread`, `group_thread_sender` |
| `reply_in_thread` | Whether replies stay in a thread when the platform supports threads | `enabled`, `disabled` |
| `allow_from` | Direct-message allowlist | platform-specific user IDs |
| `group_allow_from` | Group allowlist | platform-specific user or group IDs |

If a channel is connected but “does nothing,” check these first.

## Recommended Validation Loop

Use this sequence after every channel change:

```bash
hopclaw config validate
hopclaw channels list
hopclaw channels status
hopclaw channels validate <channel-name>
hopclaw channels test <channel-name> --message "HopClaw channel smoke test"
hopclaw doctor connectivity
```

Examples:

```bash
hopclaw channels validate slack
hopclaw channels test slack --message "HopClaw channel smoke test"

hopclaw channels validate matrix
hopclaw channels test matrix --message "HopClaw channel smoke test" --target '!roomid:example.com'
```

## Copy-Paste Config Examples

### Slack

```yaml
channels:
  slack:
    enabled: true
    bot_token: env:SLACK_BOT_TOKEN
    app_token: env:SLACK_APP_TOKEN
    dm_policy: open
    group_policy: open
    group_session_scope: group_thread
    reply_in_thread: enabled
```

### Discord

```yaml
channels:
  discord:
    enabled: true
    bot_token: env:DISCORD_BOT_TOKEN
    dm_policy: open
    group_policy: open
    group_session_scope: group
```

### Telegram

```yaml
channels:
  telegram:
    enabled: true
    bot_token: env:TELEGRAM_BOT_TOKEN
    dm_policy: open
    group_policy: allowlist
    require_mention: true
```

### Feishu / Lark

```yaml
channels:
  feishu:
    enabled: true
    app_id: ${FEISHU_APP_ID}
    app_secret: ${FEISHU_APP_SECRET}
    encrypt_key: env:FEISHU_ENCRYPT_KEY
    verification_token: env:FEISHU_VERIFICATION_TOKEN
    domain: feishu
    connection_mode: websocket
```

Feishu also supports multiple accounts:

```yaml
channels:
  feishu:
    enabled: true
    default_account: corp-a
    accounts:
      corp-a:
        enabled: true
        app_id: ${FEISHU_APP_ID_A}
        app_secret: ${FEISHU_APP_SECRET_A}
      corp-b:
        enabled: true
        app_id: ${FEISHU_APP_ID_B}
        app_secret: ${FEISHU_APP_SECRET_B}
```

### Matrix

```yaml
channels:
  matrix:
    enabled: true
    homeserver: https://matrix.example.com
    user_id: "@hopclaw:example.com"
    access_token: env:MATRIX_ACCESS_TOKEN
    group_policy: open
    require_mention: true
    reply_in_thread: enabled
```

### WhatsApp

```yaml
channels:
  whatsapp:
    enabled: true
    phone_id: ${WHATSAPP_PHONE_ID}
    api_token: env:WHATSAPP_API_TOKEN
    base_url: https://graph.facebook.com
    dm_policy: open
```

### Nostr

```yaml
channels:
  nostr:
    enabled: true
    private_key: env:NOSTR_PRIVATE_KEY
    relays:
      - wss://relay.damus.io
      - wss://relay.primal.net
```

### Generic Webhook

Built-in webhook delivery is useful for callback-style integrations and operator notifications.

```yaml
channels:
  webhook:
    enabled: true
    instances:
      alerts:
        callback_url: https://hooks.example.com/hopclaw
        secret: env:HOPCLAW_WEBHOOK_SECRET
```

## Choosing A Channel

Use this heuristic:

- choose `slack`, `discord`, `telegram`, or `feishu` when you want mainstream interactive chat workflows
- choose `matrix`, `mattermost`, or `irc` for self-hosted or infra-oriented communities
- choose `whatsapp`, `signal`, `line`, `twitch`, `zalo`, or `tlon` when the delivery surface is the product requirement
- choose `webhook` when you mainly need outbound callbacks or HTTP-driven bridges
- choose `stdio` when you need a process-isolated custom bridge implemented as a plugin package
- choose `imessage` or `bluebubbles` only when you already operate the compatible bridge

## Setup vs Manual Wiring

Use the setup wizard when:

- the channel is marked setup-supported
- you only need one account
- the default operator fields are enough

Edit YAML or use `hopclaw channels add` manually when:

- you need a channel that is not exposed in setup/onboarding
- you need multi-account Feishu/Lark
- you need fine-grained policy fields or secret-reference control
- you are building a repeatable infra-managed config

## Troubleshooting Checklist

If a channel is configured but not behaving correctly:

1. Validate the file and the operator view.

```bash
hopclaw config validate
hopclaw channels list
```

2. Check health and reconnection.

```bash
hopclaw channels status
hopclaw channels validate <channel-name>
```

3. Send a smoke test.

```bash
hopclaw channels test <channel-name> --message "HopClaw smoke test"
```

4. Run cross-surface diagnostics.

```bash
hopclaw doctor connectivity
hopclaw doctor auth
```

5. Re-check channel policy fields.

Look especially at `group_policy`, `require_mention`, and `reply_in_thread`.

## 中文同步

### TL;DR

- 内建渠道都写在 `channels.<channel-id>` 下面。
- 渠道接好后先跑 `hopclaw channels list`、`hopclaw channels status`、`hopclaw channels validate <name>`。
- 安装向导只覆盖一部分渠道，真实运行面比 setup/onboarding 展示的更大。
- `dm_policy`、`group_policy`、`require_mention`、`reply_in_thread` 这些策略字段会直接影响“能不能触发”和“回复发到哪里”。
- 当前没有内建的一方 `wechat` adapter；`wechat.md` 是状态说明页，不是接入指南。
- `stdio` 渠道单独写在 [`stdio.md`](./stdio.md)；它不是普通 `channels.<id>` YAML 配置，而是插件 manifest 驱动、运行时名形如 `plugin:<key>`。

### 最常用校验命令

```bash
hopclaw config validate
hopclaw channels list
hopclaw channels status
hopclaw channels validate slack
hopclaw channels test slack --message "HopClaw channel smoke test"
hopclaw doctor connectivity
```

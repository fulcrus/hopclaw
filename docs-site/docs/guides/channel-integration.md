---
sidebar_position: 3
title: Channel Integration
---

# Channel Integration

HopClaw can receive requests and deliver outcomes through multiple chat channels. This guide covers practical examples for the most common ones.

## General pattern

Every channel block lives under:

```yaml
channels:
  <channel-id>:
```

After editing config:

```bash
hopclaw serve
hopclaw dashboard --open
```

Then validate from **Settings → Channels**.

## Slack

```yaml title="config.yaml"
channels:
  slack:
    enabled: true
    bot_token: ${SLACK_BOT_TOKEN}
    app_token: ${SLACK_APP_TOKEN}
    dm_policy: open
    group_policy: open
    require_mention: true
    reply_in_thread: enabled
```

Use Slack when you want:

- Socket Mode delivery
- thread-aware replies
- workspace-native operator access

## Discord

```yaml title="config.yaml"
channels:
  discord:
    enabled: true
    bot_token: ${DISCORD_BOT_TOKEN}
    dm_policy: open
    group_policy: open
    require_mention: true
    reply_in_thread: enabled
```

## Telegram

```yaml title="config.yaml"
channels:
  telegram:
    enabled: true
    bot_token: ${TELEGRAM_BOT_TOKEN}
    dm_policy: open
    group_policy: open
    require_mention: true
```

## Feishu / Lark

```yaml title="config.yaml"
channels:
  feishu:
    enabled: true
    app_id: ${FEISHU_APP_ID}
    app_secret: ${FEISHU_APP_SECRET}
    domain: feishu
    connection_mode: websocket
    dm_policy: open
    group_policy: open
```

You can also configure multiple accounts under `accounts` if your deployment spans different domains.

## Generic webhook delivery

The built-in `webhook` channel profile is currently not part of interactive setup, but it is still useful for outbound integration and callback-style workflows.

Example plugin-declared webhook channel:

```yaml
channels:
  alerts:
    type: webhook
    callback_url: https://hooks.example.com/hopclaw
    secret: ${HOPCLAW_WEBHOOK_SECRET}
```

## Channel policies that matter

Common options from the shipped examples:

- `dm_policy`
- `group_policy`
- `require_mention`
- `group_session_scope`
- `reply_in_thread`
- `allow_from`
- `group_allow_from`

These determine how much of the runtime is reachable from each chat surface.

## Test after wiring

Recommended loop:

```bash
hopclaw serve
hopclaw status
hopclaw dashboard --open
```

Then:

1. send a simple test message through the channel
2. confirm it lands in **Assistant**
3. inspect the resulting execution in **Runs**
4. verify approvals, if any, in **Approvals**

## Supported catalog channels today

The current channel catalog includes options such as:

- Feishu / Lark
- Slack
- Discord
- Telegram
- WhatsApp
- Signal
- iMessage / BlueBubbles
- LINE
- Microsoft Teams
- IRC
- Matrix
- Mattermost
- Nextcloud Talk
- Nostr
- Tlon / Urbit
- Twitch
- Zalo User

Not every channel is exposed by the setup wizard, but the runtime and product catalog already cover a broad integration surface.

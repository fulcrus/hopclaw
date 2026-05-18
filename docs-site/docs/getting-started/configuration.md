---
sidebar_position: 3
title: Configuration
---

# Configuration

HopClaw loads YAML configuration and expands `${ENV_VAR}` placeholders at parse time.

## Config discovery

HopClaw uses:

1. `--config /path/to/config.yaml`
2. auto-discovery via config search
3. environment-backed generated defaults during setup and onboarding

## Top-level sections

The main config object currently includes these sections:

```yaml
server:
auth:
store:
agent:
runtime:
update:
diagnostics:
skills:
models:
tools:
hosts:
channels:
plugins:
cron:
watch:
wakeup:
sandbox:
exec_approval:
security:
locale:
```

## Model providers

The simplest provider path is `models.openai_compat`:

```yaml
models:
  openai_compat:
    base_url: https://api.openai.com/v1
    api_key: ${OPENAI_API_KEY}
    model: gpt-4.1-mini
    timeout: 60s
```

When you want HopClaw to generate the provider block interactively:

```bash
hopclaw onboard
```

## Channels

The setup and onboarding catalogs currently expose practical integrations such as:

- Feishu / Lark
- Slack
- Discord
- Telegram
- WhatsApp
- Signal
- LINE
- Microsoft Teams
- IRC
- Matrix
- Mattermost
- Nostr
- Tlon / Urbit
- Twitch
- Zalo User

Example Slack block:

```yaml
channels:
  slack:
    enabled: true
    bot_token: ${SLACK_BOT_TOKEN}
    app_token: ${SLACK_APP_TOKEN}
    dm_policy: open
    group_policy: open
    require_mention: true
```

## Approval and execution controls

HopClaw’s built-in execution controls live under `tools` and `exec_approval` / `security` surfaces:

```yaml
tools:
  builtins:
    enabled: true
    root: .
    default_exec_timeout: 30s
  capabilities:
    exec:
      mode: approve
      timeout: 30s
      max_output: 1048576
    net:
      allow_private: false
      allow_local: true
```

Use these knobs when you want:

- explicit approval before execution
- bounded file reads
- network restrictions
- separate local helper configuration

## Skills and plugins

Skills can be auto-detected from directories:

```yaml
skills:
  include_catalog: true
  auto_detect: true
  install_policy: ask
  ensure_limit: 5
  dirs:
    - ./.hopclaw/skills
```

Plugins have their own config section and are discovered from:

- `./.hopclaw/plugins`
- `./.hopclaw/extensions`
- `./extensions`
- `~/.hopclaw/plugins`
- `~/.hopclaw/extensions`

## Runtime profile

Profiles change the security and operations posture:

```yaml
runtime:
  profile: desktop
```

Supported examples from the current config help text:

- `desktop`
- `trusted_desktop`
- `production`

## Locale

Set the product locale explicitly:

```yaml
locale: ja-JP
```

Or use environment-based detection with:

```bash
export HOPCLAW_LOCALE=ja-JP
```

## Recommended edit loop

```bash
hopclaw config show
hopclaw doctor
hopclaw serve
hopclaw dashboard --open
```

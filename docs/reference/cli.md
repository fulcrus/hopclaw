# CLI Reference

## TL;DR

- `hopclaw` with no subcommand starts the interactive REPL.
- Use `hopclaw --config ./config.yaml <command>` when you want to target a specific config file.
- Use `--json` on operator and inspection commands when you need machine-readable output.
- `hopclaw update` is advisory only: it checks releases and prints guidance, but does not replace binaries automatically.

English is canonical in this file. 中文同步 follows after the English section.

## Root Command

The root command is:

```bash
hopclaw
```

When you run it without a subcommand:

- on a TTY, HopClaw launches the interactive REPL
- if local `hopclaw serve` instances are detected, the REPL asks you to choose a local connection or start a private local session
- with piped stdin or non-TTY stdout, HopClaw switches to one-shot mode and handles the provided text as a single interaction

Examples:

```bash
hopclaw
hopclaw --session incident-42
hopclaw --remote local-dev
hopclaw --remote https://hopclaw.example.com
hopclaw --local
hopclaw --session incident-42 --model gpt-4.1-mini --think
printf 'Summarize the last failed run.\n' | hopclaw --session ops
```

## Global Flags

These flags are available on the root command and most subcommands:

| Flag | Meaning |
| --- | --- |
| `--config <path>` | Read config from a specific YAML file instead of auto-discovery |
| `--verbose` | Print extra progress output |
| `--json` | Emit JSON instead of human-readable text when supported |
| `--session`, `-s` | Session key used by the interactive REPL |
| `--model <name>` | Model override for the interactive REPL |
| `--think` | Enable REPL thinking mode at startup |
| `--remote <name-or-url>` | Attach the REPL or gateway-backed command to a named local connection, saved remote, or explicit gateway URL |
| `--local` | Force the REPL or gateway-backed command to use the configured local connection path |

## Config Auto-Discovery

Unless you pass `--config`, HopClaw looks for config files in this order:

```text
$HOPCLAW_CONFIG
./.hopclaw/config.yaml
~/.hopclaw/config.yaml
/etc/hopclaw/config.yaml
```

## Command Groups

Top-level commands are organized into four groups in the CLI help:

| Group | Commands |
| --- | --- |
| Getting Started | `dashboard`, `onboard`, `serve`, `setup` |
| Runtime & Dashboard | `agents`, `approvals`, `browser`, `devices`, `health`, `memory`, `message`, `models`, `nodes`, `pairing`, `qr`, `remote`, `sessions`, `status`, `tui` |
| Automation & Integrations | `automation`, `channels`, `hooks`, `plugins`, `skills`, `tools`, `webhooks` |
| Config, Ops & Maintenance | `backup`, `bug-report`, `completion`, `config`, `daemon`, `dns`, `doctor`, `logs`, `sandbox`, `secrets`, `security`, `uninstall`, `update`, `version` |

## Getting Started Commands

| Command | What it does | Notes |
| --- | --- | --- |
| `hopclaw serve` | Start the local gateway server | Default service entrypoint; use `--name <local-name>` to publish a stable local runtime name for CLI selection |
| `hopclaw setup` | Run the minimal first-time setup wizard | `--non-interactive` uses environment variables only |
| `hopclaw onboard` | Run the full guided onboarding flow | `--web-first` defers model/channel wiring to the dashboard; `--non-interactive` skips prompts |
| `hopclaw dashboard` | Print the dashboard URL | Alias: `hopclaw console`; use `--open` to launch the browser; `--remote <name-or-url>` selects a named or explicit remote/local runtime |

Common startup examples:

```bash
hopclaw setup
hopclaw onboard --web-first
hopclaw serve
hopclaw serve --name local-dev
hopclaw dashboard --open
```

## Runtime And Dashboard Commands

### `agents`

Manage named agent profiles.

| Command | What it does | Key flags |
| --- | --- | --- |
| `hopclaw agents list` | List agent profiles | `--json` supported |
| `hopclaw agents get <name>` | Show one profile |  |
| `hopclaw agents add <name>` | Add a profile | `--model`, `--system-prompt`, `--description`, `--tools`, `--skills`, `--max-tokens` |
| `hopclaw agents delete <name>` | Delete a profile |  |
| `hopclaw agents bind <name>` | Bind an agent to a channel | `--channel` required, `--session-key` optional |

Examples:

```bash
hopclaw agents add reviewer --model gpt-4.1-mini --tools web.fetch,web.search
hopclaw agents bind reviewer --channel slack --session-key incident-42
```

### `approvals`

Inspect and resolve approval requests.

| Command | What it does | Key flags |
| --- | --- | --- |
| `hopclaw approvals list` | List approval requests | `--status pending|approved|denied|cancelled` |
| `hopclaw approvals get <id>` | Show approval details |  |
| `hopclaw approvals approve <id>` | Approve a request | `--note` |
| `hopclaw approvals deny <id>` | Deny a request | `--note` |

Examples:

```bash
hopclaw approvals list --status pending
hopclaw approvals approve apr_123 --note "allowed for this run"
```

### `browser`

Manage browser helper sessions.

| Command | What it does | Key flags |
| --- | --- | --- |
| `hopclaw browser sessions` | List browser sessions |  |
| `hopclaw browser open <url>` | Open a new browser session |  |
| `hopclaw browser tabs <session-id>` | List tabs for one session |  |
| `hopclaw browser screenshot <session-id>` | Capture a screenshot | `--output` |
| `hopclaw browser close <id>` | Close a browser session |  |
| `hopclaw browser status` | Show browser capability status |  |

### `devices`

Pair or launch local helper binaries.

Alias: `hopclaw device`.

| Command | What it does | Key flags |
| --- | --- | --- |
| `hopclaw devices pair <desktopd\|browserd>` | Create a pairing code and print the helper launch command | `--gateway-url`, `--auth-token`, `--device-id`, `--name`, `--platform`, `--family` |
| `hopclaw devices launch <desktopd\|browserd>` | Launch a paired helper locally | `--gateway-url`, `--pairing-code`, `--device-id`, `--device-name`, `--store-dir`, `--listen`, `--auth-token`, `--print` |

### `health`

`hopclaw health` performs a gateway health check and exits with a non-zero status when the gateway is unhealthy.

```bash
hopclaw health
hopclaw --json health
```

### `memory`

Manage the runtime memory store.

| Command | What it does | Key flags |
| --- | --- | --- |
| `hopclaw memory get <key>` | Read one memory value |  |
| `hopclaw memory set <key> <value>` | Write one memory value |  |
| `hopclaw memory delete <key>` | Remove one memory value |  |
| `hopclaw memory search <query>` | Search memory entries |  |
| `hopclaw memory list` | List entries | `--limit` |
| `hopclaw memory status` | Show memory store status |  |
| `hopclaw memory index` | Trigger reindexing | `--force` |

### `message`

Submit prompts and inspect message/run history without entering the REPL.

| Command | What it does | Key flags |
| --- | --- | --- |
| `hopclaw message send <message>` | Submit a message and wait for completion | `--session-key`, `--channel` |
| `hopclaw message list` | List recent runs | `--session-key`, `--limit` |
| `hopclaw message read <session-id>` | Read all messages in a session |  |
| `hopclaw message edit <run-id>` | Edit a stored message | `--content` |
| `hopclaw message delete <run-id>` | Delete a message/run |  |
| `hopclaw message search <query>` | Search messages across runs | `--limit`, `--session-key` |
| `hopclaw message broadcast` | Broadcast a message to multiple channels | `--channels`, `--content` |
| `hopclaw message react <run-id>` | React to a message | `--emoji` |
| `hopclaw message thread <session-id>` | Reply in an existing session thread | `--content` |

Examples:

```bash
hopclaw message send --session-key demo "Summarize the current status."
hopclaw message list --limit 10
hopclaw message search error --session-key incident-42
```

### `models`

Inspect configured model providers and manage mutable provider config through the operator API.

| Command | What it does | Key flags |
| --- | --- | --- |
| `hopclaw models list` | List configured providers |  |
| `hopclaw models router` | Show effective router profiles |  |
| `hopclaw models status` | Show gateway model status |  |
| `hopclaw models info <provider>` | Show provider details |  |
| `hopclaw models test [provider]` | Send a test probe |  |
| `hopclaw models bench` | Benchmark provider latency |  |
| `hopclaw models add <name>` | Add a model provider | `--catalog-provider`, `--api`, `--set`, `--interactive` |
| `hopclaw models update <name>` | Update a provider | `--catalog-provider`, `--api`, `--set`, `--interactive`, `--clear` |
| `hopclaw models delete <name>` | Delete a mutable provider |  |
| `hopclaw models validate <name>` | Validate provider connectivity | `--catalog-provider`, `--api`, `--set`, `--interactive` |
| `hopclaw models test-chat <name>` | Send a temporary chat through a provider | `--message` required; also accepts schema-driven provider flags |

Examples:

```bash
hopclaw models list
hopclaw models validate openai
hopclaw models add local-ollama --api ollama --set base_url=http://127.0.0.1:11434/v1 --set default_model=llama3.3
hopclaw models test-chat local-ollama --message "hello"
```

### `nodes`

Inspect cluster nodes.

| Command | What it does |
| --- | --- |
| `hopclaw nodes list` | List known cluster nodes |
| `hopclaw nodes status <address>` | Show node status |
| `hopclaw nodes ping <address>` | Ping one node |

### `remote`

Manage local and remote connections for the interactive client.

| Command | What it does | Key flags |
| --- | --- | --- |
| `hopclaw remote list` | List live local runtimes, saved remotes, and the builtin local entry | `--json` supported |
| `hopclaw remote get <name>` | Show one connection definition | `--json` supported |
| `hopclaw remote add [name] [url]` | Save a named remote; if fields are missing, HopClaw prompts for them | `--auth none|bearer`, `--token`, `--token-env`, `--insecure` |
| `hopclaw remote login <name>` | Save or update credentials for a remote; if no flags are given, HopClaw prompts for a bearer token | `--token`, `--token-env` |
| `hopclaw remote logout <name>` | Clear saved credentials for a remote |  |
| `hopclaw remote remove <name>` | Remove one saved remote |  |
| `hopclaw remote test <name-or-url>` | Probe connection health through `/healthz` | `--json` supported |

Examples:

```bash
hopclaw remote add
hopclaw remote add prod https://prod.example.com --auth bearer
hopclaw remote login prod --token-env HOPCLAW_PROD_TOKEN
hopclaw remote list
hopclaw remote test prod
hopclaw --remote prod
hopclaw dashboard --remote prod --open
```

Notes:

- A remote can be saved with `--auth bearer` before credentials are configured. In that state, `remote list` shows `login required`.
- `hopclaw remote login <name>` is the normal follow-up step after saving a protected remote.

### `pairing`

Manage channel pairing records.

| Command | What it does | Key flags |
| --- | --- | --- |
| `hopclaw pairing list` | List pairing records |  |
| `hopclaw pairing initiate <channel> <user-id>` | Create or refresh a pairing code | `--name` |
| `hopclaw pairing verify <code>` | Verify a pairing code |  |
| `hopclaw pairing revoke <channel> <user-id>` | Revoke a pairing record |  |

### `qr`

Generate QR-style connection URLs.

| Command | What it does | Key flags |
| --- | --- | --- |
| `hopclaw qr generate` | Generate a pairing URL | `--session`, `--channel` |
| `hopclaw qr show` | Show the gateway connection URL |  |

### `sessions`

Inspect runtime sessions.

| Command | What it does |
| --- | --- |
| `hopclaw sessions list` | List sessions |
| `hopclaw sessions get <id>` | Show one session, including messages when available |

### `status`

`hopclaw status` queries `/operator/status` and prints a compact gateway summary.

```bash
hopclaw status
hopclaw --json status
```

### `tui`

`hopclaw tui` launches the legacy gateway monitor TUI. The default `hopclaw` command is the primary interactive terminal.

## Automation And Integration Commands

### `automation`

Inspect scheduled jobs, hooks, wakeups, and watches.

Alias: `hopclaw cron`.

| Command | What it does | Key flags |
| --- | --- | --- |
| `hopclaw automation list` | List automation items | `--kind cron|wakeup|watch|hook` |
| `hopclaw automation inspect <kind> <id>` | Show one automation item |  |
| `hopclaw automation recent <kind> <id>` | Show recent executions |  |
| `hopclaw automation templates` | List starter templates | `--kind` |
| `hopclaw automation pause <kind> <id>` | Disable one automation item |  |
| `hopclaw automation resume <kind> <id>` | Re-enable one automation item |  |
| `hopclaw automation create` | Create a cron job | `--name`, `--schedule-kind`, `--expression` or `--every` or `--at`, `--content`, optional `--model`, `--session-key`, `--timezone`, `--disabled` |
| `hopclaw automation delete <id>` | Delete a cron job |  |
| `hopclaw automation trigger <id>` | Trigger a cron job now | Alias: `run` |
| `hopclaw automation status` | Show automation service status |  |

Examples:

```bash
hopclaw automation list --kind cron
hopclaw automation create \
  --name morning-brief \
  --schedule-kind cron \
  --expression "0 9 * * 1-5" \
  --timezone Asia/Shanghai \
  --content "Post the daily briefing."
hopclaw automation trigger cron_123
```

### `channels`

Inspect and manage configured channels.

| Command | What it does | Key flags |
| --- | --- | --- |
| `hopclaw channels list` | List configured channels |  |
| `hopclaw channels status [name]` | Show channel health |  |
| `hopclaw channels validate <name>` | Reconnect and validate a channel |  |
| `hopclaw channels test <name>` | Send a test message | `--message`, `--target` |
| `hopclaw channels add <type>` | Add a channel adapter | `--name`, `--set field=value`, `--interactive`, `--disabled`, `--token` |
| `hopclaw channels remove <name>` | Remove a channel adapter |  |
| `hopclaw channels logs <name>` | Show recent channel-related runtime events | `--limit` |

Examples:

```bash
hopclaw channels add slack --interactive
hopclaw channels add matrix \
  --set homeserver=https://matrix.example.com \
  --set user_id=@hopclaw:example.com \
  --set access_token=env:MATRIX_ACCESS_TOKEN
hopclaw channels validate slack
hopclaw channels test slack --message "HopClaw test message"
```

Plugin-backed stdio channels use the runtime name `plugin:<channel-key>` rather
than a built-in YAML key. Install or enable the plugin first, then validate and
test it like any other channel:

```bash
hopclaw plugins enable hello-channel
hopclaw channels validate plugin:hello-channel
hopclaw channels test plugin:hello-channel --target demo-room --message "HopClaw stdio test message"
```

### `hooks`

Inspect and debug runtime hooks.

| Command | What it does | Key flags |
| --- | --- | --- |
| `hopclaw hooks events` | List supported hook events |  |
| `hopclaw hooks list` | List registered hooks |  |
| `hopclaw hooks inspect <id>` | Inspect one hook | Alias: `get` |
| `hopclaw hooks recent <id>` | Show recent executions | Alias: `results` |
| `hopclaw hooks test-fire <id>` | Fire a hook manually | Alias: `fire`; `--trigger`, `--phase`, `--payload` |
| `hopclaw hooks replay <id>` | Replay the latest hook payload |  |
| `hopclaw hooks errors <id>` | Show clustered recent hook errors |  |
| `hopclaw hooks delete <id>` | Delete a hook |  |

### `plugins`

Manage plugin packages and create local plugin starters.

Alias: `hopclaw plugin`.

| Command | What it does | Key flags |
| --- | --- | --- |
| `hopclaw plugins init <name>` | Scaffold a local plugin starter | `--kind tool|channel|provider|skill`, `--dir` |
| `hopclaw plugins list` | List installed plugins |  |
| `hopclaw plugins info <name>` | Show plugin details and components |  |
| `hopclaw plugins enable <name>` | Enable a plugin |  |
| `hopclaw plugins disable <name>` | Disable a plugin |  |
| `hopclaw plugins install <name-or-url>` | Install a plugin | accepts a plugin name or source URL |
| `hopclaw plugins uninstall <name>` | Uninstall a plugin |  |
| `hopclaw plugins validate <path>` | Validate a manifest on disk | accepts a plugin directory or manifest file path |

Examples:

```bash
hopclaw plugins init hello-tool --kind tool --dir /tmp
hopclaw plugins validate /tmp/hello-tool
hopclaw plugins list
```

### `skills`

Manage discovered skills and installable catalog entries.

| Command | What it does | Key flags |
| --- | --- | --- |
| `hopclaw skills list` | List discovered skills |  |
| `hopclaw skills search <query>` | Search the skill catalog |  |
| `hopclaw skills info <name>` | Show installed and catalog info |  |
| `hopclaw skills install <name-or-path>` | Install from the catalog or a local skill directory | `--version` |
| `hopclaw skills remove <name>` | Remove an installed skill | Aliases: `delete`, `uninstall` |

Examples:

```bash
hopclaw skills list
hopclaw skills search summarize
hopclaw skills install summarize
hopclaw skills install ./skills/my-skill
```

### `tools`

Inspect runtime tools visible to the gateway.

| Command | What it does | Key flags |
| --- | --- | --- |
| `hopclaw tools list` | List tools | `--session-key` |
| `hopclaw tools search <query>` | Search by name or description | `--session-key` |
| `hopclaw tools info <name>` | Show one tool | `--session-key` |
| `hopclaw tools check [name]` | Check tool availability | `--session-key` |

### `webhooks`

Manage outbound or callback webhooks.

| Command | What it does | Key flags |
| --- | --- | --- |
| `hopclaw webhooks list` | List webhooks |  |
| `hopclaw webhooks create` | Create a webhook | `--url`, `--events`, `--secret` |
| `hopclaw webhooks delete <id>` | Delete a webhook |  |
| `hopclaw webhooks test <id>` | Send a test event |  |
| `hopclaw webhooks info <id>` | Show webhook details |  |

## Config, Ops, And Maintenance Commands

### `backup`

| Command | What it does |
| --- | --- |
| `hopclaw backup create` | Create a timestamped backup archive |
| `hopclaw backup list` | List backup archives |
| `hopclaw backup restore <path>` | Restore from an archive |

### `bug-report`

Generate a local support bundle.

Useful flags:

| Flag | Meaning |
| --- | --- |
| `-o`, `--output` | Write the ZIP bundle to a specific path |
| `--include-logs` | Include redacted local logs |
| `--submit` | Submit the bundle to the configured diagnostics collector |
| `--submit-url` | Override the upload URL |
| `--submit-token` | Override the upload bearer token |

Example:

```bash
hopclaw bug-report --output /tmp/hopclaw-bug-report.zip
```

### `completion`

Generate shell completion scripts:

```bash
hopclaw completion bash > ./hopclaw.bash
hopclaw completion zsh > ./_hopclaw
hopclaw completion fish > ./hopclaw.fish
hopclaw completion powershell > ./hopclaw.ps1
```

### `config`

Manage the active YAML config.

| Command | What it does |
| --- | --- |
| `hopclaw config path` | Print the active config file path |
| `hopclaw config show` | Print the fully resolved config |
| `hopclaw config get <key>` | Read a dotted-path value |
| `hopclaw config set <key> <value>` | Write a dotted-path value |
| `hopclaw config unset <key>` | Remove a dotted-path value |
| `hopclaw config validate` | Validate the active config |
| `hopclaw config edit` | Open the config in your editor |

### `daemon`

Manage the HopClaw background service.

| Command | What it does |
| --- | --- |
| `hopclaw daemon install` | Install the system service |
| `hopclaw daemon uninstall` | Remove the system service |
| `hopclaw daemon start` | Start the service |
| `hopclaw daemon stop` | Stop the service |
| `hopclaw daemon restart` | Restart the service |
| `hopclaw daemon status` | Show service status |

### `dns`

Manage peer discovery and DNS-style setup.

| Command | What it does | Key flags |
| --- | --- | --- |
| `hopclaw dns status` | Show discovery status |  |
| `hopclaw dns discover` | Discover peers | `--timeout` |
| `hopclaw dns setup` | Configure discovery settings | `--tailscale`, `--static` |

### `doctor`

Run diagnostics end to end or per section.

| Command | What it does |
| --- | --- |
| `hopclaw doctor` | Run every doctor section |
| `hopclaw doctor auth` | Check auth configuration |
| `hopclaw doctor config` | Check config validity and update policy |
| `hopclaw doctor connectivity` | Check gateway, providers, and channels |
| `hopclaw doctor skills` | Check skill directories and dependencies |
| `hopclaw doctor storage` | Check state dir, databases, and disk space |
| `hopclaw doctor security` | Check secret exposure and file permissions |
| `hopclaw doctor platform` | Check Go, daemon, Docker, and platform notes |

All `doctor` commands accept:

```bash
hopclaw doctor --fix
```

### `logs`

| Command | What it does | Key flags |
| --- | --- | --- |
| `hopclaw logs list` | List recent events | `--limit`, `--type` |
| `hopclaw logs stream` | Stream live events over SSE |  |

### `sandbox`

Run commands in the gateway sandbox and inspect sandbox readiness.

| Command | What it does | Key flags |
| --- | --- | --- |
| `hopclaw sandbox run -- <command>` | Execute a command inside a sandbox container | `--image`, `--stdin`, `--env`, `--timeout` |
| `hopclaw sandbox images` | List allowed images |  |
| `hopclaw sandbox status` | Show Docker/sandbox availability |  |

Examples:

```bash
hopclaw sandbox run -- echo hello
hopclaw sandbox run --image python:3.12-slim -- python -c "print('hello')"
hopclaw sandbox status
```

### `secrets`

Manage keychain-backed secrets.

| Command | What it does |
| --- | --- |
| `hopclaw secrets set <key> <value>` | Store a secret in the keychain |
| `hopclaw secrets get <key>` | Read a secret from the keychain |
| `hopclaw secrets delete <key>` | Delete a secret |
| `hopclaw secrets list` | List stored secret keys only |

### `security`

Audit local security posture and rotate credentials.

| Command | What it does | Key flags |
| --- | --- | --- |
| `hopclaw security audit` | Audit config file permissions, auth, secrets, state dir, logs, and TLS | `--fix` |
| `hopclaw security rotate` | Rotate security credentials | `--auth-token` |

### `uninstall`

Remove HopClaw from the current machine.

Useful flags:

| Flag | Meaning |
| --- | --- |
| `--yes` | Skip the confirmation prompt |
| `--keep-data` | Preserve `~/.hopclaw` while removing binaries and service registrations |

Example:

```bash
hopclaw uninstall --yes --keep-data
```

### `update`

Check GitHub releases for new versions.

Useful flags:

| Flag | Meaning |
| --- | --- |
| `--check` | Only perform the check; skip the longer upgrade guidance |
| `--channel stable\|beta\|nightly` | Override the release channel for this check |
| `--version` | Compatibility flag; advisory mode still reports the latest visible release |
| `--no-restart`, `--yes` | Compatibility flags with no effect in advisory mode |

Examples:

```bash
hopclaw update
hopclaw update --check --channel beta
hopclaw --json update
```

### `version`

Print build and platform information.

```bash
hopclaw version
hopclaw --json version
```

## Practical Recipes

### Start locally and verify

```bash
hopclaw serve
hopclaw health
hopclaw status
hopclaw dashboard --open
```

### Submit one prompt without entering the REPL

```bash
hopclaw message send --session-key ops "Summarize the current gateway status."
```

### Validate config, auth, and connectivity after an edit

```bash
hopclaw config validate
hopclaw doctor auth
hopclaw doctor connectivity
```

### Inspect channels and send a smoke-test message

```bash
hopclaw channels list
hopclaw channels status
hopclaw channels validate slack
hopclaw channels test slack --message "HopClaw CLI smoke test"
hopclaw channels validate plugin:hello-channel
```

## 中文同步

### TL;DR

- `hopclaw` 不带子命令时会进入交互式 REPL。
- 想明确指定配置文件时，用 `hopclaw --config ./config.yaml <command>`。
- 需要脚本消费输出时，优先加 `--json`。
- `hopclaw update` 只做检查和提示，不会自动替换本机二进制。
- `stdio` 插件渠道在运行时用 `plugin:<channel-key>` 命名，而不是普通 `channels.<id>`。

### 常用入口

- 首次配置：`hopclaw setup` 或 `hopclaw onboard --web-first`
- 启动网关：`hopclaw serve`
- 打开控制台：`hopclaw dashboard --open`
- 发送一次性消息：`hopclaw message send --session-key demo "hello"`
- 全量诊断：`hopclaw doctor`

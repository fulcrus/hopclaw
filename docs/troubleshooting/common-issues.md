# Common Issues

## TL;DR

- If HopClaw cannot find a config file, run `hopclaw setup` or point `--config` at the file you want.
- If the gateway is not reachable, start it with `hopclaw serve` and verify with `hopclaw health` and `hopclaw status`.
- If models, channels, or skills look configured but do not work, `hopclaw doctor` is the fastest way to narrow the failure domain.
- If a channel is connected but silent, re-check policy fields such as `require_mention`, `group_policy`, and `reply_in_thread`.

English is canonical in this file. 中文同步 follows after the English section.

## First-Line Triage Commands

Run these before deeper debugging:

```bash
hopclaw config validate
hopclaw health
hopclaw status
hopclaw doctor
```

If you only need one area:

```bash
hopclaw doctor auth
hopclaw doctor config
hopclaw doctor connectivity
hopclaw doctor skills
hopclaw doctor storage
hopclaw doctor security
hopclaw doctor platform
```

## Symptom Index

| Symptom | Most likely cause | First command to run |
| --- | --- | --- |
| `no config file found` | No discovered config file and no supported provider env vars | `hopclaw setup` |
| `gateway is not running` or `UNHEALTHY` | The gateway has not started or is bound to the wrong address | `hopclaw health` |
| Config validation fails | YAML syntax issue or invalid config combination | `hopclaw config validate` |
| Provider requests fail | Missing credentials or bad provider settings | `hopclaw doctor auth` and `hopclaw doctor connectivity` |
| Channel is configured but does not answer | Bad credentials, health issues, or restrictive policy fields | `hopclaw channels status` |
| Skill is discovered but not ready | Missing binaries, env vars, or unsupported OS | `hopclaw doctor skills` |
| Sandbox execution fails | Docker or the default image is missing | `hopclaw sandbox status` |
| Plugin is ignored or fails validation | Bad manifest or invalid plugin layout | `hopclaw plugins validate .` |
| Operator endpoints return auth errors | Missing or mismatched auth config | `hopclaw doctor auth` |
| Daemon behaves strangely | Service installed state and runtime state are out of sync | `hopclaw daemon status` and `hopclaw doctor platform` |

## 1. “No Config File Found”

Typical messages:

- `no config file found`
- `no config file found; run 'hopclaw setup' first`

Fix:

```bash
hopclaw setup
hopclaw config path
hopclaw config validate
```

If you already have a file elsewhere:

```bash
hopclaw --config ./local.yaml config validate
hopclaw --config ./local.yaml serve
```

If you prefer an env-driven first boot, set a supported provider key first:

```bash
export OPENAI_BASE_URL=https://api.openai.com/v1
export OPENAI_API_KEY=your-api-key
export OPENAI_MODEL=gpt-4.1-mini
hopclaw serve
```

## 2. The Gateway Does Not Start Or Is Not Reachable

Symptoms:

- `hopclaw health` reports `UNHEALTHY`
- `hopclaw status` says the gateway is not running
- `hopclaw doctor connectivity` warns that the gateway is not reachable

Checklist:

```bash
hopclaw serve
hopclaw health
hopclaw status
hopclaw doctor connectivity
```

If a different process already owns the port, `doctor connectivity` will flag the conflict and suggest changing `server.address` or stopping the conflicting process.

If you use the service manager:

```bash
hopclaw daemon status
hopclaw daemon start
hopclaw doctor platform
```

## 3. Config Validation Fails

Symptoms:

- `hopclaw config validate` fails
- `hopclaw doctor config` shows config file or syntax failures

Start here:

```bash
hopclaw config validate
hopclaw doctor config
```

Common causes:

- invalid YAML indentation or scalar formatting
- deprecated keys that should move into `auth.*`
- invalid combinations such as multiple providers without an unambiguous default model path

If you use multiple providers, make the intent explicit:

```yaml
agent:
  default_model: "openai/gpt-4.1-mini"

models:
  default_provider: openai
```

## 4. Authentication Or Operator Access Fails

Symptoms:

- operator endpoints return `401` or `403`
- `doctor auth` reports no authentication in a production profile
- the dashboard loads but operator actions fail

Check the auth section:

```bash
hopclaw doctor auth
hopclaw doctor security
```

A simple bearer-token setup:

```yaml
auth:
  bearer_token: env:HOPCLAW_AUTH_TOKEN
```

And then:

```bash
export HOPCLAW_AUTH_TOKEN=change-me
hopclaw serve
```

Avoid raw secrets in YAML when possible. Use `env:` or `keychain:` references instead.

## 5. Model Provider Is Configured But Calls Still Fail

Symptoms:

- `message send` fails after submission
- `models test` or `models validate` fails
- `doctor connectivity` reports provider reachability problems

Checklist:

```bash
hopclaw doctor auth
hopclaw doctor connectivity
hopclaw models list
hopclaw models info <provider>
hopclaw models validate <provider>
hopclaw models test <provider>
```

Things to verify:

- the provider has credentials
- `base_url` is correct
- `default_model` exists on that provider
- network egress is available from the machine running HopClaw

## 6. Channel Is Connected But The Bot Does Not Reply

Symptoms:

- `channels list` shows configured
- `channels status` shows warnings or looks healthy, but messages do not trigger runs

Check both health and policy:

```bash
hopclaw channels list
hopclaw channels status
hopclaw channels validate <channel-name>
hopclaw channels test <channel-name> --message "HopClaw smoke test"
hopclaw doctor connectivity
```

Then re-check these fields in config:

- `dm_policy`
- `group_policy`
- `require_mention`
- `group_session_scope`
- `reply_in_thread`

Example of a restrictive configuration that often surprises operators:

```yaml
channels:
  telegram:
    enabled: true
    bot_token: env:TELEGRAM_BOT_TOKEN
    group_policy: allowlist
    require_mention: true
```

That is valid, but group messages without a mention will not trigger the runtime.

## 7. Skills Are Discovered But Not Ready

Symptoms:

- `hopclaw skills list` shows a skill, but it is not usable
- `hopclaw doctor skills` reports missing dependencies

Run:

```bash
hopclaw doctor skills
hopclaw skills info <skill-name>
```

Typical causes:

- missing external CLI dependencies
- missing environment variables
- unsupported operating system
- skill directories configured in `skills.dirs` do not exist

If you maintain local skill paths, verify them:

```bash
hopclaw doctor skills
```

## 8. Sandbox Commands Fail

Symptoms:

- `hopclaw sandbox run ...` fails immediately
- `doctor platform` warns about Docker or the default image

Check:

```bash
hopclaw sandbox status
hopclaw sandbox images
hopclaw doctor platform
```

The default doctor hint expects this image to exist locally:

```bash
docker pull python:3.12-slim
```

Then retry:

```bash
hopclaw sandbox run --image python:3.12-slim -- python -c "print('hello')"
```

## 9. Plugin Manifest Validation Fails Or The Plugin Is Not Discovered

Symptoms:

- `plugins validate` reports errors
- the plugin directory exists but does not appear in `hopclaw plugins list`

Validate it on disk first:

```bash
hopclaw plugins validate .
```

Common causes:

- missing `name` in the manifest
- invalid `version` formatting
- provider declarations missing `api`
- manifest path is neither a plugin directory nor a manifest file
- `skills_dir` or `hooks_dir` escapes the plugin root

Use the scaffold if you want a known-good baseline:

```bash
hopclaw plugins init hello-tool --kind tool --dir /tmp
hopclaw plugins validate /tmp/hello-tool
```

## 10. Service Or Uninstall Behavior Is Confusing

If the machine-level service does not behave as expected:

```bash
hopclaw daemon status
hopclaw doctor platform
```

If you want to remove the service but keep local state:

```bash
hopclaw uninstall --yes --keep-data
```

If you want a clean uninstall:

```bash
hopclaw uninstall --yes
```

## 11. Secret Exposure Warnings

Symptoms:

- `doctor security` reports literal secrets
- `security audit` warns that config values look like raw tokens

Check:

```bash
hopclaw doctor security
hopclaw security audit
```

Prefer this:

```yaml
auth:
  bearer_token: env:HOPCLAW_AUTH_TOKEN
```

Or this:

```yaml
channels:
  slack:
    bot_token: keychain:slack-bot-token
```

Avoid this:

```yaml
auth:
  bearer_token: "my-plain-token"
```

## Escalate With A Support Bundle

When you need to share a reproducible state snapshot:

```bash
hopclaw bug-report --output /tmp/hopclaw-bug-report.zip
```

For a broad system picture before filing an issue:

```bash
hopclaw doctor
hopclaw --json status
hopclaw --json health
```

## 中文同步

### TL;DR

- 找不到配置文件就先跑 `hopclaw setup`，或者用 `--config` 指到正确文件。
- 网关不可达时先跑 `hopclaw serve`，再用 `hopclaw health` 和 `hopclaw status` 验证。
- 模型、渠道、技能看起来“配了但不能用”时，最快的是先跑 `hopclaw doctor`。
- 渠道“连上但不回复”时，先检查 `require_mention`、`group_policy`、`reply_in_thread` 这些策略字段。

### 最常用排障命令

```bash
hopclaw config validate
hopclaw health
hopclaw status
hopclaw doctor
hopclaw channels status
hopclaw doctor skills
hopclaw sandbox status
hopclaw plugins validate .
```


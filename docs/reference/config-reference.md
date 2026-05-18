# Config Reference

## TL;DR

- HopClaw auto-discovers config from `$HOPCLAW_CONFIG`, `./.hopclaw/config.yaml`, `~/.hopclaw/config.yaml`, and `/etc/hopclaw/config.yaml`.
- YAML values support `${ENV_VAR}` expansion before parsing.
- Secret-bearing fields can safely point to `env:NAME` or `keychain:NAME` instead of storing raw secrets inline.
- This page focuses on the system and operator sections of the configuration, and shows where they sit under the full root.

English is canonical in this file. 中文同步 follows after the English section.

## Where The Config Lives

HopClaw discovers config in this order:

```text
$HOPCLAW_CONFIG
./.hopclaw/config.yaml
~/.hopclaw/config.yaml
/etc/hopclaw/config.yaml
```

Inspect the active file with:

```bash
hopclaw config path
hopclaw config show
hopclaw config validate
```

## Parsing Rules

### Environment Expansion

HopClaw runs `os.ExpandEnv` before YAML unmarshalling, so this works:

```yaml
models:
  openai_compat:
    base_url: ${OPENAI_BASE_URL}
    api_key: ${OPENAI_API_KEY}
    model: ${OPENAI_MODEL}
```

### Secret References

Many secret-bearing fields can safely use symbolic references instead of literal values:

```yaml
auth:
  bearer_token: env:HOPCLAW_AUTH_TOKEN

authz:
  webhook:
    url: https://policy.example.com/hopclaw/authz/decide

channels:
  slack:
    bot_token: keychain:slack-bot-token
    app_token: env:SLACK_APP_TOKEN
```

Recognized forms:

| Form | Meaning |
| --- | --- |
| `env:NAME` | Resolve from an environment variable |
| `keychain:NAME` | Resolve from the platform keychain or secret store |
| literal text | Stored as-is; use sparingly for secrets |

### Duration Values

Every `time.Duration` field uses Go duration syntax:

```yaml
5s
30s
5m
1h
24h
```

## Full Root Layout

The root configuration spans several sections. This reference covers the
system and operator sections; runtime, store, agent, model, tool, channel,
host, plugin, and skill sections are documented in their dedicated guides.

| Root key | Covered here |
| --- | --- |
| `server` | No (see runtime configuration) |
| `auth` | Yes |
| `authz` | Yes |
| `store` | No (see runtime configuration) |
| `agent` | No (see runtime configuration) |
| `runtime` | No (see runtime configuration) |
| `update` | Yes |
| `diagnostics` | Yes |
| `skills` | No (see skills guides) |
| `models` | No (see provider guides) |
| `tools` | No (see tool reference) |
| `hosts` | No (see capability host docs) |
| `channels` | No (see channel guides) |
| `plugins` | No (see plugin SDK guide) |
| `cron` | Yes |
| `watch` | Yes |
| `heartbeat` | Yes |
| `wire` | Yes |
| `wakeup` | Yes |
| `allowlist` | Yes |
| `sandbox` | Yes |
| `isolation` | Yes |
| `tunnel` | Yes |
| `exec_approval` | Yes |
| `channel_health` | Yes |
| `embedding` | Yes |
| `security` | Yes |
| `discovery` | Yes |
| `canvas` | Yes |
| `logging` | Yes |
| `usage_storage` | No |
| `memory_storage` | No |
| `locale` | No |

## Minimal System-Ops Example

```yaml
update:
  enabled: true
  check_on_start: true
  check_interval: 24h
  channel: stable

diagnostics:
  enabled: true
  bug_report_dir: .hopclaw/bug-reports
  include_logs: true

heartbeat:
  interval: 30s
  timeout: 2m

sandbox:
  enabled: true
  image: python:3.12-slim
  timeout: 30

auth:
  bearer_token: env:HOPCLAW_AUTH_TOKEN
authz:
  fallback: deny
  webhook:
    url: https://policy.example.com/hopclaw/authz/decide
  rbac:
    mode: static
    default_role: viewer
```

## `update`

Controls release checks and update-channel policy.

Defaults are listed in the table below.

| Key | Type | Default | Notes |
| --- | --- | --- | --- |
| `update.enabled` | bool | `true` | Master switch for update checks |
| `update.check_on_start` | bool | `true` | Run a check on startup |
| `update.check_interval` | duration | `24h` | Background check interval |
| `update.channel` | string | `stable` | Release channel: `stable`, `beta`, or `nightly` |
| `update.manifest_url` | string | release manifest URL | Override the manifest endpoint |
| `update.skip_version` | string | none | Ignore a specific version |

Example:

```yaml
update:
  enabled: true
  check_on_start: true
  check_interval: 24h
  channel: beta
```

## `diagnostics`

Controls bug-report creation, telemetry, and collector uploads.

| Key | Type | Default | Notes |
| --- | --- | --- | --- |
| `diagnostics.enabled` | bool | none | Enable diagnostics features |
| `diagnostics.bug_report_dir` | string | none | Where redacted bug-report bundles are written |
| `diagnostics.include_logs` | bool | none | Include local logs in bug reports |
| `diagnostics.max_log_bytes` | int64 | none | Per-report log size cap |
| `diagnostics.redact_patterns` | string list | none | Extra patterns to redact from bundles |
| `diagnostics.telemetry_enabled` | bool | none | Enable telemetry upload |
| `diagnostics.telemetry_endpoint` | string | none | Telemetry receiver URL |
| `diagnostics.telemetry_token` | string | none | Bearer token or secret ref |
| `diagnostics.telemetry_timeout` | duration | none | Telemetry request timeout |
| `diagnostics.telemetry_debug_log` | bool | none | Print extra telemetry debug logs |
| `diagnostics.telemetry_collector_enabled` | bool | none | Enable a telemetry collector uploader |
| `diagnostics.telemetry_collector_dir` | string | none | Local collector spool directory |
| `diagnostics.telemetry_collector_auth_token` | string | none | Collector auth token or ref |
| `diagnostics.telemetry_collector_max_upload_bytes` | int64 | none | Collector upload size cap |
| `diagnostics.crash_reports_enabled` | bool | none | Enable crash-report upload |
| `diagnostics.upload_url` | string | none | Crash or support bundle upload URL |
| `diagnostics.upload_token` | string | none | Upload token or ref |
| `diagnostics.upload_timeout` | duration | none | Upload timeout |
| `diagnostics.collector_enabled` | bool | none | Generic diagnostics collector switch |
| `diagnostics.collector_dir` | string | none | Generic collector spool directory |
| `diagnostics.collector_auth_token` | string | none | Generic collector auth token or ref |
| `diagnostics.collector_max_upload_bytes` | int64 | none | Generic collector upload size cap |

## `logging`

Controls local logging format, destination, and sampling.

| Key | Type | Default | Notes |
| --- | --- | --- | --- |
| `logging.level` | string | none | Typical values: `debug`, `info`, `warn`, `error` |
| `logging.format` | string | none | Text or JSON style, depending on the logger implementation |
| `logging.output` | string | none | Output target such as stdout or file |
| `logging.file_path` | string | none | File destination when logging to disk |
| `logging.max_size_mb` | int | none | Log rotation size hint |
| `logging.redact_keys` | string list | none | Extra keys to redact |
| `logging.subsystem_levels` | map[string]string | none | Per-subsystem log levels |
| `logging.console_capture` | bool | none | Capture console output into logs |
| `logging.sampling.enabled` | bool | none | Enable log sampling |
| `logging.sampling.initial_n` | int | none | Emit the first N messages |
| `logging.sampling.thereafter_n` | int | none | Emit every Nth message afterward |
| `logging.sampling.interval_sec` | int | none | Sampling interval in seconds |

## `security`

These settings define content-scanning and tool-safety policy.

`security.max_content_size` defaults to `10485760` bytes (`10 MiB`).

| Key | Type | Default | Notes |
| --- | --- | --- | --- |
| `security.allowed_paths` | string list | none | Explicit file-system allowlist |
| `security.blocked_domains` | string list | none | Domains to reject |
| `security.blocked_commands` | string list | none | Command names to reject |
| `security.max_content_size` | int64 | `10 MiB` | Maximum content size processed by security checks |
| `security.dangerous_tools` | string list | none | Tool names that require stronger policy |
| `security.custom_patterns[].name` | string | none | Pattern label |
| `security.custom_patterns[].pattern` | string | none | Pattern text or regex-like matcher |
| `security.custom_patterns[].severity` | string | none | Severity level |
| `security.custom_patterns[].category` | string | none | Category label |

## `embedding`

Configures the embedding provider used for memory or semantic indexing.

| Key | Type | Default | Notes |
| --- | --- | --- | --- |
| `embedding.enabled` | bool | none | Enable embedding features |
| `embedding.provider` | string | none | Selected provider name |
| `embedding.base_url` | string | none | Provider base URL |
| `embedding.api_key` | string | none | API key or secret ref |
| `embedding.model` | string | none | Embedding model ID |
| `embedding.fallback` | string | none | Fallback provider/model identifier |
| `embedding.cache_size` | int | none | Local embedding cache size |
| `embedding.providers[<name>].api` | string | none | Provider API type |
| `embedding.providers[<name>].base_url` | string | none | Provider base URL override |
| `embedding.providers[<name>].api_key` | string | none | Provider API key or ref |
| `embedding.providers[<name>].model` | string | none | Provider default embedding model |

## `cron`

Controls scheduled automation persistence.

| Key | Type | Default | Notes |
| --- | --- | --- | --- |
| `cron.enabled` | bool | none | Enable cron automation storage/service |
| `cron.store_path` | string | none | Cron state store path |
| `cron.execution_timeout` | duration | none | Per-job execution timeout |

## `watch`

Controls watch automation persistence.

| Key | Type | Default | Notes |
| --- | --- | --- | --- |
| `watch.enabled` | bool | none | Enable watch automation storage/service |
| `watch.store_path` | string | none | Watch state store path |
| `watch.execution_timeout` | duration | none | Per-watch execution timeout |

## `heartbeat`

Heartbeat settings for long-running services.

Defaults:

- `heartbeat.interval`: `30s`
- `heartbeat.timeout`: `2m`

| Key | Type | Default | Notes |
| --- | --- | --- | --- |
| `heartbeat.enabled` | bool | none | Enable heartbeat emission |
| `heartbeat.interval` | duration | `30s` | Heartbeat interval |
| `heartbeat.timeout` | duration | `2m` | Consider the service stale after this timeout |

## `wire`

Controls wire-level event retention and redaction.

Defaults:

- `wire.max_entries`: `1000`
- `wire.max_body_bytes`: `65536`
- `wire.retention_time`: `1h`

| Key | Type | Default | Notes |
| --- | --- | --- | --- |
| `wire.enabled` | bool | none | Enable wire capture |
| `wire.max_entries` | int | `1000` | Maximum retained entries |
| `wire.max_body_bytes` | int | `65536` | Per-entry body cap |
| `wire.retention_time` | duration | `1h` | Retention period |
| `wire.redact_headers` | string list | none | Headers to redact |
| `wire.providers` | string list | none | Provider allowlist for capture |

## `wakeup`

Controls wakeup task storage.

| Key | Type | Default | Notes |
| --- | --- | --- | --- |
| `wakeup.enabled` | bool | none | Enable wakeup scheduling |
| `wakeup.store_path` | string | none | Wakeup state store path |

## `allowlist`

Restricts which users or groups can interact with specific channels.

| Key | Type | Default | Notes |
| --- | --- | --- | --- |
| `allowlist.enabled` | bool | none | Enable allowlist enforcement |
| `allowlist.channels[].channel` | string | none | Channel identifier |
| `allowlist.channels[].allow_all` | bool | none | Allow every actor on that channel |
| `allowlist.channels[].allow_users` | string list | none | Explicitly allowed users |
| `allowlist.channels[].deny_users` | string list | none | Explicitly denied users |
| `allowlist.channels[].allow_groups` | string list | none | Explicitly allowed groups |
| `allowlist.channels[].deny_groups` | string list | none | Explicitly denied groups |

## `sandbox`

Controls containerized sandbox execution.

`sandbox.timeout` defaults to `30` seconds.

| Key | Type | Default | Notes |
| --- | --- | --- | --- |
| `sandbox.enabled` | bool | none | Enable sandbox execution |
| `sandbox.image` | string | none | Default container image |
| `sandbox.memory_limit` | string | none | Memory limit string |
| `sandbox.cpu_limit` | string | none | CPU limit string |
| `sandbox.timeout` | int | `30` | Default timeout in seconds |
| `sandbox.network_mode` | string | none | Container network mode |
| `sandbox.work_dir` | string | none | Container working directory |
| `sandbox.allowed_images` | string list | none | Explicit image allowlist |
| `sandbox.process_mode` | bool | none | Use process-style mode instead of container mode when supported |

## `isolation`

Controls lightweight filesystem isolation.

| Key | Type | Default | Notes |
| --- | --- | --- | --- |
| `isolation.enabled` | bool | none | Enable isolation mode |
| `isolation.base_dir` | string | none | Base directory used for isolated workspaces |

## `tunnel`

Controls reverse-tunnel style exposure.

| Key | Type | Default | Notes |
| --- | --- | --- | --- |
| `tunnel.enabled` | bool | none | Enable tunnel support |
| `tunnel.provider` | string | none | `"ssh"` or `"tailscale"` |
| `tunnel.host` | string | none | SSH remote host |
| `tunnel.port` | int | none | SSH remote port; comment notes `22` as the typical default |
| `tunnel.user` | string | none | SSH username |
| `tunnel.key_file` | string | none | SSH private key path |
| `tunnel.remote_host` | string | none | Remote bind host |
| `tunnel.remote_port` | int | none | Remote bind port |
| `tunnel.local_port` | int | none | Local port to expose |
| `tunnel.auth_token` | string | none | Provider token or secret ref |

## `discovery`

Controls peer/service discovery.

| Key | Type | Default | Notes |
| --- | --- | --- | --- |
| `discovery.enabled` | bool | none | Enable peer discovery |
| `discovery.method` | string | none | Discovery backend or method |
| `discovery.service` | string | none | Service name |
| `discovery.peers` | string list | none | Static peer list |
| `discovery.instance_name` | string | none | Advertised instance name |
| `discovery.port` | int | none | Discovery port |
| `discovery.interface` | string | none | Specific network interface |

## `canvas`

Controls the canvas-style UI or asset surface.

| Key | Type | Default | Notes |
| --- | --- | --- | --- |
| `canvas.enabled` | bool | none | Enable canvas features |
| `canvas.port` | int | none | Canvas listener port |
| `canvas.root` | string | none | Root directory or asset path |
| `canvas.live_reload` | bool | none | Enable live reload |
| `canvas.token_ttl` | duration | none | Token time-to-live |

## `exec_approval`

Controls approval providers used for exec or sensitive actions.

Defaults:

- `exec_approval.approval_timeout`: `5m`
- `exec_approval.grace_period`: `30s`
- `callback_auth.mode`: inferred as `hmac` when `secret` is present, otherwise `token`
- `callback_auth.header_name`: `X-HopClaw-Approval-Token`
- `callback_auth.signature_header`: `X-HopClaw-Signature`
- `callback_auth.timestamp_header`: `X-HopClaw-Timestamp`
- `callback_auth.max_age`: `5m`
- `webhook.timeout`: `15s`

| Key | Type | Default | Notes |
| --- | --- | --- | --- |
| `exec_approval.safe_patterns` | string list | none | Safe command or path patterns that can bypass stronger approval |
| `exec_approval.approval_timeout` | duration | `5m` | How long to wait for an approval |
| `exec_approval.grace_period` | duration | `30s` | Post-approval grace period |
| `exec_approval.providers[].name` | string | none | Provider instance name |
| `exec_approval.providers[].type` | string | none | Provider type, normalized to lowercase |
| `exec_approval.providers[].enabled` | bool | none | Enable or disable one provider |
| `exec_approval.providers[].callback_auth.mode` | string | inferred | `token` or `hmac` |
| `exec_approval.providers[].callback_auth.header_name` | string | `X-HopClaw-Approval-Token` | Token header name |
| `exec_approval.providers[].callback_auth.token` | string | none | Shared token or secret ref |
| `exec_approval.providers[].callback_auth.secret` | string | none | HMAC secret or secret ref |
| `exec_approval.providers[].callback_auth.signature_header` | string | `X-HopClaw-Signature` | HMAC signature header |
| `exec_approval.providers[].callback_auth.timestamp_header` | string | `X-HopClaw-Timestamp` | Timestamp header |
| `exec_approval.providers[].callback_auth.max_age` | duration | `5m` | Maximum timestamp age |
| `exec_approval.providers[].webhook.submit_url` | string | none | Submit endpoint |
| `exec_approval.providers[].webhook.update_url` | string | none | Update endpoint |
| `exec_approval.providers[].webhook.sync_url` | string | none | Sync endpoint |
| `exec_approval.providers[].webhook.timeout` | duration | `15s` | Webhook timeout |
| `exec_approval.providers[].webhook.headers` | map[string]string | none | Extra outbound headers |
| `exec_approval.providers[].webhook.secret` | string | none | Shared secret or secret ref |
| `exec_approval.providers[].metadata` | map[string]any | none | Provider-specific metadata |

## `channel_health`

Controls health monitoring for channels.

Defaults:

- `channel_health.check_interval`: `30s`
- `channel_health.stale_socket_timeout`: `5m`
- `channel_health.stuck_run_timeout`: `10m`
- `channel_health.startup_grace`: `30s`
- `channel_health.max_restarts_per_hour`: `5`

| Key | Type | Default | Notes |
| --- | --- | --- | --- |
| `channel_health.enabled` | bool | none | Enable channel health monitoring |
| `channel_health.check_interval` | duration | `30s` | Health polling interval |
| `channel_health.stale_socket_timeout` | duration | `5m` | Socket staleness threshold |
| `channel_health.stuck_run_timeout` | duration | `10m` | Run-stuck threshold |
| `channel_health.startup_grace` | duration | `30s` | Grace window after startup |
| `channel_health.max_restarts_per_hour` | int | `5` | Restart cap for runaway channels |

## `auth`

The modern auth section for operator access and API protection.

`server.auth_token` still exists elsewhere for backward compatibility, but the doctor and migration hints prefer `auth.*`.

### Top-Level Auth Keys

| Key | Type | Default | Notes |
| --- | --- | --- | --- |
| `auth.bearer_token` | string | none | Shared bearer token or secret ref |
| `auth.jwt` | object | none | JWT validation settings |
| `auth.api_keys` | array | none | Static API key entries |
| `auth.oauth2` | object | none | OAuth2/OIDC settings |
| `auth.session` | object | none | Cookie-session settings |
| `auth.rbac` | object | none | Role-based access control rules |

## `authz`

This section controls authorization strategy selection without changing core
code. It is especially useful for `hopclaw serve` and other `toB` deployments.

Automatic selection order when `authz.mode` is empty:

1. injected `Config.AuthorizationDecider`
2. configured `authz.webhook`
3. configured `auth.rbac` through `contrib/authz-rbac`
4. `authz.OpenDecider{}`

| Key | Type | Default | Notes |
| --- | --- | --- | --- |
| `authz.mode` | string | auto | Optional explicit selector. When set to `open`, `rbac`, or `webhook`, the gateway honors that strategy after any injected `Config.AuthorizationDecider`; when empty, the automatic order above applies |
| `authz.fallback` | string | none | Fallback when the webhook delegate fails: `open`, `rbac`, or `deny` |
| `authz.webhook.url` | string | none | External authorization decision endpoint |
| `authz.webhook.timeout` | duration | `5s` | HTTP timeout for policy requests |
| `authz.webhook.headers` | map[string]string | none | Extra outbound headers |
| `authz.webhook.secret` | string | none | Shared secret or secret ref; emits HopClaw signature headers |

### `auth.rbac`

| Key | Type | Default | Notes |
| --- | --- | --- | --- |
| `auth.rbac.mode` | string | none | RBAC mode selector |
| `auth.rbac.default_role` | string | none | Overrides the `contrib/authz-rbac` authenticated-caller fallback when no role, group, or scope match is found |
| `auth.rbac.scope_prefixes` | string list | none | Scope prefixes to map into permissions |
| `auth.rbac.role_metadata_keys` | string list | none | Metadata keys that can provide roles |
| `auth.rbac.group_metadata_keys` | string list | none | Metadata keys that can provide groups |
| `auth.rbac.group_roles` | map[string]string | none | Group-to-role mapping |
| `auth.rbac.roles[].name` | string | none | Role name |
| `auth.rbac.roles[].extends` | string list | none | Parent roles to inherit |
| `auth.rbac.roles[].replace` | bool | none | Replace an existing role definition |
| `auth.rbac.roles[].grants[].resource` | string | none | Resource identifier |
| `auth.rbac.roles[].grants[].permissions` | string list | none | Permission list for the resource |

### `auth.oauth2`

| Key | Type | Default | Notes |
| --- | --- | --- | --- |
| `auth.oauth2.issuer` | string | none | OIDC issuer |
| `auth.oauth2.client_id` | string | none | Client ID |
| `auth.oauth2.client_secret` | string | none | Client secret or ref |
| `auth.oauth2.redirect_uri` | string | none | Redirect URI |
| `auth.oauth2.scopes` | string list | none | Requested scopes |
| `auth.oauth2.discovery_url` | string | none | Explicit discovery document URL |

### `auth.session`

| Key | Type | Default | Notes |
| --- | --- | --- | --- |
| `auth.session.cookie_name` | string | none | Session cookie name |
| `auth.session.cookie_domain` | string | none | Cookie domain |
| `auth.session.max_age` | duration | none | Cookie lifetime |
| `auth.session.secure` | bool | none | Mark cookie as secure-only |

### `auth.jwt`

| Key | Type | Default | Notes |
| --- | --- | --- | --- |
| `auth.jwt.secret` | string | none | HMAC secret or secret ref |
| `auth.jwt.public_key` | string | none | Public key for asymmetric verification |
| `auth.jwt.issuer` | string | none | Expected issuer |
| `auth.jwt.audience` | string | none | Expected audience |
| `auth.jwt.algorithm` | string | none | JWT algorithm |
| `auth.jwt.clock_skew` | duration | none | Allowed clock skew |

### `auth.api_keys`

| Key | Type | Default | Notes |
| --- | --- | --- | --- |
| `auth.api_keys[].key` | string | none | API key or secret ref |
| `auth.api_keys[].name` | string | none | Human-readable key name |
| `auth.api_keys[].scopes` | string list | none | Optional scope list |
| `auth.api_keys[].enabled` | bool | none | Enable or disable that key |

## `runtime.audit` Addendum

This reference is mostly focused on system sections, but the audit export
surface matters for enterprise integrations.

| Key | Type | Default | Notes |
| --- | --- | --- | --- |
| `runtime.audit.enabled` | bool | none | Enables runtime audit fan-out features |
| `runtime.audit.output` | string | none | Local JSONL audit file |
| `runtime.audit.delivery.backend` | string | auto | `memory` or `sqlite`; `sqlite` is selected automatically when `store.backend=sqlite` |
| `runtime.audit.delivery.max_attempts` | int | `8` | Maximum delivery attempts before dead-letter |
| `runtime.audit.delivery.base_backoff` | duration | `5s` | Initial retry backoff |
| `runtime.audit.delivery.max_backoff` | duration | `5m` | Maximum retry backoff |
| `runtime.audit.delivery.poll_interval` | duration | `2s` | Dispatcher poll interval |
| `runtime.audit.delivery.batch_size` | int | `32` | Number of due deliveries processed per poll |
| `runtime.audit.sinks[].name` | string | none | Sink name shown in operator surfaces |
| `runtime.audit.sinks[].type` | string | inferred | `webhook`, `elasticsearch`, or `splunk_hec` |
| `runtime.audit.sinks[].enabled` | bool | `true` | Enable or disable that sink |
| `runtime.audit.sinks[].webhook.url` | string | none | Audit event receiver URL |
| `runtime.audit.sinks[].webhook.timeout` | duration | `15s` | Delivery timeout |
| `runtime.audit.sinks[].webhook.headers` | map[string]string | none | Extra outbound headers |
| `runtime.audit.sinks[].webhook.secret` | string | none | Shared secret or secret ref; emits HopClaw signature headers |
| `runtime.audit.sinks[].elasticsearch.url` | string | none | Base Elasticsearch endpoint |
| `runtime.audit.sinks[].elasticsearch.index` | string | none | Target index name |
| `runtime.audit.sinks[].elasticsearch.timeout` | duration | `15s` | Delivery timeout |
| `runtime.audit.sinks[].elasticsearch.headers` | map[string]string | none | Extra outbound headers |
| `runtime.audit.sinks[].elasticsearch.api_key` | string | none | Optional API key; sets `Authorization: ApiKey ...` when no explicit Authorization header exists |
| `runtime.audit.sinks[].splunk_hec.url` | string | none | Full Splunk HEC endpoint |
| `runtime.audit.sinks[].splunk_hec.token` | string | none | HEC token or secret ref |
| `runtime.audit.sinks[].splunk_hec.timeout` | duration | `15s` | Delivery timeout |
| `runtime.audit.sinks[].splunk_hec.headers` | map[string]string | none | Extra outbound headers |
| `runtime.audit.sinks[].splunk_hec.source` | string | none | Optional Splunk source |
| `runtime.audit.sinks[].splunk_hec.source_type` | string | none | Optional Splunk sourcetype |
| `runtime.audit.sinks[].splunk_hec.index` | string | none | Optional target index |
| `runtime.audit.sinks[].splunk_hec.host` | string | none | Optional host tag |
| `runtime.audit.sinks[].metadata` | map[string]any | none | Operator-visible metadata for the sink |

Notes:

1. `runtime.audit.sinks[]` must configure exactly one sink kind.
2. When `store.backend=sqlite`, the recommended production setting is `runtime.audit.delivery.backend=sqlite`.
3. Official direct sinks are currently `webhook`, `elasticsearch`, and `splunk_hec`. Kafka, OTLP, and object-storage pipelines should currently connect through a webhook bridge.

## Validation Workflow

After editing system sections, run:

```bash
hopclaw config validate
hopclaw doctor config
hopclaw doctor auth
hopclaw doctor security
```

For connectivity-sensitive settings:

```bash
hopclaw doctor connectivity
hopclaw update --check
hopclaw sandbox status
```

## 中文同步

### TL;DR

- 配置会按 `$HOPCLAW_CONFIG`、项目内、用户目录、系统目录的顺序自动发现。
- YAML 在解析前会先做 `${ENV_VAR}` 展开。
- Secret 字段建议写成 `env:NAME` 或 `keychain:NAME`，不要直接明文写进文件。
- 本页重点覆盖系统与运维相关的配置段，运行时、模型、工具、渠道等其他段在各自指南中说明。

### 推荐校验顺序

```bash
hopclaw config validate
hopclaw doctor config
hopclaw doctor auth
hopclaw doctor security
```

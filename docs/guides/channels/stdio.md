# Stdio Channel Plugins

## TL;DR

- `stdio` channel integrations are declared in a plugin manifest, not under `channels.<id>` in the main YAML config.
- The runtime channel name is `plugin:<channel-key>`, so that is the name you pass to `hopclaw channels list`, `status`, `validate`, and `test`.
- Fastest path: scaffold with `hopclaw plugins init <name> --kind channel`, run `go test ./...`, run `hopclaw plugins validate .`, then place the plugin in a discovery path and enable it.
- The transport is JSON-RPC 2.0 over NDJSON on stdio.

English is canonical in this file. 中文同步 follows after the English section.

## What This Surface Is

Use a stdio channel plugin when you want HopClaw to talk to a messaging
platform through a separate process instead of shipping a built-in adapter in
core Go code.

This is the right lane when:

- you need a bridge in Python, Node.js, Rust, or another non-Go runtime
- you want crash isolation between the core gateway and the platform-specific bridge
- you need a private or experimental adapter that should not become a built-in `channels.<id>` block yet

Unlike built-in channels such as `slack` or `telegram`, stdio channels are
discovered from plugin manifests and registered at runtime as `plugin:<key>`.

## Minimal Manifest

The smallest useful manifest looks like this:

```yaml
name: hello-channel
version: "1.0.0"
description: Example Level 0 channel plugin built with the HopClaw typed SDK.

channels:
  hello-channel:
    type: stdio
    command: ./hello-channel
    capabilities:
      - connect
      - send
```

Important naming rule:

- `name` is the plugin package name
- `hello-channel` under `channels:` is the channel key
- the runtime channel name becomes `plugin:hello-channel`

If your manifest uses a different channel key, the `plugin:<channel-key>` form
still applies.

## Fastest Development Loop

Scaffold a starter channel plugin:

```bash
mkdir -p ./.hopclaw/plugins
hopclaw plugins init hello-channel --kind channel --dir ./.hopclaw/plugins
cd ./.hopclaw/plugins/hello-channel
go test ./...
hopclaw plugins validate .
```

Start or restart the gateway so plugin discovery picks it up, then inspect the
loaded plugin and channel surfaces:

```bash
hopclaw plugins list
hopclaw plugins info hello-channel
hopclaw plugins enable hello-channel
hopclaw channels list
hopclaw channels status plugin:hello-channel
```

If you want a non-scaffold example, start from:

- `examples/stdio-channel-template/`
- `sdk/plugin/examples/hello-channel/`

## Validate And Smoke Test

Once the plugin is visible, validate it like any other channel using the
runtime name:

```bash
hopclaw channels validate plugin:hello-channel
hopclaw channels test plugin:hello-channel \
  --target demo-room \
  --message "HopClaw stdio channel smoke test"
hopclaw doctor connectivity
```

Notes:

- `hopclaw channels add` does not create stdio plugin channels; it only edits
  ordinary built-in channel config.
- `--target` is adapter-specific. For some plugins it may be a room ID, chat
  GUID, thread identifier, or another platform-native destination.
- If the plugin package is installed but the channel is absent, check the
  plugin manifest `channels.<key>.type`, plugin enablement state, and whether
  the gateway has reloaded plugin discovery.

## Discovery Paths

HopClaw discovers plugins in standard plugin roots such as:

```text
./.hopclaw/plugins
./extensions
~/.hopclaw/plugins
~/.hopclaw/extensions
```

That means a local repo-scoped development loop can stay entirely inside
`./.hopclaw/plugins`, while user-global experiments can live under
`~/.hopclaw/plugins`.

## Protocol Notes

The current stdio channel protocol is:

- JSON-RPC 2.0 message envelopes
- one JSON document per line on stdin/stdout
- host-to-plugin requests: `initialize`, `connect`, `disconnect`, `send`
- plugin-to-host notifications: `channel/status`, `channel/inbound`

The authoritative protocol structs live in:

- `channels/stdio/protocol.go`
- `channels/stdio/adapter.go`

For extension-author detail, see:

- `docs/development/plugin-sdk.md`

## Troubleshooting

If `hopclaw channels validate plugin:<key>` fails:

1. Validate the plugin package itself.

```bash
hopclaw plugins validate ./.hopclaw/plugins/hello-channel
```

2. Confirm the plugin is discovered and enabled.

```bash
hopclaw plugins list
hopclaw plugins info hello-channel
```

3. Confirm the runtime channel name you are testing.

```bash
hopclaw channels list
hopclaw channels status plugin:hello-channel
```

4. If the process starts but never connects, inspect the plugin command path,
   file permissions, working directory, and any required environment variables.

## 中文同步

### TL;DR

- `stdio` 渠道不是写在主配置文件的 `channels.<id>` 下面，而是写在插件 manifest 里。
- 运行时渠道名是 `plugin:<channel-key>`，所以 `hopclaw channels list/status/validate/test` 都要用这个名字。
- 最快路径是先用 `hopclaw plugins init <name> --kind channel` 起脚手架，跑 `go test ./...` 和 `hopclaw plugins validate .`，再放进插件发现目录并启用。
- 传输协议是跑在 stdio 上的 NDJSON + JSON-RPC 2.0。

### 最小可用心智模型

- 内建渠道：主配置里的 `channels.slack`、`channels.telegram` 这类 YAML 块
- `stdio` 渠道：插件 manifest 里的 `channels.<key>.type: stdio`
- 运行时名字：统一映射成 `plugin:<key>`

### 最常用命令

```bash
mkdir -p ./.hopclaw/plugins
hopclaw plugins init hello-channel --kind channel --dir ./.hopclaw/plugins
cd ./.hopclaw/plugins/hello-channel
go test ./...
hopclaw plugins validate .
hopclaw plugins list
hopclaw plugins info hello-channel
hopclaw plugins enable hello-channel
hopclaw channels validate plugin:hello-channel
hopclaw channels test plugin:hello-channel --target demo-room --message "HopClaw stdio channel smoke test"
```

### 排障优先级

- 先看插件有没有被发现和启用
- 再看运行时渠道名是不是 `plugin:<key>`
- 再检查插件命令路径、执行权限、工作目录和环境变量

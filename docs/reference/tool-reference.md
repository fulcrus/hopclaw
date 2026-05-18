# Tool Reference

## TL;DR

- The live source of truth is always `hopclaw tools list`, `hopclaw tools search`, and `hopclaw tools info`.
- Built-in tools are organized by category and registered through the runtime tool catalog.
- Some tool families are always present, while others appear only when related services or helpers are configured.

English is canonical in this file. 中文同步 follows after the English section.

## Discover The Live Runtime

```bash
hopclaw tools list
hopclaw tools search browser
hopclaw tools info fs.read
hopclaw tools check
```

Use `--session-key` when you need context-aware tool visibility:

```bash
hopclaw tools list --session-key release-ops
```

## Core Always-On Families

These are the families most operators and skill authors hit first:

- filesystem: `fs.read`, `fs.write`, `fs.list`, `fs.find`, `fs.patch`, `fs.tree`, `fs.stat`
- process and shell: `exec.run`, `exec.script`, `proc.start`, `proc.logs`, `proc.wait`, `proc.stop`
- env and state: `env.get`, `env.set`, `env.info`, `env.probe`, `env.refresh`
- text and data shaping: `text.json`, `text.yaml`, `text.regex`, `text.template`, `text.csv`, `text.markdown`
- network: `net.http`, `net.fetch`, `net.download`, `net.dns`, `net.ping`, `net.port_check`

## Automation Families

- `cron.*`
- `watch.*`
- `wakeup.*`
- `automation.search`
- `automation.stats`
- `session.*`
- `memory.*`

## Integration Families

- `channel.*`
- `knowledge.*`
- `skill.*`
- `semantic.*`
- `destination.*`

## Browser, Desktop, And Device Families

These appear only when the related helper or capability surface is available:

- browser automation: `browser.*`
- canvas/browser UI surface: `canvas.*`
- node or desktop helper tools: `nodes.*`

## Content And Artifact Families

- `document.*`
- `pdf.*`
- `presentation.*`
- `spreadsheet.*`
- `archive.*`

## Optional Utility Families

- `crypto.*`
- `db.kv.*`
- `web.fetch`

## Layer-2 And Auxiliary Runtime Tools

Some shipped surfaces live outside the built-in category registry but still appear in the runtime when enabled:

- `git.*`
- `container.*`
- `calendar.*`
- `email.*`
- `media.*`
- `news.*`
- `pkg.*`
- `search.*`
- `speech.*`

## Practical Debug Loop

When a tool seems to be “missing,” do this in order:

```bash
hopclaw tools search <term>
hopclaw tools info <name>
hopclaw tools check <name>
hopclaw doctor skills
hopclaw doctor connectivity
```

## 中文同步

### TL;DR

- 实时真相以 `hopclaw tools list/search/info` 为准
- built-in tools 按类别注册在运行时 tool catalog 中
- 有些工具恒定存在，有些只会在 helper 或相关服务可用时出现

### 最常用家族

- `fs.*`
- `exec.*` / `proc.*`
- `env.*`
- `text.*`
- `net.*`
- `browser.*`
- `watch.*` / `cron.*` / `wakeup.*`
- `knowledge.*`
- `channel.*`

# Watch Mode

## TL;DR

- Watch jobs are first-class automation items backed by `/operator/watch/*`.
- The CLI can inspect, list, pause, and resume watches through `hopclaw automation`, but create/update/run currently go through the operator API or runtime tools such as `watch.add`.
- Start with a simple HTTP watch before you try mailbox, browser snapshot, calendar, or webhook inbox sources.

English is canonical in this file. 中文同步 follows after the English section.

## Minimal HTTP Watch

Assuming your gateway is already running and your operator token is exported:

```bash
curl -sS \
  -H "Authorization: Bearer $HOPCLAW_AUTH_TOKEN" \
  -H "Content-Type: application/json" \
  -X POST http://127.0.0.1:16280/operator/watch/items \
  -d '{
    "name": "example-homepage-watch",
    "interval": "10m",
    "source": {
      "kind": "http",
      "http": {
        "url": "https://example.com"
      }
    },
    "prompt": "Inspect what changed and summarize only meaningful differences."
  }'
```

## Inspect, Pause, Resume

```bash
hopclaw automation list --kind watch
hopclaw automation inspect watch <id>
hopclaw automation recent watch <id>
hopclaw automation pause watch <id>
hopclaw automation resume watch <id>
```

Run one watch immediately:

```bash
curl -sS \
  -H "Authorization: Bearer $HOPCLAW_AUTH_TOKEN" \
  -X POST http://127.0.0.1:16280/operator/watch/items/<id>/run
```

## Current Source Kinds

The shipped watch tooling and handlers support source kinds such as:

- `http`
- `file`
- `feed`
- `mailbox`
- `browser_snapshot`
- `calendar`
- `webhook`
- `structured_app_inbox`

## Delivery Targets

A watch can push notifications into a channel-specific target:

```json
{
  "delivery": {
    "channel": "telegram",
    "target": "chat-42"
  }
}
```

Keep delivery debugging separate from source debugging. First prove the watch detects change. Then prove the delivery path.

## 中文同步

### TL;DR

- Watch 是 `/operator/watch/*` 下的一等自动化对象
- 当前 CLI 偏向“查看和运维”，创建与立即触发更适合直接调 operator API 或用 `watch.*` 工具
- 建议先从简单的 `http` watch 开始

### 最小示例

上文 `Minimal HTTP Watch` 就是当前最小可跑通的创建方式。

### 运维命令

```bash
hopclaw automation list --kind watch
hopclaw automation inspect watch <id>
hopclaw automation recent watch <id>
hopclaw automation pause watch <id>
hopclaw automation resume watch <id>
```

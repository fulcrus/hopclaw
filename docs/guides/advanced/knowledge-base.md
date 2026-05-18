# Knowledge Base

## TL;DR

- HopClaw can index external knowledge instead of forcing docs into prompt text.
- The current operator API supports source CRUD plus search and sync under `/operator/knowledge/*`.
- Knowledge sync is incremental: unchanged documents are not re-embedded on every sync or restart.
- Text and vector indexes are persistent SQLite projections, not startup-only caches.
- Search is locale-aware and can span multiple languages when semantic vectors match.
- Start with `local_dir` or `web_urls`, then expand to Notion, Confluence, Feishu Docs, Google Drive, Yuque, or Tencent Docs.

English is canonical in this file. 中文同步 follows after the English section.

## Current Supported Source Kinds

- `local_dir`
- `git_repo`
- `web_urls`
- `feishu_docs`
- `notion`
- `confluence`
- `google_drive`
- `yuque`
- `tencent_docs`

## Create A Local Directory Source

```bash
curl -sS \
  -H "Authorization: Bearer $HOPCLAW_AUTH_TOKEN" \
  -H "Content-Type: application/json" \
  -X POST http://127.0.0.1:16280/operator/knowledge/sources \
  -d '{
    "name": "Ops Docs",
    "kind": "local_dir",
    "locale": "en",
    "path": "/Users/me/docs/operations",
    "enabled": true,
    "include_globs": ["**/*.md"],
    "exclude_globs": [".git/**"]
  }'
```

## Sync And Search

```bash
curl -sS \
  -H "Authorization: Bearer $HOPCLAW_AUTH_TOKEN" \
  -X POST http://127.0.0.1:16280/operator/knowledge/sources/<source-id>/sync

curl -sS \
  -H "Authorization: Bearer $HOPCLAW_AUTH_TOKEN" \
  "http://127.0.0.1:16280/operator/knowledge/search?q=rollback&source_id=<source-id>&locale=en"
```

After a successful sync:

- the source exposes `last_sync_at` and `sync_cursor`
- unchanged documents keep their existing `content_hash` / `synced_at`
- FTS and vector projections stay on disk and survive restart

## Runtime Tool Surface

The built-in tools mirror the operator surface:

```bash
hopclaw tools info knowledge.sources
hopclaw tools info knowledge.search
hopclaw tools info knowledge.sync
```

## Practical Guidance

- Use `local_dir` when your docs already live on disk.
- Use `git_repo` when the checked-out repository is the real source of truth.
- Use `web_urls` when the published site is canonical.
- Use SaaS connectors only when the team already maintains that system as the canonical editing surface.
- Set `locale` on a source when the whole corpus is mostly one language and the upstream system does not carry a stronger locale signal.
- Leave `locale` empty when per-document locale detection should drive hybrid retrieval.
- Use the optional `locale` query parameter on `/operator/knowledge/search` when an operator wants to bias ranking toward one locale family.
- Check `GET /operator/controlplane/status` when you need to confirm incremental sync, persistent FTS/vector projections, and projection-only index boundaries.

## 中文同步

### TL;DR

- HopClaw 的知识库目标是“索引外部知识源”，而不是把文档全文塞进 prompt
- 当前 operator API 已支持 source 的增删改查、sync 和 search
- sync 是增量的；重启或再次 sync 不会默认整库重建向量
- FTS / vector 索引是持久化 projection，不是只存在于启动期内存里的缓存
- 检索会做 locale-aware 排序，并支持跨语言召回
- 最适合先落地的是 `local_dir` 与 `web_urls`

### 常用流程

```bash
curl -H "Authorization: Bearer $HOPCLAW_AUTH_TOKEN" -H "Content-Type: application/json" \
  -X POST http://127.0.0.1:16280/operator/knowledge/sources \
  -d '{"name":"Ops Docs","kind":"local_dir","locale":"en","path":"/Users/me/docs/operations","enabled":true}'

curl -H "Authorization: Bearer $HOPCLAW_AUTH_TOKEN" \
  -X POST http://127.0.0.1:16280/operator/knowledge/sources/<source-id>/sync

curl -H "Authorization: Bearer $HOPCLAW_AUTH_TOKEN" \
  "http://127.0.0.1:16280/operator/knowledge/search?q=回滚&source_id=<source-id>&locale=zh-CN"
```

- sync 完成后，source 会暴露 `last_sync_at` 和 `sync_cursor`
- 想确认这套契约是否已经发货，可查看 `GET /operator/controlplane/status` 里的 `knowledge` 摘要

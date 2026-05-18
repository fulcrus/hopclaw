# HTTP API Reference

## TL;DR

- The canonical machine-readable Runtime API lives in `docs/openapi/runtime-v1.yaml`.
- The operator/control-plane OpenAPI lives in `docs/openapi/operator-v1.yaml`.
- The stable public runtime surface is centered on `/healthz` and `/runtime/*`.
- Operator endpoints under `/operator/*` are already widely used by the CLI, but evolve faster than the runtime API.

English is canonical in this file. 中文同步 follows after the English section.

## Canonical Spec

Start with:

- `docs/openapi/runtime-v1.yaml`
- `docs/openapi/operator-v1.yaml`
- `docs/openapi/README.md`
- `docs/reference/gateway-operator-websocket.md` for gateway operator-websocket notes on `GET /operator/ws`

Preview it locally with:

```bash
docker run --rm \
  -p 18080:8080 \
  -e SWAGGER_JSON=/spec/runtime-v1.yaml \
  -v "$PWD/docs/openapi":/spec \
  swaggerapi/swagger-ui
```

Switch `SWAGGER_JSON` to `/spec/operator-v1.yaml` when you want to inspect the operator surface instead.

## Stable Runtime Endpoints

The current OpenAPI file covers the release-oriented runtime surface:

- `GET /healthz`
- `POST /runtime/interact`
- `GET /runtime/events/stream`
- `GET /runtime/tools`
- `GET /runtime/runs`
- `POST /runtime/runs`
- `GET /runtime/runs/{id}`
- `GET /runtime/runs/{id}/result`
- `GET /runtime/runs/{id}/verification`
- `GET /runtime/runs/{id}/completion`
- `POST /runtime/runs/{id}/resume`
- `POST /runtime/runs/{id}/cancel`
- `GET /runtime/sessions`
- `GET /runtime/sessions/{id}`
- `DELETE /runtime/sessions/{id}`

## Stable Machine Contracts

Round 1 freezes the machine-facing identifiers used by automation:

- API JSON field names remain canonical English
- module manifest field names remain canonical English
- API failures use stable `error.code` values
- policy decisions use stable reason codes
- verification issues use stable verification codes

Localized product copy is supported, but it is not the contract surface for
automation.

For tests and client integrations:

- assert HTTP status and `error.code`
- assert structured JSON state and `data-testid` markers for UI surfaces
- do not key automation off localized prose

## Run Semantic Diagnostics

`GET /runtime/runs` and `GET /runtime/runs/{id}` now expose a stable
`semantic_signal` diagnostic snapshot for operator tooling.

That snapshot is intended for diagnostics and orchestration visibility rather
than prompt reconstruction. It includes structured semantic state such as:

- language family/script
- `requires_current_info`
- reference and confirmation requirements
- suggested domains
- job type and target summary
- `triage_ready` and `task_contract_ready`

The runtime keeps raw user message text in the session transcript, not in the
persisted `semantic_signal` run diagnostic.

## Execution Graph Diagnostics And Structured Outcomes

HopClaw now exposes the single-session execution graph as a structured runtime
diagnostic instead of leaving supervisors to infer execution state from free
text.

- `GET /runtime/runs/{id}` includes `execution_graph` on run detail responses
- `GET /runtime/runs?include=execution_graph` includes `execution_graph` on list
  items when explicitly requested
- `GET /runtime/runs/{id}/result` includes `task_outcomes` so downstream clients
  can consume structured task status, artifacts, and idempotency keys

Recommended client behavior:

- use `execution_graph` for operator or supervisor views of active work
- use `task_outcomes` and `deliverables` for structured downstream processing
- do not infer task topology or retry state from assistant free text when these
  structured fields are present

## Event Ledger And Delivery Closure

`GET /runtime/runs/{id}/result` now also carries the Phase E closure projection
for result, audit, and external delivery.

- `event_ledger.events[*].event_class` uses the stable enum values:
  - `evidence`
  - `audit`
  - `delivery`
- `delivery` is the structured `DeliveryEnvelope` used for downstream channel or
  adapter delivery
- `receipts` exposes delivery-outbox execution state such as adapter name,
  idempotency key, attempts, and terminal status

`GET /runtime/runs/{id}/completion` is the canonical completion surface for a
run. It combines:

- `result`
- `verification`
- `delivery`
- `receipts`

Recommended client behavior:

- treat `RunResult` as a joint projection of transcript output,
  `task_outcomes`, and `event_ledger`
- use `delivery` plus `receipts` to reason about external delivery progress and
  idempotency instead of parsing assistant prose
- use `/runtime/runs/{id}/completion` when you need one stable read model for
  result plus verification plus delivery closure

For operators, `GET /operator/controlplane/status` also exposes stable result
and governance-delivery diagnostics under:

- `results.unified_event_ledger`
- `results.run_result_sources`
- `results.delivery_outbox_table`
- `governance.delivery_stats`
- `governance.delivery_health`

## Operator WebSocket Clients

Gateway deployments expose a control-plane/device WebSocket at `GET /operator/ws`.

For device or control-plane clients, also read:

- `docs/reference/gateway-operator-websocket.md`

## Runtime WebSocket Clients

The runtime RPC WebSocket is now explicitly named:

- canonical path: `GET /runtime/ws`

Use `/runtime/ws` whenever you mean the runtime handshake-based RPC socket.
This keeps it separate from the gateway operator/device socket at
`/operator/ws`.

## Streaming Direct Turns

`POST /runtime/interact` can complete in two different ways:

- tracked execution: creates a run and continues through `/runtime/runs/*`
- runless direct reply: the model answers in the conversational or clarification envelope without creating a tracked run

For direct clients, the streaming contract is shared with the normal runtime
event stream:

- subscribe to `GET /runtime/events/stream`
- consume `model.text_delta` and `model.stream_complete`
- tracked runs are correlated by `run_id`
- runless direct turns use an empty `run_id` and should be correlated by
  `attrs.interaction_turn_id`
- `attrs.interaction_envelope` indicates `conversation` or `clarification`

Recommended client behavior:

- send `metadata.interaction_turn_id` with `/runtime/interact` when you need to
  correlate a runless streamed reply
- keep using the normal run event flow for tracked execution
- do not expect runless direct turns to appear in `/runtime/runs`, even though
  they are still persisted into the session transcript

## Common Curl Smoke Tests

Health:

```bash
curl -sS http://127.0.0.1:16280/healthz
```

List tools:

```bash
curl -sS \
  -H "Authorization: Bearer $HOPCLAW_AUTH_TOKEN" \
  "http://127.0.0.1:16280/runtime/tools?session_key=default"
```

Submit a run:

```bash
curl -sS \
  -H "Authorization: Bearer $HOPCLAW_AUTH_TOKEN" \
  -H "Content-Type: application/json" \
  -X POST http://127.0.0.1:16280/runtime/runs \
  -d '{
    "session_key": "api-demo",
    "content": "Summarize the current runtime status."
  }'
```

## Operator API Surfaces Used By The CLI

The CLI also depends on faster-moving `/operator/*` endpoints such as:

- models
- channels
- webhooks
- automation
- controlplane status and storage diagnostics
- browser sessions
- skills catalog
- knowledge sources
- approvals and governance

Treat those as operator-facing surfaces, not the minimal public runtime contract.

The current machine-readable operator/control-plane snapshot lives in:

- `docs/openapi/operator-v1.yaml`

## Knowledge Incremental Sync And Search

The operator knowledge surface is intentionally separate from the stable runtime
execution contract.

- `GET /operator/knowledge/sources` lists configured sources plus supported-kind
  field metadata
- `POST /operator/knowledge/sources` creates a source with typed metadata such
  as `kind`, `locale`, and connector config
- `POST /operator/knowledge/sources/{id}/sync` performs incremental sync
- `GET /operator/knowledge/search` searches the persistent FTS/vector
  projections

Recommended operator behavior:

- treat `knowledge_sources`, `knowledge_documents`, and `knowledge_chunks` as
  truth rows
- treat FTS/vector indexes as projections, not as the source of truth
- use `sync_cursor` and `last_sync_at` to observe sync progress
- rely on locale-aware retrieval for multilingual corpora; use the optional
  `locale` query parameter when an operator wants to bias ranking explicitly

`GET /operator/controlplane/status` also exposes a stable `knowledge` summary
for shipped diagnostics:

- typed source/document/chunk metadata
- incremental sync
- persistent FTS/vector indexes
- locale-aware retrieval
- projection-only table boundaries

## Enterprise Integration Surfaces

For team or enterprise deployments, the practical integration surface is wider
than just the minimal OpenAPI file:

- `/runtime/*` for execution, sessions, runs, and results
- `GET /operator/ws` for long-lived bidirectional gateway control-plane or device-style clients
- `GET /runtime/ws` for runtime RPC websocket clients
- `/operator/*` for configuration, status, and operator workflows
- approval-provider callback flows such as `/runtime/approvals/callbacks/resolve`
- event delivery through hooks, webhooks, and event-sink style integrations
- Prometheus metrics and health checks for operations

Recommended usage pattern:

- treat `/runtime/*` as the stable execution contract
- treat `/operator/*` as the operator/control-plane surface used behind your own
  portal, internal tooling, or deployment wrapper
- keep tenant, org, user lifecycle, and business reporting in your outer system

## 中文同步

### TL;DR

- 机器可读规范以 `docs/openapi/runtime-v1.yaml` 为准
- 运维/控制面 OpenAPI 在 `docs/openapi/operator-v1.yaml`
- 稳定公共 API 主要是 `/healthz` 和 `/runtime/*`
- `/operator/*` 已被 CLI 大量使用，但演进速度更快

### 企业接入提示

如果你是团队或企业接入，不要只看最小 OpenAPI：

- `/runtime/*` 适合当执行契约
- `GET /operator/ws` 适合 gateway 控制面 / device 长连接客户端，说明见 `docs/reference/gateway-operator-websocket.md`
- `GET /runtime/ws` 适合 runtime RPC websocket 客户端
- `/operator/*` 适合当运维和控制面接口
- 审批回调、hooks、webhooks、metrics 共同组成更完整的企业接入面

更推荐的做法是：

- 用 `/runtime/*` 对接执行
- 用 `/operator/*` 支撑内部控制台或平台能力
- 把租户、组织、用户生命周期、业务报表放在外围系统

### Gateway Operator WebSocket

- Gateway 控制面 / device websocket 的 canonical 路径是 `GET /operator/ws`
- runtime websocket 的 canonical 路径是 `GET /runtime/ws`
- direct chat / terminal / webchat 的真实流式回复仍应走 `GET /runtime/events/stream`，不是 operator websocket

### 直接对话流式约定

`POST /runtime/interact` 不一定都会创建 tracked run。

- 需要执行任务时，会创建 run，并继续走 `/runtime/runs/*`
- 纯对话或 clarification turn 时，不创建 run，但模型仍会真实回复并流式输出

这类 direct turn 复用同一个事件流端点：

- 订阅 `GET /runtime/events/stream`
- 消费 `model.text_delta` 与 `model.stream_complete`
- 有 run 的事件继续用 `run_id` 关联
- 无 run 的 direct turn，`run_id` 为空，需要用 `attrs.interaction_turn_id` 关联
- `attrs.interaction_envelope` 标识 `conversation` 或 `clarification`

更稳健的客户端做法是：

- 调 `/runtime/interact` 时主动传 `metadata.interaction_turn_id`
- tracked run 继续按原来的 run 事件链消费
- 不要期待 runless direct turn 出现在 `/runtime/runs` 里；它们会写入 session transcript，但不会生成 tracked run 记录

### Run 语义诊断

`GET /runtime/runs` 和 `GET /runtime/runs/{id}` 现在会返回结构化
`semantic_signal` 诊断字段，方便 operator / CLI 看到语言、是否需要当前信息、
候选 domain，以及 `triage_ready` / `task_contract_ready` 等阶段状态。

这个字段是运行态诊断投影，不是 prompt 重建面；原始用户消息仍以 session
transcript 为准，不会写进持久化的 run 级 `semantic_signal` 诊断。

### Execution Graph 与结构化结果

现在运行时也会暴露结构化 `execution_graph` 与 `task_outcomes`，避免上层只能
靠自由文本猜当前执行拓扑。

- `GET /runtime/runs/{id}` 会返回 `execution_graph`
- `GET /runtime/runs?include=execution_graph` 会在显式请求时把
  `execution_graph` 带到列表项
- `GET /runtime/runs/{id}/result` 会返回 `task_outcomes`，方便客户端读取
  task 状态、artifact 与 `idempotency_key`

更稳健的客户端做法是：

- 用 `execution_graph` 做 operator / supervisor 视图
- 用 `task_outcomes` 和 `deliverables` 做结构化下游处理
- 在这些结构化字段存在时，不要再从 assistant 自由文本里反推任务拓扑或 retry 状态

### Event Ledger 与交付闭环

`GET /runtime/runs/{id}/result` 现在还会把事件账本和交付闭环一起投影出来：

- `event_ledger.events[].event_class` 只使用稳定枚举：
  - `evidence`
  - `audit`
  - `delivery`
- `delivery` 是给外部渠道/adapter 用的结构化 `DeliveryEnvelope`
- `receipts` 是外部交付回执，来源于 delivery outbox / governance delivery store

`GET /runtime/runs/{id}/completion` 则提供统一 completion 视图，把：

- `result`
- `verification`
- `delivery`
- `receipts`

放到一个稳定读模型里，避免上层自己拼接多份状态。

更稳健的客户端做法是：

- 用 `event_ledger` 读取证据、审计、交付三类事件，而不是自己拆多套 ledger
- 用 `delivery` 和 `receipts` 判断外部交付状态、重试进度和 `idempotency_key`
- 需要结果 + 验证 + 交付闭环时，优先读 `/runtime/runs/{id}/completion`
- 把 run 结果理解为 `transcript + task_outcomes + event_ledger` 的联合投影，不要只信任最终 assistant 文本

控制面也会在 `GET /operator/controlplane/status` 里暴露这套契约的稳定诊断：

- `results.unified_event_ledger`
- `results.run_result_sources`
- `results.delivery_outbox_table`
- `governance.delivery_stats`
- `governance.delivery_health`

### Knowledge 增量同步与检索

知识库相关的 operator surface 独立于稳定 runtime 执行契约：

- `GET /operator/knowledge/sources` 查看 source 列表和 kind 字段元数据
- `POST /operator/knowledge/sources` 创建带 typed metadata 的 source
- `POST /operator/knowledge/sources/{id}/sync` 做增量 sync
- `GET /operator/knowledge/search` 走持久化 FTS/vector projection 做检索

更稳健的 operator 做法是：

- 把 `knowledge_sources`、`knowledge_documents`、`knowledge_chunks` 当真相行
- 把 FTS / vector 索引当 projection，不要反过来把 projection 当真相
- 用 `sync_cursor` 和 `last_sync_at` 观察同步进度
- 多语言知识库默认使用 locale-aware 排序；需要强制偏向某语言时，可传 `locale` 查询参数

`GET /operator/controlplane/status` 也会在 `knowledge` 摘要下暴露这套契约：

- typed source/document/chunk metadata
- incremental sync
- persistent FTS/vector indexes
- locale-aware retrieval
- projection-only table boundaries

### 常用请求

```bash
curl -sS http://127.0.0.1:16280/healthz
curl -sS -H "Authorization: Bearer $HOPCLAW_AUTH_TOKEN" "http://127.0.0.1:16280/runtime/tools?session_key=default"
```

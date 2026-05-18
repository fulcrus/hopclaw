# ACP Protocol

## TL;DR

- ACP in HopClaw is JSON-RPC 2.0 over NDJSON on stdio.
- The current negotiated protocol version is `2024-11-05`.
- The expected lifecycle is `initialize` -> `acp/newSession` or
  `acp/loadSession` -> `acp/prompt`.
- Request objects and method params are decoded strictly: unknown fields,
  malformed payloads, and unsupported capabilities are rejected explicitly.
- Core notifications are `acp/sessionUpdate`, `acp/permissionRequest`, and
  conditionally `acp/commandsUpdate`.

English is canonical in this file. 中文同步 follows after the English section.

## Transport

- one JSON object per line
- stdio transport
- JSON-RPC 2.0 framing
- notifications omit the request ID

The ACP package is designed for external clients such as IDE extensions,
desktop apps, or mobile shells that want a subprocess-style agent integration.

## Request Methods

The current server handles:

- `initialize`
- `acp/newSession`
- `acp/loadSession`
- `acp/prompt`
- `acp/cancel`
- `acp/listSessions`
- `acp/setMode`
- `acp/setConfigOption`
- `acp/permissionResponse`

`acp/permissionResponse` may be sent as a JSON-RPC request if the client wants
an explicit acknowledgement or validation error. Notification-style usage is
accepted for compatibility but produces no reply.

## Capability Negotiation

`initialize` is the handshake boundary. The client sends:

- `protocol_version`
- `client_info`
- optional nested `capabilities`

The server replies with:

- the negotiated `protocol_version`
- `server_info`
- the server capability tree for the current runtime configuration

Current capability groups include:

- `streaming`
- `permissions`
- `commands`
- `prompt.*`
- `sessions.*`
- `notifications.*`
- `protocol_versions`

`notifications.commands_update` is runtime-derived. It is `true` only when the
server is configured to publish command inventory updates.

If the client marks an unsupported capability as required, `initialize` fails
with structured error data instead of silently ignoring the mismatch.

## Validation Rules

- top-level JSON-RPC requests reject unknown fields
- method params reject unknown fields
- missing required fields fail deterministically instead of decoding to Go zero
  values
- malformed JSON returns a parse error
- valid JSON that is not a valid request object returns an invalid-request
  error

## Error Model

ACP error responses use JSON-RPC codes plus a structured `error.data.code`
value for machine-readable diagnostics.

Current ACP-specific error codes:

- `acp.parse_error`
- `acp.invalid_request`
- `acp.method_not_found`
- `acp.invalid_params`
- `acp.internal_error`
- `acp.protocol_version_unsupported`
- `acp.capability_unsupported`
- `acp.session_not_found`

`error.data.details` may include field names, requested capabilities,
`protocol_version`, or supported versions depending on the failure.

## Core Notifications

- `acp/sessionUpdate`
  - text deltas
  - tool start / delta / end transitions
  - usage
  - stop reason or error
- `acp/commandsUpdate`
  - refreshed slash-command inventory for the client UI
  - emitted only when command inventory publishing is enabled
- `acp/permissionRequest`
  - asks the client to approve or deny a tool action

## Session States

Current session statuses include:

- `idle`
- `streaming`
- `tool_use`
- `completed`
- `error`

Current stop reasons include:

- `end_turn`
- `tool_use`
- `max_tokens`
- `cancelled`
- `error`

## Minimal Flow Example

1. Client sends `initialize`
2. Server replies with negotiated protocol version and capabilities
3. If command inventory publishing is enabled, server may send
   `acp/commandsUpdate`
4. Client sends `acp/newSession`
5. Client sends `acp/prompt`
6. Server streams `acp/sessionUpdate`
7. If a tool needs approval, server sends `acp/permissionRequest`
8. Client answers with `acp/permissionResponse`

## Built-In ACP Commands

The default command inventory currently includes:

- `help`
- `status`
- `context`
- `usage`
- `cancel`
- `compact`
- `think`
- `verbose`
- `model`
- `queue`
- `debug`
- `config`

## 中文同步

### TL;DR

- ACP 是跑在 stdio 上的 NDJSON + JSON-RPC 2.0
- 当前握手协议版本是 `2024-11-05`
- 典型流程是 `initialize -> new/load session -> prompt`
- 请求对象和参数都走严格解码；未知字段、坏 payload、能力不匹配都会被明确拒绝
- 核心通知有 `acp/sessionUpdate`、`acp/permissionRequest`，以及按配置决定的
  `acp/commandsUpdate`

### 当前方法

- `initialize`
- `acp/newSession`
- `acp/loadSession`
- `acp/prompt`
- `acp/cancel`
- `acp/listSessions`
- `acp/setMode`
- `acp/setConfigOption`
- `acp/permissionResponse`

`acp/permissionResponse` 既可以作为 request 使用，也可以继续用 notification
兼容旧客户端；如果用 request，客户端可以拿到显式确认或参数错误。

### 能力协商

- `initialize` 输入 `protocol_version`、`client_info` 和可选 `capabilities`
- 返回值里会给出当前服务端真实支持的 capability tree
- `notifications.commands_update` 不是硬编码宣言，只有服务端真的会发命令清单更新时才是
  `true`
- 客户端把不支持的 capability 标成必需时，握手会失败，不会静默忽略

### 错误模型

- JSON-RPC 标准错误码仍然保留
- 机器可读诊断放在 `error.data.code`
- 当前实现的 ACP 错误码有：
  - `acp.parse_error`
  - `acp.invalid_request`
  - `acp.method_not_found`
  - `acp.invalid_params`
  - `acp.internal_error`
  - `acp.protocol_version_unsupported`
  - `acp.capability_unsupported`
  - `acp.session_not_found`

### 当前通知

- `acp/sessionUpdate`
- `acp/commandsUpdate`
- `acp/permissionRequest`

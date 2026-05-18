# Gateway Operator WebSocket

Gateway control-plane and device clients connect to `GET /operator/ws`.

This socket is separate from the runtime RPC websocket at `GET /runtime/ws`.
Use the gateway operator websocket only for gateway/device-style request and
response flows such as node registration and invocation.

## Current Frame Shapes

Gateway operator WebSocket requests use a request/response envelope:

```json
{
  "type": "req",
  "id": "node-1",
  "method": "node.register",
  "params": {
    "node_id": "desktop-a"
  }
}
```

Successful responses:

```json
{
  "type": "res",
  "id": "node-1",
  "ok": true,
  "payload": {}
}
```

Error responses:

```json
{
  "type": "res",
  "id": "node-1",
  "ok": false,
  "error": {
    "code": -32603,
    "message": "runtime not available"
  }
}
```

Node callbacks are server-initiated `invoke` frames followed by
`invoke.result`.

## Reconnect Guidance

The gateway operator websocket does not expose a separate resume handshake.
When a connection drops, the correct recovery flow is:

1. Reconnect to `GET /operator/ws`
2. Re-authenticate or resend device credentials
3. Re-register any node or client state you need
4. Re-fetch authoritative state from `/operator/*` or `/runtime/*` when your
   UI or device needs a fresh snapshot

If a client cannot prove that its local derived state is still current after a
disconnect, it should discard that derived state and rebuild it from the
authoritative HTTP surface.

## 中文摘要

- gateway 控制面 / device websocket 的 canonical 路径是 `GET /operator/ws`
- 它与 runtime RPC websocket `GET /runtime/ws` 是两条不同的协议面
- 当前 gateway websocket 没有单独的 resume 握手机制
- 断线后应重新连接、重新鉴权，并按需通过 `/operator/*` 或 `/runtime/*`
  回源拉取权威状态

# Capability Host Template

This is a **reference template**, not a promise of a generic drop-in plugin
protocol.

Use it when you want to design a host-backed service that looks like the
built-in browser or desktop helpers:

- an HTTP action endpoint
- an auth token
- session creation and session-scoped operations
- a health endpoint

## Important Scope Note

Today, host-backed integrations in HopClaw usually still require a Go-side
adapter or capability registration in the core runtime.

So this template is useful for:

- shaping the service contract
- prototyping the helper process in Go, Python, or Node.js
- aligning request and response semantics with the existing helper style

It is **not** a public “just drop this in and it will register itself”
protocol yet.

## Files

- `request-example.json`: sample action envelope
- `go/main.go`: Go helper stub
- `python/server.py`: Python helper stub
- `node/server.mjs`: Node.js helper stub

## Contract Pattern

- `GET /healthz` returns basic liveness
- `POST /sample-host/v1` accepts action-routed JSON
- `Authorization: Bearer <token>` is used when a token is configured

Sample request:

```json
{
  "action": "create_session",
  "session_id": "",
  "params": {
    "label": "demo"
  }
}
```

Sample response:

```json
{
  "ok": true,
  "session_id": "sess-demo-1",
  "data": {
    "session_id": "sess-demo-1"
  }
}
```

## When To Use This Pattern

- device or hardware control
- stateful remote helpers
- long-lived session resources
- anything that should not live inside the main HopClaw process

## When Not To Use It

Do not start here if a simpler lane works:

- use `SKILL.md` for prompt and workflow packaging
- use a webhook integration for stateless HTTP callbacks
- use an external HTTP tool for request/response style services
- use a stdio channel plugin for messaging bridges

# Enterprise Bridge Template

This example shows the smallest business-neutral bridge that lets HopClaw
integrate with an outer enterprise system without modifying HopClaw core.

It demonstrates two concrete surfaces:

- external authorization decisions
- outbound audit event delivery

## Included Files

- [`go/main.go`](./go/main.go): minimal bridge server
- [`hopclaw.enterprise.yaml`](./hopclaw.enterprise.yaml): example HopClaw config

## Run The Bridge

```bash
export BRIDGE_SHARED_TOKEN=dev-bridge-token
go run ./examples/enterprise-bridge-template/go
```

The server listens on `127.0.0.1:18081`.

## Start HopClaw Against It

```bash
export HOPCLAW_AUTH_TOKEN=dev-hopclaw-token
export HOPCLAW_OPERATOR_KEY=dev-operator-key
export BRIDGE_SHARED_TOKEN=dev-bridge-token

hopclaw serve --config examples/enterprise-bridge-template/hopclaw.enterprise.yaml
```

## What It Proves

- HopClaw can delegate AuthZ over HTTP through `authz.webhook`
- HopClaw can export audit events through `runtime.audit.sinks[].webhook`
- neither capability requires modifying HopClaw core
- neither capability affects `toC` users unless they opt into the config

## Real Deployment Notes

Replace the sample handlers with your actual systems:

- policy / RBAC / ABAC backend
- SIEM or audit collector
- portal or ChatOps edge

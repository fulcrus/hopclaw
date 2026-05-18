# Webhook Template

This template shows the simplest HTTP-based integration pattern:

- your service sends inbound events into HopClaw
- HopClaw calls your service back with outbound replies

## Files

- `callback_server.py`: minimal HTTP server that receives HopClaw callback
  deliveries
- `go/callback_server.go`: the same callback server pattern in Go
- `node/callback_server.mjs`: the same callback server pattern in Node.js
- `send_inbound.sh`: shell script that posts a sample inbound message to
  HopClaw

## HopClaw Config

Add a webhook instance to your config:

```yaml
channels:
  webhook:
    enabled: true
    instances:
      sample-webhook:
        callback_url: "http://127.0.0.1:18081/callback"
        secret: "replace-me"
```

## Quick Start

1. Start the callback server:

```sh
python3 examples/webhook-template/callback_server.py
go run ./examples/webhook-template/go
node examples/webhook-template/node/callback_server.mjs
```

2. Start HopClaw with the webhook instance enabled.

3. Send a sample inbound event:

```sh
HOPCLAW_AUTH_TOKEN=change-me \
examples/webhook-template/send_inbound.sh
```

4. Watch the callback server log the response payload that HopClaw POSTs back.

## Environment Variables Used By `send_inbound.sh`

- `HOPCLAW_BASE_URL`: default `http://127.0.0.1:16280`
- `HOPCLAW_WEBHOOK_ID`: default `sample-webhook`
- `HOPCLAW_AUTH_TOKEN`: optional bearer token for protected gateway routes

## What To Customize

- callback payload schema
- auth and signature verification
- retry behavior
- event deduplication keys
- thread or conversation correlation in `metadata`

## Production Notes

- verify the `X-HopClaw-Signature` header if you configure a shared secret
- return a 2xx response only after durable acceptance of the callback
- make inbound posts idempotent when your source system retries aggressively

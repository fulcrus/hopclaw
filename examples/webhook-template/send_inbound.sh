#!/bin/sh

set -eu

base_url="${HOPCLAW_BASE_URL:-http://127.0.0.1:16280}"
webhook_id="${HOPCLAW_WEBHOOK_ID:-sample-webhook}"
token="${HOPCLAW_AUTH_TOKEN:-}"

if [ -n "$token" ]; then
  exec curl -fsS -X POST \
    "$base_url/channels/webhook/$webhook_id/inbound" \
    -H "Content-Type: application/json" \
    -H "Authorization: Bearer $token" \
    -d '{
      "sender_id": "template-user-1",
      "sender_name": "Webhook Template",
      "content": "Hello from the webhook template",
      "metadata": {
        "thread_id": "thread-template-1",
        "source": "examples/webhook-template/send_inbound.sh"
      }
    }'
fi

exec curl -fsS -X POST \
  "$base_url/channels/webhook/$webhook_id/inbound" \
  -H "Content-Type: application/json" \
  -d '{
    "sender_id": "template-user-1",
    "sender_name": "Webhook Template",
    "content": "Hello from the webhook template",
    "metadata": {
      "thread_id": "thread-template-1",
      "source": "examples/webhook-template/send_inbound.sh"
    }
  }'

#!/bin/sh

set -eu

endpoint="${HOPCLAW_TOOL_ENDPOINT:-http://127.0.0.1:18082/invoke}"

exec curl -fsS -X POST "$endpoint" \
  -H "Content-Type: application/json" \
  -d '{
    "protocol_version": "hopclaw.tool/v1",
    "tool_name": "sample.echo",
    "tool_call_id": "call_template_1",
    "session_id": "sess_template_1",
    "run_id": "run_template_1",
    "input": {
      "text": "hello from hopclaw template"
    }
  }'

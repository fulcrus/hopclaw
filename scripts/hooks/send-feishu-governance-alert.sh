#!/usr/bin/env sh
set -eu

SCRIPT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
. "$SCRIPT_DIR/common.sh"

hopclaw_hooks_require_python

payload_file=$(hopclaw_hooks_read_stdin)
body_file=$(hopclaw_hooks_tempfile)
trap 'rm -f "$payload_file" "$body_file"' EXIT HUP INT TERM

python3 - "$payload_file" "$body_file" <<'PY'
import json
import sys

payload_path, body_path = sys.argv[1], sys.argv[2]
with open(payload_path, "r", encoding="utf-8") as handle:
    payload = json.load(handle)

def field(name, default="-"):
    value = payload.get(name)
    if value is None:
        return default
    text = str(value).strip()
    return text if text else default

message = "\n".join([
    "[HopClaw] Governance dead-letter alert",
    f"Adapter: {field('adapter_name')}",
    f"Delivery: {field('delivery_id')}",
    f"Attempts: {field('delivery_attempts', '0')}/{field('delivery_max_attempts', '0')}",
    f"Run: {field('run_id')}",
    f"Session: {field('session_id')}",
    f"Event: {field('event_type')}",
    f"Error: {field('error', 'no error detail provided')}",
])

body = {
    "msg_type": "text",
    "content": {
        "text": message,
    },
}

with open(body_path, "w", encoding="utf-8") as handle:
    json.dump(body, handle, ensure_ascii=False)
PY

if [ -z "${FEISHU_WEBHOOK_URL:-}" ]; then
  dest=$(hopclaw_hooks_write_outbox "feishu-governance-alert" "$body_file") || {
    echo "Set FEISHU_WEBHOOK_URL or HOOK_OUTBOX_DIR before running this script" >&2
    exit 1
  }
  echo "Feishu governance alert payload written to $dest"
  exit 0
fi

hopclaw_hooks_post_json "$FEISHU_WEBHOOK_URL" "$body_file"
echo "Feishu governance alert sent"

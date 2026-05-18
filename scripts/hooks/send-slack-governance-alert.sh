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
import os
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

delivery_id = field("delivery_id")
adapter_name = field("adapter_name")
status = field("delivery_status")
attempts = field("delivery_attempts", "0")
max_attempts = field("delivery_max_attempts", "0")
run_id = field("run_id")
session_id = field("session_id")
event_type = field("event_type")
error_text = field("error", "no error detail provided")

lines = [
    f"[HopClaw] Governance delivery {status}",
    f"Adapter: {adapter_name}",
    f"Delivery: {delivery_id}",
    f"Attempts: {attempts}/{max_attempts}",
    f"Run: {run_id}",
    f"Session: {session_id}",
    f"Event: {event_type}",
    f"Error: {error_text}",
]

body = {"text": "\n".join(lines)}
channel = os.environ.get("SLACK_CHANNEL", "").strip()
username = os.environ.get("SLACK_USERNAME", "").strip()
icon_emoji = os.environ.get("SLACK_ICON_EMOJI", "").strip()
if channel:
    body["channel"] = channel
if username:
    body["username"] = username
if icon_emoji:
    body["icon_emoji"] = icon_emoji

with open(body_path, "w", encoding="utf-8") as handle:
    json.dump(body, handle, ensure_ascii=False)
PY

if [ -z "${SLACK_WEBHOOK_URL:-}" ]; then
  dest=$(hopclaw_hooks_write_outbox "slack-governance-alert" "$body_file") || {
    echo "Set SLACK_WEBHOOK_URL or HOOK_OUTBOX_DIR before running this script" >&2
    exit 1
  }
  echo "Slack governance alert payload written to $dest"
  exit 0
fi

hopclaw_hooks_post_json "$SLACK_WEBHOOK_URL" "$body_file"
echo "Slack governance alert sent"

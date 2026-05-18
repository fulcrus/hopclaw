#!/usr/bin/env sh
set -eu

SCRIPT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
. "$SCRIPT_DIR/common.sh"

hopclaw_hooks_require_python

: "${EMAIL_TO:?set EMAIL_TO before running this script}"

payload_file=$(hopclaw_hooks_read_stdin)
message_file=$(hopclaw_hooks_tempfile)
trap 'rm -f "$payload_file" "$message_file"' EXIT HUP INT TERM

python3 - "$payload_file" "$message_file" <<'PY'
import json
import os
import sys

payload_path, message_path = sys.argv[1], sys.argv[2]
with open(payload_path, "r", encoding="utf-8") as handle:
    payload = json.load(handle)

def field(name, default="-"):
    value = payload.get(name)
    if value is None:
        return default
    text = str(value).strip()
    return text if text else default

mail_from = os.environ.get("EMAIL_FROM", "hopclaw@example.local").strip() or "hopclaw@example.local"
mail_to = os.environ["EMAIL_TO"].strip()
subject = f"[HopClaw] Governance {field('delivery_status').upper()} · {field('adapter_name')} · {field('delivery_id')}"
body = "\n".join([
    "HopClaw governance alert",
    "",
    f"Adapter: {field('adapter_name')}",
    f"Delivery: {field('delivery_id')}",
    f"Status: {field('delivery_status')}",
    f"Attempts: {field('delivery_attempts', '0')}/{field('delivery_max_attempts', '0')}",
    f"Run: {field('run_id')}",
    f"Session: {field('session_id')}",
    f"Event: {field('event_type')}",
    f"Source event: {field('source_event_type')} · {field('source_event_id')}",
    "",
    f"Error: {field('error', 'no error detail provided')}",
    "",
    "Raw payload:",
    json.dumps(payload, ensure_ascii=False, indent=2),
    "",
])

with open(message_path, "w", encoding="utf-8") as handle:
    handle.write(f"From: {mail_from}\n")
    handle.write(f"To: {mail_to}\n")
    handle.write(f"Subject: {subject}\n")
    handle.write("Content-Type: text/plain; charset=utf-8\n")
    handle.write("\n")
    handle.write(body)
PY

sendmail_bin=${SENDMAIL_BIN:-}
if [ -z "$sendmail_bin" ] && command -v sendmail >/dev/null 2>&1; then
  sendmail_bin=$(command -v sendmail)
fi

if [ -n "${HOOK_OUTBOX_DIR:-}" ] && [ "${FORCE_SENDMAIL:-0}" != "1" ]; then
  dest=$(hopclaw_hooks_write_outbox "governance-email-alert" "$message_file" "eml")
  echo "Governance alert email written to $dest"
  exit 0
fi

if [ -n "$sendmail_bin" ]; then
  "$sendmail_bin" -t <"$message_file"
  echo "Governance alert email submitted via $sendmail_bin"
  exit 0
fi

dest=$(hopclaw_hooks_write_outbox "governance-email-alert" "$message_file" "eml") || {
  echo "Set SENDMAIL_BIN or HOOK_OUTBOX_DIR before running this script" >&2
  exit 1
}
echo "Governance alert email written to $dest"

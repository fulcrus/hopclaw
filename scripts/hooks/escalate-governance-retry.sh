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

attempts = int(payload.get("delivery_attempts") or 0)
max_attempts = int(payload.get("delivery_max_attempts") or 0)
severity = "high" if max_attempts and attempts >= max_attempts - 1 else "medium"

body = {
    "source": "hopclaw",
    "kind": "governance_retry_escalation",
    "dedupe_key": f"hopclaw:governance:retry:{field('delivery_id')}",
    "severity": os.environ.get("RETRY_ESCALATION_SEVERITY", severity).strip() or severity,
    "summary": f"Governance delivery retry scheduled for {field('adapter_name')}",
    "details": {
        "delivery_id": field("delivery_id"),
        "adapter_name": field("adapter_name"),
        "delivery_status": field("delivery_status"),
        "delivery_attempts": attempts,
        "delivery_max_attempts": max_attempts,
        "next_attempt_at": field("next_attempt_at"),
        "run_id": field("run_id"),
        "session_id": field("session_id"),
        "event_type": field("event_type"),
        "error": field("error", "no error detail provided"),
    },
    "raw_payload": payload,
}

with open(body_path, "w", encoding="utf-8") as handle:
    json.dump(body, handle, ensure_ascii=False)
PY

if [ -z "${RETRY_ESCALATION_WEBHOOK_URL:-}" ]; then
  dest=$(hopclaw_hooks_write_outbox "governance-retry-escalation" "$body_file") || {
    echo "Set RETRY_ESCALATION_WEBHOOK_URL or HOOK_OUTBOX_DIR before running this script" >&2
    exit 1
  }
  echo "Governance retry escalation payload written to $dest"
  exit 0
fi

hopclaw_hooks_post_json "$RETRY_ESCALATION_WEBHOOK_URL" "$body_file"
echo "Governance retry escalation sent"

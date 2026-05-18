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

body = {
    "source": "hopclaw",
    "kind": "governance_dead_letter",
    "dedupe_key": f"hopclaw:governance:{field('delivery_id')}",
    "service": os.environ.get("INCIDENT_SERVICE", "hopclaw-governance").strip() or "hopclaw-governance",
    "severity": os.environ.get("INCIDENT_SEVERITY", "high").strip() or "high",
    "summary": f"Governance delivery entered dead-letter on {field('adapter_name')}",
    "details": {
        "delivery_id": field("delivery_id"),
        "adapter_name": field("adapter_name"),
        "delivery_status": field("delivery_status"),
        "delivery_attempts": field("delivery_attempts", "0"),
        "delivery_max_attempts": field("delivery_max_attempts", "0"),
        "governance_kind": field("governance_kind"),
        "source_event_id": field("source_event_id"),
        "source_event_type": field("source_event_type"),
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

if [ -z "${INCIDENT_WEBHOOK_URL:-}" ]; then
  dest=$(hopclaw_hooks_write_outbox "governance-incident" "$body_file") || {
    echo "Set INCIDENT_WEBHOOK_URL or HOOK_OUTBOX_DIR before running this script" >&2
    exit 1
  }
  echo "Governance incident payload written to $dest"
  exit 0
fi

hopclaw_hooks_post_json "$INCIDENT_WEBHOOK_URL" "$body_file"
echo "Governance incident created"

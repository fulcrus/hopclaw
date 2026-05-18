#!/usr/bin/env python3
import json
import sys


def main() -> int:
    raw = sys.stdin.read()
    payload = json.loads(raw) if raw.strip() else {}
    hook_context = payload.get("_hook_context") or {}
    summary = {
        "ok": True,
        "language": "python",
        "event_type": payload.get("event_type", ""),
        "run_id": payload.get("run_id", ""),
        "phase": hook_context.get("phase", ""),
        "message": "Handled %s for run %s"
        % (payload.get("event_type", "unknown"), payload.get("run_id", "unknown")),
    }
    json.dump(summary, sys.stdout, ensure_ascii=True, indent=2)
    sys.stdout.write("\n")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())

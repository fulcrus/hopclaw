#!/usr/bin/env python3
import argparse
import json
import os
import sys
from datetime import datetime, timezone
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer


def utc_timestamp() -> str:
    return datetime.now(timezone.utc).strftime("%Y%m%dT%H%M%SZ")


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(description="Receive HopClaw hook HTTP payloads and write them to disk or stdout.")
    parser.add_argument("--host", default="127.0.0.1")
    parser.add_argument("--port", type=int, default=8787)
    parser.add_argument("--outbox-dir", default="")
    parser.add_argument("--status-code", type=int, default=204)
    return parser.parse_args()


def make_handler(outbox_dir: str, status_code: int):
    class Handler(BaseHTTPRequestHandler):
        def log_message(self, fmt: str, *args) -> None:
            sys.stderr.write("%s - - [%s] %s\n" % (self.client_address[0], self.log_date_time_string(), fmt % args))

        def do_POST(self) -> None:
            length = int(self.headers.get("Content-Length", "0") or "0")
            raw = self.rfile.read(length)
            text = raw.decode("utf-8", errors="replace")
            envelope = {
                "received_at": datetime.now(timezone.utc).isoformat(),
                "path": self.path,
                "headers": {key: value for key, value in self.headers.items()},
                "body": json.loads(text) if text.strip() else {},
            }
            if outbox_dir:
                os.makedirs(outbox_dir, exist_ok=True)
                dest = os.path.join(outbox_dir, f"sample-webhook-{utc_timestamp()}.json")
                with open(dest, "w", encoding="utf-8") as handle:
                    json.dump(envelope, handle, ensure_ascii=False, indent=2)
                sys.stdout.write(dest + "\n")
                sys.stdout.flush()
            else:
                json.dump(envelope, sys.stdout, ensure_ascii=False, indent=2)
                sys.stdout.write("\n")
                sys.stdout.flush()
            self.send_response(status_code)
            self.end_headers()

    return Handler


def main() -> int:
    args = parse_args()
    server = ThreadingHTTPServer((args.host, args.port), make_handler(args.outbox_dir.strip(), args.status_code))
    print(f"HopClaw sample webhook server listening on http://{args.host}:{args.port}", flush=True)
    try:
        server.serve_forever()
    except KeyboardInterrupt:
        pass
    finally:
        server.server_close()
    return 0


if __name__ == "__main__":
    raise SystemExit(main())

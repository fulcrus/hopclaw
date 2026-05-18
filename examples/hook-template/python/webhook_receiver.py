#!/usr/bin/env python3
import json
import sys
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer


class Handler(BaseHTTPRequestHandler):
    def log_message(self, fmt: str, *args) -> None:
        sys.stderr.write("%s - - [%s] %s\n" % (self.client_address[0], self.log_date_time_string(), fmt % args))

    def do_POST(self) -> None:
        length = int(self.headers.get("Content-Length", "0") or "0")
        raw = self.rfile.read(length)
        text = raw.decode("utf-8", errors="replace")
        payload = json.loads(text) if text.strip() else {}
        print("=== HopClaw hook received ===", flush=True)
        print("path:", self.path, flush=True)
        print("headers:", dict(self.headers.items()), flush=True)
        print(json.dumps(payload, ensure_ascii=True, indent=2), flush=True)
        self.send_response(204)
        self.end_headers()


def main() -> int:
    server = ThreadingHTTPServer(("127.0.0.1", 18084), Handler)
    print("Hook receiver listening on http://127.0.0.1:18084", flush=True)
    try:
        server.serve_forever()
    except KeyboardInterrupt:
        pass
    finally:
        server.server_close()
    return 0


if __name__ == "__main__":
    raise SystemExit(main())

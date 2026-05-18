#!/usr/bin/env python3

import json
from http.server import BaseHTTPRequestHandler, HTTPServer


class CallbackHandler(BaseHTTPRequestHandler):
    def do_POST(self):
        length = int(self.headers.get("Content-Length", "0"))
        body = self.rfile.read(length).decode("utf-8") if length else ""
        payload = None
        if body:
            try:
                payload = json.loads(body)
            except json.JSONDecodeError:
                payload = body

        print("=== HopClaw callback received ===")
        print("path:", self.path)
        print("headers:", dict(self.headers))
        print("payload:", json.dumps(payload, indent=2) if isinstance(payload, dict) else payload)

        self.send_response(200)
        self.send_header("Content-Type", "application/json")
        self.end_headers()
        self.wfile.write(b'{"ok":true}')

    def log_message(self, fmt, *args):
        return


def main():
    server = HTTPServer(("127.0.0.1", 18081), CallbackHandler)
    print("listening on http://127.0.0.1:18081/callback")
    server.serve_forever()


if __name__ == "__main__":
    main()

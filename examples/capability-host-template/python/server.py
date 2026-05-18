#!/usr/bin/env python3

import json
import os
from http.server import BaseHTTPRequestHandler, HTTPServer


TOKEN = os.getenv("SAMPLE_HOST_TOKEN", "").strip()


class HostHandler(BaseHTTPRequestHandler):
    def do_GET(self):
        if self.path != "/healthz":
            self.send_response(404)
            self.end_headers()
            return
        self.send_response(200)
        self.send_header("Content-Type", "application/json")
        self.end_headers()
        self.wfile.write(b'{"ok":true}')

    def do_POST(self):
        if self.path != "/sample-host/v1":
            self.send_response(404)
            self.end_headers()
            return
        if TOKEN and self.headers.get("Authorization", "").strip() != f"Bearer {TOKEN}":
            self.send_response(401)
            self.end_headers()
            return

        length = int(self.headers.get("Content-Length", "0"))
        body = self.rfile.read(length).decode("utf-8") if length else "{}"
        payload = json.loads(body)
        action = (payload.get("action") or "").strip()

        response = {"ok": True}
        if action == "create_session":
            response["session_id"] = "sess-demo-python-1"
            response["data"] = {"session_id": "sess-demo-python-1"}
        elif action == "ping":
            response["data"] = {"message": "pong", "language": "python"}
        else:
            response["ok"] = False
            response["error"] = "unsupported action"

        self.send_response(200)
        self.send_header("Content-Type", "application/json")
        self.end_headers()
        self.wfile.write(json.dumps(response).encode("utf-8"))

    def log_message(self, fmt, *args):
        return


def main():
    server = HTTPServer(("127.0.0.1", 18083), HostHandler)
    print("listening on http://127.0.0.1:18083/sample-host/v1")
    server.serve_forever()


if __name__ == "__main__":
    main()

#!/usr/bin/env python3

import json
from http.server import BaseHTTPRequestHandler, HTTPServer


class ToolHandler(BaseHTTPRequestHandler):
    def do_POST(self):
        if self.path != "/invoke":
            self.send_response(404)
            self.end_headers()
            return

        length = int(self.headers.get("Content-Length", "0"))
        body = self.rfile.read(length).decode("utf-8") if length else "{}"
        payload = json.loads(body)
        text = str((payload.get("input") or {}).get("text", ""))

        response = {
            "protocol_version": "hopclaw.tool/v1",
            "ok": True,
            "status": "success",
            "summary": "Echoed input text",
            "content": f"Echo: {text}",
            "data": {
                "echoed_text": text,
                "tool_name": payload.get("tool_name"),
            },
        }

        self.send_response(200)
        self.send_header("Content-Type", "application/json")
        self.end_headers()
        self.wfile.write(json.dumps(response).encode("utf-8"))

    def log_message(self, fmt, *args):
        return


def main():
    server = HTTPServer(("127.0.0.1", 18082), ToolHandler)
    print("listening on http://127.0.0.1:18082/invoke")
    server.serve_forever()


if __name__ == "__main__":
    main()

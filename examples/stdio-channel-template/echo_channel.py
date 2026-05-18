#!/usr/bin/env python3

import json
import sys


JSONRPC = "2.0"
PROTOCOL_VERSION = "2025-03-15"


def write_message(message):
    sys.stdout.write(json.dumps(message) + "\n")
    sys.stdout.flush()


def respond_ok(msg_id, result):
    write_message({
        "jsonrpc": JSONRPC,
        "id": msg_id,
        "result": result,
    })


def notify(method, params):
    write_message({
        "jsonrpc": JSONRPC,
        "method": method,
        "params": params,
    })


def handle_initialize(msg):
    respond_ok(msg.get("id"), {
        "protocol_version": PROTOCOL_VERSION,
        "plugin_name": "sample-echo-channel",
        "plugin_version": "0.1.0",
        "capabilities": {
            "send_text": True,
            "send_rich_text": False,
            "send_file": False,
            "edit": False,
            "delete": False,
            "react": False,
            "history": False,
        },
    })


def handle_connect(msg):
    params = msg.get("params") or {}
    config = params.get("config") or {}
    channel_id = config.get("channel_id", "sample-echo")
    respond_ok(msg.get("id"), {"ok": True})
    notify("channel/status", {
        "status": "connected",
        "message": "connected to template channel " + channel_id,
    })


def handle_disconnect(msg):
    respond_ok(msg.get("id"), {"ok": True})
    notify("channel/status", {
        "status": "disconnected",
        "message": "template channel disconnected",
    })


def handle_send(msg):
    params = msg.get("params") or {}
    channel_id = params.get("channel_id", "sample-echo")
    target_id = params.get("target_id", "unknown")
    content = params.get("content", "")
    metadata = params.get("metadata") or {}

    respond_ok(msg.get("id"), {
        "ok": True,
        "message_id": "echo-msg-1",
    })

    echo_prefix = metadata.get("echo_prefix", "Echo")
    notify("channel/inbound", {
        "channel_id": channel_id,
        "sender_id": target_id,
        "sender_name": "Echo Template",
        "content": f"{echo_prefix}: {content}",
        "raw_event": {
            "template": True,
            "source": "examples/stdio-channel-template/echo_channel.py",
        },
    })


def main():
    for line in sys.stdin:
        line = line.strip()
        if not line:
            continue
        msg = json.loads(line)
        method = msg.get("method")

        if method == "initialize":
            handle_initialize(msg)
        elif method == "connect":
            handle_connect(msg)
        elif method == "disconnect":
            handle_disconnect(msg)
            break
        elif method == "send":
            handle_send(msg)
        else:
            write_message({
                "jsonrpc": JSONRPC,
                "id": msg.get("id"),
                "error": {
                    "code": -32601,
                    "message": "method not found",
                },
            })


if __name__ == "__main__":
    main()

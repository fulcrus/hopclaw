# Stdio Channel Template

This template is the reference skeleton for a process-isolated channel plugin.

It speaks the current HopClaw stdio protocol:

- JSON-RPC 2.0
- one JSON message per line
- requests from HopClaw over stdin
- responses and notifications from the plugin over stdout

## Files

- `hopclaw.plugin.yaml`: plugin manifest that tells HopClaw how to start the
  plugin
- `echo_channel.py`: minimal Python implementation of the stdio protocol
- `node/echo_channel.mjs`: the same idea in Node.js
- `go/main.go`: the same idea in Go
- `hopclaw.plugin.node.yaml`: alternate manifest for the Node.js implementation
- `hopclaw.plugin.go.yaml`: alternate manifest for the Go implementation

## Quick Start

1. Make the example plugin executable:

```sh
chmod +x examples/stdio-channel-template/echo_channel.py
chmod +x examples/stdio-channel-template/node/echo_channel.mjs
```

2. Copy the folder into a plugin directory:

```sh
cp -R examples/stdio-channel-template "$HOME/.hopclaw/plugins/echo-channel"
```

3. Start HopClaw and install or point plugin discovery at that directory.

4. Use the configured channel to send a message. The template plugin will echo
   a synthetic inbound event back into HopClaw.

To switch languages, replace `hopclaw.plugin.yaml` with the matching manifest:

- `hopclaw.plugin.node.yaml`
- `hopclaw.plugin.go.yaml`

## What To Customize

- replace `echo_channel.py` with real platform SDK calls
- map platform users, rooms, and threads to `target_id` and `sender_id`
- translate platform errors into JSON-RPC error or `SendResult.error`
- emit real `channel/status` updates for connect and disconnect events

## Protocol Notes

The minimal methods implemented here are:

- `initialize`
- `connect`
- `disconnect`
- `send`

The minimal notifications emitted here are:

- `channel/status`
- `channel/inbound`

For field names and semantics, see:

- [`../../channels/stdio/protocol.go`](../../channels/stdio/protocol.go)

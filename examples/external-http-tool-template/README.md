# External HTTP Tool Template

This template shows how to expose an HTTP endpoint as a HopClaw tool.

It matches the current stable tool invocation flow implemented by the runtime:

- HopClaw sends a JSON POST to your endpoint
- your service returns either a simple legacy response or a structured
  `hopclaw.tool/v1` response

## Files

- `hopclaw.plugin.yaml`: sample plugin manifest contributing one tool
- `invoke_example.sh`: sample request to the tool server
- `python/tool_server.py`: Python implementation
- `node/tool_server.mjs`: Node.js implementation
- `go/main.go`: Go implementation

## Sample Tool

The template exposes a tool named `sample.echo`.

It accepts:

```json
{
  "text": "hello"
}
```

It returns a structured response containing a summary and normalized payload.

## Quick Start

1. Start one of the tool servers:

```sh
python3 examples/external-http-tool-template/python/tool_server.py
node examples/external-http-tool-template/node/tool_server.mjs
go run ./examples/external-http-tool-template/go
```

2. Point HopClaw at the tool by installing or loading the sample
   `hopclaw.plugin.yaml`.

3. Send a direct request to the server to validate the contract:

```sh
examples/external-http-tool-template/invoke_example.sh
```

## Request Contract

HopClaw sends:

```json
{
  "protocol_version": "hopclaw.tool/v1",
  "tool_name": "sample.echo",
  "tool_call_id": "call_123",
  "session_id": "sess_123",
  "run_id": "run_123",
  "input": {
    "text": "hello"
  }
}
```

## Response Contract

Preferred response:

```json
{
  "protocol_version": "hopclaw.tool/v1",
  "ok": true,
  "status": "success",
  "summary": "Echoed input text",
  "content": "Echo: hello",
  "data": {
    "echoed_text": "hello"
  }
}
```

Legacy response also works:

```json
{
  "output": "Echo: hello",
  "error": ""
}
```

## Production Notes

- validate input before calling upstream systems
- keep response bodies small unless you intentionally return artifacts
- if the tool is read-only, consider a narrower side-effect policy than the
  current external-tool default when you promote it into core

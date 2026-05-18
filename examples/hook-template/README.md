# Hook Template

This template shows the current stable hook contract in HopClaw.

Use it when you want to react to runtime lifecycle events without writing a
core Go contribution:

- send failure or approval alerts to another system
- append normalized events to an audit stream
- run lightweight policy checks before or after tool execution
- prototype callback wiring outside the main runtime

## Files

- `sample_payload.json`: a standalone payload you can pipe into the command
  handlers or POST to the webhook receivers
- `register_command_hook.sh`: creates a command hook through the operator API
- `register_http_hook.sh`: creates an HTTP hook through the operator API
- `hopclaw.plugin.yaml`: sample plugin manifest exposing a packaged HTTP hook
- `hooks/run-failed-http.yaml`: packaged HTTP hook declaration loaded from
  `hooks_dir`
- `python/command_hook.py`: Python command hook example
- `node/command_hook.mjs`: Node.js command hook example
- `go/command_hook/main.go`: Go command hook example
- `python/webhook_receiver.py`: Python HTTP receiver example
- `node/webhook_receiver.mjs`: Node.js HTTP receiver example
- `go/webhook_receiver/main.go`: Go HTTP receiver example

## Hook Contract

- command hooks receive one JSON object on `stdin`
- HTTP hooks receive the same JSON object as an `application/json` POST body
- HopClaw injects `_hook_context` with phase, run/session identifiers, and the
  original payload snapshot

## Quick Start

1. Test one command hook implementation directly:

```sh
cat examples/hook-template/sample_payload.json | \
  python3 examples/hook-template/python/command_hook.py

cat examples/hook-template/sample_payload.json | \
  node examples/hook-template/node/command_hook.mjs

cat examples/hook-template/sample_payload.json | \
  go run ./examples/hook-template/go/command_hook
```

2. Start one HTTP receiver:

```sh
python3 examples/hook-template/python/webhook_receiver.py
node examples/hook-template/node/webhook_receiver.mjs
go run ./examples/hook-template/go/webhook_receiver
```

Each sample listens on `http://127.0.0.1:18084`.

3. Register hooks against a running HopClaw gateway:

```sh
HOPCLAW_AUTH_TOKEN=change-me \
HOPCLAW_HOOK_COMMAND="python3 examples/hook-template/python/command_hook.py" \
sh examples/hook-template/register_command_hook.sh

HOPCLAW_AUTH_TOKEN=change-me \
sh examples/hook-template/register_http_hook.sh
```

4. Inspect or manually re-fire the created hooks:

```sh
hopclaw hooks list
hopclaw hooks recent <hook-id>
hopclaw hooks test-fire <hook-id> --payload '{"run_id":"run_demo_123","event_type":"run.completed"}'
```

## Packaged HTTP Hook Example

`hopclaw.plugin.yaml` plus `hooks/run-failed-http.yaml` show how to ship a
plugin-managed HTTP hook through `hooks_dir`.

Copy the folder into a plugin directory, for example:

```sh
cp -R examples/hook-template "$HOME/.hopclaw/plugins/sample-hooks"
```

Then adjust the receiver URL or secret in `hooks/run-failed-http.yaml` to match
your environment.

## Important Note About Packaged Command Hooks

The runtime stores command hooks as literal shell commands.

That means packaged command hooks should use either:

- an absolute path
- a wrapper command already available on `PATH`

To keep this template portable, the packaged example uses an HTTP hook, while
the command examples are shown through direct API registration.

## What To Customize

- hook `trigger`, `phase`, and retry/timeout behavior
- webhook auth headers and shared secret usage
- downstream deduplication keys such as `run_id`, `event_id`, or `approval_id`
- the command output format if you want a richer local smoke test
- any filtering or policy logic inside pre-phase command hooks

## References

- [`../../hooks/types.go`](../../hooks/types.go)

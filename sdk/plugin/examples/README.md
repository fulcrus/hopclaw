# Plugin SDK Examples

## TL;DR

- `hello-tool` shows the smallest inline tool plugin.
- `hello-channel` shows a Level 0 stdio channel with connect/send hooks.
- `hello-provider` shows a typed chat provider with model discovery.
- Run everything with `go test ./sdk/plugin/...`.

## What Each Example Covers

- `sdk/plugin/examples/hello-tool`: manifest + `Tool()` + `TestHarness.Execute`.
- `sdk/plugin/examples/hello-channel`: manifest + `Channel()` + connect/send lifecycle.
- `sdk/plugin/examples/hello-provider`: manifest + `Provider()` + `ListModels` and `Chat`.

## Copy-Paste Commands

```bash
go test ./sdk/plugin/...
go test ./sdk/plugin/examples/hello-tool
go test ./sdk/plugin/examples/hello-channel
go test ./sdk/plugin/examples/hello-provider
```

## How To Reuse

1. Copy the closest example into a new module.
2. Keep the `Manifest()` function as the single source of plugin metadata.
3. Replace the demo logic inside `Tool()`, `Channel()`, or `Provider()`.
4. Keep `sdkplugin.NewTestHarness(nil)` in tests so `go test` exercises the real code path without a running HopClaw server.

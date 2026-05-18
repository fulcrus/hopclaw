# Plugin SDK Guide

## TL;DR

- Use `hopclaw plugins init <name> --kind <tool|channel|provider|skill>` to scaffold a local starter.
- The typed public SDK lives in `sdk/plugin`.
- Validate the manifest with `hopclaw plugins validate .` and exercise real code paths with `go test ./...`.
- `sdkplugin.NewTestHarness(nil)` and `sdkplugin.NewMockRuntime()` let you test plugins without a running HopClaw server.

English is canonical in this file. 中文同步 follows after the English section.

## What The SDK Covers

The current typed SDK is intentionally small and practical. It gives plugin authors public contracts for:

| Surface | Entry point | Use it when |
| --- | --- | --- |
| Manifest | `sdkplugin.Manifest` | You need a release-grade manifest as the single source of plugin metadata |
| Tool plugin | `ToolPlugin` / `sdkplugin.Tool` | You want to contribute an inline external tool contract |
| Channel plugin | `ChannelPlugin` / `sdkplugin.Channel` | You want to contribute a Level 0 channel bridge with connect/send hooks |
| Provider plugin | `ProviderPlugin` / `sdkplugin.Provider` | You want to expose models and chat behavior |
| Skill plugin | `SkillPlugin` / `sdkplugin.Skill` | You want typed generation of a `SKILL.md` asset |
| Hooks | `sdkplugin.Hook` and `sdkplugin.HookSet` | You need load/unload/config-change lifecycle callbacks |
| Testing | `sdkplugin.NewTestHarness`, `sdkplugin.NewMockRuntime` | You want `go test` to exercise plugin code directly |

## Fastest Way To Start

Scaffold a local tool plugin starter:

```bash
mkdir -p /tmp/hopclaw-plugin-dev
cd /tmp/hopclaw-plugin-dev
hopclaw plugins init hello-tool --kind tool --dir .
cd hello-tool
go test ./...
hopclaw plugins validate .
```

Other starter kinds:

```bash
hopclaw plugins init hello-channel --kind channel --dir .
hopclaw plugins init hello-provider --kind provider --dir .
hopclaw plugins init hello-skill --kind skill --dir .
```

The scaffold command writes:

- `go.mod`
- `hopclaw.plugin.yaml`
- `plugin.go`
- `plugin_test.go`
- for `skill` starters, a generated `skills/<name>/SKILL.md`

## Minimal Project Layout

The scaffolded layout is intentionally small:

```text
my-plugin/
├── go.mod
├── hopclaw.plugin.yaml
├── plugin.go
└── plugin_test.go
```

Skill starters add:

```text
skills/<skill-name>/SKILL.md
```

## The Manifest Is The Contract

The SDK exposes `sdkplugin.Manifest` as the public metadata surface. HopClaw discovers plugin metadata from:

- `hopclaw.plugin.yaml`
- `openclaw.plugin.json`

The loader validates that local directories such as `skills_dir` and `hooks_dir` stay inside the plugin root.

Minimal manifest example:

```yaml
name: hello-tool
version: "1.0.0"
description: Example Level 0 tool plugin built with the HopClaw typed SDK.

tools:
  - name: hello.say
    description: Return a personalized greeting.
    endpoint: inline://hello.say
    input_schema:
      type: object
      properties:
        name:
          type: string
          description: Name to greet.
```

Validate it locally:

```bash
hopclaw plugins validate .
```

## Example: Smallest Useful Tool Plugin

This is the same pattern used by `sdk/plugin/examples/hello-tool`.

```go
package hellotool

import (
	"context"
	"fmt"

	sdkplugin "github.com/fulcrus/hopclaw/sdk/plugin"
)

const ToolName = "hello.say"

type Plugin struct{}

func Manifest() sdkplugin.Manifest {
	manifest := sdkplugin.NewManifest(
		"hello-tool",
		"1.0.0",
		"Example Level 0 tool plugin built with the HopClaw typed SDK.",
	)
	manifest.Tools = []sdkplugin.ToolDecl{{
		Name:        ToolName,
		Description: "Return a personalized greeting.",
		Endpoint:    "inline://hello.say",
	}}
	return manifest
}

func (Plugin) Tool() sdkplugin.Tool {
	return sdkplugin.Tool{
		Decl: Manifest().Tools[0],
		ExecuteFunc: func(ctx context.Context, runtime sdkplugin.PluginRuntime, request sdkplugin.ToolRequest) (sdkplugin.ToolOutput, error) {
			name := fmt.Sprint(request.Input["name"])
			if name == "" || name == "<nil>" {
				name = "world"
			}
			if err := runtime.Emit(ctx, sdkplugin.Event{Name: "hello-tool.executed"}); err != nil {
				return sdkplugin.ToolOutput{}, err
			}
			return sdkplugin.ToolOutput{Output: "Hello, " + name + "!"}, nil
		},
	}
}
```

The SDK examples still import `github.com/fulcrus/hopclaw/sdk/plugin` because that is the current Go module path.
If you reached this page from `https://github.com/fulcrus/hopclaw`, use the public repository URL for browsing and issue tracking, but keep the legacy module path in code until the module migration lands.

Test it without a running gateway:

```go
package hellotool

import (
	"context"
	"testing"

	sdkplugin "github.com/fulcrus/hopclaw/sdk/plugin"
)

func TestPluginToolExecution(t *testing.T) {
	harness := sdkplugin.NewTestHarness(nil)
	output, err := harness.Execute(context.Background(), Plugin{}, sdkplugin.ToolRequest{
		Input: map[string]any{"name": "HopClaw"},
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	sdkplugin.AssertToolOutput(t, output, "Hello, HopClaw!")
}
```

## Channel, Provider, Skill, And Hook Helpers

### Channel plugin

Use `sdkplugin.Channel` when you want typed `Connect` and `Send` hooks.

The shipped `hello-channel` example demonstrates:

- `ConnectFunc`
- `SendFunc`
- emitted events
- test-harness driven send verification

Run it:

```bash
go test ./sdk/plugin/examples/hello-channel
```

### Provider plugin

Use `sdkplugin.Provider` when you want to contribute model discovery and chat behavior.

Useful helpers:

| Helper | What it does |
| --- | --- |
| `sdkplugin.ResolveModel(request, fallback)` | Use the explicit request model when present |
| `sdkplugin.FindLastMessage(messages, role)` | Return the last message for a role |
| `sdkplugin.LastUserMessage(messages)` | Return the last trimmed user message |

Run the example:

```bash
go test ./sdk/plugin/examples/hello-provider
```

### Skill plugin

Use `sdkplugin.Skill` when you want typed generation of `SKILL.md`.

Important methods:

| Method | Purpose |
| --- | --- |
| `Markdown()` | Render the `SKILL.md` file |
| `Files()` | Return generated file contents |
| `WriteToDir(root)` | Materialize the skill files into a plugin skill root |
| `DirectoryName()` | Return the slug used on disk |

Minimal skill snippet:

```go
func (Plugin) Skill() sdkplugin.Skill {
	return sdkplugin.Skill{
		Name:        "hello-skill",
		Description: "Starter skill generated from typed Go code.",
		TLDR:        "Replace the starter workflow with the shortest safe path for your users.",
		Body:        "## Usage\n\n```bash\n# Add copy-pasteable commands here.\n```\n",
	}
}
```

### Hooks

Use `sdkplugin.Hook` or `sdkplugin.HookFuncs` when you need lifecycle callbacks:

- `OnLoad`
- `OnUnload`
- `OnConfigChange`

If you need multiple hooks, compose them with `sdkplugin.HookSet`.

## Runtime Interface

Your plugin implementation receives a `PluginRuntime` interface. It currently exposes:

| Method | Purpose |
| --- | --- |
| `Manifest()` | Read the current manifest view |
| `Config()` | Read plugin-scoped config |
| `LookupEnv(key)` | Read environment variables injected into runtime |
| `Emit(ctx, event)` | Emit structured plugin events |
| `Logf(format, args...)` | Write runtime logs |

Useful helper:

```go
value, err := sdkplugin.ConfigValue(runtime, "prefix")
```

That returns `sdkplugin.ErrConfigKeyAbsent` when the key does not exist.

## Validation Workflow

Use both manifest validation and normal Go tests:

```bash
go test ./...
hopclaw plugins validate .
```

`sdkplugin.ValidateManifest` performs lightweight public-contract checks such as:

- manifest name presence
- loose version format validation
- required provider API declarations
- required plugin command metadata

It does not replace runtime loading or your own behavioral tests.

## Discovery Paths

HopClaw discovers plugins in these standard locations:

```text
./.hopclaw/plugins
./.hopclaw/extensions
./.openclaw/plugins
./.openclaw/extensions
./extensions
~/.hopclaw/plugins
~/.hopclaw/extensions
~/.openclaw/plugins
~/.openclaw/extensions
```

That means a practical local development flow is:

```bash
mkdir -p ./.hopclaw/plugins
mv ./hello-tool ./.hopclaw/plugins/
hopclaw plugins list
hopclaw plugins info hello-tool
```

## Reference Commands

These are the most useful SDK-related commands in the main CLI:

```bash
hopclaw plugins init hello-tool --kind tool --dir .
hopclaw plugins validate .
hopclaw plugins list
hopclaw plugins info hello-tool
go test ./sdk/plugin/...
go test ./sdk/plugin/examples/hello-tool
go test ./sdk/plugin/examples/hello-channel
go test ./sdk/plugin/examples/hello-provider
```

## Recommended Development Loop

1. Scaffold the nearest starter with `hopclaw plugins init`.
2. Keep `Manifest()` as the single source of plugin metadata.
3. Add real runtime behavior in `Tool()`, `Channel()`, `Provider()`, or `Skill()`.
4. Use `sdkplugin.NewTestHarness(nil)` in tests so `go test` exercises the real plugin code path.
5. Run `hopclaw plugins validate .` before packaging or sharing.

## 中文同步

### TL;DR

- 用 `hopclaw plugins init <name> --kind <tool|channel|provider|skill>` 生成插件脚手架。
- 类型化 SDK 在 `sdk/plugin`。
- 先跑 `go test ./...`，再跑 `hopclaw plugins validate .`。
- `sdkplugin.NewTestHarness(nil)` 和 `sdkplugin.NewMockRuntime()` 可以在没有运行中网关的情况下直接测插件逻辑。

### 最常用命令

```bash
hopclaw plugins init hello-tool --kind tool --dir .
go test ./...
hopclaw plugins validate .
hopclaw plugins list
```

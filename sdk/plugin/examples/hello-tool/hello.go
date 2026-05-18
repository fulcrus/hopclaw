package hellotool

import (
	"context"
	"fmt"
	"strings"

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
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"name": map[string]any{
					"type":        "string",
					"description": "Name to greet.",
				},
			},
		},
	}}
	return manifest
}

func (Plugin) Tool() sdkplugin.Tool {
	return sdkplugin.Tool{
		Decl: Manifest().Tools[0],
		ExecuteFunc: func(ctx context.Context, runtime sdkplugin.PluginRuntime, request sdkplugin.ToolRequest) (sdkplugin.ToolOutput, error) {
			name := stringInput(request.Input, "name", "world")
			prefix := stringConfig(runtime.Config(), "prefix", "Hello")
			greeting := fmt.Sprintf("%s, %s!", prefix, name)
			if err := runtime.Emit(ctx, sdkplugin.Event{
				Name: "hello-tool.executed",
				Payload: map[string]any{
					"name": name,
					"tool": ToolName,
				},
			}); err != nil {
				return sdkplugin.ToolOutput{}, err
			}
			runtime.Logf("hello-tool greeted %s", name)
			return sdkplugin.ToolOutput{
				Output: greeting,
				Structured: map[string]any{
					"greeting": greeting,
					"name":     name,
					"tool":     ToolName,
				},
			}, nil
		},
	}
}

func stringInput(values map[string]any, key string, fallback string) string {
	if len(values) == 0 {
		return fallback
	}
	raw, ok := values[key]
	if !ok {
		return fallback
	}
	text := strings.TrimSpace(fmt.Sprint(raw))
	if text == "" || text == "<nil>" {
		return fallback
	}
	return text
}

func stringConfig(values map[string]any, key string, fallback string) string {
	if len(values) == 0 {
		return fallback
	}
	raw, ok := values[key]
	if !ok {
		return fallback
	}
	text := strings.TrimSpace(fmt.Sprint(raw))
	if text == "" || text == "<nil>" {
		return fallback
	}
	return text
}

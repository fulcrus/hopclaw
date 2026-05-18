package plugin

import (
	"context"
	"errors"
	"testing"
)

type testToolPlugin struct {
	tool Tool
}

func (p testToolPlugin) Tool() Tool {
	return p.tool
}

func TestToolExecuteAndManifest(t *testing.T) {
	t.Parallel()

	plugin := testToolPlugin{
		tool: Tool{
			Decl: ToolDecl{
				Name:        "hello.tool",
				Description: "Says hello",
				InputSchema: map[string]any{
					"type": "object",
				},
			},
			ExecuteFunc: func(_ context.Context, runtime PluginRuntime, request ToolRequest) (ToolOutput, error) {
				value, err := ConfigValue(runtime, "prefix")
				if err != nil {
					return ToolOutput{}, err
				}
				return ToolOutput{
					Output: value.(string) + request.Input["name"].(string),
					Structured: map[string]any{
						"ok": true,
					},
				}, nil
			},
		},
	}

	runtime := stubRuntime{
		config: map[string]any{
			"prefix": "hello, ",
		},
	}
	output, err := plugin.Tool().Execute(context.Background(), runtime, ToolRequest{
		Input: map[string]any{
			"name": "hopclaw",
		},
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if output.Output != "hello, hopclaw" {
		t.Fatalf("Execute() Output = %q, want %q", output.Output, "hello, hopclaw")
	}
	if output.Structured["ok"] != true {
		t.Fatalf("Execute() Structured = %#v", output.Structured)
	}

	manifest := plugin.Tool().Manifest()
	manifest.InputSchema["type"] = "string"
	if plugin.tool.Decl.InputSchema["type"] != "object" {
		t.Fatalf("Manifest() mutated original decl = %#v", plugin.tool.Decl)
	}
}

func TestToolExecuteNotImplementedAndNilRuntime(t *testing.T) {
	t.Parallel()

	plugin := testToolPlugin{}

	if _, err := plugin.Tool().Execute(context.Background(), nil, ToolRequest{}); !errors.Is(err, ErrNilRuntime) {
		t.Fatalf("Execute(nil) error = %v, want ErrNilRuntime", err)
	}

	runtime := stubRuntime{}
	if _, err := plugin.Tool().Execute(context.Background(), runtime, ToolRequest{}); !errors.Is(err, ErrNotImplemented) {
		t.Fatalf("Execute() error = %v, want ErrNotImplemented", err)
	}
}

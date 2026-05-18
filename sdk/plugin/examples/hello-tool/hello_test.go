package hellotool

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	sdkplugin "github.com/fulcrus/hopclaw/sdk/plugin"
)

func TestManifestIsValid(t *testing.T) {
	t.Parallel()

	manifest := Manifest()
	if errs := sdkplugin.ValidateManifest(manifest); len(errs) != 0 {
		t.Fatalf("ValidateManifest() errors = %#v", errs)
	}
	if manifest.Name != "hello-tool" {
		t.Fatalf("Manifest().Name = %q", manifest.Name)
	}
	if len(manifest.Tools) != 1 || manifest.Tools[0].Name != ToolName {
		t.Fatalf("Manifest().Tools = %#v", manifest.Tools)
	}

	path := filepath.Join(".", "hopclaw.plugin.yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%q) error = %v", path, err)
	}
	text := string(data)
	if !strings.Contains(text, "name: hello-tool") || !strings.Contains(text, "hello.say") {
		t.Fatalf("manifest file content = %q", text)
	}
}

func TestPluginToolExecution(t *testing.T) {
	t.Parallel()

	harness := sdkplugin.NewTestHarness(nil)
	harness.Runtime.SetConfig(map[string]any{"prefix": "Hi"})

	output, err := harness.Execute(context.Background(), Plugin{}, sdkplugin.ToolRequest{
		Input: map[string]any{
			"name": "HopClaw",
		},
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	sdkplugin.AssertToolOutput(t, output, "Hi, HopClaw!")
	if output.Structured["tool"] != ToolName {
		t.Fatalf("Structured = %#v", output.Structured)
	}
	if len(harness.Runtime.Events()) != 1 || harness.Runtime.Events()[0].Name != "hello-tool.executed" {
		t.Fatalf("Events() = %#v", harness.Runtime.Events())
	}
	if len(harness.Runtime.Logs()) != 1 || !strings.Contains(harness.Runtime.Logs()[0], "HopClaw") {
		t.Fatalf("Logs() = %#v", harness.Runtime.Logs())
	}
}

func TestPluginToolUsesDefaults(t *testing.T) {
	t.Parallel()

	harness := sdkplugin.NewTestHarness(nil)
	output, err := harness.Execute(context.Background(), Plugin{}, sdkplugin.ToolRequest{})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	sdkplugin.AssertToolOutput(t, output, "Hello, world!")
}

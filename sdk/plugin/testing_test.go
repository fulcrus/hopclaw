package plugin

import (
	"context"
	"errors"
	"testing"
)

func TestMockRuntimeRecordsEventsAndLogs(t *testing.T) {
	t.Parallel()

	runtime := NewMockRuntime()
	runtime.SetManifest(Manifest{Name: "demo"})
	runtime.SetConfig(map[string]any{"mode": "test"})
	runtime.SetEnv("DEMO_TOKEN", "secret")
	runtime.Logf("hello %s", "hopclaw")
	if err := runtime.Emit(context.Background(), Event{
		Name: "tool.executed",
		Payload: map[string]any{
			"name": "hello.tool",
		},
	}); err != nil {
		t.Fatalf("Emit() error = %v", err)
	}

	if runtime.Manifest().Name != "demo" {
		t.Fatalf("Manifest().Name = %q", runtime.Manifest().Name)
	}
	if runtime.Config()["mode"] != "test" {
		t.Fatalf("Config() = %#v", runtime.Config())
	}
	if value, ok := runtime.LookupEnv("DEMO_TOKEN"); !ok || value != "secret" {
		t.Fatalf("LookupEnv() = %q, %v", value, ok)
	}
	if len(runtime.Logs()) != 1 || runtime.Logs()[0] != "hello hopclaw" {
		t.Fatalf("Logs() = %#v", runtime.Logs())
	}
	if len(runtime.Events()) != 1 || runtime.Events()[0].Name != "tool.executed" {
		t.Fatalf("Events() = %#v", runtime.Events())
	}
}

func TestHarnessRunsToolAndHooks(t *testing.T) {
	t.Parallel()

	runtime := NewMockRuntime()
	runtime.SetConfig(map[string]any{"prefix": "hello, "})
	harness := NewTestHarness(runtime)

	toolPlugin := testToolPlugin{
		tool: Tool{
			ExecuteFunc: func(_ context.Context, runtime PluginRuntime, request ToolRequest) (ToolOutput, error) {
				value, err := ConfigValue(runtime, "prefix")
				if err != nil {
					return ToolOutput{}, err
				}
				runtime.Logf("tool called")
				_ = runtime.Emit(context.Background(), Event{Name: "tool.called"})
				return ToolOutput{Output: value.(string) + request.Input["name"].(string)}, nil
			},
		},
	}

	output, err := harness.Execute(context.Background(), toolPlugin, ToolRequest{
		Input: map[string]any{"name": "sdk"},
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	AssertToolOutput(t, output, "hello, sdk")

	changeSeen := false
	hook := HookFuncs{
		OnLoadFunc: func(context.Context, PluginRuntime) error { return nil },
		OnConfigChangeFunc: func(_ context.Context, _ PluginRuntime, change ConfigChange) error {
			changeSeen = true
			if change.Current["prefix"] != "hi, " {
				t.Fatalf("ConfigChange() Current = %#v", change.Current)
			}
			return nil
		},
	}

	if err := harness.Load(context.Background(), hook); err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if err := harness.ConfigChange(context.Background(), hook, map[string]any{"prefix": "hi, "}); err != nil {
		t.Fatalf("ConfigChange() error = %v", err)
	}
	if !changeSeen {
		t.Fatalf("OnConfigChange() was not called")
	}
	if runtime.Config()["prefix"] != "hi, " {
		t.Fatalf("runtime config = %#v", runtime.Config())
	}
	if len(runtime.Events()) != 1 || runtime.Events()[0].Name != "tool.called" {
		t.Fatalf("runtime events = %#v", runtime.Events())
	}
}

func TestHarnessListModels(t *testing.T) {
	t.Parallel()

	harness := NewTestHarness(nil)
	provider := testProviderPlugin{
		provider: Provider{
			ModelsFunc: func(context.Context, PluginRuntime) ([]ModelInfo, error) {
				return []ModelInfo{{
					ID:            "demo-chat",
					DisplayName:   "Demo Chat",
					ContextWindow: 128000,
					Capabilities:  []string{"chat"},
				}}, nil
			},
		},
	}

	models, err := harness.ListModels(context.Background(), provider)
	if err != nil {
		t.Fatalf("ListModels() error = %v", err)
	}
	if len(models) != 1 || models[0].ID != "demo-chat" {
		t.Fatalf("ListModels() = %#v", models)
	}
	models[0].Capabilities[0] = "mutated"

	again, err := harness.Models(context.Background(), provider)
	if err != nil {
		t.Fatalf("Models() error = %v", err)
	}
	if again[0].Capabilities[0] != "chat" {
		t.Fatalf("Models() capabilities mutated = %#v", again)
	}
}

func TestMockRuntimeHelpers(t *testing.T) {
	t.Parallel()

	runtime := NewMockRuntime()
	if _, ok := runtime.LastEvent(); ok {
		t.Fatal("LastEvent() unexpectedly returned an event")
	}
	if got := runtime.EventsNamed("missing"); got != nil {
		t.Fatalf("EventsNamed(missing) = %#v, want nil", got)
	}

	_ = runtime.Emit(context.Background(), Event{Name: "alpha"})
	_ = runtime.Emit(context.Background(), Event{Name: "beta"})
	_ = runtime.Emit(context.Background(), Event{Name: "beta"})

	last, ok := runtime.LastEvent()
	if !ok || last.Name != "beta" {
		t.Fatalf("LastEvent() = %#v, %v", last, ok)
	}
	if got := runtime.EventsNamed("beta"); len(got) != 2 {
		t.Fatalf("EventsNamed(beta) = %#v", got)
	}

	runtime.Reset()
	if runtime.Manifest().Name != "" || runtime.Config() != nil || runtime.Logs() != nil || runtime.Events() != nil {
		t.Fatalf("runtime after Reset() = manifest:%#v config:%#v logs:%#v events:%#v", runtime.Manifest(), runtime.Config(), runtime.Logs(), runtime.Events())
	}
}

func TestHarnessNilPluginsAndEmitErrors(t *testing.T) {
	t.Parallel()

	harness := NewTestHarness(nil)
	if err := harness.Connect(context.Background(), nil); !errors.Is(err, ErrNotImplemented) {
		t.Fatalf("Connect(nil) error = %v, want ErrNotImplemented", err)
	}
	if _, err := harness.Send(context.Background(), nil, OutboundMessage{}); !errors.Is(err, ErrNotImplemented) {
		t.Fatalf("Send(nil) error = %v, want ErrNotImplemented", err)
	}
	if _, err := harness.Execute(context.Background(), nil, ToolRequest{}); !errors.Is(err, ErrNotImplemented) {
		t.Fatalf("Execute(nil) error = %v, want ErrNotImplemented", err)
	}
	if _, err := harness.ListModels(context.Background(), nil); !errors.Is(err, ErrNotImplemented) {
		t.Fatalf("ListModels(nil) error = %v, want ErrNotImplemented", err)
	}
	if _, err := harness.Chat(context.Background(), nil, ChatRequest{}); !errors.Is(err, ErrNotImplemented) {
		t.Fatalf("Chat(nil) error = %v, want ErrNotImplemented", err)
	}

	harness.Runtime.SetEmitError(errors.New("emit failed"))
	toolPlugin := testToolPlugin{
		tool: Tool{
			ExecuteFunc: func(ctx context.Context, runtime PluginRuntime, _ ToolRequest) (ToolOutput, error) {
				if err := runtime.Emit(ctx, Event{Name: "tool.called"}); err != nil {
					return ToolOutput{}, err
				}
				return ToolOutput{Output: "ok"}, nil
			},
		},
	}
	if _, err := harness.Execute(context.Background(), toolPlugin, ToolRequest{}); err == nil || err.Error() != "emit failed" {
		t.Fatalf("Execute() error = %v, want emit failed", err)
	}
}

func TestAssertChatContent(t *testing.T) {
	t.Parallel()

	AssertChatContent(t, ChatResponse{
		Message: ChatMessage{Content: " hello "},
	}, "hello")
}

package plugin

import (
	"context"
	"errors"
	"testing"
)

func TestHookFuncsInvokeCallbacks(t *testing.T) {
	t.Parallel()

	loaded := false
	unloaded := false
	changed := false

	hook := HookFuncs{
		OnLoadFunc: func(context.Context, PluginRuntime) error {
			loaded = true
			return nil
		},
		OnUnloadFunc: func(context.Context, PluginRuntime) error {
			unloaded = true
			return nil
		},
		OnConfigChangeFunc: func(_ context.Context, _ PluginRuntime, change ConfigChange) error {
			changed = true
			change.Previous["mode"] = "mutated"
			if change.Current["mode"] != "new" {
				t.Fatalf("OnConfigChange() Current = %#v", change.Current)
			}
			return nil
		},
	}

	runtime := stubRuntime{}
	change := ConfigChange{
		Previous: map[string]any{"mode": "old"},
		Current:  map[string]any{"mode": "new"},
	}

	if err := hook.OnLoad(context.Background(), runtime); err != nil {
		t.Fatalf("OnLoad() error = %v", err)
	}
	if err := hook.OnUnload(context.Background(), runtime); err != nil {
		t.Fatalf("OnUnload() error = %v", err)
	}
	if err := hook.OnConfigChange(context.Background(), runtime, change); err != nil {
		t.Fatalf("OnConfigChange() error = %v", err)
	}

	if !loaded || !unloaded || !changed {
		t.Fatalf("callbacks invoked = load:%v unload:%v change:%v", loaded, unloaded, changed)
	}
	if change.Previous["mode"] != "old" {
		t.Fatalf("change.Previous mutated = %#v", change.Previous)
	}
}

func TestHookFuncsNilRuntimeAndNoop(t *testing.T) {
	t.Parallel()

	hook := HookFuncs{}

	if err := hook.OnLoad(context.Background(), nil); !errors.Is(err, ErrNilRuntime) {
		t.Fatalf("OnLoad(nil) error = %v, want ErrNilRuntime", err)
	}
	if err := hook.OnUnload(context.Background(), nil); !errors.Is(err, ErrNilRuntime) {
		t.Fatalf("OnUnload(nil) error = %v, want ErrNilRuntime", err)
	}
	if err := hook.OnConfigChange(context.Background(), nil, ConfigChange{}); !errors.Is(err, ErrNilRuntime) {
		t.Fatalf("OnConfigChange(nil) error = %v, want ErrNilRuntime", err)
	}

	runtime := stubRuntime{}
	if err := hook.OnLoad(context.Background(), runtime); err != nil {
		t.Fatalf("OnLoad() error = %v", err)
	}
	if err := hook.OnUnload(context.Background(), runtime); err != nil {
		t.Fatalf("OnUnload() error = %v", err)
	}
	if err := hook.OnConfigChange(context.Background(), runtime, ConfigChange{}); err != nil {
		t.Fatalf("OnConfigChange() error = %v", err)
	}
}

func TestHookSetRunsHooksInOrderAndIsolatesChanges(t *testing.T) {
	t.Parallel()

	events := make([]string, 0, 6)
	hooks := HookSet{
		HookFuncs{
			OnLoadFunc: func(context.Context, PluginRuntime) error {
				events = append(events, "load:first")
				return nil
			},
			OnConfigChangeFunc: func(_ context.Context, _ PluginRuntime, change ConfigChange) error {
				events = append(events, "change:first:"+change.Current["mode"].(string))
				change.Current["mode"] = "mutated"
				return nil
			},
			OnUnloadFunc: func(context.Context, PluginRuntime) error {
				events = append(events, "unload:first")
				return nil
			},
		},
		nil,
		HookFuncs{
			OnLoadFunc: func(context.Context, PluginRuntime) error {
				events = append(events, "load:second")
				return nil
			},
			OnConfigChangeFunc: func(_ context.Context, _ PluginRuntime, change ConfigChange) error {
				events = append(events, "change:second:"+change.Current["mode"].(string))
				if change.Current["mode"] != "new" {
					t.Fatalf("change.Current = %#v", change.Current)
				}
				return nil
			},
			OnUnloadFunc: func(context.Context, PluginRuntime) error {
				events = append(events, "unload:second")
				return nil
			},
		},
	}

	change := ConfigChange{
		Previous: map[string]any{"mode": "old"},
		Current:  map[string]any{"mode": "new"},
	}
	runtime := stubRuntime{}

	if err := hooks.OnLoad(context.Background(), runtime); err != nil {
		t.Fatalf("OnLoad() error = %v", err)
	}
	if err := hooks.OnConfigChange(context.Background(), runtime, change); err != nil {
		t.Fatalf("OnConfigChange() error = %v", err)
	}
	if err := hooks.OnUnload(context.Background(), runtime); err != nil {
		t.Fatalf("OnUnload() error = %v", err)
	}

	want := []string{
		"load:first",
		"load:second",
		"change:first:new",
		"change:second:new",
		"unload:first",
		"unload:second",
	}
	if len(events) != len(want) {
		t.Fatalf("events = %#v, want %#v", events, want)
	}
	for idx := range want {
		if events[idx] != want[idx] {
			t.Fatalf("events[%d] = %q, want %q", idx, events[idx], want[idx])
		}
	}
	if change.Current["mode"] != "new" {
		t.Fatalf("original change mutated = %#v", change.Current)
	}
}

func TestHookSetNilRuntimeAndEmptySet(t *testing.T) {
	t.Parallel()

	if err := (HookSet{}).OnLoad(context.Background(), nil); err != nil {
		t.Fatalf("empty HookSet OnLoad() error = %v", err)
	}
	hooks := HookSet{HookFuncs{}}
	if err := hooks.OnLoad(context.Background(), nil); !errors.Is(err, ErrNilRuntime) {
		t.Fatalf("OnLoad(nil) error = %v, want ErrNilRuntime", err)
	}
	if err := hooks.OnUnload(context.Background(), nil); !errors.Is(err, ErrNilRuntime) {
		t.Fatalf("OnUnload(nil) error = %v, want ErrNilRuntime", err)
	}
	if err := hooks.OnConfigChange(context.Background(), nil, ConfigChange{}); !errors.Is(err, ErrNilRuntime) {
		t.Fatalf("OnConfigChange(nil) error = %v, want ErrNilRuntime", err)
	}
}

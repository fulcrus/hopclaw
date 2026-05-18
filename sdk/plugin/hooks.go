package plugin

import "context"

type ConfigChange struct {
	Previous map[string]any
	Current  map[string]any
}

type Hook interface {
	OnLoad(ctx context.Context, runtime PluginRuntime) error
	OnUnload(ctx context.Context, runtime PluginRuntime) error
	OnConfigChange(ctx context.Context, runtime PluginRuntime, change ConfigChange) error
}

type HookFuncs struct {
	OnLoadFunc         func(ctx context.Context, runtime PluginRuntime) error
	OnUnloadFunc       func(ctx context.Context, runtime PluginRuntime) error
	OnConfigChangeFunc func(ctx context.Context, runtime PluginRuntime, change ConfigChange) error
}

// HookSet composes multiple hooks into one lifecycle handler.
type HookSet []Hook

func (h HookFuncs) OnLoad(ctx context.Context, runtime PluginRuntime) error {
	if runtime == nil {
		return ErrNilRuntime
	}
	if h.OnLoadFunc == nil {
		return nil
	}
	return h.OnLoadFunc(ctx, runtime)
}

func (h HookFuncs) OnUnload(ctx context.Context, runtime PluginRuntime) error {
	if runtime == nil {
		return ErrNilRuntime
	}
	if h.OnUnloadFunc == nil {
		return nil
	}
	return h.OnUnloadFunc(ctx, runtime)
}

func (h HookFuncs) OnConfigChange(ctx context.Context, runtime PluginRuntime, change ConfigChange) error {
	if runtime == nil {
		return ErrNilRuntime
	}
	if h.OnConfigChangeFunc == nil {
		return nil
	}
	change.Previous = cloneMapAny(change.Previous)
	change.Current = cloneMapAny(change.Current)
	return h.OnConfigChangeFunc(ctx, runtime, change)
}

func (s HookSet) OnLoad(ctx context.Context, runtime PluginRuntime) error {
	if !s.hasHooks() {
		return nil
	}
	if runtime == nil {
		return ErrNilRuntime
	}
	for _, hook := range s {
		if hook == nil {
			continue
		}
		if err := hook.OnLoad(ctx, runtime); err != nil {
			return err
		}
	}
	return nil
}

func (s HookSet) OnUnload(ctx context.Context, runtime PluginRuntime) error {
	if !s.hasHooks() {
		return nil
	}
	if runtime == nil {
		return ErrNilRuntime
	}
	for _, hook := range s {
		if hook == nil {
			continue
		}
		if err := hook.OnUnload(ctx, runtime); err != nil {
			return err
		}
	}
	return nil
}

func (s HookSet) OnConfigChange(ctx context.Context, runtime PluginRuntime, change ConfigChange) error {
	if !s.hasHooks() {
		return nil
	}
	if runtime == nil {
		return ErrNilRuntime
	}
	for _, hook := range s {
		if hook == nil {
			continue
		}
		perHook := ConfigChange{
			Previous: cloneMapAny(change.Previous),
			Current:  cloneMapAny(change.Current),
		}
		if err := hook.OnConfigChange(ctx, runtime, perHook); err != nil {
			return err
		}
	}
	return nil
}

func (s HookSet) hasHooks() bool {
	for _, hook := range s {
		if hook != nil {
			return true
		}
	}
	return false
}

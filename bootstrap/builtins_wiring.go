package bootstrap

import (
	"github.com/fulcrus/hopclaw/config"
	"github.com/fulcrus/hopclaw/toolruntime"
)

func (a *App) builtinsBindingsForConfig(cfg config.Config) toolruntime.BuiltinsBindings {
	if a == nil {
		return toolruntime.BuiltinsBindings{}
	}
	return composeBuiltinsBindings(a.builtinsBindingPacksForConfig(cfg)...)
}

func (a *App) wireBuiltinsForConfigLocked(cfg config.Config) {
	if a == nil || a.builtins == nil {
		return
	}
	a.builtins.ApplyBindings(a.builtinsBindingsForConfig(cfg))
}

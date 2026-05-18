package bootstrap

import (
	"github.com/fulcrus/hopclaw/channels/health"
	channelmgr "github.com/fulcrus/hopclaw/channels/manager"
	"github.com/fulcrus/hopclaw/internal/modules"
	extregistry "github.com/fulcrus/hopclaw/internal/registry/extensions"
)

type extensionRegistryBindings struct {
	tools         extregistry.ToolInventory
	capabilities  extregistry.CapabilityInventory
	channels      extregistry.ChannelInventory
	channelHealth extregistry.ChannelHealthReader
	modules       extregistry.ModuleInventory
}

func applyExtensionRegistryBindings(registry *extregistry.Registry, bindings extensionRegistryBindings) {
	if registry == nil {
		return
	}
	registry.SetTools(bindings.tools)
	registry.SetCapabilities(bindings.capabilities)
	registry.SetChannels(bindings.channels)
	registry.SetChannelHealth(bindings.channelHealth)
	registry.SetModules(bindings.modules)
}

func (a *App) extensionRegistryBindingsLocked() extensionRegistryBindings {
	if a == nil {
		return extensionRegistryBindings{}
	}
	return extensionRegistryBindings{
		tools:         a.toolRuntime,
		capabilities:  a.Capabilities,
		channels:      a.Channels,
		channelHealth: a.HealthMonitor,
		modules:       a.ModuleCatalog,
	}
}

func (a *App) wireExtensionRegistryLocked() {
	if a == nil {
		return
	}
	applyExtensionRegistryBindings(a.ExtensionRegistry, a.extensionRegistryBindingsLocked())
}

type channelmgrLike interface {
	extregistry.ChannelInventory
}

type channelHealthLike interface {
	extregistry.ChannelHealthReader
}

type moduleInventoryLike interface {
	extregistry.ModuleInventory
}

var (
	_ channelmgrLike      = (*channelmgr.Manager)(nil)
	_ channelHealthLike   = (*health.Monitor)(nil)
	_ moduleInventoryLike = (*modules.Store)(nil)
)

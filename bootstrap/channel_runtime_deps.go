package bootstrap

import (
	"time"

	"github.com/fulcrus/hopclaw/agent"
	"github.com/fulcrus/hopclaw/channels"
	channelmgr "github.com/fulcrus/hopclaw/channels/manager"
	"github.com/fulcrus/hopclaw/channels/pairing"
	channelregistry "github.com/fulcrus/hopclaw/channels/registry"
	"github.com/fulcrus/hopclaw/config"
	"github.com/fulcrus/hopclaw/eventbus"
	"github.com/fulcrus/hopclaw/internal/modules"
	runtimesvc "github.com/fulcrus/hopclaw/runtime"
)

func newChannelRuntimeDeps(
	cfg config.Config,
	channelManager *channelmgr.Manager,
	runtimeService *runtimesvc.Service,
	sessions agent.SessionStore,
	bus *eventbus.InMemoryBus,
	statusDelay time.Duration,
	moduleCatalog *modules.Store,
	pairingManager *pairing.Manager,
	threadBindings *channels.ThreadBinding,
) channelregistry.RuntimeDeps {
	return channelregistry.RuntimeDeps{
		Channels:       cfg.Channels,
		StorePath:      cfg.Store.Path,
		ChannelManager: channelManager,
		RuntimeService: runtimeService,
		Sessions:       sessions,
		Bus:            bus,
		StatusDelay:    statusDelay,
		ModuleCatalog:  moduleCatalog,
		PairingManager: pairingManager,
		ThreadBindings: threadBindings,
	}
}

func (a *App) channelRuntimeDeps(cfg config.Config, channelManager *channelmgr.Manager, moduleCatalog *modules.Store) channelregistry.RuntimeDeps {
	if a == nil {
		return channelregistry.RuntimeDeps{}
	}
	if channelManager == nil {
		channelManager = a.Channels
	}
	if moduleCatalog == nil {
		moduleCatalog = a.ModuleCatalog
	}
	return newChannelRuntimeDeps(
		cfg,
		channelManager,
		a.Runtime,
		a.Sessions,
		a.Bus,
		a.statusDelay,
		moduleCatalog,
		a.pairingManager,
		a.threadBindings,
	)
}

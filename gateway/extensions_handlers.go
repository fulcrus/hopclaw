package gateway

import (
	"net/http"

	extregistry "github.com/fulcrus/hopclaw/internal/registry/extensions"
)

func (g *Gateway) handleExtensions(w http.ResponseWriter, r *http.Request) {
	gwJSON(w, http.StatusOK, g.extensionSnapshot(r))
}

func (g *Gateway) extensionSnapshot(r *http.Request) extregistry.Snapshot {
	registry := g.extensionRegistry()
	if registry == nil {
		return extregistry.Snapshot{}
	}
	return registry.Snapshot(r.Context(), nil)
}

func (g *Gateway) extensionRegistry() *extregistry.Registry {
	if g.extensions == nil {
		return nil
	}
	if g.capabilities != nil {
		g.extensions.SetCapabilities(g.capabilities)
	}
	if g.channels != nil {
		g.extensions.SetChannels(g.channels)
	}
	if g.channelHealth != nil {
		g.extensions.SetChannelHealth(g.channelHealth)
	}
	if g.moduleCatalog != nil {
		g.extensions.SetModules(g.moduleCatalog)
	}
	return g.extensions
}

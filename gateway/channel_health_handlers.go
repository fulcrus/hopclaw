package gateway

import (
	"net/http"
)

func (g *Gateway) handleChannelHealth(w http.ResponseWriter, r *http.Request) {
	if registry := g.extensionRegistry(); registry != nil {
		statuses := registry.ChannelHealth()
		if statuses != nil {
			gwJSON(w, http.StatusOK, countedItemsResponse{Items: statuses, Count: len(statuses)})
			return
		}
	}
	if g.channelHealth == nil {
		gwError(w, http.StatusServiceUnavailable, "channel health monitor not available")
		return
	}
	statuses := g.channelHealth.Status()
	gwJSON(w, http.StatusOK, countedItemsResponse{Items: statuses, Count: len(statuses)})
}

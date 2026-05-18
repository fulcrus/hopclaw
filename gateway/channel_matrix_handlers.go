package gateway

import (
	"net/http"
	"sort"
	"strings"

	"github.com/fulcrus/hopclaw/channels"
)

type channelMatrixItem struct {
	Name         string                    `json:"name"`
	Status       string                    `json:"status"`
	Capabilities channels.CapabilityMatrix `json:"capabilities"`
}

type channelMatrixResponse struct {
	Items []channelMatrixItem `json:"items"`
	Count int                 `json:"count"`
}

type threadBindingItem struct {
	Channel    string `json:"channel"`
	ThreadID   string `json:"thread_id"`
	SessionKey string `json:"session_key"`
}

type threadBindingsResponse struct {
	Items []threadBindingItem `json:"items"`
	Count int                 `json:"count"`
}

func (g *Gateway) handleChannelMatrix(w http.ResponseWriter, _ *http.Request) {
	if registry := g.extensionRegistry(); registry != nil {
		items := registry.Channels()
		payload := make([]channelMatrixItem, 0, len(items))
		for _, item := range items {
			payload = append(payload, channelMatrixItem{
				Name:         item.Name,
				Status:       item.Status,
				Capabilities: item.CapabilityMatrix,
			})
		}
		gwJSON(w, http.StatusOK, channelMatrixResponse{Items: payload, Count: len(payload)})
		return
	}
	if g.channels == nil {
		gwError(w, http.StatusServiceUnavailable, "channels not available")
		return
	}
	names := g.channels.Names()
	sort.Strings(names)
	items := make([]channelMatrixItem, 0, len(names))
	for _, name := range names {
		adapter, ok := g.channels.Get(name)
		if !ok {
			continue
		}
		items = append(items, channelMatrixItem{
			Name:         name,
			Status:       string(adapter.Status()),
			Capabilities: channels.MatrixForAdapter(adapter),
		})
	}
	gwJSON(w, http.StatusOK, channelMatrixResponse{Items: items, Count: len(items)})
}

func (g *Gateway) handleChannelThreadBindings(w http.ResponseWriter, _ *http.Request) {
	if g.threadBindings == nil {
		gwError(w, http.StatusServiceUnavailable, "thread binding manager not available")
		return
	}
	raw := g.threadBindings.List()
	items := make([]threadBindingItem, 0, len(raw))
	for key, sessionKey := range raw {
		channel, threadID, ok := strings.Cut(key, ":")
		if !ok {
			continue
		}
		items = append(items, threadBindingItem{
			Channel:    channel,
			ThreadID:   threadID,
			SessionKey: sessionKey,
		})
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].Channel != items[j].Channel {
			return items[i].Channel < items[j].Channel
		}
		return items[i].ThreadID < items[j].ThreadID
	})
	gwJSON(w, http.StatusOK, threadBindingsResponse{Items: items, Count: len(items)})
}

func (g *Gateway) handleChannelThreadBindingDelete(w http.ResponseWriter, r *http.Request) {
	if g.threadBindings == nil {
		gwError(w, http.StatusServiceUnavailable, "thread binding manager not available")
		return
	}
	channel := strings.TrimSpace(r.PathValue("channel"))
	threadID := strings.TrimSpace(r.PathValue("thread_id"))
	if channel == "" || threadID == "" {
		gwError(w, http.StatusBadRequest, "channel and thread_id are required")
		return
	}
	g.threadBindings.Unbind(channel, threadID)
	gwJSON(w, http.StatusOK, channelThreadOKResponse{OK: true, Channel: channel, ThreadID: threadID})
}

package gateway

import (
	"context"
	"net/http"
	"sort"
	"time"

	captypes "github.com/fulcrus/hopclaw/capability/types"
)

type instanceSummary struct {
	ID         string         `json:"id"`
	Kind       string         `json:"kind"`
	Name       string         `json:"name,omitempty"`
	Status     string         `json:"status"`
	Platform   string         `json:"platform,omitempty"`
	RemoteIP   string         `json:"remote_ip,omitempty"`
	Capability string         `json:"capability,omitempty"`
	CreatedAt  time.Time      `json:"created_at,omitempty"`
	LastSeenAt time.Time      `json:"last_seen_at,omitempty"`
	Metadata   map[string]any `json:"metadata,omitempty"`
}

type instancesListResponse struct {
	Items []instanceSummary `json:"items"`
	Count int               `json:"count"`
}

func sessionInstanceSummary(handle *captypes.SessionHandle) instanceSummary {
	metadata := map[string]any{}
	for k, v := range handle.Metadata {
		metadata[k] = v
	}
	return instanceSummary{
		ID:         handle.ID,
		Kind:       "capability-session",
		Name:       handle.Capability,
		Status:     "active",
		Capability: handle.Capability,
		CreatedAt:  handle.CreatedAt,
		Metadata:   metadata,
	}
}

func (g *Gateway) collectInstanceSummaries(ctx context.Context) []instanceSummary {
	items := make([]instanceSummary, 0)

	if g.wsHandler != nil {
		for _, client := range g.wsHandler.registry.List() {
			items = append(items, instanceSummary{
				ID:        client.ID,
				Kind:      "websocket",
				Name:      client.ID,
				Status:    "connected",
				Platform:  client.Platform,
				RemoteIP:  client.RemoteAddr,
				CreatedAt: client.ConnectedAt,
			})
		}
	}

	for _, node := range g.listNodeSummaries() {
		items = append(items, instanceSummary{
			ID:         node.NodeID,
			Kind:       "node",
			Name:       node.NodeID,
			Status:     node.Status,
			Platform:   node.Platform,
			RemoteIP:   node.RemoteIP,
			CreatedAt:  node.ConnectedAt,
			LastSeenAt: node.LastSeenAt,
			Metadata: map[string]any{
				"capabilities": node.Capabilities,
				"commands":     node.Commands,
			},
		})
	}

	if g.capabilities != nil {
		for _, name := range g.capabilities.Names() {
			for _, session := range g.capabilities.ListCapabilitySessions(name) {
				items = append(items, sessionInstanceSummary(session))
			}
		}
	}

	if g.discovery != nil {
		peers, err := g.discovery.Discover(ctx)
		if err != nil {
			log.Warn("instance discovery failed", "error", err)
		} else {
			for _, peer := range peers {
				items = append(items, instanceSummary{
					ID:         peer.ID,
					Kind:       "peer",
					Name:       peer.Name,
					Status:     peer.Status,
					RemoteIP:   peer.Address,
					CreatedAt:  peer.SeenAt,
					LastSeenAt: peer.SeenAt,
					Metadata: map[string]any{
						"version": peer.Version,
					},
				})
			}
		}
	}

	sort.Slice(items, func(i, j int) bool {
		if items[i].Kind != items[j].Kind {
			return items[i].Kind < items[j].Kind
		}
		left := items[i].LastSeenAt
		if left.IsZero() {
			left = items[i].CreatedAt
		}
		right := items[j].LastSeenAt
		if right.IsZero() {
			right = items[j].CreatedAt
		}
		if !left.Equal(right) {
			return left.After(right)
		}
		if items[i].Name != items[j].Name {
			return items[i].Name < items[j].Name
		}
		return items[i].ID < items[j].ID
	})
	return items
}

func (g *Gateway) handleInstancesList(w http.ResponseWriter, r *http.Request) {
	items := g.collectInstanceSummaries(r.Context())
	gwJSON(w, http.StatusOK, instancesListResponse{Items: items, Count: len(items)})
}

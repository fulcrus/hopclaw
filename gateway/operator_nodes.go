package gateway

import (
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/fulcrus/hopclaw/gateway/nodes"
)

const operatorNodeStaleAfter = 2 * time.Minute

type operatorNodeSummary struct {
	NodeID          string    `json:"node_id"`
	Name            string    `json:"name,omitempty"`
	Platform        string    `json:"platform,omitempty"`
	Version         string    `json:"version,omitempty"`
	DeviceFamily    string    `json:"device_family,omitempty"`
	ModelIdentifier string    `json:"model_identifier,omitempty"`
	RemoteIP        string    `json:"remote_ip,omitempty"`
	Capabilities    []string  `json:"capabilities,omitempty"`
	Commands        []string  `json:"commands,omitempty"`
	ConnectedAt     time.Time `json:"connected_at,omitempty"`
	LastSeenAt      time.Time `json:"last_seen_at,omitempty"`
	Status          string    `json:"status"`
}

type operatorNodeListResponse struct {
	Items []operatorNodeSummary `json:"items"`
	Count int                   `json:"count"`
}

type operatorNodeGetResponse struct {
	Node operatorNodeSummary `json:"node"`
}

func summarizeNodeSession(session nodes.NodeSession, name string) operatorNodeSummary {
	summary := operatorNodeSummary{
		NodeID:          session.NodeID,
		Name:            strings.TrimSpace(name),
		Platform:        session.Platform,
		Version:         session.Version,
		DeviceFamily:    session.DeviceFamily,
		ModelIdentifier: session.ModelIdentifier,
		RemoteIP:        session.RemoteIP,
		Capabilities:    append([]string(nil), session.Capabilities...),
		Commands:        append([]string(nil), session.Commands...),
		ConnectedAt:     session.ConnectedAt,
		LastSeenAt:      session.LastSeenAt,
		Status:          nodeSessionStatus(session),
	}
	sort.Strings(summary.Capabilities)
	sort.Strings(summary.Commands)
	return summary
}

func nodeSessionStatus(session nodes.NodeSession) string {
	if session.LastSeenAt.IsZero() {
		return "unknown"
	}
	if time.Since(session.LastSeenAt) > operatorNodeStaleAfter {
		return "stale"
	}
	return "connected"
}

func (g *Gateway) listNodeSummaries() []operatorNodeSummary {
	if g.wsHandler == nil || g.wsHandler.nodes == nil {
		return []operatorNodeSummary{}
	}
	sessions := g.wsHandler.nodes.List()
	items := make([]operatorNodeSummary, 0, len(sessions))
	for _, session := range sessions {
		items = append(items, summarizeNodeSession(session, g.nodeDisplayName(session.NodeID)))
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].ConnectedAt.Equal(items[j].ConnectedAt) {
			return items[i].NodeID < items[j].NodeID
		}
		return items[i].ConnectedAt.After(items[j].ConnectedAt)
	})
	return items
}

func (g *Gateway) handleNodesList(w http.ResponseWriter, _ *http.Request) {
	items := g.listNodeSummaries()
	gwJSON(w, http.StatusOK, operatorNodeListResponse{Items: items, Count: len(items)})
}

func (g *Gateway) handleNodeGet(w http.ResponseWriter, r *http.Request) {
	nodeID := strings.TrimSpace(r.PathValue("id"))
	if nodeID == "" {
		gwError(w, http.StatusBadRequest, "missing node id")
		return
	}
	if g.wsHandler == nil || g.wsHandler.nodes == nil {
		gwError(w, http.StatusNotFound, "node not found")
		return
	}
	session, ok := g.wsHandler.nodes.Get(nodeID)
	if !ok {
		gwError(w, http.StatusNotFound, "node not found")
		return
	}
	gwJSON(w, http.StatusOK, operatorNodeGetResponse{Node: summarizeNodeSession(session, g.nodeDisplayName(session.NodeID))})
}

func (g *Gateway) nodeDisplayName(nodeID string) string {
	if g == nil || g.deviceStore == nil {
		return ""
	}
	device, ok := g.deviceStore.GetDevice(strings.TrimSpace(nodeID))
	if !ok || device == nil {
		return ""
	}
	return strings.TrimSpace(device.Name)
}

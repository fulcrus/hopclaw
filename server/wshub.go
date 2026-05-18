package server

import (
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/fulcrus/hopclaw/eventbus"
	"github.com/fulcrus/hopclaw/logging"
)

// ---------------------------------------------------------------------------
// Hub constants
// ---------------------------------------------------------------------------

const (
	wsSendChannelSize = 256
	wsMaxConnections  = 1000
)

// WSHub manages all active WebSocket connections.
type WSHub struct {
	mu          sync.RWMutex       // guards connections
	connections map[string]*WSConn // connID -> conn
	seq         atomic.Int64       // global event sequence counter
	eventSub    *eventbus.Subscription
	bus         *eventbus.InMemoryBus
	done        chan struct{}
}

// NewWSHub creates a new hub. If bus is nil, event broadcasting is disabled.
func NewWSHub(bus *eventbus.InMemoryBus) *WSHub {
	return &WSHub{
		connections: make(map[string]*WSConn),
		bus:         bus,
		done:        make(chan struct{}),
	}
}

// Start begins the event broadcast loop. Must be called once.
func (h *WSHub) Start() {
	if h.bus != nil {
		h.eventSub = h.bus.SubscribeChannel(wsSendChannelSize)
		go h.eventLoop()
	}
	go h.tickLoop()
}

// Stop shuts down all connections and the broadcast loop.
func (h *WSHub) Stop() {
	close(h.done)
	if h.eventSub != nil {
		h.eventSub.Close()
	}

	h.mu.Lock()
	conns := make([]*WSConn, 0, len(h.connections))
	for _, c := range h.connections {
		conns = append(conns, c)
	}
	h.connections = make(map[string]*WSConn)
	h.mu.Unlock()

	for _, c := range conns {
		c.Close()
	}
}

// Register adds a new connection. Returns an error if the max connection
// limit is reached.
func (h *WSHub) Register(conn *WSConn) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.connections) >= wsMaxConnections {
		return fmt.Errorf("ws: max connections (%d) reached", wsMaxConnections)
	}
	h.connections[conn.id] = conn
	return nil
}

// Unregister removes a connection by ID.
func (h *WSHub) Unregister(connID string) {
	h.mu.Lock()
	delete(h.connections, connID)
	h.mu.Unlock()
}

// Broadcast sends an event to all connected clients.
func (h *WSHub) Broadcast(event string, payload any) {
	seq := h.seq.Add(1)
	h.mu.RLock()
	conns := make([]*WSConn, 0, len(h.connections))
	for _, c := range h.connections {
		conns = append(conns, c)
	}
	h.mu.RUnlock()

	for _, c := range conns {
		logging.LogIfErr(c.context(), c.SendEvent(event, payload, seq), "send hub event failed")
	}
}

// BroadcastTo sends an event to specific connections.
func (h *WSHub) BroadcastTo(connIDs []string, event string, payload any) {
	seq := h.seq.Add(1)
	h.mu.RLock()
	for _, id := range connIDs {
		if c, ok := h.connections[id]; ok {
			logging.LogIfErr(c.context(), c.SendEvent(event, payload, seq), "send hub event failed")
		}
	}
	h.mu.RUnlock()
}

// ConnectionCount returns the number of active connections.
func (h *WSHub) ConnectionCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.connections)
}

// WSConnectionInfo describes a connected WebSocket client.
type WSConnectionInfo struct {
	ConnID      string    `json:"conn_id"`
	ClientID    string    `json:"client_id"`
	ClientName  string    `json:"client_name,omitempty"`
	Role        string    `json:"role"`
	Platform    string    `json:"platform,omitempty"`
	ConnectedAt time.Time `json:"connected_at"`
}

// Connections returns info about all active connections.
func (h *WSHub) Connections() []WSConnectionInfo {
	h.mu.RLock()
	defer h.mu.RUnlock()

	infos := make([]WSConnectionInfo, 0, len(h.connections))
	for _, c := range h.connections {
		infos = append(infos, WSConnectionInfo{
			ConnID:      c.id,
			ClientID:    c.clientInfo.ID,
			ClientName:  c.clientInfo.DisplayName,
			Role:        c.role,
			Platform:    c.clientInfo.Platform,
			ConnectedAt: c.connectedAt,
		})
	}
	return infos
}

// eventLoop subscribes to the event bus and broadcasts to all connections.
func (h *WSHub) eventLoop() {
	ch := h.eventSub.Events()
	for {
		select {
		case <-h.done:
			return
		case evt, ok := <-ch:
			if !ok {
				return
			}
			h.Broadcast(string(evt.Type), evt)
		}
	}
}

// tickLoop sends periodic tick events to all connections.
func (h *WSHub) tickLoop() {
	ticker := time.NewTicker(wsTickInterval)
	defer ticker.Stop()

	for {
		select {
		case <-h.done:
			return
		case now := <-ticker.C:
			h.Broadcast("tick", map[string]any{
				"time":        now.UTC().Format(time.RFC3339),
				"connections": h.ConnectionCount(),
			})
		}
	}
}

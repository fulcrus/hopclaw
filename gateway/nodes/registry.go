package nodes

import (
	"encoding/json"
	"fmt"
	"sync"
	"time"
)

const (
	nodeTimeout       = 2 * time.Minute
	cleanupInterval   = 30 * time.Second
	defaultCmdTimeout = 30 * time.Second
)

// Registry manages all connected device nodes.
type Registry struct {
	mu       sync.RWMutex
	nodes    map[string]*connectedNode
	done     chan struct{}
	stopOnce sync.Once
}

type connectedNode struct {
	session     NodeSession
	sendFn      func(msg []byte) error
	pendingCmds map[int]*pendingNodeCmd
	nextCmdID   int
	mu          sync.Mutex // guards pendingCmds, nextCmdID
}

type pendingNodeCmd struct {
	ch     chan NodeInvokeResponse
	sentAt time.Time
}

// NewRegistry creates a new node registry with background cleanup.
func NewRegistry() *Registry {
	r := &Registry{
		nodes: make(map[string]*connectedNode),
		done:  make(chan struct{}),
	}
	go r.cleanupLoop()
	return r
}

// Register adds a new node to the registry.
func (r *Registry) Register(session NodeSession, sendFn func([]byte) error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	session.ConnectedAt = time.Now().UTC()
	session.LastSeenAt = time.Now().UTC()

	r.nodes[session.NodeID] = &connectedNode{
		session:     session,
		sendFn:      sendFn,
		pendingCmds: make(map[int]*pendingNodeCmd),
	}
}

// Unregister removes a node from the registry.
func (r *Registry) Unregister(nodeID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.nodes, nodeID)
}

// Heartbeat updates the last seen time for a node.
func (r *Registry) Heartbeat(nodeID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if n, ok := r.nodes[nodeID]; ok {
		n.session.LastSeenAt = time.Now().UTC()
	}
}

// List returns all currently connected nodes.
func (r *Registry) List() []NodeSession {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make([]NodeSession, 0, len(r.nodes))
	for _, n := range r.nodes {
		result = append(result, n.session)
	}
	return result
}

// Get returns a specific node by ID.
func (r *Registry) Get(nodeID string) (NodeSession, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	n, ok := r.nodes[nodeID]
	if !ok {
		return NodeSession{}, false
	}
	return n.session, true
}

// Invoke sends a command to a node and waits for the response.
func (r *Registry) Invoke(req NodeInvokeRequest) (*NodeInvokeResponse, error) {
	r.mu.RLock()
	node, ok := r.nodes[req.NodeID]
	r.mu.RUnlock()

	if !ok {
		return nil, fmt.Errorf("node %q not connected", req.NodeID)
	}

	// Check command policy.
	if !IsCommandAllowed(node.session.Platform, req.Command) {
		return nil, fmt.Errorf("command %q not allowed on platform %q", req.Command, node.session.Platform)
	}

	timeout := req.Timeout
	if timeout == 0 {
		timeout = defaultCmdTimeout
	}

	node.mu.Lock()
	node.nextCmdID++
	cmdID := node.nextCmdID
	ch := make(chan NodeInvokeResponse, 1)
	node.pendingCmds[cmdID] = &pendingNodeCmd{ch: ch, sentAt: time.Now()}
	node.mu.Unlock()

	// Marshal and send command to node.
	msg, _ := json.Marshal(map[string]any{
		"type":    "invoke",
		"id":      cmdID,
		"command": req.Command,
		"params":  req.Params,
	})
	if err := node.sendFn(msg); err != nil {
		node.mu.Lock()
		delete(node.pendingCmds, cmdID)
		node.mu.Unlock()
		return nil, fmt.Errorf("send to node: %w", err)
	}

	select {
	case resp := <-ch:
		return &resp, nil
	case <-time.After(timeout):
		node.mu.Lock()
		delete(node.pendingCmds, cmdID)
		node.mu.Unlock()
		return nil, fmt.Errorf("node %q: command %q timed out after %s", req.NodeID, req.Command, timeout)
	}
}

// HandleResponse routes a response from a node to the waiting caller.
func (r *Registry) HandleResponse(nodeID string, cmdID int, resp NodeInvokeResponse) {
	r.mu.RLock()
	node, ok := r.nodes[nodeID]
	r.mu.RUnlock()
	if !ok {
		return
	}

	node.mu.Lock()
	cmd, ok := node.pendingCmds[cmdID]
	if ok {
		delete(node.pendingCmds, cmdID)
	}
	node.mu.Unlock()

	if ok {
		cmd.ch <- resp
	}
}

// Stop shuts down the registry.
func (r *Registry) Stop() {
	r.stopOnce.Do(func() { close(r.done) })
}

func (r *Registry) cleanupLoop() {
	ticker := time.NewTicker(cleanupInterval)
	defer ticker.Stop()
	for {
		select {
		case <-r.done:
			return
		case <-ticker.C:
			r.mu.Lock()
			now := time.Now()
			for id, node := range r.nodes {
				if now.Sub(node.session.LastSeenAt) > nodeTimeout {
					delete(r.nodes, id)
				}
			}
			r.mu.Unlock()
		}
	}
}

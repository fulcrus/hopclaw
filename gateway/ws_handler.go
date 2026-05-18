package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/fulcrus/hopclaw/agent"
	"github.com/fulcrus/hopclaw/deviceauth"
	"github.com/fulcrus/hopclaw/gateway/nodes"
	"github.com/fulcrus/hopclaw/internal/support/normalize"
	runtimesvc "github.com/fulcrus/hopclaw/runtime"
	"github.com/gorilla/websocket"
)

// ---------------------------------------------------------------------------
// Constants
// ---------------------------------------------------------------------------

const (
	wsWriteTimeout   = 10 * time.Second
	wsPongTimeout    = 60 * time.Second
	wsPingInterval   = 30 * time.Second
	wsSendBufSize    = 64
	wsClientIDBytes  = 16
	wsEnqueueTimeout = 2 * time.Second
	wsReadLimitBytes = 1 << 20
)

// ---------------------------------------------------------------------------
// Client registry
// ---------------------------------------------------------------------------

// WSClientRegistry tracks connected WebSocket clients.
type WSClientRegistry struct {
	mu      sync.RWMutex
	clients map[string]*wsClient // clientID -> client
}

// NewWSClientRegistry creates an empty client registry.
func NewWSClientRegistry() *WSClientRegistry {
	return &WSClientRegistry{
		clients: make(map[string]*wsClient),
	}
}

func (r *WSClientRegistry) add(c *wsClient) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.clients[c.id] = c
}

func (r *WSClientRegistry) remove(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.clients, id)
}

// Count returns the number of connected clients.
func (r *WSClientRegistry) Count() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.clients)
}

// List returns a stable snapshot of the connected WebSocket clients.
func (r *WSClientRegistry) List() []wsClientSnapshot {
	r.mu.RLock()
	defer r.mu.RUnlock()

	out := make([]wsClientSnapshot, 0, len(r.clients))
	for _, c := range r.clients {
		out = append(out, wsClientSnapshot{
			ID:          c.id,
			Platform:    c.platform,
			RemoteAddr:  c.remoteAddr,
			ConnectedAt: c.connectedAt,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].ConnectedAt.Equal(out[j].ConnectedAt) {
			return out[i].ID < out[j].ID
		}
		return out[i].ConnectedAt.Before(out[j].ConnectedAt)
	})
	return out
}

// ---------------------------------------------------------------------------
// WebSocket client
// ---------------------------------------------------------------------------

type wsClient struct {
	id          string
	ctx         context.Context
	cancel      context.CancelFunc
	conn        *websocket.Conn
	platform    string
	remoteAddr  string
	connectedAt time.Time
	sendCh      chan []byte
	device      *deviceauth.DeviceContext
	nodeID      string
	authScope   authScope
}

type wsClientOptions struct {
	Context    context.Context
	ID         string
	Platform   string
	RemoteAddr string
	Device     *deviceauth.DeviceContext
	AuthScope  authScope
}

type wsClientSnapshot struct {
	ID          string    `json:"id"`
	Platform    string    `json:"platform,omitempty"`
	RemoteAddr  string    `json:"remote_addr,omitempty"`
	ConnectedAt time.Time `json:"connected_at"`
}

// ---------------------------------------------------------------------------
// Handler function type
// ---------------------------------------------------------------------------

type handlerFunc func(client *wsClient, params json.RawMessage) (json.RawMessage, error)

// ---------------------------------------------------------------------------
// WSHandler
// ---------------------------------------------------------------------------

// WSHandler manages WebSocket client connections and dispatches RPC methods.
type WSHandler struct {
	gateway  *Gateway
	registry *WSClientRegistry
	nodes    *nodes.Registry
	methods  map[string]handlerFunc
}

// NewWSHandler creates a handler with all RPC methods registered.
func NewWSHandler(gw *Gateway, nodeRegistry *nodes.Registry) *WSHandler {
	h := &WSHandler{
		gateway:  gw,
		registry: NewWSClientRegistry(),
		nodes:    nodeRegistry,
	}
	h.methods = map[string]handlerFunc{
		"config.get":      h.handleConfigGet,
		"config.set":      h.handleConfigSet,
		"sessions.list":   h.handleSessionsList,
		"chat.send":       h.handleChatSend,
		"chat.abort":      h.handleChatAbort,
		"node.list":       h.handleNodeList,
		"node.describe":   h.handleNodeDescribe,
		"node.invoke":     h.handleNodeInvoke,
		"node.register":   h.handleNodeRegister,
		"node.heartbeat":  h.handleNodeHeartbeat,
		"system.presence": h.handleSystemPresence,
		"voice.wake":      h.handleVoiceWake,
	}
	return h
}

// ---------------------------------------------------------------------------
// Dispatch loop
// ---------------------------------------------------------------------------

// ServeClient reads frames from the WebSocket connection and dispatches
// them to registered RPC methods. It blocks until the connection closes.
func (h *WSHandler) ServeClient(conn *websocket.Conn, opts wsClientOptions) {
	parentCtx := opts.Context
	if parentCtx == nil {
		parentCtx = context.Background()
	}
	clientCtx, cancel := context.WithCancel(parentCtx)
	client := &wsClient{
		id:          opts.ID,
		ctx:         clientCtx,
		cancel:      cancel,
		conn:        conn,
		platform:    opts.Platform,
		remoteAddr:  opts.RemoteAddr,
		connectedAt: time.Now().UTC(),
		sendCh:      make(chan []byte, wsSendBufSize),
		device:      opts.Device,
		authScope:   opts.AuthScope,
	}
	if client.id == "" {
		client.id = fmt.Sprintf("ws-%d", time.Now().UnixNano())
	}
	h.registry.add(client)
	defer func() {
		if client.cancel != nil {
			client.cancel()
		}
		if client.nodeID != "" && h.nodes != nil {
			h.nodes.Unregister(client.nodeID)
		}
		h.registry.remove(client.id)
	}()

	// Writer goroutine.
	done := make(chan struct{})
	go h.writeLoop(client, done)

	// Reader loop.
	conn.SetReadLimit(wsReadLimitBytes)
	conn.SetReadDeadline(time.Now().Add(wsPongTimeout))
	conn.SetPongHandler(func(string) error {
		conn.SetReadDeadline(time.Now().Add(wsPongTimeout))
		return nil
	})

	for {
		_, msg, err := conn.ReadMessage()
		if err != nil {
			break
		}
		h.dispatch(client, msg)
	}
	close(done)
}

func (h *WSHandler) writeLoop(client *wsClient, done <-chan struct{}) {
	ticker := time.NewTicker(wsPingInterval)
	defer ticker.Stop()

	for {
		select {
		case msg, ok := <-client.sendCh:
			if !ok {
				return
			}
			client.conn.SetWriteDeadline(time.Now().Add(wsWriteTimeout))
			if err := client.conn.WriteMessage(websocket.TextMessage, msg); err != nil {
				return
			}
		case <-ticker.C:
			client.conn.SetWriteDeadline(time.Now().Add(wsWriteTimeout))
			if err := client.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		case <-client.context().Done():
			return
		case <-done:
			return
		}
	}
}

func (h *WSHandler) dispatch(client *wsClient, raw []byte) {
	var envelope struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		h.sendError(client, "", errCodeParseError, "invalid request frame")
		return
	}
	switch envelope.Type {
	case "req":
		h.dispatchRequest(client, raw)
	case "invoke.result":
		h.handleInvokeResult(client, raw)
	default:
		h.sendError(client, "", errCodeInvalidRequest, "unsupported frame type")
	}
}

func (h *WSHandler) dispatchRequest(client *wsClient, raw []byte) {
	var req operatorWSRequestFrame
	if err := json.Unmarshal(raw, &req); err != nil {
		h.sendError(client, "", errCodeParseError, "invalid request frame")
		return
	}
	if req.Method == "" {
		h.sendError(client, req.ID, errCodeInvalidRequest, "method is required")
		return
	}

	fn, ok := h.methods[req.Method]
	if !ok {
		h.sendError(client, req.ID, errCodeMethodNotFound, fmt.Sprintf("unknown method %q", req.Method))
		return
	}

	result, err := fn(client, req.Params)
	if err != nil {
		h.sendError(client, req.ID, errCodeInternal, err.Error())
		return
	}

	resp := operatorWSResponseFrame{Type: "res", ID: req.ID, OK: true, Payload: result}
	data, _ := json.Marshal(resp)
	if err := h.enqueueClientMessage(client, data); err != nil {
		log.Warn("ws: failed to enqueue response", "client_id", client.id, "request_id", req.ID, "error", err)
	}
}

func (h *WSHandler) handleInvokeResult(client *wsClient, raw []byte) {
	if h.nodes == nil || client.nodeID == "" {
		return
	}
	var frame struct {
		Type  string         `json:"type"`
		ID    int            `json:"id"`
		OK    bool           `json:"ok"`
		Data  map[string]any `json:"data,omitempty"`
		Error string         `json:"error,omitempty"`
	}
	if err := json.Unmarshal(raw, &frame); err != nil {
		return
	}
	h.nodes.HandleResponse(client.nodeID, frame.ID, nodes.NodeInvokeResponse{OK: frame.OK, Data: frame.Data, Error: strings.TrimSpace(frame.Error)})
}

func (h *WSHandler) sendError(client *wsClient, reqID string, code int, message string) {
	resp := operatorWSResponseFrame{
		Type: "res",
		ID:   reqID,
		OK:   false,
		Error: &operatorWSFrameError{
			Code:    code,
			Message: message,
		},
	}
	data, _ := json.Marshal(resp)
	if err := h.enqueueClientMessage(client, data); err != nil {
		log.Warn("ws: failed to enqueue error response", "client_id", client.id, "request_id", reqID, "error", err)
	}
}

func (h *WSHandler) enqueueClientMessage(client *wsClient, data []byte) error {
	if client == nil {
		return nil
	}
	timer := time.NewTimer(wsEnqueueTimeout)
	defer timer.Stop()
	select {
	case <-client.context().Done():
		return context.Canceled
	case client.sendCh <- data:
		return nil
	case <-timer.C:
		h.closeClient(client)
		return fmt.Errorf("send buffer timed out")
	}
}

func (h *WSHandler) closeClient(client *wsClient) {
	if client == nil {
		return
	}
	if client.cancel != nil {
		client.cancel()
	}
	if client.conn != nil {
		_ = client.conn.Close()
	}
}

// ---------------------------------------------------------------------------
// Error codes
// ---------------------------------------------------------------------------

const (
	errCodeParseError     = -32700
	errCodeInvalidRequest = -32600
	errCodeMethodNotFound = -32601
	errCodeInternal       = -32603
)

// ---------------------------------------------------------------------------
// RPC method handlers
// ---------------------------------------------------------------------------

// handleConfigGet returns the current gateway configuration summary.
func (h *WSHandler) handleConfigGet(_ *wsClient, _ json.RawMessage) (json.RawMessage, error) {
	result := configGetResponse{
		Version: h.gateway.config.Version,
	}
	return json.Marshal(result)
}

type configGetResponse struct {
	Version string `json:"version"`
}

// handleConfigSet updates configuration values.
func (h *WSHandler) handleConfigSet(_ *wsClient, params json.RawMessage) (json.RawMessage, error) {
	if params == nil {
		return nil, fmt.Errorf("params are required")
	}
	return nil, fmt.Errorf("config.set is not supported on gateway websocket; use /operator/config endpoints")
}

// handleSessionsList returns active sessions from the runtime service.
func (h *WSHandler) handleSessionsList(client *wsClient, _ json.RawMessage) (json.RawMessage, error) {
	rt := h.gateway.runtime
	if rt == nil {
		return nil, fmt.Errorf("runtime not available")
	}
	sessions, err := rt.ListSessionsFiltered(client.context(), agent.SessionListFilter{
		Scope: clientScopeFilter(client),
	})
	if err != nil {
		return nil, fmt.Errorf("list sessions: %w", err)
	}
	return json.Marshal(sessionsListResponse{
		Items: sessions,
		Count: len(sessions),
	})
}

type sessionsListResponse struct {
	Items any `json:"items"`
	Count int `json:"count"`
}

// handleChatSend sends a user message to a session.
func (h *WSHandler) handleChatSend(client *wsClient, params json.RawMessage) (json.RawMessage, error) {
	var req chatSendRequest
	if params == nil {
		return nil, fmt.Errorf("params are required")
	}
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, fmt.Errorf("invalid params: %w", err)
	}
	if req.Content == "" {
		return nil, fmt.Errorf("content is required")
	}
	rt := h.gateway.runtime
	if rt == nil {
		return nil, fmt.Errorf("runtime not available")
	}
	resolvedAutomationID, err := clientResolvedAuthScope(client).resolveAutomationID(req.AutomationID)
	if err != nil {
		return nil, err
	}
	sessionKey := strings.TrimSpace(req.SessionKey)
	if sessionKey == "" {
		sessionRef := strings.TrimSpace(req.SessionID)
		if sessionRef == "" {
			return nil, fmt.Errorf("session_id or session_key is required")
		}
		if session, err := rt.GetSessionScoped(client.context(), sessionRef, clientScopeFilter(client)); err == nil && session != nil && strings.TrimSpace(session.Key) != "" {
			sessionKey = strings.TrimSpace(session.Key)
		} else {
			sessionKey = sessionRef
		}
	}
	run, err := rt.Submit(client.context(), runtimesvc.SubmitRequest{
		SessionKey:   sessionKey,
		Content:      req.Content,
		Model:        strings.TrimSpace(req.Model),
		AutomationID: resolvedAutomationID,
	})
	if err != nil {
		return nil, fmt.Errorf("send message: %w", err)
	}
	return json.Marshal(chatSendResponse{OK: true, RunID: run.ID})
}

type chatSendRequest struct {
	SessionID    string `json:"session_id"`
	SessionKey   string `json:"session_key,omitempty"`
	Content      string `json:"content"`
	Model        string `json:"model,omitempty"`
	AutomationID string `json:"automation_id,omitempty"`
}

type chatSendResponse struct {
	OK    bool   `json:"ok"`
	RunID string `json:"run_id"`
}

// handleChatAbort aborts the current run in a session.
func (h *WSHandler) handleChatAbort(client *wsClient, params json.RawMessage) (json.RawMessage, error) {
	var req chatAbortRequest
	if params == nil {
		return nil, fmt.Errorf("params are required")
	}
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, fmt.Errorf("invalid params: %w", err)
	}
	if req.SessionID == "" {
		return nil, fmt.Errorf("session_id is required")
	}

	rt := h.gateway.runtime
	if rt == nil {
		return nil, fmt.Errorf("runtime not available")
	}

	// If a run_id is provided, cancel that specific run.
	runID := strings.TrimSpace(req.RunID)
	if runID == "" {
		runs, err := rt.ListRuns(client.context(), agent.RunListFilter{
			SessionID: strings.TrimSpace(req.SessionID),
			Scope:     clientScopeFilter(client),
			Limit:     64,
		})
		if err != nil {
			return nil, fmt.Errorf("list runs: %w", err)
		}
		for _, run := range runs {
			if run == nil || run.Status.Terminal() {
				continue
			}
			runID = strings.TrimSpace(run.ID)
			break
		}
		if runID == "" {
			return nil, fmt.Errorf("no active run found for session %q", strings.TrimSpace(req.SessionID))
		}
	}
	if _, err := rt.GetRunScoped(client.context(), runID, clientScopeFilter(client)); err != nil {
		return nil, fmt.Errorf("cancel run: %w", err)
	}
	if _, err := rt.CancelRun(client.context(), runID); err != nil {
		return nil, fmt.Errorf("cancel run: %w", err)
	}

	return json.Marshal(chatAbortResponse{OK: true})
}

type chatAbortRequest struct {
	SessionID string `json:"session_id"`
	RunID     string `json:"run_id,omitempty"`
}

type chatAbortResponse struct {
	OK bool `json:"ok"`
}

func clientScopeFilter(client *wsClient) agent.ScopeFilter {
	return clientResolvedAuthScope(client).scopeFilter()
}

func clientResolvedAuthScope(client *wsClient) authScope {
	if client == nil {
		return authScope{}
	}
	resolved, err := client.authScope.constrain("", "", "", "", true)
	if err != nil {
		return client.authScope
	}
	return resolved
}

func (c *wsClient) context() context.Context {
	if c == nil || c.ctx == nil {
		return context.Background()
	}
	return c.ctx
}

// handleNodeList returns all connected device nodes.
func (h *WSHandler) handleNodeList(_ *wsClient, _ json.RawMessage) (json.RawMessage, error) {
	if h.nodes == nil {
		return json.Marshal(nodeListResponse{Items: []nodes.NodeSession{}, Count: 0})
	}
	items := h.nodes.List()
	return json.Marshal(nodeListResponse{Items: items, Count: len(items)})
}

type nodeListResponse struct {
	Items []nodes.NodeSession `json:"items"`
	Count int                 `json:"count"`
}

type nodeDescribeRequest struct {
	NodeID string `json:"node_id"`
}

type nodeRegisterRequest struct {
	NodeID          string   `json:"node_id,omitempty"`
	Version         string   `json:"version,omitempty"`
	Platform        string   `json:"platform,omitempty"`
	DeviceFamily    string   `json:"device_family,omitempty"`
	ModelIdentifier string   `json:"model_identifier,omitempty"`
	Capabilities    []string `json:"capabilities,omitempty"`
	Commands        []string `json:"commands,omitempty"`
}

type nodeHeartbeatRequest struct {
	NodeID string `json:"node_id,omitempty"`
}

type nodeRegisterResponse struct {
	OK   bool              `json:"ok"`
	Node nodes.NodeSession `json:"node"`
}

func (h *WSHandler) handleNodeRegister(client *wsClient, params json.RawMessage) (json.RawMessage, error) {
	if h.nodes == nil {
		return nil, fmt.Errorf("node registry not available")
	}
	if client.device == nil {
		return nil, fmt.Errorf("device authentication is required")
	}
	if client.device.Role != deviceauth.RoleNode && client.device.Role != deviceauth.RoleOperator {
		return nil, fmt.Errorf("device role %q is not allowed to register nodes", client.device.Role)
	}
	var req nodeRegisterRequest
	if params != nil {
		if err := json.Unmarshal(params, &req); err != nil {
			return nil, fmt.Errorf("invalid params: %w", err)
		}
	}
	nodeID := strings.TrimSpace(req.NodeID)
	if nodeID == "" {
		nodeID = client.device.DeviceID
	}
	if nodeID == "" {
		return nil, fmt.Errorf("node_id is required")
	}
	if client.device.DeviceID != "" && nodeID != client.device.DeviceID {
		return nil, fmt.Errorf("node_id must match authenticated device")
	}
	platform := strings.TrimSpace(req.Platform)
	if platform == "" {
		platform = strings.TrimSpace(client.device.Platform)
	}
	if platform == "" {
		platform = strings.TrimSpace(client.platform)
	}
	commands := filterAllowedNodeCommands(platform, req.Commands)
	if len(commands) == 0 {
		commands = nodes.PlatformCommands(platform)
	}
	session := nodes.NodeSession{
		NodeID:          nodeID,
		Platform:        platform,
		Version:         strings.TrimSpace(req.Version),
		DeviceFamily:    normalize.FirstNonEmpty(strings.TrimSpace(req.DeviceFamily), strings.TrimSpace(client.device.DeviceFamily)),
		ModelIdentifier: strings.TrimSpace(req.ModelIdentifier),
		RemoteIP:        remoteHost(client.remoteAddr),
		Capabilities:    uniqueSortedStrings(req.Capabilities),
		Commands:        uniqueSortedStrings(commands),
	}
	h.nodes.Register(session, func(msg []byte) error {
		select {
		case client.sendCh <- msg:
			return nil
		default:
			return fmt.Errorf("client send buffer full")
		}
	})
	client.nodeID = nodeID
	if h.gateway.deviceStore != nil {
		if err := h.gateway.deviceStore.RegisterDevice(&deviceauth.DeviceIdentity{
			DeviceID:     nodeID,
			Platform:     platform,
			DeviceFamily: session.DeviceFamily,
			Trusted:      client.device.Trusted,
		}); err != nil {
			log.Warn("gateway ws: register device failed",
				"node_id", nodeID,
				"platform", platform,
				"error", err)
		}
	}
	registered, _ := h.nodes.Get(nodeID)
	return json.Marshal(nodeRegisterResponse{OK: true, Node: registered})
}

func (h *WSHandler) handleNodeHeartbeat(client *wsClient, params json.RawMessage) (json.RawMessage, error) {
	if h.nodes == nil {
		return nil, fmt.Errorf("node registry not available")
	}
	var req nodeHeartbeatRequest
	if params != nil {
		if err := json.Unmarshal(params, &req); err != nil {
			return nil, fmt.Errorf("invalid params: %w", err)
		}
	}
	nodeID := strings.TrimSpace(req.NodeID)
	if nodeID == "" {
		nodeID = client.nodeID
	}
	if nodeID == "" {
		return nil, fmt.Errorf("node is not registered")
	}
	h.nodes.Heartbeat(nodeID)
	if h.gateway.deviceStore != nil {
		if err := h.gateway.deviceStore.UpdateLastSeen(nodeID); err != nil {
			log.Warn("gateway ws: update device last seen failed",
				"node_id", nodeID,
				"error", err)
		}
	}
	return json.Marshal(nodeHeartbeatResponse{OK: true, NodeID: nodeID})
}

type nodeDescribeResponse struct {
	Node   operatorNodeSummary `json:"node"`
	Status string              `json:"status"`
}

func (h *WSHandler) handleNodeDescribe(_ *wsClient, params json.RawMessage) (json.RawMessage, error) {
	if h.nodes == nil {
		return nil, fmt.Errorf("node registry not available")
	}
	var req nodeDescribeRequest
	if params == nil {
		return nil, fmt.Errorf("params are required")
	}
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, fmt.Errorf("invalid params: %w", err)
	}
	req.NodeID = strings.TrimSpace(req.NodeID)
	if req.NodeID == "" {
		return nil, fmt.Errorf("node_id is required")
	}
	session, ok := h.nodes.Get(req.NodeID)
	if !ok {
		return nil, fmt.Errorf("node %q not found", req.NodeID)
	}
	summary := summarizeNodeSession(session, h.gateway.nodeDisplayName(session.NodeID))
	return json.Marshal(nodeDescribeResponse{
		Node:   summary,
		Status: summary.Status,
	})
}

// handleNodeInvoke sends a command to a connected node and returns the result.
func (h *WSHandler) handleNodeInvoke(_ *wsClient, params json.RawMessage) (json.RawMessage, error) {
	if h.nodes == nil {
		return nil, fmt.Errorf("node registry not available")
	}
	var req nodes.NodeInvokeRequest
	if params == nil {
		return nil, fmt.Errorf("params are required")
	}
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, fmt.Errorf("invalid params: %w", err)
	}
	if req.NodeID == "" {
		return nil, fmt.Errorf("node_id is required")
	}
	if req.Command == "" {
		return nil, fmt.Errorf("command is required")
	}
	resp, err := h.nodes.Invoke(req)
	if err != nil {
		return nil, err
	}
	return json.Marshal(resp)
}

type systemPresenceResponse struct {
	Items []instanceSummary `json:"items"`
	Count int               `json:"count"`
}

func filterAllowedNodeCommands(platform string, commands []string) []string {
	allowed := make([]string, 0, len(commands))
	for _, command := range commands {
		command = strings.TrimSpace(command)
		if command == "" {
			continue
		}
		if nodes.IsCommandAllowed(platform, command) {
			allowed = append(allowed, command)
		}
	}
	return uniqueSortedStrings(allowed)
}

func uniqueSortedStrings(items []string) []string {
	seen := make(map[string]struct{}, len(items))
	out := make([]string, 0, len(items))
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		out = append(out, item)
	}
	sort.Strings(out)
	return out
}

func remoteHost(remoteAddr string) string {
	host, _, err := net.SplitHostPort(strings.TrimSpace(remoteAddr))
	if err != nil {
		return strings.TrimSpace(remoteAddr)
	}
	return host
}

func (h *WSHandler) handleSystemPresence(client *wsClient, _ json.RawMessage) (json.RawMessage, error) {
	items := h.gateway.collectInstanceSummaries(client.context())
	return json.Marshal(systemPresenceResponse{
		Items: items,
		Count: len(items),
	})
}

// handleVoiceWake triggers voice activation.
func (h *WSHandler) handleVoiceWake(_ *wsClient, params json.RawMessage) (json.RawMessage, error) {
	var req voiceWakeRequest
	if params != nil {
		if err := json.Unmarshal(params, &req); err != nil {
			return nil, fmt.Errorf("invalid params: %w", err)
		}
	}

	return json.Marshal(voiceWakeResponse{OK: true, Keyword: req.Keyword})
}

type voiceWakeRequest struct {
	Keyword string `json:"keyword,omitempty"`
}

type voiceWakeResponse struct {
	OK      bool   `json:"ok"`
	Keyword string `json:"keyword,omitempty"`
}

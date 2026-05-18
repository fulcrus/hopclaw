package browserd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/fulcrus/hopclaw/logging"

	"github.com/gorilla/websocket"
)

var relayLog = logging.WithSubsystem("extension-relay")

// ---------------------------------------------------------------------------
// Constants
// ---------------------------------------------------------------------------

const (
	extensionReconnectGrace = 20 * time.Second
	extensionCommandTimeout = 10 * time.Second
	extensionPingInterval   = 15 * time.Second
	extensionWriteWait      = 5 * time.Second
	extensionReadLimit      = 4 * 1024 * 1024 // 4 MiB
)

// ---------------------------------------------------------------------------
// Relay message types
// ---------------------------------------------------------------------------

const (
	relayMsgForwardCDPCommand = "forwardCDPCommand"
	relayMsgForwardCDPEvent   = "forwardCDPEvent"
	relayMsgPing              = "ping"
	relayMsgPong              = "pong"
	relayMsgTargetList        = "targetList"
)

// ---------------------------------------------------------------------------
// Errors
// ---------------------------------------------------------------------------

var (
	ErrRelayNoConnection = errors.New("no extension connection available")
	ErrRelayTimeout      = errors.New("extension command timed out")
)

// ---------------------------------------------------------------------------
// Wire types
// ---------------------------------------------------------------------------

type relayMessage struct {
	Type     string          `json:"type"`
	ID       int             `json:"id,omitempty"`
	TargetID string          `json:"target_id,omitempty"`
	Method   string          `json:"method,omitempty"`
	Params   json.RawMessage `json:"params,omitempty"`
	Result   json.RawMessage `json:"result,omitempty"`
	Error    string          `json:"error,omitempty"`
	Targets  []relayTarget   `json:"targets,omitempty"`
}

type relayTarget struct {
	TargetID string `json:"target_id"`
	URL      string `json:"url"`
	Title    string `json:"title,omitempty"`
}

type pendingRelayCmd struct {
	result chan relayMessage
}

// ---------------------------------------------------------------------------
// ExtensionRelayServer
// ---------------------------------------------------------------------------

// ExtensionRelayServer manages WebSocket connections from Chrome extensions,
// forwarding CDP commands to browser tabs and relaying events back.
type ExtensionRelayServer struct {
	mu          sync.Mutex
	port        int
	authToken   string
	conn        *websocket.Conn
	targets     map[string]string // targetID → tabURL
	pendingCmds map[int]*pendingRelayCmd
	nextID      int
	done        chan struct{}
	server      *http.Server
}

var relayUpgrader = websocket.Upgrader{
	ReadBufferSize:  4096,
	WriteBufferSize: 4096,
	CheckOrigin:     func(r *http.Request) bool { return true },
}

// NewExtensionRelayServer creates a new relay server listening on the given
// port. Connections are authenticated using authToken.
func NewExtensionRelayServer(port int, authToken string) *ExtensionRelayServer {
	return &ExtensionRelayServer{
		port:        port,
		authToken:   strings.TrimSpace(authToken),
		targets:     make(map[string]string),
		pendingCmds: make(map[int]*pendingRelayCmd),
		done:        make(chan struct{}),
	}
}

// Start begins listening for WebSocket connections from the Chrome extension.
func (s *ExtensionRelayServer) Start() error {
	mux := http.NewServeMux()
	mux.HandleFunc("/relay", s.handleUpgrade)
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	})

	s.server = &http.Server{
		Addr:              fmt.Sprintf("127.0.0.1:%d", s.port),
		Handler:           mux,
		ReadHeaderTimeout: extensionCommandTimeout,
	}

	go func() {
		if err := s.server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			relayLog.Error("server listen failed", slog.String("error", err.Error()))
		}
	}()
	return nil
}

// Stop gracefully shuts down the relay server and closes any active connection.
func (s *ExtensionRelayServer) Stop() {
	close(s.done)

	s.mu.Lock()
	conn := s.conn
	s.conn = nil
	s.mu.Unlock()

	if conn != nil {
		_ = conn.Close()
	}
	if s.server != nil {
		ctx, cancel := context.WithTimeout(context.Background(), extensionCommandTimeout)
		defer cancel()
		_ = s.server.Shutdown(ctx)
	}
}

// SendCDPCommand forwards a CDP command to the extension and waits for the
// response. It returns the raw JSON result or an error.
func (s *ExtensionRelayServer) SendCDPCommand(ctx context.Context, targetID, method string, params json.RawMessage) (json.RawMessage, error) {
	s.mu.Lock()
	if s.conn == nil {
		s.mu.Unlock()
		return nil, ErrRelayNoConnection
	}

	s.nextID++
	id := s.nextID
	pending := &pendingRelayCmd{
		result: make(chan relayMessage, 1),
	}
	s.pendingCmds[id] = pending
	conn := s.conn
	s.mu.Unlock()

	msg := relayMessage{
		Type:     relayMsgForwardCDPCommand,
		ID:       id,
		TargetID: targetID,
		Method:   method,
		Params:   params,
	}

	data, err := json.Marshal(msg)
	if err != nil {
		s.removePending(id)
		return nil, fmt.Errorf("marshal relay command: %w", err)
	}

	_ = conn.SetWriteDeadline(time.Now().Add(extensionWriteWait))
	if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
		s.removePending(id)
		return nil, fmt.Errorf("send relay command: %w", err)
	}

	timeout := extensionCommandTimeout
	deadline, ok := ctx.Deadline()
	if ok {
		remaining := time.Until(deadline)
		if remaining < timeout {
			timeout = remaining
		}
	}

	select {
	case <-ctx.Done():
		s.removePending(id)
		return nil, ctx.Err()
	case <-time.After(timeout):
		s.removePending(id)
		return nil, ErrRelayTimeout
	case resp := <-pending.result:
		if resp.Error != "" {
			return nil, fmt.Errorf("extension cdp error: %s", resp.Error)
		}
		return resp.Result, nil
	}
}

// ---------------------------------------------------------------------------
// Internal
// ---------------------------------------------------------------------------

func (s *ExtensionRelayServer) handleUpgrade(w http.ResponseWriter, r *http.Request) {
	if !s.authorizeRelay(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	ws, err := relayUpgrader.Upgrade(w, r, nil)
	if err != nil {
		relayLog.Warn("websocket upgrade failed", slog.String("error", err.Error()))
		return
	}

	s.mu.Lock()
	old := s.conn
	s.conn = ws
	s.mu.Unlock()

	if old != nil {
		_ = old.Close()
	}

	s.handleConnection(ws)
}

func (s *ExtensionRelayServer) authorizeRelay(r *http.Request) bool {
	if s.authToken == "" {
		return true
	}

	// Check header first.
	headerToken := strings.TrimSpace(r.Header.Get("X-HopClaw-Relay-Token"))
	if tokenEqual(headerToken, s.authToken) {
		return true
	}

	// Fall back to query param.
	queryToken := strings.TrimSpace(r.URL.Query().Get("token"))
	return tokenEqual(queryToken, s.authToken)
}

func (s *ExtensionRelayServer) handleConnection(ws *websocket.Conn) {
	defer func() {
		s.mu.Lock()
		if s.conn == ws {
			s.conn = nil
		}
		s.mu.Unlock()
		_ = ws.Close()
	}()

	ws.SetReadLimit(extensionReadLimit)

	// Start ping ticker.
	go s.pingLoop(ws)

	for {
		select {
		case <-s.done:
			return
		default:
		}

		_ = ws.SetReadDeadline(time.Now().Add(extensionReconnectGrace))
		_, data, err := ws.ReadMessage()
		if err != nil {
			if !websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
				relayLog.Warn("read error", slog.String("error", err.Error()))
			}
			return
		}

		var msg relayMessage
		if err := json.Unmarshal(data, &msg); err != nil {
			relayLog.Warn("unmarshal error", slog.String("error", err.Error()))
			continue
		}

		s.dispatch(msg)
	}
}

func (s *ExtensionRelayServer) dispatch(msg relayMessage) {
	switch msg.Type {
	case relayMsgPong:
		// Extension responded to ping; nothing to do.
	case relayMsgTargetList:
		s.mu.Lock()
		s.targets = make(map[string]string, len(msg.Targets))
		for _, t := range msg.Targets {
			s.targets[t.TargetID] = t.URL
		}
		s.mu.Unlock()
	case relayMsgForwardCDPEvent:
		// CDP events from the extension; resolve any pending command.
		if msg.ID > 0 {
			s.resolvePending(msg.ID, msg)
		}
	default:
		// Unknown message type or CDP response by ID.
		if msg.ID > 0 {
			s.resolvePending(msg.ID, msg)
		}
	}
}

func (s *ExtensionRelayServer) resolvePending(id int, msg relayMessage) {
	s.mu.Lock()
	pending, ok := s.pendingCmds[id]
	if ok {
		delete(s.pendingCmds, id)
	}
	s.mu.Unlock()

	if ok {
		pending.result <- msg
	}
}

func (s *ExtensionRelayServer) removePending(id int) {
	s.mu.Lock()
	delete(s.pendingCmds, id)
	s.mu.Unlock()
}

func (s *ExtensionRelayServer) pingLoop(ws *websocket.Conn) {
	ticker := time.NewTicker(extensionPingInterval)
	defer ticker.Stop()

	for {
		select {
		case <-s.done:
			return
		case <-ticker.C:
			s.mu.Lock()
			current := s.conn
			s.mu.Unlock()
			if current != ws {
				return
			}

			ping := relayMessage{Type: relayMsgPing}
			data, _ := json.Marshal(ping)
			_ = ws.SetWriteDeadline(time.Now().Add(extensionWriteWait))
			if err := ws.WriteMessage(websocket.TextMessage, data); err != nil {
				return
			}
		}
	}
}

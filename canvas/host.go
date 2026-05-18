package canvas

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fulcrus/hopclaw/logging"

	"github.com/fsnotify/fsnotify"
	"github.com/gorilla/websocket"
)

var canvasLog = logging.WithSubsystem("canvas")

// ---------------------------------------------------------------------------
// Constants
// ---------------------------------------------------------------------------

const (
	defaultPort         = 18793
	defaultRootSubdir   = ".hopclaw/workspace/canvas"
	liveReloadPath      = "/__hopclaw__/live-reload"
	wsWriteWait         = 5 * time.Second
	wsPongWait          = 60 * time.Second
	wsPingInterval      = 30 * time.Second
	shutdownGracePeriod = 5 * time.Second
)

// ---------------------------------------------------------------------------
// Config
// ---------------------------------------------------------------------------

// HostConfig configures the Canvas host service.
type HostConfig struct {
	Enabled    bool          `json:"enabled" yaml:"enabled"`
	Port       int           `json:"port" yaml:"port"`
	Root       string        `json:"root" yaml:"root"`
	LiveReload bool          `json:"live_reload" yaml:"live_reload"`
	TokenTTL   time.Duration `json:"token_ttl" yaml:"token_ttl"`
}

// ---------------------------------------------------------------------------
// Live-reload notification payload
// ---------------------------------------------------------------------------

type changeNotification struct {
	Type     string `json:"type"`
	Filename string `json:"filename"`
}

// ---------------------------------------------------------------------------
// Host
// ---------------------------------------------------------------------------

// Host serves canvas files over HTTP and provides live-reload notifications
// via a WebSocket endpoint.
type Host struct {
	cfg        HostConfig
	clients    map[*websocket.Conn]bool
	clientsMu  sync.Mutex
	server     *http.Server
	watcher    *fsnotify.Watcher
	components *ComponentRegistry
	tokens     *TokenStore
}

var liveReloadUpgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin:     func(r *http.Request) bool { return true },
}

// NewHost creates a new canvas Host. It validates the configuration and
// ensures the root directory exists.
func NewHost(cfg HostConfig) (*Host, error) {
	if cfg.Port == 0 {
		cfg.Port = defaultPort
	}

	if cfg.Root == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("resolve home directory: %w", err)
		}
		cfg.Root = filepath.Join(home, defaultRootSubdir)
	}

	if err := os.MkdirAll(cfg.Root, 0o755); err != nil {
		return nil, fmt.Errorf("create canvas root directory %s: %w", cfg.Root, err)
	}

	tokenTTL := cfg.TokenTTL
	if tokenTTL <= 0 {
		tokenTTL = defaultTokenTTL
	}

	return &Host{
		cfg:        cfg,
		clients:    make(map[*websocket.Conn]bool),
		components: NewComponentRegistry(),
		tokens:     NewTokenStore(tokenTTL),
	}, nil
}

// Start begins serving canvas files and, if enabled, sets up live-reload
// file watching and the WebSocket endpoint.
func (h *Host) Start() error {
	mux := http.NewServeMux()

	// Static file server for canvas root.
	fileServer := http.FileServer(http.Dir(h.cfg.Root))
	mux.Handle("/", fileServer)

	// A2UI WebSocket and canvas token endpoints.
	mux.HandleFunc("/__hopclaw__/a2ui", h.handleA2UI)
	mux.HandleFunc("/__hopclaw__/canvas/", h.handleCanvasToken)

	// Live-reload WebSocket endpoint.
	if h.cfg.LiveReload {
		mux.HandleFunc(liveReloadPath, h.handleLiveReload)
		if err := h.startWatcher(); err != nil {
			return fmt.Errorf("start file watcher: %w", err)
		}
	}

	h.server = &http.Server{
		Addr:              fmt.Sprintf("127.0.0.1:%d", h.cfg.Port),
		Handler:           mux,
		ReadHeaderTimeout: shutdownGracePeriod,
	}

	go func() {
		if err := h.server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			canvasLog.Error("server listen failed", slog.String("error", err.Error()))
		}
	}()

	return nil
}

// Stop gracefully shuts down the host server and closes all WebSocket clients.
func (h *Host) Stop(ctx context.Context) error {
	if h.tokens != nil {
		h.tokens.Stop()
	}
	if h.watcher != nil {
		_ = h.watcher.Close()
	}

	h.clientsMu.Lock()
	for conn := range h.clients {
		_ = conn.Close()
		delete(h.clients, conn)
	}
	h.clientsMu.Unlock()

	if h.server != nil {
		return h.server.Shutdown(ctx)
	}
	return nil
}

// NotifyChange sends a file change notification to all connected live-reload
// clients.
func (h *Host) NotifyChange(filename string) {
	notification := changeNotification{
		Type:     "change",
		Filename: filename,
	}
	data, err := json.Marshal(notification)
	if err != nil {
		canvasLog.Warn("marshal change notification failed", slog.String("error", err.Error()))
		return
	}

	h.clientsMu.Lock()
	defer h.clientsMu.Unlock()

	for conn := range h.clients {
		_ = conn.SetWriteDeadline(time.Now().Add(wsWriteWait))
		if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
			_ = conn.Close()
			delete(h.clients, conn)
		}
	}
}

// ---------------------------------------------------------------------------
// Internal
// ---------------------------------------------------------------------------

func (h *Host) handleLiveReload(w http.ResponseWriter, r *http.Request) {
	ws, err := liveReloadUpgrader.Upgrade(w, r, nil)
	if err != nil {
		canvasLog.Warn("live-reload upgrade failed", slog.String("error", err.Error()))
		return
	}

	h.clientsMu.Lock()
	h.clients[ws] = true
	h.clientsMu.Unlock()

	// Keep the connection alive by reading (and discarding) client messages.
	// When the client disconnects, clean up.
	defer func() {
		h.clientsMu.Lock()
		delete(h.clients, ws)
		h.clientsMu.Unlock()
		_ = ws.Close()
	}()

	ws.SetReadLimit(512)
	_ = ws.SetReadDeadline(time.Now().Add(wsPongWait))
	ws.SetPongHandler(func(string) error {
		_ = ws.SetReadDeadline(time.Now().Add(wsPongWait))
		return nil
	})

	// Ping loop in a separate goroutine.
	go h.clientPingLoop(ws)

	for {
		if _, _, err := ws.ReadMessage(); err != nil {
			break
		}
	}
}

func (h *Host) clientPingLoop(ws *websocket.Conn) {
	ticker := time.NewTicker(wsPingInterval)
	defer ticker.Stop()

	for range ticker.C {
		h.clientsMu.Lock()
		_, ok := h.clients[ws]
		h.clientsMu.Unlock()
		if !ok {
			return
		}

		_ = ws.SetWriteDeadline(time.Now().Add(wsWriteWait))
		if err := ws.WriteMessage(websocket.PingMessage, nil); err != nil {
			return
		}
	}
}

func (h *Host) startWatcher() error {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return fmt.Errorf("create fsnotify watcher: %w", err)
	}
	h.watcher = watcher

	if err := watcher.Add(h.cfg.Root); err != nil {
		_ = watcher.Close()
		return fmt.Errorf("watch canvas root %s: %w", h.cfg.Root, err)
	}

	go h.watchLoop()
	return nil
}

func (h *Host) watchLoop() {
	for {
		select {
		case event, ok := <-h.watcher.Events:
			if !ok {
				return
			}
			if event.Has(fsnotify.Write) || event.Has(fsnotify.Create) {
				relPath, err := filepath.Rel(h.cfg.Root, event.Name)
				if err != nil {
					relPath = filepath.Base(event.Name)
				}
				h.NotifyChange(relPath)
			}
		case err, ok := <-h.watcher.Errors:
			if !ok {
				return
			}
			canvasLog.Warn("file watcher error", slog.String("error", err.Error()))
		}
	}
}

// ---------------------------------------------------------------------------
// A2UI public API
// ---------------------------------------------------------------------------

// PushComponents updates the registry and broadcasts to all connected A2UI clients.
func (h *Host) PushComponents(sessionID string, components []Component, replace bool) int64 {
	version := h.components.Push(sessionID, components, replace)

	frameType := FrameTypePush
	if replace {
		frameType = FrameTypeState
	}
	frame := Frame{
		Type:       frameType,
		SessionID:  sessionID,
		Version:    version,
		Components: components,
	}
	data, err := EncodeFrame(frame)
	if err != nil {
		canvasLog.Warn("encode a2ui frame failed", "error", err.Error())
		return version
	}
	h.broadcastA2UI(data)
	return version
}

// ResetComponents clears all components and notifies clients.
func (h *Host) ResetComponents(sessionID string) {
	h.components.Reset(sessionID)

	frame := Frame{
		Type:      FrameTypeReset,
		SessionID: sessionID,
	}
	data, err := EncodeFrame(frame)
	if err != nil {
		canvasLog.Warn("encode a2ui reset frame failed", "error", err.Error())
		return
	}
	h.broadcastA2UI(data)
}

// IssueToken creates a capability token for the given session.
func (h *Host) IssueToken(sessionID string) (CapabilityToken, error) {
	return h.tokens.Issue(sessionID)
}

// ComponentRegistry returns the underlying component registry.
func (h *Host) ComponentRegistry() *ComponentRegistry {
	return h.components
}

// ---------------------------------------------------------------------------
// A2UI internal handlers
// ---------------------------------------------------------------------------

const a2uiReadLimit = 4096

func (h *Host) handleA2UI(w http.ResponseWriter, r *http.Request) {
	ws, err := liveReloadUpgrader.Upgrade(w, r, nil)
	if err != nil {
		canvasLog.Warn("a2ui websocket upgrade failed", "error", err.Error())
		return
	}

	h.clientsMu.Lock()
	h.clients[ws] = true
	h.clientsMu.Unlock()

	// Send current state snapshot.
	// (Client can send a subscribe frame with session_id to get filtered state)

	defer func() {
		h.clientsMu.Lock()
		delete(h.clients, ws)
		h.clientsMu.Unlock()
		_ = ws.Close()
	}()

	ws.SetReadLimit(a2uiReadLimit)
	_ = ws.SetReadDeadline(time.Now().Add(wsPongWait))
	ws.SetPongHandler(func(string) error {
		_ = ws.SetReadDeadline(time.Now().Add(wsPongWait))
		return nil
	})

	go h.clientPingLoop(ws)

	for {
		_, msg, err := ws.ReadMessage()
		if err != nil {
			break
		}

		// Handle client messages (e.g., subscribe to a session).
		frame, err := DecodeFrame(msg)
		if err != nil {
			continue
		}
		if frame.Type == FrameTypeAck {
			continue // acknowledgment, no action needed
		}

		// If client sends a state request, respond with current components.
		if frame.Type == FrameTypeState && frame.SessionID != "" {
			components := h.components.Get(frame.SessionID)
			version := h.components.Version(frame.SessionID)
			stateFrame := Frame{
				Type:       FrameTypeState,
				SessionID:  frame.SessionID,
				Version:    version,
				Components: components,
			}
			data, err := EncodeFrame(stateFrame)
			if err == nil {
				_ = ws.SetWriteDeadline(time.Now().Add(wsWriteWait))
				_ = ws.WriteMessage(websocket.TextMessage, data)
			}
		}
	}
}

func (h *Host) handleCanvasToken(w http.ResponseWriter, r *http.Request) {
	// Extract token from path: /__hopclaw__/canvas/{token}
	token := strings.TrimPrefix(r.URL.Path, "/__hopclaw__/canvas/")
	token = strings.TrimSuffix(token, "/")
	if token == "" {
		http.Error(w, "missing token", http.StatusBadRequest)
		return
	}

	sessionID, err := h.tokens.Validate(token)
	if err != nil {
		status := http.StatusUnauthorized
		if err == ErrTokenExpired {
			status = http.StatusGone
		}
		http.Error(w, err.Error(), status)
		return
	}

	// Return the session's components as JSON.
	components := h.components.Get(sessionID)
	if components == nil {
		components = make([]Component, 0)
	}

	type canvasTokenResponse struct {
		SessionID  string      `json:"session_id"`
		Components []Component `json:"components"`
		Version    int64       `json:"version"`
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(canvasTokenResponse{
		SessionID:  sessionID,
		Components: components,
		Version:    h.components.Version(sessionID),
	})
}

func (h *Host) broadcastA2UI(data []byte) {
	h.clientsMu.Lock()
	defer h.clientsMu.Unlock()

	for conn := range h.clients {
		_ = conn.SetWriteDeadline(time.Now().Add(wsWriteWait))
		if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
			_ = conn.Close()
			delete(h.clients, conn)
		}
	}
}

package nodeclient

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"path"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/fulcrus/hopclaw/deviceauth"
	"github.com/fulcrus/hopclaw/internal/support/normalize"
	"github.com/gorilla/websocket"
)

type Handler func(ctx context.Context, params map[string]any) (map[string]any, error)

const (
	operatorWebSocketPath = "/operator/ws"
)

type Config struct {
	GatewayURL        string
	WebSocketURL      string
	DeviceID          string
	Token             string
	Role              deviceauth.DeviceRole
	Scopes            []string
	ClientID          string
	ClientMode        string
	NodeID            string
	DeviceName        string
	Platform          string
	DeviceFamily      string
	Version           string
	ModelIdentifier   string
	Capabilities      []string
	Commands          []string
	HeartbeatInterval time.Duration
	ReconnectDelay    time.Duration
	WebSocketDialer   *websocket.Dialer
	HTTPClient        *http.Client
}

type Client struct {
	cfg           Config
	handlers      map[string]Handler
	writeMu       sync.Mutex
	nextIDCounter atomic.Uint64
}

type PairClaimRequest struct {
	Code         string   `json:"code"`
	DeviceID     string   `json:"device_id,omitempty"`
	Name         string   `json:"name,omitempty"`
	Platform     string   `json:"platform,omitempty"`
	DeviceFamily string   `json:"device_family,omitempty"`
	Role         string   `json:"role,omitempty"`
	Scopes       []string `json:"scopes,omitempty"`
	ExpiresAt    string   `json:"expires_at,omitempty"`
}

type PairClaimResponse struct {
	OK        bool     `json:"ok"`
	DeviceID  string   `json:"device_id"`
	Channel   string   `json:"channel,omitempty"`
	Role      string   `json:"role,omitempty"`
	Scopes    []string `json:"scopes,omitempty"`
	Token     string   `json:"token"`
	WSURL     string   `json:"ws_url,omitempty"`
	ExpiresAt string   `json:"expires_at,omitempty"`
	Error     string   `json:"error,omitempty"`
}

type requestFrame struct {
	Type   string `json:"type"`
	ID     string `json:"id"`
	Method string `json:"method"`
	Params any    `json:"params,omitempty"`
}

type responseFrame struct {
	Type    string          `json:"type"`
	ID      string          `json:"id"`
	OK      bool            `json:"ok"`
	Payload json.RawMessage `json:"payload,omitempty"`
	Error   *frameError     `json:"error,omitempty"`
}

type frameError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type invokeFrame struct {
	Type    string         `json:"type"`
	ID      int            `json:"id"`
	Command string         `json:"command"`
	Params  map[string]any `json:"params,omitempty"`
}

type invokeResultFrame struct {
	Type  string         `json:"type"`
	ID    int            `json:"id"`
	OK    bool           `json:"ok"`
	Data  map[string]any `json:"data,omitempty"`
	Error string         `json:"error,omitempty"`
}

func New(cfg Config) *Client {
	if cfg.Role == "" {
		cfg.Role = deviceauth.RoleNode
	}
	if strings.TrimSpace(cfg.ClientID) == "" {
		cfg.ClientID = strings.TrimSpace(cfg.DeviceID)
	}
	if strings.TrimSpace(cfg.ClientMode) == "" {
		cfg.ClientMode = "node"
	}
	if cfg.HeartbeatInterval <= 0 {
		cfg.HeartbeatInterval = 20 * time.Second
	}
	if cfg.ReconnectDelay <= 0 {
		cfg.ReconnectDelay = 3 * time.Second
	}
	if cfg.HTTPClient == nil {
		cfg.HTTPClient = &http.Client{Timeout: 15 * time.Second}
	}
	return &Client{cfg: cfg, handlers: make(map[string]Handler)}
}

func (c *Client) Register(command string, handler Handler) {
	if c == nil || handler == nil {
		return
	}
	command = strings.TrimSpace(command)
	if command == "" {
		return
	}
	c.handlers[command] = handler
}

func (c *Client) Run(ctx context.Context) error {
	for {
		err := c.runOnce(ctx)
		if ctx.Err() != nil {
			return nil
		}
		if err != nil {
			select {
			case <-time.After(c.cfg.ReconnectDelay):
			case <-ctx.Done():
				return nil
			}
			continue
		}
		return nil
	}
}

func (c *Client) runOnce(ctx context.Context) error {
	wsTarget := normalize.FirstNonEmpty(strings.TrimSpace(c.cfg.WebSocketURL), strings.TrimSpace(c.cfg.GatewayURL))
	wsURL, err := websocketURL(wsTarget)
	if err != nil {
		return err
	}
	dialer := c.cfg.WebSocketDialer
	if dialer == nil {
		dialer = websocket.DefaultDialer
	}
	headers := http.Header{}
	headers.Set("X-HopClaw-Device-Auth", c.authHeader())
	conn, _, err := dialer.DialContext(ctx, wsURL, headers)
	if err != nil {
		return fmt.Errorf("dial gateway websocket: %w", err)
	}
	defer conn.Close()
	conn.SetReadLimit(4 << 20)

	registerID := c.nextID()
	if err := c.writeJSON(conn, requestFrame{
		Type:   "req",
		ID:     registerID,
		Method: "node.register",
		Params: map[string]any{
			"node_id":          normalize.FirstNonEmpty(c.cfg.NodeID, c.cfg.DeviceID),
			"version":          c.cfg.Version,
			"platform":         c.cfg.Platform,
			"device_family":    c.cfg.DeviceFamily,
			"model_identifier": c.cfg.ModelIdentifier,
			"capabilities":     append([]string(nil), c.cfg.Capabilities...),
			"commands":         append([]string(nil), c.cfg.Commands...),
		},
	}); err != nil {
		return err
	}
	if err := c.awaitOKResponse(ctx, conn, registerID); err != nil {
		return err
	}

	hbCtx, hbCancel := context.WithCancel(ctx)
	defer hbCancel()
	go c.heartbeatLoop(hbCtx, conn)

	for {
		_, data, err := conn.ReadMessage()
		if err != nil {
			return err
		}
		var envelope struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(data, &envelope); err != nil {
			continue
		}
		switch envelope.Type {
		case "invoke":
			if err := c.handleInvoke(ctx, conn, data); err != nil {
				return err
			}
		case "res":
			continue
		}
	}
}

func (c *Client) handleInvoke(ctx context.Context, conn *websocket.Conn, raw []byte) error {
	var frame invokeFrame
	if err := json.Unmarshal(raw, &frame); err != nil {
		return nil
	}
	handler, ok := c.handlers[strings.TrimSpace(frame.Command)]
	result := invokeResultFrame{Type: "invoke.result", ID: frame.ID}
	if !ok {
		result.Error = fmt.Sprintf("unsupported command %q", frame.Command)
		return c.writeJSON(conn, result)
	}
	payload, err := handler(ctx, frame.Params)
	if err != nil {
		result.Error = err.Error()
		return c.writeJSON(conn, result)
	}
	result.OK = true
	result.Data = payload
	return c.writeJSON(conn, result)
}

func (c *Client) heartbeatLoop(ctx context.Context, conn *websocket.Conn) {
	ticker := time.NewTicker(c.cfg.HeartbeatInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			_ = c.writeJSON(conn, requestFrame{
				Type:   "req",
				ID:     c.nextID(),
				Method: "node.heartbeat",
				Params: map[string]any{"node_id": normalize.FirstNonEmpty(c.cfg.NodeID, c.cfg.DeviceID)},
			})
		case <-ctx.Done():
			return
		}
	}
}

func (c *Client) awaitOKResponse(ctx context.Context, conn *websocket.Conn, reqID string) error {
	// Set a read deadline so we don't block forever if the server never responds.
	const awaitTimeout = 30 * time.Second
	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetReadDeadline(deadline)
	} else {
		_ = conn.SetReadDeadline(time.Now().Add(awaitTimeout))
	}
	defer conn.SetReadDeadline(time.Time{}) // clear deadline when done
	for {
		_, data, err := conn.ReadMessage()
		if err != nil {
			return err
		}
		var envelope struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(data, &envelope); err != nil {
			continue
		}
		if envelope.Type != "res" {
			continue
		}
		var resp responseFrame
		if err := json.Unmarshal(data, &resp); err != nil {
			continue
		}
		if resp.ID != reqID {
			continue
		}
		if resp.OK {
			return nil
		}
		if resp.Error != nil && strings.TrimSpace(resp.Error.Message) != "" {
			return fmt.Errorf("%s", resp.Error.Message)
		}
		return fmt.Errorf("request %s failed", reqID)
	}
}

func (c *Client) authHeader() string {
	payload := deviceauth.AuthPayload{
		DeviceID:     c.cfg.DeviceID,
		ClientID:     c.cfg.ClientID,
		ClientMode:   c.cfg.ClientMode,
		Role:         c.cfg.Role,
		Scopes:       append([]string(nil), c.cfg.Scopes...),
		SignedAtMs:   time.Now().UTC().UnixMilli(),
		Token:        c.cfg.Token,
		Nonce:        randomHex(8),
		Platform:     c.cfg.Platform,
		DeviceFamily: c.cfg.DeviceFamily,
	}
	return deviceauth.EncodePayloadV3(payload)
}

func (c *Client) writeJSON(conn *websocket.Conn, value any) error {
	body, err := json.Marshal(value)
	if err != nil {
		return err
	}
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	return conn.WriteMessage(websocket.TextMessage, body)
}

func (c *Client) nextID() string {
	return fmt.Sprintf("node-%d-%d", time.Now().UnixNano(), c.nextIDCounter.Add(1))
}

func ClaimPairing(ctx context.Context, gatewayURL string, req PairClaimRequest) (*PairClaimResponse, error) {
	httpURL, err := httpBaseURL(gatewayURL)
	if err != nil {
		return nil, err
	}
	body, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, httpURL+"/device/pair/claim", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var payload PairClaimResponse
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, err
	}
	if resp.StatusCode >= http.StatusBadRequest {
		if strings.TrimSpace(payload.Error) != "" {
			return nil, fmt.Errorf("%s", payload.Error)
		}
		return nil, fmt.Errorf("pairing claim failed: %s", resp.Status)
	}
	return &payload, nil
}

func websocketURL(raw string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return "", fmt.Errorf("parse gateway url: %w", err)
	}
	originalScheme := u.Scheme
	cleanPath := normalizedPath(u.Path)
	switch u.Scheme {
	case "http":
		u.Scheme = "ws"
	case "https":
		u.Scheme = "wss"
	case "ws", "wss":
	default:
		return "", fmt.Errorf("unsupported gateway scheme %q", u.Scheme)
	}
	switch {
	case cleanPath == "" || cleanPath == "/":
		u.Path = operatorWebSocketPath
	case strings.HasSuffix(cleanPath, operatorWebSocketPath):
		u.Path = cleanPath
	case originalScheme == "ws" || originalScheme == "wss":
		return "", fmt.Errorf("websocket gateway url must end with %q", operatorWebSocketPath)
	default:
		u.Path = strings.TrimRight(cleanPath, "/") + operatorWebSocketPath
	}
	return u.String(), nil
}

func httpBaseURL(raw string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return "", fmt.Errorf("parse gateway url: %w", err)
	}
	originalScheme := u.Scheme
	switch u.Scheme {
	case "ws":
		u.Scheme = "http"
	case "wss":
		u.Scheme = "https"
	case "http", "https":
	default:
		return "", fmt.Errorf("unsupported gateway scheme %q", u.Scheme)
	}
	clean := normalizedPath(u.Path)
	switch {
	case clean == "" || clean == "/":
		u.Path = ""
	case strings.HasSuffix(clean, operatorWebSocketPath):
		u.Path = strings.TrimSuffix(clean, operatorWebSocketPath)
	case originalScheme == "ws" || originalScheme == "wss":
		return "", fmt.Errorf("websocket gateway url must end with %q", operatorWebSocketPath)
	default:
		u.Path = clean
	}
	if u.Path == "/" {
		u.Path = ""
	}
	return strings.TrimRight(u.String(), "/"), nil
}

func normalizedPath(raw string) string {
	clean := strings.TrimRight(path.Clean("/"+strings.TrimSpace(raw)), "/")
	if clean == "." {
		return ""
	}
	if clean == "" {
		return "/"
	}
	return clean
}

func randomHex(n int) string {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(buf)
}

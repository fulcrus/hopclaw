package server

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/fulcrus/hopclaw/logging"

	"github.com/gorilla/websocket"
)

// ---------------------------------------------------------------------------
// WebSocket connection buffer sizes
// ---------------------------------------------------------------------------

const (
	wsReadBufferSize  = 4096
	wsWriteBufferSize = 4096
)

// WSConn represents a single authenticated WebSocket connection.
type WSConn struct {
	id         string
	conn       *websocket.Conn
	server     *Server
	ctx        context.Context
	cancel     context.CancelFunc
	clientInfo ConnectClientInfo
	role       string
	scopes     []string
	authScope  requestAuthScope
	authed     bool

	connectedAt time.Time
	sendCh      chan []byte
	done        chan struct{}
	closeOnce   sync.Once

	mu            sync.Mutex // guards bufferedBytes
	bufferedBytes int64
}

func (c *WSConn) releaseBufferedBytes(size int64) {
	if size <= 0 {
		return
	}
	c.mu.Lock()
	c.bufferedBytes -= size
	if c.bufferedBytes < 0 {
		c.bufferedBytes = 0
	}
	c.mu.Unlock()
}

func newWSConn(parent context.Context, id string, conn *websocket.Conn, srv *Server) *WSConn {
	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithCancel(parent)
	return &WSConn{
		id:          id,
		conn:        conn,
		server:      srv,
		ctx:         ctx,
		cancel:      cancel,
		connectedAt: time.Now().UTC(),
		sendCh:      make(chan []byte, wsSendChannelSize),
		done:        make(chan struct{}),
	}
}

func (c *WSConn) context() context.Context {
	if c == nil || c.ctx == nil {
		return context.Background()
	}
	return c.ctx
}

// readPump reads messages from the WebSocket and dispatches them.
// It runs in its own goroutine and is the sole reader of the connection.
func (c *WSConn) readPump() {
	defer func() {
		c.server.wsHub.Unregister(c.id)
		c.Close()
	}()

	c.conn.SetReadLimit(int64(wsMaxPayloadBytes))
	if err := c.conn.SetReadDeadline(time.Now().Add(wsPongWait)); err != nil {
		return
	}
	c.conn.SetPongHandler(func(string) error {
		return c.conn.SetReadDeadline(time.Now().Add(wsPongWait))
	})

	for {
		_, data, err := c.conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseNormalClosure) {
				log.Warn("websocket read error", "conn_id", c.id, "error", err)
			}
			return
		}
		c.handleMessage(data)
	}
}

// writePump writes messages from sendCh to the WebSocket, plus periodic pings.
// It runs in its own goroutine and is the sole writer of the connection.
func (c *WSConn) writePump() {
	ticker := time.NewTicker(wsPingInterval)
	defer func() {
		ticker.Stop()
		c.Close()
	}()

	for {
		select {
		case data, ok := <-c.sendCh:
			if !ok {
				// Channel closed; send a close frame.
				logging.LogIfErr(c.context(), c.conn.WriteControl(
					websocket.CloseMessage,
					websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""),
					time.Now().Add(wsWriteWait),
				), "websocket write control failed")
				return
			}
			if err := c.conn.SetWriteDeadline(time.Now().Add(wsWriteWait)); err != nil {
				return
			}
			if err := c.conn.WriteMessage(websocket.TextMessage, data); err != nil {
				return
			}
			c.releaseBufferedBytes(int64(len(data)))

		case <-ticker.C:
			if err := c.conn.SetWriteDeadline(time.Now().Add(wsWriteWait)); err != nil {
				return
			}
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}

		case <-c.done:
			return
		}
	}
}

// SendEvent pushes an event frame to this connection.
// Non-blocking: drops the event if the send buffer is full.
func (c *WSConn) SendEvent(event string, payload any, seq int64) error {
	frame := EventFrame{
		Type:    frameTypeEvent,
		Event:   event,
		Payload: payload,
		Seq:     seq,
	}
	data, err := json.Marshal(frame)
	if err != nil {
		return fmt.Errorf("ws: failed to marshal event frame: %w", err)
	}
	return c.send(data)
}

// sendResponse sends a response frame.
func (c *WSConn) sendResponse(resp ResponseFrame) error {
	data, err := json.Marshal(resp)
	if err != nil {
		return fmt.Errorf("ws: failed to marshal response frame: %w", err)
	}
	return c.send(data)
}

// send marshals and queues a message for the write pump.
func (c *WSConn) send(data []byte) error {
	select {
	case <-c.done:
		return fmt.Errorf("ws: connection %s closed", c.id)
	default:
	}

	size := int64(len(data))
	c.mu.Lock()
	nextBufferedBytes := c.bufferedBytes + size
	if nextBufferedBytes > int64(wsMaxBufferedBytes) {
		bufferedBytes := c.bufferedBytes
		c.mu.Unlock()
		// Slow consumer: close the connection.
		log.Warn("closing slow consumer", "conn_id", c.id, "buffered_bytes", bufferedBytes)
		c.Close()
		return fmt.Errorf("ws: send buffer overflow for connection %s", c.id)
	}

	select {
	case <-c.done:
		c.mu.Unlock()
		return fmt.Errorf("ws: connection %s closed", c.id)
	case c.sendCh <- data:
		c.bufferedBytes = nextBufferedBytes
		c.mu.Unlock()
		return nil
	default:
		c.mu.Unlock()
		// Channel full: close the connection.
		log.Warn("send channel full", "conn_id", c.id)
		c.Close()
		return fmt.Errorf("ws: send channel full for connection %s", c.id)
	}
}

// Close gracefully closes the connection.
func (c *WSConn) Close() {
	c.closeOnce.Do(func() {
		if c.cancel != nil {
			c.cancel()
		}
		close(c.done)
		if c.conn != nil {
			logging.DebugIfErr(c.conn.Close(), "close websocket connection failed")
		}
	})
}

// handleMessage dispatches an incoming raw message.
func (c *WSConn) handleMessage(data []byte) {
	// Peek at the type field.
	var peek struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(data, &peek); err != nil {
		logging.LogIfErr(c.context(), c.sendResponse(ResponseFrame{
			Type: frameTypeResponse,
			OK:   false,
			Error: &WSError{
				Code:    WSErrInvalidRequest,
				Message: "invalid json",
			},
		}), "send websocket response failed")
		return
	}

	switch peek.Type {
	case frameTypeRequest:
		var frame RequestFrame
		if err := json.Unmarshal(data, &frame); err != nil {
			logging.LogIfErr(c.context(), c.sendResponse(ResponseFrame{
				Type: frameTypeResponse,
				OK:   false,
				Error: &WSError{
					Code:    WSErrInvalidRequest,
					Message: "invalid request frame",
				},
			}), "send websocket response failed")
			return
		}
		c.handleRequest(frame)
	default:
		logging.LogIfErr(c.context(), c.sendResponse(ResponseFrame{
			Type: frameTypeResponse,
			OK:   false,
			Error: &WSError{
				Code:    WSErrInvalidRequest,
				Message: fmt.Sprintf("unknown frame type %q", peek.Type),
			},
		}), "send websocket response failed")
	}
}

// handleRequest processes an RPC request.
func (c *WSConn) handleRequest(frame RequestFrame) {
	ctx := c.context()
	result, wsErr := c.server.dispatchMethod(ctx, c, frame.Method, frame.Params)
	if wsErr != nil {
		logging.LogIfErr(ctx, c.sendResponse(ResponseFrame{
			Type:  frameTypeResponse,
			ID:    frame.ID,
			OK:    false,
			Error: wsErr,
		}), "send websocket response failed")
		return
	}

	payload, err := json.Marshal(result)
	if err != nil {
		logging.LogIfErr(ctx, c.sendResponse(ResponseFrame{
			Type: frameTypeResponse,
			ID:   frame.ID,
			OK:   false,
			Error: &WSError{
				Code:    WSErrInternal,
				Message: "failed to marshal response",
			},
		}), "send websocket response failed")
		return
	}

	logging.LogIfErr(ctx, c.sendResponse(ResponseFrame{
		Type:    frameTypeResponse,
		ID:      frame.ID,
		OK:      true,
		Payload: payload,
	}), "send websocket response failed")
}

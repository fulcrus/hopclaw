package acp

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sync"
	"sync/atomic"
)

type Notification struct {
	Method string
	Params json.RawMessage
}

type InProcessClient struct {
	transport     *Transport
	serverCancel  context.CancelFunc
	serverDone    chan struct{}
	readDone      chan struct{}
	done          chan struct{}
	clientReader  *io.PipeReader
	clientWriter  *io.PipeWriter
	serverReader  *io.PipeReader
	serverWriter  *io.PipeWriter
	notifications chan Notification
	nextID        atomic.Int64
	pendingMu     sync.Mutex
	pending       map[int64]chan *JSONRPCMessage
	closeOnce     sync.Once
}

func NewInProcessClient(parent context.Context, server *Server) (*InProcessClient, error) {
	if server == nil {
		return nil, fmt.Errorf("acp: server is required")
	}

	clientReader, serverWriter := io.Pipe()
	serverReader, clientWriter := io.Pipe()

	serverCtx, cancel := context.WithCancel(parent)
	client := &InProcessClient{
		transport:     NewTransport(clientReader, clientWriter),
		serverCancel:  cancel,
		serverDone:    make(chan struct{}),
		readDone:      make(chan struct{}),
		done:          make(chan struct{}),
		clientReader:  clientReader,
		clientWriter:  clientWriter,
		serverReader:  serverReader,
		serverWriter:  serverWriter,
		notifications: make(chan Notification, 128),
		pending:       make(map[int64]chan *JSONRPCMessage),
	}

	go func() {
		defer close(client.serverDone)
		_ = server.Serve(serverCtx, serverReader, serverWriter)
	}()
	go func() {
		defer close(client.readDone)
		client.readLoop()
	}()

	return client, nil
}

func (c *InProcessClient) Notifications() <-chan Notification {
	return c.notifications
}

func (c *InProcessClient) Close() error {
	c.closeOnce.Do(func() {
		close(c.done)
		c.serverCancel()
		c.transport.Close()
		_ = c.clientWriter.Close()
		_ = c.clientReader.Close()
		_ = c.serverWriter.Close()
		_ = c.serverReader.Close()
		<-c.serverDone
		<-c.readDone

		c.pendingMu.Lock()
		for id, ch := range c.pending {
			close(ch)
			delete(c.pending, id)
		}
		c.pendingMu.Unlock()

		// Drain any notifications buffered before the readLoop noticed the
		// shutdown. Once Close returns, callers expect Notifications() to
		// be observably closed and empty.
		for {
			select {
			case <-c.notifications:
			default:
				close(c.notifications)
				return
			}
		}
	})
	return nil
}

func (c *InProcessClient) Initialize(ctx context.Context, params InitializeParams) (*InitializeResult, error) {
	var result InitializeResult
	if err := c.request(ctx, "initialize", params, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

func (c *InProcessClient) NewSession(ctx context.Context, params NewSessionParams) (*SessionInfo, error) {
	var result SessionInfo
	if err := c.request(ctx, "acp/newSession", params, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

func (c *InProcessClient) LoadSession(ctx context.Context, params LoadSessionParams) (*SessionInfo, error) {
	var result SessionInfo
	if err := c.request(ctx, "acp/loadSession", params, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

func (c *InProcessClient) Prompt(ctx context.Context, params PromptParams) error {
	var result map[string]any
	return c.request(ctx, "acp/prompt", params, &result)
}

func (c *InProcessClient) Cancel(ctx context.Context, params CancelParams) error {
	var result map[string]any
	return c.request(ctx, "acp/cancel", params, &result)
}

func (c *InProcessClient) ListSessions(ctx context.Context, params ListSessionsParams) ([]SessionInfo, error) {
	var result struct {
		Sessions []SessionInfo `json:"sessions"`
	}
	if err := c.request(ctx, "acp/listSessions", params, &result); err != nil {
		return nil, err
	}
	return result.Sessions, nil
}

func (c *InProcessClient) SetMode(ctx context.Context, params SetSessionModeParams) error {
	var result map[string]any
	return c.request(ctx, "acp/setMode", params, &result)
}

func (c *InProcessClient) SetConfigOption(ctx context.Context, params SetConfigOptionParams) error {
	var result map[string]any
	return c.request(ctx, "acp/setConfigOption", params, &result)
}

func (c *InProcessClient) request(ctx context.Context, method string, params any, out any) error {
	id := c.nextID.Add(1)

	var rawParams json.RawMessage
	if params != nil {
		data, err := json.Marshal(params)
		if err != nil {
			return fmt.Errorf("acp: marshal params: %w", err)
		}
		rawParams = data
	}

	respCh := make(chan *JSONRPCMessage, 1)
	c.pendingMu.Lock()
	c.pending[id] = respCh
	c.pendingMu.Unlock()

	defer func() {
		c.pendingMu.Lock()
		delete(c.pending, id)
		c.pendingMu.Unlock()
	}()

	if err := c.transport.Send(&JSONRPCMessage{
		JSONRPC: jsonrpcVersion,
		ID:      id,
		HasID:   true,
		Method:  method,
		Params:  rawParams,
	}); err != nil {
		return err
	}

	select {
	case <-ctx.Done():
		return ctx.Err()
	case resp, ok := <-respCh:
		if !ok || resp == nil {
			return fmt.Errorf("acp: client closed")
		}
		if resp.Error != nil {
			return fmt.Errorf("acp: %s", resp.Error.Message)
		}
		if out != nil && resp.Result != nil {
			if err := json.Unmarshal(resp.Result, out); err != nil {
				return fmt.Errorf("acp: decode result: %w", err)
			}
		}
		return nil
	}
}

func (c *InProcessClient) readLoop() {
	for {
		msg, err := c.transport.Receive()
		if err != nil {
			return
		}

		if msg.HasID {
			id, ok := toInt64(msg.ID)
			if !ok {
				continue
			}
			c.pendingMu.Lock()
			ch := c.pending[id]
			c.pendingMu.Unlock()
			if ch != nil {
				ch <- msg
			}
			continue
		}

		select {
		case c.notifications <- Notification{
			Method: msg.Method,
			Params: msg.Params,
		}:
		case <-c.done:
			return
		}
	}
}

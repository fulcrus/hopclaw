package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/fulcrus/hopclaw/internal/execenv"
)

// ---------------------------------------------------------------------------
// Client timeouts
// ---------------------------------------------------------------------------

const (
	// defaultRequestTimeout is applied when the caller's context has no deadline.
	defaultRequestTimeout = 120 * time.Second

	// pendingChannelSize is the buffer size for per-request response channels.
	pendingChannelSize = 1

	// shutdownGrace is the time to wait for the server process to exit
	// before sending SIGKILL.
	shutdownGrace = 5 * time.Second
)

var (
	clientRestartInitialBackoff  = 2 * time.Second
	clientRestartMaxBackoff      = 5 * time.Minute
	clientRestartHealthyDuration = 2 * time.Minute
	clientRestartMaxAttempts     = 5
)

var errClientClosed = errors.New("client closed")

type clientState string

const (
	clientStateReady    clientState = "ready"
	clientStateStarting clientState = "starting"
	clientStateBackoff  clientState = "backoff"
	clientStateDead     clientState = "dead"
	clientStateClosed   clientState = "closed"
)

type rpcResult struct {
	message *JSONRPCMessage
	err     error
}

type temporaryError struct {
	message string
	cause   error
}

func (e *temporaryError) Error() string {
	if e == nil {
		return ""
	}
	if e.cause == nil {
		return e.message
	}
	return e.message + ": " + e.cause.Error()
}

func (e *temporaryError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.cause
}

func (e *temporaryError) Temporary() bool {
	return true
}

// IsTemporary reports whether err represents a transient MCP restart state.
func IsTemporary(err error) bool {
	type temporary interface {
		Temporary() bool
	}
	var target temporary
	return errors.As(err, &target) && target.Temporary()
}

type clientProcess struct {
	transport       *Transport
	cmd             *exec.Cmd
	stdin           io.Closer
	stdout          io.Closer
	waitDone        chan struct{}
	waitErr         error
	recovering      atomic.Bool
	suppressRestart atomic.Bool
}

// Client connects to an MCP server process via stdio and provides methods to
// invoke MCP operations (initialize, list tools, call tool).
type Client struct {
	cfg        ServerConfig
	managed    bool
	serverInfo Implementation
	tools      []Tool

	nextID  atomic.Int64
	pending map[any]chan rpcResult

	mu             sync.Mutex
	proc           *clientProcess
	state          clientState
	lastError      error
	initialized    bool
	toolsLoaded    bool
	restartPending bool
	restartCount   int
	lastStart      time.Time

	stopOnce  sync.Once
	stopCh    chan struct{}
	restartCh chan error
	wg        sync.WaitGroup
}

// NewClient spawns the MCP server described by cfg and returns a managed client.
func NewClient(cfg ServerConfig) (*Client, error) {
	proc, err := newClientProcess(cfg)
	if err != nil {
		return nil, err
	}

	c := &Client{
		cfg:       cfg,
		managed:   true,
		pending:   make(map[any]chan rpcResult),
		proc:      proc,
		state:     clientStateReady,
		lastStart: time.Now(),
		stopCh:    make(chan struct{}),
		restartCh: make(chan error, 1),
	}
	c.startReadLoop(proc)
	c.wg.Add(1)
	go c.restartLoop()
	return c, nil
}

func newClientProcess(cfg ServerConfig) (*clientProcess, error) {
	cmd := exec.Command(cfg.Command, cfg.Args...)
	if cfg.WorkDir != "" {
		cmd.Dir = cfg.WorkDir
	}
	cmdEnv, err := buildServerCommandEnv(cfg)
	if err != nil {
		return nil, err
	}
	cmd.Env = cmdEnv
	cmd.Stderr = os.Stderr

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("create stdin pipe: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		_ = stdin.Close()
		return nil, fmt.Errorf("create stdout pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		_ = stdin.Close()
		_ = stdout.Close()
		return nil, fmt.Errorf("start mcp server %q: %w", cfg.Command, err)
	}

	proc := &clientProcess{
		transport: NewTransport(stdout, stdin),
		cmd:       cmd,
		stdin:     stdin,
		stdout:    stdout,
		waitDone:  make(chan struct{}),
	}
	go func() {
		proc.waitErr = cmd.Wait()
		close(proc.waitDone)
	}()
	return proc, nil
}

func buildServerCommandEnv(cfg ServerConfig) ([]string, error) {
	resolved, err := execenv.DefaultSecretResolver().ResolveMap(cfg.Env)
	if err != nil {
		return nil, fmt.Errorf("resolve mcp server env: %w", err)
	}
	return execenv.BuildChildEnv(execenv.ModuleExecProfile, nil, resolved, nil, nil), nil
}

// newClientFromTransport creates a Client from an existing transport. This is
// used by tests that wire up in-process pipes instead of spawning a process.
func newClientFromTransport(t *Transport) *Client {
	proc := &clientProcess{
		transport: t,
		waitDone:  make(chan struct{}),
	}
	close(proc.waitDone)

	c := &Client{
		pending: make(map[any]chan rpcResult),
		proc:    proc,
		state:   clientStateReady,
		stopCh:  make(chan struct{}),
	}
	c.startReadLoop(proc)
	return c
}

// Initialize performs the MCP initialize handshake. It must be called before
// any other method.
func (c *Client) Initialize(ctx context.Context) (*InitializeResult, error) {
	proc, err := c.currentProcess()
	if err != nil {
		return nil, err
	}
	return c.performInitialize(ctx, proc)
}

func (c *Client) performInitialize(ctx context.Context, proc *clientProcess) (*InitializeResult, error) {
	params := InitializeParams{
		ProtocolVersion: ProtocolVersion,
		Capabilities:    ClientCapabilities{},
		ClientInfo: Implementation{
			Name:    ClientName,
			Version: ClientVersion,
		},
	}

	resp, err := c.sendRequestOnProcess(ctx, proc, MethodInitialize, params)
	if err != nil {
		return nil, fmt.Errorf("initialize request: %w", err)
	}
	if resp.Error != nil {
		return nil, fmt.Errorf("initialize error %d: %s", resp.Error.Code, resp.Error.Message)
	}

	var result InitializeResult
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		return nil, fmt.Errorf("decode initialize result: %w", err)
	}

	notification := &JSONRPCMessage{
		JSONRPC: JSONRPC,
		Method:  MethodInitialized,
	}
	if err := proc.transport.Send(notification); err != nil {
		return nil, fmt.Errorf("send initialized notification: %w", err)
	}

	c.mu.Lock()
	c.serverInfo = result.ServerInfo
	c.initialized = true
	c.mu.Unlock()
	return &result, nil
}

// ListTools requests the list of tools from the MCP server and caches the result.
func (c *Client) ListTools(ctx context.Context) ([]Tool, error) {
	proc, err := c.currentProcess()
	if err != nil {
		return nil, err
	}
	return c.performListTools(ctx, proc)
}

func (c *Client) performListTools(ctx context.Context, proc *clientProcess) ([]Tool, error) {
	resp, err := c.sendRequestOnProcess(ctx, proc, MethodToolsList, nil)
	if err != nil {
		return nil, fmt.Errorf("list tools request: %w", err)
	}
	if resp.Error != nil {
		return nil, fmt.Errorf("list tools error %d: %s", resp.Error.Code, resp.Error.Message)
	}

	var result ToolListResult
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		return nil, fmt.Errorf("decode tools list: %w", err)
	}

	c.mu.Lock()
	c.tools = append([]Tool(nil), result.Tools...)
	c.toolsLoaded = true
	c.mu.Unlock()
	return result.Tools, nil
}

// CallTool invokes a tool on the MCP server.
func (c *Client) CallTool(ctx context.Context, name string, args map[string]any) (*CallToolResult, error) {
	proc, err := c.currentProcess()
	if err != nil {
		return nil, err
	}

	params := CallToolParams{
		Name:      name,
		Arguments: args,
	}
	resp, err := c.sendRequestOnProcess(ctx, proc, MethodToolsCall, params)
	if err != nil {
		return nil, fmt.Errorf("call tool %q: %w", name, err)
	}
	if resp.Error != nil {
		return nil, fmt.Errorf("call tool %q error %d: %s", name, resp.Error.Code, resp.Error.Message)
	}

	var result CallToolResult
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		return nil, fmt.Errorf("decode tool result for %q: %w", name, err)
	}
	return &result, nil
}

// Close shuts down the MCP server process and transport.
func (c *Client) Close() error {
	c.stopOnce.Do(func() {
		close(c.stopCh)
	})

	c.mu.Lock()
	if c.state == clientStateClosed {
		c.mu.Unlock()
		c.wg.Wait()
		return nil
	}
	c.state = clientStateClosed
	c.lastError = errClientClosed
	proc := c.proc
	c.proc = nil
	pending := c.takePendingLocked()
	c.mu.Unlock()

	deliverPending(pending, rpcResult{err: errClientClosed})
	if proc != nil {
		_ = proc.close(shutdownGrace)
	}
	c.wg.Wait()
	return nil
}

// ---------------------------------------------------------------------------
// Internal
// ---------------------------------------------------------------------------

func (c *Client) currentProcess() (*clientProcess, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.currentProcessLocked()
}

func (c *Client) currentProcessLocked() (*clientProcess, error) {
	switch c.state {
	case clientStateReady:
		if c.proc == nil {
			return nil, c.temporaryErrorLocked("is reconnecting")
		}
		return c.proc, nil
	case clientStateStarting, clientStateBackoff:
		return nil, c.temporaryErrorLocked("is restarting")
	case clientStateDead:
		if c.lastError != nil {
			return nil, fmt.Errorf("mcp server %q is dead: %w", c.serverLabelLocked(), c.lastError)
		}
		return nil, fmt.Errorf("mcp server %q is dead", c.serverLabelLocked())
	case clientStateClosed:
		return nil, errClientClosed
	default:
		return nil, c.temporaryErrorLocked("is unavailable")
	}
}

func (c *Client) serverLabelLocked() string {
	if label := strings.TrimSpace(c.cfg.Name); label != "" {
		return label
	}
	return "mcp"
}

func (c *Client) temporaryErrorLocked(message string) error {
	msg := fmt.Sprintf("mcp server %q %s", c.serverLabelLocked(), message)
	if c.lastError != nil {
		return &temporaryError{message: msg, cause: c.lastError}
	}
	return &temporaryError{message: msg}
}

func (c *Client) classifyProcessError(err error) error {
	if err == nil {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()

	switch c.state {
	case clientStateClosed:
		return errClientClosed
	case clientStateDead:
		if c.lastError != nil {
			return fmt.Errorf("mcp server %q is dead: %w", c.serverLabelLocked(), c.lastError)
		}
		return fmt.Errorf("mcp server %q is dead", c.serverLabelLocked())
	default:
		if c.managed {
			return c.temporaryErrorLocked("is restarting")
		}
		return err
	}
}

// sendRequestOnProcess sends a JSON-RPC request and waits for its response.
func (c *Client) sendRequestOnProcess(ctx context.Context, proc *clientProcess, method string, params any) (*JSONRPCMessage, error) {
	id := c.nextID.Add(1)

	var rawParams json.RawMessage
	if params != nil {
		data, err := json.Marshal(params)
		if err != nil {
			return nil, fmt.Errorf("marshal params: %w", err)
		}
		rawParams = data
	}

	ch := make(chan rpcResult, pendingChannelSize)
	c.mu.Lock()
	if c.state == clientStateClosed {
		c.mu.Unlock()
		return nil, errClientClosed
	}
	c.pending[float64(id)] = ch
	c.mu.Unlock()

	defer func() {
		c.mu.Lock()
		delete(c.pending, float64(id))
		c.mu.Unlock()
	}()

	msg := &JSONRPCMessage{
		JSONRPC: JSONRPC,
		ID:      id,
		Method:  method,
		Params:  rawParams,
	}
	if err := proc.transport.Send(msg); err != nil {
		return nil, c.classifyProcessError(err)
	}

	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, defaultRequestTimeout)
		defer cancel()
	}

	select {
	case resp := <-ch:
		if resp.err != nil {
			return nil, resp.err
		}
		return resp.message, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-c.stopCh:
		return nil, errClientClosed
	}
}

func (c *Client) startReadLoop(proc *clientProcess) {
	c.wg.Add(1)
	go c.readLoop(proc)
}

// readLoop continuously reads messages from the transport and routes responses
// to the pending request channels.
func (c *Client) readLoop(proc *clientProcess) {
	defer c.wg.Done()

	for {
		msg, err := proc.transport.Receive()
		if err != nil {
			c.handleProcessExit(proc, err)
			return
		}

		if !msg.IsResponse() {
			continue
		}

		c.mu.Lock()
		ch, ok := c.pending[msg.ID]
		c.mu.Unlock()
		if ok {
			select {
			case ch <- rpcResult{message: msg}:
			default:
			}
		}
	}
}

func (c *Client) handleProcessExit(proc *clientProcess, recvErr error) {
	if proc.stdout != nil {
		_ = proc.stdout.Close()
	}
	if proc.stdin != nil {
		_ = proc.stdin.Close()
	}
	if proc.transport != nil {
		_ = proc.transport.Close()
	}

	exitErr := recvErr
	select {
	case <-proc.waitDone:
		if proc.waitErr != nil {
			exitErr = proc.waitErr
		}
	default:
	}
	if exitErr == nil {
		exitErr = io.EOF
	}

	c.mu.Lock()
	if c.proc == proc {
		c.proc = nil
	}
	if c.state == clientStateClosed || proc.suppressRestart.Load() {
		c.mu.Unlock()
		return
	}

	pending := c.takePendingLocked()
	recovering := proc.recovering.Load()
	restartRequested := false
	if c.managed {
		c.state = clientStateStarting
		c.lastError = exitErr
		if !recovering && !c.restartPending {
			c.restartPending = true
			restartRequested = true
		}
	} else {
		c.state = clientStateDead
		c.lastError = exitErr
	}
	c.mu.Unlock()

	if c.managed {
		deliverPending(pending, rpcResult{
			err: &temporaryError{
				message: fmt.Sprintf("mcp server %q is restarting", strings.TrimSpace(c.cfg.Name)),
				cause:   exitErr,
			},
		})
		if restartRequested {
			select {
			case c.restartCh <- exitErr:
			case <-c.stopCh:
			default:
			}
		}
		return
	}

	deliverPending(pending, rpcResult{err: exitErr})
}

func (c *Client) restartLoop() {
	defer c.wg.Done()

	backoff := clientRestartInitialBackoff
	for {
		select {
		case <-c.stopCh:
			return
		case cause := <-c.restartCh:
			for {
				c.mu.Lock()
				if c.state == clientStateClosed {
					c.restartPending = false
					c.mu.Unlock()
					return
				}
				if !c.lastStart.IsZero() && time.Since(c.lastStart) >= clientRestartHealthyDuration {
					c.restartCount = 0
					backoff = clientRestartInitialBackoff
				}
				c.restartCount++
				if clientRestartMaxAttempts > 0 && c.restartCount > clientRestartMaxAttempts {
					c.state = clientStateDead
					c.restartPending = false
					if cause != nil {
						c.lastError = fmt.Errorf("restart limit exceeded: %w", cause)
					}
					c.mu.Unlock()
					return
				}
				delay := backoff
				if delay <= 0 {
					delay = clientRestartInitialBackoff
				}
				c.state = clientStateBackoff
				c.lastError = cause
				c.mu.Unlock()

				timer := time.NewTimer(delay)
				select {
				case <-c.stopCh:
					timer.Stop()
					return
				case <-timer.C:
				}

				proc, err := newClientProcess(c.cfg)
				if err != nil {
					cause = err
					backoff = nextRestartBackoff(backoff)
					continue
				}
				proc.recovering.Store(true)

				c.mu.Lock()
				if c.state == clientStateClosed {
					c.mu.Unlock()
					proc.suppressRestart.Store(true)
					_ = proc.close(shutdownGrace)
					return
				}
				c.proc = proc
				c.state = clientStateStarting
				c.lastStart = time.Now()
				c.mu.Unlock()

				c.startReadLoop(proc)
				restoreErr := c.restoreProcess(proc)
				proc.recovering.Store(false)
				if restoreErr != nil {
					proc.suppressRestart.Store(true)
					_ = proc.close(shutdownGrace)
					cause = restoreErr
					backoff = nextRestartBackoff(backoff)
					continue
				}

				c.mu.Lock()
				if c.state != clientStateClosed {
					c.state = clientStateReady
					c.lastError = nil
				}
				c.restartPending = false
				c.mu.Unlock()
				break
			}
		}
	}
}

func (c *Client) restoreProcess(proc *clientProcess) error {
	c.mu.Lock()
	needsInitialize := c.initialized
	needsTools := c.toolsLoaded
	c.mu.Unlock()

	if !needsInitialize && !needsTools {
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), initializeTimeout)
	defer cancel()

	if needsInitialize {
		if _, err := c.performInitialize(ctx, proc); err != nil {
			return fmt.Errorf("initialize: %w", err)
		}
	}
	if needsTools {
		if _, err := c.performListTools(ctx, proc); err != nil {
			return fmt.Errorf("list tools: %w", err)
		}
	}
	return nil
}

func (c *Client) takePendingLocked() map[any]chan rpcResult {
	if len(c.pending) == 0 {
		return nil
	}
	pending := c.pending
	c.pending = make(map[any]chan rpcResult)
	return pending
}

func deliverPending(pending map[any]chan rpcResult, result rpcResult) {
	for _, ch := range pending {
		select {
		case ch <- result:
		default:
		}
	}
}

func nextRestartBackoff(current time.Duration) time.Duration {
	if current <= 0 {
		return clientRestartInitialBackoff
	}
	current *= 2
	if clientRestartMaxBackoff > 0 && current > clientRestartMaxBackoff {
		return clientRestartMaxBackoff
	}
	return current
}

func (p *clientProcess) close(grace time.Duration) error {
	if p == nil {
		return nil
	}
	p.suppressRestart.Store(true)
	if p.transport != nil {
		_ = p.transport.Close()
	}
	if p.stdin != nil {
		_ = p.stdin.Close()
	}
	if p.stdout != nil {
		_ = p.stdout.Close()
	}
	if p.cmd == nil || p.cmd.Process == nil {
		return nil
	}

	if grace > 0 {
		timer := time.NewTimer(grace)
		defer timer.Stop()
		select {
		case <-p.waitDone:
			return p.waitErr
		case <-timer.C:
		}
	}

	_ = p.cmd.Process.Kill()
	<-p.waitDone
	return p.waitErr
}

func (c *Client) toolSnapshot() []Tool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.tools) == 0 {
		return nil
	}
	return append([]Tool(nil), c.tools...)
}

func (c *Client) statusSnapshot() ServerStatus {
	c.mu.Lock()
	defer c.mu.Unlock()

	status := ServerStatus{
		Connected: c.state == clientStateReady,
	}
	if status.Connected {
		status.Tools = len(c.tools)
	}
	if c.lastError != nil {
		status.Error = c.lastError.Error()
	}
	return status
}

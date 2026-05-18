package stdio

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"sync/atomic"
	"time"

	"github.com/fulcrus/hopclaw/channels"
	"github.com/fulcrus/hopclaw/internal/execenv"
	"github.com/fulcrus/hopclaw/logging"
)

var log = logging.WithSubsystem("channel.stdio")

// ---------------------------------------------------------------------------
// Constants
// ---------------------------------------------------------------------------

const (
	// scannerInitBuf is the initial buffer size for the NDJSON scanner.
	scannerInitBuf = 64 * 1024 // 64 KB

	// scannerMaxBuf is the maximum buffer size for the NDJSON scanner.
	scannerMaxBuf = 4 * 1024 * 1024 // 4 MB

	// initializeTimeout is the maximum time for the handshake.
	initializeTimeout = 30 * time.Second

	// connectTimeout is the maximum time for the connect call.
	connectTimeout = 30 * time.Second

	// disconnectTimeout is the maximum time for the disconnect call.
	disconnectTimeout = 10 * time.Second

	// sendTimeout is the default timeout for a send call.
	sendTimeout = 30 * time.Second

	// shutdownGrace is the time to wait for the process to exit before SIGKILL.
	shutdownGrace = 5 * time.Second

	// pendingChannelSize is the buffer for per-request response channels.
	pendingChannelSize = 1
)

// ---------------------------------------------------------------------------
// Config
// ---------------------------------------------------------------------------

// Config describes how to spawn and communicate with a channel plugin.
type Config struct {
	// Name is the channel name used for registration and session keys.
	Name string

	// Command is the executable path for the plugin process.
	Command string

	// Args are command-line arguments passed to the plugin.
	Args []string

	// Env is extra environment variables. Values may be literals, env:VAR, or
	// keychain:key references and are applied to a sanitized child env.
	Env map[string]string

	// WorkDir is the working directory for the plugin process.
	WorkDir string

	// ChannelConfig is opaque config forwarded to the plugin on connect.
	ChannelConfig map[string]any
}

// ---------------------------------------------------------------------------
// Adapter
// ---------------------------------------------------------------------------

// Adapter implements channels.Adapter by spawning an external plugin process
// and communicating over JSON-RPC 2.0 / NDJSON on stdio.
type Adapter struct {
	cfg Config

	base        channels.BaseAdapter
	lifecycleMu sync.Mutex
	mu          sync.RWMutex
	caps        channels.Capabilities
	plugInfo    InitializeResult

	cmd     *exec.Cmd
	scanner *bufio.Scanner
	writer  io.Writer
	writerM sync.Mutex // guards writer

	nextID  atomic.Int64
	pending map[any]chan *Message
	pendMu  sync.Mutex

	done chan struct{}
}

// New creates a new stdio channel adapter with the given config.
func New(cfg Config) *Adapter {
	return &Adapter{
		cfg:     cfg,
		base:    channels.NewBaseAdapter("stdio"),
		pending: make(map[any]chan *Message),
	}
}

// ---------------------------------------------------------------------------
// channels.Adapter interface
// ---------------------------------------------------------------------------

// Connect spawns the plugin process, performs the handshake, and tells the
// plugin to connect to its platform.
func (a *Adapter) Connect(ctx context.Context) error {
	a.lifecycleMu.Lock()
	defer a.lifecycleMu.Unlock()

	status := a.base.Status()
	if status == channels.StatusConnected || status == channels.StatusConnecting {
		return nil
	}
	a.base.SetStatus(channels.StatusConnecting)

	if err := a.spawnProcess(); err != nil {
		a.base.SetStatus(channels.StatusError)
		return fmt.Errorf("stdio channel %s: spawn process: %w", a.cfg.Name, err)
	}

	// Handshake.
	initCtx, cancel := context.WithTimeout(ctx, initializeTimeout)
	defer cancel()

	result, err := a.initialize(initCtx)
	if err != nil {
		a.killProcess()
		a.base.SetStatus(channels.StatusError)
		return fmt.Errorf("stdio channel %s: initialize: %w", a.cfg.Name, err)
	}
	a.mu.Lock()
	a.plugInfo = *result
	a.caps = result.Capabilities.ToChannelCapabilities()
	a.mu.Unlock()

	// Tell plugin to connect.
	connCtx, connCancel := context.WithTimeout(ctx, connectTimeout)
	defer connCancel()

	connResult, err := a.callConnect(connCtx)
	if err != nil {
		a.killProcess()
		a.base.SetStatus(channels.StatusError)
		return fmt.Errorf("stdio channel %s: connect: %w", a.cfg.Name, err)
	}
	if !connResult.OK {
		a.killProcess()
		a.base.SetStatus(channels.StatusError)
		return fmt.Errorf("stdio channel %s: connect rejected: %s", a.cfg.Name, connResult.Error)
	}

	a.base.MarkConnected(nil)
	log.Info("stdio channel connected", "name", a.cfg.Name, "plugin", result.PluginName, "version", result.PluginVersion)
	return nil
}

// Disconnect tells the plugin to disconnect and shuts down the process.
func (a *Adapter) Disconnect(ctx context.Context) error {
	a.lifecycleMu.Lock()
	defer a.lifecycleMu.Unlock()

	if a.base.Status() == channels.StatusDisconnected {
		return nil
	}

	// Best-effort disconnect call.
	discCtx, cancel := context.WithTimeout(ctx, disconnectTimeout)
	defer cancel()

	_ = a.callDisconnect(discCtx)
	a.killProcess()
	a.base.MarkDisconnected()
	log.Info("stdio channel disconnected", "name", a.cfg.Name)
	return nil
}

// Send delivers an outbound message through the plugin.
func (a *Adapter) Send(ctx context.Context, msg channels.OutboundMessage) error {
	if a.base.Status() != channels.StatusConnected {
		return fmt.Errorf("stdio channel %s: adapter is not connected", a.cfg.Name)
	}

	params := SendParams{
		ChannelID: msg.ChannelID,
		TargetID:  msg.TargetID,
		ReplyToID: msg.ReplyToID,
		Content:   msg.Content,
		Format:    msg.Format,
		Metadata:  msg.Metadata,
	}

	sendCtx, cancel := context.WithTimeout(ctx, sendTimeout)
	defer cancel()

	resp, err := a.sendRequest(sendCtx, MethodSend, params)
	if err != nil {
		return fmt.Errorf("stdio channel %s: send: %w", a.cfg.Name, err)
	}
	if resp.Error != nil {
		return fmt.Errorf("stdio channel %s: send error %d: %s", a.cfg.Name, resp.Error.Code, resp.Error.Message)
	}

	var result SendResult
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		return fmt.Errorf("stdio channel %s: decode send result: %w", a.cfg.Name, err)
	}
	if !result.OK {
		return fmt.Errorf("stdio channel %s: send rejected: %s", a.cfg.Name, result.Error)
	}
	return nil
}

// Capabilities returns the plugin's declared capabilities.
func (a *Adapter) Capabilities() channels.ChannelCapabilityDescriptor {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.caps
}

// Status returns the current connection state.
func (a *Adapter) Status() channels.Status {
	return a.base.Status()
}

// SubscribeEvents returns the channel that receives inbound messages from
// the plugin.
func (a *Adapter) SubscribeEvents() <-chan channels.InboundMessage {
	return a.base.SubscribeEvents()
}

// ---------------------------------------------------------------------------
// Process lifecycle
// ---------------------------------------------------------------------------

func (a *Adapter) spawnProcess() error {
	cmd := exec.Command(a.cfg.Command, a.cfg.Args...)
	if a.cfg.WorkDir != "" {
		cmd.Dir = a.cfg.WorkDir
	}
	cmdEnv, err := buildPluginProcessEnv(a.cfg)
	if err != nil {
		return err
	}
	cmd.Env = cmdEnv
	cmd.Stderr = os.Stderr

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("create stdin pipe: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("create stdout pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start plugin %q: %w", a.cfg.Command, err)
	}

	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 0, scannerInitBuf), scannerMaxBuf)

	a.mu.Lock()
	a.cmd = cmd
	a.scanner = scanner
	a.writer = stdin
	a.done = make(chan struct{})
	a.mu.Unlock()

	go a.readLoop()

	return nil
}

func buildPluginProcessEnv(cfg Config) ([]string, error) {
	resolved, err := execenv.DefaultSecretResolver().ResolveMap(cfg.Env)
	if err != nil {
		return nil, fmt.Errorf("resolve plugin env: %w", err)
	}
	return execenv.BuildChildEnv(execenv.ModuleExecProfile, nil, resolved, nil, nil), nil
}

func (a *Adapter) killProcess() {
	a.mu.RLock()
	cmd := a.cmd
	done := a.done
	a.mu.RUnlock()

	if cmd == nil || cmd.Process == nil {
		return
	}

	// Close the writer to signal EOF to the plugin.
	a.writerM.Lock()
	if closer, ok := a.writer.(io.Closer); ok {
		closer.Close()
	}
	a.writerM.Unlock()

	// Wait for graceful exit.
	waitDone := make(chan error, 1)
	go func() {
		waitDone <- cmd.Wait()
	}()

	select {
	case <-waitDone:
	case <-time.After(shutdownGrace):
		cmd.Process.Kill()
		<-waitDone
	}

	// Wait for read loop to finish.
	if done != nil {
		<-done
	}

	// Drain pending requests.
	a.pendMu.Lock()
	for id, ch := range a.pending {
		close(ch)
		delete(a.pending, id)
	}
	a.pendMu.Unlock()
}

// ---------------------------------------------------------------------------
// Read loop — receives messages from the plugin process
// ---------------------------------------------------------------------------

func (a *Adapter) readLoop() {
	defer func() {
		a.mu.RLock()
		done := a.done
		a.mu.RUnlock()
		if done != nil {
			close(done)
		}
	}()

	for {
		a.mu.RLock()
		scanner := a.scanner
		a.mu.RUnlock()

		if scanner == nil {
			return
		}

		if !scanner.Scan() {
			if err := scanner.Err(); err != nil {
				log.Error("stdio channel read error", "name", a.cfg.Name, "error", err)
			}
			return
		}

		line := scanner.Bytes()
		var msg Message
		if err := json.Unmarshal(line, &msg); err != nil {
			log.Error("stdio channel unmarshal error", "name", a.cfg.Name, "error", err)
			continue
		}

		if msg.IsResponse() {
			a.routeResponse(&msg)
			continue
		}

		if msg.IsNotification() {
			a.handleNotification(&msg)
			continue
		}
	}
}

func (a *Adapter) routeResponse(msg *Message) {
	a.pendMu.Lock()
	ch, ok := a.pending[msg.ID]
	a.pendMu.Unlock()
	if ok {
		ch <- msg
	}
}

func (a *Adapter) handleNotification(msg *Message) {
	switch msg.Method {
	case MethodChannelInbound:
		var notif InboundNotification
		if err := json.Unmarshal(msg.Params, &notif); err != nil {
			log.Error("stdio channel: decode inbound notification", "name", a.cfg.Name, "error", err)
			return
		}
		a.base.PublishInbound(notif.ToInboundMessage(), func() {
			log.Warn("stdio channel: inbound buffer full, dropping message", "name", a.cfg.Name)
		})

	case MethodChannelStatus:
		var notif StatusNotification
		if err := json.Unmarshal(msg.Params, &notif); err != nil {
			log.Error("stdio channel: decode status notification", "name", a.cfg.Name, "error", err)
			return
		}
		newStatus := channels.Status(notif.Status)
		a.base.SetStatus(newStatus)
		log.Info("stdio channel status update", "name", a.cfg.Name, "status", notif.Status, "message", notif.Message)

	default:
		log.Warn("stdio channel: unknown notification", "name", a.cfg.Name, "method", msg.Method)
	}
}

// ---------------------------------------------------------------------------
// JSON-RPC request/response
// ---------------------------------------------------------------------------

func (a *Adapter) sendRaw(msg *Message) error {
	data, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("marshal message: %w", err)
	}
	data = append(data, '\n')

	a.writerM.Lock()
	defer a.writerM.Unlock()

	if a.writer == nil {
		return fmt.Errorf("writer is nil")
	}
	if _, err := a.writer.Write(data); err != nil {
		return fmt.Errorf("write message: %w", err)
	}
	return nil
}

func (a *Adapter) sendRequest(ctx context.Context, method string, params any) (*Message, error) {
	id := a.nextID.Add(1)

	var rawParams json.RawMessage
	if params != nil {
		data, err := json.Marshal(params)
		if err != nil {
			return nil, fmt.Errorf("marshal params: %w", err)
		}
		rawParams = data
	}

	ch := make(chan *Message, pendingChannelSize)
	a.pendMu.Lock()
	a.pending[float64(id)] = ch // JSON decoding turns int64 to float64
	a.pendMu.Unlock()

	defer func() {
		a.pendMu.Lock()
		delete(a.pending, float64(id))
		a.pendMu.Unlock()
	}()

	msg := &Message{
		JSONRPC: JSONRPC,
		ID:      id,
		Method:  method,
		Params:  rawParams,
	}
	if err := a.sendRaw(msg); err != nil {
		return nil, err
	}

	select {
	case resp, ok := <-ch:
		if !ok {
			return nil, fmt.Errorf("plugin process exited")
		}
		return resp, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// ---------------------------------------------------------------------------
// Protocol helpers
// ---------------------------------------------------------------------------

func (a *Adapter) initialize(ctx context.Context) (*InitializeResult, error) {
	params := InitializeParams{
		ProtocolVersion: ProtocolVersion,
		HostName:        "hopclaw",
		HostVersion:     "0.1.0",
	}

	resp, err := a.sendRequest(ctx, MethodInitialize, params)
	if err != nil {
		return nil, err
	}
	if resp.Error != nil {
		return nil, fmt.Errorf("error %d: %s", resp.Error.Code, resp.Error.Message)
	}

	var result InitializeResult
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		return nil, fmt.Errorf("decode initialize result: %w", err)
	}
	return &result, nil
}

func (a *Adapter) callConnect(ctx context.Context) (*ConnectResult, error) {
	params := ConnectParams{
		Config: a.cfg.ChannelConfig,
	}

	resp, err := a.sendRequest(ctx, MethodConnect, params)
	if err != nil {
		return nil, err
	}
	if resp.Error != nil {
		return nil, fmt.Errorf("error %d: %s", resp.Error.Code, resp.Error.Message)
	}

	var result ConnectResult
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		return nil, fmt.Errorf("decode connect result: %w", err)
	}
	return &result, nil
}

func (a *Adapter) callDisconnect(ctx context.Context) error {
	resp, err := a.sendRequest(ctx, MethodDisconnect, nil)
	if err != nil {
		return err
	}
	if resp.Error != nil {
		return fmt.Errorf("error %d: %s", resp.Error.Code, resp.Error.Message)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

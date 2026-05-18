package stdio

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/fulcrus/hopclaw/channels"
)

// ---------------------------------------------------------------------------
// Mock plugin process — simulates a channel plugin over in-process pipes
// ---------------------------------------------------------------------------

type mockPlugin struct {
	scanner *bufio.Scanner
	writer  io.Writer
	writerM sync.Mutex
}

func newMockPluginPipes() (hostReader io.Reader, hostWriter io.Writer, plug *mockPlugin) {
	// Host writes to pluginReader, plugin writes to hostReader.
	pluginReader, hostWriter := io.Pipe()
	hostReader, pluginWriter := io.Pipe()

	scanner := bufio.NewScanner(pluginReader)
	scanner.Buffer(make([]byte, 0, scannerInitBuf), scannerMaxBuf)

	plug = &mockPlugin{
		scanner: scanner,
		writer:  pluginWriter,
	}
	return hostReader, hostWriter, plug
}

func (p *mockPlugin) send(msg *Message) {
	data, _ := json.Marshal(msg)
	data = append(data, '\n')
	p.writerM.Lock()
	defer p.writerM.Unlock()
	p.writer.Write(data)
}

func (p *mockPlugin) receive() *Message {
	if !p.scanner.Scan() {
		return nil
	}
	var msg Message
	json.Unmarshal(p.scanner.Bytes(), &msg)
	return &msg
}

func (p *mockPlugin) respondOK(id any, result any) {
	data, _ := json.Marshal(result)
	p.send(&Message{
		JSONRPC: JSONRPC,
		ID:      id,
		Result:  data,
	})
}

// serveLoop handles the standard handshake+connect sequence and then
// echoes inbound messages back as notifications.
func (p *mockPlugin) serveLoop(t *testing.T) {
	t.Helper()
	for {
		msg := p.receive()
		if msg == nil {
			return
		}

		switch msg.Method {
		case MethodInitialize:
			p.respondOK(msg.ID, InitializeResult{
				ProtocolVersion: ProtocolVersion,
				PluginName:      "test-plugin",
				PluginVersion:   "1.0.0",
				Capabilities: PluginCapability{
					SendText: true,
				},
			})

		case MethodConnect:
			p.respondOK(msg.ID, ConnectResult{OK: true})

		case MethodDisconnect:
			p.respondOK(msg.ID, DisconnectResult{OK: true})
			return

		case MethodSend:
			p.respondOK(msg.ID, SendResult{OK: true, MessageID: "msg-123"})

		default:
			t.Logf("mock plugin: unexpected method %s", msg.Method)
		}
	}
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

func TestAdapterConnectDisconnect(t *testing.T) {
	hostReader, hostWriter, plug := newMockPluginPipes()

	a := &Adapter{
		cfg:     Config{Name: "test"},
		base:    channels.NewBaseAdapter("stdio"),
		pending: make(map[any]chan *Message),
	}

	// Wire up the adapter's internal state as if spawnProcess ran.
	scanner := bufio.NewScanner(hostReader)
	scanner.Buffer(make([]byte, 0, scannerInitBuf), scannerMaxBuf)
	a.scanner = scanner
	a.writer = hostWriter
	a.done = make(chan struct{})
	go a.readLoop()

	// Start the mock plugin.
	go plug.serveLoop(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Initialize.
	result, err := a.initialize(ctx)
	if err != nil {
		t.Fatalf("initialize: %v", err)
	}
	if result.PluginName != "test-plugin" {
		t.Errorf("plugin name = %q, want %q", result.PluginName, "test-plugin")
	}
	if !result.Capabilities.SendText {
		t.Error("expected SendText capability")
	}

	// Connect.
	connResult, err := a.callConnect(ctx)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	if !connResult.OK {
		t.Errorf("connect OK = %v", connResult.OK)
	}

	// Disconnect.
	if err := a.callDisconnect(ctx); err != nil {
		t.Fatalf("disconnect: %v", err)
	}
}

func TestAdapterSend(t *testing.T) {
	hostReader, hostWriter, plug := newMockPluginPipes()

	a := &Adapter{
		cfg:     Config{Name: "test"},
		base:    channels.NewBaseAdapter("stdio"),
		pending: make(map[any]chan *Message),
	}
	if !a.base.MarkConnected(nil) {
		t.Fatal("MarkConnected() = false, want true")
	}

	scanner := bufio.NewScanner(hostReader)
	scanner.Buffer(make([]byte, 0, scannerInitBuf), scannerMaxBuf)
	a.scanner = scanner
	a.writer = hostWriter
	a.done = make(chan struct{})
	go a.readLoop()
	go plug.serveLoop(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := a.Send(ctx, channels.OutboundMessage{
		ChannelID: "test",
		TargetID:  "user-1",
		Content:   "hello",
	})
	if err != nil {
		t.Fatalf("send: %v", err)
	}
}

func TestAdapterInboundNotification(t *testing.T) {
	hostReader, hostWriter, plug := newMockPluginPipes()

	a := &Adapter{
		cfg:     Config{Name: "test"},
		base:    channels.NewBaseAdapter("stdio"),
		pending: make(map[any]chan *Message),
	}

	scanner := bufio.NewScanner(hostReader)
	scanner.Buffer(make([]byte, 0, scannerInitBuf), scannerMaxBuf)
	a.scanner = scanner
	a.writer = hostWriter
	a.done = make(chan struct{})
	go a.readLoop()
	sub := a.SubscribeEvents()

	// Plugin sends an inbound notification.
	notifParams, _ := json.Marshal(InboundNotification{
		ChannelID:  "test",
		SenderID:   "user-42",
		SenderName: "Alice",
		Content:    "hello from platform",
	})
	plug.send(&Message{
		JSONRPC: JSONRPC,
		Method:  MethodChannelInbound,
		Params:  notifParams,
	})

	select {
	case msg := <-sub:
		if msg.SenderID != "user-42" {
			t.Errorf("sender_id = %q, want %q", msg.SenderID, "user-42")
		}
		if msg.Content != "hello from platform" {
			t.Errorf("content = %q, want %q", msg.Content, "hello from platform")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for inbound message")
	}
}

func TestAdapterStatusNotification(t *testing.T) {
	hostReader, hostWriter, plug := newMockPluginPipes()

	a := &Adapter{
		cfg:     Config{Name: "test"},
		base:    channels.NewBaseAdapter("stdio"),
		pending: make(map[any]chan *Message),
	}

	scanner := bufio.NewScanner(hostReader)
	scanner.Buffer(make([]byte, 0, scannerInitBuf), scannerMaxBuf)
	a.scanner = scanner
	a.writer = hostWriter
	a.done = make(chan struct{})
	if !a.base.MarkConnected(nil) {
		t.Fatal("MarkConnected() = false, want true")
	}
	go a.readLoop()

	statusParams, _ := json.Marshal(StatusNotification{
		Status:  "error",
		Message: "connection lost",
	})
	plug.send(&Message{
		JSONRPC: JSONRPC,
		Method:  MethodChannelStatus,
		Params:  statusParams,
	})

	// Give the read loop time to process.
	time.Sleep(100 * time.Millisecond)

	if a.Status() != channels.StatusError {
		t.Errorf("status = %q, want %q", a.Status(), channels.StatusError)
	}
}

func TestBuildPluginProcessEnvResolvesRefsAndSanitizesHostEnv(t *testing.T) {
	t.Setenv("HOPCLAW_STDIO_SECRET", "stdio-secret")
	t.Setenv("HOPCLAW_STDIO_LEAK", "host-only")

	env, err := buildPluginProcessEnv(Config{
		Name: "test",
		Env: map[string]string{
			"TOKEN": "env:HOPCLAW_STDIO_SECRET",
			"MODE":  "literal",
		},
	})
	if err != nil {
		t.Fatalf("buildPluginProcessEnv() error = %v", err)
	}
	if got := envSliceValue(env, "TOKEN"); got != "stdio-secret" {
		t.Fatalf("TOKEN = %q, want %q", got, "stdio-secret")
	}
	if got := envSliceValue(env, "MODE"); got != "literal" {
		t.Fatalf("MODE = %q, want %q", got, "literal")
	}
	if got := envSliceValue(env, "HOPCLAW_STDIO_LEAK"); got != "" {
		t.Fatalf("unexpected host env leak = %q", got)
	}
	if got := envSliceValue(env, "PATH"); got == "" {
		t.Fatal("PATH should be present in child env")
	}
}

func envSliceValue(env []string, key string) string {
	for _, entry := range env {
		currentKey, value, ok := strings.Cut(entry, "=")
		if ok && currentKey == key {
			return value
		}
	}
	return ""
}

func TestProtocolTypes(t *testing.T) {
	cap := PluginCapability{
		SendText:     true,
		SendRichText: true,
		Edit:         true,
	}
	cc := cap.ToChannelCapabilities()
	if !cc.SendText || !cc.SendRichText {
		t.Error("capability conversion failed")
	}
	if !cc.ReceiveMessage {
		t.Error("stdio plugins should always receive")
	}

	notif := InboundNotification{
		ChannelID:  "ch",
		SenderID:   "s1",
		SenderName: "Alice",
		Content:    "hi",
	}
	msg := notif.ToInboundMessage()
	if msg.SenderID != "s1" || msg.Content != "hi" {
		t.Error("inbound message conversion failed")
	}
}

func TestMessageHelpers(t *testing.T) {
	resp := &Message{JSONRPC: JSONRPC, ID: 1}
	if !resp.IsResponse() {
		t.Error("expected IsResponse=true")
	}
	if resp.IsNotification() {
		t.Error("expected IsNotification=false")
	}

	notif := &Message{JSONRPC: JSONRPC, Method: "test"}
	if !notif.IsNotification() {
		t.Error("expected IsNotification=true")
	}
	if notif.IsResponse() {
		t.Error("expected IsResponse=false for notification")
	}
}

func TestAdapterCapabilities(t *testing.T) {
	a := New(Config{Name: "test"})
	caps := a.Capabilities()
	if caps.SendText {
		t.Error("expected no capabilities before connect")
	}

	status := a.Status()
	if status != channels.StatusDisconnected {
		t.Errorf("initial status = %q, want %q", status, channels.StatusDisconnected)
	}

	ch := a.SubscribeEvents()
	if ch == nil {
		t.Error("SubscribeEvents returned nil")
	}
}

// Suppress unused warning for mockPlugin.
var _ = strings.TrimSpace

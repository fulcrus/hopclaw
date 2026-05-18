package feishu

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/fulcrus/hopclaw/channels"
	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher"
)

type stubManagedWebsocketClient struct {
	started chan struct{}
	closed  chan struct{}
	done    chan struct{}

	mu         sync.Mutex
	closeCalls int
	closeOnce  sync.Once
}

func newStubManagedWebsocketClient() *stubManagedWebsocketClient {
	return &stubManagedWebsocketClient{
		started: make(chan struct{}),
		closed:  make(chan struct{}),
		done:    make(chan struct{}),
	}
}

func (c *stubManagedWebsocketClient) Start(context.Context) error {
	close(c.started)
	<-c.closed
	close(c.done)
	return nil
}

func (c *stubManagedWebsocketClient) Close() error {
	c.mu.Lock()
	c.closeCalls++
	c.mu.Unlock()
	c.closeOnce.Do(func() { close(c.closed) })
	return nil
}

func (c *stubManagedWebsocketClient) CloseCalls() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.closeCalls
}

func TestAdapterDisconnectWaitsForWebsocketShutdown(t *testing.T) {
	stubClient := newStubManagedWebsocketClient()
	originalFactory := newManagedWebsocketClientFunc
	newManagedWebsocketClientFunc = func(appID, appSecret, domain string, eventHandler *dispatcher.EventDispatcher) websocketClient {
		return stubClient
	}
	defer func() {
		newManagedWebsocketClientFunc = originalFactory
	}()

	adapter := New(Config{
		AppID:          "app-id",
		AppSecret:      "app-secret",
		ConnectionMode: "websocket",
	})

	if err := adapter.Connect(context.Background()); err != nil {
		t.Fatalf("Connect() error = %v", err)
	}

	select {
	case <-stubClient.started:
	case <-time.After(time.Second):
		t.Fatal("websocket client was not started")
	}

	disconnectCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := adapter.Disconnect(disconnectCtx); err != nil {
		t.Fatalf("Disconnect() error = %v", err)
	}

	select {
	case <-stubClient.done:
	default:
		t.Fatal("Disconnect() returned before websocket goroutine exited")
	}
	if stubClient.CloseCalls() != 1 {
		t.Fatalf("CloseCalls() = %d, want 1", stubClient.CloseCalls())
	}
	if got := adapter.Status(); got != channels.StatusDisconnected {
		t.Fatalf("Status() = %q, want %q", got, channels.StatusDisconnected)
	}
}

func TestAdapterDisconnectClosesSubscribersAndReconnectAllowsResubscribe(t *testing.T) {
	t.Parallel()

	adapter := New(Config{
		AppID:     "app-id",
		AppSecret: "app-secret",
	})

	if err := adapter.Connect(context.Background()); err != nil {
		t.Fatalf("Connect() error = %v", err)
	}

	sub := adapter.SubscribeEvents()
	if err := adapter.Disconnect(context.Background()); err != nil {
		t.Fatalf("Disconnect() error = %v", err)
	}
	if err := adapter.Disconnect(context.Background()); err != nil {
		t.Fatalf("Disconnect(second) error = %v", err)
	}

	select {
	case _, ok := <-sub:
		if ok {
			t.Fatal("expected closed subscriber channel")
		}
	default:
		t.Fatal("subscriber channel should be closed after disconnect")
	}

	if err := adapter.Connect(context.Background()); err != nil {
		t.Fatalf("Connect(reconnect) error = %v", err)
	}
	if got := adapter.Status(); got != channels.StatusConnected {
		t.Fatalf("Status() = %q", got)
	}

	sub = adapter.SubscribeEvents()
	select {
	case _, ok := <-sub:
		if !ok {
			t.Fatal("new subscriber channel should stay open after reconnect")
		}
	default:
	}
}

func TestAdapterCapabilitiesReflectCurrentImplementation(t *testing.T) {
	t.Parallel()

	caps := New(Config{}).Capabilities()
	if !caps.SendText {
		t.Fatalf("expected SendText=true, got %#v", caps)
	}
	if caps.SendRichText || caps.SendFile {
		t.Fatalf("unexpected send capabilities: %#v", caps)
	}
	if !caps.ReceiveMessage || !caps.ReceiveEvent {
		t.Fatalf("unexpected receive capabilities: %#v", caps)
	}
}

func TestResolveOpenBaseURLCanonicalizesOpenAPIsSuffix(t *testing.T) {
	t.Parallel()

	cases := []struct {
		input string
		want  string
	}{
		{input: "", want: "https://open.feishu.cn"},
		{input: "feishu", want: "https://open.feishu.cn"},
		{input: "lark", want: "https://open.larksuite.com"},
		{input: "https://open.feishu.cn/open-apis", want: "https://open.feishu.cn"},
		{input: "https://open.larksuite.com/open-apis/", want: "https://open.larksuite.com"},
	}

	for _, tc := range cases {
		if got := resolveOpenBaseURL(tc.input); got != tc.want {
			t.Fatalf("resolveOpenBaseURL(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

func TestNextReconnectBackoffDoublesToCap(t *testing.T) {
	t.Parallel()

	if got := nextReconnectBackoff(0); got != feishuReconnectInitialBackoff {
		t.Fatalf("nextReconnectBackoff(0) = %s, want %s", got, feishuReconnectInitialBackoff)
	}
	if got := nextReconnectBackoff(feishuReconnectInitialBackoff); got != 4*time.Second {
		t.Fatalf("nextReconnectBackoff(initial) = %s, want 4s", got)
	}
	if got := nextReconnectBackoff(feishuReconnectMaxBackoff); got != feishuReconnectMaxBackoff {
		t.Fatalf("nextReconnectBackoff(max) = %s, want %s", got, feishuReconnectMaxBackoff)
	}
}

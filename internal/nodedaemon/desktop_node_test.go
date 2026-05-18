package nodedaemon

import (
	"context"
	"testing"

	desktoptypes "github.com/fulcrus/hopclaw/desktopapi/types"
	"github.com/fulcrus/hopclaw/nodeclient"
)

type fakeNodeRegistrar struct {
	handlers map[string]nodeclient.Handler
}

func (r *fakeNodeRegistrar) Register(command string, handler nodeclient.Handler) {
	if r.handlers == nil {
		r.handlers = make(map[string]nodeclient.Handler)
	}
	r.handlers[command] = handler
}

type fakeDesktopInvoker struct {
	healthErr error
	requests  []desktoptypes.Request
	responses []*desktoptypes.Response
}

func (f *fakeDesktopInvoker) Health(context.Context) error {
	return f.healthErr
}

func (f *fakeDesktopInvoker) Do(_ context.Context, req desktoptypes.Request) (*desktoptypes.Response, error) {
	f.requests = append(f.requests, req)
	if len(f.responses) == 0 {
		return &desktoptypes.Response{OK: true}, nil
	}
	resp := f.responses[0]
	f.responses = f.responses[1:]
	return resp, nil
}

func TestDesktopNodeCommandsByPlatform(t *testing.T) {
	macos := DesktopNodeCommands("macOS")
	if !containsString(macos, "desktop.capture_tree") {
		t.Fatalf("macOS commands missing capture_tree: %#v", macos)
	}
	if !containsString(macos, "desktop.focus_app") {
		t.Fatalf("macOS commands missing focus_app: %#v", macos)
	}
	if !containsString(macos, "desktop.screen_record") || !containsString(macos, "desktop.scroll") {
		t.Fatalf("macOS commands missing screen_record/scroll: %#v", macos)
	}

	linux := DesktopNodeCommands("Linux")
	if containsString(linux, "desktop.capture_tree") {
		t.Fatalf("Linux commands unexpectedly include capture_tree: %#v", linux)
	}
	if !containsString(linux, "desktop.screenshot") {
		t.Fatalf("Linux commands missing screenshot: %#v", linux)
	}
	if !containsString(linux, "desktop.find_text") || !containsString(linux, "desktop.mouse_click") {
		t.Fatalf("Linux commands missing locator/mouse commands: %#v", linux)
	}
}

func TestRegisterDesktopNodeHandlersCreatesEphemeralSession(t *testing.T) {
	registry := &fakeNodeRegistrar{}
	client := &fakeDesktopInvoker{
		responses: []*desktoptypes.Response{
			{OK: true, SessionID: "sess-1"},
			{OK: true, ArtifactRef: "art-1", Data: map[string]any{"kind": "png"}},
			{OK: true, Data: map[string]any{"closed": true}},
		},
	}

	RegisterDesktopNodeHandlers(registry, client, DesktopNodeConfig{
		DeviceID:   "desktop-1",
		DeviceName: "Desk",
		Platform:   "Linux",
		Version:    "1.2.3",
		ListenAddr: "127.0.0.1:9224",
	})

	handler := registry.handlers["desktop.screenshot"]
	if handler == nil {
		t.Fatal("desktop.screenshot handler not registered")
	}

	result, err := handler(context.Background(), map[string]any{})
	if err != nil {
		t.Fatalf("handler error = %v", err)
	}
	if got := result["artifact_ref"]; got != "art-1" {
		t.Fatalf("artifact_ref = %#v, want art-1", got)
	}
	if len(client.requests) != 3 {
		t.Fatalf("requests len = %d, want 3", len(client.requests))
	}
	if got := client.requests[0].Action; got != desktoptypes.ActionCreateSession {
		t.Fatalf("first action = %q, want create_session", got)
	}
	if got := client.requests[1].Action; got != desktoptypes.ActionScreenshot {
		t.Fatalf("second action = %q, want screenshot", got)
	}
	if got := client.requests[1].SessionID; got != "sess-1" {
		t.Fatalf("screenshot session_id = %q, want sess-1", got)
	}
	if got := client.requests[2].Action; got != desktoptypes.ActionCloseSession {
		t.Fatalf("third action = %q, want close_session", got)
	}
}

func TestRegisterDesktopNodeHandlersProxyUsesProvidedSession(t *testing.T) {
	registry := &fakeNodeRegistrar{}
	client := &fakeDesktopInvoker{
		responses: []*desktoptypes.Response{
			{OK: true, Data: map[string]any{"matches": []any{map[string]any{"text": "Terminal"}}}},
		},
	}

	RegisterDesktopNodeHandlers(registry, client, DesktopNodeConfig{
		DeviceID:   "desktop-1",
		DeviceName: "Desk",
		Platform:   "macOS",
		Version:    "1.2.3",
		ListenAddr: "127.0.0.1:9224",
	})

	handler := registry.handlers["desktop.proxy"]
	if handler == nil {
		t.Fatal("desktop.proxy handler not registered")
	}

	result, err := handler(context.Background(), map[string]any{
		"action":     desktoptypes.ActionFindText,
		"session_id": "sess-existing",
		"params": map[string]any{
			"query": "Terminal",
		},
	})
	if err != nil {
		t.Fatalf("proxy handler error = %v", err)
	}
	if len(client.requests) != 1 {
		t.Fatalf("requests len = %d, want 1", len(client.requests))
	}
	if got := client.requests[0].SessionID; got != "sess-existing" {
		t.Fatalf("session_id = %q, want sess-existing", got)
	}
	if got := client.requests[0].Action; got != desktoptypes.ActionFindText {
		t.Fatalf("action = %q, want find_text", got)
	}
	if got, _ := result["matches"].([]any); len(got) != 1 {
		t.Fatalf("matches len = %d, want 1", len(got))
	}
}

func TestRegisterDesktopNodeHandlersStatusUsesHealth(t *testing.T) {
	registry := &fakeNodeRegistrar{}
	client := &fakeDesktopInvoker{}

	RegisterDesktopNodeHandlers(registry, client, DesktopNodeConfig{
		DeviceID:   "desktop-1",
		DeviceName: "Desk",
		Platform:   "Windows",
		Version:    "1.2.3",
		ListenAddr: "127.0.0.1:9224",
	})

	handler := registry.handlers["device.status"]
	if handler == nil {
		t.Fatal("device.status handler not registered")
	}

	result, err := handler(context.Background(), nil)
	if err != nil {
		t.Fatalf("device.status error = %v", err)
	}
	if got := result["daemon"]; got != "desktopd" {
		t.Fatalf("daemon = %#v, want desktopd", got)
	}
}

func containsString(items []string, want string) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}

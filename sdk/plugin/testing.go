package plugin

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

type MockRuntime struct {
	mu       sync.Mutex
	manifest Manifest
	config   map[string]any
	env      map[string]string
	events   []Event
	logs     []string
	emitErr  error
}

func NewMockRuntime() *MockRuntime {
	return &MockRuntime{}
}

func (m *MockRuntime) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.manifest = Manifest{}
	m.config = nil
	m.env = nil
	m.events = nil
	m.logs = nil
	m.emitErr = nil
}

func (m *MockRuntime) Manifest() Manifest {
	m.mu.Lock()
	defer m.mu.Unlock()
	return cloneManifest(m.manifest)
}

func (m *MockRuntime) SetManifest(manifest Manifest) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.manifest = cloneManifest(manifest)
}

func (m *MockRuntime) Config() map[string]any {
	m.mu.Lock()
	defer m.mu.Unlock()
	return cloneMapAny(m.config)
}

func (m *MockRuntime) SetConfig(config map[string]any) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.config = cloneMapAny(config)
}

func (m *MockRuntime) LookupEnv(key string) (string, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	value, ok := m.env[key]
	return value, ok
}

func (m *MockRuntime) SetEnv(key string, value string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.env == nil {
		m.env = make(map[string]string)
	}
	m.env[key] = value
}

func (m *MockRuntime) Emit(_ context.Context, event Event) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.emitErr != nil {
		return m.emitErr
	}
	if event.Time.IsZero() {
		event.Time = time.Now().UTC()
	}
	event.Payload = cloneMapAny(event.Payload)
	m.events = append(m.events, event)
	return nil
}

func (m *MockRuntime) SetEmitError(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.emitErr = err
}

func (m *MockRuntime) Events() []Event {
	m.mu.Lock()
	defer m.mu.Unlock()
	return cloneEvents(m.events)
}

func (m *MockRuntime) EventsNamed(name string) []Event {
	m.mu.Lock()
	defer m.mu.Unlock()
	filtered := make([]Event, 0, len(m.events))
	for _, event := range m.events {
		if event.Name != name {
			continue
		}
		filtered = append(filtered, event)
	}
	return cloneEvents(filtered)
}

func (m *MockRuntime) LastEvent() (Event, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.events) == 0 {
		return Event{}, false
	}
	event := m.events[len(m.events)-1]
	event.Payload = cloneMapAny(event.Payload)
	return event, true
}

func (m *MockRuntime) Logf(format string, args ...any) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.logs = append(m.logs, fmt.Sprintf(format, args...))
}

func (m *MockRuntime) Logs() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return slices.Clone(m.logs)
}

type TestHarness struct {
	Runtime *MockRuntime
}

func NewTestHarness(runtime *MockRuntime) *TestHarness {
	if runtime == nil {
		runtime = NewMockRuntime()
	}
	return &TestHarness{Runtime: runtime}
}

func (h *TestHarness) Connect(ctx context.Context, plugin ChannelPlugin) error {
	if plugin == nil {
		return ErrNotImplemented
	}
	return plugin.Channel().Connect(ctx, h.Runtime)
}

func (h *TestHarness) Send(ctx context.Context, plugin ChannelPlugin, message OutboundMessage) (SendResult, error) {
	if plugin == nil {
		return SendResult{}, ErrNotImplemented
	}
	return plugin.Channel().Send(ctx, h.Runtime, message)
}

func (h *TestHarness) Execute(ctx context.Context, plugin ToolPlugin, request ToolRequest) (ToolOutput, error) {
	if plugin == nil {
		return ToolOutput{}, ErrNotImplemented
	}
	return plugin.Tool().Execute(ctx, h.Runtime, request)
}

func (h *TestHarness) Models(ctx context.Context, plugin ProviderPlugin) ([]ModelInfo, error) {
	return h.ListModels(ctx, plugin)
}

func (h *TestHarness) ListModels(ctx context.Context, plugin ProviderPlugin) ([]ModelInfo, error) {
	if plugin == nil {
		return nil, ErrNotImplemented
	}
	return plugin.Provider().ListModels(ctx, h.Runtime)
}

func (h *TestHarness) Chat(ctx context.Context, plugin ProviderPlugin, request ChatRequest) (ChatResponse, error) {
	if plugin == nil {
		return ChatResponse{}, ErrNotImplemented
	}
	return plugin.Provider().Chat(ctx, h.Runtime, request)
}

func (h *TestHarness) Load(ctx context.Context, hook Hook) error {
	if hook == nil {
		return nil
	}
	return hook.OnLoad(ctx, h.Runtime)
}

func (h *TestHarness) Unload(ctx context.Context, hook Hook) error {
	if hook == nil {
		return nil
	}
	return hook.OnUnload(ctx, h.Runtime)
}

func (h *TestHarness) ConfigChange(ctx context.Context, hook Hook, next map[string]any) error {
	if hook == nil {
		h.Runtime.SetConfig(next)
		return nil
	}
	previous := h.Runtime.Config()
	change := ConfigChange{
		Previous: previous,
		Current:  cloneMapAny(next),
	}
	if err := hook.OnConfigChange(ctx, h.Runtime, change); err != nil {
		return err
	}
	h.Runtime.SetConfig(next)
	return nil
}

func AssertToolOutput(t testing.TB, got ToolOutput, want string) {
	t.Helper()
	if strings.TrimSpace(got.Output) != strings.TrimSpace(want) {
		t.Fatalf("tool output = %q, want %q", got.Output, want)
	}
}

func AssertChatContent(t testing.TB, got ChatResponse, want string) {
	t.Helper()
	if strings.TrimSpace(got.Message.Content) != strings.TrimSpace(want) {
		t.Fatalf("chat content = %q, want %q", got.Message.Content, want)
	}
}

func cloneEvents(src []Event) []Event {
	if len(src) == 0 {
		return nil
	}
	dst := make([]Event, len(src))
	for idx, value := range src {
		value.Payload = cloneMapAny(value.Payload)
		dst[idx] = value
	}
	return dst
}

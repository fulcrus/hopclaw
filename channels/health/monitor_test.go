package health

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/fulcrus/hopclaw/channels"
	channelmgr "github.com/fulcrus/hopclaw/channels/manager"
	"github.com/fulcrus/hopclaw/eventbus"
)

func TestConfigApplyDefaults(t *testing.T) {
	t.Parallel()

	cfg := Config{}
	cfg.applyDefaults()

	if cfg.CheckInterval != defaultCheckInterval {
		t.Fatalf("CheckInterval = %v, want %v", cfg.CheckInterval, defaultCheckInterval)
	}
	if cfg.StaleSocketTimeout != defaultStaleSocketTimeout {
		t.Fatalf("StaleSocketTimeout = %v, want %v", cfg.StaleSocketTimeout, defaultStaleSocketTimeout)
	}
	if cfg.StuckRunTimeout != defaultStuckRunTimeout {
		t.Fatalf("StuckRunTimeout = %v, want %v", cfg.StuckRunTimeout, defaultStuckRunTimeout)
	}
	if cfg.StartupGrace != defaultStartupGrace {
		t.Fatalf("StartupGrace = %v, want %v", cfg.StartupGrace, defaultStartupGrace)
	}
	if cfg.MaxRestartsPerHour != defaultMaxRestartsPerHour {
		t.Fatalf("MaxRestartsPerHour = %d, want %d", cfg.MaxRestartsPerHour, defaultMaxRestartsPerHour)
	}
	if cfg.CooldownAfterRestart != defaultCooldownAfterRestart {
		t.Fatalf("CooldownAfterRestart = %v, want %v", cfg.CooldownAfterRestart, defaultCooldownAfterRestart)
	}
}

func TestConfigApplyDefaultsPreservesCustom(t *testing.T) {
	t.Parallel()

	cfg := Config{
		CheckInterval:      1 * time.Minute,
		StaleSocketTimeout: 10 * time.Minute,
		MaxRestartsPerHour: 3,
	}
	cfg.applyDefaults()

	if cfg.CheckInterval != 1*time.Minute {
		t.Fatalf("CheckInterval = %v, should preserve custom", cfg.CheckInterval)
	}
	if cfg.StaleSocketTimeout != 10*time.Minute {
		t.Fatalf("StaleSocketTimeout = %v, should preserve custom", cfg.StaleSocketTimeout)
	}
	if cfg.MaxRestartsPerHour != 3 {
		t.Fatalf("MaxRestartsPerHour = %d, should preserve custom", cfg.MaxRestartsPerHour)
	}
	// Non-set fields should get defaults.
	if cfg.StuckRunTimeout != defaultStuckRunTimeout {
		t.Fatalf("StuckRunTimeout = %v, want default", cfg.StuckRunTimeout)
	}
}

func TestNewMonitor(t *testing.T) {
	t.Parallel()

	mgr := channelmgr.New()
	bus := eventbus.NewInMemoryBus()
	cfg := Config{}

	m := NewMonitor(cfg, mgr, bus)
	if m == nil {
		t.Fatal("NewMonitor returned nil")
	}

	// Config defaults should be applied.
	if m.cfg.CheckInterval != defaultCheckInterval {
		t.Fatalf("monitor cfg.CheckInterval = %v, want default", m.cfg.CheckInterval)
	}
}

func TestMonitorStatusEmpty(t *testing.T) {
	t.Parallel()

	mgr := channelmgr.New()
	m := NewMonitor(Config{}, mgr, nil)

	statuses := m.Status()
	if len(statuses) != 0 {
		t.Fatalf("expected 0 statuses before Start, got %d", len(statuses))
	}
}

func TestStateConstants(t *testing.T) {
	t.Parallel()

	tests := []struct {
		state State
		want  string
	}{
		{StateConnected, "connected"},
		{StateDisconnected, "disconnected"},
		{StateStaleSock, "stale_socket"},
		{StateStuckRun, "stuck_run"},
		{StateGrace, "startup_grace"},
		{StateUnmanaged, "unmanaged"},
		{StateStopped, "stopped"},
	}

	for _, tt := range tests {
		if string(tt.state) != tt.want {
			t.Fatalf("State %q != %q", tt.state, tt.want)
		}
	}
}

func TestChannelHealthStruct(t *testing.T) {
	t.Parallel()

	h := ChannelHealth{
		Name:         "slack",
		State:        StateConnected,
		RestartCount: 2,
		ActiveRuns:   1,
	}

	if h.Name != "slack" {
		t.Fatalf("Name = %q", h.Name)
	}
	if h.State != StateConnected {
		t.Fatalf("State = %q", h.State)
	}
	if h.RestartCount != 2 {
		t.Fatalf("RestartCount = %d", h.RestartCount)
	}
}

func TestMonitorHandleEventTracksRunLifecycle(t *testing.T) {
	t.Parallel()

	m := &Monitor{
		states: map[string]*channelState{
			"slack": {state: StateConnected},
		},
	}

	m.handleEvent(eventbus.Event{
		Type:  eventbus.EventRunStarted,
		Attrs: map[string]any{"channel": "slack"},
	})
	if got := m.states["slack"].activeRuns; got != 1 {
		t.Fatalf("activeRuns after start = %d, want 1", got)
	}

	m.handleEvent(eventbus.Event{
		Type:  eventbus.EventRunResumed,
		Attrs: map[string]any{"channel": "slack"},
	})
	if got := m.states["slack"].activeRuns; got != 2 {
		t.Fatalf("activeRuns after resume = %d, want 2", got)
	}

	m.handleEvent(eventbus.Event{
		Type:  eventbus.EventRunCompleted,
		Attrs: map[string]any{"channel": "slack"},
	})
	if got := m.states["slack"].activeRuns; got != 1 {
		t.Fatalf("activeRuns after completion = %d, want 1", got)
	}
}

func TestMonitorStopWaitsForLoopExit(t *testing.T) {
	t.Parallel()

	mgr := channelmgr.New()
	adapter := &blockingStatusAdapter{
		entered: make(chan struct{}),
		release: make(chan struct{}),
	}
	if err := mgr.Register("slack", adapter); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	m := NewMonitor(Config{
		CheckInterval: 1 * time.Millisecond,
		StartupGrace:  0,
	}, mgr, nil)
	if err := m.Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	select {
	case <-adapter.entered:
	case <-time.After(time.Second):
		t.Fatal("monitor loop did not reach adapter.Status()")
	}

	stopped := make(chan struct{})
	go func() {
		m.Stop()
		close(stopped)
	}()

	select {
	case <-stopped:
		t.Fatal("Stop() returned before the loop exited")
	case <-time.After(50 * time.Millisecond):
	}

	close(adapter.release)

	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("Stop() did not finish after the loop was released")
	}
}

type blockingStatusAdapter struct {
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func (a *blockingStatusAdapter) Connect(context.Context) error { return nil }

func (a *blockingStatusAdapter) Disconnect(context.Context) error { return nil }

func (a *blockingStatusAdapter) Send(context.Context, channels.OutboundMessage) error { return nil }

func (a *blockingStatusAdapter) Capabilities() channels.ChannelCapabilityDescriptor {
	return channels.Capabilities{SendText: true, ReceiveMessage: true}
}

func (a *blockingStatusAdapter) Status() channels.Status {
	a.once.Do(func() {
		close(a.entered)
		<-a.release
	})
	return channels.StatusConnected
}

func (a *blockingStatusAdapter) SubscribeEvents() <-chan channels.InboundMessage {
	return make(chan channels.InboundMessage)
}

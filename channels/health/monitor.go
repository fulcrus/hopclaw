package health

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/fulcrus/hopclaw/channels"
	channelmgr "github.com/fulcrus/hopclaw/channels/manager"
	"github.com/fulcrus/hopclaw/eventbus"
	"github.com/fulcrus/hopclaw/logging"
)

var log = logging.WithSubsystem("health")

// State represents the health state of a channel connection.
type State string

const (
	StateConnected    State = "connected"
	StateDisconnected State = "disconnected"
	StateStaleSock    State = "stale_socket"
	StateStuckRun     State = "stuck_run"
	StateGrace        State = "startup_grace"
	StateUnmanaged    State = "unmanaged"
	StateStopped      State = "stopped"
)

// ChannelHealth holds the health status of a single channel.
type ChannelHealth struct {
	Name         string    `json:"name"`
	State        State     `json:"state"`
	Since        time.Time `json:"since"`
	RestartCount int       `json:"restart_count"`
	LastError    string    `json:"last_error,omitempty"`
	ActiveRuns   int       `json:"active_runs"`
}

// Config configures the health monitor.
type Config struct {
	CheckInterval        time.Duration
	StaleSocketTimeout   time.Duration
	StuckRunTimeout      time.Duration
	StartupGrace         time.Duration
	MaxRestartsPerHour   int
	CooldownAfterRestart time.Duration
}

const (
	defaultCheckInterval        = 30 * time.Second
	defaultStaleSocketTimeout   = 5 * time.Minute
	defaultStuckRunTimeout      = 10 * time.Minute
	defaultStartupGrace         = 30 * time.Second
	defaultMaxRestartsPerHour   = 5
	defaultCooldownAfterRestart = 10 * time.Second
)

func (c *Config) applyDefaults() {
	if c.CheckInterval <= 0 {
		c.CheckInterval = defaultCheckInterval
	}
	if c.StaleSocketTimeout <= 0 {
		c.StaleSocketTimeout = defaultStaleSocketTimeout
	}
	if c.StuckRunTimeout <= 0 {
		c.StuckRunTimeout = defaultStuckRunTimeout
	}
	if c.StartupGrace <= 0 {
		c.StartupGrace = defaultStartupGrace
	}
	if c.MaxRestartsPerHour <= 0 {
		c.MaxRestartsPerHour = defaultMaxRestartsPerHour
	}
	if c.CooldownAfterRestart <= 0 {
		c.CooldownAfterRestart = defaultCooldownAfterRestart
	}
}

// channelState tracks internal state for a single channel.
type channelState struct {
	state         State
	since         time.Time
	restartCount  int
	restartTimes  []time.Time // sliding window for rate limiting
	lastEventAt   time.Time
	lastError     string
	activeRuns    int
	lastRestartAt time.Time
	skipped       bool // unmanaged flag
}

// Monitor watches channel adapter health and triggers automatic restarts.
type Monitor struct {
	mu       sync.RWMutex // guards states
	lifeMu   sync.Mutex
	wg       sync.WaitGroup
	cfg      Config
	channels *channelmgr.Manager
	bus      *eventbus.InMemoryBus
	states   map[string]*channelState
	cancel   context.CancelFunc
	sub      *eventbus.Subscription
}

// NewMonitor creates a health monitor.
func NewMonitor(cfg Config, channels *channelmgr.Manager, bus *eventbus.InMemoryBus) *Monitor {
	cfg.applyDefaults()
	return &Monitor{
		cfg:      cfg,
		channels: channels,
		bus:      bus,
		states:   make(map[string]*channelState),
	}
}

// Start begins the health check loop.
func (m *Monitor) Start(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	var sub *eventbus.Subscription
	if m.bus != nil {
		sub = m.bus.SubscribeChannel(64)
	}
	m.lifeMu.Lock()
	m.cancel = cancel
	m.sub = sub
	m.lifeMu.Unlock()

	// Subscribe to event bus for run tracking.
	if sub != nil {
		m.wg.Add(1)
		go func() {
			defer m.wg.Done()
			m.trackEvents(ctx, sub)
		}()
	}

	// Initialize states for all registered channels.
	m.mu.Lock()
	now := time.Now()
	for _, name := range m.channels.Names() {
		m.states[name] = &channelState{
			state: StateGrace,
			since: now,
		}
	}
	m.mu.Unlock()

	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		m.loop(ctx)
	}()
	log.Info("channel health monitor started",
		"check_interval", m.cfg.CheckInterval,
		"channels", len(m.channels.Names()))
	return nil
}

// Stop shuts down the monitor.
func (m *Monitor) Stop() {
	m.lifeMu.Lock()
	cancel := m.cancel
	sub := m.sub
	m.cancel = nil
	m.sub = nil
	m.lifeMu.Unlock()

	if cancel != nil {
		cancel()
	}
	if sub != nil {
		sub.Close()
	}
	m.wg.Wait()
}

// Status returns the current health of all monitored channels.
func (m *Monitor) Status() []ChannelHealth {
	m.mu.RLock()
	defer m.mu.RUnlock()

	out := make([]ChannelHealth, 0, len(m.states))
	for name, st := range m.states {
		out = append(out, ChannelHealth{
			Name:         name,
			State:        st.state,
			Since:        st.since,
			RestartCount: st.restartCount,
			LastError:    st.lastError,
			ActiveRuns:   st.activeRuns,
		})
	}
	return out
}

// Skip marks a channel as unmanaged (no automatic restarts).
func (m *Monitor) Skip(name string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if st, ok := m.states[name]; ok {
		st.skipped = true
		st.state = StateUnmanaged
		st.since = time.Now()
	}
}

// Resume re-enables monitoring for a previously skipped channel.
func (m *Monitor) Resume(name string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if st, ok := m.states[name]; ok && st.skipped {
		st.skipped = false
		st.state = StateGrace
		st.since = time.Now()
	}
}

func (m *Monitor) loop(ctx context.Context) {
	ticker := time.NewTicker(m.cfg.CheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.checkAll(ctx)
		}
	}
}

func (m *Monitor) checkAll(ctx context.Context) {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now()

	for _, name := range m.channels.Names() {
		st, ok := m.states[name]
		if !ok {
			st = &channelState{state: StateGrace, since: now}
			m.states[name] = st
		}

		if st.skipped {
			continue
		}

		adapter, ok := m.channels.Get(name)
		if !ok {
			st.state = StateStopped
			st.since = now
			continue
		}

		m.checkOne(ctx, name, adapter, st, now)
	}
}

func (m *Monitor) checkOne(ctx context.Context, name string, adapter channels.Adapter, st *channelState, now time.Time) {
	adapterStatus := adapter.Status()

	// Handle startup grace period.
	if st.state == StateGrace {
		if now.Sub(st.since) < m.cfg.StartupGrace {
			return
		}
		// Grace period expired, evaluate actual state.
	}

	// Check adapter reported status.
	switch adapterStatus {
	case channels.StatusDisconnected, channels.StatusError:
		if st.state != StateDisconnected {
			st.state = StateDisconnected
			st.since = now
			st.lastError = fmt.Sprintf("adapter status: %s", adapterStatus)
		}
		m.tryRestart(ctx, name, adapter, st, now)
		return

	case channels.StatusConnecting:
		// Still connecting, leave current state.
		return

	case channels.StatusConnected:
		// Check for stale socket.
		if !st.lastEventAt.IsZero() && now.Sub(st.lastEventAt) > m.cfg.StaleSocketTimeout {
			if st.state != StateStaleSock {
				st.state = StateStaleSock
				st.since = now
				st.lastError = "no events received within stale socket timeout"
				log.Warn("channel health: stale socket detected",
					"channel", name,
					"last_event_at", st.lastEventAt)
			}
			m.tryRestart(ctx, name, adapter, st, now)
			return
		}

		// Check for stuck runs.
		if st.activeRuns > 0 && !st.lastEventAt.IsZero() && now.Sub(st.lastEventAt) > m.cfg.StuckRunTimeout {
			if st.state != StateStuckRun {
				st.state = StateStuckRun
				st.since = now
				st.lastError = fmt.Sprintf("%d active runs stuck beyond timeout", st.activeRuns)
				log.Warn("channel health: stuck run detected",
					"channel", name,
					"active_runs", st.activeRuns)
			}
			m.tryRestart(ctx, name, adapter, st, now)
			return
		}

		// Everything looks good.
		if st.state != StateConnected {
			st.state = StateConnected
			st.since = now
			st.lastError = ""
		}
	}
}

func (m *Monitor) tryRestart(ctx context.Context, name string, adapter channels.Adapter, st *channelState, now time.Time) {
	// Respect cooldown after restart.
	if !st.lastRestartAt.IsZero() && now.Sub(st.lastRestartAt) < m.cfg.CooldownAfterRestart {
		return
	}

	// Rate limit restarts.
	cutoff := now.Add(-time.Hour)
	recentRestarts := 0
	for _, t := range st.restartTimes {
		if t.After(cutoff) {
			recentRestarts++
		}
	}
	if recentRestarts >= m.cfg.MaxRestartsPerHour {
		log.Warn("channel health: restart rate limit reached",
			"channel", name,
			"restarts_this_hour", recentRestarts)
		return
	}

	log.Info("channel health: restarting channel",
		"channel", name,
		"state", st.state)

	// Disconnect then reconnect.
	if err := adapter.Disconnect(ctx); err != nil {
		log.Warn("channel health: disconnect failed during restart",
			"channel", name,
			"error", err)
	}

	if err := adapter.Connect(ctx); err != nil {
		st.lastError = fmt.Sprintf("restart failed: %s", err)
		log.Warn("channel health: reconnect failed",
			"channel", name,
			"error", err)
		return
	}

	st.restartCount++
	st.restartTimes = append(st.restartTimes, now)
	st.lastRestartAt = now
	st.state = StateGrace
	st.since = now
	st.lastError = ""
	st.activeRuns = 0

	log.Info("channel health: channel restarted successfully", "channel", name)
}

func (m *Monitor) trackEvents(ctx context.Context, sub *eventbus.Subscription) {
	for {
		select {
		case <-ctx.Done():
			return
		case event, ok := <-sub.Events():
			if !ok {
				return
			}
			m.handleEvent(event)
		}
	}
}

func (m *Monitor) handleEvent(event eventbus.Event) {
	channelName := monitorEventChannel(event)
	if channelName == "" {
		return
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	st, ok := m.states[channelName]
	if !ok {
		return
	}

	now := time.Now()
	st.lastEventAt = now

	switch event.Type {
	case eventbus.EventRunStarted, eventbus.EventRunResumed:
		st.activeRuns++
	case eventbus.EventRunCompleted, eventbus.EventRunFailed, eventbus.EventRunCancelled:
		if st.activeRuns > 0 {
			st.activeRuns--
		}
	}
}

func monitorEventChannel(event eventbus.Event) string {
	switch event.Type {
	case eventbus.EventRunStarted:
		if payload, ok := event.RunStartedPayload(); ok {
			return payload.Channel
		}
	case eventbus.EventRunResumed:
		if payload, ok := event.RunResumedPayload(); ok {
			return payload.Channel
		}
	case eventbus.EventRunCompleted:
		if payload, ok := event.RunCompletedPayload(); ok {
			return payload.Channel
		}
	case eventbus.EventRunFailed:
		if payload, ok := event.RunFailedPayload(); ok {
			return payload.Channel
		}
	case eventbus.EventRunCancelled:
		if payload, ok := event.RunCancelledPayload(); ok {
			return payload.Channel
		}
	}
	return ""
}

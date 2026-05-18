package bootstrap

import (
	"context"
	"errors"
	"sort"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/fulcrus/hopclaw/channels"
	channelregistry "github.com/fulcrus/hopclaw/channels/registry"
	"github.com/fulcrus/hopclaw/config"
)

func TestBootstrapApplyBaseConfigRefreshesOnlyChangedBuiltinChannels(t *testing.T) {
	testRefreshGlobalsMu.Lock()
	defer testRefreshGlobalsMu.Unlock()

	recorder := newChannelRuntimeDiffRecorder()
	originalBuildBuiltinChannels := buildBuiltinChannels
	buildBuiltinChannels = func(_ context.Context, deps channelregistry.RuntimeDeps) (channelregistry.RuntimeBuildResult, error) {
		return recorder.build(config.Config{
			Channels: deps.Channels,
			Store:    config.StoreConfig{Path: deps.StorePath},
		}), nil
	}
	defer func() {
		buildBuiltinChannels = originalBuildBuiltinChannels
	}()

	cfg := config.Config{
		Server: config.ServerConfig{Address: "127.0.0.1:0", AuthToken: "test-token"},
		Store:  config.StoreConfig{Backend: "memory"},
		Agent:  config.AgentConfig{DefaultModel: "test-model", MaxToolRounds: 2, QueueMode: "enqueue"},
		Skills: config.SkillsConfig{},
		Tools: config.ToolsConfig{
			Builtins:  config.BuiltinsConfig{Enabled: boolPtr(false), Root: ".", DefaultExecTimeout: 30 * time.Second, MaxReadBytes: 64 * 1024},
			LocalExec: config.LocalExecConfig{Enabled: boolPtr(false), DefaultTimeout: 30 * time.Second},
		},
	}
	cfg.ApplyDefaults()

	app, err := New(context.Background(), cfg, Dependencies{})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer app.Close(context.Background())

	first := cfg
	first.Channels.Slack = config.SlackChannelConfig{
		Enabled:  boolPtr(true),
		BotToken: "slack-bot-v1",
		AppToken: "slack-app-v1",
	}
	first.Channels.Discord = config.DiscordChannelConfig{
		Enabled:  boolPtr(true),
		BotToken: "discord-bot-v1",
	}
	first.ApplyDefaults()

	if err := app.ApplyBaseConfig(context.Background(), first); err != nil {
		t.Fatalf("ApplyBaseConfig(first) error = %v", err)
	}

	slackBefore, slackBridgeBefore := observedChannelRuntime(t, app, "slack")
	discordBefore, discordBridgeBefore := observedChannelRuntime(t, app, "discord")

	second := first
	second.Channels.Slack.AppToken = "slack-app-v2"
	second.ApplyDefaults()

	if err := app.ApplyBaseConfig(context.Background(), second); err != nil {
		t.Fatalf("ApplyBaseConfig(second) error = %v", err)
	}

	slackAfterUpdate, slackBridgeAfterUpdate := observedChannelRuntime(t, app, "slack")
	discordAfterUpdate, discordBridgeAfterUpdate := observedChannelRuntime(t, app, "discord")

	if slackAfterUpdate == slackBefore {
		t.Fatal("slack adapter was not replaced after slack config update")
	}
	if slackBridgeAfterUpdate == slackBridgeBefore {
		t.Fatal("slack bridge was not replaced after slack config update")
	}
	if discordAfterUpdate != discordBefore {
		t.Fatal("discord adapter changed during unrelated slack refresh")
	}
	if discordBridgeAfterUpdate != discordBridgeBefore {
		t.Fatal("discord bridge changed during unrelated slack refresh")
	}
	if slackBefore.connects.Load() != 1 || slackBefore.disconnects.Load() != 1 {
		t.Fatalf("slack old adapter lifecycle = connects:%d disconnects:%d, want 1/1", slackBefore.connects.Load(), slackBefore.disconnects.Load())
	}
	if slackBridgeBefore.starts.Load() != 1 || slackBridgeBefore.stops.Load() != 1 {
		t.Fatalf("slack old bridge lifecycle = starts:%d stops:%d, want 1/1", slackBridgeBefore.starts.Load(), slackBridgeBefore.stops.Load())
	}
	if slackAfterUpdate.connects.Load() != 1 || slackAfterUpdate.disconnects.Load() != 0 {
		t.Fatalf("slack new adapter lifecycle = connects:%d disconnects:%d, want 1/0", slackAfterUpdate.connects.Load(), slackAfterUpdate.disconnects.Load())
	}
	if slackBridgeAfterUpdate.starts.Load() != 1 || slackBridgeAfterUpdate.stops.Load() != 0 {
		t.Fatalf("slack new bridge lifecycle = starts:%d stops:%d, want 1/0", slackBridgeAfterUpdate.starts.Load(), slackBridgeAfterUpdate.stops.Load())
	}
	if discordBefore.connects.Load() != 1 || discordBefore.disconnects.Load() != 0 {
		t.Fatalf("discord adapter lifecycle after slack refresh = connects:%d disconnects:%d, want 1/0", discordBefore.connects.Load(), discordBefore.disconnects.Load())
	}
	if discordBridgeBefore.starts.Load() != 1 || discordBridgeBefore.stops.Load() != 0 {
		t.Fatalf("discord bridge lifecycle after slack refresh = starts:%d stops:%d, want 1/0", discordBridgeBefore.starts.Load(), discordBridgeBefore.stops.Load())
	}

	third := second
	third.Channels.Discord = config.DiscordChannelConfig{}
	third.ApplyDefaults()

	if err := app.ApplyBaseConfig(context.Background(), third); err != nil {
		t.Fatalf("ApplyBaseConfig(third) error = %v", err)
	}

	slackAfterRemove, slackBridgeAfterRemove := observedChannelRuntime(t, app, "slack")
	if slackAfterRemove != slackAfterUpdate {
		t.Fatal("slack adapter changed while removing discord")
	}
	if slackBridgeAfterRemove != slackBridgeAfterUpdate {
		t.Fatal("slack bridge changed while removing discord")
	}
	if _, ok := app.Channels.Get("discord"); ok {
		t.Fatal("discord adapter still registered after removal")
	}
	if findObservedBridge(app.channelBridges, "discord") != nil {
		t.Fatal("discord bridge still active after removal")
	}
	if discordBefore.disconnects.Load() != 1 {
		t.Fatalf("discord adapter disconnects after removal = %d, want 1", discordBefore.disconnects.Load())
	}
	if discordBridgeBefore.stops.Load() != 1 {
		t.Fatalf("discord bridge stops after removal = %d, want 1", discordBridgeBefore.stops.Load())
	}
	if got := app.Channels.Names(); len(got) != 1 || got[0] != "slack" {
		t.Fatalf("channel names after discord removal = %#v, want [slack]", got)
	}
}

func TestBootstrapApplyBaseConfigStopsAllChangedChannelsBeforeRollbackOnDisconnectError(t *testing.T) {
	testRefreshGlobalsMu.Lock()
	defer testRefreshGlobalsMu.Unlock()

	recorder := newChannelRuntimeDiffRecorder()
	originalBuildBuiltinChannels := buildBuiltinChannels
	buildBuiltinChannels = func(_ context.Context, deps channelregistry.RuntimeDeps) (channelregistry.RuntimeBuildResult, error) {
		return recorder.build(config.Config{
			Channels: deps.Channels,
			Store:    config.StoreConfig{Path: deps.StorePath},
		}), nil
	}
	defer func() {
		buildBuiltinChannels = originalBuildBuiltinChannels
	}()

	cfg := config.Config{
		Server: config.ServerConfig{Address: "127.0.0.1:0", AuthToken: "test-token"},
		Store:  config.StoreConfig{Backend: "memory"},
		Agent:  config.AgentConfig{DefaultModel: "test-model", MaxToolRounds: 2, QueueMode: "enqueue"},
		Skills: config.SkillsConfig{},
		Tools: config.ToolsConfig{
			Builtins:  config.BuiltinsConfig{Enabled: boolPtr(false), Root: ".", DefaultExecTimeout: 30 * time.Second, MaxReadBytes: 64 * 1024},
			LocalExec: config.LocalExecConfig{Enabled: boolPtr(false), DefaultTimeout: 30 * time.Second},
		},
	}
	cfg.ApplyDefaults()

	app, err := New(context.Background(), cfg, Dependencies{})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer app.Close(context.Background())

	first := cfg
	first.Channels.Slack = config.SlackChannelConfig{
		Enabled:  boolPtr(true),
		BotToken: "slack-bot-v1",
		AppToken: "slack-app-v1",
	}
	first.Channels.Discord = config.DiscordChannelConfig{
		Enabled:  boolPtr(true),
		BotToken: "discord-bot-v1",
	}
	first.ApplyDefaults()

	if err := app.ApplyBaseConfig(context.Background(), first); err != nil {
		t.Fatalf("ApplyBaseConfig(first) error = %v", err)
	}

	slackBefore, slackBridgeBefore := observedChannelRuntime(t, app, "slack")
	discordBefore, discordBridgeBefore := observedChannelRuntime(t, app, "discord")
	slackBefore.disconnectErr = errors.New("slack disconnect boom")
	discordBefore.disconnectErr = errors.New("discord disconnect boom")

	second := first
	second.Channels.Slack.AppToken = "slack-app-v2"
	second.Channels.Discord.BotToken = "discord-bot-v2"
	second.ApplyDefaults()

	if err := app.ApplyBaseConfig(context.Background(), second); err == nil {
		t.Fatal("expected ApplyBaseConfig(second) to fail")
	}

	if slackBridgeBefore.stops.Load() != 1 || discordBridgeBefore.stops.Load() != 1 {
		t.Fatalf("old bridge stop counts = slack:%d discord:%d, want 1/1", slackBridgeBefore.stops.Load(), discordBridgeBefore.stops.Load())
	}
	if slackBefore.disconnects.Load() != 1 || discordBefore.disconnects.Load() != 1 {
		t.Fatalf("old adapter disconnect counts = slack:%d discord:%d, want 1/1", slackBefore.disconnects.Load(), discordBefore.disconnects.Load())
	}
	if got := app.Channels.Names(); len(got) != 0 {
		t.Fatalf("channel names after failed refresh = %#v, want all changed channels removed", got)
	}
}

type channelRuntimeDiffRecorder struct {
	mu      sync.Mutex
	seq     int
	history map[string][]*channelRuntimeDiffAdapter
	bridges map[string][]*channelRuntimeDiffBridge
}

func newChannelRuntimeDiffRecorder() *channelRuntimeDiffRecorder {
	return &channelRuntimeDiffRecorder{
		history: make(map[string][]*channelRuntimeDiffAdapter),
		bridges: make(map[string][]*channelRuntimeDiffBridge),
	}
}

func (r *channelRuntimeDiffRecorder) build(cfg config.Config) channelregistry.RuntimeBuildResult {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.seq++
	active := channelregistry.BuiltinRuntimeChannelConfigs(cfg)
	names := make([]string, 0, len(active))
	for name := range active {
		names = append(names, name)
	}
	sort.Strings(names)

	installations := make([]channelregistry.Installation, 0, len(names))
	for _, name := range names {
		adapter := &channelRuntimeDiffAdapter{name: name, generation: r.seq}
		bridge := &channelRuntimeDiffBridge{name: name, generation: r.seq}
		r.history[name] = append(r.history[name], adapter)
		r.bridges[name] = append(r.bridges[name], bridge)
		installations = append(installations, channelregistry.Installation{
			Name:    name,
			Adapter: adapter,
			Bridge:  bridge,
		})
	}
	return channelregistry.RuntimeBuildResult{Installations: installations}
}

type channelRuntimeDiffAdapter struct {
	name          string
	generation    int
	connectErr    error
	disconnectErr error
	connects      atomic.Int32
	disconnects   atomic.Int32
}

func (a *channelRuntimeDiffAdapter) Connect(context.Context) error {
	a.connects.Add(1)
	return a.connectErr
}

func (a *channelRuntimeDiffAdapter) Disconnect(context.Context) error {
	a.disconnects.Add(1)
	return a.disconnectErr
}

func (*channelRuntimeDiffAdapter) Send(context.Context, channels.OutboundMessage) error {
	return nil
}

func (*channelRuntimeDiffAdapter) Capabilities() channels.ChannelCapabilityDescriptor {
	return channels.ChannelCapabilityDescriptor{}
}

func (*channelRuntimeDiffAdapter) Status() channels.Status { return channels.StatusConnected }

func (*channelRuntimeDiffAdapter) SubscribeEvents() <-chan channels.InboundMessage { return nil }

type channelRuntimeDiffBridge struct {
	name       string
	generation int
	starts     atomic.Int32
	stops      atomic.Int32
}

func (b *channelRuntimeDiffBridge) Start(context.Context) {
	b.starts.Add(1)
}

func (b *channelRuntimeDiffBridge) Stop() {
	b.stops.Add(1)
}

func observedChannelRuntime(t *testing.T, app *App, name string) (*channelRuntimeDiffAdapter, *channelRuntimeDiffBridge) {
	t.Helper()

	adapter, ok := app.Channels.Get(name)
	if !ok {
		t.Fatalf("channel %q not registered", name)
	}
	observedAdapter, ok := adapter.(*channelRuntimeDiffAdapter)
	if !ok {
		t.Fatalf("channel %q adapter type = %T, want *channelRuntimeDiffAdapter", name, adapter)
	}

	bridge := findObservedBridge(app.channelBridges, name)
	if bridge == nil {
		t.Fatalf("channel %q bridge not active", name)
	}
	return observedAdapter, bridge
}

func findObservedBridge(bridges []namedChannelBridge, name string) *channelRuntimeDiffBridge {
	for _, item := range bridges {
		if item.name != name {
			continue
		}
		if bridge, ok := item.bridge.(*channelRuntimeDiffBridge); ok {
			return bridge
		}
	}
	return nil
}

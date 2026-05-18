package extensions

import (
	"context"
	"testing"
	"time"

	"github.com/fulcrus/hopclaw/agent"
	capregistry "github.com/fulcrus/hopclaw/capability/registry"
	captypes "github.com/fulcrus/hopclaw/capability/types"
	"github.com/fulcrus/hopclaw/channels"
	channelhealth "github.com/fulcrus/hopclaw/channels/health"
	channelmgr "github.com/fulcrus/hopclaw/channels/manager"
	modtypes "github.com/fulcrus/hopclaw/internal/modules"
)

func TestSnapshotAggregatesCapabilitiesChannelsAndTools(t *testing.T) {
	t.Parallel()

	caps := capregistry.New()
	if err := caps.Register(&stubSessionCapability{
		manifest: captypes.Manifest{
			Name:       "browser",
			Kind:       captypes.KindSession,
			Operations: []captypes.OperationSpec{{Name: "click"}},
		},
		health: captypes.Health{Status: captypes.StatusReady},
		sessions: []*captypes.SessionHandle{{
			ID:         "sess-1",
			Capability: "browser",
			CreatedAt:  time.Now().UTC(),
		}},
	}); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	channelsMgr := channelmgr.New()
	if err := channelsMgr.Register("slack", &stubChannelAdapter{}); err != nil {
		t.Fatalf("Register(channel) error = %v", err)
	}

	reg := New(Options{
		Capabilities: caps,
		Channels:     channelsMgr,
		ChannelHealth: staticChannelHealthReader{
			items: []channelhealth.ChannelHealth{{
				Name:  "slack",
				State: channelhealth.StateConnected,
			}},
		},
		Tools: staticToolInventory{defs: []agent.ToolDefinition{
			{Name: "browser.click", Source: "capability"},
			{Name: "fs.read", Source: "builtin"},
		}},
		Modules: modtypes.NewStore(modtypes.BuildCatalog([]modtypes.StaticModule{{
			ManifestValue: modtypes.Manifest{
				ID:       "builtin:core",
				Name:     "core",
				Source:   modtypes.SourceBuiltin,
				Delivery: modtypes.DeliveryEmbedded,
				Level:    modtypes.ModuleLevelManaged,
			},
			HealthValue: modtypes.HealthReport{
				Status:  modtypes.HealthReady,
				Summary: "runtime assembled",
			},
			ContributionsValue: modtypes.Contributions{
				Tools:           []modtypes.Component{{Name: "fs.read"}},
				Channels:        []modtypes.Component{{Name: "slack"}},
				ConfigContracts: []modtypes.Component{{Name: "compat"}},
				RuntimeBridges:  []modtypes.Component{{Name: "openclaw-native-runtime"}},
			},
		}})),
	})

	snapshot := reg.Snapshot(context.Background(), nil)
	if snapshot.ProjectionVersion == "" {
		t.Fatal("expected projection version")
	}
	if snapshot.Counts.CapabilityCount != 1 {
		t.Fatalf("capability_count = %d", snapshot.Counts.CapabilityCount)
	}
	if snapshot.Counts.ChannelCount != 1 {
		t.Fatalf("channel_count = %d", snapshot.Counts.ChannelCount)
	}
	if snapshot.Counts.ToolCount != 2 {
		t.Fatalf("tool_count = %d", snapshot.Counts.ToolCount)
	}
	if snapshot.Counts.ModuleCount != 1 {
		t.Fatalf("module_count = %d", snapshot.Counts.ModuleCount)
	}
	if len(snapshot.Capabilities) != 1 || snapshot.Capabilities[0].SessionCount != 1 {
		t.Fatalf("capabilities = %#v", snapshot.Capabilities)
	}
	if len(snapshot.Channels) != 1 || snapshot.Channels[0].Health == nil {
		t.Fatalf("channels = %#v", snapshot.Channels)
	}
	if snapshot.Channels[0].CapabilityMatrix.EditMessage != true {
		t.Fatalf("channel capability_matrix = %#v", snapshot.Channels[0].CapabilityMatrix)
	}
	if len(snapshot.Tools) != 2 || snapshot.Tools[0].Name != "browser.click" || snapshot.Tools[1].Name != "fs.read" {
		t.Fatalf("tools = %#v", snapshot.Tools)
	}
	if len(snapshot.Modules) != 1 || snapshot.Modules[0].ID != "builtin:core" {
		t.Fatalf("modules = %#v", snapshot.Modules)
	}
	if snapshot.Modules[0].Level != modtypes.ModuleLevelManaged {
		t.Fatalf("module level = %q, want %q", snapshot.Modules[0].Level, modtypes.ModuleLevelManaged)
	}
	if snapshot.Modules[0].Health.Status != modtypes.HealthReady {
		t.Fatalf("module health = %#v", snapshot.Modules[0].Health)
	}
	if snapshot.Modules[0].Contributions.TotalCount != 4 {
		t.Fatalf("module contributions = %#v", snapshot.Modules[0].Contributions)
	}
	if snapshot.Modules[0].Contributions.ToolCount != 1 || snapshot.Modules[0].Contributions.ChannelCount != 1 {
		t.Fatalf("module contribution counts = %#v", snapshot.Modules[0].Contributions)
	}
	if snapshot.Modules[0].Contributions.ConfigContractCount != 1 || snapshot.Modules[0].Contributions.RuntimeBridgeCount != 1 {
		t.Fatalf("module compatibility counts = %#v", snapshot.Modules[0].Contributions)
	}
	if len(snapshot.Modules[0].Contributions.ToolNames) != 1 || snapshot.Modules[0].Contributions.ToolNames[0] != "fs.read" {
		t.Fatalf("module tool names = %#v", snapshot.Modules[0].Contributions.ToolNames)
	}
	if len(snapshot.Modules[0].Contributions.ConfigContractNames) != 1 || snapshot.Modules[0].Contributions.ConfigContractNames[0] != "compat" {
		t.Fatalf("module config contract names = %#v", snapshot.Modules[0].Contributions.ConfigContractNames)
	}
	if len(snapshot.Modules[0].Contributions.RuntimeBridgeNames) != 1 || snapshot.Modules[0].Contributions.RuntimeBridgeNames[0] != "openclaw-native-runtime" {
		t.Fatalf("module runtime bridge names = %#v", snapshot.Modules[0].Contributions.RuntimeBridgeNames)
	}
}

func TestChannelLookupUsesUnifiedInventory(t *testing.T) {
	t.Parallel()

	channelsMgr := channelmgr.New()
	if err := channelsMgr.Register("webhook:ops", &stubChannelAdapter{}); err != nil {
		t.Fatalf("Register(channel) error = %v", err)
	}
	reg := New(Options{Channels: channelsMgr})

	entry, ok := reg.Channel("webhook:ops")
	if !ok {
		t.Fatal("expected channel lookup to succeed")
	}
	if entry.Family != "webhook" {
		t.Fatalf("family = %q", entry.Family)
	}
	if entry.Source != "webhook" {
		t.Fatalf("source = %q", entry.Source)
	}
}

func TestModulesFallbackToManifestInventoryWithUnknownHealth(t *testing.T) {
	t.Parallel()

	reg := New(Options{
		Modules: manifestOnlyModuleInventory{
			manifests: []modtypes.Manifest{{
				ID:       "plugin:legacy-pack",
				Name:     "legacy-pack",
				Source:   modtypes.SourcePlugin,
				Delivery: modtypes.DeliveryManifest,
			}},
		},
	})

	items := reg.Modules()
	if len(items) != 1 {
		t.Fatalf("modules = %#v", items)
	}
	if items[0].Health.Status != modtypes.HealthUnknown {
		t.Fatalf("module health = %#v", items[0].Health)
	}
}

type staticToolInventory struct {
	defs []agent.ToolDefinition
}

func (s staticToolInventory) ToolDefinitions(*agent.Session) []agent.ToolDefinition {
	return append([]agent.ToolDefinition(nil), s.defs...)
}

type staticChannelHealthReader struct {
	items []channelhealth.ChannelHealth
}

func (s staticChannelHealthReader) Status() []channelhealth.ChannelHealth {
	return append([]channelhealth.ChannelHealth(nil), s.items...)
}

type staticModuleInventory struct {
	modules []modtypes.StaticModule
}

func (s staticModuleInventory) Modules() []modtypes.StaticModule {
	return append([]modtypes.StaticModule(nil), s.modules...)
}

func (s staticModuleInventory) Manifests() []modtypes.Manifest {
	out := make([]modtypes.Manifest, 0, len(s.modules))
	for _, item := range s.modules {
		out = append(out, item.Manifest())
	}
	return out
}

type manifestOnlyModuleInventory struct {
	manifests []modtypes.Manifest
}

func (s manifestOnlyModuleInventory) Manifests() []modtypes.Manifest {
	return append([]modtypes.Manifest(nil), s.manifests...)
}

type stubSessionCapability struct {
	manifest captypes.Manifest
	health   captypes.Health
	sessions []*captypes.SessionHandle
}

func (s *stubSessionCapability) Manifest() captypes.Manifest {
	return s.manifest
}

func (s *stubSessionCapability) Health(context.Context) captypes.Health {
	return s.health
}

func (s *stubSessionCapability) Invoke(context.Context, captypes.InvokeRequest) (*captypes.InvokeResult, error) {
	return &captypes.InvokeResult{OK: true}, nil
}

func (s *stubSessionCapability) OpenSession(context.Context, map[string]any) (*captypes.SessionHandle, error) {
	return &captypes.SessionHandle{ID: "opened", Capability: s.manifest.Name, CreatedAt: time.Now().UTC()}, nil
}

func (s *stubSessionCapability) CloseSession(context.Context, string) error {
	return nil
}

func (s *stubSessionCapability) ListSessions() []*captypes.SessionHandle {
	return append([]*captypes.SessionHandle(nil), s.sessions...)
}

type stubChannelAdapter struct{}

func (s *stubChannelAdapter) Connect(context.Context) error                        { return nil }
func (s *stubChannelAdapter) Disconnect(context.Context) error                     { return nil }
func (s *stubChannelAdapter) Send(context.Context, channels.OutboundMessage) error { return nil }
func (s *stubChannelAdapter) Capabilities() channels.ChannelCapabilityDescriptor {
	return channels.Capabilities{SendText: true, ReceiveMessage: true}
}
func (s *stubChannelAdapter) Status() channels.Status { return channels.StatusConnected }
func (s *stubChannelAdapter) SubscribeEvents() <-chan channels.InboundMessage {
	return nil
}
func (s *stubChannelAdapter) EditMessage(context.Context, string, string, string) error {
	return nil
}

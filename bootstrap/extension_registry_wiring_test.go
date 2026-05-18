package bootstrap

import (
	"context"
	"testing"

	"github.com/fulcrus/hopclaw/agent"
	captypes "github.com/fulcrus/hopclaw/capability/types"
	"github.com/fulcrus/hopclaw/channels"
	channelhealth "github.com/fulcrus/hopclaw/channels/health"
	"github.com/fulcrus/hopclaw/internal/modules"
	extregistry "github.com/fulcrus/hopclaw/internal/registry/extensions"
)

type extensionToolInventoryStub struct{}

func (extensionToolInventoryStub) ToolDefinitions(*agent.Session) []agent.ToolDefinition {
	return []agent.ToolDefinition{{Name: "stub.tool"}}
}

type extensionCapabilityInventoryStub struct{}

func (extensionCapabilityInventoryStub) Reports(context.Context) []captypes.Report {
	return []captypes.Report{{
		Manifest: captypes.Manifest{Name: "stub.capability"},
		Health:   captypes.Health{Status: captypes.StatusReady},
	}}
}

func (extensionCapabilityInventoryStub) ListCapabilitySessions(string) []*captypes.SessionHandle {
	return nil
}

type extensionChannelAdapterStub struct{}

func (extensionChannelAdapterStub) Connect(context.Context) error { return nil }

func (extensionChannelAdapterStub) Disconnect(context.Context) error { return nil }

func (extensionChannelAdapterStub) Send(context.Context, channels.OutboundMessage) error { return nil }

func (extensionChannelAdapterStub) Capabilities() channels.ChannelCapabilityDescriptor {
	return channels.Capabilities{SendText: true}
}

func (extensionChannelAdapterStub) Status() channels.Status { return channels.StatusConnected }

func (extensionChannelAdapterStub) SubscribeEvents() <-chan channels.InboundMessage { return nil }

type extensionChannelInventoryStub struct{}

func (extensionChannelInventoryStub) Names() []string { return []string{"stub-channel"} }

func (extensionChannelInventoryStub) Get(string) (channels.Adapter, bool) {
	return extensionChannelAdapterStub{}, true
}

type extensionChannelHealthStub struct{}

func (extensionChannelHealthStub) Status() []channelhealth.ChannelHealth {
	return []channelhealth.ChannelHealth{{Name: "stub-channel", State: channelhealth.StateConnected}}
}

type extensionModuleInventoryStub struct{}

func (extensionModuleInventoryStub) Modules() []modules.StaticModule {
	return []modules.StaticModule{{
		ManifestValue: modules.Manifest{ID: "stub:module", Name: "stub-module"},
		HealthValue: modules.HealthReport{
			Status:  modules.HealthReady,
			Summary: "wired",
		},
	}}
}

func (extensionModuleInventoryStub) Manifests() []modules.Manifest {
	return []modules.Manifest{{ID: "stub:module", Name: "stub-module"}}
}

func TestApplyExtensionRegistryBindingsPopulatesSnapshotSources(t *testing.T) {
	t.Parallel()

	registry := extregistry.New(extregistry.Options{})
	applyExtensionRegistryBindings(registry, extensionRegistryBindings{
		tools:         extensionToolInventoryStub{},
		capabilities:  extensionCapabilityInventoryStub{},
		channels:      extensionChannelInventoryStub{},
		channelHealth: extensionChannelHealthStub{},
		modules:       extensionModuleInventoryStub{},
	})

	snapshot := registry.Snapshot(context.Background(), nil)
	if snapshot.Counts.ToolCount != 1 {
		t.Fatalf("tool count = %d", snapshot.Counts.ToolCount)
	}
	if snapshot.Counts.CapabilityCount != 1 {
		t.Fatalf("capability count = %d", snapshot.Counts.CapabilityCount)
	}
	if snapshot.Counts.ChannelCount != 1 {
		t.Fatalf("channel count = %d", snapshot.Counts.ChannelCount)
	}
	if snapshot.Counts.ModuleCount != 1 {
		t.Fatalf("module count = %d", snapshot.Counts.ModuleCount)
	}
	if len(snapshot.Modules) != 1 || snapshot.Modules[0].Health.Status != modules.HealthReady {
		t.Fatalf("module snapshot = %#v", snapshot.Modules)
	}
}

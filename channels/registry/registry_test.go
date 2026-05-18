package registry

import (
	"context"
	"testing"

	"github.com/fulcrus/hopclaw/channels"
)

type stubAdapter struct{}

func (stubAdapter) Connect(context.Context) error    { return nil }
func (stubAdapter) Disconnect(context.Context) error { return nil }
func (stubAdapter) Send(context.Context, channels.OutboundMessage) error {
	return nil
}
func (stubAdapter) Capabilities() channels.ChannelCapabilityDescriptor {
	return channels.ChannelCapabilityDescriptor{}
}
func (stubAdapter) Status() channels.Status                         { return channels.StatusConnected }
func (stubAdapter) SubscribeEvents() <-chan channels.InboundMessage { return nil }

type stubBridge struct{}

func (stubBridge) Start(context.Context) {}
func (stubBridge) Stop()                 {}

func TestRegistryBuildOrdersInstallations(t *testing.T) {
	t.Parallel()

	reg := New()
	reg.Register(Descriptor{
		Name:  "z-last",
		Order: 20,
		Build: func(context.Context) ([]Installation, error) {
			return []Installation{{
				Name:    "z-last",
				Adapter: stubAdapter{},
				Bridge:  stubBridge{},
			}}, nil
		},
	})
	reg.Register(Descriptor{
		Name:  "a-first",
		Order: 10,
		Build: func(context.Context) ([]Installation, error) {
			return []Installation{{
				Name:    "a-first",
				Adapter: stubAdapter{},
				Bridge:  stubBridge{},
			}}, nil
		},
	})

	installations, err := reg.Build(context.Background())
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	if len(installations) != 2 {
		t.Fatalf("len(installations) = %d, want 2", len(installations))
	}
	if installations[0].Name != "a-first" || installations[1].Name != "z-last" {
		t.Fatalf("installation order = %#v", installations)
	}
}

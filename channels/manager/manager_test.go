package manager

import (
	"context"
	"reflect"
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
func (stubAdapter) Status() channels.Status { return channels.StatusConnected }
func (stubAdapter) SubscribeEvents() <-chan channels.InboundMessage {
	return nil
}

func TestNamesReturnsSortedNames(t *testing.T) {
	t.Parallel()

	mgr := New()
	for _, name := range []string{"slack", "discord", "email"} {
		if err := mgr.Register(name, stubAdapter{}); err != nil {
			t.Fatalf("Register(%q) error = %v", name, err)
		}
	}

	got := mgr.Names()
	want := []string{"discord", "email", "slack"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Names() = %v, want %v", got, want)
	}
}

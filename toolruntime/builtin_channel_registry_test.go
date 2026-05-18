package toolruntime

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/fulcrus/hopclaw/agent"
	"github.com/fulcrus/hopclaw/channels"
	channelhealth "github.com/fulcrus/hopclaw/channels/health"
	channelmgr "github.com/fulcrus/hopclaw/channels/manager"
	extregistry "github.com/fulcrus/hopclaw/internal/registry/extensions"
)

func TestChannelListUsesExtensionRegistry(t *testing.T) {
	t.Parallel()

	builtins := NewBuiltins(BuiltinsConfig{Root: t.TempDir()})
	mgr := channelmgr.New()
	if err := mgr.Register("slack", &builtinRegistryChannelAdapter{}); err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	builtins.ApplyBindings(BuiltinsBindings{
		ExtensionRegistry: extregistry.New(extregistry.Options{
			Channels: mgr,
			ChannelHealth: builtinRegistryHealth{
				items: []channelhealth.ChannelHealth{{Name: "slack", State: channelhealth.StateConnected, Since: time.Now().UTC()}},
			},
		}),
	})

	results, err := builtins.ExecuteBatch(context.Background(), &agent.Run{ID: "run-1"}, &agent.Session{ID: "sess-1"}, []agent.ToolCall{{
		ID:   "call-1",
		Name: "channel.list",
	}})
	if err != nil {
		t.Fatalf("ExecuteBatch() error = %v", err)
	}

	var payload struct {
		Channels []struct {
			Name   string `json:"name"`
			Status string `json:"status"`
		} `json:"channels"`
		Count int `json:"count"`
	}
	if err := json.Unmarshal([]byte(results[0].Content), &payload); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	if payload.Count != 1 || len(payload.Channels) != 1 || payload.Channels[0].Name != "slack" {
		t.Fatalf("payload = %#v", payload)
	}
}

func TestChannelStatusUsesExtensionRegistry(t *testing.T) {
	t.Parallel()

	builtins := NewBuiltins(BuiltinsConfig{Root: t.TempDir()})
	mgr := channelmgr.New()
	if err := mgr.Register("slack", &builtinRegistryChannelAdapter{}); err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	builtins.ApplyBindings(BuiltinsBindings{
		ExtensionRegistry: extregistry.New(extregistry.Options{Channels: mgr}),
	})

	results, err := builtins.ExecuteBatch(context.Background(), &agent.Run{ID: "run-1"}, &agent.Session{ID: "sess-1"}, []agent.ToolCall{{
		ID:   "call-1",
		Name: "channel.status",
		Input: map[string]any{
			"name": "slack",
		},
	}})
	if err != nil {
		t.Fatalf("ExecuteBatch() error = %v", err)
	}

	var payload struct {
		Name           string `json:"name"`
		SupportsEdit   bool   `json:"supports_edit"`
		SupportsDelete bool   `json:"supports_delete"`
	}
	if err := json.Unmarshal([]byte(results[0].Content), &payload); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	if payload.Name != "slack" || !payload.SupportsEdit || !payload.SupportsDelete {
		t.Fatalf("payload = %#v", payload)
	}
}

type builtinRegistryHealth struct {
	items []channelhealth.ChannelHealth
}

func (b builtinRegistryHealth) Status() []channelhealth.ChannelHealth {
	return append([]channelhealth.ChannelHealth(nil), b.items...)
}

type builtinRegistryChannelAdapter struct{}

func (b *builtinRegistryChannelAdapter) Connect(context.Context) error    { return nil }
func (b *builtinRegistryChannelAdapter) Disconnect(context.Context) error { return nil }
func (b *builtinRegistryChannelAdapter) Send(context.Context, channels.OutboundMessage) error {
	return nil
}
func (b *builtinRegistryChannelAdapter) Capabilities() channels.Capabilities {
	return channels.Capabilities{SendText: true, ReceiveMessage: true}
}
func (b *builtinRegistryChannelAdapter) Status() channels.Status { return channels.StatusConnected }
func (b *builtinRegistryChannelAdapter) SubscribeEvents() <-chan channels.InboundMessage {
	return nil
}
func (b *builtinRegistryChannelAdapter) EditMessage(context.Context, string, string, string) error {
	return nil
}
func (b *builtinRegistryChannelAdapter) DeleteMessage(context.Context, string, string) error {
	return nil
}

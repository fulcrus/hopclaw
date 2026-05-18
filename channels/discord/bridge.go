package discord

import (
	"time"

	"github.com/fulcrus/hopclaw/agent"
	"github.com/fulcrus/hopclaw/channels"
	sharedbridge "github.com/fulcrus/hopclaw/channels/shared"
	"github.com/fulcrus/hopclaw/eventbus"
)

// Bridge connects a Discord channel adapter to the HopClaw runtime.
// It delegates all shared bridge logic to the embedded StandardBridge.
type Bridge struct {
	*sharedbridge.StandardBridge
}

// NewBridge creates a new Discord bridge backed by the shared StandardBridge.
func NewBridge(adapter channels.Adapter, runtime sharedbridge.BridgeRuntime, sessions agent.SessionStore, bus *eventbus.InMemoryBus, statusDelay time.Duration) *Bridge {
	return &Bridge{
		StandardBridge: sharedbridge.NewStandardBridge(sharedbridge.StandardBridgeConfig{
			ChannelName:  "discord",
			TargetIDKey:  "channel_id",
			MessageIDKey: "message_id",
			ThreadIDKey:  "thread_id",
		}, adapter, runtime, sessions, bus, statusDelay),
	}
}

// WithThreadBindings sets the thread binding manager for thread-to-session routing.
func (b *Bridge) WithThreadBindings(tb *channels.ThreadBinding) *Bridge {
	if b == nil {
		return nil
	}
	b.StandardBridge.WithThreadBindings(tb)
	return b
}

func (b *Bridge) WithPolicy(policy channels.PolicyConfig) *Bridge {
	if b == nil {
		return nil
	}
	b.StandardBridge.WithPolicy(policy)
	return b
}

func (b *Bridge) WithMessageDeduper(deduper *channels.MessageDeduper) *Bridge {
	if b == nil {
		return nil
	}
	b.StandardBridge.WithMessageDeduper(deduper)
	return b
}

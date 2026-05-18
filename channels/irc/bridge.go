package irc

import (
	"time"

	"github.com/fulcrus/hopclaw/agent"
	"github.com/fulcrus/hopclaw/channels"
	sharedbridge "github.com/fulcrus/hopclaw/channels/shared"
	"github.com/fulcrus/hopclaw/eventbus"
)

type Bridge = sharedbridge.StandardBridge

func NewBridge(adapter channels.Adapter, runtime sharedbridge.BridgeRuntime, sessions agent.SessionStore, bus *eventbus.InMemoryBus, statusDelay time.Duration) *Bridge {
	return sharedbridge.NewStandardBridge(sharedbridge.StandardBridgeConfig{
		ChannelName:      "irc",
		TargetIDKey:      "target",
		MessageIDKey:     "message_id",
		DirectUsesChatID: false,
	}, adapter, runtime, sessions, bus, statusDelay)
}

package mattermost

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
		ChannelName:      "mattermost",
		TargetIDKey:      "channel_id",
		MessageIDKey:     "post_id",
		ThreadIDKey:      "root_id",
		DirectUsesChatID: true,
	}, adapter, runtime, sessions, bus, statusDelay)
}

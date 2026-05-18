package webhook

import (
	"fmt"
	"time"

	"github.com/fulcrus/hopclaw/agent"
	"github.com/fulcrus/hopclaw/channels"
	sharedbridge "github.com/fulcrus/hopclaw/channels/shared"
	"github.com/fulcrus/hopclaw/eventbus"
)

// Bridge connects a webhook adapter to the HopClaw runtime.
// It delegates all shared bridge logic to the embedded StandardBridge.
type Bridge struct {
	*sharedbridge.StandardBridge
	webhookID string
}

func NewBridge(webhookID string, adapter channels.Adapter, runtime sharedbridge.BridgeRuntime, sessions agent.SessionStore, bus *eventbus.InMemoryBus, statusDelay time.Duration) *Bridge {
	return &Bridge{
		webhookID: webhookID,
		StandardBridge: sharedbridge.NewStandardBridge(sharedbridge.StandardBridgeConfig{
			ChannelName:      fmt.Sprintf("webhook:%s", webhookID),
			TargetIDKey:      "sender_id",
			MessageIDKey:     "message_id",
			DirectUsesChatID: true,
		}, adapter, runtime, sessions, bus, statusDelay),
	}
}

package imessage

import (
	"time"

	"github.com/fulcrus/hopclaw/channels"
	"github.com/fulcrus/hopclaw/channels/registration"
)

func init() {
	registration.RegisterBuiltinProvider(func(deps registration.RuntimeDeps, _ registration.DescriptorState) []registration.Descriptor {
		cfg := deps.Channels.IMessage
		return registration.SharedBridgeDescriptors(
			deps,
			"imessage",
			cfg,
			registration.ChannelActive(cfg.Enabled, cfg.BaseURL),
			"chat_guid",
			"message_id",
			"",
			channels.PolicyConfig{},
			false,
			func() channels.Adapter {
				return New(Config{BaseURL: cfg.BaseURL, APIKey: cfg.APIKey})
			},
			func(adapter channels.Adapter) registration.BridgeLifecycle {
				return NewBridge(adapter, deps.RuntimeService, deps.Sessions, deps.Bus, deps.StatusDelay).
					WithPolicy(channels.PolicyConfig{}).
					WithMessageDeduper(channels.NewMessageDeduper(registration.ChannelDedupePath(deps.StorePath, "imessage", ""), 24*time.Hour)).
					WithThreadBindings(deps.ThreadBindings)
			},
		)
	})
}

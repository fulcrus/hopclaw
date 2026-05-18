package twitch

import (
	"time"

	"github.com/fulcrus/hopclaw/channels"
	"github.com/fulcrus/hopclaw/channels/registration"
)

func init() {
	registration.RegisterBuiltinProvider(func(deps registration.RuntimeDeps, _ registration.DescriptorState) []registration.Descriptor {
		cfg := deps.Channels.Twitch
		return registration.SharedBridgeDescriptors(
			deps,
			"twitch",
			cfg,
			registration.ChannelActive(cfg.Enabled, cfg.OAuthToken, cfg.Nick),
			"channel",
			"message_id",
			"",
			channels.PolicyConfig{},
			false,
			func() channels.Adapter {
				return New(Config{OAuthToken: cfg.OAuthToken, Nick: cfg.Nick, Channels: cfg.Channels})
			},
			func(adapter channels.Adapter) registration.BridgeLifecycle {
				return NewBridge(adapter, deps.RuntimeService, deps.Sessions, deps.Bus, deps.StatusDelay).
					WithPolicy(channels.PolicyConfig{}).
					WithMessageDeduper(channels.NewMessageDeduper(registration.ChannelDedupePath(deps.StorePath, "twitch", ""), 24*time.Hour)).
					WithThreadBindings(deps.ThreadBindings)
			},
		)
	})
}

package irc

import (
	"time"

	"github.com/fulcrus/hopclaw/channels"
	"github.com/fulcrus/hopclaw/channels/registration"
)

func init() {
	registration.RegisterBuiltinProvider(func(deps registration.RuntimeDeps, _ registration.DescriptorState) []registration.Descriptor {
		cfg := deps.Channels.IRC
		return registration.SharedBridgeDescriptors(
			deps,
			"irc",
			cfg,
			registration.ChannelActive(cfg.Enabled, cfg.Server, cfg.Nick),
			"channel",
			"message_id",
			"",
			channels.PolicyConfig{},
			false,
			func() channels.Adapter {
				return New(Config{Server: cfg.Server, Nick: cfg.Nick, Password: cfg.Password, UseTLS: cfg.UseTLS, Channels: cfg.Channels})
			},
			func(adapter channels.Adapter) registration.BridgeLifecycle {
				return NewBridge(adapter, deps.RuntimeService, deps.Sessions, deps.Bus, deps.StatusDelay).
					WithPolicy(channels.PolicyConfig{}).
					WithMessageDeduper(channels.NewMessageDeduper(registration.ChannelDedupePath(deps.StorePath, "irc", ""), 24*time.Hour)).
					WithThreadBindings(deps.ThreadBindings)
			},
		)
	})
}

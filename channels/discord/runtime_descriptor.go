package discord

import (
	"github.com/fulcrus/hopclaw/channels"
	"github.com/fulcrus/hopclaw/channels/registration"
)

func init() {
	registration.RegisterBuiltinProvider(func(deps registration.RuntimeDeps, _ registration.DescriptorState) []registration.Descriptor {
		cfg := deps.Channels.Discord
		if !registration.ChannelActive(cfg.Enabled, cfg.BotToken) {
			return nil
		}
		policy := registration.CommonChannelPolicy(cfg.CommonChannelConfig)
		return registration.SharedBridgeDescriptors(
			deps,
			"discord",
			cfg,
			true,
			"channel_id",
			"message_id",
			"",
			policy,
			false,
			func() channels.Adapter {
				return New(Config{BotToken: cfg.BotToken})
			},
		)
	})
}

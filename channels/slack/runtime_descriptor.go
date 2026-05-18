package slack

import (
	"github.com/fulcrus/hopclaw/channels"
	"github.com/fulcrus/hopclaw/channels/registration"
)

func init() {
	registration.RegisterBuiltinProvider(func(deps registration.RuntimeDeps, _ registration.DescriptorState) []registration.Descriptor {
		cfg := deps.Channels.Slack
		if !registration.ChannelActive(cfg.Enabled, cfg.BotToken, cfg.AppToken) {
			return nil
		}
		policy := registration.CommonChannelPolicy(cfg.CommonChannelConfig)
		return registration.SharedBridgeDescriptors(
			deps,
			"slack",
			cfg,
			true,
			"channel",
			"ts",
			"thread_ts",
			policy,
			false,
			func() channels.Adapter {
				return New(Config{BotToken: cfg.BotToken, AppToken: cfg.AppToken})
			},
		)
	})
}

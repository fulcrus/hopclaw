package zalouser

import (
	"github.com/fulcrus/hopclaw/channels"
	"github.com/fulcrus/hopclaw/channels/registration"
)

func init() {
	registration.RegisterBuiltinProvider(func(deps registration.RuntimeDeps, _ registration.DescriptorState) []registration.Descriptor {
		cfg := deps.Channels.ZaloUser
		return registration.SharedBridgeDescriptors(
			deps,
			"zalouser",
			cfg,
			registration.ChannelActive(cfg.Enabled, cfg.Cookie),
			"user_id",
			"message_id",
			"",
			channels.PolicyConfig{},
			false,
			func() channels.Adapter {
				return New(Config{Cookie: cfg.Cookie, IMEI: cfg.IMEI, BaseURL: cfg.BaseURL})
			},
		)
	})
}

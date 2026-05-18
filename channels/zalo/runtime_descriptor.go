package zalo

import (
	"github.com/fulcrus/hopclaw/channels"
	"github.com/fulcrus/hopclaw/channels/registration"
)

func init() {
	registration.RegisterBuiltinProvider(func(deps registration.RuntimeDeps, _ registration.DescriptorState) []registration.Descriptor {
		cfg := deps.Channels.Zalo
		return registration.SharedBridgeDescriptors(
			deps,
			"zalo",
			cfg,
			registration.ChannelActive(cfg.Enabled, cfg.AccessToken),
			"user_id",
			"message_id",
			"",
			channels.PolicyConfig{},
			false,
			func() channels.Adapter {
				return New(Config{
					AppID:        cfg.AppID,
					SecretKey:    cfg.SecretKey,
					AccessToken:  cfg.AccessToken,
					RefreshToken: cfg.RefreshToken,
				})
			},
		)
	})
}

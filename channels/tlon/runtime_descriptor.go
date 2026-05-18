package tlon

import (
	"github.com/fulcrus/hopclaw/channels"
	"github.com/fulcrus/hopclaw/channels/registration"
)

func init() {
	registration.RegisterBuiltinProvider(func(deps registration.RuntimeDeps, _ registration.DescriptorState) []registration.Descriptor {
		cfg := deps.Channels.Tlon
		return registration.SharedBridgeDescriptors(
			deps,
			"tlon",
			cfg,
			registration.ChannelActive(cfg.Enabled, cfg.ShipURL, cfg.ShipCode),
			"channel_path",
			"message_id",
			"",
			channels.PolicyConfig{},
			false,
			func() channels.Adapter {
				return New(Config{ShipURL: cfg.ShipURL, ShipCode: cfg.ShipCode})
			},
		)
	})
}

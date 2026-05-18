package bluebubbles

import (
	"github.com/fulcrus/hopclaw/channels"
	"github.com/fulcrus/hopclaw/channels/registration"
)

func init() {
	registration.RegisterBuiltinProvider(func(deps registration.RuntimeDeps, _ registration.DescriptorState) []registration.Descriptor {
		cfg := deps.Channels.BlueBubbles
		return registration.SharedBridgeDescriptors(
			deps,
			"bluebubbles",
			cfg,
			registration.ChannelActive(cfg.Enabled, cfg.BaseURL, cfg.Password),
			"chat_guid",
			"message_id",
			"",
			channels.PolicyConfig{},
			false,
			func() channels.Adapter {
				return New(Config{BaseURL: cfg.BaseURL, Password: cfg.Password})
			},
		)
	})
}

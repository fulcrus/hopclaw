package nextcloudtalk

import (
	"github.com/fulcrus/hopclaw/channels"
	"github.com/fulcrus/hopclaw/channels/registration"
)

func init() {
	registration.RegisterBuiltinProvider(func(deps registration.RuntimeDeps, _ registration.DescriptorState) []registration.Descriptor {
		cfg := deps.Channels.NextcloudTalk
		return registration.SharedBridgeDescriptors(
			deps,
			"nextcloud-talk",
			cfg,
			registration.ChannelActive(cfg.Enabled, cfg.BaseURL, cfg.Username),
			"room_token",
			"message_id",
			"",
			channels.PolicyConfig{},
			false,
			func() channels.Adapter {
				return New(Config{BaseURL: cfg.BaseURL, Username: cfg.Username, Password: cfg.Password})
			},
		)
	})
}
